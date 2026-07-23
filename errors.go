package e2b

import (
	"fmt"
	"net/http"
)

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

// TemplateNotFoundError is returned when a template cannot be found.
type TemplateNotFoundError struct {
	TemplateID string
}

// Error implements the error interface.
func (e *TemplateNotFoundError) Error() string {
	return fmt.Sprintf("e2b: template not found: %s", e.TemplateID)
}

// TemplateBuildError is returned when a template build finishes with "error" status.
type TemplateBuildError struct {
	TemplateID string
	BuildID    string
	Reason     BuildStatusReason
}

// Error implements the error interface.
func (e *TemplateBuildError) Error() string {
	if e.Reason.Step != "" {
		return fmt.Sprintf("e2b: template build failed: %s (step: %s)", e.Reason.Message, e.Reason.Step)
	}
	return fmt.Sprintf("e2b: template build failed: %s", e.Reason.Message)
}

// InvalidArgumentError is returned when the sandbox rejects a request because
// an argument was invalid (e.g. an unsupported option for the running envd
// version). It maps from the Connect/gRPC InvalidArgument code.
type InvalidArgumentError struct {
	Message string
}

// Error implements the error interface.
func (e *InvalidArgumentError) Error() string {
	return fmt.Sprintf("e2b: invalid argument: %s", e.Message)
}

// AuthenticationError is returned when the sandbox rejects a request because
// authentication failed (e.g. a missing or invalid access token). It maps from
// the Connect/gRPC Unauthenticated code.
type AuthenticationError struct {
	Message string
}

// Error implements the error interface.
func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("e2b: authentication failed: %s", e.Message)
}

// RateLimitError is returned when the API rejects a request because the rate
// limit was exceeded (HTTP 429).
type RateLimitError struct {
	Message string
}

// Error implements the error interface.
func (e *RateLimitError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("e2b: rate limit exceeded: %s", e.Message)
	}
	return "e2b: rate limit exceeded"
}

// apiErrorFromCode maps an API status code and message to a typed error. It is
// used both for whole-response failures and for error objects embedded in
// response bodies (for example, per-fork results). The message is prefixed with
// the code to mirror the reference SDKs' embedded-error formatting.
func apiErrorFromCode(code int, message string) error {
	text := fmt.Sprintf("%d: %s", code, message)
	switch code {
	case http.StatusUnauthorized:
		return &AuthenticationError{Message: text}
	case http.StatusTooManyRequests:
		return &RateLimitError{Message: text}
	default:
		return &Error{StatusCode: code, Message: message}
	}
}

// VolumeError is returned when a volume management or content operation fails
// with a non-2xx status that is not otherwise mapped to a more specific error
// (e.g. a not-found). It mirrors the reference SDKs' VolumeError/VolumeException.
type VolumeError struct {
	// StatusCode is the HTTP status code returned by the volume API.
	StatusCode int

	// Message describes what went wrong, as reported by the API when available.
	Message string
}

// Error implements the error interface.
func (e *VolumeError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("e2b: volume error: status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("e2b: volume error: status %d", e.StatusCode)
}

// VolumeNotFoundError is returned when a volume ID does not exist. Content-level
// (path) 404s use FileNotFoundError instead, matching the FilesystemService
// error taxonomy.
type VolumeNotFoundError struct {
	VolumeID string
}

// Error implements the error interface.
func (e *VolumeNotFoundError) Error() string {
	return fmt.Sprintf("e2b: volume not found: %s", e.VolumeID)
}

// CommandExitError is returned by CommandHandle.Wait when a command finishes
// with a non-zero exit code. It embeds the CommandResult so callers can still
// inspect the captured stdout, stderr, and exit code.
type CommandExitError struct {
	// Stdout is the accumulated standard output of the command.
	Stdout string

	// Stderr is the accumulated standard error output of the command.
	Stderr string

	// ExitCode is the non-zero process exit code.
	ExitCode int

	// Message is the error message reported by the sandbox, if any.
	Message string
}

// Error implements the error interface.
func (e *CommandExitError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("e2b: command exited with code %d: %s", e.ExitCode, e.Message)
	}
	return fmt.Sprintf("e2b: command exited with code %d", e.ExitCode)
}
