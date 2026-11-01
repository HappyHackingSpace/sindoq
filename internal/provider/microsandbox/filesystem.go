//go:build microsandbox && cgo

package microsandbox

import (
	"context"
	"errors"
	"io"
	"path"

	msb "github.com/superradcompany/microsandbox/sdk/go"

	"github.com/happyhackingspace/sindoq/pkg/fs"
)

// microsandboxFS implements fs.FileSystem for microsandbox sandboxes.
// It wraps the SDK filesystem API, which talks to the guest agent.
type microsandboxFS struct {
	instance *Instance
}

func (f *microsandboxFS) ops() *msb.SandboxFSOps {
	return f.instance.sb.FS()
}

// Read reads file contents.
func (f *microsandboxFS) Read(ctx context.Context, p string) ([]byte, error) {
	return f.ops().Read(ctx, p)
}

// Write writes data to a file, creating parent directories.
func (f *microsandboxFS) Write(ctx context.Context, p string, data []byte) error {
	return f.instance.writeFile(ctx, p, data)
}

// Delete removes a file or directory.
func (f *microsandboxFS) Delete(ctx context.Context, p string) error {
	st, err := f.ops().Stat(ctx, p)
	if err != nil {
		return err
	}
	if st.IsDir {
		return f.ops().RemoveDir(ctx, p)
	}
	return f.ops().Remove(ctx, p)
}

// List lists directory contents.
func (f *microsandboxFS) List(ctx context.Context, p string) ([]fs.FileInfo, error) {
	entries, err := f.ops().List(ctx, p)
	if err != nil {
		return nil, err
	}

	infos := make([]fs.FileInfo, 0, len(entries))
	for _, e := range entries {
		infos = append(infos, fs.FileInfo{
			Name:  path.Base(e.Path),
			Path:  e.Path,
			Size:  e.Size,
			IsDir: e.Kind == msb.FsEntryKindDirectory,
			Mode:  e.Mode,
		})
	}
	return infos, nil
}

// Exists checks if a path exists.
func (f *microsandboxFS) Exists(ctx context.Context, p string) (bool, error) {
	return f.ops().Exists(ctx, p)
}

// Stat returns file info.
func (f *microsandboxFS) Stat(ctx context.Context, p string) (*fs.FileInfo, error) {
	st, err := f.ops().Stat(ctx, p)
	if err != nil {
		return nil, err
	}
	return &fs.FileInfo{
		Name:    path.Base(p),
		Path:    p,
		Size:    st.Size,
		IsDir:   st.IsDir,
		ModTime: st.ModTime,
		Mode:    st.Mode,
	}, nil
}

// Upload copies a local file into the sandbox.
func (f *microsandboxFS) Upload(ctx context.Context, localPath, remotePath string) error {
	if err := f.ops().Mkdir(ctx, path.Dir(remotePath)); err != nil {
		return err
	}
	return f.ops().CopyFromHost(ctx, localPath, remotePath)
}

// UploadReader uploads from a reader.
func (f *microsandboxFS) UploadReader(ctx context.Context, reader io.Reader, remotePath string) error {
	if err := f.ops().Mkdir(ctx, path.Dir(remotePath)); err != nil {
		return err
	}
	w, err := f.ops().WriteStream(ctx, remotePath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(w, reader)
	return errors.Join(copyErr, w.Close(ctx))
}

// Download writes a sandbox file to writer.
func (f *microsandboxFS) Download(ctx context.Context, remotePath string, writer io.Writer) error {
	r, err := f.ops().ReadStream(ctx, remotePath)
	if err != nil {
		return err
	}
	_, copyErr := r.CopyTo(ctx, writer)
	return errors.Join(copyErr, r.Close())
}

// MkDir creates a directory and any missing parents.
func (f *microsandboxFS) MkDir(ctx context.Context, p string) error {
	return f.ops().Mkdir(ctx, p)
}

// Copy copies a file.
func (f *microsandboxFS) Copy(ctx context.Context, src, dst string) error {
	return f.ops().Copy(ctx, src, dst)
}

// Move moves or renames a file or directory.
func (f *microsandboxFS) Move(ctx context.Context, src, dst string) error {
	return f.ops().Rename(ctx, src, dst)
}

var _ fs.FileSystem = (*microsandboxFS)(nil)
