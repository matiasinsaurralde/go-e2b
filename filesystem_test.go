package e2b

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newFilesystemTestSandbox starts a TLS test server handling /files and wires a
// Sandbox whose httpClient redirects all traffic to it.
func newFilesystemTestSandbox(t *testing.T, handler http.HandlerFunc) *Sandbox {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/files", handler)

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	origTransport := srv.Client().Transport
	sbx := &Sandbox{
		ID:            "sbx-test",
		accessToken:   "token-test",
		sandboxDomain: "test.e2b.app",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "https"
				req.URL.Host = srv.Listener.Addr().String()
				return origTransport.RoundTrip(req)
			}),
		},
	}
	sbx.Filesystem = newFilesystemService(sbx)
	return sbx
}

// writeFileInfoResponse encodes a single FileInfo as the JSON array envd returns.
func writeFileInfoResponse(w http.ResponseWriter, info FileInfo) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode([]FileInfo{info})
}

// --- Write tests ---

func TestFilesystemWriteString(t *testing.T) {
	const content = "hello, sandbox"
	const path = "/home/user/hello.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Query().Get("path") != path {
			t.Errorf("path param = %q, want %q", r.URL.Query().Get("path"), path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/octet-stream" {
			t.Errorf("Content-Type = %q, want application/octet-stream", ct)
		}
		if tok := r.Header.Get("X-Access-Token"); tok != "token-test" {
			t.Errorf("X-Access-Token = %q, want token-test", tok)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != content {
			t.Errorf("body = %q, want %q", string(body), content)
		}
		writeFileInfoResponse(w, FileInfo{Name: "hello.txt", Path: path})
	})

	info, err := sbx.Filesystem.WriteString(context.Background(), path, content)
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if info.Path != path {
		t.Errorf("FileInfo.Path = %q, want %q", info.Path, path)
	}
	if info.Name != "hello.txt" {
		t.Errorf("FileInfo.Name = %q, want %q", info.Name, "hello.txt")
	}
}

func TestFilesystemWriteBytes(t *testing.T) {
	data := []byte{0x00, 0xFF, 0xAB, 0xCD}
	const path = "/tmp/binary.bin"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != string(data) {
			t.Errorf("body bytes mismatch")
		}
		writeFileInfoResponse(w, FileInfo{Name: "binary.bin", Path: path})
	})

	info, err := sbx.Filesystem.WriteBytes(context.Background(), path, data)
	if err != nil {
		t.Fatalf("WriteBytes: %v", err)
	}
	if info.Path != path {
		t.Errorf("FileInfo.Path = %q, want %q", info.Path, path)
	}
}

func TestFilesystemWriteFromReader(t *testing.T) {
	const content = "streamed content"
	const path = "/tmp/stream.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != content {
			t.Errorf("body = %q, want %q", string(body), content)
		}
		writeFileInfoResponse(w, FileInfo{Name: "stream.txt", Path: path})
	})

	info, err := sbx.Filesystem.Write(context.Background(), path, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if info.Path != path {
		t.Errorf("FileInfo.Path = %q, want %q", info.Path, path)
	}
}

func TestFilesystemWriteWithUser(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if u := r.URL.Query().Get("username"); u != "root" {
			t.Errorf("username param = %q, want %q", u, "root")
		}
		writeFileInfoResponse(w, FileInfo{Name: "f.txt", Path: "/root/f.txt"})
	})

	_, err := sbx.Filesystem.WriteString(context.Background(), "/root/f.txt", "data", WithFileUser("root"))
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}
}

func TestFilesystemWriteNotFound(t *testing.T) {
	const path = "/nonexistent/dir/file.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := sbx.Filesystem.WriteString(context.Background(), path, "data")
	if err == nil {
		t.Fatal("expected error")
	}
	var e *FileNotFoundError
	if !errors.As(err, &e) {
		t.Fatalf("expected *FileNotFoundError, got %T: %v", err, err)
	}
	if e.Path != path {
		t.Errorf("FileNotFoundError.Path = %q, want %q", e.Path, path)
	}
}

func TestFilesystemWriteServerError(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	})

	_, err := sbx.Filesystem.WriteString(context.Background(), "/tmp/f.txt", "data")
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

func TestFilesystemWriteEmptyResponse(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	})

	_, err := sbx.Filesystem.WriteString(context.Background(), "/tmp/f.txt", "data")
	if err == nil {
		t.Fatal("expected error for empty response array")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
}

func TestFilesystemWriteInvalidJSON(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})

	_, err := sbx.Filesystem.WriteString(context.Background(), "/tmp/f.txt", "data")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFilesystemWriteCreatedStatus(t *testing.T) {
	const path = "/tmp/new.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode([]FileInfo{{Name: "new.txt", Path: path}})
	})

	info, err := sbx.Filesystem.WriteString(context.Background(), path, "data")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if info.Path != path {
		t.Errorf("FileInfo.Path = %q, want %q", info.Path, path)
	}
}

// --- Read tests ---

func TestFilesystemReadString(t *testing.T) {
	const content = "file content"
	const path = "/home/user/hello.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Query().Get("path") != path {
			t.Errorf("path param = %q, want %q", r.URL.Query().Get("path"), path)
		}
		if tok := r.Header.Get("X-Access-Token"); tok != "token-test" {
			t.Errorf("X-Access-Token = %q, want token-test", tok)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	})

	got, err := sbx.Filesystem.ReadString(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadString: %v", err)
	}
	if got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestFilesystemReadBytes(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0xFF}
	const path = "/tmp/binary.bin"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})

	got, err := sbx.Filesystem.ReadBytes(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadBytes: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("bytes mismatch")
	}
}

func TestFilesystemReadStream(t *testing.T) {
	const content = "streamed data"
	const path = "/tmp/large.dat"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	})

	rc, err := sbx.Filesystem.Read(context.Background(), path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != content {
		t.Errorf("content = %q, want %q", string(got), content)
	}
}

func TestFilesystemReadWithUser(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if u := r.URL.Query().Get("username"); u != "root" {
			t.Errorf("username param = %q, want root", u)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	})

	_, err := sbx.Filesystem.ReadString(context.Background(), "/root/cfg", WithFileUser("root"))
	if err != nil {
		t.Fatalf("ReadString: %v", err)
	}
}

func TestFilesystemReadNotFound(t *testing.T) {
	const path = "/does/not/exist.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := sbx.Filesystem.ReadString(context.Background(), path)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *FileNotFoundError
	if !errors.As(err, &e) {
		t.Fatalf("expected *FileNotFoundError, got %T: %v", err, err)
	}
	if e.Path != path {
		t.Errorf("FileNotFoundError.Path = %q, want %q", e.Path, path)
	}
}

func TestFilesystemReadServerError(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("oops"))
	})

	_, err := sbx.Filesystem.ReadString(context.Background(), "/tmp/f.txt")
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

// --- Round-trip test ---

func TestFilesystemWriteReadRoundTrip(t *testing.T) {
	const content = "round-trip content"
	const path = "/tmp/roundtrip.txt"

	var stored string

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			stored = string(body)
			writeFileInfoResponse(w, FileInfo{Name: "roundtrip.txt", Path: path})
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(stored))
		}
	})

	if _, err := sbx.Filesystem.WriteString(context.Background(), path, content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	got, err := sbx.Filesystem.ReadString(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadString: %v", err)
	}
	if got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

// --- Context / timeout tests ---

func TestFilesystemReadCanceledContext(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	_, err := sbx.Filesystem.ReadString(ctx, "/tmp/f.txt")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestFilesystemWriteCanceledContext(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		writeFileInfoResponse(w, FileInfo{Name: "f.txt", Path: "/tmp/f.txt"})
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	_, err := sbx.Filesystem.WriteString(ctx, "/tmp/f.txt", "data")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestFilesystemReadTimeout(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data"))
	})

	_, err := sbx.Filesystem.ReadString(context.Background(), "/tmp/f.txt", WithReadTimeout(1*time.Millisecond))
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestFilesystemWriteTimeout(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		writeFileInfoResponse(w, FileInfo{Name: "f.txt", Path: "/tmp/f.txt"})
	})

	_, err := sbx.Filesystem.WriteString(context.Background(), "/tmp/f.txt", "data", WithWriteTimeout(1*time.Millisecond))
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// --- URL construction tests ---

func TestFilesystemFileURL(t *testing.T) {
	sbx := &Sandbox{
		ID:            "sbx-abc",
		sandboxDomain: "e2b.app",
	}
	fs := newFilesystemService(sbx)

	got, err := fs.fileURL("/home/user/file.txt", "")
	if err != nil {
		t.Fatalf("fileURL: %v", err)
	}
	want := "https://49983-sbx-abc.e2b.app/files?path=%2Fhome%2Fuser%2Ffile.txt"
	if got != want {
		t.Errorf("fileURL = %q, want %q", got, want)
	}
}

func TestFilesystemFileURLWithUser(t *testing.T) {
	sbx := &Sandbox{
		ID:            "sbx-abc",
		sandboxDomain: "e2b.app",
	}
	fs := newFilesystemService(sbx)

	got, err := fs.fileURL("/tmp/f.txt", "root")
	if err != nil {
		t.Fatalf("fileURL: %v", err)
	}
	if !strings.Contains(got, "username=root") {
		t.Errorf("fileURL %q missing username=root", got)
	}
}

func TestFilesystemFileURLSpecialChars(t *testing.T) {
	sbx := &Sandbox{
		ID:            "sbx-abc",
		sandboxDomain: "e2b.app",
	}
	fs := newFilesystemService(sbx)

	got, err := fs.fileURL("/tmp/my file & data.txt", "")
	if err != nil {
		t.Fatalf("fileURL: %v", err)
	}
	// Spaces and & must be percent-encoded in the query string.
	if strings.Contains(got, " ") || strings.Contains(got, "&path") {
		t.Errorf("fileURL %q contains unencoded special chars", got)
	}
}

// --- Sandbox initialisation ---

func TestNewSandboxInitializesFilesystem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createResponse{SandboxID: "sbx-fs"})
	}))
	defer srv.Close()

	sbx, err := NewSandbox(SandboxConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if sbx.Filesystem == nil {
		t.Error("Filesystem is nil")
	}
}
