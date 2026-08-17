package seed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	// ErrCommandNotApproved is returned unless the caller explicitly approved a command ID.
	ErrCommandNotApproved = errors.New("command has not been approved")
	// ErrOutputLimit is returned when command output exceeds its configured limit.
	ErrOutputLimit = errors.New("command output exceeded its limit")
	// ErrCommandTimeout is returned when a command exceeds its configured timeout.
	ErrCommandTimeout = errors.New("command timed out")
)

// Command is an argv-only external command definition. Program is executed
// directly; no command shell or shell expression is supported.
type Command struct {
	ID          string
	Program     string
	Args        []string
	CWD         string
	Timeout     time.Duration
	Environment []string
}

// CommandOptions supplies runtime approvals and output-safety settings.
type CommandOptions struct {
	ApprovedCommandIDs map[string]bool
	Redactions         []string
	OutputLimit        int
}

// CommandResult is safe to display: configured redaction values are replaced.
type CommandResult struct {
	Program       string
	Output        string
	OutputTrimmed bool
	Duration      time.Duration
}

// RunApprovedCommand runs an explicitly approved command inside workspace. It
// inherits only the named environment variables and closes stdin by default.
func RunApprovedCommand(ctx context.Context, workspace *Workspace, definition Command, options CommandOptions) (CommandResult, error) {
	if workspace == nil || workspace.Root == "" {
		return CommandResult{}, errors.New("workspace is required")
	}
	if definition.ID == "" || !options.ApprovedCommandIDs[definition.ID] {
		return CommandResult{}, ErrCommandNotApproved
	}
	if definition.Program == "" || strings.ContainsRune(definition.Program, '\x00') {
		return CommandResult{}, errors.New("command program is required")
	}
	if isShellProgram(definition.Program) {
		return CommandResult{}, fmt.Errorf("shell program %q is not allowed", definition.Program)
	}
	for _, arg := range definition.Args {
		if strings.ContainsRune(arg, '\x00') {
			return CommandResult{}, errors.New("command arguments may not contain NUL")
		}
	}
	workingDirectory, err := confinedDirectory(workspace.Root, definition.CWD)
	if err != nil {
		return CommandResult{}, err
	}
	program, err := exec.LookPath(definition.Program)
	if err != nil {
		return CommandResult{}, fmt.Errorf("resolve command program: %w", err)
	}
	if program, err = filepath.EvalSymlinks(program); err != nil {
		return CommandResult{}, fmt.Errorf("resolve command program symlinks: %w", err)
	}
	environment, err := selectedEnvironment(definition.Environment)
	if err != nil {
		return CommandResult{}, err
	}
	outputLimit := options.OutputLimit
	if outputLimit == 0 {
		outputLimit = 64 << 10
	}
	if outputLimit < 1 {
		return CommandResult{}, errors.New("command output limit must be positive")
	}

	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	timedOut := false
	if definition.Timeout > 0 {
		var cancelTimeout context.CancelFunc
		runContext, cancelTimeout = context.WithTimeout(runContext, definition.Timeout)
		defer cancelTimeout()
	}
	buffer := newCappedBuffer(outputLimit, cancel)
	command := exec.CommandContext(runContext, program, definition.Args...)
	command.Dir = workingDirectory
	command.Env = environment
	command.Stdin = nil
	command.Stdout = buffer
	command.Stderr = buffer
	started := time.Now()
	err = command.Run()
	duration := time.Since(started)
	if definition.Timeout > 0 && errors.Is(runContext.Err(), context.DeadlineExceeded) {
		timedOut = true
	}
	result := CommandResult{
		Program:       program,
		Output:        redact(buffer.String(), options.Redactions),
		OutputTrimmed: buffer.Exceeded(),
		Duration:      duration,
	}
	if buffer.Exceeded() {
		return result, ErrOutputLimit
	}
	if timedOut {
		return result, ErrCommandTimeout
	}
	if err != nil {
		return result, fmt.Errorf("run command %q: %w", definition.ID, err)
	}
	return result, nil
}

func confinedDirectory(root, requested string) (string, error) {
	if requested == "" {
		requested = "."
	}
	if filepath.IsAbs(requested) {
		return "", errors.New("command cwd must be relative to the workspace")
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	candidate := filepath.Join(rootResolved, requested)
	candidateResolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve command cwd: %w", err)
	}
	rel, err := filepath.Rel(rootResolved, candidateResolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("command cwd escapes the workspace")
	}
	info, err := os.Stat(candidateResolved)
	if err != nil {
		return "", fmt.Errorf("stat command cwd: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("command cwd is not a directory")
	}
	return candidateResolved, nil
}

func selectedEnvironment(names []string) ([]string, error) {
	seen := map[string]bool{}
	environment := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, "=\x00") {
			return nil, fmt.Errorf("invalid allowed environment variable name %q", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if value, exists := os.LookupEnv(name); exists {
			environment = append(environment, name+"="+value)
		}
	}
	if runtime.GOOS == "windows" {
		// Windows processes require SystemRoot to start reliably. It is not user
		// configuration and is never reported as inherited command input.
		if value, exists := os.LookupEnv("SystemRoot"); exists && !seen["SystemRoot"] {
			environment = append(environment, "SystemRoot="+value)
		}
	}
	return environment, nil
}

func isShellProgram(program string) bool {
	name := strings.ToLower(filepath.Base(program))
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case "sh", "bash", "zsh", "fish", "dash", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

type cappedBuffer struct {
	mu       sync.Mutex
	contents []byte
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func newCappedBuffer(limit int, cancel context.CancelFunc) *cappedBuffer {
	return &cappedBuffer{limit: limit, cancel: cancel}
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - len(b.contents)
	if remaining > 0 {
		if len(value) > remaining {
			b.contents = append(b.contents, value[:remaining]...)
		} else {
			b.contents = append(b.contents, value...)
		}
	}
	if len(value) > remaining {
		b.exceeded = true
		b.cancel()
	}
	return len(value), nil
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.contents)
}

func (b *cappedBuffer) Exceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exceeded
}

func redact(value string, redactions []string) string {
	for _, secret := range redactions {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "***")
		}
	}
	return value
}

var _ io.Writer = (*cappedBuffer)(nil)
