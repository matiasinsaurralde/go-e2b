package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ForkResult is the outcome of one requested fork. Exactly one of Sandbox or
// Err is non-nil: Sandbox is a running, ready-to-use fork; Err explains why that
// fork failed to start. Per-fork errors map to the same error types as other API
// errors (for example, *RateLimitError for HTTP 429).
type ForkResult struct {
	// Sandbox is the running fork, or nil when this fork failed to start.
	Sandbox *Sandbox

	// Err describes why this fork failed to start, or nil on success.
	Err error
}

// ForkOption configures a ForkSandbox or Sandbox.Fork call.
type ForkOption func(*forkConfig)

type forkConfig struct {
	count   int
	timeout time.Duration
}

// WithForkCount sets the number of forks to create. It defaults to 1 and must be
// at least 1. All forks boot from the same snapshot — the source is snapshotted
// once regardless of count.
func WithForkCount(n int) ForkOption {
	return func(fc *forkConfig) { fc.count = n }
}

// WithForkTimeout sets the lifetime of each forked sandbox. It defaults to
// DefaultTimeout (300s). The value is sent to the API rounded to whole seconds.
func WithForkTimeout(d time.Duration) ForkOption {
	return func(fc *forkConfig) { fc.timeout = d }
}

// forkRequest is the request body for the fork endpoint.
type forkRequest struct {
	Count   int `json:"count"`
	Timeout int `json:"timeout"` // seconds
}

// forkSandboxObj is the connection info of a successfully started fork. Optional
// fields may be absent for non-secure sandboxes.
type forkSandboxObj struct {
	SandboxID          string `json:"sandboxID"`
	EnvdVersion        string `json:"envdVersion"`
	Domain             string `json:"domain"`
	EnvdAccessToken    string `json:"envdAccessToken"`
	TrafficAccessToken string `json:"trafficAccessToken"`
}

// forkErrorObj is the error for a fork that failed to start.
type forkErrorObj struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// forkResultWire is one element of the fork response array. Exactly one of
// Sandbox or Error is set.
type forkResultWire struct {
	Sandbox *forkSandboxObj `json:"sandbox"`
	Error   *forkErrorObj   `json:"error"`
}

// ForkSandbox forks a running sandbox, identified by ID, into count copies. The
// source sandbox is checkpointed in place — briefly paused, snapshotted with its
// full memory state, and resumed; its ID and expiration stay untouched — and
// each fork boots from that single snapshot.
//
// The returned slice has one entry per requested fork. Each fork succeeds or
// fails independently: an entry's Sandbox is a running fork, or its Err explains
// why that fork failed to start (per-fork errors map to the same error types as
// other API errors, e.g. *RateLimitError for 429).
//
// The returned error is non-nil only when the whole request fails — for example
// a missing source sandbox (*SandboxNotFoundError), authentication failure, or a
// malformed request — in which case the result slice is nil.
func (c *Client) ForkSandbox(ctx context.Context, sandboxID string, opts ...ForkOption) ([]ForkResult, error) {
	fc := &forkConfig{count: 1, timeout: DefaultTimeout * time.Second}
	for _, o := range opts {
		o(fc)
	}

	if fc.count < 1 {
		return nil, &InvalidArgumentError{Message: "count must be at least 1"}
	}

	body, err := json.Marshal(forkRequest{
		Count:   fc.count,
		Timeout: int(fc.timeout / time.Second),
	})
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal fork request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/sandboxes/"+sandboxID+"/fork", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: build fork request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send fork request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// Proceed to decode the per-fork results.
	default:
		respBody, _ := io.ReadAll(resp.Body)
		// A missing source sandbox surfaces as 404; a malformed sandbox ID
		// surfaces as 400 with an "invalid sandbox ID" message. Map both to the
		// typed SandboxNotFoundError so callers can handle "no such sandbox"
		// uniformly (mirrors Client.Connect), while leaving other statuses to
		// the shared code mapping (401 -> auth, 429 -> rate limit, else generic).
		if resp.StatusCode == http.StatusNotFound ||
			(resp.StatusCode == http.StatusBadRequest &&
				strings.Contains(strings.ToLower(string(respBody)), "invalid sandbox id")) {
			return nil, &SandboxNotFoundError{SandboxID: sandboxID}
		}
		if msg := errorMessageFromBody(respBody); msg != "" {
			return nil, apiErrorFromCode(resp.StatusCode, msg)
		}
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var wire []forkResultWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("e2b: decode fork response: %w", err)
	}

	results := make([]ForkResult, 0, len(wire))
	for _, w := range wire {
		results = append(results, c.forkResultFromWire(w))
	}
	return results, nil
}

// forkResultFromWire converts one wire result into a ForkResult, building a
// fully-wired *Sandbox on success and mapping per-fork errors to typed errors.
func (c *Client) forkResultFromWire(w forkResultWire) ForkResult {
	if w.Error != nil || w.Sandbox == nil {
		return ForkResult{Err: forkEntryError(w.Error)}
	}
	return ForkResult{
		Sandbox: c.newSandboxFromResponse(
			w.Sandbox.SandboxID,
			w.Sandbox.EnvdAccessToken,
			w.Sandbox.TrafficAccessToken,
			w.Sandbox.Domain,
		),
	}
}

// forkEntryError maps a per-fork error object to a typed error. A nil error
// object (no sandbox and no error) means the fork failed to start for an
// unreported reason. A per-fork 404 is left generic: it refers to a resource
// needed to start that fork (e.g. the snapshot), not the source sandbox, which
// would have failed the whole request.
func forkEntryError(e *forkErrorObj) error {
	if e == nil {
		return &Error{Message: "failed to start forked sandbox"}
	}
	if e.Code == http.StatusNotFound {
		return &Error{Message: fmt.Sprintf("%d: %s", e.Code, e.Message)}
	}
	return apiErrorFromCode(e.Code, e.Message)
}

// Fork forks this sandbox. It is equivalent to calling Client.ForkSandbox with
// this sandbox's ID; see ForkSandbox for the full semantics.
func (s *Sandbox) Fork(ctx context.Context, opts ...ForkOption) ([]ForkResult, error) {
	return s.client.ForkSandbox(ctx, s.ID, opts...)
}

// errorMessageFromBody extracts the "message" field from a JSON error body,
// returning "" when the body is absent or not the expected shape.
func errorMessageFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var e struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return ""
	}
	return e.Message
}
