package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/kkamegawa/gh-repospec/internal/cli"
)

func TestExitCodeMapsResults(t *testing.T) {
	t.Parallel()

	t.Run("nil error succeeds silently", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		if got := exitCode(nil, &stderr); got != 0 {
			t.Fatalf("exitCode(nil) = %d, want 0", got)
		}
		if stderr.Len() != 0 {
			t.Fatalf("exitCode(nil) stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("drift error exits 2 without an error line", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		if got := exitCode(&cli.DriftError{Changes: 1}, &stderr); got != 2 {
			t.Fatalf("exitCode(DriftError) = %d, want 2", got)
		}
		if stderr.Len() != 0 {
			t.Fatalf("exitCode(DriftError) stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("other errors exit 1 with an error line", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		if got := exitCode(errors.New("boom"), &stderr); got != 1 {
			t.Fatalf("exitCode(err) = %d, want 1", got)
		}
		if got, want := stderr.String(), "error: boom\n"; got != want {
			t.Fatalf("exitCode(err) stderr = %q, want %q", got, want)
		}
	})
}

// TestMainPropagatesExitCode proves main wires exitCode into os.Exit, using
// the subprocess re-exec pattern already used in internal/seed/seed_test.go.
// Only offline cases are reachable here; exit 2 needs a live GitHub read and
// is covered by the planOutcome unit tests in internal/cli.
func TestMainPropagatesExitCode(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		runHelperProcess()
		return
	}

	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{"version succeeds", []string{"version"}, 0},
		{"unknown command fails", []string{"not-a-command"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdArgs := append([]string{"-test.run=TestMainPropagatesExitCode", "--"}, tt.args...)
			cmd := exec.Command(os.Args[0], cmdArgs...) //nolint:gosec // the helper launches the current test binary only
			cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("cmd.Run() error = %v, stderr = %q", err, stderr.String())
				}
				return
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("cmd.Run() error = %v, want *exec.ExitError", err)
			}
			if got := exitErr.ExitCode(); got != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (stderr = %q)", got, tt.wantCode, stderr.String())
			}
		})
	}
}

// runHelperProcess re-invokes main's own mapping directly. The test binary's
// own main is the testing harness, not the CLI, so the helper calls
// exitCode(cli.Run(...)) itself rather than exec'ing anything further.
func runHelperProcess() {
	args := os.Args
	separator := -1
	for index, arg := range args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator == -1 {
		os.Exit(1)
	}
	os.Exit(exitCode(cli.Run(args[separator+1:], os.Stdin, os.Stdout, os.Stderr), os.Stderr))
}
