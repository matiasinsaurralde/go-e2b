# Filesystem Read/Write — Implementation Proposal

## Overview

Add a `FilesystemService` to the Go SDK that exposes file read and write operations
against the E2B sandbox environment daemon (`envd`). Read/write content goes over
plain HTTP (`GET /files`, `POST /files`); this matches the Python SDK exactly and is
intentionally separate from the Connect-RPC `Filesystem` service (which handles
metadata: `Stat`, `ListDir`, `MakeDir`, `Move`, `Remove`).

---

## Transport

| Operation | Method | URL | Notes |
|-----------|--------|-----|-------|
| Read      | `GET`  | `https://<envdPort>-<sandboxID>.<domain>/files?path=<path>` | Response body is raw file bytes |
| Write     | `POST` | `https://<envdPort>-<sandboxID>.<domain>/files?path=<path>` | Body: raw bytes, `Content-Type: application/octet-stream` |

Auth header on every request: `X-Access-Token: <envdAccessToken>` (same pattern as
`CommandService`).

---

## New file: `filesystem.go`

```go
package e2b

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const filesRoute = "/files"

// FileInfo describes the result of a successful write operation.
type FileInfo struct {
	// Name is the base name of the written file.
	Name string `json:"name"`
	// Path is the absolute path of the written file inside the sandbox.
	Path string `json:"path"`
}

// FilesystemService provides file read and write operations within a sandbox.
type FilesystemService struct {
	sandbox *Sandbox
}

func newFilesystemService(sbx *Sandbox) *FilesystemService {
	return &FilesystemService{sandbox: sbx}
}

// ReadOption configures a single Read call.
type ReadOption func(*readConfig)

type readConfig struct {
	user    string
	timeout time.Duration
}

// WriteOption configures a single Write call.
type WriteOption func(*writeConfig)

type writeConfig struct {
	user    string
	timeout time.Duration
}

// WithFileUser sets the sandbox user for the file operation.
func WithFileUser(user string) interface {
	ReadOption
	WriteOption
} {
	// Return a value that satisfies both option types via a small adapter.
	return fileUserOption(user)
}

type fileUserOption string

func (o fileUserOption) applyRead(rc *readConfig)   { rc.user = string(o) }
func (o fileUserOption) applyWrite(wc *writeConfig) { wc.user = string(o) }

// WithReadTimeout sets the HTTP timeout for a Read call.
func WithReadTimeout(d time.Duration) ReadOption {
	return func(rc *readConfig) { rc.timeout = d }
}

// WithWriteTimeout sets the HTTP timeout for a Write call.
func WithWriteTimeout(d time.Duration) WriteOption {
	return func(wc *writeConfig) { wc.timeout = d }
}

// Read fetches the content of the file at path inside the sandbox.
// The caller is responsible for closing the returned ReadCloser.
// Use io.ReadAll to get the full content as []byte, or stream it directly.
func (f *FilesystemService) Read(ctx context.Context, path string, opts ...ReadOption) (io.ReadCloser, error) {
	rc := &readConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o(rc)
	}

	reqURL, err := f.fileURL(path, rc.user)
	if err != nil {
		return nil, err
	}

	if rc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
		// cancel is called when the caller closes the body or the context is done.
		// We attach it to a wrapper below.
		_ = cancel // see cancelReadCloser
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("e2b: build read request: %w", err)
	}
	req.Header.Set("X-Access-Token", f.sandbox.accessToken)

	resp, err := f.sandbox.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send read request: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Body, nil
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, &FileNotFoundError{Path: path}
	default:
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(body)}
	}
}

// ReadBytes is a convenience wrapper around Read that returns the full file
// content as a byte slice.
func (f *FilesystemService) ReadBytes(ctx context.Context, path string, opts ...ReadOption) ([]byte, error) {
	rc, err := f.Read(ctx, path, opts...)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// ReadString is a convenience wrapper that returns the file content as a string.
func (f *FilesystemService) ReadString(ctx context.Context, path string, opts ...ReadOption) (string, error) {
	b, err := f.ReadBytes(ctx, path, opts...)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Write uploads data to path inside the sandbox, creating parent directories
// as needed. If the file already exists it is overwritten.
// data may be any io.Reader (including *bytes.Reader, *os.File, strings.NewReader, etc.).
func (f *FilesystemService) Write(ctx context.Context, path string, data io.Reader, opts ...WriteOption) (*FileInfo, error) {
	wc := &writeConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o(wc)
	}

	if wc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wc.timeout)
		defer cancel()
	}

	reqURL, err := f.fileURL(path, wc.user)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, data)
	if err != nil {
		return nil, fmt.Errorf("e2b: build write request: %w", err)
	}
	req.Header.Set("X-Access-Token", f.sandbox.accessToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := f.sandbox.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send write request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// envd returns a JSON array: [{"name":"...","path":"..."}]
		var results []FileInfo
		if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
			return nil, fmt.Errorf("e2b: decode write response: %w", err)
		}
		if len(results) == 0 {
			return nil, &Error{Message: "write: empty response from envd"}
		}
		return &results[0], nil
	case http.StatusNotFound:
		return nil, &FileNotFoundError{Path: path}
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(body)}
	}
}

// WriteBytes is a convenience wrapper that accepts a byte slice.
func (f *FilesystemService) WriteBytes(ctx context.Context, path string, data []byte, opts ...WriteOption) (*FileInfo, error) {
	return f.Write(ctx, path, bytes.NewReader(data), opts...)
}

// WriteString is a convenience wrapper that accepts a string.
func (f *FilesystemService) WriteString(ctx context.Context, path string, data string, opts ...WriteOption) (*FileInfo, error) {
	return f.Write(ctx, path, strings.NewReader(data), opts...)
}

// fileURL constructs the envd /files URL for the given path and optional user.
func (f *FilesystemService) fileURL(path, user string) (string, error) {
	base := f.sandbox.envdBaseURL() + filesRoute
	q := url.Values{"path": {path}}
	if user != "" {
		q.Set("username", user)
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("e2b: parse files URL: %w", err)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
```

---

## Changes to `sandbox.go`

Add `Filesystem *FilesystemService` to the `Sandbox` struct and initialise it in
`NewSandboxWithContext`:

```go
type Sandbox struct {
    ID          string
    Commands    *CommandService
    Filesystem  *FilesystemService   // ← new

    accessToken   string
    // … rest unchanged
}

// In NewSandboxWithContext, after sbx.Commands = newCommandService(sbx):
sbx.Filesystem = newFilesystemService(sbx)
```

---

## New error type in `errors.go`

```go
// FileNotFoundError is returned when the requested path does not exist in the sandbox.
type FileNotFoundError struct {
    Path string
}

func (e *FileNotFoundError) Error() string {
    return fmt.Sprintf("e2b: file not found: %s", e.Path)
}
```

---

## Usage examples

```go
// Write a string
info, err := sandbox.Filesystem.WriteString(ctx, "/home/user/hello.txt", "hello, world\n")

// Write from a file on disk
f, _ := os.Open("data.csv")
defer f.Close()
info, err := sandbox.Filesystem.Write(ctx, "/home/user/data.csv", f)

// Read back as string
content, err := sandbox.Filesystem.ReadString(ctx, "/home/user/hello.txt")

// Stream a large file
rc, err := sandbox.Filesystem.Read(ctx, "/home/user/data.csv")
defer rc.Close()
io.Copy(os.Stdout, rc)

// Override user
info, err := sandbox.Filesystem.WriteString(ctx, "/root/cfg", "data",
    WithFileUser("root").(WriteOption))
```

---

## Test plan (`filesystem_test.go`)

| Test | What it covers |
|------|----------------|
| `TestWriteStringReadString` | round-trip: write text, read back, assert equal |
| `TestWriteBytesReadBytes` | binary content (non-UTF-8 bytes) |
| `TestWriteStream` | `io.Pipe` writer → `Read` stream consumer |
| `TestOverwrite` | write twice to same path, assert latest content |
| `TestReadNotFound` | assert `*FileNotFoundError` on missing path |
| `TestWriteCreatesParentDirs` | write to `/a/b/c/file.txt`, verify no error |
| `TestWithFileUser` | write/read with explicit user, assert no auth error |
| `TestContextCancellation` | cancel ctx mid-flight, assert `context.Canceled` |
| `TestWriteTimeout` | `WithWriteTimeout(1ms)` on large payload, assert timeout |

Integration tests use a real sandbox (requires `E2B_API_KEY`); unit tests mock the
`httpClient` with `httptest.NewServer`.

---

## Design decisions

- **Streaming first.** `Read` returns `io.ReadCloser` so callers can stream large
  files without buffering. `ReadBytes`/`ReadString` are thin wrappers for ergonomics.
- **`io.Reader` for writes.** `Write` accepts `io.Reader`, enabling zero-copy uploads
  from disk, network, or in-memory buffers. `WriteBytes`/`WriteString` cover the
  common cases.
- **No multipart fallback.** The Python SDK has a legacy multipart path for older
  envd versions. This SDK targets current envd (`application/octet-stream`), keeping
  the implementation simple. A version-negotiation layer can be added later if needed.
- **Option functions mirror `CommandService`.** `ReadOption`/`WriteOption` keep the
  API consistent with the existing codebase.
- **Separate error type.** `FileNotFoundError` (distinct from the generic `Error`)
  lets callers use `errors.As` for targeted handling without string matching.
- **No global state.** `FilesystemService` holds only a pointer to `Sandbox`; safe
  for concurrent use since all HTTP calls are stateless.
