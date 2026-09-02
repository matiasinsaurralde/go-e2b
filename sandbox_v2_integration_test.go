//go:build integration

package e2b

import (
	"context"
	"errors"
	"fmt"
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
		t.Skip("E2B_API_KEY not set, skipping integration test")
	}

	apiURL := os.Getenv("E2B_API_URL")

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

// TestIntegrationListSandboxesV2MultiMetadata verifies filtering by MULTIPLE
// metadata keys. The E2B API treats metadata as a single string of key=value
// pairs joined by "&" (e.g. metadata=env%3Ddev%26app%3Dprod), NOT as repeated
// metadata= params. The original buggy code sent repeated params, which the API
// rejects with:
//
//	"multiple values for single value parameter 'metadata'"
//
// A single-key filter happened to work; this test exercises the multi-key path
// that was broken.
func TestIntegrationListSandboxesV2MultiMetadata(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()
	tmpl := listV2Template(t)

	// Use a value unlikely to collide with other sandboxes on the account.
	const runTag = "go-e2b-multi-meta-test"

	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template: tmpl,
		Timeout:  120,
		Metadata: map[string]string{"suite": runTag, "kind": "multimeta"},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	defer func() { _ = sbx.Close() }()
	t.Logf("Created sandbox with metadata: %s", sbx.ID)

	// Filter by BOTH metadata keys. With the buggy code this produced two
	// metadata= params and failed with a 400.
	result, err := client.ListSandboxesV2(ctx,
		WithSandboxMetadata(map[string]string{"suite": runTag, "kind": "multimeta"}),
	)
	if err != nil {
		t.Fatalf(
			"ListSandboxesV2(metadata suite=%s,kind=multimeta): %v\n\n"+
				"BUG: If you see a 400 mentioning 'multiple values for single value parameter',\n"+
				"the SDK is sending repeated metadata= params instead of one comma/&-joined value.",
			runTag, err,
		)
	}

	t.Logf("Found %d sandboxes matching both metadata keys", len(result.Sandboxes))
	found := false
	for _, s := range result.Sandboxes {
		t.Logf("  id=%s metadata=%v", s.ID, s.Metadata)
		if s.ID == sbx.ID {
			found = true
			if s.Metadata["suite"] != runTag || s.Metadata["kind"] != "multimeta" {
				t.Errorf("sandbox %s metadata = %v, want suite=%s kind=multimeta", s.ID, s.Metadata, runTag)
			}
		}
	}
	if !found {
		t.Errorf("created sandbox %s not found when filtering by both metadata keys", sbx.ID)
	}

	t.Log("SUCCESS: Multi-key metadata filter works correctly.")
}

func listV2RunTag(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func createTaggedSandbox(t *testing.T, client *Client, template, runTag, kind string) *Sandbox {
	t.Helper()
	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		Template: template,
		Timeout:  180,
		Metadata: map[string]string{"suite": runTag, "kind": kind},
	})
	if err != nil {
		t.Fatalf("NewSandbox(%s): %v", kind, err)
	}
	t.Cleanup(func() { _ = sbx.Close() })
	t.Logf("Created sandbox %s kind=%s", sbx.ID, kind)
	return sbx
}

func idsInOrder(result *ListSandboxesV2Result) []string {
	ids := make([]string, 0, len(result.Sandboxes))
	for _, s := range result.Sandboxes {
		ids = append(ids, s.ID)
	}
	return ids
}

func findSandboxIndex(result *ListSandboxesV2Result, id string) int {
	for i, s := range result.Sandboxes {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// TestIntegrationListSandboxesV2Order verifies sort order across the matching set.
func TestIntegrationListSandboxesV2Order(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()
	tmpl := listV2Template(t)
	runTag := listV2RunTag("go-e2b-list-order")

	a := createTaggedSandbox(t, client, tmpl, runTag, "a")
	time.Sleep(2 * time.Second)
	b := createTaggedSandbox(t, client, tmpl, runTag, "b")

	meta := WithSandboxMetadata(map[string]string{"suite": runTag})

	asc, err := client.ListSandboxesV2(ctx, meta, WithSandboxOrder(OrderAsc))
	if err != nil {
		t.Fatalf("ListSandboxesV2(order=asc): %v", err)
	}
	idxA := findSandboxIndex(asc, a.ID)
	idxB := findSandboxIndex(asc, b.ID)
	if idxA < 0 || idxB < 0 {
		t.Fatalf("order=asc missing sandboxes: a=%d b=%d ids=%v", idxA, idxB, idsInOrder(asc))
	}
	if idxA >= idxB {
		t.Errorf("order=asc: expected %s before %s, got %v", a.ID, b.ID, idsInOrder(asc))
	}

	desc, err := client.ListSandboxesV2(ctx, meta, WithSandboxOrder(OrderDesc))
	if err != nil {
		t.Fatalf("ListSandboxesV2(order=desc): %v", err)
	}
	idxA = findSandboxIndex(desc, a.ID)
	idxB = findSandboxIndex(desc, b.ID)
	if idxA < 0 || idxB < 0 {
		t.Fatalf("order=desc missing sandboxes: a=%d b=%d ids=%v", idxA, idxB, idsInOrder(desc))
	}
	if idxB >= idxA {
		t.Errorf("order=desc: expected %s before %s, got %v", b.ID, a.ID, idsInOrder(desc))
	}

	def, err := client.ListSandboxesV2(ctx, meta)
	if err != nil {
		t.Fatalf("ListSandboxesV2(default order): %v", err)
	}
	idxA = findSandboxIndex(def, a.ID)
	idxB = findSandboxIndex(def, b.ID)
	if idxA < 0 || idxB < 0 {
		t.Fatalf("default order missing sandboxes: a=%d b=%d ids=%v", idxA, idxB, idsInOrder(def))
	}
	if idxB >= idxA {
		t.Errorf("default order: expected newest-first (%s before %s), got %v", b.ID, a.ID, idsInOrder(def))
	}
}

// TestIntegrationListSandboxesV2Template verifies template ID/alias filtering.
func TestIntegrationListSandboxesV2Template(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()
	tmpl := listV2Template(t)
	runTag := listV2RunTag("go-e2b-list-template")

	sbx := createTaggedSandbox(t, client, tmpl, runTag, "tmpl")
	meta := WithSandboxMetadata(map[string]string{"suite": runTag})

	matched, err := client.ListSandboxesV2(ctx, meta, WithSandboxTemplate(tmpl))
	if err != nil {
		t.Fatalf("ListSandboxesV2(template=%s): %v", tmpl, err)
	}
	if findSandboxIndex(matched, sbx.ID) < 0 {
		t.Errorf("sandbox %s not found with template=%s; ids=%v", sbx.ID, tmpl, idsInOrder(matched))
	}

	missing, err := client.ListSandboxesV2(ctx, meta, WithSandboxTemplate("this-template-does-not-exist-"+runTag))
	if err != nil {
		t.Fatalf("ListSandboxesV2(unknown template): %v", err)
	}
	if len(missing.Sandboxes) != 0 {
		t.Errorf("unknown template returned %d sandboxes, want 0: %v", len(missing.Sandboxes), idsInOrder(missing))
	}
}

// TestIntegrationListSandboxesV2StartedAfter verifies the inclusive startedAt lower bound.
func TestIntegrationListSandboxesV2StartedAfter(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()
	tmpl := listV2Template(t)
	runTag := listV2RunTag("go-e2b-list-started")

	t0 := time.Now().UTC().Add(-time.Second)
	sbx := createTaggedSandbox(t, client, tmpl, runTag, "started")
	meta := WithSandboxMetadata(map[string]string{"suite": runTag})

	present, err := client.ListSandboxesV2(ctx, meta, WithSandboxStartedAfter(t0))
	if err != nil {
		t.Fatalf("ListSandboxesV2(startedAfter=t0): %v", err)
	}
	if findSandboxIndex(present, sbx.ID) < 0 {
		t.Errorf("sandbox %s not found with startedAfter=%s; ids=%v", sbx.ID, t0.Format(time.RFC3339), idsInOrder(present))
	}

	future := time.Now().Add(time.Hour)
	absent, err := client.ListSandboxesV2(ctx, meta, WithSandboxStartedAfter(future))
	if err != nil {
		t.Fatalf("ListSandboxesV2(startedAfter=future): %v", err)
	}
	if len(absent.Sandboxes) != 0 {
		t.Errorf("future startedAfter returned %d sandboxes, want 0: %v", len(absent.Sandboxes), idsInOrder(absent))
	}
}

// TestIntegrationListSandboxesV2NewFiltersWithPagination pages through a tagged
// set with order=asc and limit=1, re-passing the same filters on every page.
func TestIntegrationListSandboxesV2NewFiltersWithPagination(t *testing.T) {
	client := listV2IntegrationClient(t)
	ctx := context.Background()
	tmpl := listV2Template(t)
	runTag := listV2RunTag("go-e2b-list-pages")

	a := createTaggedSandbox(t, client, tmpl, runTag, "a")
	time.Sleep(2 * time.Second)
	b := createTaggedSandbox(t, client, tmpl, runTag, "b")
	time.Sleep(2 * time.Second)
	c := createTaggedSandbox(t, client, tmpl, runTag, "c")
	want := []string{a.ID, b.ID, c.ID}

	meta := map[string]string{"suite": runTag}
	var got []string
	seen := make(map[string]bool)
	token := ""
	for i := 0; i < 10; i++ {
		opts := []ListSandboxesV2Option{
			WithSandboxMetadata(meta),
			WithSandboxOrder(OrderAsc),
			WithSandboxLimit(1),
		}
		if token != "" {
			opts = append(opts, WithSandboxNextToken(token))
		}
		page, err := client.ListSandboxesV2(ctx, opts...)
		if err != nil {
			t.Fatalf("ListSandboxesV2 page %d: %v", i, err)
		}
		for _, s := range page.Sandboxes {
			if seen[s.ID] {
				t.Errorf("duplicate sandbox %s across pages", s.ID)
			}
			seen[s.ID] = true
			got = append(got, s.ID)
		}
		token = page.NextToken
		if token == "" {
			break
		}
	}

	if len(got) != 3 {
		t.Fatalf("paged %d sandboxes, want 3: %v", len(got), got)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("page order[%d] = %s, want %s (got %v)", i, got[i], id, got)
		}
	}
}

// TestIntegrationListSandboxesV2InvalidOrder confirms the server, not the client,
// rejects unknown order values.
func TestIntegrationListSandboxesV2InvalidOrder(t *testing.T) {
	client := listV2IntegrationClient(t)

	_, err := client.ListSandboxesV2(context.Background(), WithSandboxOrder("sideways"))
	if err == nil {
		t.Fatal("expected error for invalid order")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("status = %d, want 400 (%v)", apiErr.StatusCode, err)
	}
}
