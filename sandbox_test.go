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

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		EnvVars: map[string]string{"KEY": "value"},
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

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		Template: "python3",
		Timeout:  120,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if sbx.ID != "sbx-custom" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-custom")
	}
}

func TestNewSandboxAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid api key"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "bad-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.NewSandbox(context.Background())
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

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.NewSandbox(context.Background())
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

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.NewSandbox(context.Background())
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
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
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
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
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
		ID: "sbx-gone",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
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
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
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

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	sbx, err := client.NewSandbox(context.Background())
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
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

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.NewSandbox(ctx)
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
		ID: "sbx-ctx",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	if err := sbx.CloseWithContext(context.Background()); err != nil {
		t.Fatalf("CloseWithContext: %v", err)
	}
}

func TestCloseWithCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-ctx",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sbx.CloseWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestEnvdBaseURL(t *testing.T) {
	sbx := &Sandbox{
		ID: "sbx-abc",
		client: &Client{
			sandboxDomain: "e2b.app",
		},
	}
	got := sbx.envdBaseURL()
	want := "https://49983-sbx-abc.e2b.app"
	if got != want {
		t.Errorf("envdBaseURL = %q, want %q", got, want)
	}
}

func TestIsRunningHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/health" {
			t.Errorf("path = %s, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:        "test-key",
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = srv.URL[0:4]
					req.URL.Host = srv.Listener.Addr().String()
					return http.DefaultTransport.RoundTrip(req)
				}),
			},
		},
	}

	healthy, err := sbx.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !healthy {
		t.Errorf("IsRunning = %v, want true", healthy)
	}
}

func TestIsRunningHealthyStatus200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = srv.URL[0:4]
					req.URL.Host = srv.Listener.Addr().String()
					return http.DefaultTransport.RoundTrip(req)
				}),
			},
		},
	}

	healthy, err := sbx.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !healthy {
		t.Errorf("IsRunning = %v, want true", healthy)
	}
}

func TestIsRunningNotHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = srv.URL[0:4]
					req.URL.Host = srv.Listener.Addr().String()
					return http.DefaultTransport.RoundTrip(req)
				}),
			},
		},
	}

	healthy, err := sbx.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if healthy {
		t.Errorf("IsRunning = %v, want false for 502", healthy)
	}
}

func TestIsRunningWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = srv.URL[0:4]
					req.URL.Host = srv.Listener.Addr().String()
					return http.DefaultTransport.RoundTrip(req)
				}),
			},
		},
	}

	healthy, err := sbx.IsRunningWithContext(context.Background())
	if err != nil {
		t.Fatalf("IsRunningWithContext: %v", err)
	}
	if !healthy {
		t.Errorf("IsRunningWithContext = %v, want true", healthy)
	}
}

func TestIsRunningCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = srv.URL[0:4]
					req.URL.Host = srv.Listener.Addr().String()
					return http.DefaultTransport.RoundTrip(req)
				}),
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sbx.IsRunningWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
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
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
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
		ID: "sbx-ctx",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	info, err := sbx.InfoWithContext(context.Background())
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
		ID: "sbx-gone",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
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
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
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
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
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
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sbx.InfoWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestSetTimeoutSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sandboxes/sbx-123/timeout" {
			t.Errorf("path = %s, want /sandboxes/sbx-123/timeout", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var body setTimeoutRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Timeout != 600 {
			t.Errorf("timeout = %d, want %d", body.Timeout, 600)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	if err := sbx.SetTimeout(600); err != nil {
		t.Fatalf("SetTimeout: %v", err)
	}
}

func TestSetTimeoutWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	if err := sbx.SetTimeoutWithContext(context.Background(), 300); err != nil {
		t.Fatalf("SetTimeoutWithContext: %v", err)
	}
}

func TestSetTimeoutNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"sandbox not found"}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-gone",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	err := sbx.SetTimeout(600)
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

func TestSetTimeoutServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	err := sbx.SetTimeout(600)
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

func TestSetTimeoutCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sbx.SetTimeoutWithContext(ctx, 600)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestSetTimeoutOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	if err := sbx.SetTimeout(600); err != nil {
		t.Fatalf("SetTimeout with 200 OK: %v", err)
	}
}

func TestMetricsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/sandboxes/sbx-123/metrics" {
			t.Errorf("path = %s, want /sandboxes/sbx-123/metrics", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SandboxMetric{
			{
				CPUCount:      2,
				CPUUsedPct:    13.43,
				MemTotal:      505417728,
				MemUsed:       49197056,
				MemCache:      69632000,
				DiskTotal:     22772514816,
				DiskUsed:      1681707008,
				Timestamp:     "2026-05-19T07:11:20Z",
				TimestampUnix: 1779174680,
			},
			{
				CPUCount:      2,
				CPUUsedPct:    0.6,
				MemTotal:      505417728,
				MemUsed:       50085888,
				MemCache:      69632000,
				DiskTotal:     22772514816,
				DiskUsed:      1681707008,
				Timestamp:     "2026-05-19T07:11:25Z",
				TimestampUnix: 1779174685,
			},
		})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	metrics, err := sbx.Metrics()
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}

	if len(metrics) != 2 {
		t.Fatalf("len = %d, want 2", len(metrics))
	}
	if metrics[0].CPUCount != 2 {
		t.Errorf("CPUCount = %d, want 2", metrics[0].CPUCount)
	}
	if metrics[0].CPUUsedPct != 13.43 {
		t.Errorf("CPUUsedPct = %f, want 13.43", metrics[0].CPUUsedPct)
	}
	if metrics[0].MemTotal != 505417728 {
		t.Errorf("MemTotal = %d, want 505417728", metrics[0].MemTotal)
	}
	if metrics[0].DiskUsed != 1681707008 {
		t.Errorf("DiskUsed = %d, want 1681707008", metrics[0].DiskUsed)
	}
	if metrics[0].TimestampUnix != 1779174680 {
		t.Errorf("TimestampUnix = %d, want 1779174680", metrics[0].TimestampUnix)
	}
	if metrics[1].CPUUsedPct != 0.6 {
		t.Errorf("metrics[1].CPUUsedPct = %f, want 0.6", metrics[1].CPUUsedPct)
	}
}

func TestMetricsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	metrics, err := sbx.Metrics()
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("len = %d, want 0", len(metrics))
	}
}

func TestMetricsWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SandboxMetric{{CPUCount: 4, CPUUsedPct: 50.0}})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	metrics, err := sbx.MetricsWithContext(context.Background())
	if err != nil {
		t.Fatalf("MetricsWithContext: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("len = %d, want 1", len(metrics))
	}
	if metrics[0].CPUCount != 4 {
		t.Errorf("CPUCount = %d, want 4", metrics[0].CPUCount)
	}
}

func TestMetricsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	_, err := sbx.Metrics()
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

func TestMetricsInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	_, err := sbx.Metrics()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMetricsCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sbx.MetricsWithContext(ctx)
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
	// Force an empty API key so the test is hermetic even when E2B_API_KEY
	// is set in the ambient environment (e.g. during integration runs).
	t.Setenv(apiKeyEnv, "")

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

// ============================================================================
// ListSandboxesV2 unit tests
// ============================================================================

func TestClientListSandboxesV2Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v2/sandboxes" {
			t.Errorf("path = %s, want /v2/sandboxes", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SandboxInfo{
			{
				ID:         "sbx-running",
				Template:   "base",
				State:      "running",
				CPUCount:   2,
				MemoryMB:   512,
				DiskSizeMB: 23318,
				StartedAt:  "2024-01-01T00:00:00Z",
			},
			{
				ID:         "sbx-paused",
				Template:   "python3",
				State:      "paused",
				CPUCount:   4,
				MemoryMB:   1024,
				DiskSizeMB: 23318,
				StartedAt:  "2024-01-01T00:05:00Z",
				Metadata:   map[string]string{"env": "dev"},
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

	result, err := client.ListSandboxesV2(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxesV2: %v", err)
	}

	if len(result.Sandboxes) != 2 {
		t.Fatalf("len = %d, want 2", len(result.Sandboxes))
	}
	if result.Sandboxes[0].ID != "sbx-running" {
		t.Errorf("items[0].ID = %q, want %q", result.Sandboxes[0].ID, "sbx-running")
	}
	if result.Sandboxes[0].State != "running" {
		t.Errorf("items[0].State = %q, want %q", result.Sandboxes[0].State, "running")
	}
	if result.Sandboxes[1].ID != "sbx-paused" {
		t.Errorf("items[1].ID = %q, want %q", result.Sandboxes[1].ID, "sbx-paused")
	}
	if result.Sandboxes[1].State != "paused" {
		t.Errorf("items[1].State = %q, want %q", result.Sandboxes[1].State, "paused")
	}
}

func TestClientListSandboxesV2FilterByState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query()["state"]
		t.Logf("received state query: %v", state)

		// Only return paused sandboxes when filtering.
		if len(state) == 1 && state[0] == "paused" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]SandboxInfo{
				{
					ID:    "sbx-paused",
					State: "paused",
				},
			})
			return
		}
		// Return all sandboxes.
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SandboxInfo{
			{ID: "sbx-running", State: "running"},
			{ID: "sbx-paused", State: "paused"},
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

	// Filter by paused only.
	result, err := client.ListSandboxesV2(context.Background(), WithSandboxState("paused"))
	if err != nil {
		t.Fatalf("ListSandboxesV2: %v", err)
	}
	if len(result.Sandboxes) != 1 {
		t.Fatalf("len = %d, want 1", len(result.Sandboxes))
	}
	if result.Sandboxes[0].State != "paused" {
		t.Errorf("state = %q, want %q", result.Sandboxes[0].State, "paused")
	}
}

func TestClientListSandboxesV2FilterByMultipleStates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		states := r.URL.Query()["state"]
		if len(states) != 2 {
			t.Errorf("expected 2 state params, got %d: %v", len(states), states)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SandboxInfo{
			{ID: "sbx-1", State: "running"},
			{ID: "sbx-2", State: "paused"},
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

	result, err := client.ListSandboxesV2(context.Background(), WithSandboxState("running", "paused"))
	if err != nil {
		t.Fatalf("ListSandboxesV2: %v", err)
	}
	if len(result.Sandboxes) != 2 {
		t.Fatalf("len = %d, want 2", len(result.Sandboxes))
	}
}

func TestClientListSandboxesV2WithMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify metadata query params are present.
		metaValues := r.URL.Query()["metadata"]
		if len(metaValues) != 2 {
			t.Errorf("expected 2 metadata params, got %d: %v", len(metaValues), metaValues)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SandboxInfo{
			{
				ID:       "sbx-dev",
				State:    "running",
				Metadata: map[string]string{"env": "dev", "app": "myapp"},
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

	result, err := client.ListSandboxesV2(context.Background(),
		WithSandboxMetadata(map[string]string{"env": "dev", "app": "myapp"}))
	if err != nil {
		t.Fatalf("ListSandboxesV2: %v", err)
	}
	if len(result.Sandboxes) != 1 {
		t.Fatalf("len = %d, want 1", len(result.Sandboxes))
	}
	if result.Sandboxes[0].ID != "sbx-dev" {
		t.Errorf("ID = %q, want %q", result.Sandboxes[0].ID, "sbx-dev")
	}
}

func TestClientListSandboxesV2Pagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextToken := r.URL.Query().Get("nextToken")
		limit := r.URL.Query().Get("limit")

		if nextToken == "" {
			// First page.
			if limit != "1" {
				t.Errorf("first page limit = %q, want %q", limit, "1")
			}
			w.Header().Set("X-Next-Token", "page2-token")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]SandboxInfo{
				{ID: "sbx-1", State: "running"},
			})
		} else {
			if nextToken != "page2-token" {
				t.Errorf("nextToken = %q, want %q", nextToken, "page2-token")
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]SandboxInfo{
				{ID: "sbx-2", State: "paused"},
			})
		}
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// First page.
	result, err := client.ListSandboxesV2(context.Background(), WithSandboxLimit(1))
	if err != nil {
		t.Fatalf("ListSandboxesV2 page1: %v", err)
	}
	if result.NextToken != "page2-token" {
		t.Errorf("NextToken = %q, want %q", result.NextToken, "page2-token")
	}
	if len(result.Sandboxes) != 1 || result.Sandboxes[0].ID != "sbx-1" {
		t.Fatalf("page1 unexpected: %+v", result.Sandboxes)
	}

	// Second page.
	result2, err := client.ListSandboxesV2(context.Background(), WithSandboxNextToken(result.NextToken))
	if err != nil {
		t.Fatalf("ListSandboxesV2 page2: %v", err)
	}
	if result2.NextToken != "" {
		t.Errorf("NextToken = %q, want empty", result2.NextToken)
	}
	if len(result2.Sandboxes) != 1 || result2.Sandboxes[0].ID != "sbx-2" {
		t.Fatalf("page2 unexpected: %+v", result2.Sandboxes)
	}
}

func TestClientListSandboxesV2URLEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify metadata values decode correctly despite special characters.
		metaValues := r.URL.Query()["metadata"]
		expected := map[string]bool{
			"env=prod":        true,
			"session=abc&def": true,
			"key=1+2=3":       true,
		}
		found := 0
		for _, mv := range metaValues {
			t.Logf("decoded metadata: %q", mv)
			if expected[mv] {
				found++
			}
		}
		if found != 3 {
			t.Errorf("expected 3 metadata values, got %d: %v", found, metaValues)
		}

		// Verify nextToken decodes correctly.
		if got := r.URL.Query().Get("nextToken"); got != "tok+/%x" {
			t.Errorf("nextToken = %q, want %q", got, "tok+/%x")
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})

	_, err := client.ListSandboxesV2(context.Background(),
		WithSandboxMetadata(map[string]string{
			"env":     "prod",
			"session": "abc&def",
			"key":     "1+2=3",
		}),
		WithSandboxNextToken("tok+/%x"),
	)
	if err != nil {
		t.Fatalf("ListSandboxesV2: %v", err)
	}
}

func TestClientListSandboxesV2Empty(t *testing.T) {
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

	result, err := client.ListSandboxesV2(context.Background())
	if err != nil {
		t.Fatalf("ListSandboxesV2: %v", err)
	}
	if len(result.Sandboxes) != 0 {
		t.Errorf("len = %d, want 0", len(result.Sandboxes))
	}
	if result.NextToken != "" {
		t.Errorf("NextToken = %q, want empty", result.NextToken)
	}
}

func TestClientListSandboxesV2ServerError(t *testing.T) {
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

	_, err = client.ListSandboxesV2(context.Background())
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

func TestClientListSandboxesV2Unauthorized(t *testing.T) {
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

	_, err = client.ListSandboxesV2(context.Background())
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

func TestClientListSandboxesV2InvalidJSON(t *testing.T) {
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

	_, err = client.ListSandboxesV2(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestClientListSandboxesV2CanceledContext(t *testing.T) {
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

	_, err = client.ListSandboxesV2(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestLogsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v2/sandboxes/sbx-123/logs" {
			t.Errorf("path = %s, want /v2/sandboxes/sbx-123/logs", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(logsResponse{
			Logs: []SandboxLogEntry{
				{
					Level:     "info",
					Message:   "Sandbox created",
					Timestamp: "2026-05-26T05:02:49.929925877Z",
					Fields:    map[string]string{"logger": "orchestration-api", "sandboxID": "sbx-123"},
				},
				{
					Level:     "debug",
					Message:   "Started creating sandbox",
					Timestamp: "2026-05-26T05:02:49.802974399Z",
					Fields:    map[string]string{"logger": "orchestration-api"},
				},
			},
		})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	logs, err := sbx.Logs()
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("len = %d, want 2", len(logs))
	}
	if logs[0].Level != "info" {
		t.Errorf("Level = %q, want %q", logs[0].Level, "info")
	}
	if logs[0].Message != "Sandbox created" {
		t.Errorf("Message = %q, want %q", logs[0].Message, "Sandbox created")
	}
	if logs[0].Fields["logger"] != "orchestration-api" {
		t.Errorf("Fields[logger] = %q, want %q", logs[0].Fields["logger"], "orchestration-api")
	}
	if logs[1].Level != "debug" {
		t.Errorf("logs[1].Level = %q, want %q", logs[1].Level, "debug")
	}
}

func TestLogsWithOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("limit"); got != "5" {
			t.Errorf("limit = %q, want %q", got, "5")
		}
		if got := q.Get("direction"); got != "forward" {
			t.Errorf("direction = %q, want %q", got, "forward")
		}
		if got := q.Get("level"); got != "info" {
			t.Errorf("level = %q, want %q", got, "info")
		}
		if got := q.Get("search"); got != "sandbox" {
			t.Errorf("search = %q, want %q", got, "sandbox")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(logsResponse{
			Logs: []SandboxLogEntry{
				{Level: "info", Message: "Sandbox created", Timestamp: "2026-05-26T05:02:49Z"},
			},
		})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	logs, err := sbx.LogsWithContext(context.Background(),
		WithLimit(5),
		WithDirection("forward"),
		WithLevel("info"),
		WithSearch("sandbox"),
	)
	if err != nil {
		t.Fatalf("LogsWithContext: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len = %d, want 1", len(logs))
	}
}

func TestLogsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"logs":[]}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	logs, err := sbx.Logs()
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(logs) != 0 {
		t.Errorf("len = %d, want 0", len(logs))
	}
}

func TestLogsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	_, err := sbx.Logs()
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

func TestLogsInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	_, err := sbx.Logs()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLogsCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"logs":[]}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sbx.LogsWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestLogsNoOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("expected no query params, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"logs":[]}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	_, err := sbx.Logs()
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
}

func TestPauseSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sandboxes/sbx-123/pause" {
			t.Errorf("path = %s, want /sandboxes/sbx-123/pause", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}
		var body struct {
			KeepMemory bool `json:"keepMemory"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !body.KeepMemory {
			t.Errorf("keepMemory = %v, want true (default)", body.KeepMemory)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	if err := sbx.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
}

func TestPauseWithKeepMemory(t *testing.T) {
	t.Run("keepMemory=false", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				KeepMemory bool `json:"keepMemory"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.KeepMemory {
				t.Errorf("keepMemory = %v, want false", body.KeepMemory)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		sbx := &Sandbox{
			ID: "sbx-123",
			client: &Client{
				apiKey:     "test-key",
				apiBaseURL: srv.URL,
				httpClient: http.DefaultClient,
			},
		}

		if err := sbx.Pause(WithKeepMemory(false)); err != nil {
			t.Fatalf("Pause(false): %v", err)
		}
	})

	t.Run("keepMemory=true explicit", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				KeepMemory bool `json:"keepMemory"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if !body.KeepMemory {
				t.Errorf("keepMemory = %v, want true", body.KeepMemory)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		sbx := &Sandbox{
			ID: "sbx-123",
			client: &Client{
				apiKey:     "test-key",
				apiBaseURL: srv.URL,
				httpClient: http.DefaultClient,
			},
		}

		if err := sbx.Pause(WithKeepMemory(true)); err != nil {
			t.Fatalf("Pause(true): %v", err)
		}
	})
}

func TestPauseWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	if err := sbx.PauseWithContext(context.Background()); err != nil {
		t.Fatalf("PauseWithContext: %v", err)
	}
}

func TestPauseNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"sandbox not found"}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-gone",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	err := sbx.Pause()
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

func TestPauseConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":409,"message":"sandbox is already paused"}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	err := sbx.Pause()
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if e.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusConflict)
	}
}

func TestPauseServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	err := sbx.Pause()
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

func TestPauseCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sbx.PauseWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestResumeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sandboxes/sbx-123/resume" {
			t.Errorf("path = %s, want /sandboxes/sbx-123/resume", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var body resumeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Timeout != 120 {
			t.Errorf("timeout = %d, want %d", body.Timeout, 120)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-123",
			EnvdAccessToken: "token-abc",
		})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	if err := sbx.Resume(120); err != nil {
		t.Fatalf("Resume: %v", err)
	}
}

func TestResumeWithContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-123"})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	if err := sbx.ResumeWithContext(context.Background(), 300); err != nil {
		t.Fatalf("ResumeWithContext: %v", err)
	}
}

func TestResumeNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"sandbox not found"}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-gone",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	err := sbx.Resume(120)
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

func TestResumeConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":409,"message":"sandbox is already running"}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	err := sbx.Resume(120)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if e.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusConflict)
	}
}

func TestResumeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	err := sbx.Resume(120)
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

func TestResumeCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-123"})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sbx.ResumeWithContext(ctx, 120)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// --- CreateSnapshot tests ---

func TestCreateSnapshotSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sandboxes/sbx-123/snapshots" {
			t.Errorf("path = %s, want /sandboxes/sbx-123/snapshots", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(SnapshotInfo{
			Names:      []string{"team/my-snap"},
			SnapshotID: "team/my-snap:default",
		})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	info, err := sbx.CreateSnapshot(context.Background(), "my-snap")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if info.SnapshotID != "team/my-snap:default" {
		t.Errorf("SnapshotID = %q, want %q", info.SnapshotID, "team/my-snap:default")
	}
	if len(info.Names) != 1 || info.Names[0] != "team/my-snap" {
		t.Errorf("Names = %v, want [team/my-snap]", info.Names)
	}
}

func TestCreateSnapshotNoName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createSnapshotRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Name != "" {
			t.Errorf("name = %q, want empty", body.Name)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(SnapshotInfo{
			Names:      []string{},
			SnapshotID: "rawid123:default",
		})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	info, err := sbx.CreateSnapshot(context.Background())
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if info.SnapshotID != "rawid123:default" {
		t.Errorf("SnapshotID = %q, want %q", info.SnapshotID, "rawid123:default")
	}
	if len(info.Names) != 0 {
		t.Errorf("Names = %v, want empty", info.Names)
	}
}

func TestCreateSnapshotNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"Sandbox not found"}`))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-gone",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	_, err := sbx.CreateSnapshot(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var notFound *SandboxNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error type = %T, want *SandboxNotFoundError", err)
	}
	if notFound.SandboxID != "sbx-gone" {
		t.Errorf("SandboxID = %q, want %q", notFound.SandboxID, "sbx-gone")
	}
}

func TestCreateSnapshotServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	_, err := sbx.CreateSnapshot(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusInternalServerError)
	}
}

func TestCreateSnapshotInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	_, err := sbx.CreateSnapshot(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCreateSnapshotCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(SnapshotInfo{SnapshotID: "snap-1"})
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID: "sbx-123",
		client: &Client{
			apiKey:     "test-key",
			apiBaseURL: srv.URL,
			httpClient: http.DefaultClient,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sbx.CreateSnapshot(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// --- ListSnapshots tests ---

func TestListSnapshotsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/snapshots" {
			t.Errorf("path = %s, want /snapshots", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SnapshotInfo{
			{Names: []string{"team/snap1"}, SnapshotID: "team/snap1:default"},
			{Names: []string{}, SnapshotID: "rawid:default"},
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

	result, err := client.ListSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(result.Snapshots) != 2 {
		t.Fatalf("len = %d, want 2", len(result.Snapshots))
	}
	if result.Snapshots[0].SnapshotID != "team/snap1:default" {
		t.Errorf("Snapshots[0].SnapshotID = %q, want %q", result.Snapshots[0].SnapshotID, "team/snap1:default")
	}
	if result.NextToken != "" {
		t.Errorf("NextToken = %q, want empty", result.NextToken)
	}
}

func TestListSnapshotsWithOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("sandboxID"); got != "sbx-123" {
			t.Errorf("sandboxID = %q, want %q", got, "sbx-123")
		}
		if got := q.Get("limit"); got != "5" {
			t.Errorf("limit = %q, want %q", got, "5")
		}
		if got := q.Get("nextToken"); got != "abc123" {
			t.Errorf("nextToken = %q, want %q", got, "abc123")
		}

		w.Header().Set("X-Next-Token", "next-page-token")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]SnapshotInfo{
			{Names: []string{}, SnapshotID: "snap1:default"},
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

	result, err := client.ListSnapshots(context.Background(),
		WithSnapshotSandboxID("sbx-123"),
		WithSnapshotLimit(5),
		WithSnapshotNextToken("abc123"),
	)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(result.Snapshots) != 1 {
		t.Fatalf("len = %d, want 1", len(result.Snapshots))
	}
	if result.NextToken != "next-page-token" {
		t.Errorf("NextToken = %q, want %q", result.NextToken, "next-page-token")
	}
}

func TestListSnapshotsEmpty(t *testing.T) {
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

	result, err := client.ListSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(result.Snapshots) != 0 {
		t.Errorf("len = %d, want 0", len(result.Snapshots))
	}
}

func TestListSnapshotsServerError(t *testing.T) {
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

	_, err = client.ListSnapshots(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusInternalServerError)
	}
}

func TestListSnapshotsInvalidJSON(t *testing.T) {
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

	_, err = client.ListSnapshots(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestListSnapshotsCanceledContext(t *testing.T) {
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

	_, err = client.ListSnapshots(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// --- Connect tests ---

func TestConnectSuccessAlreadyRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sandboxes/sbx-123/connect" {
			t.Errorf("path = %s, want /sandboxes/sbx-123/connect", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var body struct {
			Timeout int `json:"timeout"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Timeout != 120 {
			t.Errorf("timeout = %d, want %d", body.Timeout, 120)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-123",
			EnvdAccessToken: "token-abc",
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

	sbx, err := client.Connect(context.Background(), "sbx-123", 120)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if sbx.ID != "sbx-123" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-123")
	}
	if sbx.accessToken != "token-abc" {
		t.Errorf("accessToken = %q, want %q", sbx.accessToken, "token-abc")
	}
	if sbx.Commands == nil {
		t.Error("Commands service not initialized")
	}
	if sbx.Filesystem == nil {
		t.Error("Filesystem service not initialized")
	}
}

func TestConnectSuccessResumedFromPaused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:          "sbx-paused",
			EnvdAccessToken:    "token-resumed",
			TrafficAccessToken: "traffic-token",
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

	sbx, err := client.Connect(context.Background(), "sbx-paused", 300)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if sbx.ID != "sbx-paused" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-paused")
	}
	if sbx.TrafficAccessToken != "traffic-token" {
		t.Errorf("TrafficAccessToken = %q, want %q", sbx.TrafficAccessToken, "traffic-token")
	}
}

func TestConnectNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"sandbox not found"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Connect(context.Background(), "sbx-gone", 120)
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

func TestConnectServerError(t *testing.T) {
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

	_, err = client.Connect(context.Background(), "sbx-123", 120)
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

func TestConnectCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
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

	_, err = client.Connect(ctx, "sbx-123", 120)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// --- NewSandbox lifecycle tests ---

func TestNewSandboxWithAutoPause(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !cr.AutoPause {
			t.Errorf("autoPause = %v, want true", cr.AutoPause)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-123",
			EnvdAccessToken: "token-abc",
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

	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		AutoPause: true,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()

	if sbx.ID != "sbx-123" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-123")
	}
}

func TestNewSandboxWithAutoPauseMemory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !cr.AutoPause {
			t.Errorf("autoPause = %v, want true", cr.AutoPause)
		}
		if cr.AutoPauseMemory == nil || *cr.AutoPauseMemory {
			t.Errorf("autoPauseMemory should be false")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-123",
			EnvdAccessToken: "token-abc",
		})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	autoPauseMem := false
	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		AutoPause:       true,
		AutoPauseMemory: &autoPauseMem,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()
}

func TestNewSandboxWithAutoResume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if cr.AutoResume == nil || !cr.AutoResume.Enabled {
			t.Errorf("autoResume.enabled should be true")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-123",
			EnvdAccessToken: "token-abc",
		})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		AutoResume: &AutoResumeConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()
}

func TestNewSandboxWithMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if cr.Metadata["env"] != "prod" {
			t.Errorf("metadata[env] = %q, want %q", cr.Metadata["env"], "prod")
		}
		if cr.Metadata["user"] != "alice" {
			t.Errorf("metadata[user] = %q, want %q", cr.Metadata["user"], "alice")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-123",
			EnvdAccessToken: "token-abc",
		})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		Metadata: map[string]string{"env": "prod", "user": "alice"},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()
}

func TestNewSandboxWithVolumeMounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(cr.VolumeMounts) != 1 {
			t.Fatalf("len(volumeMounts) = %d, want 1", len(cr.VolumeMounts))
		}
		if cr.VolumeMounts[0].Name != "data-vol" {
			t.Errorf("volumeMounts[0].Name = %q, want %q", cr.VolumeMounts[0].Name, "data-vol")
		}
		if cr.VolumeMounts[0].Path != "/mnt/data" {
			t.Errorf("volumeMounts[0].Path = %q, want %q", cr.VolumeMounts[0].Path, "/mnt/data")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-123",
			EnvdAccessToken: "token-abc",
		})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		VolumeMounts: []VolumeMount{{Name: "data-vol", Path: "/mnt/data"}},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()
}

func TestNewSandboxFullLifecycleConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var cr createRequest
		if err := json.NewDecoder(r.Body).Decode(&cr); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !cr.AutoPause {
			t.Errorf("autoPause should be true")
		}
		if cr.AutoPauseMemory == nil || *cr.AutoPauseMemory {
			t.Errorf("autoPauseMemory should be false")
		}
		if cr.AutoResume == nil || !cr.AutoResume.Enabled {
			t.Errorf("autoResume.enabled should be true")
		}
		if cr.Metadata["app"] != "my-app" {
			t.Errorf("metadata[app] = %q, want %q", cr.Metadata["app"], "my-app")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{
			SandboxID:       "sbx-full",
			EnvdAccessToken: "token-full",
		})
	}))
	defer srv.Close()

	client, _ := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	autoPauseMem := false
	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		AutoPause:       true,
		AutoPauseMemory: &autoPauseMem,
		AutoResume:      &AutoResumeConfig{Enabled: true},
		Metadata:        map[string]string{"app": "my-app"},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()
	if sbx.ID != "sbx-full" {
		t.Errorf("ID = %q, want %q", sbx.ID, "sbx-full")
	}
}
