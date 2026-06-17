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

func TestTemplateBuildErrorWithStep(t *testing.T) {
	e := &TemplateBuildError{
		TemplateID: "tmpl-abc",
		BuildID:    "build-123",
		Reason: BuildStatusReason{
			Message: "command exited with code 1",
			Step:    "run",
		},
	}
	got := e.Error()
	want := "e2b: template build failed: command exited with code 1 (step: run)"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestTemplateBuildErrorWithoutStep(t *testing.T) {
	e := &TemplateBuildError{
		TemplateID: "tmpl-abc",
		BuildID:    "build-123",
		Reason: BuildStatusReason{
			Message: "internal server error",
		},
	}
	got := e.Error()
	want := "e2b: template build failed: internal server error"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestTemplateBuildErrorFields(t *testing.T) {
	e := &TemplateBuildError{
		TemplateID: "tmpl-xyz",
		BuildID:    "build-456",
		Reason: BuildStatusReason{
			Message: "image not found",
			Step:    "pull",
			LogEntries: []BuildLogEntry{
				{Timestamp: "2026-06-17T10:00:00Z", Message: "pulling image", Level: "error"},
			},
		},
	}
	if e.TemplateID != "tmpl-xyz" {
		t.Errorf("TemplateID = %q, want %q", e.TemplateID, "tmpl-xyz")
	}
	if e.BuildID != "build-456" {
		t.Errorf("BuildID = %q, want %q", e.BuildID, "build-456")
	}
	if len(e.Reason.LogEntries) != 1 {
		t.Errorf("len(Reason.LogEntries) = %d, want 1", len(e.Reason.LogEntries))
	}
}
