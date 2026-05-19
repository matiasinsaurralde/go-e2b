package e2b

import (
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
