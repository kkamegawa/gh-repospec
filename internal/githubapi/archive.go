package githubapi

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxArchiveBytes         int64 = 64 << 20
	maxArchiveFiles               = 10_000
	maxArchiveExpandedBytes int64 = 256 << 20
)

// MaterializeTemplateArchive downloads a repository archive and safely expands
// its content into a new destination directory. The destination must not already
// exist, which prevents an archive from silently overwriting staged content.
func (c *Client) MaterializeTemplateArchive(ctx context.Context, template Repository, ref, destination string) error {
	if template.Owner == "" || template.Name == "" {
		return errors.New("template repository owner and name are required")
	}
	if ref == "" {
		return errors.New("template ref is required")
	}
	if destination == "" {
		return errors.New("archive destination is required")
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("archive destination already exists: %q", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect archive destination %q: %w", destination, err)
	}

	archive, err := c.downloadArchive(ctx, template, ref)
	if err != nil {
		return err
	}
	archivePath := archive.Name()
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archivePath)
	}()

	info, err := archive.Stat()
	if err != nil {
		return fmt.Errorf("inspect downloaded template archive: %w", err)
	}
	reader, err := zip.NewReader(archive, info.Size())
	if err != nil {
		return fmt.Errorf("open downloaded template archive: %w", err)
	}
	if err := validateArchive(reader.File); err != nil {
		return err
	}

	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("create template destination parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".repobootstrapper-template-")
	if err != nil {
		return fmt.Errorf("create template staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := extractArchive(reader.File, stage); err != nil {
		return err
	}
	if err := os.Rename(stage, destination); err != nil {
		return fmt.Errorf("finalize template archive at %q: %w", destination, err)
	}
	return nil
}

func (c *Client) downloadArchive(ctx context.Context, template Repository, ref string) (*os.File, error) {
	requestURL, err := c.resolveAPIPath(repoPath(template) + "/zipball/" + escape(ref))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create template archive request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download template archive: %w", err)
	}
	if apiErr := responseError(http.MethodGet, repoPath(template)+"/zipball/"+ref, resp); apiErr != nil {
		return nil, apiErr
	}
	defer func() { _ = resp.Body.Close() }()

	temporary, err := os.CreateTemp("", "repobootstrapper-template-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create template archive file: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporary.Name())
		}
	}()
	written, err := io.Copy(temporary, io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		_ = temporary.Close()
		return nil, fmt.Errorf("save template archive: %w", err)
	}
	if written > maxArchiveBytes {
		_ = temporary.Close()
		return nil, fmt.Errorf("template archive exceeds %d byte limit", maxArchiveBytes)
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		_ = temporary.Close()
		return nil, fmt.Errorf("rewind template archive: %w", err)
	}
	removeTemporary = false
	return temporary, nil
}

func validateArchive(files []*zip.File) error {
	if len(files) == 0 {
		return errors.New("template archive is empty")
	}
	if len(files) > maxArchiveFiles {
		return fmt.Errorf("template archive contains more than %d files", maxArchiveFiles)
	}
	var expanded int64
	for _, file := range files {
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("template archive contains symbolic link %q", file.Name)
		}
		if !file.FileInfo().IsDir() && !file.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("template archive contains unsupported file type %q", file.Name)
		}
		if err := validateArchivePath(file.Name); err != nil {
			return err
		}
		if file.UncompressedSize64 > uint64(maxArchiveExpandedBytes-expanded) {
			return fmt.Errorf("template archive exceeds %d byte expanded limit", maxArchiveExpandedBytes)
		}
		expanded += int64(file.UncompressedSize64) //nolint:gosec // the size is bounded above by maxArchiveExpandedBytes
	}
	return nil
}

func validateArchivePath(value string) error {
	if value == "" || strings.ContainsRune(value, '\x00') || strings.Contains(value, "\\") {
		return fmt.Errorf("unsafe path in template archive: %q", value)
	}
	trimmed := strings.TrimSuffix(value, "/")
	clean := path.Clean(trimmed)
	if path.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != trimmed {
		return fmt.Errorf("unsafe path in template archive: %q", value)
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".git" {
			return fmt.Errorf("template archive path must not contain .git: %q", value)
		}
	}
	return nil
}

func extractArchive(files []*zip.File, destination string) error {
	root := archiveRoot(files)
	for _, file := range files {
		relative := strings.TrimPrefix(path.Clean(file.Name), root)
		relative = strings.TrimPrefix(relative, "/")
		if relative == "" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if err := ensureWithin(destination, target); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return fmt.Errorf("create template directory %q: %w", relative, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fmt.Errorf("create parent directory for %q: %w", relative, err)
		}
		source, err := file.Open()
		if err != nil {
			return fmt.Errorf("open archive entry %q: %w", file.Name, err)
		}
		targetFile, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, file.Mode()&0o777) //nolint:gosec // target is validated to stay within destination
		if err != nil {
			_ = source.Close()
			return fmt.Errorf("create template file %q: %w", relative, err)
		}
		_, copyErr := io.Copy(targetFile, source) //nolint:gosec // archive size was validated before extraction
		closeErr := targetFile.Close()
		readCloseErr := source.Close()
		if copyErr != nil {
			return fmt.Errorf("extract template file %q: %w", relative, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close template file %q: %w", relative, closeErr)
		}
		if readCloseErr != nil {
			return fmt.Errorf("close archive entry %q: %w", relative, readCloseErr)
		}
	}
	return nil
}

func archiveRoot(files []*zip.File) string {
	var root string
	for _, file := range files {
		clean := path.Clean(file.Name)
		first, _, found := strings.Cut(clean, "/")
		if !found {
			if file.FileInfo().IsDir() {
				continue
			}
			return ""
		}
		if root == "" {
			root = first
			continue
		}
		if root != first {
			return ""
		}
	}
	return root
}

func ensureWithin(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("resolve template extraction path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("template archive path escapes destination: %q", target)
	}
	return nil
}
