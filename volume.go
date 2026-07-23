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
	"strconv"
	"strings"
	"time"
)

// VolumeRequestTimeout is the default timeout for volume management and content
// requests, matching the reference SDKs' 60 s request timeout.
const VolumeRequestTimeout = 60 * time.Second

// VolumeFileTimeout is the default timeout for volume file transfers (read and
// write), matching the reference SDKs' 1 h FILE_TIMEOUT.
const VolumeFileTimeout = time.Hour

// volumeContentRoute is the base path for the volume content API.
const volumeContentRoute = "/volumecontent"

// VolumeFileType is the type of a volume entry.
type VolumeFileType string

const (
	VolumeFileTypeUnknown   VolumeFileType = "unknown"
	VolumeFileTypeFile      VolumeFileType = "file"
	VolumeFileTypeDirectory VolumeFileType = "directory"
	VolumeFileTypeSymlink   VolumeFileType = "symlink"
)

// VolumeInfo identifies a volume (management API list/summary shape).
type VolumeInfo struct {
	VolumeID string `json:"volumeID"`
	Name     string `json:"name"`
}

// VolumeAndToken is VolumeInfo plus the per-volume auth token used for content
// API operations. Returned by GetVolumeInfo and CreateVolume.
type VolumeAndToken struct {
	VolumeID string `json:"volumeID"`
	Name     string `json:"name"`
	Token    string `json:"token"`
}

// VolumeEntryStat describes a file or directory inside a volume.
type VolumeEntryStat struct {
	Name   string
	Type   VolumeFileType
	Path   string
	Size   int64
	Mode   uint32
	UID    int
	GID    int
	ATime  time.Time
	MTime  time.Time
	CTime  time.Time
	Target string // symlink target, "" when absent
}

// volumeEntryStatWire is the on-the-wire representation of VolumeEntryStat.
// Timestamps arrive as ISO-8601 strings and are parsed into time.Time.
type volumeEntryStatWire struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	UID    int    `json:"uid"`
	GID    int    `json:"gid"`
	ATime  string `json:"atime"`
	MTime  string `json:"mtime"`
	CTime  string `json:"ctime"`
	Target string `json:"target,omitempty"`
}

// parseVolumeTime parses an ISO-8601 timestamp, tolerating both RFC3339 and
// RFC3339Nano forms. An empty string yields the zero time.
func parseVolumeTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// toVolumeEntryStat converts a wire struct to the public VolumeEntryStat.
func (w *volumeEntryStatWire) toVolumeEntryStat() (VolumeEntryStat, error) {
	atime, err := parseVolumeTime(w.ATime)
	if err != nil {
		return VolumeEntryStat{}, fmt.Errorf("e2b: parse atime %q: %w", w.ATime, err)
	}
	mtime, err := parseVolumeTime(w.MTime)
	if err != nil {
		return VolumeEntryStat{}, fmt.Errorf("e2b: parse mtime %q: %w", w.MTime, err)
	}
	ctime, err := parseVolumeTime(w.CTime)
	if err != nil {
		return VolumeEntryStat{}, fmt.Errorf("e2b: parse ctime %q: %w", w.CTime, err)
	}
	return VolumeEntryStat{
		Name:   w.Name,
		Type:   VolumeFileType(w.Type),
		Path:   w.Path,
		Size:   w.Size,
		Mode:   w.Mode,
		UID:    w.UID,
		GID:    w.GID,
		ATime:  atime,
		MTime:  mtime,
		CTime:  ctime,
		Target: w.Target,
	}, nil
}

// Volume is a handle to an E2B volume for content operations (list, read,
// write, etc.). Obtain one from Client.CreateVolume or Client.ConnectVolume.
//
// Volume management (create, list, destroy) lives on Client; content operations
// live here because they authenticate with the per-volume Token rather than the
// account API key.
type Volume struct {
	// VolumeID is the unique identifier of the volume.
	VolumeID string

	// Name is the human-readable volume name.
	Name string

	// Token is the per-volume bearer token used for content API operations.
	Token string

	client *Client // for httpClient + base URL/domain
}

// AsMount returns a VolumeMount for this volume at the given sandbox path,
// suitable for SandboxConfig.VolumeMounts.
func (v *Volume) AsMount(path string) VolumeMount {
	return VolumeMount{Name: v.Name, Path: path}
}

// volumeReqConfig holds optional query parameters for content operations. Nil
// pointers mean "unset" and are omitted from the request, distinguishing an
// unset value from a legitimate zero (mode=0, uid=0).
type volumeReqConfig struct {
	uid   *int
	gid   *int
	mode  *uint32
	force *bool
	depth *int
}

// VolumeWriteOption configures MakeDir and WriteFile calls.
type VolumeWriteOption interface{ applyVolumeWrite(*volumeReqConfig) }

// VolumeMetadataOption configures UpdateMetadata calls.
type VolumeMetadataOption interface{ applyVolumeMetadata(*volumeReqConfig) }

// VolumeListOption configures List calls.
type VolumeListOption interface{ applyVolumeList(*volumeReqConfig) }

type volumeUIDOption int

func (o volumeUIDOption) applyVolumeWrite(c *volumeReqConfig)    { u := int(o); c.uid = &u }
func (o volumeUIDOption) applyVolumeMetadata(c *volumeReqConfig) { u := int(o); c.uid = &u }

// WithVolumeUID sets the owner UID for MakeDir, WriteFile, and UpdateMetadata.
func WithVolumeUID(uid int) interface {
	VolumeWriteOption
	VolumeMetadataOption
} {
	return volumeUIDOption(uid)
}

type volumeGIDOption int

func (o volumeGIDOption) applyVolumeWrite(c *volumeReqConfig)    { g := int(o); c.gid = &g }
func (o volumeGIDOption) applyVolumeMetadata(c *volumeReqConfig) { g := int(o); c.gid = &g }

// WithVolumeGID sets the owner GID for MakeDir, WriteFile, and UpdateMetadata.
func WithVolumeGID(gid int) interface {
	VolumeWriteOption
	VolumeMetadataOption
} {
	return volumeGIDOption(gid)
}

type volumeModeOption uint32

func (o volumeModeOption) applyVolumeWrite(c *volumeReqConfig)    { m := uint32(o); c.mode = &m }
func (o volumeModeOption) applyVolumeMetadata(c *volumeReqConfig) { m := uint32(o); c.mode = &m }

// WithVolumeMode sets the file mode for MakeDir, WriteFile, and UpdateMetadata.
func WithVolumeMode(mode uint32) interface {
	VolumeWriteOption
	VolumeMetadataOption
} {
	return volumeModeOption(mode)
}

type volumeForceOption bool

func (o volumeForceOption) applyVolumeWrite(c *volumeReqConfig) { f := bool(o); c.force = &f }

// WithVolumeForce controls the force flag for MakeDir (create parent
// directories) and WriteFile (overwrite an existing file).
func WithVolumeForce(force bool) VolumeWriteOption { return volumeForceOption(force) }

type volumeDepthOption int

func (o volumeDepthOption) applyVolumeList(c *volumeReqConfig) { d := int(o); c.depth = &d }

// WithVolumeDepth sets how many directory layers List recurses into. The server
// default is 1 when unset.
func WithVolumeDepth(depth int) VolumeListOption { return volumeDepthOption(depth) }

// contentURL builds a content-API URL of the form
// <apiBaseURL>/volumecontent/<VolumeID>/<sub>?path=<path>&... The path argument
// is always sent as a query parameter; the volume ID is the only path segment.
func (v *Volume) contentURL(sub, path string, q url.Values) (string, error) {
	u, err := url.Parse(v.client.apiBaseURL + volumeContentRoute + "/" + url.PathEscape(v.VolumeID) + "/" + sub)
	if err != nil {
		return "", fmt.Errorf("e2b: build volume content URL: %w", err)
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("path", path)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// addOptionalParams adds the set optional parameters to q.
func addOptionalParams(q url.Values, c *volumeReqConfig) {
	if c.uid != nil {
		q.Set("uid", strconv.Itoa(*c.uid))
	}
	if c.gid != nil {
		q.Set("gid", strconv.Itoa(*c.gid))
	}
	if c.mode != nil {
		q.Set("mode", strconv.FormatUint(uint64(*c.mode), 10))
	}
	if c.force != nil {
		q.Set("force", strconv.FormatBool(*c.force))
	}
	if c.depth != nil {
		q.Set("depth", strconv.Itoa(*c.depth))
	}
}

// doVolumeContent issues a bearer-authenticated request against the content API.
// It guards against a missing token, sets the SDK User-Agent, and returns the
// raw *http.Response so callers can stream the body. The caller owns Body and
// must close it. A non-nil error means Body is already closed.
func (v *Volume) doVolumeContent(ctx context.Context, method, reqURL, contentType string, body io.Reader) (*http.Response, error) {
	if v.Token == "" {
		return nil, &AuthenticationError{Message: "volume token is required for content operations"}
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("e2b: build volume content request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+v.Token)
	req.Header.Set("User-Agent", userAgent)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := v.client.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send volume content request: %w", err)
	}
	return resp, nil
}

// volumeErrorFromResponse reads and closes resp.Body and returns a *VolumeError
// carrying the API-reported message when present.
func volumeErrorFromResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return &VolumeError{StatusCode: resp.StatusCode, Message: volumeErrorMessage(body)}
}

// volumeErrorMessage extracts a {"message": "..."} field from a body, falling
// back to the raw (trimmed) body when it is not JSON.
func volumeErrorMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		return payload.Message
	}
	return strings.TrimSpace(string(body))
}

// decodeVolumeEntryStat decodes and converts a single VolumeEntryStat from resp,
// then closes the body.
func decodeVolumeEntryStat(resp *http.Response) (*VolumeEntryStat, error) {
	defer func() { _ = resp.Body.Close() }()
	var wire volumeEntryStatWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("e2b: decode volume entry: %w", err)
	}
	stat, err := wire.toVolumeEntryStat()
	if err != nil {
		return nil, err
	}
	return &stat, nil
}

// contextWithTimeout applies d as a context timeout when d > 0, returning the
// (possibly wrapped) context and a cancel func that is always safe to call.
func contextWithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// List returns the entries in the directory at path. Depth defaults to 1 on the
// server; use WithVolumeDepth to recurse deeper. A missing path yields
// *FileNotFoundError.
func (v *Volume) List(ctx context.Context, path string, opts ...VolumeListOption) ([]VolumeEntryStat, error) {
	var cfg volumeReqConfig
	for _, o := range opts {
		o.applyVolumeList(&cfg)
	}

	ctx, cancel := contextWithTimeout(ctx, VolumeRequestTimeout)
	defer cancel()

	q := url.Values{}
	addOptionalParams(q, &cfg)
	reqURL, err := v.contentURL("dir", path, q)
	if err != nil {
		return nil, err
	}

	resp, err := v.doVolumeContent(ctx, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		defer func() { _ = resp.Body.Close() }()
		var wires []volumeEntryStatWire
		if err := json.NewDecoder(resp.Body).Decode(&wires); err != nil {
			return nil, fmt.Errorf("e2b: decode volume listing: %w", err)
		}
		entries := make([]VolumeEntryStat, len(wires))
		for i := range wires {
			stat, err := wires[i].toVolumeEntryStat()
			if err != nil {
				return nil, err
			}
			entries[i] = stat
		}
		return entries, nil
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, &FileNotFoundError{Path: path}
	default:
		return nil, volumeErrorFromResponse(resp)
	}
}

// MakeDir creates the directory at path. Use WithVolumeForce(true) to create
// missing parent directories, and WithVolumeUID/GID/Mode to set ownership.
func (v *Volume) MakeDir(ctx context.Context, path string, opts ...VolumeWriteOption) (*VolumeEntryStat, error) {
	var cfg volumeReqConfig
	for _, o := range opts {
		o.applyVolumeWrite(&cfg)
	}

	ctx, cancel := contextWithTimeout(ctx, VolumeRequestTimeout)
	defer cancel()

	q := url.Values{}
	addOptionalParams(q, &cfg)
	reqURL, err := v.contentURL("dir", path, q)
	if err != nil {
		return nil, err
	}

	resp, err := v.doVolumeContent(ctx, http.MethodPost, reqURL, "", nil)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return decodeVolumeEntryStat(resp)
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, &FileNotFoundError{Path: path}
	default:
		return nil, volumeErrorFromResponse(resp)
	}
}

// GetInfo returns metadata for the file or directory at path. A missing path
// yields *FileNotFoundError.
func (v *Volume) GetInfo(ctx context.Context, path string) (*VolumeEntryStat, error) {
	ctx, cancel := contextWithTimeout(ctx, VolumeRequestTimeout)
	defer cancel()

	reqURL, err := v.contentURL("path", path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := v.doVolumeContent(ctx, http.MethodGet, reqURL, "", nil)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return decodeVolumeEntryStat(resp)
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, &FileNotFoundError{Path: path}
	default:
		return nil, volumeErrorFromResponse(resp)
	}
}

// Exists reports whether a file or directory exists at path. It returns false
// (nil error) when the path is not found, and propagates other errors.
func (v *Volume) Exists(ctx context.Context, path string) (bool, error) {
	_, err := v.GetInfo(ctx, path)
	if err != nil {
		var fnf *FileNotFoundError
		if errors.As(err, &fnf) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// UpdateMetadata updates the uid/gid/mode of the entry at path. Only the
// options that are set are sent. A missing path yields *FileNotFoundError.
func (v *Volume) UpdateMetadata(ctx context.Context, path string, opts ...VolumeMetadataOption) (*VolumeEntryStat, error) {
	var cfg volumeReqConfig
	for _, o := range opts {
		o.applyVolumeMetadata(&cfg)
	}

	ctx, cancel := contextWithTimeout(ctx, VolumeRequestTimeout)
	defer cancel()

	reqURL, err := v.contentURL("path", path, nil)
	if err != nil {
		return nil, err
	}

	payload := make(map[string]any)
	if cfg.uid != nil {
		payload["uid"] = *cfg.uid
	}
	if cfg.gid != nil {
		payload["gid"] = *cfg.gid
	}
	if cfg.mode != nil {
		payload["mode"] = *cfg.mode
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal update metadata request: %w", err)
	}

	resp, err := v.doVolumeContent(ctx, http.MethodPatch, reqURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return decodeVolumeEntryStat(resp)
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, &FileNotFoundError{Path: path}
	default:
		return nil, volumeErrorFromResponse(resp)
	}
}

// ReadFile fetches the content of the file at path as a stream. The caller must
// close the returned ReadCloser when done. A missing path yields
// *FileNotFoundError. The request uses the 1 h file timeout, bounded by ctx.
// For small files, ReadFileBytes or ReadFileString are more convenient.
func (v *Volume) ReadFile(ctx context.Context, path string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, VolumeFileTimeout)

	reqURL, err := v.contentURL("file", path, nil)
	if err != nil {
		cancel()
		return nil, err
	}

	resp, err := v.doVolumeContent(ctx, http.MethodGet, reqURL, "", nil)
	if err != nil {
		cancel()
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}, nil
	case http.StatusNotFound:
		_ = resp.Body.Close()
		cancel()
		return nil, &FileNotFoundError{Path: path}
	default:
		err := volumeErrorFromResponse(resp)
		cancel()
		return nil, err
	}
}

// ReadFileBytes fetches the full content of the file at path as a byte slice.
func (v *Volume) ReadFileBytes(ctx context.Context, path string) ([]byte, error) {
	rc, err := v.ReadFile(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// ReadFileString fetches the full content of the file at path as a string.
func (v *Volume) ReadFileString(ctx context.Context, path string) (string, error) {
	b, err := v.ReadFileBytes(ctx, path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WriteFile uploads data from r to the file at path, streaming the body without
// buffering. The file is created if it does not exist. Use WithVolumeForce(true)
// to overwrite an existing file and WithVolumeUID/GID/Mode to set ownership. The
// request uses the 1 h file timeout, bounded by ctx.
func (v *Volume) WriteFile(ctx context.Context, path string, r io.Reader, opts ...VolumeWriteOption) (*VolumeEntryStat, error) {
	var cfg volumeReqConfig
	for _, o := range opts {
		o.applyVolumeWrite(&cfg)
	}

	ctx, cancel := context.WithTimeout(ctx, VolumeFileTimeout)
	defer cancel()

	q := url.Values{}
	addOptionalParams(q, &cfg)
	reqURL, err := v.contentURL("file", path, q)
	if err != nil {
		return nil, err
	}

	resp, err := v.doVolumeContent(ctx, http.MethodPut, reqURL, "application/octet-stream", r)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return decodeVolumeEntryStat(resp)
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, &FileNotFoundError{Path: path}
	default:
		return nil, volumeErrorFromResponse(resp)
	}
}

// WriteFileBytes writes b to the file at path.
func (v *Volume) WriteFileBytes(ctx context.Context, path string, b []byte, opts ...VolumeWriteOption) (*VolumeEntryStat, error) {
	return v.WriteFile(ctx, path, bytes.NewReader(b), opts...)
}

// WriteFileString writes s to the file at path.
func (v *Volume) WriteFileString(ctx context.Context, path string, s string, opts ...VolumeWriteOption) (*VolumeEntryStat, error) {
	return v.WriteFile(ctx, path, strings.NewReader(s), opts...)
}

// Remove deletes the file or directory at path. A missing path yields
// *FileNotFoundError.
func (v *Volume) Remove(ctx context.Context, path string) error {
	ctx, cancel := contextWithTimeout(ctx, VolumeRequestTimeout)
	defer cancel()

	reqURL, err := v.contentURL("path", path, nil)
	if err != nil {
		return err
	}

	resp, err := v.doVolumeContent(ctx, http.MethodDelete, reqURL, "", nil)
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		_ = resp.Body.Close()
		return nil
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return &FileNotFoundError{Path: path}
	default:
		return volumeErrorFromResponse(resp)
	}
}
