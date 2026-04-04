package e2b

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewSandboxSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sandboxes" {
			t.Errorf("path = %s, want /sandboxes", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if cr.TemplateID != "base" {
			t.Errorf("templateID = %q, want %q", cr.TemplateID, "base")
		}
		if cr.Timeout != DefaultTimeout {
			t.Errorf("timeout = %d, want %d", cr.Timeout, DefaultTimeout)
		}
		if cr.EnvVars["KEY"] != "value" {
			t.Errorf("envVars[KEY] = %q, want %q", cr.EnvVars["KEY"], "value")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-123",
			EnvdAccessToken: "token-abc",
		})
	}))
	defer srv.Close()

	sbx, err := NewSandbox(SandboxConfig{
		APIKey:     "test-key",
		EnvVars:    map[string]string{"KEY": "value"},
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}

	if sbx.ID != "sbx-123" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-123")
	}
	if sbx.accessToken != "token-abc" {
		t.Errorf("accessToken = %q, want %q", sbx.accessToken, "token-abc")
	}
	if sbx.Commands == nil {
		t.Error("Commands is nil")
	}
}

func TestNewSandboxCustomTemplate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if cr.TemplateID != "python3" {
			t.Errorf("templateID = %q, want %q", cr.TemplateID, "python3")
		}
		if cr.Timeout != 120 {
			t.Errorf("timeout = %d, want %d", cr.Timeout, 120)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-custom"})
	}))
	defer srv.Close()

	sbx, err := NewSandbox(SandboxConfig{
		APIKey:     "test-key",
		Template:   "python3",
		Timeout:    120,
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if sbx.ID != "sbx-custom" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-custom")
	}
}

func TestNewSandboxMissingAPIKey(t *testing.T) {
	_, err := NewSandbox(SandboxConfig{})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T", err)
	}
}

func TestNewSandboxAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid api key"))
	}))
	defer srv.Close()

	_, err := NewSandbox(SandboxConfig{
		APIKey:     "bad-key",
		APIBaseURL: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if e.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusUnauthorized)
	}
}

func TestNewSandboxNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := NewSandbox(SandboxConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if e.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusNotFound)
	}
}

func TestNewSandboxInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := NewSandbox(SandboxConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestCloseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/sandboxes/sbx-123" {
			t.Errorf("path = %s, want /sandboxes/sbx-123", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-123",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	if err := sbx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCloseOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-123",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	if err := sbx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCloseNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-gone",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	err := sbx.Close()
	if err == nil {
		t.Fatal("expected error")
	}
	var e *SandboxNotFoundError
	if !errors.As(err, &e) {
		t.Fatalf("expected *SandboxNotFoundError, got %T: %v", err, err)
	}
	if e.SandboxID != "sbx-gone" {
		t.Errorf("SandboxID = %q, want %q", e.SandboxID, "sbx-gone")
	}
}

func TestCloseServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-123",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	err := sbx.Close()
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if e.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusInternalServerError)
	}
}

func TestNewSandboxWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-ctx"})
	}))
	defer srv.Close()

	ctx := context.Background()
	sbx, err := NewSandboxWithContext(ctx, SandboxConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewSandboxWithContext: %v", err)
	}
	if sbx.ID != "sbx-ctx" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-ctx")
	}
}

func TestNewSandboxCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-cancel"})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := NewSandboxWithContext(ctx, SandboxConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestCloseWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-ctx",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	ctx := context.Background()
	if err := sbx.CloseWithContext(ctx); err != nil {
		t.Fatalf("CloseWithContext: %v", err)
	}
}

func TestCloseWithCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-ctx",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sbx.CloseWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestNewSandboxAPIKeyFromEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "env-api-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "env-api-key")
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-env"})
	}))
	defer srv.Close()

	t.Setenv(apiKeyEnv, "env-api-key")

	sbx, err := NewSandbox(SandboxConfig{
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if sbx.ID != "sbx-env" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-env")
	}
}

func TestEnvdURL(t *testing.T) {
	sbx := &Sandbox{
		ID:            "sbx-abc",
		sandboxDomain: "e2b.app",
	}
	got := sbx.envdURL("/process.Process/Start")
	want := "https://49983-sbx-abc.e2b.app/process.Process/Start"
	if got != want {
		t.Errorf("envdURL = %q, want %q", got, want)
	}
}
