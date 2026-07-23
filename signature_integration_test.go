//go:build integration

package e2b

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"testing"
	"time"
)

// Run with:
//
//	E2B_API_KEY=e2b_xxx go test -tags=integration -v -run TestIntegrationSignedURL ./...

func newSecuredSignedURLSandbox(t *testing.T) *Sandbox {
	t.Helper()

	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		t.Skip("E2B_API_KEY not set, skipping integration test")
	}
	template := os.Getenv("E2B_TEMPLATE")
	if template == "" {
		template = "base"
	}

	client, err := NewClient(ClientConfig{APIKey: apiKey})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	sbx, err := client.NewSandbox(context.Background(), SandboxConfig{
		Template: template,
		Timeout:  300,
		Secure:   true,
	})
	if err != nil {
		t.Fatalf("NewSandbox: %v", err)
	}
	t.Cleanup(func() {
		if err := sbx.Close(); err != nil {
			t.Logf("cleanup Close: %v", err)
		}
	})

	if sbx.accessToken == "" {
		t.Fatalf("secured sandbox has no envd access token; cannot exercise signed URLs")
	}
	return sbx
}

// TestIntegrationSignedURLDownload writes a file via the RPC path, then fetches
// it back through a signed download URL with a plain HTTP client carrying NO
// access-token header — proving envd accepts the signature as the sole credential.
func TestIntegrationSignedURLDownload(t *testing.T) {
	sbx := newSecuredSignedURLSandbox(t)
	ctx := context.Background()

	const path = "/home/user/signed_download.txt"
	const content = "signed download round-trip"

	if _, err := sbx.Filesystem.WriteString(ctx, path, content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	url, err := sbx.DownloadURL(path, WithSignedURLUser("user"))
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}
	t.Logf("download URL: %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build GET: %v", err)
	}
	// Deliberately no X-Access-Token header.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET signed url: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status %d, body: %s", resp.StatusCode, string(body))
	}
	if string(body) != content {
		t.Fatalf("downloaded body = %q, want %q", string(body), content)
	}
}

// TestIntegrationSignedURLUpload uploads a file via a signed upload URL using a
// multipart/form-data POST (field "file"), then reads it back via the RPC path.
func TestIntegrationSignedURLUpload(t *testing.T) {
	sbx := newSecuredSignedURLSandbox(t)
	ctx := context.Background()

	const path = "/home/user/signed_upload.txt"
	const content = "signed upload round-trip"

	url, err := sbx.UploadURL(path, WithSignedURLUser("user"))
	if err != nil {
		t.Fatalf("UploadURL: %v", err)
	}
	t.Logf("upload URL: %s", url)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", "signed_upload.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		t.Fatalf("build POST: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	// Deliberately no X-Access-Token header.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST signed url: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status %d, body: %s", resp.StatusCode, string(respBody))
	}

	got, err := sbx.Filesystem.ReadString(ctx, path)
	if err != nil {
		t.Fatalf("ReadString: %v", err)
	}
	if got != content {
		t.Fatalf("uploaded content = %q, want %q", got, content)
	}
}

// TestIntegrationSignedURLExpiration verifies a time-limited signature is
// accepted while valid.
func TestIntegrationSignedURLExpiration(t *testing.T) {
	sbx := newSecuredSignedURLSandbox(t)
	ctx := context.Background()

	const path = "/home/user/signed_expiry.txt"
	const content = "signed expiry round-trip"

	if _, err := sbx.Filesystem.WriteString(ctx, path, content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	url, err := sbx.DownloadURL(path,
		WithSignedURLUser("user"), WithSignedURLExpiration(120*time.Second))
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}
	t.Logf("expiring download URL: %s", url)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET signed url: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status %d, body: %s", resp.StatusCode, string(body))
	}
	if string(body) != content {
		t.Fatalf("downloaded body = %q, want %q", string(body), content)
	}
}
