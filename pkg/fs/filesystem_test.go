package fs

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestFileInfo(t *testing.T) {
	now := time.Now()
	info := FileInfo{
		Name:     "test.txt",
		Path:     "/workspace/test.txt",
		Size:     1024,
		IsDir:    false,
		ModTime:  now,
		Mode:     0644,
		MIMEType: "text/plain",
	}

	if info.Name != "test.txt" {
		t.Errorf("Name = %q, want %q", info.Name, "test.txt")
	}
	if info.Path != "/workspace/test.txt" {
		t.Errorf("Path = %q, want %q", info.Path, "/workspace/test.txt")
	}
	if info.Size != 1024 {
		t.Errorf("Size = %d, want 1024", info.Size)
	}
	if info.IsDir {
		t.Error("IsDir should be false")
	}
	if info.ModTime != now {
		t.Errorf("ModTime = %v, want %v", info.ModTime, now)
	}
	if info.Mode != 0644 {
		t.Errorf("Mode = %o, want %o", info.Mode, 0644)
	}
	if info.MIMEType != "text/plain" {
		t.Errorf("MIMEType = %q, want %q", info.MIMEType, "text/plain")
	}
}

func TestFileInfoDirectory(t *testing.T) {
	info := FileInfo{
		Name:  "mydir",
		Path:  "/workspace/mydir",
		IsDir: true,
		Mode:  0755,
	}

	if !info.IsDir {
		t.Error("IsDir should be true for directory")
	}
}

func TestWatchEventTypes(t *testing.T) {
	tests := []struct {
		eventType WatchEventType
		expected  string
	}{
		{WatchCreate, "create"},
		{WatchModify, "modify"},
		{WatchDelete, "delete"},
		{WatchRename, "rename"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("WatchEventType = %q, want %q", tt.eventType, tt.expected)
			}
		})
	}
}

func TestWatchEvent(t *testing.T) {
	now := time.Now()
	event := WatchEvent{
		Type:      WatchCreate,
		Path:      "/workspace/newfile.txt",
		Timestamp: now,
	}

	if event.Type != WatchCreate {
		t.Errorf("Type = %v, want %v", event.Type, WatchCreate)
	}
	if event.Path != "/workspace/newfile.txt" {
		t.Errorf("Path = %q, want %q", event.Path, "/workspace/newfile.txt")
	}
	if event.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", event.Timestamp, now)
	}
}

func TestWatchEventRename(t *testing.T) {
	event := WatchEvent{
		Type:    WatchRename,
		Path:    "/workspace/newname.txt",
		OldPath: "/workspace/oldname.txt",
	}

	if event.Type != WatchRename {
		t.Errorf("Type = %v, want %v", event.Type, WatchRename)
	}
	if event.OldPath != "/workspace/oldname.txt" {
		t.Errorf("OldPath = %q, want %q", event.OldPath, "/workspace/oldname.txt")
	}
}

// mockFileSystem implements FileSystem for interface compliance testing
type mockFileSystem struct{}

func (m *mockFileSystem) Read(ctx context.Context, path string) ([]byte, error) {
	return nil, nil
}
func (m *mockFileSystem) Write(ctx context.Context, path string, data []byte) error {
	return nil
}
func (m *mockFileSystem) Delete(ctx context.Context, path string) error {
	return nil
}
func (m *mockFileSystem) List(ctx context.Context, path string) ([]FileInfo, error) {
	return nil, nil
}
func (m *mockFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	return false, nil
}
func (m *mockFileSystem) Stat(ctx context.Context, path string) (*FileInfo, error) {
	return nil, nil
}
func (m *mockFileSystem) Upload(ctx context.Context, localPath, remotePath string) error {
	return nil
}
func (m *mockFileSystem) UploadReader(ctx context.Context, reader io.Reader, remotePath string) error {
	return nil
}
func (m *mockFileSystem) Download(ctx context.Context, remotePath string, writer io.Writer) error {
	return nil
}
func (m *mockFileSystem) MkDir(ctx context.Context, path string) error {
	return nil
}
func (m *mockFileSystem) Copy(ctx context.Context, src, dst string) error {
	return nil
}
func (m *mockFileSystem) Move(ctx context.Context, src, dst string) error {
	return nil
}

// Verify interface compliance at compile time
var _ FileSystem = (*mockFileSystem)(nil)

func TestFileSystemInterface(t *testing.T) {
	var fs FileSystem = &mockFileSystem{}
	_ = fs // interface compliance verified by compile-time check above
}

// mockWatcher implements Watcher interface
type mockWatcher struct{}

func (m *mockWatcher) Watch(ctx context.Context, path string) (<-chan *WatchEvent, func(), error) {
	ch := make(chan *WatchEvent)
	return ch, func() { close(ch) }, nil
}

var _ Watcher = (*mockWatcher)(nil)

func TestWatcherInterface(t *testing.T) {
	var w Watcher = &mockWatcher{}
	_ = w // interface compliance verified by compile-time check above
}

// mockWatchableFileSystem implements WatchableFileSystem
type mockWatchableFileSystem struct {
	mockFileSystem
	mockWatcher
}

var _ WatchableFileSystem = (*mockWatchableFileSystem)(nil)

func TestWatchableFileSystemInterface(t *testing.T) {
	var wfs WatchableFileSystem = &mockWatchableFileSystem{}
	_ = wfs // interface compliance verified by compile-time check above
}
