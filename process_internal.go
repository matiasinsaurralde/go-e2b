package e2b

import (
	"encoding/base64"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	processpb "github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process"
	"github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process/processconnect"
)

const (
	// defaultProcessUser is the sandbox user commands run as when no user is
	// specified. This mirrors the reference SDKs' default of "user".
	defaultProcessUser = "user"

	// keepAlivePingHeader asks envd to send periodic keepalive frames on long
	// server streams so idle connections are not dropped by intermediaries.
	keepAlivePingHeader = "Keepalive-Ping-Interval"

	// keepAlivePingIntervalSecStr is the keepalive interval (in seconds)
	// requested via keepAlivePingHeader, as an HTTP header value.
	keepAlivePingIntervalSecStr = "50"
)

// processClient returns a Connect client for the sandbox's process service.
func (s *Sandbox) processClient() processconnect.ProcessClient {
	return processconnect.NewProcessClient(s.client.httpClient, s.envdBaseURL())
}

// setProcessAuthHeaders sets the headers required to authenticate an envd
// process RPC: the sandbox access token and HTTP Basic authentication carrying
// the target user. The reference SDKs authenticate the user as
// `Authorization: Basic base64("<user>:")` (empty password); an empty user
// falls back to defaultProcessUser.
func setProcessAuthHeaders(h interface{ Set(string, string) }, accessToken, user string) {
	h.Set("X-Access-Token", accessToken)

	if user == "" {
		user = defaultProcessUser
	}
	cred := base64.StdEncoding.EncodeToString([]byte(user + ":"))
	h.Set("Authorization", "Basic "+cred)
}

// pidSelector builds a ProcessSelector that targets a process by PID.
func pidSelector(pid uint32) *processpb.ProcessSelector {
	return &processpb.ProcessSelector{
		Selector: &processpb.ProcessSelector_Pid{Pid: pid},
	}
}

// mapProcessRPCError converts a Connect/gRPC error from an envd process RPC into
// a typed SDK error. Errors that are already typed pass through unchanged.
func mapProcessRPCError(err error) error {
	if err == nil {
		return nil
	}

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return err
	}

	msg := connectErr.Message()
	switch connectErr.Code() {
	case connect.CodeNotFound:
		return &SandboxNotFoundError{SandboxID: ""}
	case connect.CodeInvalidArgument:
		return &InvalidArgumentError{Message: msg}
	case connect.CodeUnauthenticated, connect.CodePermissionDenied:
		return &AuthenticationError{Message: msg}
	case connect.CodeDeadlineExceeded:
		return &TimeoutError{Message: msg}
	default:
		return &Error{Message: fmt.Sprintf("process rpc: %s", msg)}
	}
}

// isNotFound reports whether err is a Connect NotFound error. Kill operations
// treat a missing process as a non-error "false" result.
func isNotFound(err error) bool {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code() == connect.CodeNotFound
	}
	return false
}
