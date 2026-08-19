package seed

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ZipLimits bounds resource consumption while extracting an untrusted archive.
type ZipLimits struct {
	MaxEntries           int
	MaxUncompressedBytes uint64
}

func (l ZipLimits) withDefaults() ZipLimits {
	if l.MaxEntries == 0 {
		l.MaxEntries = 10_000
	}
	if l.MaxUncompressedBytes == 0 {
		l.MaxUncompressedBytes = 100 << 20
	}
	return l
}

// ExtractZIP safely extracts an archive into an empty destination directory.
// It rejects paths that could escape destination, symbolic links, and .git paths.
func ExtractZIP(archivePath, destination string, limits ZipLimits) error {
	limits = limits.withDefaults()
	if limits.MaxEntries < 1 || limits.MaxUncompressedBytes < 1 {
		return errors.New("zip limits must be positive")
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return fmt.Errorf("read zip destination: %w", err)
	}
	if len(entries) != 0 {
		return errors.New("zip destination is not empty")
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer func() { _ = reader.Close() }()
	if len(reader.File) > limits.MaxEntries {
		return fmt.Errorf("zip archive has more than %d entries", limits.MaxEntries)
	}

	var total uint64
	seen := make(map[string]struct{}, len(reader.File))
	for _, file := range reader.File {
		name, err := safeZIPName(file.Name)
		if err != nil {
			return err
		}
		if _, exists := seen[name]; exists && !file.FileInfo().IsDir() {
			return fmt.Errorf("zip archive contains duplicate file %q", name)
		}
		seen[name] = struct{}{}
		if file.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("zip archive contains a symbolic link: %s", file.Name)
		}
		if !file.FileInfo().IsDir() && !file.Mode().IsRegular() {
			return fmt.Errorf("zip archive contains a non-regular file: %s", file.Name)
		}
		if file.UncompressedSize64 > limits.MaxUncompressedBytes-total {
			return fmt.Errorf("zip archive exceeds %d uncompressed bytes", limits.MaxUncompressedBytes)
		}
		total += file.UncompressedSize64
	}

	for _, file := range reader.File {
		name, _ := safeZIPName(file.Name)
		target := filepath.Join(destination, filepath.FromSlash(name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("create zip directory: %w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create zip parent directory: %w", err)
		}
		input, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %q: %w", name, err)
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, file.Mode().Perm()&0o777) //nolint:gosec // target is checked by ensureWithin before opening
		if err != nil {
			_ = input.Close()
			return fmt.Errorf("create zip entry %q: %w", name, err)
		}
		_, copyErr := io.Copy(output, input) //nolint:gosec // per-entry and total expansion limits are enforced before copying
		closeOutErr := output.Close()
		closeInErr := input.Close()
		if copyErr != nil {
			return fmt.Errorf("extract zip entry %q: %w", name, copyErr)
		}
		if closeOutErr != nil || closeInErr != nil {
			return fmt.Errorf("close zip entry %q: %w", name, errors.Join(closeOutErr, closeInErr))
		}
	}
	return nil
}

func safeZIPName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") {
		return "", fmt.Errorf("zip archive contains an invalid path: %q", name)
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || trimmed == "." || path.IsAbs(trimmed) || path.Clean(trimmed) != trimmed {
		return "", fmt.Errorf("zip archive contains an unsafe path: %q", name)
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == ".." {
			return "", fmt.Errorf("zip archive contains an unsafe path: %q", name)
		}
	}
	if isGitPath(trimmed) {
		return "", fmt.Errorf("zip archive contains prohibited .git path: %s", name)
	}
	return trimmed, nil
}
