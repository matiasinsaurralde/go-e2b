package e2b

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Stream parsing tests ---

func TestParseProcessStream(t *testing.T) {
	var stream bytes.Buffer

	writeStreamEvent(t, &stream, streamMessage{
		Event: &processEvent{Start: &startEvent{Pid: 42}},
	})
	writeStreamEvent(t, &stream, streamMessage{
		Event: &processEvent{Data: &dataEvent{Stdout: []byte("hello world\n")}},
	})
	writeStreamEvent(t, &stream, streamMessage{
		Event: &processEvent{Data: &dataEvent{Stderr: []byte("some warning\n")}},
	})
	writeStreamEvent(t, &stream, streamMessage{
		Event: &processEvent{End: &endEvent{ExitCode: 0, Exited: true}},
	})
	writeTrailer(t, &stream, nil)

	result, err := parseProcessStream(&stream)
	if err != nil {
		t.Fatalf("parseProcessStream: %v", err)
	}
	if result.Stdout != "hello world\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "hello world\n")
	}
	if result.Stderr != "some warning\n" {
		t.Errorf("stderr = %q, want %q", result.Stderr, "some warning\n")
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestParseProcessStreamNonZeroExit(t *testing.T) {
	var stream bytes.Buffer

	writeStreamEvent(t, &stream, streamMessage{
		Event: &processEvent{Start: &startEvent{Pid: 1}},
	})
	writeStreamEvent(t, &stream, streamMessage{
		Event: &processEvent{End: &endEvent{ExitCode: 127, Exited: true}},
	})
	writeTrailer(t, &stream, nil)

	result, err := parseProcessStream(&stream)
	if err != nil {
		t.Fatalf("parseProcessStream: %v", err)
	}
	if result.ExitCode != 127 {
		t.Errorf("exit code = %d, want 127", result.ExitCode)
	}
}

func TestParseProcessStreamTrailerError(t *testing.T) {
	var stream bytes.Buffer

	writeTrailer(t, &stream, &connectError{
		Code:    "deadline_exceeded",
		Message: "command timed out",
	})

	_, err := parseProcessStream(&stream)
	if err == nil {
		t.Fatal("expected error from trailer")
	}
}

func TestParseProcessStreamNilEvent(t *testing.T) {
	var stream bytes.Buffer

	// A message with no event should be skipped.
	writeStreamEvent(t, &stream, streamMessage{})
	writeStreamEvent(t, &stream, streamMessage{
		Event: &processEvent{Data: &dataEvent{Stdout: []byte("ok")}},
	})
	writeTrailer(t, &stream, nil)

	result, err := parseProcessStream(&stream)
	if err != nil {
		t.Fatalf("parseProcessStream: %v", err)
	}
	if result.Stdout != "ok" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "ok")
	}
}

func TestParseProcessStreamInvalidJSON(t *testing.T) {
	var stream bytes.Buffer

	// Write a frame with invalid JSON payload.
	stream.WriteByte(0x00)
	payload := []byte("{invalid")
	_ = binary.Write(&stream, binary.BigEndian, uint32(len(payload)))
	stream.Write(payload)

	_, err := parseProcessStream(&stream)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseProcessStreamReadError(t *testing.T) {
	// Write a valid event frame, then truncated data to trigger a read error.
	var stream bytes.Buffer
	writeStreamEvent(t, &stream, streamMessage{
		Event: &processEvent{Start: &startEvent{Pid: 1}},
	})
	// Write flags byte but truncated length to cause a non-EOF error.
	stream.WriteByte(0x00)
	stream.WriteByte(0xFF) // Partial length (need 4 bytes, only 1).

	_, err := parseProcessStream(&stream)
	if err == nil {
		t.Fatal("expected error for truncated frame")
	}
}

// --- Run integration tests with httptest ---

func TestRunSuccess(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/connect+json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := r.Header.Get("Connect-Protocol-Version"); got != "1" {
			t.Errorf("Connect-Protocol-Version = %q", got)
		}
		if got := r.Header.Get("X-Access-Token"); got != "token-abc" {
			t.Errorf("X-Access-Token = %q", got)
		}

		// Verify we received a valid Connect envelope with the process request.
		frame, err := readConnectFrame(r.Body)
		if err != nil {
			t.Fatalf("read request frame: %v", err)
		}
		var pr processRequest
		if err := json.Unmarshal(frame.Payload, &pr); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if pr.Process.Cmd != "echo" {
			t.Errorf("cmd = %q, want %q", pr.Process.Cmd, "echo")
		}
		if len(pr.Process.Args) != 1 || pr.Process.Args[0] != "hello" {
			t.Errorf("args = %v, want [hello]", pr.Process.Args)
		}

		// Write a Connect stream response.
		w.WriteHeader(http.StatusOK)
		writeHTTPStreamEvent(t, w, streamMessage{
			Event: &processEvent{Start: &startEvent{Pid: 10}},
		})
		writeHTTPStreamEvent(t, w, streamMessage{
			Event: &processEvent{Data: &dataEvent{Stdout: []byte("hello\n")}},
		})
		writeHTTPStreamEvent(t, w, streamMessage{
			Event: &processEvent{End: &endEvent{ExitCode: 0, Exited: true}},
		})
		writeHTTPTrailer(t, w, nil)
	}))
	defer srv.Close()

	sbx := &Sandbox{
		ID:            "sbx-run",
		accessToken:   "token-abc",
		sandboxDomain: srv.Listener.Addr().String(),
		httpClient:    srv.Client(),
	}
	// Override envdURL to point to the test server.
	sbx.sandboxDomain = "" // We'll use a custom approach.
	sbx.apiBaseURL = srv.URL
	sbx.httpClient = srv.Client()

	// We need to override envdURL for the test. The simplest way is to
	// construct the sandbox so envdURL returns the test server URL.
	// Since envdURL uses https://{port}-{id}.{domain}{path}, we'll
	// create a command service that directly targets our test server.
	cs := newCommandService(sbx)

	// Monkey-patch: replace the sandbox's envdURL by setting fields so that
	// the URL resolves to our test server. Instead, let's test RunWithContext
	// by creating a sandbox that points to the right place.
	// The cleanest approach: use a custom transport that redirects.
	origClient := srv.Client()
	origTransport := origClient.Transport
	sbx.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Redirect envd requests to the test server.
			req.URL.Scheme = "https"
			req.URL.Host = srv.Listener.Addr().String()
			return origTransport.RoundTrip(req)
		}),
	}
	sbx.sandboxDomain = "test.e2b.app"
	sbx.Commands = cs

	result, err := cs.Run("echo", []string{"hello"})
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

func TestRunWithOptions(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		frame, err := readConnectFrame(r.Body)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		var pr processRequest
		if err := json.Unmarshal(frame.Payload, &pr); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if pr.Process.Envs["FOO"] != "bar" {
			t.Errorf("envs[FOO] = %q, want %q", pr.Process.Envs["FOO"], "bar")
		}
		if pr.Process.Cwd != "/tmp" {
			t.Errorf("cwd = %q, want %q", pr.Process.Cwd, "/tmp")
		}
		if got := r.Header.Get("User"); got != "root" {
			t.Errorf("User header = %q, want %q", got, "root")
		}
		if got := r.Header.Get("Connect-Timeout-Ms"); got != "5000" {
			t.Errorf("Connect-Timeout-Ms = %q, want %q", got, "5000")
		}

		w.WriteHeader(http.StatusOK)
		writeHTTPStreamEvent(t, w, streamMessage{
			Event: &processEvent{End: &endEvent{ExitCode: 0, Exited: true}},
		})
		writeHTTPTrailer(t, w, nil)
	}))
	defer srv.Close()

	sbx := testSandbox(srv)

	_, err := sbx.Commands.Run("cmd", nil,
		WithEnv(map[string]string{"FOO": "bar"}),
		WithCwd("/tmp"),
		WithUser("root"),
		WithTimeout(5000),
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunHTTPError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	sbx := testSandbox(srv)

	_, err := sbx.Commands.Run("cmd", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if e.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusInternalServerError)
	}
}

func TestRunWithContext(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		writeHTTPStreamEvent(t, w, streamMessage{
			Event: &processEvent{Data: &dataEvent{Stdout: []byte("ctx")}},
		})
		writeHTTPStreamEvent(t, w, streamMessage{
			Event: &processEvent{End: &endEvent{ExitCode: 0, Exited: true}},
		})
		writeHTTPTrailer(t, w, nil)
	}))
	defer srv.Close()

	sbx := testSandbox(srv)
	ctx := context.Background()

	result, err := sbx.Commands.RunWithContext(ctx, "cmd", nil)
	if err != nil {
		t.Fatalf("RunWithContext: %v", err)
	}
	if result.Stdout != "ctx" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "ctx")
	}
}

// --- Helpers ---

// roundTripFunc allows using a function as an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// testSandbox creates a Sandbox that routes all HTTP to the given test server.
func testSandbox(srv *httptest.Server) *Sandbox {
	origTransport := srv.Client().Transport
	sbx := &Sandbox{
		ID:            "sbx-test",
		accessToken:   "token-test",
		apiKey:        "test-key",
		apiBaseURL:    srv.URL,
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

func writeStreamEvent(t *testing.T, buf *bytes.Buffer, msg streamMessage) {
	t.Helper()
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	buf.WriteByte(0x00)
	if err := binary.Write(buf, binary.BigEndian, uint32(len(payload))); err != nil {
		t.Fatalf("write length: %v", err)
	}
	buf.Write(payload)
}

func writeTrailer(t *testing.T, buf *bytes.Buffer, connErr *connectError) {
	t.Helper()
	buf.WriteByte(connectFlagEndStream)
	if connErr != nil {
		payload, _ := json.Marshal(connectTrailer{Error: connErr})
		_ = binary.Write(buf, binary.BigEndian, uint32(len(payload)))
		buf.Write(payload)
	} else {
		_ = binary.Write(buf, binary.BigEndian, uint32(0))
	}
}

func writeHTTPStreamEvent(t *testing.T, w io.Writer, msg streamMessage) {
	t.Helper()
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var frame bytes.Buffer
	frame.WriteByte(0x00)
	_ = binary.Write(&frame, binary.BigEndian, uint32(len(payload)))
	frame.Write(payload)
	_, _ = w.Write(frame.Bytes())
}

func writeHTTPTrailer(t *testing.T, w io.Writer, connErr *connectError) {
	t.Helper()
	var frame bytes.Buffer
	frame.WriteByte(connectFlagEndStream)
	if connErr != nil {
		payload, _ := json.Marshal(connectTrailer{Error: connErr})
		_ = binary.Write(&frame, binary.BigEndian, uint32(len(payload)))
		frame.Write(payload)
	} else {
		_ = binary.Write(&frame, binary.BigEndian, uint32(0))
	}
	_, _ = w.Write(frame.Bytes())
}
