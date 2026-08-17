package seed

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractZIP(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "seed.zip")
	writeZIP(t, archive, map[string]string{
		"README.md":        "hello",
		".github/test.yml": "name: test",
	})
	destination := filepath.Join(t.TempDir(), "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ExtractZIP(archive, destination, ZipLimits{}); err != nil {
		t.Fatalf("ExtractZIP() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destination, ".github", "test.yml"))
	if err != nil || string(content) != "name: test" {
		t.Fatalf("unexpected extracted content %q, %v", content, err)
	}
}

func TestExtractZIPRejectsUnsafeEntries(t *testing.T) {
	cases := []string{"../escape", ".git/config", "dir\\file"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := safeZIPName(name); err == nil {
				t.Fatal("safeZIPName() unexpectedly accepted unsafe path")
			}
		})
	}
}

func TestExtractZIPRejectsSymbolicLink(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "seed.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "link"}
	header.SetMode(fs.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err == nil {
		_, err = entry.Write([]byte("target"))
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ExtractZIP(archive, destination, ZipLimits{}); err == nil {
		t.Fatal("ExtractZIP() unexpectedly accepted symbolic link")
	}
}

func TestApplyOverlayRequiresExplicitOverwrite(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	for _, directory := range []string{source, target} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(source, "same.txt"), "same")
	writeFile(t, filepath.Join(source, "different.txt"), "new")
	writeFile(t, filepath.Join(source, "new.txt"), "new file")
	writeFile(t, filepath.Join(target, "same.txt"), "same")
	writeFile(t, filepath.Join(target, "different.txt"), "old")

	_, err := ApplyOverlay(source, target, OverlayOptions{})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || len(conflict.Paths) != 1 || conflict.Paths[0] != "different.txt" {
		t.Fatalf("ApplyOverlay() error = %v, want one different.txt conflict", err)
	}
	if _, err := os.Stat(filepath.Join(target, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("overlay wrote before conflict detection: %v", err)
	}

	result, err := ApplyOverlay(source, target, OverlayOptions{OverwritePaths: map[string]bool{"different.txt": true}})
	if err != nil {
		t.Fatalf("ApplyOverlay() error = %v", err)
	}
	if strings.Join(result.Added, ",") != "new.txt" || strings.Join(result.Unchanged, ",") != "same.txt" || strings.Join(result.Overwritten, ",") != "different.txt" {
		t.Fatalf("unexpected overlay result: %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(target, "different.txt"))
	if err != nil || string(content) != "new" {
		t.Fatalf("overwrite content = %q, %v", content, err)
	}
}

func TestStageSourceDirectorySkipsGitMetadata(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "README.md"), "hello")
	writeFile(t, filepath.Join(source, ".git", "config"), "metadata")
	workspace, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	destination, err := StageSource(context.Background(), workspace, Source{Kind: SourceDirectory, Path: source}, nil, StageOptions{})
	if err != nil {
		t.Fatalf("StageSource() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "README.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".git was staged: %v", err)
	}
}

func TestRunApprovedCommand(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		commandHelperProcess()
		return
	}
	workspace, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	if err := os.Mkdir(filepath.Join(workspace.Root, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("COMMAND_TEST_VALUE", "visible-only-to-command")
	base := Command{
		ID:          "test",
		Program:     os.Args[0],
		CWD:         "project",
		Environment: []string{"GO_WANT_HELPER_PROCESS", "COMMAND_TEST_VALUE"},
	}
	options := CommandOptions{ApprovedCommandIDs: map[string]bool{"test": true}, Redactions: []string{"token-value"}}

	base.Args = []string{"-test.run=TestRunApprovedCommand", "--", "echo", "token-value"}
	result, err := RunApprovedCommand(context.Background(), workspace, base, options)
	if err != nil || result.Output != "***" {
		t.Fatalf("RunApprovedCommand() = %#v, %v", result, err)
	}
	base.Args = []string{"-test.run=TestRunApprovedCommand", "--", "cwd"}
	result, err = RunApprovedCommand(context.Background(), workspace, base, options)
	if err != nil || filepath.Base(strings.TrimSpace(result.Output)) != "project" {
		t.Fatalf("command cwd = %q, %v", result.Output, err)
	}
	base.Args = []string{"-test.run=TestRunApprovedCommand", "--", "env"}
	result, err = RunApprovedCommand(context.Background(), workspace, base, options)
	if err != nil || strings.TrimSpace(result.Output) != "visible-only-to-command" {
		t.Fatalf("command environment = %q, %v", result.Output, err)
	}
}

func TestRunApprovedCommandSafetyLimits(t *testing.T) {
	workspace, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	if err := os.Mkdir(filepath.Join(workspace.Root, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := Command{ID: "test", Program: os.Args[0], CWD: "project", Environment: []string{"GO_WANT_HELPER_PROCESS"}}
	options := CommandOptions{ApprovedCommandIDs: map[string]bool{"test": true}, OutputLimit: 32}
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	command.Args = []string{"-test.run=TestRunApprovedCommand", "--", "large"}
	result, err := RunApprovedCommand(context.Background(), workspace, command, options)
	if !errors.Is(err, ErrOutputLimit) || !result.OutputTrimmed || len(result.Output) != 32 {
		t.Fatalf("output limit result = %#v, %v", result, err)
	}
	command.Args = []string{"-test.run=TestRunApprovedCommand", "--", "sleep"}
	command.Timeout = 20 * time.Millisecond
	_, err = RunApprovedCommand(context.Background(), workspace, command, options)
	if !errors.Is(err, ErrCommandTimeout) {
		t.Fatalf("timeout error = %v", err)
	}
	command.Program = "sh"
	if _, err := RunApprovedCommand(context.Background(), workspace, command, options); err == nil {
		t.Fatal("shell program was accepted")
	}
	command.Program = os.Args[0]
	command.CWD = ".."
	if _, err := RunApprovedCommand(context.Background(), workspace, command, options); err == nil {
		t.Fatal("escaping cwd was accepted")
	}
	command.CWD = "project"
	if _, err := RunApprovedCommand(context.Background(), workspace, Command{ID: "not-approved", Program: os.Args[0]}, options); !errors.Is(err, ErrCommandNotApproved) {
		t.Fatalf("unapproved command error = %v", err)
	}
}

func commandHelperProcess() {
	args := os.Args
	separator := -1
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator == -1 || separator+1 >= len(args) {
		os.Exit(2)
	}
	switch args[separator+1] {
	case "echo":
		fmt.Fprint(os.Stdout, args[separator+2])
	case "cwd":
		current, _ := os.Getwd()
		fmt.Fprint(os.Stdout, current)
	case "env":
		fmt.Fprint(os.Stdout, os.Getenv("COMMAND_TEST_VALUE"))
	case "large":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 1024))
	case "sleep":
		time.Sleep(time.Second)
	default:
		os.Exit(3)
	}
	os.Exit(0)
}

func writeZIP(t *testing.T, archive string, files map[string]string) {
	t.Helper()
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err == nil {
			_, err = entry.Write([]byte(content))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, filename, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
