package e2b

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newForkTestClient starts an httptest server with the given handler and returns
// a client pointed at it.
func newForkTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := NewClient(ClientConfig{
		APIKey:        "test-key",
		APIBaseURL:    srv.URL,
		SandboxDomain: "example.test",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, srv
}

func TestForkSandboxSuccess(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sandboxes/src-1/fork" {
			t.Errorf("path = %s, want /sandboxes/src-1/fork", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var req forkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.Count != 2 {
			t.Errorf("count = %d, want 2", req.Count)
		}
		if req.Timeout != 120 {
			t.Errorf("timeout = %d, want 120", req.Timeout)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode([]forkResultWire{
			{Sandbox: &forkSandboxObj{SandboxID: "fork-a", EnvdAccessToken: "tok-a", Domain: "fork-domain.test"}},
			{Sandbox: &forkSandboxObj{SandboxID: "fork-b", EnvdAccessToken: "tok-b"}},
		})
	})

	results, err := client.ForkSandbox(context.Background(), "src-1",
		WithForkCount(2), WithForkTimeout(120*time.Second))
	if err != nil {
		t.Fatalf("ForkSandbox: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	for i, r := range results {
		if r.Err != nil {
			t.Errorf("results[%d].Err = %v, want nil", i, r.Err)
		}
		if r.Sandbox == nil {
			t.Fatalf("results[%d].Sandbox is nil", i)
		}
		if r.Sandbox.Commands == nil || r.Sandbox.Pty == nil || r.Sandbox.Filesystem == nil {
			t.Errorf("results[%d]: services not wired", i)
		}
	}

	if results[0].Sandbox.ID != "fork-a" {
		t.Errorf("results[0].ID = %q, want fork-a", results[0].Sandbox.ID)
	}
	if results[0].Sandbox.accessToken != "tok-a" {
		t.Errorf("results[0].accessToken = %q, want tok-a", results[0].Sandbox.accessToken)
	}
	// The first fork reported its own domain; it must be used for envd URLs.
	if got := results[0].Sandbox.envdBaseURL(); got != "https://49983-fork-a.fork-domain.test" {
		t.Errorf("results[0].envdBaseURL = %q, want per-fork domain", got)
	}
	// The second fork reported no domain; it must fall back to the client domain.
	if got := results[1].Sandbox.envdBaseURL(); got != "https://49983-fork-b.example.test" {
		t.Errorf("results[1].envdBaseURL = %q, want client fallback domain", got)
	}
}

func TestForkSandboxMixedSuccessAndRateLimit(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode([]forkResultWire{
			{Sandbox: &forkSandboxObj{SandboxID: "fork-ok", EnvdAccessToken: "tok"}},
			{Error: &forkErrorObj{Code: http.StatusTooManyRequests, Message: "slow down"}},
		})
	})

	results, err := client.ForkSandbox(context.Background(), "src-1", WithForkCount(2))
	if err != nil {
		t.Fatalf("ForkSandbox: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	if results[0].Sandbox == nil || results[0].Err != nil {
		t.Errorf("results[0] = %+v, want a sandbox", results[0])
	}

	if results[1].Sandbox != nil {
		t.Errorf("results[1].Sandbox = %+v, want nil", results[1].Sandbox)
	}
	var rle *RateLimitError
	if !errors.As(results[1].Err, &rle) {
		t.Fatalf("results[1].Err = %v, want *RateLimitError", results[1].Err)
	}
}

func TestForkSandboxPerForkNotFoundStaysGeneric(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode([]forkResultWire{
			{Error: &forkErrorObj{Code: http.StatusNotFound, Message: "snapshot missing"}},
		})
	})

	results, err := client.ForkSandbox(context.Background(), "src-1")
	if err != nil {
		t.Fatalf("ForkSandbox: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	// A per-fork 404 must NOT surface as *SandboxNotFoundError.
	var snf *SandboxNotFoundError
	if errors.As(results[0].Err, &snf) {
		t.Errorf("results[0].Err = %v, want generic *Error not *SandboxNotFoundError", results[0].Err)
	}
	var generic *Error
	if !errors.As(results[0].Err, &generic) {
		t.Fatalf("results[0].Err = %v, want *Error", results[0].Err)
	}
}

func TestForkSandboxPerForkEmptyEntry(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		// Neither sandbox nor error set.
		_ = json.NewEncoder(w).Encode([]forkResultWire{{}})
	})

	results, err := client.ForkSandbox(context.Background(), "src-1")
	if err != nil {
		t.Fatalf("ForkSandbox: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("results[0].Err = nil, want an error")
	}
	if results[0].Err.Error() != "e2b: failed to start forked sandbox" {
		t.Errorf("results[0].Err = %q, want failed-to-start message", results[0].Err.Error())
	}
}

func TestForkSandboxEmptyArray(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode([]forkResultWire{})
	})

	results, err := client.ForkSandbox(context.Background(), "src-1")
	if err != nil {
		t.Fatalf("ForkSandbox: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}

func TestForkSandboxWholeRequestNotFound(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    404,
			"message": `Sandbox "src-1" doesn't exist`,
		})
	})

	results, err := client.ForkSandbox(context.Background(), "src-1")
	if results != nil {
		t.Errorf("results = %v, want nil", results)
	}
	var snf *SandboxNotFoundError
	if !errors.As(err, &snf) {
		t.Fatalf("err = %v, want *SandboxNotFoundError", err)
	}
	if snf.SandboxID != "src-1" {
		t.Errorf("SandboxID = %q, want src-1", snf.SandboxID)
	}
}

func TestForkSandboxWholeRequestInvalidID(t *testing.T) {
	// A malformed sandbox ID surfaces as 400 with an "invalid sandbox ID"
	// message; it must map to *SandboxNotFoundError just like Connect.
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Invalid sandbox ID"))
	})

	_, err := client.ForkSandbox(context.Background(), "bad id")
	var snf *SandboxNotFoundError
	if !errors.As(err, &snf) {
		t.Fatalf("err = %v, want *SandboxNotFoundError", err)
	}
}

func TestForkSandboxWholeRequestOtherBadRequest(t *testing.T) {
	// A 400 that is not about an invalid sandbox ID stays a generic *Error.
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "bad timeout"})
	})

	_, err := client.ForkSandbox(context.Background(), "src-1")
	var snf *SandboxNotFoundError
	if errors.As(err, &snf) {
		t.Errorf("err = %v, want generic *Error not *SandboxNotFoundError", err)
	}
	var ge *Error
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want *Error", err)
	}
}

func TestForkSandboxWholeRequestRateLimit(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "too many"})
	})

	_, err := client.ForkSandbox(context.Background(), "src-1")
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("err = %v, want *RateLimitError", err)
	}
}

func TestForkSandboxWholeRequestUnauthorized(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "bad key"})
	})

	_, err := client.ForkSandbox(context.Background(), "src-1")
	var ae *AuthenticationError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *AuthenticationError", err)
	}
}

func TestForkSandboxInvalidCountNoRequest(t *testing.T) {
	called := false
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})

	_, err := client.ForkSandbox(context.Background(), "src-1", WithForkCount(0))
	var iae *InvalidArgumentError
	if !errors.As(err, &iae) {
		t.Fatalf("err = %v, want *InvalidArgumentError", err)
	}
	if called {
		t.Error("server was called, want no HTTP request for invalid count")
	}
}

func TestForkSandboxDefaults(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req forkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.Count != 1 {
			t.Errorf("default count = %d, want 1", req.Count)
		}
		if req.Timeout != DefaultTimeout {
			t.Errorf("default timeout = %d, want %d", req.Timeout, DefaultTimeout)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode([]forkResultWire{
			{Sandbox: &forkSandboxObj{SandboxID: "fork-a"}},
		})
	})

	if _, err := client.ForkSandbox(context.Background(), "src-1"); err != nil {
		t.Fatalf("ForkSandbox: %v", err)
	}
}

func TestSandboxForkDelegates(t *testing.T) {
	client, _ := newForkTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sandboxes/my-sbx/fork" {
			t.Errorf("path = %s, want /sandboxes/my-sbx/fork", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode([]forkResultWire{
			{Sandbox: &forkSandboxObj{SandboxID: "fork-a"}},
		})
	})

	sbx := client.newSandboxFromResponse("my-sbx", "tok", "", "")
	results, err := sbx.Fork(context.Background())
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if len(results) != 1 || results[0].Sandbox == nil {
		t.Fatalf("results = %+v, want one sandbox", results)
	}
}
