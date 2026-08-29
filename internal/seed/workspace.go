// Package seed prepares repository content in an isolated local workspace.
package seed

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Workspace is a temporary directory used to stage seed content and run approved commands.
// Call Close when the workspace is no longer needed.
type Workspace struct {
	Root string

	mu     sync.Mutex
	closed bool
}

// NewWorkspace creates an empty, private temporary workspace.
func NewWorkspace() (*Workspace, error) {
	root, err := os.MkdirTemp("", "gh-repospec-")
	if err != nil {
		return nil, fmt.Errorf("create temporary workspace: %w", err)
	}
	return &Workspace{Root: root}, nil
}

// SeedDir returns the directory into which a seed source is materialized.
func (w *Workspace) SeedDir() string {
	return filepath.Join(w.Root, "seed")
}

// Close removes the temporary workspace. It is safe to call more than once.
func (w *Workspace) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if w.Root == "" {
		return errors.New("workspace root is empty")
	}
	w.closed = true
	if err := os.RemoveAll(w.Root); err != nil {
		return fmt.Errorf("remove temporary workspace: %w", err)
	}
	return nil
}
