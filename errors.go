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
