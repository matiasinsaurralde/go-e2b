//go:build integration

package e2b

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"
)

// These tests hit the real E2B API. Run with:
//
//	go test -tags=integration -v -run TestIntegration ./...
//
// Requires E2B_API_KEY in the environment or .env file.

func integrationClient(t *testing.T) *Client {
	t.Helper()

	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		t.Skip("E2B_API_KEY not set, skipping integration test")
	}

	client, err := NewClient(ClientConfig{APIKey: apiKey})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestIntegrationCheckBuildFiles(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	// First, list templates to get a valid template ID.
	templates, err := client.ListTemplates(ctx)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) == 0 {
		t.Skip("no templates available for integration test")
	}

	templateID := templates[0].TemplateID
	t.Logf("using template %q (%v)", templateID, templates[0].Names)

	// Use a fake hash — this should return present=false with an upload URL.
	fakeHash := fmt.Sprintf("%x", sha256.Sum256([]byte("integration-test-fake-content")))
	t.Logf("checking hash %s", fakeHash)

	status, err := client.CheckBuildFiles(ctx, templateID, fakeHash)
	if err != nil {
		t.Fatalf("CheckBuildFiles: %v", err)
	}

	t.Logf("present=%v url=%q", status.Present, status.URL)

	// A never-before-seen hash should not be cached.
	if status.Present {
		t.Log("hash was already present (unexpected for fake content, but not an error)")
	} else {
		if status.URL == "" {
			t.Error("Present=false but URL is empty — expected a presigned upload URL")
		}
	}
}

func TestIntegrationCheckBuildFilesNotFound(t *testing.T) {
	client := integrationClient(t)

	_, err := client.CheckBuildFiles(context.Background(), "nonexistent-template-id", "fakehash")
	if err == nil {
		t.Fatal("expected error for nonexistent template, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestIntegrationStartTemplateBuild(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	// Phase 1: Create a fresh template to get a buildID.
	info, err := client.CreateTemplate(ctx, CreateTemplateConfig{
		Name: "go-sdk-integration-start-build",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	t.Logf("created template %s, build %s", info.TemplateID, info.BuildID)

	// Clean up the template after the test.
	t.Cleanup(func() {
		if err := client.DeleteTemplate(context.Background(), info.TemplateID); err != nil {
			t.Logf("cleanup DeleteTemplate: %v", err)
		} else {
			t.Logf("cleaned up template %s", info.TemplateID)
		}
	})

	// Phase 2: Start the build with a simple config.
	err = client.StartTemplateBuild(ctx, info.TemplateID, info.BuildID, StartBuildConfig{
		FromImage: "python:3.11-slim",
		Steps: []TemplateStep{
			{Type: "run", Args: []string{"echo hello from go-sdk"}},
		},
		StartCmd: "echo started",
	})
	if err != nil {
		t.Fatalf("StartTemplateBuild: %v", err)
	}
	t.Log("StartTemplateBuild succeeded (202 Accepted)")

	// Phase 3: Calling again with the same buildID should fail (build is not in waiting state).
	err = client.StartTemplateBuild(ctx, info.TemplateID, info.BuildID, StartBuildConfig{
		FromImage: "python:3.11-slim",
		Steps:     []TemplateStep{},
	})
	if err == nil {
		t.Fatal("expected error on second StartTemplateBuild call, got nil")
	}
	t.Logf("second call correctly failed: %v", err)
}

func TestIntegrationGetBuildStatus(t *testing.T) {
	client := integrationClient(t)
	ctx := context.Background()

	// Phase 1: Create a fresh template.
	info, err := client.CreateTemplate(ctx, CreateTemplateConfig{
		Name: "go-sdk-integration-build-status",
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	t.Logf("created template %s, build %s", info.TemplateID, info.BuildID)

	t.Cleanup(func() {
		if err := client.DeleteTemplate(context.Background(), info.TemplateID); err != nil {
			t.Logf("cleanup DeleteTemplate: %v", err)
		} else {
			t.Logf("cleaned up template %s", info.TemplateID)
		}
	})

	// Before triggering the build, status should be "waiting".
	status, err := client.GetBuildStatus(ctx, info.TemplateID, info.BuildID)
	if err != nil {
		t.Fatalf("GetBuildStatus (waiting): %v", err)
	}
	t.Logf("initial status=%q", status.Status)
	if status.Status != "waiting" {
		t.Errorf("expected status %q, got %q", "waiting", status.Status)
	}

	// Phase 2: Start the build.
	err = client.StartTemplateBuild(ctx, info.TemplateID, info.BuildID, StartBuildConfig{
		FromImage: "python:3.11-slim",
		Steps: []TemplateStep{
			{Type: "run", Args: []string{"echo hello"}},
		},
	})
	if err != nil {
		t.Fatalf("StartTemplateBuild: %v", err)
	}

	// Phase 3: Poll until terminal status, with a timeout.
	deadline := time.After(3 * time.Minute)
	logsOffset := 0
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for build to complete")
		default:
		}

		status, err = client.GetBuildStatus(ctx, info.TemplateID, info.BuildID,
			WithBuildStatusLogsOffset(logsOffset),
		)
		if err != nil {
			t.Fatalf("GetBuildStatus (poll): %v", err)
		}

		// Advance offset for incremental log retrieval.
		logsOffset += len(status.Logs)

		t.Logf("status=%q logs=%d logEntries=%d offset=%d",
			status.Status, len(status.Logs), len(status.LogEntries), logsOffset)

		if status.Status == "ready" || status.Status == "error" {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Logf("final status=%q (total logs seen: %d)", status.Status, logsOffset)

	if status.Status == "error" && status.Reason != nil {
		t.Logf("build error reason: %s (step: %s)", status.Reason.Message, status.Reason.Step)
	}
}
