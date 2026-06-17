package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// ListTemplates
// ---------------------------------------------------------------------------

func TestListTemplatesSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/templates" {
			t.Errorf("path = %s, want /templates", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]TemplateDetail{
			{
				TemplateID:  "tmpl-1",
				BuildID:     "build-1",
				CPUCount:    2,
				MemoryMB:    1024,
				DiskSizeMB:  11361,
				Public:      false,
				Names:       []string{"team/my-template"},
				Aliases:     []string{"my-template"},
				CreatedAt:   "2026-05-12T19:28:55Z",
				UpdatedAt:   "2026-05-12T19:36:02Z",
				SpawnCount:  10,
				BuildCount:  4,
				EnvdVersion: "0.5.17",
				BuildStatus: "ready",
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

	templates, err := client.ListTemplates(context.Background())
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}

	if len(templates) != 1 {
		t.Fatalf("got %d templates, want 1", len(templates))
	}
	tmpl := templates[0]
	if tmpl.TemplateID != "tmpl-1" {
		t.Errorf("TemplateID = %q, want %q", tmpl.TemplateID, "tmpl-1")
	}
	if tmpl.BuildID != "build-1" {
		t.Errorf("BuildID = %q, want %q", tmpl.BuildID, "build-1")
	}
	if tmpl.CPUCount != 2 {
		t.Errorf("CPUCount = %d, want 2", tmpl.CPUCount)
	}
	if tmpl.MemoryMB != 1024 {
		t.Errorf("MemoryMB = %d, want 1024", tmpl.MemoryMB)
	}
	if tmpl.BuildStatus != "ready" {
		t.Errorf("BuildStatus = %q, want %q", tmpl.BuildStatus, "ready")
	}
	if tmpl.SpawnCount != 10 {
		t.Errorf("SpawnCount = %d, want 10", tmpl.SpawnCount)
	}
}

func TestListTemplatesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	templates, err := client.ListTemplates(context.Background())
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) != 0 {
		t.Errorf("got %d templates, want 0", len(templates))
	}
}

func TestListTemplatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"internal error"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ListTemplates(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

func TestListTemplatesCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.ListTemplates(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListTemplatesInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ListTemplates(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// CreateTemplate
// ---------------------------------------------------------------------------

func TestCreateTemplateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v3/templates" {
			t.Errorf("path = %s, want /v3/templates", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var body createTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Name != "my-template" {
			t.Errorf("name = %q, want %q", body.Name, "my-template")
		}
		if body.CPUCount != 2 {
			t.Errorf("cpuCount = %d, want 2", body.CPUCount)
		}
		if body.MemoryMB != 512 {
			t.Errorf("memoryMB = %d, want 512", body.MemoryMB)
		}

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(TemplateInfo{
			TemplateID: "tmpl-abc",
			BuildID:    "build-xyz",
			Public:     false,
			Names:      []string{"team/my-template"},
			Tags:       []string{"default"},
			Aliases:    []string{"my-template"},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	info, err := client.CreateTemplate(context.Background(), CreateTemplateConfig{
		Name:     "my-template",
		CPUCount: 2,
		MemoryMB: 512,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	if info.TemplateID != "tmpl-abc" {
		t.Errorf("TemplateID = %q, want %q", info.TemplateID, "tmpl-abc")
	}
	if info.BuildID != "build-xyz" {
		t.Errorf("BuildID = %q, want %q", info.BuildID, "build-xyz")
	}
	if len(info.Names) != 1 || info.Names[0] != "team/my-template" {
		t.Errorf("Names = %v, want [team/my-template]", info.Names)
	}
	if len(info.Tags) != 1 || info.Tags[0] != "default" {
		t.Errorf("Tags = %v, want [default]", info.Tags)
	}
}

func TestCreateTemplateWithTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if len(body.Tags) != 2 || body.Tags[0] != "v1" || body.Tags[1] != "latest" {
			t.Errorf("tags = %v, want [v1 latest]", body.Tags)
		}

		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(TemplateInfo{
			TemplateID: "tmpl-tags",
			BuildID:    "build-tags",
			Tags:       body.Tags,
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	info, err := client.CreateTemplate(context.Background(), CreateTemplateConfig{
		Name: "tagged-template",
		Tags: []string{"v1", "latest"},
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if len(info.Tags) != 2 {
		t.Errorf("Tags = %v, want 2 items", info.Tags)
	}
}

func TestCreateTemplateBadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"message":"Name is required"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.CreateTemplate(context.Background(), CreateTemplateConfig{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
}

func TestCreateTemplateServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"server error"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.CreateTemplate(context.Background(), CreateTemplateConfig{Name: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

func TestCreateTemplateCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.CreateTemplate(ctx, CreateTemplateConfig{Name: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetTemplate
// ---------------------------------------------------------------------------

func TestGetTemplateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/templates/tmpl-abc" {
			t.Errorf("path = %s, want /templates/tmpl-abc", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.Header().Set("X-Next-Token", "page2-token")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TemplateWithBuilds{
			TemplateID: "tmpl-abc",
			Public:     false,
			Names:      []string{"team/my-template"},
			CreatedAt:  "2026-06-13T15:44:48Z",
			UpdatedAt:  "2026-06-13T15:44:48Z",
			SpawnCount: 5,
			Builds: []TemplateBuild{
				{
					BuildID:   "build-1",
					Status:    "ready",
					CreatedAt: "2026-06-13T15:44:48Z",
					UpdatedAt: "2026-06-13T15:46:40Z",
					CPUCount:  2,
					MemoryMB:  512,
				},
			},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	result, err := client.GetTemplate(context.Background(), "tmpl-abc")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}

	if result.Template.TemplateID != "tmpl-abc" {
		t.Errorf("TemplateID = %q, want %q", result.Template.TemplateID, "tmpl-abc")
	}
	if result.Template.SpawnCount != 5 {
		t.Errorf("SpawnCount = %d, want 5", result.Template.SpawnCount)
	}
	if len(result.Template.Builds) != 1 {
		t.Fatalf("got %d builds, want 1", len(result.Template.Builds))
	}
	if result.Template.Builds[0].Status != "ready" {
		t.Errorf("build status = %q, want %q", result.Template.Builds[0].Status, "ready")
	}
	if result.NextToken != "page2-token" {
		t.Errorf("NextToken = %q, want %q", result.NextToken, "page2-token")
	}
}

func TestGetTemplateWithPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("limit"); got != "5" {
			t.Errorf("limit = %q, want %q", got, "5")
		}
		if got := q.Get("nextToken"); got != "tok-abc" {
			t.Errorf("nextToken = %q, want %q", got, "tok-abc")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(TemplateWithBuilds{
			TemplateID: "tmpl-abc",
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetTemplate(context.Background(), "tmpl-abc",
		WithTemplateBuildsLimit(5),
		WithTemplateBuildsNextToken("tok-abc"),
	)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
}

func TestGetTemplateNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"Template not-real not found"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetTemplate(context.Background(), "not-real")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var notFound *TemplateNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *TemplateNotFoundError, got %T: %v", err, err)
	}
	if notFound.TemplateID != "not-real" {
		t.Errorf("TemplateID = %q, want %q", notFound.TemplateID, "not-real")
	}
}

func TestGetTemplateServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"server error"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetTemplate(context.Background(), "tmpl-abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

func TestGetTemplateCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetTemplate(ctx, "tmpl-abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// DeleteTemplate
// ---------------------------------------------------------------------------

func TestDeleteTemplateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/templates/tmpl-del" {
			t.Errorf("path = %s, want /templates/tmpl-del", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.DeleteTemplate(context.Background(), "tmpl-del")
	if err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
}

func TestDeleteTemplateNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"template 'not-real' not found"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.DeleteTemplate(context.Background(), "not-real")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var notFound *TemplateNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *TemplateNotFoundError, got %T: %v", err, err)
	}
	if notFound.TemplateID != "not-real" {
		t.Errorf("TemplateID = %q, want %q", notFound.TemplateID, "not-real")
	}
}

func TestDeleteTemplateServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"server error"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.DeleteTemplate(context.Background(), "tmpl-abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

func TestDeleteTemplateCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = client.DeleteTemplate(ctx, "tmpl-abc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetTemplateBuildLogs
// ---------------------------------------------------------------------------

func TestGetTemplateBuildLogsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/templates/tmpl-abc/builds/build-xyz/logs" {
			t.Errorf("path = %s, want /templates/tmpl-abc/builds/build-xyz/logs", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(buildLogsResponse{
			Logs: []BuildLogEntry{
				{
					Timestamp: "2026-06-13T15:45:33Z",
					Message:   "Building template",
					Level:     "info",
				},
				{
					Timestamp: "2026-06-13T15:45:34Z",
					Message:   "FROM python:3.11-slim",
					Level:     "info",
					Step:      "base",
				},
			},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	logs, err := client.GetTemplateBuildLogs(context.Background(), "tmpl-abc", "build-xyz")
	if err != nil {
		t.Fatalf("GetTemplateBuildLogs: %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("got %d log entries, want 2", len(logs))
	}
	if logs[0].Level != "info" {
		t.Errorf("logs[0].Level = %q, want %q", logs[0].Level, "info")
	}
	if logs[0].Message != "Building template" {
		t.Errorf("logs[0].Message = %q, want %q", logs[0].Message, "Building template")
	}
	if logs[1].Step != "base" {
		t.Errorf("logs[1].Step = %q, want %q", logs[1].Step, "base")
	}
}

func TestGetTemplateBuildLogsWithOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("limit"); got != "10" {
			t.Errorf("limit = %q, want %q", got, "10")
		}
		if got := q.Get("direction"); got != "backward" {
			t.Errorf("direction = %q, want %q", got, "backward")
		}
		if got := q.Get("level"); got != "info" {
			t.Errorf("level = %q, want %q", got, "info")
		}
		if got := q.Get("source"); got != "persistent" {
			t.Errorf("source = %q, want %q", got, "persistent")
		}
		if got := q.Get("cursor"); got != "1000" {
			t.Errorf("cursor = %q, want %q", got, "1000")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(buildLogsResponse{Logs: []BuildLogEntry{}})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetTemplateBuildLogs(context.Background(), "tmpl-abc", "build-xyz",
		WithBuildLogLimit(10),
		WithBuildLogDirection("backward"),
		WithBuildLogLevel("info"),
		WithBuildLogSource("persistent"),
		WithBuildLogCursor(1000),
	)
	if err != nil {
		t.Fatalf("GetTemplateBuildLogs: %v", err)
	}
}

func TestGetTemplateBuildLogsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"message":"template not found"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetTemplateBuildLogs(context.Background(), "not-real", "build-xyz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var notFound *TemplateNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *TemplateNotFoundError, got %T: %v", err, err)
	}
}

func TestGetTemplateBuildLogsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"server error"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetTemplateBuildLogs(context.Background(), "tmpl-abc", "build-xyz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

func TestGetTemplateBuildLogsCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"logs":[]}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetTemplateBuildLogs(ctx, "tmpl-abc", "build-xyz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetTemplateBuildLogsInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetTemplateBuildLogs(context.Background(), "tmpl-abc", "build-xyz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// TemplateNotFoundError
// ---------------------------------------------------------------------------

func TestTemplateNotFoundErrorMessage(t *testing.T) {
	err := &TemplateNotFoundError{TemplateID: "tmpl-123"}
	want := "e2b: template not found: tmpl-123"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// CheckBuildFiles
// ---------------------------------------------------------------------------

func TestCheckBuildFilesPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/templates/tmpl-abc/files/abc123hash" {
			t.Errorf("path = %s, want /templates/tmpl-abc/files/abc123hash", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(BuildFileStatus{Present: true})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	status, err := client.CheckBuildFiles(context.Background(), "tmpl-abc", "abc123hash")
	if err != nil {
		t.Fatalf("CheckBuildFiles: %v", err)
	}
	if !status.Present {
		t.Error("Present = false, want true")
	}
	if status.URL != "" {
		t.Errorf("URL = %q, want empty", status.URL)
	}
}

func TestCheckBuildFilesNotPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(BuildFileStatus{
			Present: false,
			URL:     "https://storage.example.com/upload?token=xyz",
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	status, err := client.CheckBuildFiles(context.Background(), "tmpl-abc", "somehash")
	if err != nil {
		t.Fatalf("CheckBuildFiles: %v", err)
	}
	if status.Present {
		t.Error("Present = true, want false")
	}
	if status.URL != "https://storage.example.com/upload?token=xyz" {
		t.Errorf("URL = %q, want presigned URL", status.URL)
	}
}

func TestCheckBuildFilesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.CheckBuildFiles(context.Background(), "tmpl-missing", "somehash")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *TemplateNotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected TemplateNotFoundError, got %T: %v", err, err)
	}
	if nfe.TemplateID != "tmpl-missing" {
		t.Errorf("TemplateID = %q, want %q", nfe.TemplateID, "tmpl-missing")
	}
}

func TestCheckBuildFilesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid hash format"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.CheckBuildFiles(context.Background(), "tmpl-abc", "badhash")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
}

func TestCheckBuildFilesCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(BuildFileStatus{Present: true})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.CheckBuildFiles(ctx, "tmpl-abc", "somehash")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// UploadBuildFiles
// ---------------------------------------------------------------------------

func TestUploadBuildFilesSuccess(t *testing.T) {
	payload := []byte("fake-tar-gz-content")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !bytes.Equal(body, payload) {
			t.Errorf("body = %q, want %q", body, payload)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: "https://unused.example.com"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.UploadBuildFiles(context.Background(), srv.URL+"/upload", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("UploadBuildFiles: %v", err)
	}
}

func TestUploadBuildFilesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("expired presigned URL"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: "https://unused.example.com"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.UploadBuildFiles(context.Background(), srv.URL+"/upload", bytes.NewReader([]byte("data")))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusForbidden)
	}
}

func TestUploadBuildFilesNoAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Errorf("X-API-Key = %q, want empty (presigned URL should not have auth header)", got)
		}
		if got := r.Header.Get("Content-Type"); got != "" {
			t.Errorf("Content-Type = %q, want empty", got)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "secret-key", APIBaseURL: "https://unused.example.com"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.UploadBuildFiles(context.Background(), srv.URL+"/upload", bytes.NewReader([]byte("data")))
	if err != nil {
		t.Fatalf("UploadBuildFiles: %v", err)
	}
}

// ---------------------------------------------------------------------------
// StartTemplateBuild
// ---------------------------------------------------------------------------

func TestStartTemplateBuildSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v2/templates/tmpl-abc/builds/build-123" {
			t.Errorf("path = %s, want /v2/templates/tmpl-abc/builds/build-123", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		var body StartBuildConfig
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body.FromImage != "python:3.11-slim" {
			t.Errorf("FromImage = %q, want %q", body.FromImage, "python:3.11-slim")
		}
		if len(body.Steps) != 1 {
			t.Fatalf("len(Steps) = %d, want 1", len(body.Steps))
		}
		if body.Steps[0].Type != "run" {
			t.Errorf("Steps[0].Type = %q, want %q", body.Steps[0].Type, "run")
		}
		if len(body.Steps[0].Args) != 1 || body.Steps[0].Args[0] != "pip install requests" {
			t.Errorf("Steps[0].Args = %v, want [pip install requests]", body.Steps[0].Args)
		}
		if body.StartCmd != "echo hello" {
			t.Errorf("StartCmd = %q, want %q", body.StartCmd, "echo hello")
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.StartTemplateBuild(context.Background(), "tmpl-abc", "build-123", StartBuildConfig{
		FromImage: "python:3.11-slim",
		Steps: []TemplateStep{
			{Type: "run", Args: []string{"pip install requests"}},
		},
		StartCmd: "echo hello",
	})
	if err != nil {
		t.Fatalf("StartTemplateBuild: %v", err)
	}
}

func TestStartTemplateBuildWithForce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body StartBuildConfig
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if !body.Force {
			t.Error("Force = false, want true")
		}
		if len(body.Steps) != 1 || !body.Steps[0].Force {
			t.Error("Steps[0].Force = false, want true")
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.StartTemplateBuild(context.Background(), "tmpl-abc", "build-123", StartBuildConfig{
		FromImage: "ubuntu:22.04",
		Force:     true,
		Steps: []TemplateStep{
			{Type: "run", Args: []string{"apt-get update"}, Force: true},
		},
	})
	if err != nil {
		t.Fatalf("StartTemplateBuild: %v", err)
	}
}

func TestStartTemplateBuildAlreadyTriggered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"message":"build is not in waiting state"}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.StartTemplateBuild(context.Background(), "tmpl-abc", "build-123", StartBuildConfig{
		FromImage: "python:3.11",
		Steps:     []TemplateStep{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
}

func TestStartTemplateBuildServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.StartTemplateBuild(context.Background(), "tmpl-abc", "build-123", StartBuildConfig{
		FromImage: "python:3.11",
		Steps:     []TemplateStep{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusInternalServerError)
	}
}

func TestStartTemplateBuildCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = client.StartTemplateBuild(ctx, "tmpl-abc", "build-123", StartBuildConfig{
		FromImage: "python:3.11",
		Steps:     []TemplateStep{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetBuildStatus
// ---------------------------------------------------------------------------

func TestGetBuildStatusBuilding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/templates/tmpl-abc/builds/build-123/status" {
			t.Errorf("path = %s, want /templates/tmpl-abc/builds/build-123/status", r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want %q", got, "test-key")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BuildStatus{
			TemplateID: "tmpl-abc",
			BuildID:    "build-123",
			Status:     "building",
			Logs:       []string{"Step 1: pulling image", "Step 2: running commands"},
			LogEntries: []BuildLogEntry{
				{Timestamp: "2026-06-17T10:00:00Z", Message: "pulling image", Level: "info"},
				{Timestamp: "2026-06-17T10:00:01Z", Message: "running commands", Level: "info"},
			},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	status, err := client.GetBuildStatus(context.Background(), "tmpl-abc", "build-123")
	if err != nil {
		t.Fatalf("GetBuildStatus: %v", err)
	}
	if status.Status != "building" {
		t.Errorf("Status = %q, want %q", status.Status, "building")
	}
	if status.TemplateID != "tmpl-abc" {
		t.Errorf("TemplateID = %q, want %q", status.TemplateID, "tmpl-abc")
	}
	if len(status.Logs) != 2 {
		t.Errorf("len(Logs) = %d, want 2", len(status.Logs))
	}
	if len(status.LogEntries) != 2 {
		t.Errorf("len(LogEntries) = %d, want 2", len(status.LogEntries))
	}
	if status.Reason != nil {
		t.Errorf("Reason = %v, want nil", status.Reason)
	}
}

func TestGetBuildStatusReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BuildStatus{
			TemplateID: "tmpl-abc",
			BuildID:    "build-123",
			Status:     "ready",
			Logs:       []string{},
			LogEntries: []BuildLogEntry{},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	status, err := client.GetBuildStatus(context.Background(), "tmpl-abc", "build-123")
	if err != nil {
		t.Fatalf("GetBuildStatus: %v", err)
	}
	if status.Status != "ready" {
		t.Errorf("Status = %q, want %q", status.Status, "ready")
	}
}

func TestGetBuildStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BuildStatus{
			TemplateID: "tmpl-abc",
			BuildID:    "build-123",
			Status:     "error",
			Logs:       []string{"failed to pull image"},
			LogEntries: []BuildLogEntry{
				{Timestamp: "2026-06-17T10:00:00Z", Message: "failed to pull image", Level: "error"},
			},
			Reason: &BuildStatusReason{
				Message: "image not found",
				Step:    "pull",
				LogEntries: []BuildLogEntry{
					{Timestamp: "2026-06-17T10:00:00Z", Message: "failed to pull image", Level: "error"},
				},
			},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	status, err := client.GetBuildStatus(context.Background(), "tmpl-abc", "build-123")
	if err != nil {
		t.Fatalf("GetBuildStatus: %v", err)
	}
	if status.Status != "error" {
		t.Errorf("Status = %q, want %q", status.Status, "error")
	}
	if status.Reason == nil {
		t.Fatal("Reason is nil, want non-nil")
	}
	if status.Reason.Message != "image not found" {
		t.Errorf("Reason.Message = %q, want %q", status.Reason.Message, "image not found")
	}
	if status.Reason.Step != "pull" {
		t.Errorf("Reason.Step = %q, want %q", status.Reason.Step, "pull")
	}
}

func TestGetBuildStatusWithLogsOffset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("logsOffset"); got != "50" {
			t.Errorf("logsOffset = %q, want %q", got, "50")
		}
		if got := q.Get("limit"); got != "5" {
			t.Errorf("limit = %q, want %q", got, "5")
		}
		if got := q.Get("level"); got != "warn" {
			t.Errorf("level = %q, want %q", got, "warn")
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BuildStatus{
			TemplateID: "tmpl-abc",
			BuildID:    "build-123",
			Status:     "building",
			Logs:       []string{},
			LogEntries: []BuildLogEntry{},
		})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetBuildStatus(context.Background(), "tmpl-abc", "build-123",
		WithBuildStatusLogsOffset(50),
		WithBuildStatusLimit(5),
		WithBuildStatusLevel("warn"),
	)
	if err != nil {
		t.Fatalf("GetBuildStatus: %v", err)
	}
}

func TestGetBuildStatusNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.GetBuildStatus(context.Background(), "tmpl-missing", "build-123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var nfe *TemplateNotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected TemplateNotFoundError, got %T: %v", err, err)
	}
}

func TestGetBuildStatusCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(BuildStatus{Status: "building"})
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{APIKey: "test-key", APIBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.GetBuildStatus(ctx, "tmpl-abc", "build-123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
