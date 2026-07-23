package e2b

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Golden vectors generated from the reference algorithm (identical in the JS and
// Python SDKs):
//
//	raw = "<path>:<op>:<user>:<token>[:<exp>]"
//	sig = "v1_" + base64_std(sha256(raw)).rstrip("=")
//
// python3 -c "import base64,hashlib; ..."
func TestGetSignatureGoldenVectors(t *testing.T) {
	const token = "tok_abc123"

	cases := []struct {
		name      string
		path      string
		operation string
		user      string
		expSecs   int
		fixedNow  int64 // unix seconds; the vector's expiration is fixedNow+expSecs
		want      string
		wantExp   int64
		wantHas   bool
	}{
		{
			name:      "no expiration read",
			path:      "/home/user/demo.txt",
			operation: sigOpRead,
			user:      "user",
			want:      "v1_/EdO6Ot9ZWcyYu3WUTGTrppX9hNCRDRDQRV+mcXKE/Q",
		},
		{
			name:      "no expiration write empty user",
			path:      "",
			operation: sigOpWrite,
			user:      "",
			want:      "v1_kCOdUesHaIzKFeIPgyTWkmPUAYqrkDoHDubiqKLs4Oc",
		},
		{
			name:      "with expiration read",
			path:      "/home/user/demo.txt",
			operation: sigOpRead,
			user:      "user",
			expSecs:   1000,
			fixedNow:  1699999000, // + 1000 == 1700000000, the vector's timestamp
			want:      "v1_b4YFDyWpDmMdMFM50GNNgbFmrsmY/FoGjL08e9PTZYo",
			wantExp:   1700000000,
			wantHas:   true,
		},
		{
			name:      "unicode raw path",
			path:      "/tmp/файл .txt",
			operation: sigOpRead,
			user:      "user",
			want:      "v1_kRgF007fZjt9fDFB9M87baGh3mNyUdULrFBVaByoFpA",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.fixedNow != 0 {
				restore := timeNow
				timeNow = func() time.Time { return time.Unix(tc.fixedNow, 0) }
				defer func() { timeNow = restore }()
			}

			sig, exp, has, err := getSignature(tc.path, tc.operation, tc.user, token, tc.expSecs)
			if err != nil {
				t.Fatalf("getSignature: %v", err)
			}
			if sig != tc.want {
				t.Errorf("signature = %q, want %q", sig, tc.want)
			}
			if has != tc.wantHas {
				t.Errorf("hasExpiration = %v, want %v", has, tc.wantHas)
			}
			if exp != tc.wantExp {
				t.Errorf("expiration = %d, want %d", exp, tc.wantExp)
			}
		})
	}
}

// The signature must be computed over the RAW (unescaped) path, while the URL
// query carries the ESCAPED path. This asserts we sign the raw value.
func TestGetSignatureUsesRawPath(t *testing.T) {
	const token = "tok_abc123"
	// A path with a space: raw signature uses the literal space.
	sig, _, _, err := getSignature("/tmp/файл .txt", sigOpRead, "user", token, 0)
	if err != nil {
		t.Fatalf("getSignature: %v", err)
	}
	if sig != "v1_kRgF007fZjt9fDFB9M87baGh3mNyUdULrFBVaByoFpA" {
		t.Fatalf("raw-path signature mismatch: %q", sig)
	}
}

func TestGetSignatureNoToken(t *testing.T) {
	_, _, _, err := getSignature("/x", sigOpRead, "user", "", 0)
	var iae *InvalidArgumentError
	if !errors.As(err, &iae) {
		t.Fatalf("want *InvalidArgumentError, got %v", err)
	}
}

// fakeSignedURLSandbox builds a *Sandbox wired just enough for URL construction
// (ID, domain, accessToken) without any network dependency.
func fakeSignedURLSandbox(t *testing.T, accessToken string) *Sandbox {
	t.Helper()
	c, err := NewClient(ClientConfig{APIKey: "e2b_test", SandboxDomain: "e2b.app"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return &Sandbox{ID: "sbx123", domain: "e2b.app", accessToken: accessToken, client: c}
}

func TestDownloadURLUnsecuredPlain(t *testing.T) {
	sbx := fakeSignedURLSandbox(t, "")
	got, err := sbx.DownloadURL("/tmp/demo.txt")
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}
	if !strings.HasPrefix(got, "https://49983-sbx123.e2b.app/files") {
		t.Errorf("unexpected host: %q", got)
	}
	u, _ := url.Parse(got)
	if u.Query().Get("path") != "/tmp/demo.txt" {
		t.Errorf("path = %q", u.Query().Get("path"))
	}
	if u.Query().Has("signature") {
		t.Errorf("unsecured URL must not be signed: %q", got)
	}
}

func TestDownloadURLUnsecuredWithExpirationErrors(t *testing.T) {
	sbx := fakeSignedURLSandbox(t, "")
	_, err := sbx.DownloadURL("/tmp/demo.txt", WithSignedURLExpiration(60*time.Second))
	var iae *InvalidArgumentError
	if !errors.As(err, &iae) {
		t.Fatalf("want *InvalidArgumentError, got %v", err)
	}
}

func TestDownloadURLSecuredSigned(t *testing.T) {
	sbx := fakeSignedURLSandbox(t, "tok_abc123")
	got, err := sbx.DownloadURL("/home/user/demo.txt", WithSignedURLUser("user"))
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}
	u, _ := url.Parse(got)
	q := u.Query()
	if q.Get("signature") != "v1_/EdO6Ot9ZWcyYu3WUTGTrppX9hNCRDRDQRV+mcXKE/Q" {
		t.Errorf("signature = %q", q.Get("signature"))
	}
	if q.Get("username") != "user" {
		t.Errorf("username = %q", q.Get("username"))
	}
	if q.Has("signature_expiration") {
		t.Errorf("no expiration expected: %q", got)
	}
}

func TestDownloadURLSecuredWithExpiration(t *testing.T) {
	restore := timeNow
	timeNow = func() time.Time { return time.Unix(1699999000, 0) }
	defer func() { timeNow = restore }()

	sbx := fakeSignedURLSandbox(t, "tok_abc123")
	got, err := sbx.DownloadURL("/home/user/demo.txt",
		WithSignedURLUser("user"), WithSignedURLExpiration(1000*time.Second))
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}
	u, _ := url.Parse(got)
	q := u.Query()
	if q.Get("signature") != "v1_b4YFDyWpDmMdMFM50GNNgbFmrsmY/FoGjL08e9PTZYo" {
		t.Errorf("signature = %q", q.Get("signature"))
	}
	if q.Get("signature_expiration") != "1700000000" {
		t.Errorf("signature_expiration = %q", q.Get("signature_expiration"))
	}
}

func TestUploadURLEmptyPathOmitsPath(t *testing.T) {
	sbx := fakeSignedURLSandbox(t, "tok_abc123")
	got, err := sbx.UploadURL("")
	if err != nil {
		t.Fatalf("UploadURL: %v", err)
	}
	u, _ := url.Parse(got)
	q := u.Query()
	if q.Has("path") {
		t.Errorf("empty path must be omitted: %q", got)
	}
	// signature computed over empty path + empty user + write op
	if q.Get("signature") != "v1_kCOdUesHaIzKFeIPgyTWkmPUAYqrkDoHDubiqKLs4Oc" {
		t.Errorf("signature = %q", q.Get("signature"))
	}
}

func TestUploadURLOperationIsWrite(t *testing.T) {
	// Same inputs, read vs write, must differ.
	sbx := fakeSignedURLSandbox(t, "tok_abc123")
	dl, _ := sbx.DownloadURL("/x", WithSignedURLUser("user"))
	ul, _ := sbx.UploadURL("/x", WithSignedURLUser("user"))
	du, _ := url.Parse(dl)
	uu, _ := url.Parse(ul)
	if du.Query().Get("signature") == uu.Query().Get("signature") {
		t.Errorf("read and write signatures must differ")
	}
}
