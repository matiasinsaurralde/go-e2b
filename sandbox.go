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

type setTimeoutRequest struct {
	Timeout int `json:"timeout"`
}

// SetTimeout updates the sandbox lifetime timeout in seconds.
func (s *Sandbox) SetTimeout(timeoutSeconds int) error {
	return s.SetTimeoutWithContext(context.Background(), timeoutSeconds)
}

// SetTimeoutWithContext updates the sandbox lifetime timeout using the provided context.
func (s *Sandbox) SetTimeoutWithContext(ctx context.Context, timeoutSeconds int) error {
	body, err := json.Marshal(setTimeoutRequest{Timeout: timeoutSeconds})
	if err != nil {
		return fmt.Errorf("e2b: marshal timeout request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.client.apiBaseURL+"/sandboxes/"+s.ID+"/timeout", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("e2b: build timeout request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.client.apiKey)

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: send timeout request: %w", err)
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

// Pause pauses the sandbox, stopping billing while preserving state.
// A paused sandbox can be resumed with Resume.
func (s *Sandbox) Pause() error {
	return s.PauseWithContext(context.Background())
}

// PauseWithContext pauses the sandbox using the provided context.
func (s *Sandbox) PauseWithContext(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.client.apiBaseURL+"/sandboxes/"+s.ID+"/pause", nil)
	if err != nil {
		return fmt.Errorf("e2b: build pause request: %w", err)
	}
	req.Header.Set("X-API-Key", s.client.apiKey)

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: send pause request: %w", err)
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

type resumeRequest struct {
	Timeout int `json:"timeout"`
}

// Resume resumes a paused sandbox with the given lifetime timeout in seconds.
func (s *Sandbox) Resume(timeoutSeconds int) error {
	return s.ResumeWithContext(context.Background(), timeoutSeconds)
}

// ResumeWithContext resumes a paused sandbox using the provided context.
func (s *Sandbox) ResumeWithContext(ctx context.Context, timeoutSeconds int) error {
	body, err := json.Marshal(resumeRequest{Timeout: timeoutSeconds})
	if err != nil {
		return fmt.Errorf("e2b: marshal resume request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.client.apiBaseURL+"/sandboxes/"+s.ID+"/resume", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("e2b: build resume request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.client.apiKey)

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: send resume request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return &SandboxNotFoundError{SandboxID: s.ID}
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}
}

// SandboxMetric holds a single resource usage snapshot for a sandbox.
type SandboxMetric struct {
	CPUCount      int     `json:"cpuCount"`
	CPUUsedPct    float64 `json:"cpuUsedPct"`
	MemTotal      int64   `json:"memTotal"`
	MemUsed       int64   `json:"memUsed"`
	MemCache      int64   `json:"memCache"`
	DiskTotal     int64   `json:"diskTotal"`
	DiskUsed      int64   `json:"diskUsed"`
	Timestamp     string  `json:"timestamp"`
	TimestampUnix int64   `json:"timestampUnix"`
}

// Metrics retrieves resource usage metrics for the sandbox.
func (s *Sandbox) Metrics() ([]SandboxMetric, error) {
	return s.MetricsWithContext(context.Background())
}

// MetricsWithContext retrieves resource usage metrics using the provided context.
func (s *Sandbox) MetricsWithContext(ctx context.Context) ([]SandboxMetric, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.client.apiBaseURL+"/sandboxes/"+s.ID+"/metrics", nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build metrics request: %w", err)
	}
	req.Header.Set("X-API-Key", s.client.apiKey)

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send metrics request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var metrics []SandboxMetric
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("e2b: decode metrics response: %w", err)
	}

	return metrics, nil
}

// SandboxLogEntry holds a single structured log entry from a sandbox.
type SandboxLogEntry struct {
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Timestamp string            `json:"timestamp"`
	Fields    map[string]string `json:"fields"`
}

type logsResponse struct {
	Logs []SandboxLogEntry `json:"logs"`
}

type logsParams struct {
	limit     int
	direction string
	level     string
	search    string
}

// LogsOption configures a Logs request.
type LogsOption func(*logsParams)

// WithLimit sets the maximum number of log entries to return (default 1000).
func WithLimit(n int) LogsOption {
	return func(p *logsParams) { p.limit = n }
}

// WithDirection sets the sort order: "forward" (oldest first) or "backward" (newest first, default).
func WithDirection(d string) LogsOption {
	return func(p *logsParams) { p.direction = d }
}

// WithLevel filters logs by level: "debug", "info", "warn", or "error".
func WithLevel(l string) LogsOption {
	return func(p *logsParams) { p.level = l }
}

// WithSearch filters logs by a text search on the message field.
func WithSearch(q string) LogsOption {
	return func(p *logsParams) { p.search = q }
}

// Logs retrieves structured log entries for the sandbox.
func (s *Sandbox) Logs(opts ...LogsOption) ([]SandboxLogEntry, error) {
	return s.LogsWithContext(context.Background(), opts...)
}

// LogsWithContext retrieves structured log entries using the provided context.
func (s *Sandbox) LogsWithContext(ctx context.Context, opts ...LogsOption) ([]SandboxLogEntry, error) {
	var p logsParams
	for _, o := range opts {
		o(&p)
	}

	u := s.client.apiBaseURL + "/v2/sandboxes/" + s.ID + "/logs"
	sep := '?'
	if p.limit > 0 {
		u += string(sep) + "limit=" + fmt.Sprintf("%d", p.limit)
		sep = '&'
	}
	if p.direction != "" {
		u += string(sep) + "direction=" + p.direction
		sep = '&'
	}
	if p.level != "" {
		u += string(sep) + "level=" + p.level
		sep = '&'
	}
	if p.search != "" {
		u += string(sep) + "search=" + p.search
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build logs request: %w", err)
	}
	req.Header.Set("X-API-Key", s.client.apiKey)

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send logs request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var lr logsResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, fmt.Errorf("e2b: decode logs response: %w", err)
	}

	return lr.Logs, nil
}

// SnapshotInfo holds information about a sandbox snapshot.
type SnapshotInfo struct {
	Names      []string `json:"names"`
	SnapshotID string   `json:"snapshotID"`
}

type createSnapshotRequest struct {
	Name string `json:"name,omitempty"`
}

// CreateSnapshot creates a snapshot of the sandbox's current state.
// An optional name can be provided; if omitted, the API generates an ID.
func (s *Sandbox) CreateSnapshot(ctx context.Context, name ...string) (*SnapshotInfo, error) {
	var reqBody createSnapshotRequest
	if len(name) > 0 {
		reqBody.Name = name[0]
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal snapshot request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.client.apiBaseURL+"/sandboxes/"+s.ID+"/snapshots", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: build snapshot request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.client.apiKey)

	resp, err := s.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send snapshot request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return nil, &SandboxNotFoundError{SandboxID: s.ID}
		}
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var info SnapshotInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("e2b: decode snapshot response: %w", err)
	}

	return &info, nil
}
