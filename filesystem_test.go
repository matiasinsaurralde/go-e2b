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
		ID:          "sbx-test",
		accessToken: "token-test",
		client: &Client{
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					req.URL.Scheme = "https"
					req.URL.Host = srv.Listener.Addr().String()
					return origTransport.RoundTrip(req)
				}),
			},
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
		ID: "sbx-abc",
		client: &Client{
			sandboxDomain: "e2b.app",
		},
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
		ID: "sbx-abc",
		client: &Client{
			sandboxDomain: "e2b.app",
		},
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
		ID: "sbx-abc",
		client: &Client{
			sandboxDomain: "e2b.app",
		},
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

	client, err := NewClient(ClientConfig{
		APIKey:     "test-key",
		APIBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	sbx, err := client.NewSandbox(context.Background())
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	if sbx.Filesystem == nil {
		t.Error("Filesystem is nil")
	}
}

// --- List tests ---

func TestFilesystemList(t *testing.T) {
	const dir = "/home/user/mydir"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Query().Get("path") != dir {
			t.Errorf("path param = %q, want %q", r.URL.Query().Get("path"), dir)
		}
		if tok := r.Header.Get("X-Access-Token"); tok != "token-test" {
			t.Errorf("X-Access-Token = %q, want token-test", tok)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "file1.txt", "path": dir + "/file1.txt", "type": "file", "size": 100, "mtime": "2024-01-15T10:30:00Z"},
			{"name": "subdir", "path": dir + "/subdir", "type": "dir", "size": 0, "mtime": "2024-01-15T11:00:00Z"},
		})
	})

	entries, err := sbx.Filesystem.List(context.Background(), dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Name != "file1.txt" {
		t.Errorf("entries[0].Name = %q, want file1.txt", entries[0].Name)
	}
	if entries[0].Type != "file" {
		t.Errorf("entries[0].Type = %q, want file", entries[0].Type)
	}
	if entries[1].Name != "subdir" {
		t.Errorf("entries[1].Name = %q, want subdir", entries[1].Name)
	}
	if entries[1].Type != "dir" {
		t.Errorf("entries[1].Type = %q, want dir", entries[1].Type)
	}
	// ModTime should be parsed
	if entries[0].ModTime.IsZero() {
		t.Error("entries[0].ModTime is zero")
	}
}

func TestFilesystemListEmpty(t *testing.T) {
	const dir = "/empty/dir"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	})

	entries, err := sbx.Filesystem.List(context.Background(), dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
}

func TestFilesystemListWithUser(t *testing.T) {
	const dir = "/root"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if u := r.URL.Query().Get("username"); u != "root" {
			t.Errorf("username param = %q, want root", u)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[]"))
	})

	_, err := sbx.Filesystem.List(context.Background(), dir, WithFileUser("root"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
}

func TestFilesystemListNotFound(t *testing.T) {
	const dir = "/nonexistent/dir"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := sbx.Filesystem.List(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *FileNotFoundError
	if !errors.As(err, &e) {
		t.Fatalf("expected *FileNotFoundError, got %T: %v", err, err)
	}
	if e.Path != dir {
		t.Errorf("FileNotFoundError.Path = %q, want %q", e.Path, dir)
	}
}

func TestFilesystemListServerError(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})

	_, err := sbx.Filesystem.List(context.Background(), "/tmp")
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

func TestFilesystemListInvalidJSON(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})

	_, err := sbx.Filesystem.List(context.Background(), "/tmp")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFilesystemListModTimeFormats(t *testing.T) {
	// verify that various mtime formats from envd are parsed correctly.
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "a.txt", "path": "/tmp/a.txt", "mtime": "2024-06-15T08:00:00Z"},
			{"name": "b.txt", "path": "/tmp/b.txt", "mtime": "2024-06-15T08:00:00.123456789Z"},
			{"name": "c.txt", "path": "/tmp/c.txt", "mtime": "2024-06-15T08:00:00+08:00"},
		})
	})

	entries, err := sbx.Filesystem.List(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, entry := range entries {
		if entry.ModTime.IsZero() {
			t.Errorf("ModTime is zero for %s", entry.Name)
		}
	}
}

// --- Stat tests ---

func TestFilesystemStat(t *testing.T) {
	const path = "/home/user/file.txt"

	sbx := &Sandbox{
		ID:          "sbx-test",
		accessToken: "token-test",
		client: &Client{
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.Method != http.MethodGet {
						t.Errorf("method = %s, want HEAD", req.Method)
					}
					if req.URL.Query().Get("path") != path {
						t.Errorf("path param = %q, want %q", req.URL.Query().Get("path"), path)
					}
					if tok := req.Header.Get("X-Access-Token"); tok != "token-test" {
						t.Errorf("X-Access-Token = %q, want token-test", tok)
					}
					body := `{"name":"file.txt","path":"/home/user/file.txt","type":"file","size":2048,"mtime":"2024-03-20T14:00:00Z"}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"application/json"}},
						Body:       io.NopCloser(strings.NewReader(body)),
					}, nil
				}),
			},
		},
	}
	sbx.Filesystem = newFilesystemService(sbx)

	info, err := sbx.Filesystem.Stat(context.Background(), path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Name != "file.txt" {
		t.Errorf("Name = %q, want file.txt", info.Name)
	}
	if info.Path != path {
		t.Errorf("Path = %q, want %q", info.Path, path)
	}
	if info.Type != "file" {
		t.Errorf("Type = %q, want file", info.Type)
	}
	if info.Size != 2048 {
		t.Errorf("Size = %d, want 2048", info.Size)
	}
	if info.ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
}

func TestFilesystemStatDir(t *testing.T) {
	const path = "/home/user/mydir"

	sbx := &Sandbox{
		ID:          "sbx-test",
		accessToken: "token-test",
		client: &Client{
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					body := `{"name":"mydir","path":"/home/user/mydir","type":"dir","size":4096,"mtime":"2024-03-20T14:30:00Z"}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"application/json"}},
						Body:       io.NopCloser(strings.NewReader(body)),
					}, nil
				}),
			},
		},
	}
	sbx.Filesystem = newFilesystemService(sbx)

	info, err := sbx.Filesystem.Stat(context.Background(), path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Type != "dir" {
		t.Errorf("Type = %q, want dir", info.Type)
	}
}

func TestFilesystemStatWithUser(t *testing.T) {
	const path = "/root/.bashrc"

	sbx := &Sandbox{
		ID:          "sbx-test",
		accessToken: "token-test",
		client: &Client{
			sandboxDomain: "test.e2b.app",
			httpClient: &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if u := req.URL.Query().Get("username"); u != "root" {
						t.Errorf("username param = %q, want root", u)
					}
					body := `{"name":".bashrc","path":"/root/.bashrc"}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"application/json"}},
						Body:       io.NopCloser(strings.NewReader(body)),
					}, nil
				}),
			},
		},
	}
	sbx.Filesystem = newFilesystemService(sbx)

	_, err := sbx.Filesystem.Stat(context.Background(), path, WithFileUser("root"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

func TestFilesystemStatNotFound(t *testing.T) {
	const path = "/does/not/exist.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := sbx.Filesystem.Stat(context.Background(), path)
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

func TestFilesystemStatServerError(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	})

	_, err := sbx.Filesystem.Stat(context.Background(), "/tmp/f.txt")
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if e.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusBadGateway)
	}
}

// Stat on a non-directory path returns binary content from envd, so
// invalid JSON is treated as a plain file (binary fallback), not an error.
func TestFilesystemStatInvalidJSON(t *testing.T) {
	const path = "/tmp/f.txt"
	const body = "{!invalid}"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})

	info, err := sbx.Filesystem.Stat(context.Background(), path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Path != path {
		t.Errorf("Path = %q, want %q", info.Path, path)
	}
	if info.Type != "file" {
		t.Errorf("Type = %q, want file (binary fallback)", info.Type)
	}
	if info.Size != int64(len(body)) {
		t.Errorf("Size = %d, want %d", info.Size, len(body))
	}
}
// --- MakeDir tests ---

func TestFilesystemMakeDir(t *testing.T) {
	const dir = "/home/user/newdir"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Query().Get("path") != dir {
			t.Errorf("path param = %q, want %q", r.URL.Query().Get("path"), dir)
		}
		if r.URL.Query().Get("type") != "directory" {
			t.Errorf("type param = %q, want directory", r.URL.Query().Get("type"))
		}
		if tok := r.Header.Get("X-Access-Token"); tok != "token-test" {
			t.Errorf("X-Access-Token = %q, want token-test", tok)
		}
		w.WriteHeader(http.StatusOK)
	})

	err := sbx.Filesystem.MakeDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
}

func TestFilesystemMakeDirCreated(t *testing.T) {
	const dir = "/home/user/newdir"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	err := sbx.Filesystem.MakeDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
}

func TestFilesystemMakeDirWithUser(t *testing.T) {
	const dir = "/root/special"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if u := r.URL.Query().Get("username"); u != "root" {
			t.Errorf("username param = %q, want root", u)
		}
		w.WriteHeader(http.StatusOK)
	})

	err := sbx.Filesystem.MakeDir(context.Background(), dir, WithFileUser("root"))
	if err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
}

func TestFilesystemMakeDirNotFound(t *testing.T) {
	const dir = "/nonexistent/parent/child"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	err := sbx.Filesystem.MakeDir(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error")
	}
	var e *FileNotFoundError
	if !errors.As(err, &e) {
		t.Fatalf("expected *FileNotFoundError, got %T: %v", err, err)
	}
	if e.Path != dir {
		t.Errorf("FileNotFoundError.Path = %q, want %q", e.Path, dir)
	}
}

func TestFilesystemMakeDirServerError(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	err := sbx.Filesystem.MakeDir(context.Background(), "/root/protected")
	if err == nil {
		t.Fatal("expected error")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if e.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want %d", e.StatusCode, http.StatusForbidden)
	}
}

func TestFilesystemMakeDirTimeout(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	err := sbx.Filesystem.MakeDir(context.Background(), "/tmp/mydir", WithWriteTimeout(1*time.Millisecond))
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// --- Remove tests ---

func TestFilesystemRemove(t *testing.T) {
	const path = "/tmp/file-to-delete.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Query().Get("path") != path {
			t.Errorf("path param = %q, want %q", r.URL.Query().Get("path"), path)
		}
		if tok := r.Header.Get("X-Access-Token"); tok != "token-test" {
			t.Errorf("X-Access-Token = %q, want token-test", tok)
		}
		w.WriteHeader(http.StatusOK)
	})

	err := sbx.Filesystem.Remove(context.Background(), path)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestFilesystemRemoveNoContent(t *testing.T) {
	const path = "/tmp/file-deleted.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	err := sbx.Filesystem.Remove(context.Background(), path)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestFilesystemRemoveDir(t *testing.T) {
	const dir = "/tmp/dir-to-remove"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	err := sbx.Filesystem.Remove(context.Background(), dir)
	if err != nil {
		t.Fatalf("Remove (dir): %v", err)
	}
}

func TestFilesystemRemoveNotFound(t *testing.T) {
	const path = "/does/not/exist.txt"

	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	err := sbx.Filesystem.Remove(context.Background(), path)
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

func TestFilesystemRemoveServerError(t *testing.T) {
	sbx := newFilesystemTestSandbox(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("remove failed"))
	})

	err := sbx.Filesystem.Remove(context.Background(), "/tmp/f.txt")
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
