//go:build integration

package e2b

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// Run with:
//
//	E2B_API_KEY=e2b_xxx go test -tags=integration -v -run TestIntegrationCommands ./...
//	E2B_API_KEY=e2b_xxx go test -tags=integration -v -run TestIntegrationPty ./...

// --- Commands: basic run ---

func TestIntegrationCommandsRunEcho(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	result, err := sbx.Commands.Run(ctx, "echo hello world")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stdout != "hello world\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "hello world\n")
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	t.Logf("run echo: stdout=%q exit=%d", result.Stdout, result.ExitCode)
}

func TestIntegrationCommandsShellFeatures(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// Pipes, redirection, and env expansion prove the login-shell wrapping.
	result, err := sbx.Commands.Run(ctx, "echo one two three | wc -w")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "3" {
		t.Errorf("stdout = %q, want 3", result.Stdout)
	}
}

func TestIntegrationCommandsStderr(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	result, err := sbx.Commands.Run(ctx, "echo to-stderr 1>&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(result.Stderr) != "to-stderr" {
		t.Errorf("stderr = %q, want to-stderr", result.Stderr)
	}
	if result.Stdout != "" {
		t.Errorf("stdout = %q, want empty", result.Stdout)
	}
}

func TestIntegrationCommandsNonZeroExit(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	result, err := sbx.Commands.Run(ctx, "exit 3")
	if err == nil {
		t.Fatal("expected CommandExitError for non-zero exit")
	}
	var exitErr *CommandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want *CommandExitError", err)
	}
	if exitErr.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", exitErr.ExitCode)
	}
	if result == nil || result.ExitCode != 3 {
		t.Errorf("result = %+v, want ExitCode 3", result)
	}
}

func TestIntegrationCommandsEnvAndCwd(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	result, err := sbx.Commands.Run(ctx, "echo $MY_VAR in $(pwd)",
		WithEnv(map[string]string{"MY_VAR": "custom-value"}),
		WithCwd("/tmp"),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(result.Stdout, "custom-value in /tmp") {
		t.Errorf("stdout = %q, want to contain 'custom-value in /tmp'", result.Stdout)
	}
}

func TestIntegrationCommandsUser(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	result, err := sbx.Commands.Run(ctx, "whoami", WithUser("root"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "root" {
		t.Errorf("whoami = %q, want root", result.Stdout)
	}
}

// --- Commands: streaming ---

func TestIntegrationCommandsStreaming(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	var mu sync.Mutex
	var lines []string
	firstAt := time.Time{}
	lastAt := time.Time{}

	// Print 5 lines with a delay between each; assert the callback fires
	// incrementally rather than all at once at the end.
	handle, err := sbx.Commands.Start(ctx, "for i in 1 2 3 4 5; do echo line$i; sleep 0.3; done",
		WithOnStdout(func(b []byte) {
			mu.Lock()
			now := time.Now()
			if firstAt.IsZero() {
				firstAt = now
			}
			lastAt = now
			lines = append(lines, string(b))
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	result, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(lines, "")
	for i := 1; i <= 5; i++ {
		if !strings.Contains(joined, "line") {
			t.Errorf("missing output, got %q", joined)
			break
		}
	}
	if !strings.Contains(result.Stdout, "line1") || !strings.Contains(result.Stdout, "line5") {
		t.Errorf("result stdout missing lines: %q", result.Stdout)
	}
	// The spread between first and last callback should reflect the sleeps,
	// confirming incremental delivery (not a single terminal flush).
	spread := lastAt.Sub(firstAt)
	t.Logf("streaming spread = %v across %d callbacks", spread, len(lines))
	if spread < 300*time.Millisecond {
		t.Errorf("callbacks arrived too close together (%v); expected incremental streaming", spread)
	}
}

// --- Commands: background + list + kill ---

func TestIntegrationCommandsBackgroundListKill(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	handle, err := sbx.Commands.Start(ctx, "sleep 300")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := handle.PID()
	t.Logf("background pid = %d", pid)

	// The process should appear in the list.
	procs, err := sbx.Commands.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, p := range procs {
		if p.PID == pid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("pid %d not found in process list (%d processes)", pid, len(procs))
	}

	// Kill it.
	killed, err := sbx.Commands.Kill(ctx, pid)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if !killed {
		t.Error("Kill returned false, want true")
	}

	// A second kill of the now-gone process returns false.
	killedAgain, err := sbx.Commands.Kill(ctx, pid)
	if err != nil {
		t.Fatalf("second Kill: %v", err)
	}
	if killedAgain {
		t.Error("second Kill returned true, want false")
	}

	_ = handle.Disconnect()
}

// --- Commands: stdin ---

func TestIntegrationCommandsStdin(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// `cat` echoes stdin to stdout. Start it with stdin open, feed a line,
	// close stdin to signal EOF, then wait for it to finish.
	handle, err := sbx.Commands.Start(ctx, "cat", WithStdin(true))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := handle.SendStdin(ctx, []byte("hello stdin\n")); err != nil {
		t.Fatalf("SendStdin: %v", err)
	}
	if err := handle.CloseStdin(ctx); err != nil {
		t.Fatalf("CloseStdin: %v", err)
	}

	result, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello stdin") {
		t.Errorf("stdout = %q, want to contain 'hello stdin'", result.Stdout)
	}
}

// --- Commands: connect (reattach) ---

func TestIntegrationCommandsConnect(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	// Start a background command that emits output over time, then disconnect
	// and reconnect to it by PID.
	handle, err := sbx.Commands.Start(ctx, "for i in 1 2 3; do echo tick$i; sleep 0.5; done")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	pid := handle.PID()
	if err := handle.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	var mu sync.Mutex
	var got string
	reconnected, err := sbx.Commands.Connect(ctx, pid, WithConnectOnStdout(func(b []byte) {
		mu.Lock()
		got += string(b)
		mu.Unlock()
	}))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := reconnected.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// We should observe at least the tail of the output produced after we
	// reattached.
	if !strings.Contains(got, "tick") {
		t.Errorf("reconnected output = %q, want to contain 'tick'", got)
	}
	t.Logf("reconnected output = %q", got)
}

// --- Commands: unicode ---

func TestIntegrationCommandsUnicode(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	result, err := sbx.Commands.Run(ctx, "printf 'héllo 🌍 wörld'")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stdout != "héllo 🌍 wörld" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "héllo 🌍 wörld")
	}
}

// --- Commands: timeout ---

func TestIntegrationCommandsTimeout(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	start := time.Now()
	_, err := sbx.Commands.Run(ctx, "sleep 30", WithTimeout(2*time.Second))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 10*time.Second {
		t.Errorf("timeout took %v, expected it to fire near 2s", elapsed)
	}
	t.Logf("timeout fired after %v with error: %v", elapsed, err)
}

// --- PTY ---

func TestIntegrationPtyEcho(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	var mu sync.Mutex
	var buf []byte
	handle, err := sbx.Pty.Create(ctx, 80, 24, WithPtyOnData(func(b []byte) {
		mu.Lock()
		buf = append(buf, b...)
		mu.Unlock()
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pid := handle.PID()
	t.Logf("pty pid = %d", pid)

	// Drive the terminal: send a command that prints TERM, then exit the shell
	// so Wait returns.
	if err := sbx.Pty.SendInput(ctx, pid, []byte("echo TERM=$TERM\n")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if err := sbx.Pty.SendInput(ctx, pid, []byte("exit\n")); err != nil {
		t.Fatalf("SendInput exit: %v", err)
	}

	if _, err := handle.Wait(ctx); err != nil {
		// A PTY shell that exits may report a non-zero code depending on the
		// last command; we only care that it terminated and produced output.
		var exitErr *CommandExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("Wait: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	out := string(buf)
	if !strings.Contains(out, "TERM=xterm-256color") {
		t.Errorf("pty output = %q, want to contain TERM=xterm-256color", out)
	}
}

func TestIntegrationPtyResize(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	var mu sync.Mutex
	var buf []byte
	handle, err := sbx.Pty.Create(ctx, 80, 24, WithPtyOnData(func(b []byte) {
		mu.Lock()
		buf = append(buf, b...)
		mu.Unlock()
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pid := handle.PID()

	// Resize, then ask the shell for the new column count.
	if err := sbx.Pty.Resize(ctx, pid, 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := sbx.Pty.SendInput(ctx, pid, []byte("tput cols\n")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if err := sbx.Pty.SendInput(ctx, pid, []byte("exit\n")); err != nil {
		t.Fatalf("SendInput exit: %v", err)
	}

	if _, err := handle.Wait(ctx); err != nil {
		var exitErr *CommandExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("Wait: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(string(buf), "120") {
		t.Logf("pty output after resize = %q", string(buf))
		t.Errorf("expected column count 120 in output")
	}
}

func TestIntegrationPtyKill(t *testing.T) {
	sbx := newIntegrationSandbox(t)
	ctx := context.Background()

	handle, err := sbx.Pty.Create(ctx, 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	pid := handle.PID()

	killed, err := sbx.Pty.Kill(ctx, pid)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if !killed {
		t.Error("Kill returned false, want true")
	}
	_ = handle.Disconnect()
}
