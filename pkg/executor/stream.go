package executor

import (
	"io"
	"sync"
	"time"
)

// StreamEventType categorizes stream events.
type StreamEventType string

const (
	// StreamStdout indicates standard output data.
	StreamStdout StreamEventType = "stdout"

	// StreamStderr indicates standard error data.
	StreamStderr StreamEventType = "stderr"

	// StreamStart indicates execution started.
	StreamStart StreamEventType = "start"

	// StreamComplete indicates execution completed.
	StreamComplete StreamEventType = "complete"

	// StreamError indicates an error occurred.
	StreamError StreamEventType = "error"
)

// StreamEvent represents an output event during streaming execution.
type StreamEvent struct {
	// Type indicates the event type.
	Type StreamEventType

	// Data contains the event payload.
	Data string

	// Timestamp when the event occurred.
	Timestamp time.Time

	// ExitCode is set when Type is StreamComplete.
	ExitCode int

	// Error is set when Type is StreamError.
	Error error
}

// StreamHandler processes streaming events.
type StreamHandler func(event *StreamEvent) error

// StreamWriter wraps streaming with io.Writer interface.
type StreamWriter interface {
	io.Writer
	io.Closer
	Events() <-chan *StreamEvent
	WriteEvent(event *StreamEvent) error
}

// OutputStream handles real-time output streaming.
type OutputStream struct {
	mu        sync.RWMutex
	events    chan *StreamEvent
	handlers  []StreamHandler
	closed    bool
	eventType StreamEventType
}

// NewOutputStream creates a new output stream.
func NewOutputStream(bufferSize int, eventType StreamEventType) *OutputStream {
	return &OutputStream{
		events:    make(chan *StreamEvent, bufferSize),
		eventType: eventType,
	}
}

// Write implements io.Writer for stdout/stderr streaming.
func (s *OutputStream) Write(p []byte) (n int, err error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return 0, io.ErrClosedPipe
	}
	s.mu.RUnlock()

	event := &StreamEvent{
		Type:      s.eventType,
		Data:      string(p),
		Timestamp: time.Now(),
	}

	if err := s.WriteEvent(event); err != nil {
		return 0, err
	}

	return len(p), nil
}

// WriteEvent sends an event to the stream.
func (s *OutputStream) WriteEvent(event *StreamEvent) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return io.ErrClosedPipe
	}
	handlers := s.handlers
	s.mu.RUnlock()

	// Send to channel (non-blocking)
	select {
	case s.events <- event:
	default:
		// Buffer full, skip (or handle backpressure)
	}

	// Call handlers synchronously
	for _, handler := range handlers {
		if err := handler(event); err != nil {
			return err
		}
	}

	return nil
}

// Events returns the event channel for consumers.
func (s *OutputStream) Events() <-chan *StreamEvent {
	return s.events
}

// OnEvent registers a handler for stream events.
func (s *OutputStream) OnEvent(handler StreamHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, handler)
}

// Close closes the stream.
func (s *OutputStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		s.closed = true
		close(s.events)
	}
	return nil
}

// MultiStreamWriter combines stdout and stderr streams.
type MultiStreamWriter struct {
	stdout *OutputStream
	stderr *OutputStream
	events chan *StreamEvent
	closed bool
	mu     sync.RWMutex
}

// NewMultiStreamWriter creates a writer that handles both stdout and stderr.
func NewMultiStreamWriter(bufferSize int) *MultiStreamWriter {
	m := &MultiStreamWriter{
		stdout: NewOutputStream(bufferSize, StreamStdout),
		stderr: NewOutputStream(bufferSize, StreamStderr),
		events: make(chan *StreamEvent, bufferSize*2),
	}

	// Forward events from both streams to combined channel
	go m.forward(m.stdout.Events())
	go m.forward(m.stderr.Events())

	return m
}

func (m *MultiStreamWriter) forward(src <-chan *StreamEvent) {
	for event := range src {
		m.mu.RLock()
		if !m.closed {
			select {
			case m.events <- event:
			default:
			}
		}
		m.mu.RUnlock()
	}
}

// Stdout returns the stdout writer.
func (m *MultiStreamWriter) Stdout() io.Writer {
	return m.stdout
}

// Stderr returns the stderr writer.
func (m *MultiStreamWriter) Stderr() io.Writer {
	return m.stderr
}

// Events returns the combined event channel.
func (m *MultiStreamWriter) Events() <-chan *StreamEvent {
	return m.events
}

// OnEvent registers a handler for all stream events.
func (m *MultiStreamWriter) OnEvent(handler StreamHandler) {
	m.stdout.OnEvent(handler)
	m.stderr.OnEvent(handler)
}

// Close closes both streams.
func (m *MultiStreamWriter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		m.closed = true
		_ = m.stdout.Close()
		_ = m.stderr.Close()
		close(m.events)
	}
	return nil
}
