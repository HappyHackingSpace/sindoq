package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/happyhackingspace/sindoq/pkg/fs"
)

// dockerFS implements fs.FileSystem for Docker containers.
type dockerFS struct {
	instance *Instance
}

// Read reads file contents.
func (d *dockerFS) Read(ctx context.Context, path string) ([]byte, error) {
	reader, _, err := d.instance.client.CopyFromContainer(ctx, d.instance.id, path)
	if err != nil {
		return nil, fmt.Errorf("copy from container: %w", err)
	}
	defer func() { _ = reader.Close() }()

	// Extract from tar
	tr := tar.NewReader(reader)
	_, err = tr.Next()
	if err != nil {
		return nil, fmt.Errorf("read tar header: %w", err)
	}

	return io.ReadAll(tr)
}

// Write writes data to a file.
func (d *dockerFS) Write(ctx context.Context, path string, data []byte) error {
	var buf bytes.Buffer
	tw := newTarWriter(&buf)
	if err := tw.WriteFile(path, data); err != nil {
		return err
	}
	_ = tw.Close()

	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		dir = "/"
	}

	return d.instance.client.CopyToContainer(ctx, d.instance.id, dir, &buf, container.CopyToContainerOptions{})
}

// Delete removes a file or directory.
func (d *dockerFS) Delete(ctx context.Context, path string) error {
	result, err := d.instance.RunCommand(ctx, "rm", []string{"-rf", path})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("delete failed: %s", result.Stderr)
	}
	return nil
}

// List lists files in a directory.
func (d *dockerFS) List(ctx context.Context, path string) ([]fs.FileInfo, error) {
	result, err := d.instance.RunCommand(ctx, "ls", []string{"-la", path})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("list failed: %s", result.Stderr)
	}

	// Parse ls output
	lines := strings.Split(result.Stdout, "\n")
	files := make([]fs.FileInfo, 0)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Skip total line
		if strings.HasPrefix(line, "total") {
			continue
		}

		// Parse ls -la output
		parts := strings.Fields(line)
		if len(parts) < 9 {
			continue
		}

		name := strings.Join(parts[8:], " ")
		if name == "." || name == ".." {
			continue
		}

		isDir := strings.HasPrefix(parts[0], "d")

		files = append(files, fs.FileInfo{
			Name:  name,
			Path:  filepath.Join(path, name),
			IsDir: isDir,
		})
	}

	return files, nil
}

// Exists checks if a path exists.
func (d *dockerFS) Exists(ctx context.Context, path string) (bool, error) {
	result, err := d.instance.RunCommand(ctx, "test", []string{"-e", path})
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0, nil
}

// Stat returns file information.
func (d *dockerFS) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	result, err := d.instance.RunCommand(ctx, "stat", []string{"-c", "%n|%s|%F|%Y", path})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("stat failed: %s", result.Stderr)
	}

	output := strings.TrimSpace(result.Stdout)
	parts := strings.Split(output, "|")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid stat output")
	}

	var size int64
	_, _ = fmt.Sscanf(parts[1], "%d", &size)

	var modTime int64
	_, _ = fmt.Sscanf(parts[3], "%d", &modTime)

	return &fs.FileInfo{
		Name:    filepath.Base(parts[0]),
		Path:    parts[0],
		Size:    size,
		IsDir:   parts[2] == "directory",
		ModTime: time.Unix(modTime, 0),
	}, nil
}

// Upload uploads a local file to the sandbox.
func (d *dockerFS) Upload(ctx context.Context, localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}

	return d.Write(ctx, remotePath, data)
}

// UploadReader uploads content from a reader to the sandbox.
func (d *dockerFS) UploadReader(ctx context.Context, reader io.Reader, remotePath string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read content: %w", err)
	}

	return d.Write(ctx, remotePath, data)
}

// Download downloads a file from the sandbox.
func (d *dockerFS) Download(ctx context.Context, remotePath string, writer io.Writer) error {
	data, err := d.Read(ctx, remotePath)
	if err != nil {
		return err
	}

	_, err = writer.Write(data)
	return err
}

// MkDir creates a directory.
func (d *dockerFS) MkDir(ctx context.Context, path string) error {
	result, err := d.instance.RunCommand(ctx, "mkdir", []string{"-p", path})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("mkdir failed: %s", result.Stderr)
	}
	return nil
}

// Copy copies a file within the sandbox.
func (d *dockerFS) Copy(ctx context.Context, src, dst string) error {
	result, err := d.instance.RunCommand(ctx, "cp", []string{"-r", src, dst})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("copy failed: %s", result.Stderr)
	}
	return nil
}

// Move moves/renames a file within the sandbox.
func (d *dockerFS) Move(ctx context.Context, src, dst string) error {
	result, err := d.instance.RunCommand(ctx, "mv", []string{src, dst})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("move failed: %s", result.Stderr)
	}
	return nil
}

// Ensure dockerFS implements fs.FileSystem
var _ fs.FileSystem = (*dockerFS)(nil)
