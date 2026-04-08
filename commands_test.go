package e2b

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
	"github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process/processconnect"
)

// --- Test server helpers ---

// testProcessServer is a minimal Connect ProcessHandler for testing.
// Only Start is implemented; all other methods return Unimplemented via the embedded type.
type testProcessServer struct {
	processconnect.UnimplementedProcessHandler
	startFn func(context.Context, *connect.Request[processpb.StartRequest], *connect.ServerStream[processpb.StartResponse]) error
}

func (s *testProcessServer) Start(ctx context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
	return s.startFn(ctx, req, stream)
}

// newTestSandbox sets up a TLS Connect server backed by startFn and returns a Sandbox
// that routes all envd traffic to it.
func newTestSandbox(t *testing.T, startFn func(context.Context, *connect.Request[processpb.StartRequest], *connect.ServerStream[processpb.StartResponse]) error) *Sandbox {
	t.Helper()
	mux := http.NewServeMux()
	path, handler := processconnect.NewProcessHandler(&testProcessServer{startFn: startFn})
	mux.Handle(path, handler)

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	origTransport := srv.Client().Transport
	sbx := &Sandbox{
		ID:            "sbx-test",
		accessToken:   "token-test",
		apiKey:        "key-test",
		sandboxDomain: "test.e2b.app",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "https"
				req.URL.Host = srv.Listener.Addr().String()
				return origTransport.RoundTrip(req)
			}),
		},
	}
	sbx.Commands = newCommandService(sbx)
	return sbx
}

// sendEvent is a convenience helper to stream a ProcessEvent to the client.
func sendEvent(stream *connect.ServerStream[processpb.StartResponse], event *processpb.ProcessEvent) error {
	return stream.Send(&processpb.StartResponse{Event: event})
}

func startEvent(pid uint32) *processpb.ProcessEvent {
	return &processpb.ProcessEvent{Event: &processpb.ProcessEvent_Start{
		Start: &processpb.ProcessEvent_StartEvent{Pid: pid},
	}}
}

func stdoutEvent(data []byte) *processpb.ProcessEvent {
	return &processpb.ProcessEvent{Event: &processpb.ProcessEvent_Data{
		Data: &processpb.ProcessEvent_DataEvent{
			Output: &processpb.ProcessEvent_DataEvent_Stdout{Stdout: data},
		},
	}}
}

func stderrEvent(data []byte) *processpb.ProcessEvent {
	return &processpb.ProcessEvent{Event: &processpb.ProcessEvent_Data{
		Data: &processpb.ProcessEvent_DataEvent{
			Output: &processpb.ProcessEvent_DataEvent_Stderr{Stderr: data},
		},
	}}
}

func endEvent(exitCode int32, exited bool) *processpb.ProcessEvent {
	return &processpb.ProcessEvent{Event: &processpb.ProcessEvent_End{
		End: &processpb.ProcessEvent_EndEvent{ExitCode: exitCode, Exited: exited},
	}}
}

// roundTripFunc allows using a function as an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// --- Tests ---

func TestRunSuccess(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		if req.Msg.Process.Cmd != "echo" {
			t.Errorf("cmd = %q, want %q", req.Msg.Process.Cmd, "echo")
		}
		if len(req.Msg.Process.Args) != 1 || req.Msg.Process.Args[0] != "hello" {
			t.Errorf("args = %v, want [hello]", req.Msg.Process.Args)
		}
		if req.Header().Get("X-Access-Token") != "token-test" {
			t.Errorf("X-Access-Token missing or wrong")
		}
		_ = sendEvent(stream, startEvent(10))
		_ = sendEvent(stream, stdoutEvent([]byte("hello\n")))
		_ = sendEvent(stream, endEvent(0, true))
		return nil
	})

	result, err := sbx.Commands.Run("echo", []string{"hello"})
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

func TestRunStderr(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendEvent(stream, stdoutEvent([]byte("out\n")))
		_ = sendEvent(stream, stderrEvent([]byte("err\n")))
		_ = sendEvent(stream, endEvent(0, true))
		return nil
	})

	result, err := sbx.Commands.Run("cmd", nil)
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

func TestRunNonZeroExit(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendEvent(stream, endEvent(127, true))
		return nil
	})

	result, err := sbx.Commands.Run("cmd", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 127 {
		t.Errorf("exit code = %d, want 127", result.ExitCode)
	}
}

func TestRunWithOptions(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		if req.Msg.Process.Envs["FOO"] != "bar" {
			t.Errorf("envs[FOO] = %q, want %q", req.Msg.Process.Envs["FOO"], "bar")
		}
		if req.Msg.Process.GetCwd() != "/tmp" {
			t.Errorf("cwd = %q, want %q", req.Msg.Process.GetCwd(), "/tmp")
		}
		if req.Header().Get("User") != "root" {
			t.Errorf("User header = %q, want %q", req.Header().Get("User"), "root")
		}
		_ = sendEvent(stream, endEvent(0, true))
		return nil
	})

	_, err := sbx.Commands.Run("cmd", nil,
		WithEnv(map[string]string{"FOO": "bar"}),
		WithCwd("/tmp"),
		WithUser("root"),
		WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunStreamError(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], _ *connect.ServerStream[processpb.StartResponse]) error {
		return connect.NewError(connect.CodeDeadlineExceeded, errors.New("command timed out"))
	})

	_, err := sbx.Commands.Run("cmd", nil)
	if err == nil {
		t.Fatal("expected error from stream")
	}
}

func TestRunWithContext(t *testing.T) {
	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
		_ = sendEvent(stream, stdoutEvent([]byte("ctx\n")))
		_ = sendEvent(stream, endEvent(0, true))
		return nil
	})

	result, err := sbx.Commands.RunWithContext(context.Background(), "cmd", nil)
	if err != nil {
		t.Fatalf("RunWithContext: %v", err)
	}
	if result.Stdout != "ctx\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "ctx\n")
	}
}

func TestRunCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sbx := newTestSandbox(t, func(_ context.Context, _ *connect.Request[processpb.StartRequest], _ *connect.ServerStream[processpb.StartResponse]) error {
		return nil
	})

	_, err := sbx.Commands.RunWithContext(ctx, "cmd", nil)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}
