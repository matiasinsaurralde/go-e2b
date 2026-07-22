package e2b

import (
	"context"
	"fmt"
	"sync"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
)

// startStream adapts a Start RPC server stream to eventStream.
type startStream struct {
	s *connect.ServerStreamForClient[processpb.StartResponse]
}

func (a *startStream) Receive() bool                  { return a.s.Receive() }
func (a *startStream) Event() *processpb.ProcessEvent { return a.s.Msg().GetEvent() }
func (a *startStream) Err() error                     { return a.s.Err() }
func (a *startStream) Close() error                   { return a.s.Close() }

// connectStream adapts a Connect RPC server stream to eventStream.
type connectStream struct {
	s *connect.ServerStreamForClient[processpb.ConnectResponse]
}

func (a *connectStream) Receive() bool                  { return a.s.Receive() }
func (a *connectStream) Event() *processpb.ProcessEvent { return a.s.Msg().GetEvent() }
func (a *connectStream) Err() error                     { return a.s.Err() }
func (a *connectStream) Close() error                   { return a.s.Close() }

// CommandResult holds the output of a completed command.
type CommandResult struct {
	// Stdout is the standard output of the command.
	Stdout string

	// Stderr is the standard error output of the command.
	Stderr string

	// ExitCode is the process exit code. 0 indicates success.
	ExitCode int

	// Error is the error message reported by the sandbox, if the command
	// failed to run. It is empty for commands that started successfully.
	Error string
}

// ProcessInfo describes a command or PTY session running in the sandbox.
type ProcessInfo struct {
	// PID is the process ID.
	PID uint32

	// Tag is an optional identifier for special processes (e.g. a template
	// start command).
	Tag string

	// Cmd is the executable that was launched.
	Cmd string

	// Args are the arguments passed to Cmd.
	Args []string

	// Envs are the environment variables the process was started with.
	Envs map[string]string

	// Cwd is the working directory of the process, if set.
	Cwd string
}

// eventStream abstracts a server stream of process events. Both the Start and
// Connect RPCs return distinct stream types that carry the same ProcessEvent,
// so CommandHandle consumes them through this interface.
type eventStream interface {
	// Receive advances to the next message, returning false when the stream is
	// exhausted or errors.
	Receive() bool
	// Event returns the ProcessEvent from the most recent Receive.
	Event() *processpb.ProcessEvent
	// Err returns the terminal stream error, if any.
	Err() error
	// Close releases the stream.
	Close() error
}

// outputKind identifies which stream a chunk of process output came from.
type outputKind int

const (
	outputStdout outputKind = iota
	outputStderr
	outputPty
)

// CommandHandle is a handle to a command (or PTY session) running in a sandbox.
//
// It provides methods to wait for completion, stream output, send input, and
// kill the process. The underlying event stream is drained by Wait: output
// accumulators and the final result populate as Wait runs. For a background
// command started with CommandService.Start, call Wait to drive it to
// completion (optionally with streaming callbacks), or Disconnect to stop
// receiving events without killing the process.
type CommandHandle struct {
	pid uint32

	stream eventStream

	// handleKill kills the process (SIGKILL). Returns false if not found.
	handleKill func(context.Context) (bool, error)
	// handleSendStdin sends data to the process stdin. Nil for PTY handles.
	handleSendStdin func(context.Context, []byte) error
	// handleCloseStdin closes the process stdin. Nil for PTY handles.
	handleCloseStdin func(context.Context) error

	// onStdout/onStderr/onPty are the default streaming callbacks supplied at
	// creation (e.g. via WithOnStdout). Wait may add per-call callbacks on top.
	onStdout func([]byte)
	onStderr func([]byte)
	onPty    func([]byte)

	// cancel releases the context that bounds the command's lifetime (set when
	// the process was started with a timeout). It is invoked once when the
	// stream is closed by Wait or Disconnect. Nil when there is no such context.
	cancel context.CancelFunc

	stdoutDecoder incrementalUTF8Decoder
	stderrDecoder incrementalUTF8Decoder

	mu           sync.Mutex
	stdoutChunks []string
	stderrChunks []string
	result       *CommandResult
	done         bool
	disconnected bool
	closed       bool
}

// PID returns the process ID of the command.
func (h *CommandHandle) PID() uint32 { return h.pid }

// Stdout returns the standard output accumulated so far. It is complete once
// Wait has returned.
func (h *CommandHandle) Stdout() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return joinChunks(h.stdoutChunks)
}

// Stderr returns the standard error accumulated so far. It is complete once
// Wait has returned.
func (h *CommandHandle) Stderr() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return joinChunks(h.stderrChunks)
}

// ExitCode returns the process exit code and whether the command has finished.
// While the command is still running (Wait has not observed an end event), done
// is false.
func (h *CommandHandle) ExitCode() (code int, done bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.result == nil {
		return 0, false
	}
	return h.result.ExitCode, true
}

// waitOptions carries per-call streaming callbacks for Wait.
type waitOptions struct {
	onStdout func([]byte)
	onStderr func([]byte)
	onPty    func([]byte)
}

// WaitOption customizes a Wait call, typically to stream output via callbacks.
type WaitOption func(*waitOptions)

// WithWaitOnStdout registers a callback invoked with each decoded stdout chunk
// as Wait drains the stream.
func WithWaitOnStdout(fn func([]byte)) WaitOption {
	return func(o *waitOptions) { o.onStdout = fn }
}

// WithWaitOnStderr registers a callback invoked with each decoded stderr chunk
// as Wait drains the stream.
func WithWaitOnStderr(fn func([]byte)) WaitOption {
	return func(o *waitOptions) { o.onStderr = fn }
}

// WithWaitOnPty registers a callback invoked with each raw PTY output chunk as
// Wait drains the stream.
func WithWaitOnPty(fn func([]byte)) WaitOption {
	return func(o *waitOptions) { o.onPty = fn }
}

// Wait blocks until the command finishes and returns its result. If the command
// exits with a non-zero exit code, Wait returns a *CommandExitError (the result
// is still returned so callers can inspect output).
//
// Wait drives the event stream on the calling goroutine, invoking any streaming
// callbacks (from creation options and WaitOptions) as output arrives. It is
// safe to call Wait only once; subsequent calls return the cached result.
func (h *CommandHandle) Wait(ctx context.Context, opts ...WaitOption) (*CommandResult, error) {
	var wo waitOptions
	for _, opt := range opts {
		opt(&wo)
	}

	h.mu.Lock()
	if h.done {
		res := h.result
		h.mu.Unlock()
		return finalResult(res)
	}
	h.mu.Unlock()

	defer h.closeStream()

	for h.stream.Receive() {
		if err := ctx.Err(); err != nil {
			return nil, &TimeoutError{Message: err.Error()}
		}

		h.mu.Lock()
		if h.disconnected {
			h.mu.Unlock()
			break
		}
		h.mu.Unlock()

		event := h.stream.Event()
		if event == nil {
			continue
		}
		h.processEvent(event, &wo)
	}

	if err := h.stream.Err(); err != nil {
		// Flush any buffered partial runes so trailing bytes are not dropped.
		h.flush(&wo)
		return nil, mapProcessRPCError(err)
	}

	h.mu.Lock()
	if h.result == nil {
		h.flushLocked(&wo)
	}
	res := h.result
	h.done = true
	h.mu.Unlock()

	if res == nil {
		return nil, &Error{Message: "command ended without an end event"}
	}
	return finalResult(res)
}

// processEvent handles a single process event: dispatching output chunks to
// callbacks and recording the final result on an end event.
func (h *CommandHandle) processEvent(event *processpb.ProcessEvent, wo *waitOptions) {
	if data := event.GetData(); data != nil {
		if b := data.GetStdout(); len(b) > 0 {
			text := h.stdoutDecoder.Decode(b)
			if text != "" {
				h.mu.Lock()
				h.stdoutChunks = append(h.stdoutChunks, text)
				h.mu.Unlock()
				h.emit(outputStdout, []byte(text), wo)
			}
		}
		if b := data.GetStderr(); len(b) > 0 {
			text := h.stderrDecoder.Decode(b)
			if text != "" {
				h.mu.Lock()
				h.stderrChunks = append(h.stderrChunks, text)
				h.mu.Unlock()
				h.emit(outputStderr, []byte(text), wo)
			}
		}
		if b := data.GetPty(); len(b) > 0 {
			// PTY output is raw bytes; deliver a copy without UTF-8 decoding.
			h.emit(outputPty, append([]byte(nil), b...), wo)
		}
		return
	}

	if end := event.GetEnd(); end != nil {
		// Flush trailing decoder bytes and record the result *before* emitting
		// the flushed chunks, so a consumer that stops early still observes the
		// exit code.
		h.mu.Lock()
		h.flushBuffersLocked()
		h.result = &CommandResult{
			Stdout:   joinChunks(h.stdoutChunks),
			Stderr:   joinChunks(h.stderrChunks),
			ExitCode: int(end.GetExitCode()),
			Error:    end.GetError(),
		}
		h.mu.Unlock()
	}
}

// emit dispatches an output chunk to the matching creation-time and per-call
// callbacks, unless the handle has been disconnected.
func (h *CommandHandle) emit(kind outputKind, data []byte, wo *waitOptions) {
	h.mu.Lock()
	disconnected := h.disconnected
	h.mu.Unlock()
	if disconnected {
		return
	}

	switch kind {
	case outputStdout:
		if h.onStdout != nil {
			h.onStdout(data)
		}
		if wo.onStdout != nil {
			wo.onStdout(data)
		}
	case outputStderr:
		if h.onStderr != nil {
			h.onStderr(data)
		}
		if wo.onStderr != nil {
			wo.onStderr(data)
		}
	case outputPty:
		if h.onPty != nil {
			h.onPty(data)
		}
		if wo.onPty != nil {
			wo.onPty(data)
		}
	}
}

// flush flushes the decoders and emits any resulting chunks (used on stream
// error). Callers must not hold the lock.
func (h *CommandHandle) flush(wo *waitOptions) {
	h.mu.Lock()
	stdoutRest := h.stdoutDecoder.Flush()
	if stdoutRest != "" {
		h.stdoutChunks = append(h.stdoutChunks, stdoutRest)
	}
	stderrRest := h.stderrDecoder.Flush()
	if stderrRest != "" {
		h.stderrChunks = append(h.stderrChunks, stderrRest)
	}
	h.mu.Unlock()

	if stdoutRest != "" {
		h.emit(outputStdout, []byte(stdoutRest), wo)
	}
	if stderrRest != "" {
		h.emit(outputStderr, []byte(stderrRest), wo)
	}
}

// flushLocked flushes decoders into the accumulators and emits chunks. The
// caller must hold h.mu; it is released around callback dispatch to avoid
// re-entrancy deadlock and reacquired before returning.
func (h *CommandHandle) flushLocked(wo *waitOptions) {
	stdoutRest := h.stdoutDecoder.Flush()
	if stdoutRest != "" {
		h.stdoutChunks = append(h.stdoutChunks, stdoutRest)
	}
	stderrRest := h.stderrDecoder.Flush()
	if stderrRest != "" {
		h.stderrChunks = append(h.stderrChunks, stderrRest)
	}
	h.mu.Unlock()
	if stdoutRest != "" {
		h.emit(outputStdout, []byte(stdoutRest), wo)
	}
	if stderrRest != "" {
		h.emit(outputStderr, []byte(stderrRest), wo)
	}
	h.mu.Lock()
}

// flushBuffersLocked flushes decoder buffers into the accumulators without
// emitting callbacks. The caller must hold h.mu. Used on the end event, where
// the flushed chunks are folded into the recorded result.
func (h *CommandHandle) flushBuffersLocked() {
	if rest := h.stdoutDecoder.Flush(); rest != "" {
		h.stdoutChunks = append(h.stdoutChunks, rest)
	}
	if rest := h.stderrDecoder.Flush(); rest != "" {
		h.stderrChunks = append(h.stderrChunks, rest)
	}
}

// closeStream closes the underlying stream and cancels the lifetime context (if
// any), each at most once across the handle's life.
func (h *CommandHandle) closeStream() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	cancel := h.cancel
	h.mu.Unlock()

	_ = h.stream.Close()
	if cancel != nil {
		cancel()
	}
}

// Disconnect stops receiving events from the command without killing it. Once
// it returns, no further streaming callbacks fire. The command keeps running
// and can be reattached with CommandService.Connect. A concurrent Wait unblocks
// and returns the result observed so far.
func (h *CommandHandle) Disconnect() error {
	h.mu.Lock()
	h.disconnected = true
	h.mu.Unlock()
	h.closeStream()
	return nil
}

// Kill terminates the command with SIGKILL. It returns true if the command was
// found and killed, false if it was not found.
func (h *CommandHandle) Kill(ctx context.Context) (bool, error) {
	return h.handleKill(ctx)
}

// SendStdin sends data to the command's standard input. The command must have
// been started with WithStdin(true). It is not supported for PTY handles.
func (h *CommandHandle) SendStdin(ctx context.Context, data []byte) error {
	if h.handleSendStdin == nil {
		return &Error{Message: "sending stdin is not supported for this handle"}
	}
	return h.handleSendStdin(ctx, data)
}

// CloseStdin closes the command's standard input, signaling EOF. The command
// must have been started with WithStdin(true). It is not supported for PTY
// handles.
func (h *CommandHandle) CloseStdin(ctx context.Context) error {
	if h.handleCloseStdin == nil {
		return &Error{Message: "closing stdin is not supported for this handle"}
	}
	return h.handleCloseStdin(ctx)
}

// finalResult returns res, wrapping it in a *CommandExitError when the exit code
// is non-zero.
func finalResult(res *CommandResult) (*CommandResult, error) {
	if res == nil {
		return nil, &Error{Message: "command ended without a result"}
	}
	if res.ExitCode != 0 {
		return res, &CommandExitError{
			Stdout:   res.Stdout,
			Stderr:   res.Stderr,
			ExitCode: res.ExitCode,
			Message:  res.Error,
		}
	}
	return res, nil
}

func joinChunks(chunks []string) string {
	switch len(chunks) {
	case 0:
		return ""
	case 1:
		return chunks[0]
	}
	n := 0
	for _, c := range chunks {
		n += len(c)
	}
	b := make([]byte, 0, n)
	for _, c := range chunks {
		b = append(b, c...)
	}
	return string(b)
}

// startEventStream reads the first event from a freshly opened stream, which
// must be a Start event carrying the PID. It returns the PID or an error if the
// first event is missing or of the wrong kind. On error it closes the stream.
func startEventStream(stream eventStream) (uint32, error) {
	if !stream.Receive() {
		err := stream.Err()
		_ = stream.Close()
		if err != nil {
			return 0, mapProcessRPCError(err)
		}
		return 0, &Error{Message: "process stream closed before start event"}
	}
	event := stream.Event()
	start := event.GetStart()
	if start == nil {
		_ = stream.Close()
		return 0, &Error{Message: fmt.Sprintf("expected start event, got %T", event.GetEvent())}
	}
	return start.GetPid(), nil
}
