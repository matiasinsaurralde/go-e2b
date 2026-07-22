package e2b

import (
	"context"
	"time"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
)

// CommandService provides command execution within a sandbox. It supports
// blocking and background execution, output streaming, standard input, and
// management of running processes (list, kill, reconnect).
type CommandService struct {
	sandbox *Sandbox
}

func newCommandService(sbx *Sandbox) *CommandService {
	return &CommandService{sandbox: sbx}
}

// RunOption configures a command started with Run or Start.
type RunOption func(*runConfig)

type runConfig struct {
	envVars  map[string]string
	cwd      string
	user     string
	timeout  time.Duration
	stdin    bool
	onStdout func([]byte)
	onStderr func([]byte)
}

// WithEnv sets environment variables for the command.
func WithEnv(envs map[string]string) RunOption {
	return func(rc *runConfig) { rc.envVars = envs }
}

// WithCwd sets the working directory for the command.
func WithCwd(cwd string) RunOption {
	return func(rc *runConfig) { rc.cwd = cwd }
}

// WithUser sets the user to run the command as. Defaults to the sandbox's
// default user.
func WithUser(user string) RunOption {
	return func(rc *runConfig) { rc.user = user }
}

// WithTimeout sets the command's maximum lifetime. Defaults to
// DefaultCommandTimeout (60s). A zero or negative duration disables the
// timeout, letting the command run until it exits on its own.
func WithTimeout(d time.Duration) RunOption {
	return func(rc *runConfig) { rc.timeout = d }
}

// WithStdin keeps the command's standard input open so data can be sent with
// CommandHandle.SendStdin (or CommandService.SendStdin). Defaults to false.
func WithStdin(enabled bool) RunOption {
	return func(rc *runConfig) { rc.stdin = enabled }
}

// WithOnStdout registers a callback invoked with each decoded stdout chunk while
// the command's handle is being waited on.
func WithOnStdout(fn func([]byte)) RunOption {
	return func(rc *runConfig) { rc.onStdout = fn }
}

// WithOnStderr registers a callback invoked with each decoded stderr chunk while
// the command's handle is being waited on.
func WithOnStderr(fn func([]byte)) RunOption {
	return func(rc *runConfig) { rc.onStderr = fn }
}

// Run executes a shell command in the sandbox and blocks until it finishes,
// returning the result. The command string is run through a login shell
// (/bin/bash -l -c), so shell features such as pipes, redirection, and
// environment expansion are available.
//
// If the command exits with a non-zero exit code, Run returns a
// *CommandExitError; the *CommandResult is still returned so output can be
// inspected.
func (c *CommandService) Run(ctx context.Context, cmd string, opts ...RunOption) (*CommandResult, error) {
	handle, err := c.Start(ctx, cmd, opts...)
	if err != nil {
		return nil, err
	}
	return handle.Wait(ctx)
}

// Start launches a shell command in the background and returns a CommandHandle
// immediately, without waiting for the command to finish. Use the handle to
// stream output, send input, wait for completion, or kill the command.
//
// The command string is run through a login shell (/bin/bash -l -c).
func (c *CommandService) Start(ctx context.Context, cmd string, opts ...RunOption) (*CommandHandle, error) {
	rc := &runConfig{timeout: DefaultCommandTimeout}
	for _, opt := range opts {
		opt(rc)
	}

	// The timeout bounds the whole command lifetime (the server stream), not
	// just the Start call. Ownership of the cancel is handed to the returned
	// handle, which fires it when the stream closes. On any failure before the
	// handle is built, cancel is invoked here.
	var cancel context.CancelFunc
	if rc.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
	}
	ok := false
	defer func() {
		if !ok && cancel != nil {
			cancel()
		}
	}()

	config := &processpb.ProcessConfig{
		Cmd:  "/bin/bash",
		Args: []string{"-l", "-c", cmd},
		Envs: rc.envVars,
	}
	if rc.cwd != "" {
		cwd := rc.cwd
		config.Cwd = &cwd
	}

	stdin := rc.stdin
	req := connect.NewRequest(&processpb.StartRequest{
		Process: config,
		Stdin:   &stdin,
	})
	setProcessAuthHeaders(req.Header(), c.sandbox.accessToken, rc.user)
	req.Header().Set(keepAlivePingHeader, keepAlivePingIntervalSecStr)

	client := c.sandbox.processClient()
	stream, err := client.Start(ctx, req)
	if err != nil {
		return nil, mapProcessRPCError(err)
	}

	adapter := &startStream{s: stream}
	pid, err := startEventStream(adapter)
	if err != nil {
		return nil, err
	}

	handle := c.newHandle(pid, adapter, rc.onStdout, rc.onStderr, nil)
	handle.cancel = cancel
	ok = true
	return handle, nil
}

// ConnectOption configures a Connect call.
type ConnectOption func(*connectConfig)

type connectConfig struct {
	onStdout func([]byte)
	onStderr func([]byte)
}

// WithConnectOnStdout registers a callback for stdout chunks on a reconnected
// command.
func WithConnectOnStdout(fn func([]byte)) ConnectOption {
	return func(cc *connectConfig) { cc.onStdout = fn }
}

// WithConnectOnStderr registers a callback for stderr chunks on a reconnected
// command.
func WithConnectOnStderr(fn func([]byte)) ConnectOption {
	return func(cc *connectConfig) { cc.onStderr = fn }
}

// Connect reattaches to a command already running in the sandbox, identified by
// its PID (obtainable from List). It returns a CommandHandle whose Wait streams
// the command's remaining output and captures its result.
func (c *CommandService) Connect(ctx context.Context, pid uint32, opts ...ConnectOption) (*CommandHandle, error) {
	cc := &connectConfig{}
	for _, opt := range opts {
		opt(cc)
	}

	req := connect.NewRequest(&processpb.ConnectRequest{
		Process: pidSelector(pid),
	})
	req.Header().Set("X-Access-Token", c.sandbox.accessToken)
	req.Header().Set(keepAlivePingHeader, keepAlivePingIntervalSecStr)

	client := c.sandbox.processClient()
	stream, err := client.Connect(ctx, req)
	if err != nil {
		return nil, mapProcessRPCError(err)
	}

	adapter := &connectStream{s: stream}
	gotPID, err := startEventStream(adapter)
	if err != nil {
		return nil, err
	}

	return c.newHandle(gotPID, adapter, cc.onStdout, cc.onStderr, nil), nil
}

// List returns all commands and PTY sessions currently running in the sandbox.
func (c *CommandService) List(ctx context.Context) ([]ProcessInfo, error) {
	req := connect.NewRequest(&processpb.ListRequest{})
	req.Header().Set("X-Access-Token", c.sandbox.accessToken)

	client := c.sandbox.processClient()
	resp, err := client.List(ctx, req)
	if err != nil {
		return nil, mapProcessRPCError(err)
	}

	processes := resp.Msg.GetProcesses()
	out := make([]ProcessInfo, 0, len(processes))
	for _, p := range processes {
		info := ProcessInfo{
			PID: p.GetPid(),
			Tag: p.GetTag(),
		}
		if cfg := p.GetConfig(); cfg != nil {
			info.Cmd = cfg.GetCmd()
			info.Args = cfg.GetArgs()
			info.Envs = cfg.GetEnvs()
			info.Cwd = cfg.GetCwd()
		}
		out = append(out, info)
	}
	return out, nil
}

// Kill terminates the command with the given PID using SIGKILL. It returns true
// if the command was found and killed, false if no such command exists.
func (c *CommandService) Kill(ctx context.Context, pid uint32) (bool, error) {
	req := connect.NewRequest(&processpb.SendSignalRequest{
		Process: pidSelector(pid),
		Signal:  processpb.Signal_SIGNAL_SIGKILL,
	})
	req.Header().Set("X-Access-Token", c.sandbox.accessToken)

	client := c.sandbox.processClient()
	if _, err := client.SendSignal(ctx, req); err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, mapProcessRPCError(err)
	}
	return true, nil
}

// SendStdin sends data to the standard input of the command with the given PID.
// The command must have been started with WithStdin(true).
func (c *CommandService) SendStdin(ctx context.Context, pid uint32, data []byte) error {
	req := connect.NewRequest(&processpb.SendInputRequest{
		Process: pidSelector(pid),
		Input: &processpb.ProcessInput{
			Input: &processpb.ProcessInput_Stdin{Stdin: data},
		},
	})
	req.Header().Set("X-Access-Token", c.sandbox.accessToken)

	client := c.sandbox.processClient()
	if _, err := client.SendInput(ctx, req); err != nil {
		return mapProcessRPCError(err)
	}
	return nil
}

// CloseStdin closes the standard input of the command with the given PID,
// signaling EOF. The command must have been started with WithStdin(true).
func (c *CommandService) CloseStdin(ctx context.Context, pid uint32) error {
	req := connect.NewRequest(&processpb.CloseStdinRequest{
		Process: pidSelector(pid),
	})
	req.Header().Set("X-Access-Token", c.sandbox.accessToken)

	client := c.sandbox.processClient()
	if _, err := client.CloseStdin(ctx, req); err != nil {
		return mapProcessRPCError(err)
	}
	return nil
}

// newHandle builds a CommandHandle wired to this service's process management
// methods for the given PID.
func (c *CommandService) newHandle(pid uint32, stream eventStream, onStdout, onStderr, onPty func([]byte)) *CommandHandle {
	return &CommandHandle{
		pid:              pid,
		stream:           stream,
		handleKill:       func(ctx context.Context) (bool, error) { return c.Kill(ctx, pid) },
		handleSendStdin:  func(ctx context.Context, data []byte) error { return c.SendStdin(ctx, pid, data) },
		handleCloseStdin: func(ctx context.Context) error { return c.CloseStdin(ctx, pid) },
		onStdout:         onStdout,
		onStderr:         onStderr,
		onPty:            onPty,
	}
}
