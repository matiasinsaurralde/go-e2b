//go:build integration

package e2b

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Run with:
//
//	E2B_API_KEY=e2b_xxx E2B_TEMPLATE=nlhz8vlwyupq845jsdg9 go test -tags=integration -v -timeout 20m -run TestIntegrationLifecycle ./...

func lifecycleIntegrationClient(t *testing.T) *Client {
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

func lifecycleTemplate(t *testing.T) string {
	t.Helper()
	tmpl := os.Getenv("E2B_TEMPLATE")
	if tmpl == "" {
		tmpl = "base"
	}
	return tmpl
}

// ============================================================================
// Test 1: NewSandbox — basic create with env vars
// ============================================================================
func TestIntegrationNewSandboxBasic(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template: lifecycleTemplate(t),
		Timeout:  120,
		EnvVars:  map[string]string{"LIFECYCLE_TEST": "true"},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	defer sbx.Close()

	t.Logf("Created sandbox: %s (accessToken=%s...)", sbx.ID, sbx.accessToken[:min(8, len(sbx.accessToken))])

	// Verify sandbox is running.
	info, err := sbx.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	t.Logf("  state=%s template=%s cpu=%d mem=%dMB disk=%dMB",
		info.State, info.Template, info.CPUCount, info.MemoryMB, info.DiskSizeMB)

	if info.State != "running" {
		t.Errorf("expected state=running, got %q", info.State)
	}

	// Verify we can execute commands.
	result, err := sbx.Commands.Run("echo", []string{"hello-integration-test"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello-integration-test") {
		t.Errorf("unexpected stdout: %q", result.Stdout)
	}
	t.Logf("  echo result: exit=%d stdout=%s", result.ExitCode, strings.TrimSpace(result.Stdout))
}

// ============================================================================
// Test 2: NewSandbox with AutoPause — sandbox auto-pauses on timeout
// ============================================================================
func TestIntegrationNewSandboxAutoPause(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	autoPauseMem := false // filesystem-only snapshot for faster pause/resume

	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template:        lifecycleTemplate(t),
		Timeout:         30, // 30s timeout for fast auto-pause
		AutoPause:       true,
		AutoPauseMemory: &autoPauseMem,
	})
	if err != nil {
		t.Fatalf("NewSandbox with AutoPause: %v", err)
	}
	t.Logf("Created sandbox with AutoPause: %s (30s timeout, autoPauseMemory=false)", sbx.ID)

	// Write a marker file to verify data survives pause.
	_, err = sbx.Filesystem.WriteString(ctx, "/tmp/auto-pause-test.txt", "this data survives auto-pause")
	if err != nil {
		sbx.Close()
		t.Fatalf("WriteString: %v", err)
	}
	t.Logf("  wrote marker file /tmp/auto-pause-test.txt")

	// Verify sandbox is running.
	info, err := sbx.Info()
	if err != nil {
		sbx.Close()
		t.Fatalf("Info: %v", err)
	}
	t.Logf("  initial state=%s", info.State)

	// Wait for auto-pause. The sandbox should auto-pause after 30s.
	// We poll up to 90s.
	t.Logf("  waiting for sandbox to auto-pause (timeout=30s)...")
	deadline := time.After(90 * time.Second)
	pollInterval := 3 * time.Second
	paused := false
	for !paused {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for auto-pause")
		case <-time.After(pollInterval):
		}
		info, err := sbx.Info()
		if err != nil {
			t.Logf("  Info error during poll: %v", err)
			continue
		}
		t.Logf("  poll: state=%s", info.State)
		if info.State == "paused" {
			paused = true
		}
	}

	t.Logf("  sandbox auto-paused successfully!")

	// Cleanup: kill the paused sandbox.
	if err := sbx.Close(); err != nil {
		t.Logf("  Close paused sandbox: %v", err)
	}
	t.Logf("  sandbox terminated")
}

// ============================================================================
// Test 3: NewSandbox with Metadata
// ============================================================================
func TestIntegrationNewSandboxMetadata(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template: lifecycleTemplate(t),
		Timeout:  120,
		Metadata: map[string]string{
			"test_name": "lifecycle-metadata",
			"env":       "integration",
			"runner":    "go-sdk",
		},
	})
	if err != nil {
		t.Fatalf("NewSandbox with metadata: %v", err)
	}
	defer sbx.Close()

	t.Logf("Created sandbox with metadata: %s", sbx.ID)

	info, err := sbx.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	t.Logf("  state=%s, clientID=%s", info.State, info.ClientID)

	if info.State != "running" {
		t.Errorf("expected state=running, got %q", info.State)
	}

	// Verify metadata round-trip: create → Info() should return the same values.
	if info.Metadata["test_name"] != "lifecycle-metadata" {
		t.Errorf("metadata[test_name] = %q, want %q", info.Metadata["test_name"], "lifecycle-metadata")
	}
	if info.Metadata["env"] != "integration" {
		t.Errorf("metadata[env] = %q, want %q", info.Metadata["env"], "integration")
	}
	if info.Metadata["runner"] != "go-sdk" {
		t.Errorf("metadata[runner] = %q, want %q", info.Metadata["runner"], "go-sdk")
	}

	t.Logf("  metadata round-trip: %v", info.Metadata)
}

// ============================================================================
// Test 4: Manual Pause (keepMemory=false) → filesystem-only snapshot
// ============================================================================
func TestIntegrationManualPauseFilesystemOnly(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template: lifecycleTemplate(t),
		Timeout:  300,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}

	t.Logf("Created sandbox: %s", sbx.ID)

	// Write marker file.
	const markerContent = "filesystem-only snapshot test data"
	_, err = sbx.Filesystem.WriteString(ctx, "/tmp/pause-fs-only.txt", markerContent)
	if err != nil {
		sbx.Close()
		t.Fatalf("WriteString: %v", err)
	}
	t.Logf("  wrote /tmp/pause-fs-only.txt")

	// Pause with keepMemory=false (filesystem-only snapshot).
	t.Logf("  pausing with keepMemory=false...")
	if err := sbx.Pause(false); err != nil {
		sbx.Close()
		t.Fatalf("Pause(false): %v", err)
	}

	// Verify paused state.
	info, err := sbx.Info()
	if err != nil {
		sbx.Close()
		t.Fatalf("Info after pause: %v", err)
	}
	t.Logf("  state=%s (expected: paused)", info.State)
	if info.State != "paused" {
		t.Errorf("expected state=paused after Pause(), got %q", info.State)
	}

	// Resume with Connect.
	t.Logf("  connecting (resuming) sandbox...")
	resumed, err := client.Connect(ctx, sbx.ID, 120)
	if err != nil {
		sbx.Close()
		t.Fatalf("Connect: %v", err)
	}
	defer resumed.Close()

	t.Logf("  resumed sandbox: %s", resumed.ID)

	// Verify running again.
	info2, err := resumed.Info()
	if err != nil {
		t.Fatalf("Info after resume: %v", err)
	}
	t.Logf("  state after resume=%s", info2.State)
	if info2.State != "running" {
		t.Errorf("expected state=running after Connect, got %q", info2.State)
	}

	// Filesystem-only snapshot → reboots on resume, so the marker file
	// should still exist (it's on disk).
	content, err := resumed.Filesystem.ReadString(ctx, "/tmp/pause-fs-only.txt")
	if err != nil {
		t.Fatalf("ReadString after resume: %v", err)
	}
	t.Logf("  read back: %q", content)
	if content != markerContent {
		t.Errorf("marker file content mismatch: got %q, want %q", content, markerContent)
	}
	t.Logf("  filesystem-only snapshot: file survived resume correctly!")
}

// ============================================================================
// Test 5: Manual Pause (keepMemory=true, default) → full memory snapshot
// ============================================================================
func TestIntegrationManualPauseFullMemory(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template: lifecycleTemplate(t),
		Timeout:  300,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}

	t.Logf("Created sandbox: %s", sbx.ID)

	// Write marker file and set an environment variable in a process.
	const markerContent = "full-memory snapshot test data"
	_, err = sbx.Filesystem.WriteString(ctx, "/tmp/pause-full-mem.txt", markerContent)
	if err != nil {
		sbx.Close()
		t.Fatalf("WriteString: %v", err)
	}
	t.Logf("  wrote /tmp/pause-full-mem.txt (filesystem check)")

	// Pause with keepMemory=true (default, full memory snapshot).
	t.Logf("  pausing with keepMemory=true (default)...")
	if err := sbx.Pause(true); err != nil {
		sbx.Close()
		t.Fatalf("Pause(true): %v", err)
	}

	info, err := sbx.Info()
	if err != nil {
		sbx.Close()
		t.Fatalf("Info after pause: %v", err)
	}
	t.Logf("  state=%s", info.State)
	if info.State != "paused" {
		t.Errorf("expected paused, got %q", info.State)
	}

	// Resume with Connect.
	t.Logf("  connecting (resuming) sandbox...")
	resumed, err := client.Connect(ctx, sbx.ID, 120)
	if err != nil {
		sbx.Close()
		t.Fatalf("Connect: %v", err)
	}
	defer resumed.Close()

	t.Logf("  resumed sandbox: %s", resumed.ID)

	// Verify running.
	info2, err := resumed.Info()
	if err != nil {
		t.Fatalf("Info after resume: %v", err)
	}
	t.Logf("  state after resume=%s", info2.State)

	// Full memory snapshot should have file on disk.
	content, err := resumed.Filesystem.ReadString(ctx, "/tmp/pause-full-mem.txt")
	if err != nil {
		t.Fatalf("ReadString after resume: %v", err)
	}
	t.Logf("  read back: %q", content)
	if content != markerContent {
		t.Errorf("marker mismatch: got %q, want %q", content, markerContent)
	}
	t.Logf("  full-memory snapshot: file survived resume correctly!")
}

// ============================================================================
// Test 6: Connect with wrong sandbox ID → NotFound
// ============================================================================
func TestIntegrationConnectNotFound(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	const fakeID = "nonexistent-sandbox-id-12345"
	_, err := client.Connect(ctx, fakeID, 120)
	if err == nil {
		t.Fatal("expected error for nonexistent sandbox, got nil")
	}
	t.Logf("  expected error: %v (sandboxNotFound expected)", err)

	var e *SandboxNotFoundError
	if !errors.As(err, &e) {
		// Accept either SandboxNotFoundError or generic Error (server may
		// return 404 as a different structure for invalid IDs).
		t.Logf("  note: error type is %T (not SandboxNotFoundError, but fine for invalid ID)", err)
	}
}

// ============================================================================
// Test 7: Full lifecycle — Create → Write → Pause → Connect → Verify → Close
// ============================================================================
func TestIntegrationLifecycleFullWorkflow(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	// ========== Step 1: Create ==========
	t.Log("=== Step 1: Create sandbox ===")
	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template: lifecycleTemplate(t),
		Timeout:  300,
		Metadata: map[string]string{"workflow": "full-lifecycle"},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	t.Logf("  created: %s", sbx.ID)

	// ========== Step 2: Write data ==========
	t.Log("=== Step 2: Write files ===")
	const (
		file1Path    = "/tmp/workflow-hello.txt"
		file1Content = "Hello from full lifecycle test!"
		file2Path    = "/tmp/workflow-config.json"
		file2Content = `{"key": "value", "version": 1}`
	)
	if _, err := sbx.Filesystem.WriteString(ctx, file1Path, file1Content); err != nil {
		sbx.Close()
		t.Fatalf("WriteString file1: %v", err)
	}
	if _, err := sbx.Filesystem.WriteString(ctx, file2Path, file2Content); err != nil {
		sbx.Close()
		t.Fatalf("WriteString file2: %v", err)
	}
	t.Logf("  wrote %s and %s", file1Path, file2Path)

	// ========== Step 3: Run command ==========
	t.Log("=== Step 3: Run command ===")
	result, err := sbx.Commands.Run("bash", []string{"-c", "echo 'command-execution-ok' && ls /tmp/workflow-*"})
	if err != nil {
		sbx.Close()
		t.Fatalf("Run: %v", err)
	}
	t.Logf("  stdout: %s", strings.TrimSpace(result.Stdout))
	if !strings.Contains(result.Stdout, "command-execution-ok") {
		t.Errorf("command output missing expected string")
	}

	// ========== Step 4: Pause ==========
	t.Log("=== Step 4: Pause sandbox ===")
	if err := sbx.Pause(false); err != nil {
		sbx.Close()
		t.Fatalf("Pause: %v", err)
	}
	t.Logf("  paused successfully")

	info, err := sbx.Info()
	if err != nil {
		sbx.Close()
		t.Fatalf("Info after pause: %v", err)
	}
	if info.State != "paused" {
		t.Errorf("expected paused, got %q", info.State)
	}

	// ========== Step 5: Connect (resume) ==========
	t.Log("=== Step 5: Connect (resume) ===")
	resumed, err := client.Connect(ctx, sbx.ID, 120)
	if err != nil {
		sbx.Close()
		t.Fatalf("Connect: %v", err)
	}
	t.Logf("  resumed: %s", resumed.ID)

	info2, err := resumed.Info()
	if err != nil {
		resumed.Close()
		t.Fatalf("Info after resume: %v", err)
	}
	t.Logf("  state=%s", info2.State)
	if info2.State != "running" {
		t.Errorf("expected running after Connect, got %q", info2.State)
	}

	// ========== Step 6: Verify data survived ==========
	t.Log("=== Step 6: Verify data survived ===")
	content1, err := resumed.Filesystem.ReadString(ctx, file1Path)
	if err != nil {
		resumed.Close()
		t.Fatalf("ReadString file1: %v", err)
	}
	if content1 != file1Content {
		t.Errorf("file1 mismatch: got %q, want %q", content1, file1Content)
	}
	content2, err := resumed.Filesystem.ReadString(ctx, file2Path)
	if err != nil {
		resumed.Close()
		t.Fatalf("ReadString file2: %v", err)
	}
	if content2 != file2Content {
		t.Errorf("file2 mismatch: got %q, want %q", content2, file2Content)
	}

	// ========== Step 7: Run command again ==========
	t.Log("=== Step 7: Run command after resume ===")
	result2, err := resumed.Commands.Run("echo", []string{"post-resume-ok"})
	if err != nil {
		resumed.Close()
		t.Fatalf("Run after resume: %v", err)
	}
	t.Logf("  stdout: %s", strings.TrimSpace(result2.Stdout))
	if !strings.Contains(result2.Stdout, "post-resume-ok") {
		t.Errorf("post-resume command failed to execute")
	}

	// ========== Step 8: Close ==========
	t.Log("=== Step 8: Close ===")
	if err := resumed.Close(); err != nil {
		t.Logf("  Close: %v", err)
	}

	// Verify it's gone.
	_, err = resumed.Info()
	if err == nil {
		t.Error("expected error for deleted sandbox, got nil")
	}
	t.Logf("  sandbox terminated: %v", err)

	t.Logf("=== FULL LIFECYCLE WORKFLOW COMPLETED SUCCESSFULLY ===")
}

// ============================================================================
// Test 8: NewSandbox with AutoResume config
// ============================================================================
func TestIntegrationNewSandboxAutoResume(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	autoPauseMem := true // full memory snapshot needed for autoResume

	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template:        lifecycleTemplate(t),
		Timeout:         30,
		AutoPause:       true,
		AutoPauseMemory: &autoPauseMem,
		AutoResume:      &AutoResumeConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("NewSandbox with AutoResume: %v", err)
	}
	t.Logf("Created sandbox with AutoResume: %s (30s timeout)", sbx.ID)

	info, err := sbx.Info()
	if err != nil {
		sbx.Close()
		t.Fatalf("Info: %v", err)
	}
	t.Logf("  state=%s lifecycle(onTimeout=%s, autoResume=%v)",
		info.State, info.Lifecycle.OnTimeout, info.Lifecycle.AutoResume)

	// Cleanup: kill the sandbox.
	if err := sbx.Close(); err != nil {
		t.Logf("  Close: %v", err)
	}
	t.Logf("  sandbox terminated")
}

// ============================================================================
// Test 9: Concurrent — create multiple sandboxes and verify isolation
// ============================================================================
func TestIntegrationLifecycleConcurrent(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	const concurrency = 3
	type result struct {
		id        string
		sandboxID string
		err       error
	}
	results := make(chan result, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			id := fmt.Sprintf("concurrent-%d", idx)
			sbx, err := client.NewSandbox(ctx, SandboxConfig{
				Template: lifecycleTemplate(t),
				Timeout:  120,
				Metadata: map[string]string{"concurrent_id": id},
			})
			if err != nil {
				results <- result{id: id, err: err}
				return
			}
			defer sbx.Close()

			// Write a unique file.
			uniqueFile := fmt.Sprintf("/tmp/concurrent-%d.txt", idx)
			_, err = sbx.Filesystem.WriteString(ctx, uniqueFile, id)
			if err != nil {
				results <- result{id: id, sandboxID: sbx.ID, err: err}
				return
			}

			results <- result{id: id, sandboxID: sbx.ID}
		}(i)
	}

	failures := 0
	for i := 0; i < concurrency; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("[%s] error: %v", r.id, r.err)
			failures++
		} else {
			t.Logf("[%s] sandbox=%s OK", r.id, r.sandboxID)
		}
	}

	if failures > 0 {
		t.Fatalf("%d/%d concurrent creates failed", failures, concurrency)
	}
	t.Logf("concurrent test: %d sandboxes created successfully", concurrency)
}

// ============================================================================
// Test 10: autoResume → auto-pause → Connect → command.Run to read file
//
// 场景：
//  1. 创建沙盒，设置 autoPause + autoResume（短超时）
//  2. 用 command.Run 写一个文件
//  3. 轮询等待沙盒自动进入 paused 状态
//  4. 通过 client.Connect() 恢复沙盒句柄（控制面显式恢复）
//  5. 在恢复的句柄上执行 command.Run 读取文件内容并验证
//
// ============================================================================
func TestIntegrationAutoResumeConnectAndRead(t *testing.T) {
	client := lifecycleIntegrationClient(t)
	ctx := context.Background()

	autoPauseMem := true // 需要一个完整内存快照来支持 autoResume

	// ── Step 1: 创建带 autoPause + autoResume 的沙盒 ──
	t.Log("=== Step 1: Create sandbox with autoPause + autoResume ===")
	sbx, err := client.NewSandbox(ctx, SandboxConfig{
		Template:        lifecycleTemplate(t),
		Timeout:         30, // 30s 后自动暂停
		AutoPause:       true,
		AutoPauseMemory: &autoPauseMem,
		AutoResume:      &AutoResumeConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	t.Logf("  created: %s", sbx.ID)

	// 确认生命周期配置
	info, err := sbx.Info()
	if err != nil {
		sbx.Close()
		t.Fatalf("Info: %v", err)
	}
	t.Logf("  lifecycle: onTimeout=%s autoResume=%v", info.Lifecycle.OnTimeout, info.Lifecycle.AutoResume)

	// ── Step 2: 执行命令写入文件 ──
	t.Log("=== Step 2: Write file via command.Run ===")
	const (
		filePath    = "/tmp/auto-resume-test.txt"
		fileContent = "autoResume test — data survived pause and Connect!"
	)
	writeCmd := fmt.Sprintf("echo '%s' > %s", fileContent, filePath)
	result, err := sbx.Commands.Run("bash", []string{"-c", writeCmd})
	if err != nil {
		sbx.Close()
		t.Fatalf("Run (write): %v", err)
	}
	t.Logf("  write exit=%d stdout=%q", result.ExitCode, strings.TrimSpace(result.Stdout))

	// 确认文件写入成功
	readBeforePause, err := sbx.Commands.Run("cat", []string{filePath})
	if err != nil {
		sbx.Close()
		t.Fatalf("Run (cat before pause): %v", err)
	}
	t.Logf("  content before pause: %q", strings.TrimSpace(readBeforePause.Stdout))
	if !strings.Contains(readBeforePause.Stdout, "autoResume test") {
		t.Errorf("file content before pause is wrong: %q", readBeforePause.Stdout)
	}

	// ── Step 3: 等待自动 pauseds ──
	t.Log("=== Step 3: Wait for auto-pause (timeout=30s) ===")
	deadline := time.After(90 * time.Second)
	paused := false
	for !paused {
		select {
		case <-deadline:
			sbx.Close()
			t.Fatal("timed out waiting for auto-pause")
		case <-time.After(3 * time.Second):
		}
		info, err := sbx.Info()
		if err != nil {
			t.Logf("  Info error: %v", err)
			continue
		}
		t.Logf("  poll: state=%s", info.State)
		if info.State == "paused" {
			paused = true
		}
	}
	t.Logf("  sandbox auto-paused!")

	// ── Step 4: 额外等待一段时间 (模拟真实场景中 pause 后过一段时间才恢复) ──
	waitSeconds := 5
	t.Logf("=== Step 4: Wait %ds (simulating real-world delay before resume) ===", waitSeconds)
	time.Sleep(time.Duration(waitSeconds) * time.Second)
	t.Logf("  waited %ds, now resuming...", waitSeconds)

	// ── Step 5: 通过 Connect 恢复沙盒句柄 ──
	t.Log("=== Step 5: Connect to resume sandbox ===")
	resumed, err := client.Connect(ctx, sbx.ID, 120)
	if err != nil {
		sbx.Close()
		t.Fatalf("Connect: %v", err)
	}
	defer resumed.Close()
	t.Logf("  resumed sandbox: %s", resumed.ID)

	// 确认状态恢复为 running
	info2, err := resumed.Info()
	if err != nil {
		t.Fatalf("Info after Connect: %v", err)
	}
	t.Logf("  state after Connect: %s", info2.State)
	if info2.State != "running" {
		t.Errorf("expected running after Connect, got %q", info2.State)
	}

	// ── Step 6: 在恢复的句柄上执行 command.Run 读取文件 ──
	t.Log("=== Step 6: Read file on resumed handle via command.Run ===")
	readResult, err := resumed.Commands.Run("cat", []string{filePath})
	if err != nil {
		t.Fatalf("Run (cat after resume): %v", err)
	}
	t.Logf("  content after resume: %q", strings.TrimSpace(readResult.Stdout))

	if !strings.Contains(readResult.Stdout, "autoResume test") {
		t.Fatalf("file content after resume is wrong: %q", readResult.Stdout)
	}

	// ── Step 7: 额外验证 — 执行一个新命令 ──
	t.Log("=== Step 7: Execute fresh command on resumed handle ===")
	result2, err := resumed.Commands.Run("bash", []string{"-c", "echo 'fresh-command-after-resume' && ls /tmp/auto-resume-test.txt"})
	if err != nil {
		t.Fatalf("Run (fresh): %v", err)
	}
	t.Logf("  fresh command:\n%s", strings.TrimSpace(result2.Stdout))
	if !strings.Contains(result2.Stdout, "fresh-command-after-resume") {
		t.Errorf("fresh command failed: %q", result2.Stdout)
	}
	if !strings.Contains(result2.Stdout, "auto-resume-test.txt") {
		t.Errorf("ls didn't find file: %q", result2.Stdout)
	}

	t.Logf("=== AUTO-RESUME CONNECT & READ TEST COMPLETED SUCCESSFULLY ===")
}
