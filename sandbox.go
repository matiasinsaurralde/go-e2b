package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SandboxConfig holds the configuration for creating a new sandbox.
type SandboxConfig struct {
	// APIKey is the E2B API key. If empty, the E2B_API_KEY environment variable is used.
	APIKey string

	// Template is the sandbox template ID. Defaults to "base".
	Template string

	// Timeout is the sandbox lifetime in seconds. Defaults to 300.
	Timeout int

	// EnvVars are environment variables set in the sandbox.
	EnvVars map[string]string

	// APIBaseURL overrides the default API base URL.
	// If empty, the E2B_API_URL environment variable or the default URL is used.
	APIBaseURL string

	// SandboxDomain overrides the default sandbox domain.
	// If empty, the E2B_SANDBOX_URL environment variable or the default domain is used.
	SandboxDomain string

	// HTTPClient is the HTTP client used for API requests.
	// If nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// Sandbox represents a running E2B sandbox microVM.
type Sandbox struct {
	// ID is the unique identifier of the sandbox.
	ID string

	// Commands provides command execution within the sandbox.
	Commands *CommandService

	accessToken   string
	apiKey        string
	apiBaseURL    string
	sandboxDomain string
	httpClient    *http.Client
}

type createRequest struct {
	TemplateID string            `json:"templateID"`
	Timeout    int               `json:"timeout"`
	EnvVars    map[string]string `json:"envVars,omitempty"`
}

type createResponse struct {
	SandboxID       string `json:"sandboxID"`
	EnvdAccessToken string `json:"envdAccessToken"`
}

// NewSandbox creates a new E2B sandbox microVM with the given configuration.
// The caller should call Close when the sandbox is no longer needed.
func NewSandbox(cfg SandboxConfig) (*Sandbox, error) {
	return NewSandboxWithContext(context.Background(), cfg)
}

// NewSandboxWithContext creates a new E2B sandbox microVM with the given
// configuration and context for cancellation and deadline control.
func NewSandboxWithContext(ctx context.Context, cfg SandboxConfig) (*Sandbox, error) {
	apiKey := resolveAPIKey(cfg.APIKey)
	if apiKey == "" {
		return nil, &Error{Message: "API key is required: set SandboxConfig.APIKey or the E2B_API_KEY environment variable"}
	}

	template := cfg.Template
	if template == "" {
		template = DefaultTemplate
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	apiBaseURL := resolveAPIBaseURL(cfg.APIBaseURL)
	sandboxDomain := resolveSandboxDomain(cfg.SandboxDomain)

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	body, err := json.Marshal(createRequest{
		TemplateID: template,
		Timeout:    timeout,
		EnvVars:    cfg.EnvVars,
	})
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal create request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBaseURL+"/sandboxes", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: build create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)

	resp, err := httpClient.Do(req)
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
		ID:            cr.SandboxID,
		accessToken:   cr.EnvdAccessToken,
		apiKey:        apiKey,
		apiBaseURL:    apiBaseURL,
		sandboxDomain: sandboxDomain,
		httpClient:    httpClient,
	}
	sbx.Commands = newCommandService(sbx)
	return sbx, nil
}

// envdBaseURL returns the base URL of the sandbox environment daemon.
func (s *Sandbox) envdBaseURL() string {
	return fmt.Sprintf("https://%d-%s.%s", envdPort, s.ID, s.sandboxDomain)
}

// Close destroys the sandbox, freeing all associated resources.
func (s *Sandbox) Close() error {
	return s.CloseWithContext(context.Background())
}

// CloseWithContext destroys the sandbox using the provided context.
func (s *Sandbox) CloseWithContext(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.apiBaseURL+"/sandboxes/"+s.ID, nil)
	if err != nil {
		return fmt.Errorf("e2b: build delete request: %w", err)
	}
	req.Header.Set("X-API-Key", s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: send delete request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusNotFound:
		return &SandboxNotFoundError{SandboxID: s.ID}
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}
}
