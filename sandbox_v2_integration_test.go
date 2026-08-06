//go:build integration

package e2b

import (
	"context"
	"os"
	"testing"
	"time"
)

// Run with:
//
//	E2B_API_KEY=e2b_xxx E2B_TEMPLATE=nlhz8vlwyupq845jsdg9 go test -tags=integration -v -timeout 10m -run TestIntegrationListSandboxesV2 ./...

func listV2IntegrationClient(t *testing.T) *Client {
	t.Helper()

	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("WEKNORA_SANDBOX_E2B_API_KEY")
	}
	if apiKey == "" {
		t.Skip("E2B_API_KEY or WEKNORA_SANDBOX_E2B_API_KEY not set, skipping integration test")
	}

	apiURL := os.Getenv("E2B_API_URL")
	if apiURL == "" {
		apiURL = os.Getenv("WEKNORA_SANDBOX_E2B_API_URL")
	}

	cfg := ClientConfig{APIKey: apiKey}
	if apiURL != "" {
		cfg.APIBaseURL = apiURL
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func listV2Template(t *testing.T) string {
	t.Helper()
	tmpl := os.Getenv("E2B_TEMPLATE")
	if tmpl == "" {
		tmpl = os.Getenv("WEKNORA_SANDBOX_E2B_TEMPLATE")
	}
	if tmpl == "" {
		tmpl = "base"
	}
	return tmpl
}

// TestIntegrationListSandboxesV2NoFilter verifies ListSandboxesV2 works
// without any state filter (should return both running and paused sandboxes).
func TestIntegrationListSandboxesV2NoFilter(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()

	result, err := client.ListSandboxesV2(ctx)
	if err != nil {
		t.Fatalf("ListSandboxesV2 (no filter): %v", err)
	}

	t.Logf("Found %d sandboxes (nextToken=%q)", len(result.Sandboxes), result.NextToken)
	for i, s := range result.Sandboxes {
		t.Logf("  [%d] id=%s state=%s template=%s", i, s.ID, s.State, s.Template)
	}
}

// TestIntegrationListSandboxesV2SingleState verifies ListSandboxesV2 works
// with a single state filter (e.g., only "running").
func TestIntegrationListSandboxesV2SingleState(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()

	result, err := client.ListSandboxesV2(ctx, WithSandboxState("running"))
	if err != nil {
		t.Fatalf("ListSandboxesV2(state=running): %v", err)
	}

	t.Logf("Found %d running sandboxes", len(result.Sandboxes))
	for i, s := range result.Sandboxes {
		t.Logf("  [%d] id=%s state=%s template=%s", i, s.ID, s.State, s.Template)
		if s.State != "running" {
			t.Errorf("sandbox %s has state=%q, expected running", s.ID, s.State)
		}
	}
}

// TestIntegrationListSandboxesV2MultipleStates is the KEY test that verifies
// the state parameter fix. It calls ListSandboxesV2 with multiple states
// (running + paused). The original buggy code would send:
//
//	?state=running&state=paused
//
// causing a 400 error. The fixed code correctly sends:
//
//	?state=running,paused
//
// If this test passes without a 400 error, the fix is working correctly.
func TestIntegrationListSandboxesV2MultipleStates(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()

	// This is the call that triggers the bug. With the original code
	// (q.Add in a loop), this would produce ?state=running&state=paused
	// and E2B would reject it with:
	//   "parameter 'state' is not exploded, but is specified multiple times"
	//
	// With the fix (q.Set + strings.Join), this produces ?state=running,paused
	// and should succeed.
	result, err := client.ListSandboxesV2(ctx, WithSandboxState("running", "paused"))
	if err != nil {
		t.Fatalf(
			"ListSandboxesV2(state=running,paused): %v\n\n"+
				"BUG CONFIRMED: If you see a 400 error mentioning 'not exploded' or 'specified multiple times',\n"+
				"the SDK is still sending state=running&state=paused (multiple params) instead of state=running,paused (comma-separated).\n"+
				"The fix in client.go should use q.Set(\"state\", strings.Join(p.state, \",\")) instead of q.Add in a loop.",
			err,
		)
	}

	t.Logf("Found %d sandboxes matching running+paused (nextToken=%q)", len(result.Sandboxes), result.NextToken)
	for i, s := range result.Sandboxes {
		t.Logf("  [%d] id=%s state=%s template=%s", i, s.ID, s.State, s.Template)
		if s.State != "running" && s.State != "paused" {
			t.Errorf("unexpected state %q for sandbox %s (expected running or paused)", s.State, s.ID)
		}
	}

	t.Log("SUCCESS: Multiple state filter works correctly with comma-separated format.")
}

// TestIntegrationListSandboxesV2MultipleStatesWithSandboxes creates sandboxes
// in different states and verifies the multi-state filter returns correct results.
func TestIntegrationListSandboxesV2MultipleStatesWithSandboxes(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()
	tmpl := listV2Template(t)

	// Create a running sandbox.
	running, err := client.NewSandbox(ctx, SandboxConfig{Template: tmpl, Timeout: 300})
	if err != nil {
		t.Fatalf("NewSandbox (running): %v", err)
	}
	defer func() { _ = running.Close() }()
	t.Logf("Created running sandbox: %s", running.ID)

	// Create a sandbox and pause it.
	paused, err := client.NewSandbox(ctx, SandboxConfig{Template: tmpl, Timeout: 300})
	if err != nil {
		t.Fatalf("NewSandbox (to-pause): %v", err)
	}
	defer func() { _ = paused.Close() }()
	t.Logf("Created sandbox to pause: %s", paused.ID)

	if err := paused.Pause(); err != nil {
		t.Fatalf("Pause sandbox %s: %v", paused.ID, err)
	}
	t.Logf("Paused sandbox: %s", paused.ID)

	// Give E2B a moment to settle state.
	time.Sleep(2 * time.Second)

	// Now call ListSandboxesV2 with both states — this is the critical test.
	result, err := client.ListSandboxesV2(ctx, WithSandboxState("running", "paused"))
	if err != nil {
		t.Fatalf(
			"ListSandboxesV2(state=running,paused): %v\n\n"+
				"BUG: SDK sent multiple state= params instead of comma-separated.",
			err,
		)
	}

	t.Logf("Found %d sandboxes (running+paused)", len(result.Sandboxes))
	for i, s := range result.Sandboxes {
		t.Logf("  [%d] id=%s state=%s template=%s", i, s.ID, s.State, s.Template)
	}

	// Verify our sandboxes are in the results.
	foundRunning := false
	foundPaused := false
	for _, s := range result.Sandboxes {
		if s.ID == running.ID {
			foundRunning = true
			if s.State != "running" {
				t.Errorf("sandbox %s state=%q, expected running", s.ID, s.State)
			}
		}
		if s.ID == paused.ID {
			foundPaused = true
			if s.State != "paused" {
				t.Errorf("sandbox %s state=%q, expected paused", s.ID, s.State)
			}
		}
	}
	if !foundRunning {
		t.Errorf("running sandbox %s not found in results", running.ID)
	}
	if !foundPaused {
		t.Errorf("paused sandbox %s not found in results", paused.ID)
	}

	// Now test with only "paused" state to verify filtering works.
	pausedOnly, err := client.ListSandboxesV2(ctx, WithSandboxState("paused"))
	if err != nil {
		t.Fatalf("ListSandboxesV2(state=paused): %v", err)
	}
	for _, s := range pausedOnly.Sandboxes {
		if s.State != "paused" {
			t.Errorf("sandbox %s state=%q in paused-only results", s.ID, s.State)
		}
	}

	t.Log("SUCCESS: Multi-state filter verified with actual running and paused sandboxes.")
}

// TestIntegrationListSandboxesV2Pagination verifies pagination works with the fix.
func TestIntegrationListSandboxesV2Pagination(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()

	page1, err := client.ListSandboxesV2(ctx, WithSandboxLimit(2))
	if err != nil {
		t.Fatalf("ListSandboxesV2 page1: %v", err)
	}
	t.Logf("Page 1: %d items, nextToken=%q", len(page1.Sandboxes), page1.NextToken)

	if page1.NextToken != "" {
		page2, err := client.ListSandboxesV2(ctx, WithSandboxNextToken(page1.NextToken))
		if err != nil {
			t.Fatalf("ListSandboxesV2 page2: %v", err)
		}
		t.Logf("Page 2: %d items, nextToken=%q", len(page2.Sandboxes), page2.NextToken)

		// Verify no duplicates between pages.
		ids := make(map[string]bool)
		for _, s := range page1.Sandboxes {
			ids[s.ID] = true
		}
		for _, s := range page2.Sandboxes {
			if ids[s.ID] {
				t.Errorf("duplicate sandbox %s in both pages", s.ID)
			}
		}
	}
}

// TestIntegrationListSandboxesV2PaginationWithState combines pagination with
// multi-state filter — the most complex scenario that was broken.
func TestIntegrationListSandboxesV2PaginationWithState(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()

	result, err := client.ListSandboxesV2(ctx,
		WithSandboxState("running", "paused"),
		WithSandboxLimit(3),
	)
	if err != nil {
		t.Fatalf(
			"ListSandboxesV2(state=running,paused, limit=3): %v\n\n"+
				"BUG: Combined multi-state + pagination failed with 400 error.",
			err,
		)
	}

	t.Logf("Combined filter: %d items, nextToken=%q", len(result.Sandboxes), result.NextToken)
	for i, s := range result.Sandboxes {
		t.Logf("  [%d] id=%s state=%s", i, s.ID, s.State)
		if s.State != "running" && s.State != "paused" {
			t.Errorf("unexpected state %q", s.State)
		}
	}

	t.Log("SUCCESS: Multi-state + pagination works correctly.")
}
