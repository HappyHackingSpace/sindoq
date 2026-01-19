// Package sindoq provides a unified SDK for code execution across multiple isolated environments.
package sindoq

import (
	"errors"
	"fmt"
)

// Sentinel errors for common conditions.
var (
	// ErrSandboxNotFound indicates sandbox doesn't exist.
	ErrSandboxNotFound = errors.New("sandbox not found")

	// ErrSandboxStopped indicates sandbox is not running.
	ErrSandboxStopped = errors.New("sandbox is stopped")

	// ErrExecutionTimeout indicates execution exceeded timeout.
	ErrExecutionTimeout = errors.New("execution timeout")

	// ErrProviderUnavailable indicates provider is not accessible.
	ErrProviderUnavailable = errors.New("provider unavailable")

	// ErrLanguageNotSupported indicates language isn't supported.
	ErrLanguageNotSupported = errors.New("language not supported")

	// ErrLanguageDetectionFailed indicates detection couldn't determine language.
	ErrLanguageDetectionFailed = errors.New("language detection failed")

	// ErrResourceExhausted indicates resource limits exceeded.
	ErrResourceExhausted = errors.New("resource exhausted")

	// ErrPermissionDenied indicates operation not permitted.
	ErrPermissionDenied = errors.New("permission denied")

	// ErrInvalidConfiguration indicates invalid configuration.
	ErrInvalidConfiguration = errors.New("invalid configuration")

	// ErrProviderNotRegistered indicates provider is not registered.
	ErrProviderNotRegistered = errors.New("provider not registered")
)

// SandboxError wraps errors with context.
type SandboxError struct {
	Op        string // Operation that failed
	Provider  string // Provider involved
	SandboxID string // Sandbox ID if known
	Err       error  // Underlying error
}

// Error implements the error interface.
func (e *SandboxError) Error() string {
	if e.SandboxID != "" {
		return fmt.Sprintf("%s [%s/%s]: %v", e.Op, e.Provider, e.SandboxID, e.Err)
	}
	if e.Provider != "" {
		return fmt.Sprintf("%s [%s]: %v", e.Op, e.Provider, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

// Unwrap returns the underlying error.
func (e *SandboxError) Unwrap() error {
	return e.Err
}

// Is supports errors.Is for comparison.
func (e *SandboxError) Is(target error) bool {
	return errors.Is(e.Err, target)
}

// NewError creates a SandboxError.
func NewError(op, provider, sandboxID string, err error) *SandboxError {
	return &SandboxError{
		Op:        op,
		Provider:  provider,
		SandboxID: sandboxID,
		Err:       err,
	}
}

// ExecutionError contains details about execution failures.
type ExecutionError struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

// Error implements the error interface.
func (e *ExecutionError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("execution failed (exit code %d): %v\nstderr: %s",
			e.ExitCode, e.Err, e.Stderr)
	}
	return fmt.Sprintf("execution failed (exit code %d): %v", e.ExitCode, e.Err)
}

// Unwrap returns the underlying error.
func (e *ExecutionError) Unwrap() error {
	return e.Err
}

// NewExecutionError creates an ExecutionError.
func NewExecutionError(exitCode int, stdout, stderr string, err error) *ExecutionError {
	return &ExecutionError{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderr,
		Err:      err,
	}
}
