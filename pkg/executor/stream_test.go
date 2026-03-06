package executor

import (
	"io"
	"sync"
	"testing"
	"time"
)

func TestStreamEventTypes(t *testing.T) {
	tests := []struct {
		eventType StreamEventType
		expected  string
	}{
		{StreamStdout, "stdout"},
		{StreamStderr, "stderr"},
		{StreamStart, "start"},
		{StreamComplete, "complete"},
		{StreamError, "error"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("StreamEventType = %q, want %q", tt.eventType, tt.expected)
			}
		})
	}
}

func TestStreamEvent(t *testing.T) {
	now := time.Now()
	event := StreamEvent{
		Type:      StreamStdout,
		Data:      "Hello, World!",
		Timestamp: now,
		ExitCode:  0,
	}

	if event.Type != StreamStdout {
		t.Errorf("Type = %v, want %v", event.Type, StreamStdout)
	}
	if event.Data != "Hello, World!" {
		t.Errorf("Data = %q, want %q", event.Data, "Hello, World!")
	}
	if event.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", event.Timestamp, now)
	}
	if event.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", event.ExitCode)
	}
}

func TestNewOutputStream(t *testing.T) {
	stream := NewOutputStream(10, StreamStdout)

	if stream == nil {
		t.Fatal("NewOutputStream() returned nil")
	}
	if stream.events == nil {
		t.Error("events channel should be initialized")
	}
	if stream.eventType != StreamStdout {
		t.Errorf("eventType = %v, want %v", stream.eventType, StreamStdout)
	}
	if stream.closed {
		t.Error("stream should not be closed initially")
	}
}

func TestOutputStreamWrite(t *testing.T) {
	stream := NewOutputStream(10, StreamStdout)
	defer func() { _ = stream.Close() }()

	data := []byte("Hello, World!")
	n, err := stream.Write(data)

	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() = %d, want %d", n, len(data))
	}

	// Check event was sent
	select {
	case event := <-stream.Events():
		if event.Type != StreamStdout {
			t.Errorf("event.Type = %v, want %v", event.Type, StreamStdout)
		}
		if event.Data != "Hello, World!" {
			t.Errorf("event.Data = %q, want %q", event.Data, "Hello, World!")
		}
	case <-time.After(time.Second):
		t.Error("expected event was not received")
	}
}

func TestOutputStreamWriteAfterClose(t *testing.T) {
	stream := NewOutputStream(10, StreamStdout)
	_ = stream.Close()

	_, err := stream.Write([]byte("test"))
	if err != io.ErrClosedPipe {
		t.Errorf("Write() after close = %v, want %v", err, io.ErrClosedPipe)
	}
}

func TestOutputStreamWriteEvent(t *testing.T) {
	stream := NewOutputStream(10, StreamStdout)
	defer func() { _ = stream.Close() }()

	event := &StreamEvent{
		Type:      StreamStderr,
		Data:      "Error message",
		Timestamp: time.Now(),
	}

	err := stream.WriteEvent(event)
	if err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	select {
	case received := <-stream.Events():
		if received != event {
			t.Error("received event should be same as sent")
		}
	case <-time.After(time.Second):
		t.Error("expected event was not received")
	}
}

func TestOutputStreamWriteEventAfterClose(t *testing.T) {
	stream := NewOutputStream(10, StreamStdout)
	_ = stream.Close()

	err := stream.WriteEvent(&StreamEvent{Type: StreamStdout, Data: "test"})
	if err != io.ErrClosedPipe {
		t.Errorf("WriteEvent() after close = %v, want %v", err, io.ErrClosedPipe)
	}
}

func TestOutputStreamOnEvent(t *testing.T) {
	stream := NewOutputStream(10, StreamStdout)
	defer func() { _ = stream.Close() }()

	var receivedEvents []*StreamEvent
	var mu sync.Mutex

	stream.OnEvent(func(e *StreamEvent) error {
		mu.Lock()
		receivedEvents = append(receivedEvents, e)
		mu.Unlock()
		return nil
	})

	_, _ = stream.Write([]byte("line 1"))
	_, _ = stream.Write([]byte("line 2"))

	// Wait a bit for handlers to process
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(receivedEvents) != 2 {
		t.Errorf("received %d events, want 2", len(receivedEvents))
	}
}

func TestOutputStreamClose(t *testing.T) {
	stream := NewOutputStream(10, StreamStdout)

	err := stream.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !stream.closed {
		t.Error("stream should be closed")
	}

	// Second close should be safe
	err = stream.Close()
	if err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestOutputStreamEvents(t *testing.T) {
	stream := NewOutputStream(10, StreamStdout)

	events := stream.Events()
	if events == nil {
		t.Error("Events() should not return nil")
	}
}

func TestNewMultiStreamWriter(t *testing.T) {
	msw := NewMultiStreamWriter(10)

	if msw == nil {
		t.Fatal("NewMultiStreamWriter() returned nil")
	}
	if msw.stdout == nil {
		t.Error("stdout stream should be initialized")
	}
	if msw.stderr == nil {
		t.Error("stderr stream should be initialized")
	}
	if msw.events == nil {
		t.Error("events channel should be initialized")
	}
}

func TestMultiStreamWriterStdout(t *testing.T) {
	msw := NewMultiStreamWriter(10)
	defer func() { _ = msw.Close() }()

	stdout := msw.Stdout()
	if stdout == nil {
		t.Fatal("Stdout() returned nil")
	}

	n, err := stdout.Write([]byte("stdout data"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 11 {
		t.Errorf("Write() = %d, want 11", n)
	}
}

func TestMultiStreamWriterStderr(t *testing.T) {
	msw := NewMultiStreamWriter(10)
	defer func() { _ = msw.Close() }()

	stderr := msw.Stderr()
	if stderr == nil {
		t.Fatal("Stderr() returned nil")
	}

	n, err := stderr.Write([]byte("stderr data"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 11 {
		t.Errorf("Write() = %d, want 11", n)
	}
}

func TestMultiStreamWriterEvents(t *testing.T) {
	msw := NewMultiStreamWriter(10)
	defer func() { _ = msw.Close() }()

	events := msw.Events()
	if events == nil {
		t.Error("Events() should not return nil")
	}

	// Write to both streams
	_, _ = msw.Stdout().Write([]byte("out"))
	_, _ = msw.Stderr().Write([]byte("err"))

	// Give time for forwarding
	time.Sleep(100 * time.Millisecond)

	// Should receive events from combined channel
	received := 0
	timeout := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case <-events:
			received++
			if received >= 2 {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if received < 2 {
		t.Errorf("received %d events, want at least 2", received)
	}
}

func TestMultiStreamWriterOnEvent(t *testing.T) {
	msw := NewMultiStreamWriter(10)
	defer func() { _ = msw.Close() }()

	var receivedEvents []*StreamEvent
	var mu sync.Mutex

	msw.OnEvent(func(e *StreamEvent) error {
		mu.Lock()
		receivedEvents = append(receivedEvents, e)
		mu.Unlock()
		return nil
	})

	_, _ = msw.Stdout().Write([]byte("stdout"))
	_, _ = msw.Stderr().Write([]byte("stderr"))

	// Wait for handlers
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(receivedEvents) != 2 {
		t.Errorf("received %d events, want 2", len(receivedEvents))
	}

	// Check we got both types
	hasStdout := false
	hasStderr := false
	for _, e := range receivedEvents {
		if e.Type == StreamStdout {
			hasStdout = true
		}
		if e.Type == StreamStderr {
			hasStderr = true
		}
	}
	if !hasStdout {
		t.Error("should receive stdout event")
	}
	if !hasStderr {
		t.Error("should receive stderr event")
	}
}

func TestMultiStreamWriterClose(t *testing.T) {
	msw := NewMultiStreamWriter(10)

	err := msw.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !msw.closed {
		t.Error("should be closed")
	}

	// Second close should be safe
	err = msw.Close()
	if err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestOutputStreamConcurrent(t *testing.T) {
	stream := NewOutputStream(100, StreamStdout)
	defer func() { _ = stream.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _ = stream.Write([]byte("data"))
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent writes timed out")
	}
}
