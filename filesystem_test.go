package e2b

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	filesystempb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/filesystem"
	"github.com/matiasinsaurralde/go-e2b/internal/gen/envd/filesystem/filesystemconnect"
)

// --- Connect RPC Filesystem test helpers ---

// testFilesystemHandler is a test implementation of filesystemconnect.FilesystemHandler.
// Only Stat, ListDir, MakeDir, Remove, Move, CreateWatcher, GetWatcherEvents,
// and RemoveWatcher are implemented; others delegate to
// UnimplementedFilesystemHandler and return CodeUnimplemented.
type testFilesystemHandler struct {
	filesystemconnect.UnimplementedFilesystemHandler
	statFn             func(context.Context, *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error)
	listFn             func(context.Context, *connect.Request[filesystempb.ListDirRequest]) (*connect.Response[filesystempb.ListDirResponse], error)
	mkdirFn            func(context.Context, *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error)
	removeFn           func(context.Context, *connect.Request[filesystempb.RemoveRequest]) (*connect.Response[filesystempb.RemoveResponse], error)
	moveFn             func(context.Context, *connect.Request[filesystempb.MoveRequest]) (*connect.Response[filesystempb.MoveResponse], error)
	createWatcherFn    func(context.Context, *connect.Request[filesystempb.CreateWatcherRequest]) (*connect.Response[filesystempb.CreateWatcherResponse], error)
	getWatcherEventsFn func(context.Context, *connect.Request[filesystempb.GetWatcherEventsRequest]) (*connect.Response[filesystempb.GetWatcherEventsResponse], error)
	removeWatcherFn    func(context.Context, *connect.Request[filesystempb.RemoveWatcherRequest]) (*connect.Response[filesystempb.RemoveWatcherResponse], error)
}

func (h *testFilesystemHandler) Stat(ctx context.Context, req *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
	return h.statFn(ctx, req)
}

func (h *testFilesystemHandler) ListDir(ctx context.Context, req *connect.Request[filesystempb.ListDirRequest]) (*connect.Response[filesystempb.ListDirResponse], error) {
	return h.listFn(ctx, req)
}

func (h *testFilesystemHandler) MakeDir(ctx context.Context, req *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error) {
	return h.mkdirFn(ctx, req)
}

func (h *testFilesystemHandler) Remove(ctx context.Context, req *connect.Request[filesystempb.RemoveRequest]) (*connect.Response[filesystempb.RemoveResponse], error) {
	return h.removeFn(ctx, req)
}

func (h *testFilesystemHandler) Move(ctx context.Context, req *connect.Request[filesystempb.MoveRequest]) (*connect.Response[filesystempb.MoveResponse], error) {
	return h.moveFn(ctx, req)
}

func (h *testFilesystemHandler) CreateWatcher(ctx context.Context, req *connect.Request[filesystempb.CreateWatcherRequest]) (*connect.Response[filesystempb.CreateWatcherResponse], error) {
	return h.createWatcherFn(ctx, req)
}

func (h *testFilesystemHandler) GetWatcherEvents(ctx context.Context, req *connect.Request[filesystempb.GetWatcherEventsRequest]) (*connect.Response[filesystempb.GetWatcherEventsResponse], error) {
	return h.getWatcherEventsFn(ctx, req)
}

func (h *testFilesystemHandler) RemoveWatcher(ctx context.Context, req *connect.Request[filesystempb.RemoveWatcherRequest]) (*connect.Response[filesystempb.RemoveWatcherResponse], error) {
	if h.removeWatcherFn != nil {
		return h.removeWatcherFn(ctx, req)
	}
	return connect.NewResponse(&filesystempb.RemoveWatcherResponse{}), nil
}

// newFilesystemRPCTestSandbox creates a Sandbox whose envd traffic is routed to
// a Connect RPC FilesystemHandler backed by the provided test handler.
func newFilesystemRPCTestSandbox(t *testing.T, h *testFilesystemHandler) *Sandbox {
	t.Helper()
	mux := http.NewServeMux()
	path, handler := filesystemconnect.NewFilesystemHandler(h)
	mux.Handle(path, handler)

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
	sbx.Commands = newCommandService(sbx)
	return sbx
}

// --- HTTP /files test helper (for Read/Write tests) ---

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
	sbx.Commands = newCommandService(sbx)
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
	mtime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		listFn: func(_ context.Context, req *connect.Request[filesystempb.ListDirRequest]) (*connect.Response[filesystempb.ListDirResponse], error) {
			if req.Msg.Path != dir {
				t.Errorf("path = %q, want %q", req.Msg.Path, dir)
			}
			if req.Header().Get("X-Access-Token") != "token-test" {
				t.Errorf("X-Access-Token = %q, want token-test", req.Header().Get("X-Access-Token"))
			}
			return connect.NewResponse(&filesystempb.ListDirResponse{
				Entries: []*filesystempb.EntryInfo{
					{Name: "file1.txt", Path: dir + "/file1.txt", Type: filesystempb.FileType_FILE_TYPE_FILE, Size: 100, ModifiedTime: timestamppb.New(mtime)},
					{Name: "subdir", Path: dir + "/subdir", Type: filesystempb.FileType_FILE_TYPE_DIRECTORY, Size: 0, ModifiedTime: timestamppb.New(mtime.Add(30 * time.Minute))},
				},
			}), nil
		},
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
	if entries[1].Type != "directory" {
		t.Errorf("entries[1].Type = %q, want directory", entries[1].Type)
	}
	if entries[0].ModTime.IsZero() {
		t.Error("entries[0].ModTime is zero")
	}
}

func TestFilesystemListEmpty(t *testing.T) {
	const dir = "/empty/dir"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		listFn: func(_ context.Context, _ *connect.Request[filesystempb.ListDirRequest]) (*connect.Response[filesystempb.ListDirResponse], error) {
			return connect.NewResponse(&filesystempb.ListDirResponse{}), nil
		},
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

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		listFn: func(_ context.Context, req *connect.Request[filesystempb.ListDirRequest]) (*connect.Response[filesystempb.ListDirResponse], error) {
			if userID := req.Header().Get("X-User-ID"); userID != "root" {
				t.Errorf("X-User-ID = %q, want root", userID)
			}
			return connect.NewResponse(&filesystempb.ListDirResponse{}), nil
		},
	})

	_, err := sbx.Filesystem.List(context.Background(), dir, WithFileUser("root"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
}

func TestFilesystemListNotFound(t *testing.T) {
	const dir = "/nonexistent/dir"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		listFn: func(_ context.Context, _ *connect.Request[filesystempb.ListDirRequest]) (*connect.Response[filesystempb.ListDirResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("not found"))
		},
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
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		listFn: func(_ context.Context, _ *connect.Request[filesystempb.ListDirRequest]) (*connect.Response[filesystempb.ListDirResponse], error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("boom"))
		},
	})

	_, err := sbx.Filesystem.List(context.Background(), "/tmp")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") && !strings.Contains(err.Error(), "internal") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFilesystemListModTimeFormats(t *testing.T) {
	mt := time.Date(2024, 6, 15, 8, 0, 0, 123456789, time.UTC)

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		listFn: func(_ context.Context, _ *connect.Request[filesystempb.ListDirRequest]) (*connect.Response[filesystempb.ListDirResponse], error) {
			return connect.NewResponse(&filesystempb.ListDirResponse{
				Entries: []*filesystempb.EntryInfo{
					{Name: "a.txt", Path: "/tmp/a.txt", ModifiedTime: timestamppb.New(mt)},
					{Name: "b.txt", Path: "/tmp/b.txt", ModifiedTime: timestamppb.New(mt)},
					{Name: "c.txt", Path: "/tmp/c.txt", ModifiedTime: timestamppb.New(mt)},
				},
			}), nil
		},
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
	mtime := time.Date(2024, 3, 20, 14, 0, 0, 0, time.UTC)

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, req *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			if req.Msg.Path != path {
				t.Errorf("path = %q, want %q", req.Msg.Path, path)
			}
			if req.Header().Get("X-Access-Token") != "token-test" {
				t.Errorf("X-Access-Token = %q, want token-test", req.Header().Get("X-Access-Token"))
			}
			return connect.NewResponse(&filesystempb.StatResponse{
				Entry: &filesystempb.EntryInfo{
					Name: "file.txt", Path: path, Type: filesystempb.FileType_FILE_TYPE_FILE,
					Size: 2048, ModifiedTime: timestamppb.New(mtime),
				},
			}), nil
		},
	})

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

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, _ *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			return connect.NewResponse(&filesystempb.StatResponse{
				Entry: &filesystempb.EntryInfo{
					Name: "mydir", Path: path, Type: filesystempb.FileType_FILE_TYPE_DIRECTORY, Size: 4096,
				},
			}), nil
		},
	})

	info, err := sbx.Filesystem.Stat(context.Background(), path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Type != "directory" {
		t.Errorf("Type = %q, want directory", info.Type)
	}
}

func TestFilesystemStatWithUser(t *testing.T) {
	const path = "/root/.bashrc"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, req *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			if userID := req.Header().Get("X-User-ID"); userID != "root" {
				t.Errorf("X-User-ID = %q, want root", userID)
			}
			return connect.NewResponse(&filesystempb.StatResponse{
				Entry: &filesystempb.EntryInfo{Name: ".bashrc", Path: path},
			}), nil
		},
	})

	_, err := sbx.Filesystem.Stat(context.Background(), path, WithFileUser("root"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

func TestFilesystemStatNotFound(t *testing.T) {
	const path = "/does/not/exist.txt"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, _ *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("not found"))
		},
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
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, _ *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("bad gateway"))
		},
	})

	_, err := sbx.Filesystem.Stat(context.Background(), "/tmp/f.txt")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad gateway") && !strings.Contains(err.Error(), "internal") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestFilesystemStatNilEntry verifies Stat does not panic when envd returns a
// success response with a nil Entry (e.g. protocol skew or an empty proto).
func TestFilesystemStatNilEntry(t *testing.T) {
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, _ *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			return connect.NewResponse(&filesystempb.StatResponse{}), nil
		},
	})

	info, err := sbx.Filesystem.Stat(context.Background(), "/tmp/f.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil FileInfo")
	}
	if info.Name != "" || info.Path != "" {
		t.Errorf("expected zero FileInfo, got %+v", info)
	}
}

// TestFilesystemStatOwnerGroup verifies Stat exposes the owner and group
// fields returned by envd.
func TestFilesystemStatOwnerGroup(t *testing.T) {
	const path = "/root/.bashrc"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, _ *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			return connect.NewResponse(&filesystempb.StatResponse{
				Entry: &filesystempb.EntryInfo{Name: ".bashrc", Path: path, Owner: "root", Group: "root"},
			}), nil
		},
	})

	info, err := sbx.Filesystem.Stat(context.Background(), path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Owner != "root" {
		t.Errorf("Owner = %q, want root", info.Owner)
	}
	if info.Group != "root" {
		t.Errorf("Group = %q, want root", info.Group)
	}
}

func TestFilesystemStatInvalidJSON(t *testing.T) {
	// gRPC errors are not JSON-decode issues; just test that error propagation works.
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, _ *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			return nil, connect.NewError(connect.CodeUnknown, errors.New("unexpected"))
		},
	})

	_, err := sbx.Filesystem.Stat(context.Background(), "/tmp/f.txt")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- MakeDir tests ---

func TestFilesystemMakeDir(t *testing.T) {
	const dir = "/home/user/newdir"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		mkdirFn: func(_ context.Context, req *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error) {
			if req.Msg.Path != dir {
				t.Errorf("path = %q, want %q", req.Msg.Path, dir)
			}
			if req.Header().Get("X-Access-Token") != "token-test" {
				t.Errorf("X-Access-Token = %q, want token-test", req.Header().Get("X-Access-Token"))
			}
			return connect.NewResponse(&filesystempb.MakeDirResponse{}), nil
		},
	})

	if err := sbx.Filesystem.MakeDir(context.Background(), dir); err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
}

// TestFilesystemMakeDirExisting verifies MakeDir is idempotent: envd reports an
// existing directory with the AlreadyExists code, and MakeDir must treat that as
// success (matching the reference JS/Python SDKs). It also accepts envd variants
// that report an existing directory as a plain success response.
func TestFilesystemMakeDirExisting(t *testing.T) {
	const dir = "/home/user/existing"

	tests := []struct {
		name    string
		mkdirFn func(context.Context, *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error)
	}{
		{
			// Live envd behavior: existing dir -> AlreadyExists code.
			name: "already exists code",
			mkdirFn: func(_ context.Context, _ *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error) {
				return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("directory already exists: "+dir))
			},
		},
		{
			// Some envd variants report an existing dir as a plain success.
			name: "success response",
			mkdirFn: func(_ context.Context, _ *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error) {
				return connect.NewResponse(&filesystempb.MakeDirResponse{
					Entry: &filesystempb.EntryInfo{Name: "existing", Path: dir, Type: filesystempb.FileType_FILE_TYPE_DIRECTORY},
				}), nil
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{mkdirFn: tc.mkdirFn})
			if err := sbx.Filesystem.MakeDir(context.Background(), dir); err != nil {
				t.Fatalf("MakeDir on existing dir: %v", err)
			}
		})
	}
}

func TestFilesystemMakeDirWithUser(t *testing.T) {
	const dir = "/root/special"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		mkdirFn: func(_ context.Context, req *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error) {
			if userID := req.Header().Get("X-User-ID"); userID != "root" {
				t.Errorf("X-User-ID = %q, want root", userID)
			}
			return connect.NewResponse(&filesystempb.MakeDirResponse{}), nil
		},
	})

	err := sbx.Filesystem.MakeDir(context.Background(), dir, WithFileUser("root"))
	if err != nil {
		t.Fatalf("MakeDir: %v", err)
	}
}

func TestFilesystemMakeDirNotFound(t *testing.T) {
	const dir = "/nonexistent/parent/child"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		mkdirFn: func(_ context.Context, _ *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("not found"))
		},
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
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		mkdirFn: func(_ context.Context, _ *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("forbidden"))
		},
	})

	err := sbx.Filesystem.MakeDir(context.Background(), "/root/protected")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "forbidden") && !strings.Contains(err.Error(), "permission_denied") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFilesystemMakeDirTimeout(t *testing.T) {
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		mkdirFn: func(_ context.Context, _ *connect.Request[filesystempb.MakeDirRequest]) (*connect.Response[filesystempb.MakeDirResponse], error) {
			time.Sleep(50 * time.Millisecond)
			return connect.NewResponse(&filesystempb.MakeDirResponse{}), nil
		},
	})

	err := sbx.Filesystem.MakeDir(context.Background(), "/tmp/mydir", WithWriteTimeout(1*time.Millisecond))
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// --- Remove tests ---

func TestFilesystemRemove(t *testing.T) {
	const path = "/tmp/file-to-delete.txt"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		removeFn: func(_ context.Context, req *connect.Request[filesystempb.RemoveRequest]) (*connect.Response[filesystempb.RemoveResponse], error) {
			if req.Msg.Path != path {
				t.Errorf("path = %q, want %q", req.Msg.Path, path)
			}
			if req.Header().Get("X-Access-Token") != "token-test" {
				t.Errorf("X-Access-Token = %q, want token-test", req.Header().Get("X-Access-Token"))
			}
			return connect.NewResponse(&filesystempb.RemoveResponse{}), nil
		},
	})

	err := sbx.Filesystem.Remove(context.Background(), path)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestFilesystemRemoveNoContent(t *testing.T) {
	const path = "/tmp/file-deleted.txt"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		removeFn: func(_ context.Context, _ *connect.Request[filesystempb.RemoveRequest]) (*connect.Response[filesystempb.RemoveResponse], error) {
			return connect.NewResponse(&filesystempb.RemoveResponse{}), nil
		},
	})

	err := sbx.Filesystem.Remove(context.Background(), path)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestFilesystemRemoveDir(t *testing.T) {
	const dir = "/tmp/dir-to-remove"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		removeFn: func(_ context.Context, _ *connect.Request[filesystempb.RemoveRequest]) (*connect.Response[filesystempb.RemoveResponse], error) {
			return connect.NewResponse(&filesystempb.RemoveResponse{}), nil
		},
	})

	err := sbx.Filesystem.Remove(context.Background(), dir)
	if err != nil {
		t.Fatalf("Remove (dir): %v", err)
	}
}

func TestFilesystemRemoveNotFound(t *testing.T) {
	const path = "/does/not/exist.txt"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		removeFn: func(_ context.Context, _ *connect.Request[filesystempb.RemoveRequest]) (*connect.Response[filesystempb.RemoveResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("not found"))
		},
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
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		removeFn: func(_ context.Context, _ *connect.Request[filesystempb.RemoveRequest]) (*connect.Response[filesystempb.RemoveResponse], error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("remove failed"))
		},
	})

	err := sbx.Filesystem.Remove(context.Background(), "/tmp/f.txt")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "remove failed") && !strings.Contains(err.Error(), "internal") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- Exists tests ---

func TestFilesystemExistsTrue(t *testing.T) {
	const path = "/home/user/exists.txt"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, req *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			if req.Msg.Path != path {
				t.Errorf("path = %q, want %q", req.Msg.Path, path)
			}
			return connect.NewResponse(&filesystempb.StatResponse{
				Entry: &filesystempb.EntryInfo{
					Name: "exists.txt", Path: path, Type: filesystempb.FileType_FILE_TYPE_FILE, Size: 100,
				},
			}), nil
		},
	})

	ok, err := sbx.Filesystem.Exists(context.Background(), path)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Error("expected true, got false")
	}
}

func TestFilesystemExistsFalse(t *testing.T) {
	const path = "/does/not/exist.txt"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, _ *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("not found"))
		},
	})

	ok, err := sbx.Filesystem.Exists(context.Background(), path)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if ok {
		t.Error("expected false for nonexistent path, got true")
	}
}

func TestFilesystemExistsServerError(t *testing.T) {
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		statFn: func(_ context.Context, _ *connect.Request[filesystempb.StatRequest]) (*connect.Response[filesystempb.StatResponse], error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("boom"))
		},
	})

	_, err := sbx.Filesystem.Exists(context.Background(), "/tmp/f.txt")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

// --- Rename tests ---

func TestFilesystemRename(t *testing.T) {
	const oldPath = "/tmp/old.txt"
	const newPath = "/tmp/new.txt"
	mtime := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		moveFn: func(_ context.Context, req *connect.Request[filesystempb.MoveRequest]) (*connect.Response[filesystempb.MoveResponse], error) {
			if req.Msg.Source != oldPath {
				t.Errorf("source = %q, want %q", req.Msg.Source, oldPath)
			}
			if req.Msg.Destination != newPath {
				t.Errorf("destination = %q, want %q", req.Msg.Destination, newPath)
			}
			if req.Header().Get("X-Access-Token") != "token-test" {
				t.Errorf("X-Access-Token = %q, want token-test", req.Header().Get("X-Access-Token"))
			}
			return connect.NewResponse(&filesystempb.MoveResponse{
				Entry: &filesystempb.EntryInfo{
					Name: "new.txt", Path: newPath, Type: filesystempb.FileType_FILE_TYPE_FILE,
					Size: 42, ModifiedTime: timestamppb.New(mtime),
				},
			}), nil
		},
	})

	info, err := sbx.Filesystem.Rename(context.Background(), oldPath, newPath)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if info.Name != "new.txt" {
		t.Errorf("Name = %q, want new.txt", info.Name)
	}
	if info.Path != newPath {
		t.Errorf("Path = %q, want %q", info.Path, newPath)
	}
	if info.Type != "file" {
		t.Errorf("Type = %q, want file", info.Type)
	}
	if info.ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
}

func TestFilesystemRenameNotFound(t *testing.T) {
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		moveFn: func(_ context.Context, _ *connect.Request[filesystempb.MoveRequest]) (*connect.Response[filesystempb.MoveResponse], error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("not found"))
		},
	})

	_, err := sbx.Filesystem.Rename(context.Background(), "/tmp/old.txt", "/tmp/new.txt")
	if err == nil {
		t.Fatal("expected error")
	}
	var e *FileNotFoundError
	if !errors.As(err, &e) {
		t.Fatalf("expected *FileNotFoundError, got %T: %v", err, err)
	}
}

// --- WatchDir tests ---

func TestFilesystemWatchDir(t *testing.T) {
	const dir = "/tmp/watched"

	var watcherID = "watcher-abc"

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		createWatcherFn: func(_ context.Context, req *connect.Request[filesystempb.CreateWatcherRequest]) (*connect.Response[filesystempb.CreateWatcherResponse], error) {
			if req.Msg.Path != dir {
				t.Errorf("path = %q, want %q", req.Msg.Path, dir)
			}
			if !req.Msg.Recursive {
				t.Error("expected recursive=true")
			}
			return connect.NewResponse(&filesystempb.CreateWatcherResponse{WatcherId: watcherID}), nil
		},
		getWatcherEventsFn: func(_ context.Context, req *connect.Request[filesystempb.GetWatcherEventsRequest]) (*connect.Response[filesystempb.GetWatcherEventsResponse], error) {
			if req.Msg.WatcherId != watcherID {
				t.Errorf("watcher_id = %q, want %q", req.Msg.WatcherId, watcherID)
			}
			return connect.NewResponse(&filesystempb.GetWatcherEventsResponse{
				Events: []*filesystempb.FilesystemEvent{
					{Name: "newfile.txt", Type: filesystempb.EventType_EVENT_TYPE_CREATE},
					{Name: "newfile.txt", Type: filesystempb.EventType_EVENT_TYPE_WRITE},
				},
			}), nil
		},
	})

	handle, err := sbx.Filesystem.WatchDir(context.Background(), dir, true)
	if err != nil {
		t.Fatalf("WatchDir: %v", err)
	}
	if handle == nil {
		t.Fatal("expected non-nil WatchHandle")
	}

	events, err := handle.GetEvents(context.Background())
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Name != "newfile.txt" {
		t.Errorf("events[0].Name = %q, want newfile.txt", events[0].Name)
	}

	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("handle.Stop: %v", err)
	}
}

func TestFilesystemWatchDirCreateError(t *testing.T) {
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		createWatcherFn: func(_ context.Context, _ *connect.Request[filesystempb.CreateWatcherRequest]) (*connect.Response[filesystempb.CreateWatcherResponse], error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed"))
		},
	})

	_, err := sbx.Filesystem.WatchDir(context.Background(), "/tmp", false)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestFilesystemWatchDirUserThreaded verifies the user context passed to
// WatchDir is reused on the follow-up GetWatcherEvents and RemoveWatcher calls,
// not dropped to the default user.
func TestFilesystemWatchDirUserThreaded(t *testing.T) {
	const dir = "/root/watched"

	checkUser := func(hdr string) {
		if hdr != "root" {
			t.Errorf("X-User-ID = %q, want root", hdr)
		}
	}

	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		createWatcherFn: func(_ context.Context, req *connect.Request[filesystempb.CreateWatcherRequest]) (*connect.Response[filesystempb.CreateWatcherResponse], error) {
			checkUser(req.Header().Get("X-User-ID"))
			return connect.NewResponse(&filesystempb.CreateWatcherResponse{WatcherId: "w1"}), nil
		},
		getWatcherEventsFn: func(_ context.Context, req *connect.Request[filesystempb.GetWatcherEventsRequest]) (*connect.Response[filesystempb.GetWatcherEventsResponse], error) {
			checkUser(req.Header().Get("X-User-ID"))
			return connect.NewResponse(&filesystempb.GetWatcherEventsResponse{}), nil
		},
		removeWatcherFn: func(_ context.Context, req *connect.Request[filesystempb.RemoveWatcherRequest]) (*connect.Response[filesystempb.RemoveWatcherResponse], error) {
			checkUser(req.Header().Get("X-User-ID"))
			return connect.NewResponse(&filesystempb.RemoveWatcherResponse{}), nil
		},
	})

	handle, err := sbx.Filesystem.WatchDir(context.Background(), dir, false, WithFileUser("root"))
	if err != nil {
		t.Fatalf("WatchDir: %v", err)
	}
	if _, err := handle.GetEvents(context.Background()); err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestFilesystemWatchHandleConcurrentStop exercises GetEvents and Stop from
// separate goroutines; run under -race it guards against a data race on the
// watcher's stopped flag.
func TestFilesystemWatchHandleConcurrentStop(t *testing.T) {
	sbx := newFilesystemRPCTestSandbox(t, &testFilesystemHandler{
		createWatcherFn: func(_ context.Context, _ *connect.Request[filesystempb.CreateWatcherRequest]) (*connect.Response[filesystempb.CreateWatcherResponse], error) {
			return connect.NewResponse(&filesystempb.CreateWatcherResponse{WatcherId: "w1"}), nil
		},
		getWatcherEventsFn: func(_ context.Context, _ *connect.Request[filesystempb.GetWatcherEventsRequest]) (*connect.Response[filesystempb.GetWatcherEventsResponse], error) {
			return connect.NewResponse(&filesystempb.GetWatcherEventsResponse{}), nil
		},
	})

	handle, err := sbx.Filesystem.WatchDir(context.Background(), "/tmp/watched", false)
	if err != nil {
		t.Fatalf("WatchDir: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			// Ignore errors: once stopped, GetEvents returns an error by design.
			_, _ = handle.GetEvents(context.Background())
		}
	}()
	go func() {
		defer wg.Done()
		_ = handle.Stop(context.Background())
	}()
	wg.Wait()
}
