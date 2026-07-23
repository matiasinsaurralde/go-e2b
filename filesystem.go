package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	filesystempb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/filesystem"
	"github.com/matiasinsaurralde/go-e2b/internal/gen/envd/filesystem/filesystemconnect"
)

const filesRoute = "/files"

// FileInfo describes a file written to the sandbox filesystem.
type FileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Type    string    `json:"type,omitempty"`
	Size    int64     `json:"size,omitempty"`
	Owner   string    `json:"owner,omitempty"`
	Group   string    `json:"group,omitempty"`
	ModTime time.Time `json:"-"`
}

// FilesystemService provides file read and write operations within a sandbox.
type FilesystemService struct {
	sandbox *Sandbox

	fsClientOnce sync.Once
	fsClient     filesystemconnect.FilesystemClient
}

func newFilesystemService(sbx *Sandbox) *FilesystemService {
	return &FilesystemService{sandbox: sbx}
}

func (f *FilesystemService) getFilesystemClient() filesystemconnect.FilesystemClient {
	f.fsClientOnce.Do(func() {
		f.fsClient = filesystemconnect.NewFilesystemClient(
			f.sandbox.client.httpClient,
			f.sandbox.envdBaseURL(),
		)
	})
	return f.fsClient
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

	resp, err := f.sandbox.client.httpClient.Do(req)
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

	resp, err := f.sandbox.client.httpClient.Do(req)
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
	return f.sandbox.filesURL(path, user)
}

// filesURL builds the envd /files URL for the given path and optional user,
// without any signature. path is omitted from the query when empty (upload to
// the default location); user is omitted when empty. It is the single source of
// truth for the /files route, shared by the filesystem service and the signed
// URL helpers.
func (s *Sandbox) filesURL(path, user string) (string, error) {
	u, err := url.Parse(s.envdBaseURL() + filesRoute)
	if err != nil {
		return "", fmt.Errorf("e2b: parse files URL: %w", err)
	}
	q := url.Values{}
	if path != "" {
		q.Set("path", path)
	}
	if user != "" {
		q.Set("username", user)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// newRPCRequest creates a Connect RPC request with the sandbox access token
// and optional user header. For gRPC operations, user context is passed via
// the X-User-ID header (same as Python SDK's authentication_header()).
func newRPCRequest[T any](accessToken string, msg *T, user string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("X-Access-Token", accessToken)
	if user != "" {
		req.Header().Set("X-User-ID", user)
	}
	return req
}

// ===== List — list directory contents =====
//
// Uses the Filesystem/ListDir gRPC endpoint to enumerate directory entries.
func (f *FilesystemService) List(ctx context.Context, path string, opts ...ReadOption) ([]FileInfo, error) {
	rc := &readConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o.applyRead(rc)
	}

	if rc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
		defer cancel()
	}

	req := newRPCRequest(f.sandbox.accessToken, &filesystempb.ListDirRequest{
		Path:  path,
		Depth: 1,
	}, rc.user)

	resp, err := f.getFilesystemClient().ListDir(ctx, req)
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, &FileNotFoundError{Path: path}
		}
		return nil, fmt.Errorf("e2b: list: %w", err)
	}

	entries := make([]FileInfo, len(resp.Msg.Entries))
	for i, e := range resp.Msg.Entries {
		entries[i] = entryToFileInfo(e)
	}
	return entries, nil
}

// ===== Stat — get single file/dir metadata =====
//
// Uses the Filesystem/Stat gRPC endpoint.
func (f *FilesystemService) Stat(ctx context.Context, path string, opts ...ReadOption) (*FileInfo, error) {
	rc := &readConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o.applyRead(rc)
	}

	if rc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
		defer cancel()
	}

	req := newRPCRequest(f.sandbox.accessToken, &filesystempb.StatRequest{
		Path: path,
	}, rc.user)

	resp, err := f.getFilesystemClient().Stat(ctx, req)
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, &FileNotFoundError{Path: path}
		}
		return nil, fmt.Errorf("e2b: stat: %w", err)
	}

	info := entryToFileInfo(resp.Msg.Entry)
	return &info, nil
}

// Exists checks whether a file or directory exists at path.
// Corresponds to Python SDK's files.exists().
// Returns false (not an error) when the path is not found.
func (f *FilesystemService) Exists(ctx context.Context, path string, opts ...ReadOption) (bool, error) {
	_, err := f.Stat(ctx, path, opts...)
	if err != nil {
		var fnf *FileNotFoundError
		if errors.As(err, &fnf) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ===== MakeDir — create directory =====
//
// Uses the Filesystem/MakeDir gRPC endpoint.
// Creates directories recursively and is idempotent (mkdir -p semantics):
// calling it on an existing directory is not an error. envd returns HTTP 200
// with the entry in that case rather than an AlreadyExists code.
func (f *FilesystemService) MakeDir(ctx context.Context, path string, opts ...WriteOption) error {
	wc := &writeConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o.applyWrite(wc)
	}

	if wc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wc.timeout)
		defer cancel()
	}

	req := newRPCRequest(f.sandbox.accessToken, &filesystempb.MakeDirRequest{
		Path: path,
	}, wc.user)

	_, err := f.getFilesystemClient().MakeDir(ctx, req)
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return &FileNotFoundError{Path: path}
		}
		return fmt.Errorf("e2b: mkdir: %w", err)
	}
	return nil
}

// ===== Remove — delete file or dir =====
//
// Uses the Filesystem/Remove gRPC endpoint.
// When deleting a directory it is recursively deleted.
func (f *FilesystemService) Remove(ctx context.Context, path string, opts ...ReadOption) error {
	rc := &readConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o.applyRead(rc)
	}

	if rc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
		defer cancel()
	}

	req := newRPCRequest(f.sandbox.accessToken, &filesystempb.RemoveRequest{
		Path: path,
	}, rc.user)

	_, err := f.getFilesystemClient().Remove(ctx, req)
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return &FileNotFoundError{Path: path}
		}
		return fmt.Errorf("e2b: remove: %w", err)
	}
	return nil
}

// ===== Rename — rename/move file or directory =====
//
// Uses the Filesystem/Move gRPC endpoint.
// Corresponds to Python SDK's files.rename().
func (f *FilesystemService) Rename(ctx context.Context, oldPath, newPath string, opts ...WriteOption) (*FileInfo, error) {
	wc := &writeConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o.applyWrite(wc)
	}

	if wc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wc.timeout)
		defer cancel()
	}

	req := newRPCRequest(f.sandbox.accessToken, &filesystempb.MoveRequest{
		Source:      oldPath,
		Destination: newPath,
	}, wc.user)

	resp, err := f.getFilesystemClient().Move(ctx, req)
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, &FileNotFoundError{Path: oldPath}
		}
		return nil, fmt.Errorf("e2b: rename: %w", err)
	}

	info := entryToFileInfo(resp.Msg.Entry)
	return &info, nil
}

// ===== WatchDir — watch directory for filesystem events =====
//
// Uses the non-streaming CreateWatcher + GetWatcherEvents RPCs (sync polling approach).
// Returns a WatchHandle that can be used to get events and stop watching.
func (f *FilesystemService) WatchDir(ctx context.Context, path string, recursive bool, opts ...ReadOption) (*WatchHandle, error) {
	rc := &readConfig{timeout: DefaultCommandTimeout}
	for _, o := range opts {
		o.applyRead(rc)
	}

	if rc.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rc.timeout)
		defer cancel()
	}

	createReq := newRPCRequest(f.sandbox.accessToken, &filesystempb.CreateWatcherRequest{
		Path:      path,
		Recursive: recursive,
	}, rc.user)

	createResp, err := f.getFilesystemClient().CreateWatcher(ctx, createReq)
	if err != nil {
		return nil, fmt.Errorf("e2b: watch dir: %w", err)
	}

	return &WatchHandle{
		watcherID: createResp.Msg.WatcherId,
		fs:        f,
		user:      rc.user,
	}, nil
}

// WatchHandle represents an active directory watcher.
// Use GetEvents to poll for new events and Stop to stop watching.
// A WatchHandle is safe for concurrent use: one goroutine may poll
// GetEvents while another calls Stop.
type WatchHandle struct {
	watcherID string
	fs        *FilesystemService
	user      string

	mu      sync.Mutex
	stopped bool
}

// GetEvents retrieves new filesystem events since the last call.
// Returns an empty slice if no events occurred.
func (w *WatchHandle) GetEvents(ctx context.Context) ([]FilesystemEvent, error) {
	w.mu.Lock()
	stopped := w.stopped
	w.mu.Unlock()
	if stopped {
		return nil, fmt.Errorf("e2b: watcher already stopped")
	}

	req := newRPCRequest(w.fs.sandbox.accessToken, &filesystempb.GetWatcherEventsRequest{
		WatcherId: w.watcherID,
	}, w.user)

	resp, err := w.fs.getFilesystemClient().GetWatcherEvents(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("e2b: get watcher events: %w", err)
	}

	events := make([]FilesystemEvent, len(resp.Msg.Events))
	for i, e := range resp.Msg.Events {
		events[i] = FilesystemEvent{
			Name: e.Name,
			Type: eventTypeToString(e.Type),
		}
		if e.Entry != nil {
			info := entryToFileInfo(e.Entry)
			events[i].Entry = &info
		}
	}
	return events, nil
}

// Stop stops the directory watcher and releases server-side resources.
// Stop is idempotent and safe to call concurrently with GetEvents.
func (w *WatchHandle) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return nil
	}

	req := newRPCRequest(w.fs.sandbox.accessToken, &filesystempb.RemoveWatcherRequest{
		WatcherId: w.watcherID,
	}, w.user)

	_, err := w.fs.getFilesystemClient().RemoveWatcher(ctx, req)
	if err != nil {
		return fmt.Errorf("e2b: stop watcher: %w", err)
	}

	w.stopped = true
	return nil
}

// FilesystemEvent represents a filesystem change event.
type FilesystemEvent struct {
	Name  string    `json:"name"`
	Type  string    `json:"type"`
	Entry *FileInfo `json:"entry,omitempty"`
}

// eventTypeToString converts a protobuf EventType to a human-readable string.
func eventTypeToString(t filesystempb.EventType) string {
	switch t {
	case filesystempb.EventType_EVENT_TYPE_CREATE:
		return "create"
	case filesystempb.EventType_EVENT_TYPE_WRITE:
		return "write"
	case filesystempb.EventType_EVENT_TYPE_REMOVE:
		return "remove"
	case filesystempb.EventType_EVENT_TYPE_RENAME:
		return "rename"
	case filesystempb.EventType_EVENT_TYPE_CHMOD:
		return "chmod"
	default:
		return "unknown"
	}
}

// entryToFileInfo converts a protobuf EntryInfo to a FileInfo.
// A nil entry yields a zero FileInfo so callers never panic on a
// success response that unexpectedly omits the entry.
func entryToFileInfo(e *filesystempb.EntryInfo) FileInfo {
	if e == nil {
		return FileInfo{}
	}
	info := FileInfo{
		Name:  e.Name,
		Path:  e.Path,
		Type:  fileTypeToString(e.Type),
		Size:  e.Size,
		Owner: e.Owner,
		Group: e.Group,
	}
	if e.ModifiedTime != nil {
		info.ModTime = e.ModifiedTime.AsTime()
	}
	return info
}

// fileTypeToString converts a protobuf FileType to a human-readable string.
func fileTypeToString(t filesystempb.FileType) string {
	switch t {
	case filesystempb.FileType_FILE_TYPE_FILE:
		return "file"
	case filesystempb.FileType_FILE_TYPE_DIRECTORY:
		return "directory"
	default:
		return "unknown"
	}
}
