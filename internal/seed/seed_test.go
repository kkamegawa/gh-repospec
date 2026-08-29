package seed

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkamegawa/gh-repospec/internal/githubapi"
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
	content, err := os.ReadFile(filepath.Join(destination, ".github", "test.yml")) //nolint:gosec // destination is a t.TempDir path
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
	file, err := os.Create(archive) //nolint:gosec // archive is a t.TempDir path
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

func TestExtractZIPEnforcesResourceLimitsAndDestinationInvariant(t *testing.T) {
	tests := []struct {
		name        string
		entries     []zipEntry
		limits      ZipLimits
		destination map[string]string
		want        string
	}{
		{
			name:    "entry limit",
			entries: []zipEntry{{name: "one.txt", content: "one"}, {name: "two.txt", content: "two"}},
			limits:  ZipLimits{MaxEntries: 1},
			want:    "more than 1 entries",
		},
		{
			name:    "expanded byte limit",
			entries: []zipEntry{{name: "payload.txt", content: "ab"}},
			limits:  ZipLimits{MaxUncompressedBytes: 1},
			want:    "exceeds 1 uncompressed bytes",
		},
		{
			name:    "duplicate file",
			entries: []zipEntry{{name: "same.txt", content: "one"}, {name: "same.txt", content: "two"}},
			want:    "duplicate file",
		},
		{
			name:        "non-empty destination",
			entries:     []zipEntry{{name: "README.md", content: "seed"}},
			destination: map[string]string{"existing.txt": "keep"},
			want:        "destination is not empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "seed.zip")
			writeZIPEntries(t, archive, test.entries)
			destination := filepath.Join(t.TempDir(), "destination")
			if err := os.Mkdir(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			for path, content := range test.destination {
				writeFile(t, filepath.Join(destination, path), content)
			}
			err := ExtractZIP(archive, destination, test.limits)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ExtractZIP() error = %v, want %q", err, test.want)
			}
		})
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
	content, err := os.ReadFile(filepath.Join(target, "different.txt")) //nolint:gosec // target is a t.TempDir path
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

// TestStageSourceDirectoryPreservesReadableFilePermissions guards against
// copying files with fs.DirEntry.Type().Perm(), which always yields zero
// because DirEntry.Type() carries only file-type bits, never permission
// bits. That bug creates every staged file with mode 000, which Windows
// tolerates but Unix does not: the file becomes unreadable immediately
// after being staged, breaking every directory seed on Linux and macOS.
func TestStageSourceDirectoryPreservesReadableFilePermissions(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "README.md"), "hello")
	workspace, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	destination, err := StageSource(context.Background(), workspace, Source{Kind: SourceDirectory, Path: source}, nil, StageOptions{})
	if err != nil {
		t.Fatalf("StageSource() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "README.md")) //nolint:gosec // destination is the isolated staging workspace
	if err != nil {
		t.Fatalf("staged file is not readable: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("staged file content = %q, want %q", content, "hello")
	}
}

func TestStageSourceTemplateWithArchiveMaterializer(t *testing.T) {
	archive := writeTemplateArchive(t, map[string]string{
		"template-sha/":          "",
		"template-sha/README.md": "hello from template",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repos/octo/template/zipball/main"; got != want {
			t.Errorf("request path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	client, err := githubapi.NewClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := NewWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })

	materializer := archiveMaterializer{
		client: client,
		repo:   githubapi.Repository{Owner: "octo", Name: "template"},
		ref:    "main",
	}
	destination, err := StageSource(context.Background(), workspace, Source{
		Kind: SourceTemplate, Template: "octo/template",
	}, materializer, StageOptions{})
	if err != nil {
		t.Fatalf("StageSource() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "README.md")) //nolint:gosec // destination is a t.TempDir path
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(content), "hello from template"; got != want {
		t.Fatalf("README content = %q, want %q", got, want)
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
	environment := []string{"GO_WANT_HELPER_PROCESS", "COMMAND_TEST_VALUE"}
	if os.Getenv("GOCOVERDIR") != "" {
		environment = append(environment, "GOCOVERDIR")
	}
	base := Command{
		ID:          "test",
		Program:     os.Args[0],
		CWD:         "project",
		Environment: environment,
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
	command.Timeout = 0
	command.Args = []string{"-test.run=TestRunApprovedCommand", "--", "orphan"}
	options.OutputLimit = 64 << 10
	started := time.Now()
	_, err = RunApprovedCommand(context.Background(), workspace, command, options)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("orphaned output pipe error = %v, want exec.ErrWaitDelay", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("command with an orphaned output pipe took %s, want less than %s", elapsed, time.Second)
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
		_, _ = fmt.Fprint(os.Stdout, args[separator+2])
	case "cwd":
		current, _ := os.Getwd()
		_, _ = fmt.Fprint(os.Stdout, current)
	case "env":
		_, _ = fmt.Fprint(os.Stdout, os.Getenv("COMMAND_TEST_VALUE"))
	case "large":
		_, _ = fmt.Fprint(os.Stdout, strings.Repeat("x", 1024))
	case "sleep":
		time.Sleep(time.Second)
	case "orphan":
		child := exec.Command(args[0], "-test.run=TestRunApprovedCommand", "--", "sleep") //nolint:gosec // the helper launches the current test binary only
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(4)
		}
	default:
		os.Exit(3)
	}
	os.Exit(0)
}

type archiveMaterializer struct {
	client *githubapi.Client
	repo   githubapi.Repository
	ref    string
}

func (m archiveMaterializer) MaterializeTemplate(ctx context.Context, _ string, destination string) error {
	return m.client.MaterializeTemplateArchive(ctx, m.repo, m.ref, destination)
}

func writeTemplateArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range entries {
		var entry io.Writer
		var err error
		if strings.HasSuffix(name, "/") {
			header := &zip.FileHeader{Name: name}
			header.SetMode(os.ModeDir | 0o755)
			entry, err = writer.CreateHeader(header)
		} else {
			entry, err = writer.Create(name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func writeZIP(t *testing.T, archive string, files map[string]string) {
	t.Helper()
	file, err := os.Create(archive) //nolint:gosec // archive is a t.TempDir path
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

type zipEntry struct {
	name    string
	content string
}

func writeZIPEntries(t *testing.T, archive string, entries []zipEntry) {
	t.Helper()
	file, err := os.Create(archive) //nolint:gosec // archive is a t.TempDir path
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		output, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := output.Write([]byte(entry.content)); err != nil {
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
