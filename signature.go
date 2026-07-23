package e2b

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// timeNow is overridable in tests so signature expiration timestamps are
// deterministic. Production code always uses time.Now.
var timeNow = time.Now

// Signature operations. These are baked into the raw signature string and must
// match the values envd recomputes server-side.
const (
	sigOpRead  = "read"
	sigOpWrite = "write"
)

// getSignature computes the v1 URL signature for a sandbox file operation. It
// matches the E2B JS and Python SDKs byte-for-byte: the signature is
// "v1_" + base64(sha256("<path>:<operation>:<user>:<token>[:<expiration>]"))
// with the trailing base64 padding removed.
//
// expirationInSeconds <= 0 means no expiration; in that case the returned
// expiration is 0 and hasExpiration is false, and the ":<expiration>" suffix is
// omitted from the signed string.
func getSignature(path, operation, user, envdAccessToken string, expirationInSeconds int) (sig string, expiration int64, hasExpiration bool, err error) {
	if envdAccessToken == "" {
		return "", 0, false, &InvalidArgumentError{
			Message: "access token is not set and signature cannot be generated",
		}
	}

	if expirationInSeconds > 0 {
		expiration = timeNow().Unix() + int64(expirationInSeconds)
		hasExpiration = true
	}

	var raw string
	if hasExpiration {
		raw = fmt.Sprintf("%s:%s:%s:%s:%d", path, operation, user, envdAccessToken, expiration)
	} else {
		raw = fmt.Sprintf("%s:%s:%s:%s", path, operation, user, envdAccessToken)
	}

	sum := sha256.Sum256([]byte(raw))
	// RawStdEncoding uses the standard +/ alphabet with no padding, matching
	// JS btoa / Python b64encode followed by stripping "=".
	sig = "v1_" + base64.RawStdEncoding.EncodeToString(sum[:])
	return sig, expiration, hasExpiration, nil
}

// SignedURLOption configures a DownloadURL or UploadURL call.
type SignedURLOption interface {
	applySignedURL(*signedURLConfig)
}

type signedURLConfig struct {
	user                string
	expirationInSeconds int
	hasExpiration       bool
}

type signedURLUserOption string

func (o signedURLUserOption) applySignedURL(c *signedURLConfig) { c.user = string(o) }

// WithSignedURLUser sets the sandbox user for the signed URL. When unset the
// username is left empty, letting modern envd apply its default user. Callers
// targeting older templates that require an explicit user should pass
// WithSignedURLUser("user").
func WithSignedURLUser(user string) SignedURLOption { return signedURLUserOption(user) }

type signedURLExpirationOption struct{ d time.Duration }

func (o signedURLExpirationOption) applySignedURL(c *signedURLConfig) {
	c.expirationInSeconds = int(o.d.Seconds())
	c.hasExpiration = true
}

// WithSignedURLExpiration sets how long the signed URL remains valid. It maps to
// the wire "signature_expiration" query parameter as a whole number of seconds
// (fractional seconds are truncated). It is only valid on secured sandboxes;
// using it on an unsecured sandbox returns an *InvalidArgumentError.
func WithSignedURLExpiration(d time.Duration) SignedURLOption {
	return signedURLExpirationOption{d: d}
}

// DownloadURL returns a URL for downloading the file at path from the sandbox
// via an HTTP GET request. When the sandbox was created secured (Secure: true),
// the URL is signed so it can be fetched without an access-token header; the
// signature is the only credential required. On an unsecured sandbox the plain
// /files URL is returned.
//
// Passing WithSignedURLExpiration on an unsecured sandbox returns an
// *InvalidArgumentError.
func (s *Sandbox) DownloadURL(path string, opts ...SignedURLOption) (string, error) {
	return s.buildFileURL(path, sigOpRead, opts)
}

// UploadURL returns a URL for uploading a file to path in the sandbox. Send an
// HTTP POST request to the URL with the file as multipart/form-data under a form
// field named "file". path may be empty to upload to the default location. When
// the sandbox was created secured (Secure: true), the URL is signed; otherwise
// the plain /files URL is returned.
//
// Passing WithSignedURLExpiration on an unsecured sandbox returns an
// *InvalidArgumentError.
func (s *Sandbox) UploadURL(path string, opts ...SignedURLOption) (string, error) {
	return s.buildFileURL(path, sigOpWrite, opts)
}

// buildFileURL is the shared implementation behind DownloadURL and UploadURL.
func (s *Sandbox) buildFileURL(path, operation string, opts []SignedURLOption) (string, error) {
	cfg := signedURLConfig{}
	for _, o := range opts {
		o.applySignedURL(&cfg)
	}

	useSignature := s.accessToken != ""
	if !useSignature && cfg.hasExpiration {
		return "", &InvalidArgumentError{
			Message: "signature expiration can be used only when sandbox is created as secured",
		}
	}

	base, err := s.filesURL(path, cfg.user)
	if err != nil {
		return "", err
	}
	if !useSignature {
		return base, nil
	}

	sig, expiration, hasExpiration, err := getSignature(path, operation, cfg.user, s.accessToken, cfg.expirationInSeconds)
	if err != nil {
		return "", err
	}

	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("e2b: parse files URL: %w", err)
	}
	q := u.Query()
	q.Set("signature", sig)
	if hasExpiration {
		q.Set("signature_expiration", strconv.FormatInt(expiration, 10))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
