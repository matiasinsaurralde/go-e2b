package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
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

type processRequest struct {
	Process processSpec `json:"process"`
}

type processSpec struct {
	Cmd  string            `json:"cmd"`
	Args []string          `json:"args,omitempty"`
	Envs map[string]string `json:"envs,omitempty"`
	Cwd  string            `json:"cwd,omitempty"`
}

type streamMessage struct {
	Event *processEvent `json:"event,omitempty"`
}

type processEvent struct {
	Start *startEvent `json:"start,omitempty"`
	Data  *dataEvent  `json:"data,omitempty"`
	End   *endEvent   `json:"end,omitempty"`
}

type startEvent struct {
	Pid int `json:"pid,omitempty"`
}

type dataEvent struct {
	Stdout []byte `json:"stdout,omitempty"`
	Stderr []byte `json:"stderr,omitempty"`
}

type endEvent struct {
	ExitCode int  `json:"exitCode,omitempty"`
	Exited   bool `json:"exited,omitempty"`
}

// Run executes a command in the sandbox and returns the result.
// It blocks until the command completes.
func (c *CommandService) Run(cmd string, args []string, opts ...RunOption) (*CommandResult, error) {
	return c.RunWithContext(context.Background(), cmd, args, opts...)
}

// RunWithContext executes a command in the sandbox using the provided context
// for cancellation and deadline control.
func (c *CommandService) RunWithContext(ctx context.Context, cmd string, args []string, opts ...RunOption) (*CommandResult, error) {
	rc := &runConfig{
		timeout: DefaultCommandTimeout,
	}
	for _, opt := range opts {
		opt(rc)
	}

	spec := processSpec{
		Cmd:  cmd,
		Args: args,
		Envs: rc.envVars,
		Cwd:  rc.cwd,
	}

	jsonBody, err := json.Marshal(processRequest{Process: spec})
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal command request: %w", err)
	}

	envelope, err := connectEnvelope(jsonBody)
	if err != nil {
		return nil, fmt.Errorf("e2b: create envelope: %w", err)
	}

	url := c.sandbox.envdURL("/process.Process/Start")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(envelope))
	if err != nil {
		return nil, fmt.Errorf("e2b: build command request: %w", err)
	}
	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Access-Token", c.sandbox.accessToken)
	req.Header.Set("Connect-Timeout-Ms", strconv.FormatInt(rc.timeout.Milliseconds(), 10))
	if rc.user != "" {
		req.Header.Set("User", rc.user)
	}

	resp, err := c.sandbox.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send command request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	return parseProcessStream(resp.Body)
}

// parseProcessStream reads Connect-framed JSON messages from a process
// stream and assembles the command result.
func parseProcessStream(r io.Reader) (*CommandResult, error) {
	var stdoutBuf, stderrBuf strings.Builder
	var exitCode int

	for {
		frame, err := readConnectFrame(r)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("e2b: read stream frame: %w", err)
		}

		if frame.isEndOfStream() {
			if err := parseTrailerError(frame.Payload); err != nil {
				return nil, err
			}
			break
		}

		var msg streamMessage
		if err := json.Unmarshal(frame.Payload, &msg); err != nil {
			return nil, fmt.Errorf("e2b: decode stream event: %w", err)
		}

		if msg.Event == nil {
			continue
		}

		if msg.Event.Data != nil {
			if len(msg.Event.Data.Stdout) > 0 {
				stdoutBuf.Write(msg.Event.Data.Stdout)
			}
			if len(msg.Event.Data.Stderr) > 0 {
				stderrBuf.Write(msg.Event.Data.Stderr)
			}
		}

		if msg.Event.End != nil {
			exitCode = msg.Event.End.ExitCode
		}
	}

	return &CommandResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
	}, nil
}
