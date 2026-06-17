package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CreateTemplateConfig holds the configuration for creating a new template.
type CreateTemplateConfig struct {
	// Name is the template name (required). Supports tag syntax with colon separator (e.g. "my-template:v1").
	Name string

	// Tags are labels to assign to the template build. Defaults to ["default"] server-side.
	Tags []string

	// CPUCount is the number of CPU cores for sandbox instances (min 1).
	CPUCount int

	// MemoryMB is the memory in MiB for sandbox instances (min 128).
	MemoryMB int
}

type createTemplateRequest struct {
	Name     string   `json:"name"`
	Tags     []string `json:"tags,omitempty"`
	CPUCount int      `json:"cpuCount,omitempty"`
	MemoryMB int      `json:"memoryMB,omitempty"`
}

// TemplateInfo holds the response from creating a template.
type TemplateInfo struct {
	TemplateID string   `json:"templateID"`
	BuildID    string   `json:"buildID"`
	Public     bool     `json:"public"`
	Names      []string `json:"names"`
	Tags       []string `json:"tags"`
	Aliases    []string `json:"aliases"`
}

// TemplateDetail holds full details about a template, as returned by ListTemplates.
type TemplateDetail struct {
	TemplateID    string   `json:"templateID"`
	BuildID       string   `json:"buildID"`
	CPUCount      int      `json:"cpuCount"`
	MemoryMB      int      `json:"memoryMB"`
	DiskSizeMB    int      `json:"diskSizeMB"`
	Public        bool     `json:"public"`
	Names         []string `json:"names"`
	Aliases       []string `json:"aliases"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
	LastSpawnedAt *string  `json:"lastSpawnedAt"`
	SpawnCount    int      `json:"spawnCount"`
	BuildCount    int      `json:"buildCount"`
	EnvdVersion   string   `json:"envdVersion"`
	BuildStatus   string   `json:"buildStatus"`
}

// TemplateBuild holds information about a single build of a template.
type TemplateBuild struct {
	BuildID     string `json:"buildID"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	FinishedAt  string `json:"finishedAt,omitempty"`
	CPUCount    int    `json:"cpuCount"`
	MemoryMB    int    `json:"memoryMB"`
	DiskSizeMB  int    `json:"diskSizeMB,omitempty"`
	EnvdVersion string `json:"envdVersion,omitempty"`
}

// TemplateWithBuilds holds template details including its builds, as returned by GetTemplate.
type TemplateWithBuilds struct {
	TemplateID    string          `json:"templateID"`
	Public        bool            `json:"public"`
	Names         []string        `json:"names"`
	Aliases       []string        `json:"aliases"`
	CreatedAt     string          `json:"createdAt"`
	UpdatedAt     string          `json:"updatedAt"`
	LastSpawnedAt *string         `json:"lastSpawnedAt"`
	SpawnCount    int             `json:"spawnCount"`
	Builds        []TemplateBuild `json:"builds"`
}

// GetTemplateOption configures a GetTemplate request.
type GetTemplateOption func(*getTemplateParams)

type getTemplateParams struct {
	limit     int
	nextToken string
}

// WithTemplateBuildsLimit sets the maximum number of builds to return (1-100, default 100).
func WithTemplateBuildsLimit(n int) GetTemplateOption {
	return func(p *getTemplateParams) { p.limit = n }
}

// WithTemplateBuildsNextToken sets the pagination token from a previous GetTemplateResult.
func WithTemplateBuildsNextToken(token string) GetTemplateOption {
	return func(p *getTemplateParams) { p.nextToken = token }
}

// GetTemplateResult holds the result of a GetTemplate call, including pagination.
type GetTemplateResult struct {
	Template  TemplateWithBuilds
	NextToken string
}

// BuildLogEntry holds a single structured log entry from a template build.
type BuildLogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Level     string `json:"level"`
	Step      string `json:"step,omitempty"`
}

type buildLogsResponse struct {
	Logs []BuildLogEntry `json:"logs"`
}

// BuildLogOption configures a GetTemplateBuildLogs request.
type BuildLogOption func(*buildLogParams)

type buildLogParams struct {
	cursor    int
	limit     int
	direction string
	level     string
	source    string
}

// WithBuildLogCursor sets the starting timestamp in milliseconds.
func WithBuildLogCursor(ms int) BuildLogOption {
	return func(p *buildLogParams) { p.cursor = ms }
}

// WithBuildLogLimit sets the maximum number of log entries (0-100, default 100).
func WithBuildLogLimit(n int) BuildLogOption {
	return func(p *buildLogParams) { p.limit = n }
}

// WithBuildLogDirection sets the log order: "forward" or "backward".
func WithBuildLogDirection(d string) BuildLogOption {
	return func(p *buildLogParams) { p.direction = d }
}

// WithBuildLogLevel filters logs by severity level.
func WithBuildLogLevel(level string) BuildLogOption {
	return func(p *buildLogParams) { p.level = level }
}

// WithBuildLogSource filters logs by source: "temporary" or "persistent".
func WithBuildLogSource(source string) BuildLogOption {
	return func(p *buildLogParams) { p.source = source }
}

// BuildFileStatus holds the response from checking if build files are cached.
type BuildFileStatus struct {
	// Present indicates whether the files are already cached server-side.
	Present bool `json:"present"`

	// URL is the presigned upload URL. Only populated when Present is false.
	URL string `json:"url,omitempty"`
}

// CheckBuildFiles checks whether a file bundle (identified by its SHA-256 hash)
// is already cached on the server. If not cached, the response includes a
// presigned URL for uploading the tar archive.
func (c *Client) CheckBuildFiles(ctx context.Context, templateID, hash string) (*BuildFileStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+"/templates/"+templateID+"/files/"+hash, nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build check files request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send check files request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		if resp.StatusCode == http.StatusNotFound {
			return nil, &TemplateNotFoundError{TemplateID: templateID}
		}
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var status BuildFileStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("e2b: decode check files response: %w", err)
	}

	return &status, nil
}

// UploadBuildFiles uploads a gzipped tar archive to the presigned URL returned
// by CheckBuildFiles. The data parameter should be a *bytes.NewReader (fully
// buffered) so that Content-Length is set — presigned URLs reject chunked encoding.
// No authentication headers are sent; the presigned URL contains credentials.
func (c *Client) UploadBuildFiles(ctx context.Context, url string, data io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, data)
	if err != nil {
		return fmt.Errorf("e2b: build upload files request: %w", err)
	}
	// No X-API-Key or Content-Type headers — presigned URL has embedded credentials.

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: send upload files request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	return nil
}

// ListTemplates returns all templates for this client's API key.
func (c *Client) ListTemplates(ctx context.Context) ([]TemplateDetail, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+"/templates", nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build list templates request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send list templates request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var items []TemplateDetail
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("e2b: decode list templates response: %w", err)
	}

	return items, nil
}

// CreateTemplate registers a new template and allocates a build ID.
// This is phase 1 of the build workflow — the build has not started yet.
func (c *Client) CreateTemplate(ctx context.Context, cfg CreateTemplateConfig) (*TemplateInfo, error) {
	body, err := json.Marshal(createTemplateRequest(cfg))
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal create template request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+"/v3/templates", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("e2b: build create template request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send create template request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var info TemplateInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("e2b: decode create template response: %w", err)
	}

	return &info, nil
}

// GetTemplate retrieves details about a template, including its builds.
func (c *Client) GetTemplate(ctx context.Context, templateID string, opts ...GetTemplateOption) (*GetTemplateResult, error) {
	var p getTemplateParams
	for _, o := range opts {
		o(&p)
	}

	u := c.apiBaseURL + "/templates/" + templateID
	sep := '?'
	if p.limit > 0 {
		u += string(sep) + "limit=" + fmt.Sprintf("%d", p.limit)
		sep = '&'
	}
	if p.nextToken != "" {
		u += string(sep) + "nextToken=" + p.nextToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build get template request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send get template request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, &TemplateNotFoundError{TemplateID: templateID}
		}
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var tmpl TemplateWithBuilds
	if err := json.NewDecoder(resp.Body).Decode(&tmpl); err != nil {
		return nil, fmt.Errorf("e2b: decode get template response: %w", err)
	}

	return &GetTemplateResult{
		Template:  tmpl,
		NextToken: resp.Header.Get("X-Next-Token"),
	}, nil
}

// DeleteTemplate deletes a template by ID.
func (c *Client) DeleteTemplate(ctx context.Context, templateID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.apiBaseURL+"/templates/"+templateID, nil)
	if err != nil {
		return fmt.Errorf("e2b: build delete template request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("e2b: send delete template request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return &TemplateNotFoundError{TemplateID: templateID}
	default:
		respBody, _ := io.ReadAll(resp.Body)
		return &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}
}

// GetTemplateBuildLogs retrieves structured log entries for a template build.
func (c *Client) GetTemplateBuildLogs(ctx context.Context, templateID, buildID string, opts ...BuildLogOption) ([]BuildLogEntry, error) {
	var p buildLogParams
	for _, o := range opts {
		o(&p)
	}

	u := c.apiBaseURL + "/templates/" + templateID + "/builds/" + buildID + "/logs"
	sep := '?'
	if p.cursor > 0 {
		u += string(sep) + "cursor=" + fmt.Sprintf("%d", p.cursor)
		sep = '&'
	}
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
	if p.source != "" {
		u += string(sep) + "source=" + p.source
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build get build logs request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send get build logs request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, &TemplateNotFoundError{TemplateID: templateID}
		}
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var lr buildLogsResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, fmt.Errorf("e2b: decode build logs response: %w", err)
	}

	return lr.Logs, nil
}
