package e2b

import (
	"context"
	"time"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
)

// PtyService provides interactive pseudo-terminal (PTY) sessions within a
// sandbox. A PTY runs an interactive login shell whose raw output is delivered
// to an OnData callback, and which accepts keystrokes via SendInput.
type PtyService struct {
	sandbox *Sandbox
}

func newPtyService(sbx *Sandbox) *PtyService {
	return &PtyService{sandbox: sbx}
}

// PtyOption configures a PTY created with Create or connected with Connect.
type PtyOption func(*ptyConfig)

type ptyConfig struct {
	envVars map[string]string
	cwd     string
	user    string
	timeout time.Duration
	onData  func([]byte)
}

// WithPtyEnv sets environment variables for the PTY.
func WithPtyEnv(envs map[string]string) PtyOption {
	return func(pc *ptyConfig) { pc.envVars = envs }
}

// WithPtyCwd sets the working directory for the PTY.
func WithPtyCwd(cwd string) PtyOption {
	return func(pc *ptyConfig) { pc.cwd = cwd }
}

// WithPtyUser sets the user to run the PTY as. Defaults to the sandbox's default
// user.
func WithPtyUser(user string) PtyOption {
	return func(pc *ptyConfig) { pc.user = user }
}

// WithPtyTimeout sets the PTY's maximum lifetime. Defaults to
// DefaultCommandTimeout (60s). A zero or negative duration disables the timeout.
func WithPtyTimeout(d time.Duration) PtyOption {
	return func(pc *ptyConfig) { pc.timeout = d }
}

// WithPtyOnData registers a callback invoked with each raw PTY output chunk
// while the handle is being waited on. It is equivalent to passing the callback
// to Create/Connect and can also be supplied per Wait via WithWaitOnPty.
func WithPtyOnData(fn func([]byte)) PtyOption {
	return func(pc *ptyConfig) { pc.onData = fn }
}

// Create starts a new PTY of the given size (in character columns and rows) and
// returns a CommandHandle. PTY output arrives via the OnData callback (see
// WithPtyOnData) as the handle is waited on; send keystrokes with SendInput.
//
// The PTY runs /bin/bash -i -l with TERM, LANG, and LC_ALL defaulted to
// interactive-friendly values unless overridden via WithPtyEnv.
func (p *PtyService) Create(ctx context.Context, cols, rows uint32, opts ...PtyOption) (*CommandHandle, error) {
	pc := &ptyConfig{timeout: DefaultCommandTimeout}
	for _, opt := range opts {
		opt(pc)
	}

	envs := map[string]string{
		"TERM":   "xterm-256color",
		"LANG":   "C.UTF-8",
		"LC_ALL": "C.UTF-8",
	}
	for k, v := range pc.envVars {
		envs[k] = v
	}

	var cancel context.CancelFunc
	if pc.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, pc.timeout)
	}
	ok := false
	defer func() {
		if !ok && cancel != nil {
			cancel()
		}
	}()

	config := &processpb.ProcessConfig{
		Cmd:  "/bin/bash",
		Args: []string{"-i", "-l"},
		Envs: envs,
	}
	if pc.cwd != "" {
		cwd := pc.cwd
		config.Cwd = &cwd
	}

	req := connect.NewRequest(&processpb.StartRequest{
		Process: config,
		Pty: &processpb.PTY{
			Size: &processpb.PTY_Size{Cols: cols, Rows: rows},
		},
	})
	setProcessAuthHeaders(req.Header(), p.sandbox.accessToken, pc.user)
	req.Header().Set(keepAlivePingHeader, keepAlivePingIntervalSecStr)

	client := p.sandbox.processClient()
	stream, err := client.Start(ctx, req)
	if err != nil {
		return nil, mapProcessRPCError(err)
	}

	adapter := &startStream{s: stream}
	pid, err := startEventStream(adapter)
	if err != nil {
		return nil, err
	}

	handle := p.newHandle(pid, adapter, pc.onData)
	handle.cancel = cancel
	ok = true
	return handle, nil
}

// Connect reattaches to a running PTY identified by its PID (obtainable from
// CommandService.List). PTY output arrives via the OnData callback.
func (p *PtyService) Connect(ctx context.Context, pid uint32, opts ...PtyOption) (*CommandHandle, error) {
	pc := &ptyConfig{timeout: DefaultCommandTimeout}
	for _, opt := range opts {
		opt(pc)
	}

	var cancel context.CancelFunc
	if pc.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, pc.timeout)
	}
	ok := false
	defer func() {
		if !ok && cancel != nil {
			cancel()
		}
	}()

	req := connect.NewRequest(&processpb.ConnectRequest{
		Process: pidSelector(pid),
	})
	req.Header().Set("X-Access-Token", p.sandbox.accessToken)
	req.Header().Set(keepAlivePingHeader, keepAlivePingIntervalSecStr)

	client := p.sandbox.processClient()
	stream, err := client.Connect(ctx, req)
	if err != nil {
		return nil, mapProcessRPCError(err)
	}

	adapter := &connectStream{s: stream}
	gotPID, err := startEventStream(adapter)
	if err != nil {
		return nil, err
	}

	handle := p.newHandle(gotPID, adapter, pc.onData)
	handle.cancel = cancel
	ok = true
	return handle, nil
}

// SendInput sends raw input bytes (keystrokes) to the PTY with the given PID.
func (p *PtyService) SendInput(ctx context.Context, pid uint32, data []byte) error {
	req := connect.NewRequest(&processpb.SendInputRequest{
		Process: pidSelector(pid),
		Input: &processpb.ProcessInput{
			Input: &processpb.ProcessInput_Pty{Pty: data},
		},
	})
	req.Header().Set("X-Access-Token", p.sandbox.accessToken)

	client := p.sandbox.processClient()
	if _, err := client.SendInput(ctx, req); err != nil {
		return mapProcessRPCError(err)
	}
	return nil
}

// Resize changes the PTY's dimensions to the given columns and rows. Call it
// when the controlling terminal window is resized.
func (p *PtyService) Resize(ctx context.Context, pid, cols, rows uint32) error {
	req := connect.NewRequest(&processpb.UpdateRequest{
		Process: pidSelector(pid),
		Pty: &processpb.PTY{
			Size: &processpb.PTY_Size{Cols: cols, Rows: rows},
		},
	})
	req.Header().Set("X-Access-Token", p.sandbox.accessToken)

	client := p.sandbox.processClient()
	if _, err := client.Update(ctx, req); err != nil {
		return mapProcessRPCError(err)
	}
	return nil
}

// Kill terminates the PTY with the given PID using SIGKILL. It returns true if
// the PTY was found and killed, false if no such PTY exists.
func (p *PtyService) Kill(ctx context.Context, pid uint32) (bool, error) {
	req := connect.NewRequest(&processpb.SendSignalRequest{
		Process: pidSelector(pid),
		Signal:  processpb.Signal_SIGNAL_SIGKILL,
	})
	req.Header().Set("X-Access-Token", p.sandbox.accessToken)

	client := p.sandbox.processClient()
	if _, err := client.SendSignal(ctx, req); err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, mapProcessRPCError(err)
	}
	return true, nil
}

// newHandle builds a CommandHandle for a PTY. PTY handles deliver output through
// the onPty callback and do not support the stdin helpers (SendStdin/CloseStdin);
// use PtyService.SendInput instead.
func (p *PtyService) newHandle(pid uint32, stream eventStream, onData func([]byte)) *CommandHandle {
	return &CommandHandle{
		pid:        pid,
		stream:     stream,
		handleKill: func(ctx context.Context) (bool, error) { return p.Kill(ctx, pid) },
		onPty:      onData,
	}
}
