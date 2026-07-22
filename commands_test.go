package e2b

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
)

// --- Run / Start ---

func TestRunSuccess(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		// Commands are wrapped in a login shell.
		if req.Msg.Process.Cmd != "/bin/bash" {
			t.Errorf("cmd = %q, want /bin/bash", req.Msg.Process.Cmd)
		}
		wantArgs := []string{"-l", "-c", "echo hello"}
		if strings.Join(req.Msg.Process.Args, "\x00") != strings.Join(wantArgs, "\x00") {
			t.Errorf("args = %v, want %v", req.Msg.Process.Args, wantArgs)
		}
		if req.Header().Get("X-Access-Token") != "token-test" {
			t.Errorf("X-Access-Token missing or wrong")
		}
		_ = sendStart(stream, startEvent(10))
		_ = sendStart(stream, stdoutEvent([]byte("hello\n")))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	result, err := sbx.Commands.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "hello\n")
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestRunStdoutAndStderr(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, stdoutEvent([]byte("out\n")))
		_ = sendStart(stream, stderrEvent([]byte("err\n")))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	result, err := sbx.Commands.Run(context.Background(), "cmd")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stdout != "out\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "out\n")
	}
	if result.Stderr != "err\n" {
		t.Errorf("stderr = %q, want %q", result.Stderr, "err\n")
	}
}

func TestRunNonZeroExitReturnsCommandExitError(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, stdoutEvent([]byte("partial\n")))
		_ = sendStart(stream, endEventErr(127, "command not found"))
		return nil
	})

	result, err := sbx.Commands.Run(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected CommandExitError, got nil")
	}
	var exitErr *CommandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want *CommandExitError", err)
	}
	if exitErr.ExitCode != 127 {
		t.Errorf("exit code = %d, want 127", exitErr.ExitCode)
	}
	if exitErr.Message != "command not found" {
		t.Errorf("message = %q, want %q", exitErr.Message, "command not found")
	}
	if exitErr.Stdout != "partial\n" {
		t.Errorf("stdout = %q, want %q", exitErr.Stdout, "partial\n")
	}
	// The result is still returned alongside the error.
	if result == nil || result.ExitCode != 127 {
		t.Errorf("result = %+v, want ExitCode 127", result)
	}
}

func TestRunWithOptions(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		if req.Msg.Process.Envs["FOO"] != "bar" {
			t.Errorf("envs[FOO] = %q, want bar", req.Msg.Process.Envs["FOO"])
		}
		if req.Msg.Process.GetCwd() != "/tmp" {
			t.Errorf("cwd = %q, want /tmp", req.Msg.Process.GetCwd())
		}
		// The user is carried via HTTP Basic auth, base64("root:").
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("root:"))
		if got := req.Header().Get("Authorization"); got != wantAuth {
			t.Errorf("Authorization = %q, want %q", got, wantAuth)
		}
		if !req.Msg.GetStdin() {
			t.Errorf("stdin = false, want true")
		}
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	_, err := sbx.Commands.Run(context.Background(), "cmd",
		WithEnv(map[string]string{"FOO": "bar"}),
		WithCwd("/tmp"),
		WithUser("root"),
		WithStdin(true),
		WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunDefaultUserAuthHeader(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		// No user set → defaults to "user".
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:"))
		if got := req.Header().Get("Authorization"); got != wantAuth {
			t.Errorf("Authorization = %q, want %q", got, wantAuth)
		}
		if req.Header().Get(keepAlivePingHeader) != keepAlivePingIntervalSecStr {
			t.Errorf("keepalive header = %q, want %q", req.Header().Get(keepAlivePingHeader), keepAlivePingIntervalSecStr)
		}
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	if _, err := sbx.Commands.Run(context.Background(), "cmd"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestStartBackgroundThenWait(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(42))
		_ = sendStart(stream, stdoutEvent([]byte("done\n")))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	handle, err := sbx.Commands.Start(context.Background(), "cmd")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if handle.PID() != 42 {
		t.Errorf("pid = %d, want 42", handle.PID())
	}
	// Not finished until Wait drains the stream.
	if _, done := handle.ExitCode(); done {
		t.Errorf("ExitCode reported done before Wait")
	}

	result, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Stdout != "done\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "done\n")
	}
	if code, done := handle.ExitCode(); !done || code != 0 {
		t.Errorf("ExitCode = (%d, %v), want (0, true)", code, done)
	}
}

func TestRunStreamingCallbacks(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, stdoutEvent([]byte("line1\n")))
		_ = sendStart(stream, stdoutEvent([]byte("line2\n")))
		_ = sendStart(stream, stderrEvent([]byte("warn\n")))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	var mu sync.Mutex
	var stdout, stderr []string
	handle, err := sbx.Commands.Start(context.Background(), "cmd",
		WithOnStdout(func(b []byte) { mu.Lock(); stdout = append(stdout, string(b)); mu.Unlock() }),
		WithOnStderr(func(b []byte) { mu.Lock(); stderr = append(stderr, string(b)); mu.Unlock() }),
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if strings.Join(stdout, "") != "line1\nline2\n" {
		t.Errorf("stdout callbacks = %v", stdout)
	}
	if strings.Join(stderr, "") != "warn\n" {
		t.Errorf("stderr callbacks = %v", stderr)
	}
}

func TestWaitOptionCallbacks(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, stdoutEvent([]byte("hi")))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	handle, err := sbx.Commands.Start(context.Background(), "cmd")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	var got string
	if _, err := handle.Wait(context.Background(), WithWaitOnStdout(func(b []byte) { got += string(b) })); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got != "hi" {
		t.Errorf("wait callback got %q, want %q", got, "hi")
	}
}

func TestRunUTF8SplitAcrossFrames(t *testing.T) {
	// "🌍" is 4 bytes; deliver it split across two stdout frames.
	emoji := []byte("🌍")
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, stdoutEvent(emoji[:2]))
		_ = sendStart(stream, stdoutEvent(emoji[2:]))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	result, err := sbx.Commands.Run(context.Background(), "cmd")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stdout != "🌍" {
		t.Errorf("stdout = %q, want 🌍", result.Stdout)
	}
}

func TestRunExitCodeVisibleAfterEarlyStop(t *testing.T) {
	// The end event must record the result even though it also flushes; a
	// consumer inspecting ExitCode after Wait sees the code.
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(1))
		// A trailing partial rune with no continuation, flushed on end.
		_ = sendStart(stream, stdoutEvent([]byte{0xE2}))
		_ = sendStart(stream, endEvent(5, true))
		return nil
	})

	result, err := sbx.Commands.Run(context.Background(), "cmd")
	if err == nil {
		t.Fatal("expected CommandExitError")
	}
	var exitErr *CommandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want *CommandExitError", err)
	}
	if exitErr.ExitCode != 5 {
		t.Errorf("exit code = %d, want 5", exitErr.ExitCode)
	}
	// The incomplete byte surfaces as a replacement char on flush.
	if result.Stdout != "�" {
		t.Errorf("stdout = %q, want replacement char", result.Stdout)
	}
}

func TestStartStreamError(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], _ *connect.ServerStream[processpb.StartResponse]) error {
		return connect.NewError(connect.CodeDeadlineExceeded, errors.New("command timed out"))
	})

	_, err := sbx.Commands.Run(context.Background(), "cmd")
	if err == nil {
		t.Fatal("expected error from stream")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error = %T, want *TimeoutError", err)
	}
}

func TestStartNoStartEvent(t *testing.T) {
	// First event is data, not start → error.
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, stdoutEvent([]byte("oops")))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	_, err := sbx.Commands.Start(context.Background(), "cmd")
	if err == nil {
		t.Fatal("expected error for missing start event")
	}
}

func TestStartStreamClosedBeforeStart(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], _ *connect.ServerStream[processpb.StartResponse]) error {
		return nil // closes with no events
	})

	_, err := sbx.Commands.Start(context.Background(), "cmd")
	if err == nil {
		t.Fatal("expected error for stream closed before start event")
	}
}

func TestRunCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], _ *connect.ServerStream[processpb.StartResponse]) error {
		return nil
	})

	_, err := sbx.Commands.Run(ctx, "cmd")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestRunNoEndEvent(t *testing.T) {
	// Stream closes cleanly after start+data but with no end event.
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, stdoutEvent([]byte("partial")))
		return nil
	})

	_, err := sbx.Commands.Run(context.Background(), "cmd")
	if err == nil {
		t.Fatal("expected error for missing end event")
	}
	if !strings.Contains(err.Error(), "without an end event") {
		t.Errorf("error = %v, want 'without an end event'", err)
	}
}

// --- List ---

func TestList(t *testing.T) {
	cwd := "/home/user"
	tag := "start-cmd"
	sbx := newTestSandboxWith(t, &testProcessServer{
		listFn: func(_ context.Context, _ *connect.Request[processpb.ListRequest]) (*connect.Response[processpb.ListResponse], error) {
			return connect.NewResponse(&processpb.ListResponse{
				Processes: []*processpb.ProcessInfo{
					{
						Pid: 7,
						Tag: &tag,
						Config: &processpb.ProcessConfig{
							Cmd:  "/bin/bash",
							Args: []string{"-l", "-c", "sleep 100"},
							Envs: map[string]string{"K": "V"},
							Cwd:  &cwd,
						},
					},
				},
			}), nil
		},
	})

	procs, err := sbx.Commands.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(procs) != 1 {
		t.Fatalf("len(procs) = %d, want 1", len(procs))
	}
	p := procs[0]
	if p.PID != 7 || p.Tag != "start-cmd" || p.Cmd != "/bin/bash" || p.Cwd != "/home/user" {
		t.Errorf("process = %+v", p)
	}
	if len(p.Args) != 3 || p.Envs["K"] != "V" {
		t.Errorf("args/envs = %v / %v", p.Args, p.Envs)
	}
}

func TestListError(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		listFn: func(_ context.Context, _ *connect.Request[processpb.ListRequest]) (*connect.Response[processpb.ListResponse], error) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bad token"))
		},
	})

	_, err := sbx.Commands.List(context.Background())
	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %T, want *AuthenticationError", err)
	}
}

// --- Kill ---

func TestKillSuccess(t *testing.T) {
	var gotPID uint32
	var gotSignal processpb.Signal
	sbx := newTestSandboxWith(t, &testProcessServer{
		sendSignalFn: func(_ context.Context, req *connect.Request[processpb.SendSignalRequest]) (*connect.Response[processpb.SendSignalResponse], error) {
			gotPID = req.Msg.GetProcess().GetPid()
			gotSignal = req.Msg.GetSignal()
			return connect.NewResponse(&processpb.SendSignalResponse{}), nil
		},
	})

	ok, err := sbx.Commands.Kill(context.Background(), 99)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if !ok {
		t.Error("Kill returned false, want true")
	}
	if gotPID != 99 {
		t.Errorf("pid = %d, want 99", gotPID)
	}
	if gotSignal != processpb.Signal_SIGNAL_SIGKILL {
		t.Errorf("signal = %v, want SIGKILL", gotSignal)
	}
}

func TestKillNotFound(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		sendSignalFn: func(_ context.Context, _ *connect.Request[processpb.SendSignalRequest]) (*connect.Response[processpb.SendSignalResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("no such process"))
		},
	})

	ok, err := sbx.Commands.Kill(context.Background(), 1)
	if err != nil {
		t.Fatalf("Kill returned error for NotFound, want nil: %v", err)
	}
	if ok {
		t.Error("Kill returned true for missing process, want false")
	}
}

func TestKillOtherError(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		sendSignalFn: func(_ context.Context, _ *connect.Request[processpb.SendSignalRequest]) (*connect.Response[processpb.SendSignalResponse], error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("boom"))
		},
	})

	ok, err := sbx.Commands.Kill(context.Background(), 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if ok {
		t.Error("ok = true on error, want false")
	}
}

// --- SendStdin / CloseStdin ---

func TestSendStdin(t *testing.T) {
	var gotPID uint32
	var gotData []byte
	sbx := newTestSandboxWith(t, &testProcessServer{
		sendInputFn: func(_ context.Context, req *connect.Request[processpb.SendInputRequest]) (*connect.Response[processpb.SendInputResponse], error) {
			gotPID = req.Msg.GetProcess().GetPid()
			gotData = req.Msg.GetInput().GetStdin()
			return connect.NewResponse(&processpb.SendInputResponse{}), nil
		},
	})

	if err := sbx.Commands.SendStdin(context.Background(), 5, []byte("hello\n")); err != nil {
		t.Fatalf("SendStdin: %v", err)
	}
	if gotPID != 5 {
		t.Errorf("pid = %d, want 5", gotPID)
	}
	if string(gotData) != "hello\n" {
		t.Errorf("data = %q, want %q", gotData, "hello\n")
	}
}

func TestCloseStdin(t *testing.T) {
	var gotPID uint32
	called := false
	sbx := newTestSandboxWith(t, &testProcessServer{
		closeStdinFn: func(_ context.Context, req *connect.Request[processpb.CloseStdinRequest]) (*connect.Response[processpb.CloseStdinResponse], error) {
			called = true
			gotPID = req.Msg.GetProcess().GetPid()
			return connect.NewResponse(&processpb.CloseStdinResponse{}), nil
		},
	})

	if err := sbx.Commands.CloseStdin(context.Background(), 8); err != nil {
		t.Fatalf("CloseStdin: %v", err)
	}
	if !called || gotPID != 8 {
		t.Errorf("called=%v pid=%d, want true/8", called, gotPID)
	}
}

func TestHandleSendStdinDelegates(t *testing.T) {
	var gotData []byte
	sbx := newTestSandboxWith(t, &testProcessServer{
		startFn: func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
			_ = sendStart(stream, startEvent(3))
			_ = sendStart(stream, endEvent(0, true))
			return nil
		},
		sendInputFn: func(_ context.Context, req *connect.Request[processpb.SendInputRequest]) (*connect.Response[processpb.SendInputResponse], error) {
			gotData = req.Msg.GetInput().GetStdin()
			return connect.NewResponse(&processpb.SendInputResponse{}), nil
		},
	})

	handle, err := sbx.Commands.Start(context.Background(), "cat", WithStdin(true))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := handle.SendStdin(context.Background(), []byte("via handle")); err != nil {
		t.Fatalf("handle.SendStdin: %v", err)
	}
	if string(gotData) != "via handle" {
		t.Errorf("data = %q, want %q", gotData, "via handle")
	}
	_, _ = handle.Wait(context.Background())
}

// --- Connect ---

func TestConnectToRunningProcess(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		connectFn: func(_ context.Context, req *connect.Request[processpb.ConnectRequest], stream *connect.ServerStream[processpb.ConnectResponse]) error {
			if req.Msg.GetProcess().GetPid() != 21 {
				t.Errorf("connect pid = %d, want 21", req.Msg.GetProcess().GetPid())
			}
			_ = sendConnect(stream, startEvent(21))
			_ = sendConnect(stream, stdoutEvent([]byte("resumed\n")))
			_ = sendConnect(stream, endEvent(0, true))
			return nil
		},
	})

	handle, err := sbx.Commands.Connect(context.Background(), 21)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if handle.PID() != 21 {
		t.Errorf("pid = %d, want 21", handle.PID())
	}
	result, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Stdout != "resumed\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "resumed\n")
	}
}

// --- Disconnect ---

func TestDisconnectStopsCallbacks(t *testing.T) {
	// The server blocks after the first chunk until the test signals; this lets
	// us disconnect deterministically before the remaining output is dispatched.
	release := make(chan struct{})
	sbx := newTestSandboxWith(t, &testProcessServer{
		startFn: func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
			_ = sendStart(stream, startEvent(1))
			_ = sendStart(stream, stdoutEvent([]byte("first\n")))
			<-release
			_ = sendStart(stream, stdoutEvent([]byte("second\n")))
			_ = sendStart(stream, endEvent(0, true))
			return nil
		},
	})

	handle, err := sbx.Commands.Start(context.Background(), "cmd")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var mu sync.Mutex
	var chunks []string
	firstSeen := make(chan struct{})
	var once sync.Once
	waitDone := make(chan error, 1)
	go func() {
		_, werr := handle.Wait(context.Background(), WithWaitOnStdout(func(b []byte) {
			mu.Lock()
			chunks = append(chunks, string(b))
			mu.Unlock()
			once.Do(func() { close(firstSeen) })
		}))
		waitDone <- werr
	}()

	<-firstSeen
	_ = handle.Disconnect()
	close(release)
	<-waitDone

	mu.Lock()
	defer mu.Unlock()
	if len(chunks) != 1 || chunks[0] != "first\n" {
		t.Errorf("chunks = %v, want only [first]", chunks)
	}
}

func TestWaitIdempotent(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendStart(stream, startEvent(1))
		_ = sendStart(stream, stdoutEvent([]byte("once")))
		_ = sendStart(stream, endEvent(0, true))
		return nil
	})

	handle, err := sbx.Commands.Start(context.Background(), "cmd")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	r1, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait #1: %v", err)
	}
	r2, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait #2: %v", err)
	}
	if r1.Stdout != "once" || r2.Stdout != "once" {
		t.Errorf("results differ: %q vs %q", r1.Stdout, r2.Stdout)
	}
}
