package e2b

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SandboxConfig holds the configuration for creating a new sandbox.
type SandboxConfig struct {
	// Template is the sandbox template ID. Defaults to "base".
	Template string

	// Timeout is the sandbox lifetime in seconds. Defaults to 300.
	Timeout int

	// EnvVars are environment variables set in the sandbox.
	EnvVars map[string]string
}

// Sandbox represents a running E2B sandbox microVM.
type Sandbox struct {
	// ID is the unique identifier of the sandbox.
	ID string

	// Commands provides command execution within the sandbox.
	Commands *CommandService

	// Filesystem provides file read and write operations within the sandbox.
	Filesystem *FilesystemService

	accessToken string
	client      *Client
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

// SandboxLifecycle holds lifecycle configuration for a sandbox.
type SandboxLifecycle struct {
	AutoResume bool   `json:"autoResume"`
	OnTimeout  string `json:"onTimeout"` // "keep" or "kill"
}

// VolumeMount represents a mounted volume in the sandbox.
type VolumeMount struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// SandboxInfo holds details about a sandbox.
type SandboxInfo struct {
	ID           string           `json:"sandboxID"`
	Alias        string           `json:"alias,omitempty"`
	ClientID     string           `json:"clientID,omitempty"`
	Template     string           `json:"templateID"`
	State        string           `json:"state"`
	CPUCount     int              `json:"cpuCount"`
	MemoryMB     int              `json:"memoryMB"`
	DiskSizeMB   int              `json:"diskSizeMB"`
	StartedAt    string           `json:"startedAt"`
	EndAt        string           `json:"endAt,omitempty"`
	EnvdVersion  string           `json:"envdVersion,omitempty"`
	Lifecycle    SandboxLifecycle `json:"lifecycle,omitempty"`
	VolumeMounts []VolumeMount    `json:"volumeMounts,omitempty"`
}

// envdBaseURL returns the base URL of the sandbox environment daemon.
func (s *Sandbox) envdBaseURL() string {
	return fmt.Sprintf("https://%d-%s.%s", envdPort, s.ID, s.client.sandboxDomain)
}

// Close destroys the sandbox, freeing all associated resources.
func (s *Sandbox) Close() error {
	return s.CloseWithContext(context.Background())
}

// CloseWithContext destroys the sandbox using the provided context.
func (s *Sandbox) CloseWithContext(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.client.apiBaseURL+"/sandboxes/"+s.ID, nil)
	if err != nil {
		return fmt.Errorf("e2b: build delete request: %w", err)
	}
	req.Header.Set("X-API-Key", s.client.apiKey)

	resp, err := s.client.httpClient.Do(req)
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

// Info retrieves detailed information about the sandbox.
func (s *Sandbox) Info() (*SandboxInfo, error) {
	return s.InfoWithContext(context.Background())
}

// InfoWithContext retrieves detailed information about the sandbox using the provided context.
func (s *Sandbox) InfoWithContext(ctx context.Context) (*SandboxInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.client.apiBaseURL+"/sandboxes/"+s.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build info request: %w", err)
	}
	req.Header.Set("X-API-Key", s.client.apiKey)

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send info request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return nil, &SandboxNotFoundError{SandboxID: s.ID}
		}
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var info SandboxInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("e2b: decode info response: %w", err)
	}

	return &info, nil
}
