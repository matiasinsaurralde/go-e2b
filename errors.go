package e2b

import "fmt"

// Error represents an error returned by the E2B API or SDK.
type Error struct {
	// StatusCode is the HTTP status code, if applicable.
	StatusCode int

	// Message describes what went wrong.
	Message string
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("e2b: status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("e2b: %s", e.Message)
}

// SandboxNotFoundError is returned when a sandbox cannot be found.
type SandboxNotFoundError struct {
	SandboxID string
}

// Error implements the error interface.
func (e *SandboxNotFoundError) Error() string {
	return fmt.Sprintf("e2b: sandbox not found: %s", e.SandboxID)
}

// TimeoutError is returned when an operation exceeds its deadline.
type TimeoutError struct {
	Message string
}

// Error implements the error interface.
func (e *TimeoutError) Error() string {
	return fmt.Sprintf("e2b: timeout: %s", e.Message)
}

// FileNotFoundError is returned when the requested path does not exist in the sandbox.
type FileNotFoundError struct {
	Path string
}

// Error implements the error interface.
func (e *FileNotFoundError) Error() string {
	return fmt.Sprintf("e2b: file not found: %s", e.Path)
}
