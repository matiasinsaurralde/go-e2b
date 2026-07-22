//go:build integration

package e2b

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Run with:
//
//	E2B_API_KEY=e2b_xxx go test -tags=integration -v -run TestIntegrationFork ./...

// closeForks kills every successfully started fork so integration runs don't
// leak billed sandboxes.
func closeForks(t *testing.T, results []ForkResult) {
	t.Helper()
	for _, r := range results {
		if r.Sandbox == nil {
			continue
		}
		if err := r.Sandbox.Close(); err != nil {
			t.Logf("cleanup fork %s Close: %v", r.Sandbox.ID, err)
		}
	}
}

// TestIntegrationForkInheritsState is the core end-to-end test: it writes a
// marker file into the source sandbox, forks it, and verifies each fork both
// sees the marker (proving it booted from the source snapshot) and can run a
// command independently.
func TestIntegrationForkInheritsState(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// Write into the sandbox user's home so the file is fully owned/writable by
	// the default command user (a bare /tmp path can be created but not
	// re-opened for write on some templates).
	const markerPath = "/home/user/fork_marker.txt"
	const marker = "forked-from-source"

	if _, err := sbx.Filesystem.WriteString(ctx, markerPath, marker); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	results, err := sbx.Fork(ctx, WithForkCount(2), WithForkTimeout(120*time.Second))
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	t.Cleanup(func() { closeForks(t, results) })

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}

	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("results[%d].Err = %v, want nil", i, r.Err)
		}
		if r.Sandbox == nil {
			t.Fatalf("results[%d].Sandbox is nil", i)
		}
		if r.Sandbox.ID == sbx.ID {
			t.Errorf("results[%d] has same ID as source %s", i, sbx.ID)
		}

		// The marker written before the fork must be present in each fork.
		got, err := r.Sandbox.Filesystem.ReadString(ctx, markerPath)
		if err != nil {
			t.Fatalf("results[%d] read marker: %v", i, err)
		}
		if got != marker {
			t.Errorf("results[%d] marker = %q, want %q", i, got, marker)
		}

		// Each fork runs commands independently.
		res, err := r.Sandbox.Commands.Run(ctx, "echo hello from fork")
		if err != nil {
			t.Fatalf("results[%d] Run: %v", i, err)
		}
		if strings.TrimSpace(res.Stdout) != "hello from fork" {
			t.Errorf("results[%d] stdout = %q, want %q", i, res.Stdout, "hello from fork")
		}
		t.Logf("fork %d: id=%s marker-ok run-ok", i, r.Sandbox.ID)
	}

	// Forks are independent: mutating one must not affect the other or the source.
	if _, err := results[0].Sandbox.Filesystem.WriteString(ctx, markerPath, "mutated"); err != nil {
		t.Fatalf("mutate fork 0: %v", err)
	}
	got1, err := results[1].Sandbox.Filesystem.ReadString(ctx, markerPath)
	if err != nil {
		t.Fatalf("re-read fork 1: %v", err)
	}
	if got1 != marker {
		t.Errorf("fork 1 marker = %q after mutating fork 0, want unchanged %q", got1, marker)
	}
	gotSrc, err := sbx.Filesystem.ReadString(ctx, markerPath)
	if err != nil {
		t.Fatalf("re-read source: %v", err)
	}
	if gotSrc != marker {
		t.Errorf("source marker = %q after mutating fork 0, want unchanged %q", gotSrc, marker)
	}
}

// TestIntegrationForkDefaultCount forks with defaults (count 1) via the static
// Client entry point.
func TestIntegrationForkDefaultCount(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	results, err := sbx.client.ForkSandbox(ctx, sbx.ID)
	if err != nil {
		t.Fatalf("ForkSandbox: %v", err)
	}
	t.Cleanup(func() { closeForks(t, results) })

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Sandbox == nil {
		t.Fatalf("results[0].Sandbox is nil (err=%v)", results[0].Err)
	}
	running, err := results[0].Sandbox.IsRunningWithContext(ctx)
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !running {
		t.Error("forked sandbox is not running")
	}
}

// TestIntegrationForkSourceNotFound verifies the whole-request 404 path maps to
// *SandboxNotFoundError.
func TestIntegrationForkSourceNotFound(t *testing.T) {
	client, _ := integrationFilesystemClient(t)
	ctx := context.Background()

	_, err := client.ForkSandbox(ctx, "does-not-exist-12345")
	var snf *SandboxNotFoundError
	if !errors.As(err, &snf) {
		t.Fatalf("err = %v, want *SandboxNotFoundError", err)
	}
}
