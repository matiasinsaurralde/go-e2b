package e2b

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
)

func TestPtyCreate(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		startFn: func(_ context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
			// PTY runs an interactive login shell.
			if req.Msg.Process.Cmd != "/bin/bash" {
				t.Errorf("cmd = %q, want /bin/bash", req.Msg.Process.Cmd)
			}
			wantArgs := []string{"-i", "-l"}
			if strings.Join(req.Msg.Process.Args, "\x00") != strings.Join(wantArgs, "\x00") {
				t.Errorf("args = %v, want %v", req.Msg.Process.Args, wantArgs)
			}
			// PTY size is set.
			pty := req.Msg.GetPty()
			if pty == nil || pty.GetSize().GetCols() != 80 || pty.GetSize().GetRows() != 24 {
				t.Errorf("pty size = %+v, want 80x24", pty.GetSize())
			}
			// Default terminal envs.
			if req.Msg.Process.Envs["TERM"] != "xterm-256color" {
				t.Errorf("TERM = %q, want xterm-256color", req.Msg.Process.Envs["TERM"])
			}
			if req.Msg.Process.Envs["LANG"] != "C.UTF-8" || req.Msg.Process.Envs["LC_ALL"] != "C.UTF-8" {
				t.Errorf("LANG/LC_ALL not defaulted: %v", req.Msg.Process.Envs)
			}
			_ = sendStart(stream, startEvent(55))
			_ = sendStart(stream, ptyEvent([]byte("\x1b[0m$ ")))
			_ = sendStart(stream, endEvent(0, true))
			return nil
		},
	})

	var mu sync.Mutex
	var data []byte
	handle, err := sbx.Pty.Create(context.Background(), 80, 24, WithPtyOnData(func(b []byte) {
		mu.Lock()
		data = append(data, b...)
		mu.Unlock()
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if handle.PID() != 55 {
		t.Errorf("pid = %d, want 55", handle.PID())
	}
	if _, err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if string(data) != "\x1b[0m$ " {
		t.Errorf("pty data = %q, want the raw escape sequence", data)
	}
}

func TestPtyCreateEnvOverride(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		startFn: func(_ context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
			if req.Msg.Process.Envs["TERM"] != "vt100" {
				t.Errorf("TERM = %q, want overridden vt100", req.Msg.Process.Envs["TERM"])
			}
			if req.Msg.Process.Envs["EXTRA"] != "1" {
				t.Errorf("EXTRA = %q, want 1", req.Msg.Process.Envs["EXTRA"])
			}
			// Basic auth carries the user.
			wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("root:"))
			if got := req.Header().Get("Authorization"); got != wantAuth {
				t.Errorf("Authorization = %q, want %q", got, wantAuth)
			}
			_ = sendStart(stream, startEvent(1))
			_ = sendStart(stream, endEvent(0, true))
			return nil
		},
	})

	handle, err := sbx.Pty.Create(context.Background(), 100, 40,
		WithPtyEnv(map[string]string{"TERM": "vt100", "EXTRA": "1"}),
		WithPtyUser("root"),
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestPtySendInput(t *testing.T) {
	var gotPID uint32
	var gotData []byte
	sbx := newTestSandboxWith(t, &testProcessServer{
		sendInputFn: func(_ context.Context, req *connect.Request[processpb.SendInputRequest]) (*connect.Response[processpb.SendInputResponse], error) {
			gotPID = req.Msg.GetProcess().GetPid()
			gotData = req.Msg.GetInput().GetPty()
			return connect.NewResponse(&processpb.SendInputResponse{}), nil
		},
	})

	if err := sbx.Pty.SendInput(context.Background(), 12, []byte("ls\n")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if gotPID != 12 {
		t.Errorf("pid = %d, want 12", gotPID)
	}
	if string(gotData) != "ls\n" {
		t.Errorf("data = %q, want %q", gotData, "ls\n")
	}
}

func TestPtyResize(t *testing.T) {
	var gotCols, gotRows uint32
	var gotPID uint32
	sbx := newTestSandboxWith(t, &testProcessServer{
		updateFn: func(_ context.Context, req *connect.Request[processpb.UpdateRequest]) (*connect.Response[processpb.UpdateResponse], error) {
			gotPID = req.Msg.GetProcess().GetPid()
			gotCols = req.Msg.GetPty().GetSize().GetCols()
			gotRows = req.Msg.GetPty().GetSize().GetRows()
			return connect.NewResponse(&processpb.UpdateResponse{}), nil
		},
	})

	if err := sbx.Pty.Resize(context.Background(), 9, 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if gotPID != 9 || gotCols != 120 || gotRows != 40 {
		t.Errorf("resize = pid %d %dx%d, want 9 120x40", gotPID, gotCols, gotRows)
	}
}

func TestPtyKill(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		sendSignalFn: func(_ context.Context, req *connect.Request[processpb.SendSignalRequest]) (*connect.Response[processpb.SendSignalResponse], error) {
			if req.Msg.GetSignal() != processpb.Signal_SIGNAL_SIGKILL {
				t.Errorf("signal = %v, want SIGKILL", req.Msg.GetSignal())
			}
			return connect.NewResponse(&processpb.SendSignalResponse{}), nil
		},
	})

	ok, err := sbx.Pty.Kill(context.Background(), 3)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if !ok {
		t.Error("Kill returned false, want true")
	}
}

func TestPtyKillNotFound(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		sendSignalFn: func(_ context.Context, _ *connect.Request[processpb.SendSignalRequest]) (*connect.Response[processpb.SendSignalResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("gone"))
		},
	})

	ok, err := sbx.Pty.Kill(context.Background(), 3)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if ok {
		t.Error("Kill returned true for missing pty, want false")
	}
}

func TestPtyConnect(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		connectFn: func(_ context.Context, req *connect.Request[processpb.ConnectRequest], stream *connect.ServerStream[processpb.ConnectResponse]) error {
			if req.Msg.GetProcess().GetPid() != 77 {
				t.Errorf("connect pid = %d, want 77", req.Msg.GetProcess().GetPid())
			}
			_ = sendConnect(stream, startEvent(77))
			_ = sendConnect(stream, ptyEvent([]byte("output")))
			_ = sendConnect(stream, endEvent(0, true))
			return nil
		},
	})

	var data []byte
	handle, err := sbx.Pty.Connect(context.Background(), 77, WithPtyOnData(func(b []byte) {
		data = append(data, b...)
	}))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if string(data) != "output" {
		t.Errorf("data = %q, want %q", data, "output")
	}
}

func TestPtyHandleStdinUnsupported(t *testing.T) {
	sbx := newTestSandboxWith(t, &testProcessServer{
		startFn: func(_ context.Context, _ *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
			_ = sendStart(stream, startEvent(1))
			_ = sendStart(stream, endEvent(0, true))
			return nil
		},
	})

	handle, err := sbx.Pty.Create(context.Background(), 80, 24)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// PTY handles do not support the stdin helpers; use Pty.SendInput instead.
	if err := handle.SendStdin(context.Background(), []byte("x")); err == nil {
		t.Error("expected SendStdin to be unsupported on a PTY handle")
	}
	if err := handle.CloseStdin(context.Background()); err == nil {
		t.Error("expected CloseStdin to be unsupported on a PTY handle")
	}
	_, _ = handle.Wait(context.Background())
}
