package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
		AutoPause:           cfg.AutoPause,
		AutoPauseMemory:     cfg.AutoPauseMemory,
		AutoResume:          cfg.AutoResume,
		Metadata:            cfg.Metadata,
		MCP:                 cfg.MCP,
		VolumeMounts:        cfg.VolumeMounts,
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
	sbx.Pty = newPtyService(sbx)
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

	baseURL, err := url.Parse(c.apiBaseURL + "/snapshots")
	if err != nil {
		return nil, fmt.Errorf("e2b: build list snapshots URL: %w", err)
	}
	q := baseURL.Query()
	if p.sandboxID != "" {
		q.Set("sandboxID", p.sandboxID)
	}
	if p.limit > 0 {
		q.Set("limit", strconv.Itoa(p.limit))
	}
	if p.nextToken != "" {
		q.Set("nextToken", p.nextToken)
	}
	baseURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
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

// connectRequest is the request body for the connect endpoint.
type connectRequest struct {
	Timeout int `json:"timeout"`
}

// Connect attaches to an existing sandbox by ID. If the sandbox is paused,
// it will be automatically resumed. The timeout sets the new lifetime in
// seconds from the current time.
//
// This is the primary mechanism for reconnecting to sandboxes from different
// execution contexts or recovering paused sandboxes.
func (c *Client) Connect(ctx context.Context, sandboxID string, timeoutSeconds int) (*Sandbox, error) {
	body, err := json.Marshal(connectRequest{Timeout: timeoutSeconds})
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal connect request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/sandboxes/"+sandboxID+"/connect", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: build connect request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send connect request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// OK = sandbox was already running, Created = resumed from paused
	default:
		respBody, _ := io.ReadAll(resp.Body)
		// A missing sandbox surfaces as 404; a malformed sandbox ID surfaces
		// as 400 with an "invalid sandbox ID" message. Map both to the typed
		// SandboxNotFoundError so callers can handle "no such sandbox"
		// uniformly, while leaving other 400s (e.g. bad timeout) as generic.
		if resp.StatusCode == http.StatusNotFound ||
			(resp.StatusCode == http.StatusBadRequest &&
				strings.Contains(strings.ToLower(string(respBody)), "invalid sandbox id")) {
			return nil, &SandboxNotFoundError{SandboxID: sandboxID}
		}
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var cr createResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, fmt.Errorf("e2b: decode connect response: %w", err)
	}

	sbx := &Sandbox{
		ID:                 cr.SandboxID,
		TrafficAccessToken: cr.TrafficAccessToken,
		accessToken:        cr.EnvdAccessToken,
		client:             c,
	}
	sbx.Commands = newCommandService(sbx)
	sbx.Pty = newPtyService(sbx)
	sbx.Filesystem = newFilesystemService(sbx)
	return sbx, nil
}

// listSandboxesV2Params holds query parameters for ListSandboxesV2.
type listSandboxesV2Params struct {
	state     []string
	metadata  map[string]string
	limit     int
	nextToken string
}

// ListSandboxesV2Option configures a ListSandboxesV2 request.
type ListSandboxesV2Option func(*listSandboxesV2Params)

// WithSandboxState filters sandboxes by one or more states (e.g. "running", "paused").
// If not specified, both running and paused sandboxes are returned.
func WithSandboxState(states ...string) ListSandboxesV2Option {
	return func(p *listSandboxesV2Params) { p.state = states }
}

// WithSandboxMetadata filters sandboxes by metadata key=value pairs.
func WithSandboxMetadata(metadata map[string]string) ListSandboxesV2Option {
	return func(p *listSandboxesV2Params) { p.metadata = metadata }
}

// WithSandboxLimit sets the maximum number of sandboxes to return per page (default 100).
func WithSandboxLimit(n int) ListSandboxesV2Option {
	return func(p *listSandboxesV2Params) { p.limit = n }
}

// WithSandboxNextToken sets the pagination token from a previous ListSandboxesV2Result.
func WithSandboxNextToken(token string) ListSandboxesV2Option {
	return func(p *listSandboxesV2Params) { p.nextToken = token }
}

// ListSandboxesV2Result holds the result of a ListSandboxesV2 call, including pagination.
type ListSandboxesV2Result struct {
	Sandboxes []SandboxInfo
	NextToken string
}

// ListSandboxesV2 returns all sandboxes (running and paused) for this client's API key
// using the v2 endpoint. It supports filtering by state and metadata, plus cursor-based
// pagination via limit and nextToken.
//
// Example:
//
//	// List all running and paused sandboxes.
//	result, err := client.ListSandboxesV2(ctx)
//
//	// List only paused sandboxes.
//	result, err := client.ListSandboxesV2(ctx, WithSandboxState("paused"))
//
//	// Filter by metadata.
//	result, err := client.ListSandboxesV2(ctx, WithSandboxMetadata(map[string]string{"env": "dev"}))
//
//	// Pagination.
//	result, err := client.ListSandboxesV2(ctx, WithSandboxLimit(10))
//	for result.NextToken != "" {
//		next, err := client.ListSandboxesV2(ctx, WithSandboxNextToken(result.NextToken))
//		// ...
//	}
func (c *Client) ListSandboxesV2(ctx context.Context, opts ...ListSandboxesV2Option) (*ListSandboxesV2Result, error) {
	var p listSandboxesV2Params
	for _, o := range opts {
		o(&p)
	}

	baseURL, err := url.Parse(c.apiBaseURL + "/v2/sandboxes")
	if err != nil {
		return nil, fmt.Errorf("e2b: build list v2 URL: %w", err)
	}
	q := baseURL.Query()

	// Append state filters.
	for _, s := range p.state {
		q.Add("state", s)
	}

	// Append metadata filters (each metadata pair is key=value).
	for k, v := range p.metadata {
		q.Add("metadata", k+"="+v)
	}

	if p.limit > 0 {
		q.Set("limit", strconv.Itoa(p.limit))
	}
	if p.nextToken != "" {
		q.Set("nextToken", p.nextToken)
	}

	baseURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build list v2 request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send list v2 request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var sandboxes []SandboxInfo
	if err := json.NewDecoder(resp.Body).Decode(&sandboxes); err != nil {
		return nil, fmt.Errorf("e2b: decode list v2 response: %w", err)
	}

	return &ListSandboxesV2Result{
		Sandboxes: sandboxes,
		NextToken: resp.Header.Get("X-Next-Token"),
	}, nil
}
