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

func TestEnvdBaseURL(t *testing.T) {
	sbx := &Sandbox{
		ID:            "sbx-abc",
		sandboxDomain: "e2b.app",
	}
	got := sbx.envdBaseURL()
	want := "https://49983-sbx-abc.e2b.app"
	if got != want {
		t.Errorf("envdBaseURL = %q, want %q", got, want)
	}
}

func TestInfoSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/sandboxes/sbx-123" {
			t.Errorf("path = %s, want /sandboxes/sbx-123", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SandboxInfo{
			ID:          "sbx-123",
			Template:    "base",
			State:       "running",
			StartedAt:   "2024-01-01T00:00:00Z",
			CPUCount:    2,
			MemoryMB:    512,
			DiskSizeMB:  1024,
			EnvdVersion: "0.5.14",
			Lifecycle: SandboxLifecycle{
				AutoResume: false,
				OnTimeout:  "kill",
			},
		})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-123",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	info, err := sbx.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}

	if info.ID != "sbx-123" {
		t.Errorf("ID = %q, want %q", info.ID, "sbx-123")
	}
	if info.Template != "base" {
		t.Errorf("Template = %q, want %q", info.Template, "base")
	}
	if info.State != "running" {
		t.Errorf("State = %q, want %q", info.State, "running")
	}
	if info.CPUCount != 2 {
		t.Errorf("CPUCount = %d, want %d", info.CPUCount, 2)
	}
	if info.MemoryMB != 512 {
		t.Errorf("MemoryMB = %d, want %d", info.MemoryMB, 512)
	}
}

func TestInfoWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SandboxInfo{
			ID:       "sbx-ctx",
			Template: "python3",
			State:    "running",
		})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-ctx",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	ctx := context.Background()
	info, err := sbx.InfoWithContext(ctx)
	if err != nil {
		t.Fatalf("InfoWithContext: %v", err)
	}

	if info.ID != "sbx-ctx" {
		t.Errorf("ID = %q, want %q", info.ID, "sbx-ctx")
	}
	if info.Template != "python3" {
		t.Errorf("Template = %q, want %q", info.Template, "python3")
	}
	if info.State != "running" {
		t.Errorf("State = %q, want %q", info.State, "running")
	}
}

func TestInfoNotFound(t *testing.T) {
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

	_, err := sbx.Info()
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

func TestInfoServerError(t *testing.T) {
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

	_, err := sbx.Info()
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

func TestInfoInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-123",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	_, err := sbx.Info()
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestInfoWithCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SandboxInfo{ID: "sbx-123"})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:         "sbx-123",
		apiKey:     "test-key",
		apiBaseURL: srv.URL,
		httpClient: http.DefaultClient,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sbx.InfoWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestClientListSandboxesSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/sandboxes" {
			t.Errorf("path = %s, want /sandboxes", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SandboxInfo{
			{
				ID:         "sbx-1",
				Template:   "base",
				State:      "running",
				CPUCount:   2,
				MemoryMB:   512,
				DiskSizeMB: 23318,
				StartedAt:  "2024-01-01T00:00:00Z",
				EndAt:      "2024-01-01T00:10:00Z",
			},
			{
				ID:         "sbx-2",
				Template:   "python3",
				State:      "running",
				CPUCount:   4,
				MemoryMB:   1024,
				DiskSizeMB: 23318,
				StartedAt:  "2024-01-01T00:05:00Z",
			},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	items, err := client.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	if items[0].ID != "sbx-1" {
		t.Errorf("items[0].ID = %q, want %q", items[0].ID, "sbx-1")
	}
	if items[0].CPUCount != 2 {
		t.Errorf("items[0].CPUCount = %d, want %d", items[0].CPUCount, 2)
	}
	if items[1].ID != "sbx-2" {
		t.Errorf("items[1].ID = %q, want %q", items[1].ID, "sbx-2")
	}
	if items[1].MemoryMB != 1024 {
		t.Errorf("items[1].MemoryMB = %d, want %d", items[1].MemoryMB, 1024)
	}
}

func TestClientListSandboxesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	items, err := client.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, want 0", len(items))
	}
}

func TestClientNewClientMissingAPIKey(t *testing.T) {
	_, err := NewClient(ClientConfig{})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T", err)
	}
}

func TestClientAPIKeyFromEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "env-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "env-key")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	t.Setenv(apiKeyEnv, "env-key")

	client, err := NewClient(ClientConfig{
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ListSandboxes(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxes: %v", err)
	}
}

func TestClientListSandboxesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ListSandboxes(context.Background())
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

func TestClientListSandboxesUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"message":"invalid api key"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "bad-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ListSandboxes(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if e.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusUnauthorized)
	}
}

func TestClientListSandboxesInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ListSandboxes(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestClientListSandboxesCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.ListSandboxes(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}
