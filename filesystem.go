package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const filesRoute = "/files"

// FileInfo describes a file written to the sandbox filesystem.
type FileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type,omitempty"`
}

// FilesystemService provides file read and write operations within a sandbox.
type FilesystemService struct {
	sandbox *Sandbox
}

func newFilesystemService(sbx *Sandbox) *FilesystemService {
	return &FilesystemService{sandbox: sbx}
}

// ReadOption configures a Read, ReadBytes, or ReadString call.
type ReadOption interface {
	applyRead(*readConfig)
}

// WriteOption configures a Write, WriteBytes, or WriteString call.
type WriteOption interface {
	applyWrite(*writeConfig)
}

type readConfig struct {
	user    string
	timeout time.Duration
}

type writeConfig struct {
	user    string
	timeout time.Duration
}

// fileUserOption implements both ReadOption and WriteOption.
type fileUserOption string

func (o fileUserOption) applyRead(rc *readConfig)   { rc.user = string(o) }
func (o fileUserOption) applyWrite(wc *writeConfig) { wc.user = string(o) }

// WithFileUser sets the sandbox user for the file operation.
// The returned value satisfies both ReadOption and WriteOption.
func WithFileUser(user string) fileUserOption { return fileUserOption(user) }

type readTimeoutOption struct{ d time.Duration }

func (o readTimeoutOption) applyRead(rc *readConfig) { rc.timeout = o.d }

// WithReadTimeout overrides the HTTP timeout for a Read call.
func WithReadTimeout(d time.Duration) ReadOption { return readTimeoutOption{d} }

type writeTimeoutOption struct{ d time.Duration }

func (o writeTimeoutOption) applyWrite(wc *writeConfig) { wc.timeout = o.d }

// WithWriteTimeout overrides the HTTP timeout for a Write call.
func WithWriteTimeout(d time.Duration) WriteOption { return writeTimeoutOption{d} }

// cancelReadCloser wraps an io.ReadCloser and calls cancel when closed,
// preventing context leaks when Read returns a streaming response body.
type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error {
	defer c.cancel()
	return c.ReadCloser.Close()
}

// Read fetches the raw content of the file at path inside the sandbox.
// The caller must close the returned ReadCloser when done.
// For small files, ReadBytes or ReadString are more convenient.
func (f *FilesystemService) Read(ctx context.Context, path string, opts ...ReadOption) (io.ReadCloser, error) {
	rc := &readConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o.applyRead(rc)
	}

	cancel := context.CancelFunc(func() {})
	if rc.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
	}

	reqURL, err := f.fileURL(path, rc.user)
	if err != nil {
		cancel()
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("e2b: build read request: %w", err)
	}
	req.Header.Set("X-Access-Token", f.sandbox.accessToken)

	resp, err := f.sandbox.httpClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("e2b: send read request: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}, nil
	case http.StatusNotFound:
		_ = resp.Body.Close()
		cancel()
		return nil, &FileNotFoundError{Path: path}
	default:
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		return nil, &Error{StatusCode: resp.StatusCode, Message: string(body)}
	}
}

// ReadBytes fetches the full content of the file at path as a byte slice.
func (f *FilesystemService) ReadBytes(ctx context.Context, path string, opts ...ReadOption) ([]byte, error) {
	rc, err := f.Read(ctx, path, opts...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// ReadString fetches the full content of the file at path as a string.
func (f *FilesystemService) ReadString(ctx context.Context, path string, opts ...ReadOption) (string, error) {
	b, err := f.ReadBytes(ctx, path, opts...)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Write uploads data from r to path inside the sandbox.
// The file is created if it does not exist and overwritten if it does.
// Parent directories are created automatically by envd.
// r may be any io.Reader: *os.File, *bytes.Reader, strings.NewReader, io.Pipe, etc.
func (f *FilesystemService) Write(ctx context.Context, path string, r io.Reader, opts ...WriteOption) (*FileInfo, error) {
	wc := &writeConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o.applyWrite(wc)
	}

	cancel := context.CancelFunc(func() {})
	if wc.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, wc.timeout)
	}
	defer cancel()

	reqURL, err := f.fileURL(path, wc.user)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, r)
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

// WriteBytes writes b to path inside the sandbox.
func (f *FilesystemService) WriteBytes(ctx context.Context, path string, b []byte, opts ...WriteOption) (*FileInfo, error) {
	return f.Write(ctx, path, bytes.NewReader(b), opts...)
}

// WriteString writes s to path inside the sandbox.
func (f *FilesystemService) WriteString(ctx context.Context, path string, s string, opts ...WriteOption) (*FileInfo, error) {
	return f.Write(ctx, path, strings.NewReader(s), opts...)
}

// fileURL constructs the envd /files URL for the given path and optional user.
func (f *FilesystemService) fileURL(path, user string) (string, error) {
	u, err := url.Parse(f.sandbox.envdBaseURL() + filesRoute)
	if err != nil {
		return "", fmt.Errorf("e2b: parse files URL: %w", err)
	}
	q := url.Values{"path": {path}}
	if user != "" {
		q.Set("username", user)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
