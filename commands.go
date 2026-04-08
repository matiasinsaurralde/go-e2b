package e2b

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
	"github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process/processconnect"
)

// CommandResult holds the output of a completed command.
type CommandResult struct {
	// Stdout is the standard output of the command.
	Stdout string

	// Stderr is the standard error output of the command.
	Stderr string

	// ExitCode is the process exit code.
	ExitCode int
}

// CommandService provides command execution within a sandbox.
type CommandService struct {
	sandbox *Sandbox
}

func newCommandService(sbx *Sandbox) *CommandService {
	return &CommandService{sandbox: sbx}
}

// RunOption configures a single command execution.
type RunOption func(*runConfig)

type runConfig struct {
	envVars map[string]string
	cwd     string
	user    string
	timeout time.Duration
}

// WithEnv sets environment variables for the command.
func WithEnv(envs map[string]string) RunOption {
	return func(rc *runConfig) {
		rc.envVars = envs
	}
}

// WithCwd sets the working directory for the command.
func WithCwd(cwd string) RunOption {
	return func(rc *runConfig) {
		rc.cwd = cwd
	}
}

// WithUser sets the user to run the command as.
func WithUser(user string) RunOption {
	return func(rc *runConfig) {
		rc.user = user
	}
}

// WithTimeout sets the command timeout.
// Defaults to DefaultCommandTimeout (60s).
func WithTimeout(d time.Duration) RunOption {
	return func(rc *runConfig) {
		rc.timeout = d
	}
}

// Run executes a command in the sandbox and returns the result.
// It blocks until the command completes.
func (c *CommandService) Run(cmd string, args []string, opts ...RunOption) (*CommandResult, error) {
	return c.RunWithContext(context.Background(), cmd, args, opts...)
}

// RunWithContext executes a command in the sandbox using the provided context
// for cancellation and deadline control.
func (c *CommandService) RunWithContext(ctx context.Context, cmd string, args []string, opts ...RunOption) (*CommandResult, error) {
	rc := &runConfig{timeout: DefaultCommandTimeout}
	for _, opt := range opts {
		opt(rc)
	}

	if rc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
		defer cancel()
	}

	cfg := &processpb.ProcessConfig{
		Cmd:  cmd,
		Args: args,
		Envs: rc.envVars,
	}
	if rc.cwd != "" {
		cwd := rc.cwd
		cfg.Cwd = &cwd
	}

	req := connect.NewRequest(&processpb.StartRequest{Process: cfg})
	req.Header().Set("X-Access-Token", c.sandbox.accessToken)
	if rc.user != "" {
		req.Header().Set("User", rc.user)
	}

	client := processconnect.NewProcessClient(c.sandbox.httpClient, c.sandbox.envdBaseURL())
	stream, err := client.Start(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("e2b: start process: %w", err)
	}

	var stdoutBuf, stderrBuf strings.Builder
	var exitCode int

	for stream.Receive() {
		event := stream.Msg().GetEvent()
		if event == nil {
			continue
		}
		if data := event.GetData(); data != nil {
			if b := data.GetStdout(); len(b) > 0 {
				stdoutBuf.Write(b)
			}
			if b := data.GetStderr(); len(b) > 0 {
				stderrBuf.Write(b)
			}
		}
		if end := event.GetEnd(); end != nil {
			exitCode = int(end.GetExitCode())
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("e2b: process stream: %w", err)
	}

	return &CommandResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
	}, nil
}
