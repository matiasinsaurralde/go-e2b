package e2b

import (
	"strings"
	"testing"
)

func TestErrorWithStatusCode(t *testing.T) {
	e := &Error{StatusCode: 500, Message: "internal error"}
	got := e.Error()
	if !strings.Contains(got, "500") || !strings.Contains(got, "internal error") {
		t.Errorf("Error() = %q, want status code and message", got)
	}
}

func TestErrorWithoutStatusCode(t *testing.T) {
	e := &Error{Message: "something failed"}
	got := e.Error()
	if strings.Contains(got, "status") {
		t.Errorf("Error() = %q, should not contain 'status' when code is 0", got)
	}
	if !strings.Contains(got, "something failed") {
		t.Errorf("Error() = %q, want message", got)
	}
}

func TestSandboxNotFoundError(t *testing.T) {
	e := &SandboxNotFoundError{SandboxID: "abc123"}
	got := e.Error()
	if !strings.Contains(got, "abc123") {
		t.Errorf("Error() = %q, want sandbox ID", got)
	}
}

func TestTimeoutError(t *testing.T) {
	e := &TimeoutError{Message: "deadline exceeded"}
	got := e.Error()
	if !strings.Contains(got, "deadline exceeded") {
		t.Errorf("Error() = %q, want message", got)
	}
}
