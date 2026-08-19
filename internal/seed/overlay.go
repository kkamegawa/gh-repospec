package seed

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// OverlayOptions defines the only files that may replace different target files.
// Paths always use forward slashes relative to the source root.
type OverlayOptions struct {
	OverwritePaths map[string]bool
}

// OverlayResult describes a non-destructive seed overlay.
type OverlayResult struct {
	Added       []string
	Unchanged   []string
	Overwritten []string
}

// ConflictError is returned before any files are written if a seed would replace
// a different file without an explicit path-level overwrite permission.
type ConflictError struct {
	Paths []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("seed conflicts with existing files: %v", e.Paths)
}

// ApplyOverlay copies files from source into target. Existing files with equal
// content are no-ops. Different files require options.OverwritePaths[path].
func ApplyOverlay(source, target string, options OverlayOptions) (OverlayResult, error) {
	if err := validateDirectory(source, false); err != nil {
		return OverlayResult{}, fmt.Errorf("validate seed source: %w", err)
	}
	if err := ensureDirectory(target); err != nil {
		return OverlayResult{}, err
	}
	files, err := regularFiles(source)
	if err != nil {
		return OverlayResult{}, err
	}
	result := OverlayResult{}
	conflicts := make([]string, 0)
	for _, rel := range files {
		destination := filepath.Join(target, filepath.FromSlash(rel))
		status, err := compareFile(filepath.Join(source, filepath.FromSlash(rel)), destination)
		if err != nil {
			return OverlayResult{}, fmt.Errorf("compare %s: %w", rel, err)
		}
		switch status {
		case fileMissing:
			result.Added = append(result.Added, rel)
		case fileSame:
			result.Unchanged = append(result.Unchanged, rel)
		case fileDifferent:
			if options.OverwritePaths[rel] {
				result.Overwritten = append(result.Overwritten, rel)
			} else {
				conflicts = append(conflicts, rel)
			}
		}
	}
	if len(conflicts) != 0 {
		return OverlayResult{}, &ConflictError{Paths: conflicts}
	}
	for _, rel := range append(append([]string{}, result.Added...), result.Overwritten...) {
		sourcePath := filepath.Join(source, filepath.FromSlash(rel))
		destination := filepath.Join(target, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return OverlayResult{}, fmt.Errorf("create target parent for %s: %w", rel, err)
		}
		if _, err := os.Lstat(destination); err == nil {
			if err := os.Remove(destination); err != nil {
				return OverlayResult{}, fmt.Errorf("replace %s: %w", rel, err)
			}
		}
		info, err := os.Stat(sourcePath)
		if err != nil {
			return OverlayResult{}, err
		}
		if err := copyFile(sourcePath, destination, info.Mode().Perm()); err != nil {
			return OverlayResult{}, fmt.Errorf("copy %s: %w", rel, err)
		}
	}
	sort.Strings(result.Added)
	sort.Strings(result.Unchanged)
	sort.Strings(result.Overwritten)
	return result, nil
}

func ensureDirectory(directory string) error {
	info, err := os.Stat(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(directory, 0o700)
		}
		return fmt.Errorf("stat target directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("target is not a directory: %s", directory)
	}
	return filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("target contains a symbolic link: %s", path)
		}
		return nil
	})
}

func regularFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

type compareStatus uint8

const (
	fileMissing compareStatus = iota
	fileSame
	fileDifferent
)

func compareFile(source, destination string) (compareStatus, error) {
	destinationInfo, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return fileMissing, nil
	}
	if err != nil {
		return fileDifferent, err
	}
	if destinationInfo.Mode()&fs.ModeSymlink != 0 || !destinationInfo.Mode().IsRegular() {
		return fileDifferent, nil
	}
	sourceHash, err := hashFile(source)
	if err != nil {
		return fileDifferent, err
	}
	destinationHash, err := hashFile(destination)
	if err != nil {
		return fileDifferent, err
	}
	if bytes.Equal(sourceHash, destinationHash) {
		return fileSame, nil
	}
	return fileDifferent, nil
}

func hashFile(filename string) ([]byte, error) {
	file, err := os.Open(filename) //nolint:gosec // callers derive filename from validated seed/target roots
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}
