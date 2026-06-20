package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ClientConfig configures a new Client.
type ClientConfig struct {
	// APIKey is the E2B API key. If empty, the E2B_API_KEY environment variable is used.
	APIKey string

	// APIBaseURL overrides the default API base URL.
	APIBaseURL string

	// SandboxDomain overrides the default sandbox domain.
	SandboxDomain string

	// HTTPClient is the HTTP client used for API requests.
	// If nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// Client is the E2B API client. Create one with NewClient and reuse it
// for all operations — it holds shared configuration like the API key.
type Client struct {
	apiKey        string
	apiBaseURL    string
	sandboxDomain string
	httpClient    *http.Client
}

// NewClient creates a new E2B client. If no config is provided,
// the API key is read from the E2B_API_KEY environment variable.
func NewClient(cfgs ...ClientConfig) (*Client, error) {
	var cfg ClientConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	apiKey := resolveAPIKey(cfg.APIKey)
	if apiKey == "" {
		return nil, &Error{Message: "API key is required: set ClientConfig.APIKey or the E2B_API_KEY environment variable"}
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		apiKey:        apiKey,
		apiBaseURL:    resolveAPIBaseURL(cfg.APIBaseURL),
		sandboxDomain: resolveSandboxDomain(cfg.SandboxDomain),
		httpClient:    httpClient,
	}, nil
}

// NewSandbox creates a new E2B sandbox microVM.
// The caller should call Close when the sandbox is no longer needed.
func (c *Client) NewSandbox(ctx context.Context, cfgs ...SandboxConfig) (*Sandbox, error) {
	var cfg SandboxConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	template := cfg.Template
	if template == "" {
		template = DefaultTemplate
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	body, err := json.Marshal(createRequest{
		TemplateID:          template,
		Timeout:             timeout,
		EnvVars:             cfg.EnvVars,
		Secure:              cfg.Secure,
		AllowInternetAccess: cfg.AllowInternetAccess,
		Network:             cfg.Network,
	})
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal create request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/sandboxes", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: build create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send create request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return nil, &Error{StatusCode: resp.StatusCode, Message: fmt.Sprintf("template not found: %s", template)}
		}
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var cr createResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("e2b: decode create response: %w", err)
	}

	sbx := &Sandbox{
		ID:                 cr.SandboxID,
		TrafficAccessToken: cr.TrafficAccessToken,
		accessToken:        cr.EnvdAccessToken,
		client:             c,
	}
	sbx.Commands = newCommandService(sbx)
	sbx.Filesystem = newFilesystemService(sbx)
	return sbx, nil
}

// ListSandboxes returns all running sandboxes for this client's API key.
func (c *Client) ListSandboxes(ctx context.Context) ([]SandboxInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+"/sandboxes", nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build list request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send list request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var items []SandboxInfo
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("e2b: decode list response: %w", err)
	}

	return items, nil
}

type listSnapshotsParams struct {
	sandboxID string
	limit     int
	nextToken string
}

// ListSnapshotsOption configures a ListSnapshots request.
type ListSnapshotsOption func(*listSnapshotsParams)

// WithSnapshotSandboxID filters snapshots by the source sandbox ID.
func WithSnapshotSandboxID(id string) ListSnapshotsOption {
	return func(p *listSnapshotsParams) { p.sandboxID = id }
}

// WithSnapshotLimit sets the maximum number of snapshots to return (default 100).
func WithSnapshotLimit(n int) ListSnapshotsOption {
	return func(p *listSnapshotsParams) { p.limit = n }
}

// WithSnapshotNextToken sets the pagination token from a previous ListSnapshotsResult.
func WithSnapshotNextToken(token string) ListSnapshotsOption {
	return func(p *listSnapshotsParams) { p.nextToken = token }
}

// ListSnapshotsResult holds the result of a ListSnapshots call, including pagination.
type ListSnapshotsResult struct {
	Snapshots []SnapshotInfo
	NextToken string
}

// ListSnapshots returns snapshots for this client's API key.
func (c *Client) ListSnapshots(ctx context.Context, opts ...ListSnapshotsOption) (*ListSnapshotsResult, error) {
	var p listSnapshotsParams
	for _, o := range opts {
		o(&p)
	}

	u := c.apiBaseURL + "/snapshots"
	sep := '?'
	if p.sandboxID != "" {
		u += string(sep) + "sandboxID=" + p.sandboxID
		sep = '&'
	}
	if p.limit > 0 {
		u += string(sep) + "limit=" + fmt.Sprintf("%d", p.limit)
		sep = '&'
	}
	if p.nextToken != "" {
		u += string(sep) + "nextToken=" + p.nextToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build list snapshots request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send list snapshots request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var snapshots []SnapshotInfo
	if err := json.NewDecoder(resp.Body).Decode(&snapshots); err != nil {
		return nil, fmt.Errorf("e2b: decode list snapshots response: %w", err)
	}

	return &ListSnapshotsResult{
		Snapshots: snapshots,
		NextToken: resp.Header.Get("X-Next-Token"),
	}, nil
}
