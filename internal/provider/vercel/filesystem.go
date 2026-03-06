package vercel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/happyhackingspace/sindoq/pkg/fs"
)

// vercelFS implements fs.FileSystem for Vercel Sandbox.
type vercelFS struct {
	instance *Instance
}

// Read reads file contents.
func (v *vercelFS) Read(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/sandbox/"+v.instance.id+"/files?path="+path, nil)
	if err != nil {
		return nil, err
	}
	v.instance.provider.setHeaders(req)

	resp, err := v.instance.provider.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("read file failed: %s - %s", resp.Status, string(bodyBytes))
	}

	var result struct {
		Content string `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return []byte(result.Content), nil
}

// Write writes data to a file.
func (v *vercelFS) Write(ctx context.Context, path string, data []byte) error {
	return v.instance.writeFile(ctx, path, data)
}

// Delete removes a file or directory.
func (v *vercelFS) Delete(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", baseURL+"/v1/sandbox/"+v.instance.id+"/files?path="+path, nil)
	if err != nil {
		return err
	}
	v.instance.provider.setHeaders(req)

	resp, err := v.instance.provider.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete file failed: %s - %s", resp.Status, string(bodyBytes))
	}

	return nil
}

// List lists files in a directory.
func (v *vercelFS) List(ctx context.Context, path string) ([]fs.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/sandbox/"+v.instance.id+"/files?path="+path+"&list=true", nil)
	if err != nil {
		return nil, err
	}
	v.instance.provider.setHeaders(req)

	resp, err := v.instance.provider.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list files failed: %s - %s", resp.Status, string(bodyBytes))
	}

	var result struct {
		Files []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			Size  int64  `json:"size"`
			IsDir bool   `json:"isDir"`
		} `json:"files"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	files := make([]fs.FileInfo, len(result.Files))
	for i, f := range result.Files {
		files[i] = fs.FileInfo{
			Name:  f.Name,
			Path:  f.Path,
			Size:  f.Size,
			IsDir: f.IsDir,
		}
	}

	return files, nil
}

// Exists checks if a path exists.
func (v *vercelFS) Exists(ctx context.Context, path string) (bool, error) {
	_, err := v.Stat(ctx, path)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// Stat returns file information.
func (v *vercelFS) Stat(ctx context.Context, path string) (*fs.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/sandbox/"+v.instance.id+"/files?path="+path+"&stat=true", nil)
	if err != nil {
		return nil, err
	}
	v.instance.provider.setHeaders(req)

	resp, err := v.instance.provider.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stat failed: %s", resp.Status)
	}

	var result struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Size  int64  `json:"size"`
		IsDir bool   `json:"isDir"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &fs.FileInfo{
		Name:  result.Name,
		Path:  result.Path,
		Size:  result.Size,
		IsDir: result.IsDir,
	}, nil
}

// Upload uploads content from a reader to the sandbox.
func (v *vercelFS) Upload(ctx context.Context, localPath, remotePath string) error {
	// Read local file and write to remote
	// For simplicity, use RunCommand to copy
	result, err := v.instance.RunCommand(ctx, "cat", []string{localPath})
	if err != nil {
		return err
	}
	return v.Write(ctx, remotePath, []byte(result.Stdout))
}

// UploadReader uploads content from a reader to the sandbox.
func (v *vercelFS) UploadReader(ctx context.Context, reader io.Reader, remotePath string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return v.Write(ctx, remotePath, data)
}

// Download downloads a file from the sandbox.
func (v *vercelFS) Download(ctx context.Context, remotePath string, writer io.Writer) error {
	data, err := v.Read(ctx, remotePath)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

// MkDir creates a directory.
func (v *vercelFS) MkDir(ctx context.Context, path string) error {
	result, err := v.instance.RunCommand(ctx, "mkdir", []string{"-p", path})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("mkdir failed: %s", result.Stderr)
	}
	return nil
}

// Copy copies a file within the sandbox.
func (v *vercelFS) Copy(ctx context.Context, src, dst string) error {
	// Read source and write to destination
	data, err := v.Read(ctx, src)
	if err != nil {
		return err
	}
	return v.Write(ctx, dst, data)
}

// Move moves/renames a file within the sandbox.
func (v *vercelFS) Move(ctx context.Context, src, dst string) error {
	reqBody := map[string]any{
		"src": src,
		"dst": dst,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/sandbox/"+v.instance.id+"/files/move", bytes.NewReader(body))
	if err != nil {
		return err
	}
	v.instance.provider.setHeaders(req)

	resp, err := v.instance.provider.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Fallback to copy + delete
		if err := v.Copy(ctx, src, dst); err != nil {
			return err
		}
		return v.Delete(ctx, src)
	}

	return nil
}

// Ensure vercelFS implements fs.FileSystem
var _ fs.FileSystem = (*vercelFS)(nil)
