package seed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SourceKind identifies how initial repository content is obtained.
type SourceKind string

const (
	SourceDirectory SourceKind = "directory"
	SourceZIP       SourceKind = "zip"
	SourceTemplate  SourceKind = "template"
)

// Source describes one seed source. Path is used by directory and zip sources;
// Template is passed unchanged to a TemplateMaterializer.
type Source struct {
	Kind     SourceKind
	Path     string
	Template string
}

// TemplateMaterializer supplies content from a GitHub template repository. The
// GitHub client owns this interface so staging remains independent of its API.
type TemplateMaterializer interface {
	MaterializeTemplate(ctx context.Context, template string, destination string) error
}

// StageOptions controls resource limits for ZIP sources.
type StageOptions struct {
	ZipLimits ZipLimits
}

// StageSource materializes source into workspace/seed. The destination is empty
// on success and may safely be used as the input to ApplyOverlay.
func StageSource(ctx context.Context, workspace *Workspace, source Source, materializer TemplateMaterializer, options StageOptions) (string, error) {
	if workspace == nil || workspace.Root == "" {
		return "", errors.New("workspace is required")
	}
	destination := workspace.SeedDir()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return "", fmt.Errorf("create seed directory: %w", err)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return "", fmt.Errorf("read seed directory: %w", err)
	}
	if len(entries) != 0 {
		return "", errors.New("seed directory is not empty")
	}

	switch source.Kind {
	case SourceDirectory:
		if source.Path == "" {
			return "", errors.New("directory seed path is required")
		}
		if err := copyDirectory(source.Path, destination); err != nil {
			return "", err
		}
	case SourceZIP:
		if source.Path == "" {
			return "", errors.New("zip seed path is required")
		}
		if err := ExtractZIP(source.Path, destination, options.ZipLimits); err != nil {
			return "", err
		}
	case SourceTemplate:
		if source.Template == "" {
			return "", errors.New("template seed is required")
		}
		if materializer == nil {
			return "", errors.New("template source requires a template materializer")
		}
		if err := materializer.MaterializeTemplate(ctx, source.Template, destination); err != nil {
			return "", fmt.Errorf("materialize template: %w", err)
		}
		if err := validateDirectory(destination, true); err != nil {
			return "", fmt.Errorf("validate materialized template: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported seed source %q", source.Kind)
	}
	return destination, nil
}

func copyDirectory(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("stat seed directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("seed directory is not a directory: %s", source)
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if isGitPath(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("seed directory contains a symbolic link: %s", rel)
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("seed directory contains a non-regular file: %s", rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return copyFile(path, target, entry.Type().Perm())
	})
}

func validateDirectory(root string, rejectGit bool) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if rejectGit && isGitPath(rel) {
			return fmt.Errorf("seed contains prohibited .git path: %s", rel)
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("seed contains a symbolic link: %s", rel)
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("seed contains a non-regular file: %s", rel)
		}
		return nil
	})
}

func copyFile(source, destination string, permissions fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, permissions&0o777)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func isGitPath(value string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(value), func(r rune) bool { return r == '/' }) {
		if strings.EqualFold(part, ".git") {
			return true
		}
	}
	return false
}
