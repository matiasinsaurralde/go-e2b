//go:build integration

package e2b

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
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
