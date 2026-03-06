//go:build linux

package nsjail

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/happyhackingspace/sindoq/pkg/fs"
)

// nsjailFS implements fs.FileSystem for nsjail sandboxes.
// It operates directly on the sandbox's workspace directory.
type nsjailFS struct {
	instance *Instance
}

// Read reads file contents.
func (f *nsjailFS) Read(ctx context.Context, path string) ([]byte, error) {
	fullPath := f.resolvePath(path)
	return os.ReadFile(fullPath)
}

// Write writes data to a file.
func (f *nsjailFS) Write(ctx context.Context, path string, data []byte) error {
	fullPath := f.resolvePath(path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, data, 0644)
}

// Delete removes a file or directory.
func (f *nsjailFS) Delete(ctx context.Context, path string) error {
	fullPath := f.resolvePath(path)
	return os.RemoveAll(fullPath)
}

// List lists files in a directory.
func (f *nsjailFS) List(ctx context.Context, path string) ([]fs.FileInfo, error) {
	fullPath := f.resolvePath(path)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	files := make([]fs.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fs.FileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(path, entry.Name()),
			Size:    info.Size(),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime(),
		})
	}

	return files, nil
}

// Exists checks if a path exists.
func (f *nsjailFS) Exists(ctx context.Context, path string) (bool, error) {
	fullPath := f.resolvePath(path)
	_, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Stat returns file information.
func (f *nsjailFS) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	fullPath := f.resolvePath(path)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	return &fs.FileInfo{
		Name:    info.Name(),
		Path:    path,
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
		Mode:    uint32(info.Mode()),
	}, nil
}

// Upload uploads a local file to the sandbox.
func (f *nsjailFS) Upload(ctx context.Context, localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}
	return f.Write(ctx, remotePath, data)
}

// UploadReader uploads content from a reader to the sandbox.
func (f *nsjailFS) UploadReader(ctx context.Context, reader io.Reader, remotePath string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read content: %w", err)
	}
	return f.Write(ctx, remotePath, data)
}

// Download downloads a file from the sandbox.
func (f *nsjailFS) Download(ctx context.Context, remotePath string, writer io.Writer) error {
	data, err := f.Read(ctx, remotePath)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

// MkDir creates a directory.
func (f *nsjailFS) MkDir(ctx context.Context, path string) error {
	fullPath := f.resolvePath(path)
	return os.MkdirAll(fullPath, 0755)
}

// Copy copies a file within the sandbox.
func (f *nsjailFS) Copy(ctx context.Context, src, dst string) error {
	srcPath := f.resolvePath(src)
	dstPath := f.resolvePath(dst)

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return f.copyDir(srcPath, dstPath)
	}

	return f.copyFile(srcPath, dstPath)
}

// copyFile copies a single file.
func (f *nsjailFS) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// copyDir recursively copies a directory.
func (f *nsjailFS) copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return f.copyFile(path, dstPath)
	})
}

// Move moves/renames a file within the sandbox.
func (f *nsjailFS) Move(ctx context.Context, src, dst string) error {
	srcPath := f.resolvePath(src)
	dstPath := f.resolvePath(dst)
	return os.Rename(srcPath, dstPath)
}

// resolvePath converts a sandbox path to an absolute path.
func (f *nsjailFS) resolvePath(path string) string {
	// Remove leading /workspace if present
	path = strings.TrimPrefix(path, "/workspace")
	path = strings.TrimPrefix(path, "/")

	return filepath.Join(f.instance.workDir, path)
}

// Watch is not implemented for nsjail.
func (f *nsjailFS) Watch(ctx context.Context, path string, events chan<- *fs.WatchEvent) error {
	return fmt.Errorf("watch not implemented for nsjail provider")
}

var _ fs.FileSystem = (*nsjailFS)(nil)
