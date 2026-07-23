package e2b

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CreateVolume creates a new volume and returns a handle usable for content
// operations. The returned Volume carries the volume's per-volume auth token.
func (c *Client) CreateVolume(ctx context.Context, name string) (*Volume, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, fmt.Errorf("e2b: marshal create volume request: %w", err)
	}

	var vt VolumeAndToken
	if err := c.doManagementJSON(ctx, http.MethodPost, c.apiBaseURL+"/volumes", bytes.NewReader(body), &vt, ""); err != nil {
		return nil, err
	}

	return &Volume{VolumeID: vt.VolumeID, Name: vt.Name, Token: vt.Token, client: c}, nil
}

// ConnectVolume returns a Volume handle for an existing volume by ID. It calls
// the management API to fetch the volume's name and content-API token, so the
// returned handle is immediately usable for content operations.
func (c *Client) ConnectVolume(ctx context.Context, volumeID string) (*Volume, error) {
	vt, err := c.GetVolumeInfo(ctx, volumeID)
	if err != nil {
		return nil, err
	}
	return &Volume{VolumeID: vt.VolumeID, Name: vt.Name, Token: vt.Token, client: c}, nil
}

// GetVolumeInfo returns a volume's id, name, and content-API token. It returns
// *VolumeNotFoundError when the volume does not exist.
func (c *Client) GetVolumeInfo(ctx context.Context, volumeID string) (*VolumeAndToken, error) {
	var vt VolumeAndToken
	err := c.doManagementJSON(ctx, http.MethodGet, c.apiBaseURL+"/volumes/"+volumeID, nil, &vt, volumeID)
	if err != nil {
		return nil, err
	}
	return &vt, nil
}

// ListVolumes returns all volumes for this client's API key.
func (c *Client) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	var volumes []VolumeInfo
	if err := c.doManagementJSON(ctx, http.MethodGet, c.apiBaseURL+"/volumes", nil, &volumes, ""); err != nil {
		return nil, err
	}
	return volumes, nil
}

// DestroyVolume deletes a volume. It returns false (nil error) when the volume
// does not exist, and true when it was destroyed. Other failures return a
// *VolumeError.
func (c *Client) DestroyVolume(ctx context.Context, volumeID string) (bool, error) {
	ctx, cancel := contextWithTimeout(ctx, VolumeRequestTimeout)
	defer cancel()

	resp, err := c.doManagementRequest(ctx, http.MethodDelete, c.apiBaseURL+"/volumes/"+volumeID, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, nil
	default:
		return false, volumeErrorFromResponse(resp)
	}
}

// doManagementRequest issues an X-API-Key request against the management API and
// returns the raw response. The caller owns Body and must close it. A non-nil
// error means Body is already closed.
func (c *Client) doManagementRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("e2b: build volume management request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("e2b: send volume management request: %w", err)
	}
	return resp, nil
}

// doManagementJSON issues an X-API-Key request and decodes a 2xx JSON body into
// out. A 404 maps to *VolumeNotFoundError when volumeID is non-empty; other
// non-2xx statuses map to *VolumeError. Passing a nil out skips decoding.
func (c *Client) doManagementJSON(ctx context.Context, method, reqURL string, body io.Reader, out any, volumeID string) error {
	ctx, cancel := contextWithTimeout(ctx, VolumeRequestTimeout)
	defer cancel()

	resp, err := c.doManagementRequest(ctx, method, reqURL, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		if out == nil {
			return nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("e2b: decode volume management response: %w", err)
		}
		return nil
	case resp.StatusCode == http.StatusNotFound && volumeID != "":
		return &VolumeNotFoundError{VolumeID: volumeID}
	default:
		return volumeErrorFromResponse(resp)
	}
}
