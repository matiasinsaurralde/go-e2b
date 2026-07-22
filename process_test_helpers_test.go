package e2b

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
	"github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process/processconnect"
)

// testProcessServer is a configurable Connect ProcessHandler for tests. Any RPC
// left nil returns Unimplemented via the embedded default handler.
type testProcessServer struct {
	processconnect.UnimplementedProcessHandler

	startFn      func(context.Context, *connect.Request[processpb.StartRequest], *connect.ServerStream[processpb.StartResponse]) error
	connectFn    func(context.Context, *connect.Request[processpb.ConnectRequest], *connect.ServerStream[processpb.ConnectResponse]) error
	listFn       func(context.Context, *connect.Request[processpb.ListRequest]) (*connect.Response[processpb.ListResponse], error)
	sendInputFn  func(context.Context, *connect.Request[processpb.SendInputRequest]) (*connect.Response[processpb.SendInputResponse], error)
	sendSignalFn func(context.Context, *connect.Request[processpb.SendSignalRequest]) (*connect.Response[processpb.SendSignalResponse], error)
	updateFn     func(context.Context, *connect.Request[processpb.UpdateRequest]) (*connect.Response[processpb.UpdateResponse], error)
	closeStdinFn func(context.Context, *connect.Request[processpb.CloseStdinRequest]) (*connect.Response[processpb.CloseStdinResponse], error)
}

func (s *testProcessServer) Start(ctx context.Context, req *connect.Request[processpb.StartRequest], stream *connect.ServerStream[processpb.StartResponse]) error {
	if s.startFn == nil {
		return connect.NewError(connect.CodeUnimplemented, nil)
	}
	return s.startFn(ctx, req, stream)
}

func (s *testProcessServer) Connect(ctx context.Context, req *connect.Request[processpb.ConnectRequest], stream *connect.ServerStream[processpb.ConnectResponse]) error {
	if s.connectFn == nil {
		return connect.NewError(connect.CodeUnimplemented, nil)
	}
	return s.connectFn(ctx, req, stream)
}

func (s *testProcessServer) List(ctx context.Context, req *connect.Request[processpb.ListRequest]) (*connect.Response[processpb.ListResponse], error) {
	if s.listFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, nil)
	}
	return s.listFn(ctx, req)
}

func (s *testProcessServer) SendInput(ctx context.Context, req *connect.Request[processpb.SendInputRequest]) (*connect.Response[processpb.SendInputResponse], error) {
	if s.sendInputFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, nil)
	}
	return s.sendInputFn(ctx, req)
}

func (s *testProcessServer) SendSignal(ctx context.Context, req *connect.Request[processpb.SendSignalRequest]) (*connect.Response[processpb.SendSignalResponse], error) {
	if s.sendSignalFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, nil)
	}
	return s.sendSignalFn(ctx, req)
}

func (s *testProcessServer) Update(ctx context.Context, req *connect.Request[processpb.UpdateRequest]) (*connect.Response[processpb.UpdateResponse], error) {
	if s.updateFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, nil)
	}
	return s.updateFn(ctx, req)
}

func (s *testProcessServer) CloseStdin(ctx context.Context, req *connect.Request[processpb.CloseStdinRequest]) (*connect.Response[processpb.CloseStdinResponse], error) {
	if s.closeStdinFn == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, nil)
	}
	return s.closeStdinFn(ctx, req)
}

// newTestSandboxWith stands up a TLS Connect server backed by srv and returns a
// Sandbox whose envd traffic is routed to it.
func newTestSandboxWith(t *testing.T, srv *testProcessServer) *Sandbox {
	t.Helper()
	mux := http.NewServeMux()
	path, handler := processconnect.NewProcessHandler(srv)
	mux.Handle(path, handler)

	httpSrv := httptest.NewTLSServer(mux)
	t.Cleanup(httpSrv.Close)

	origTransport := httpSrv.Client().Transport
	sbx := &Sandbox{
		ID:          "sbx-test",
		accessToken: "token-test",
		client: &Client{
			apiKey:        "key-test",
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = "https"
					req.URL.Host = httpSrv.Listener.Addr().String()
					return origTransport.RoundTrip(req)
				}),
			},
		},
	}
	sbx.Commands = newCommandService(sbx)
	sbx.Pty = newPtyService(sbx)
	sbx.Filesystem = newFilesystemService(sbx)
	return sbx
}

// newTestSandbox is a convenience wrapper for tests that only need Start.
func newTestSandbox(t *testing.T, startFn func(context.Context, *connect.Request[processpb.StartRequest], *connect.ServerStream[processpb.StartResponse]) error) *Sandbox {
	return newTestSandboxWith(t, &testProcessServer{startFn: startFn})
}

// --- event construction helpers ---

func sendStart(stream *connect.ServerStream[processpb.StartResponse], event *processpb.ProcessEvent) error {
	return stream.Send(&processpb.StartResponse{Event: event})
}

func sendConnect(stream *connect.ServerStream[processpb.ConnectResponse], event *processpb.ProcessEvent) error {
	return stream.Send(&processpb.ConnectResponse{Event: event})
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

func ptyEvent(data []byte) *processpb.ProcessEvent {
	return &processpb.ProcessEvent{Event: &processpb.ProcessEvent_Data{
		Data: &processpb.ProcessEvent_DataEvent{
			Output: &processpb.ProcessEvent_DataEvent_Pty{Pty: data},
		},
	}}
}

func endEvent(exitCode int32, exited bool) *processpb.ProcessEvent {
	return &processpb.ProcessEvent{Event: &processpb.ProcessEvent_End{
		End: &processpb.ProcessEvent_EndEvent{ExitCode: exitCode, Exited: exited},
	}}
}

func endEventErr(exitCode int32, errMsg string) *processpb.ProcessEvent {
	return &processpb.ProcessEvent{Event: &processpb.ProcessEvent_End{
		End: &processpb.ProcessEvent_EndEvent{ExitCode: exitCode, Exited: true, Error: &errMsg},
	}}
}

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
