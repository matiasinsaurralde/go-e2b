// Package e2b provides a Go client for the E2B cloud sandbox API.
//
// E2B sandboxes are lightweight microVMs that can run commands,
// access a filesystem, and be configured with environment variables.
//
// Basic usage:
//
//	sandbox, err := e2b.NewSandbox(e2b.SandboxConfig{
//	    APIKey:   os.Getenv("E2B_API_KEY"),
//	    Template: "base",
//	    EnvVars:  map[string]string{"VALUE": "hello"},
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer sandbox.Close()
//
//	result, err := sandbox.Commands.Run("echo", []string{"hello"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result.Stdout)
package e2b

import (
	"os"
	"time"
)

const (
	// DefaultAPIBaseURL is the default E2B API endpoint.
	DefaultAPIBaseURL = "https://api.e2b.app"

	// DefaultTemplate is the default sandbox template.
	DefaultTemplate = "base"

	// DefaultTimeout is the default sandbox lifetime in seconds.
	DefaultTimeout = 300

	// DefaultCommandTimeout is the default command execution timeout.
	DefaultCommandTimeout = 60 * time.Second

	// envdPort is the port used by the environment daemon.
	envdPort = 49983

	// defaultSandboxDomain is the default domain for sandbox connections.
	defaultSandboxDomain = "e2b.app"

	// apiKeyEnv is the environment variable name for the API key.
	apiKeyEnv = "E2B_API_KEY" // #nosec G101 -- env var name, not a credential

	// apiURLEnv is the environment variable name for a custom API URL.
	apiURLEnv = "E2B_API_URL"

	// sandboxURLEnv is the environment variable name for a custom sandbox URL.
	sandboxURLEnv = "E2B_SANDBOX_URL"
)

// resolveAPIKey returns the API key from the config or the environment.
func resolveAPIKey(key string) string {
	if key != "" {
		return key
	}
	return os.Getenv(apiKeyEnv)
}

// resolveAPIBaseURL returns the API base URL from the config or the environment.
func resolveAPIBaseURL(url string) string {
	if url != "" {
		return url
	}
	if env := os.Getenv(apiURLEnv); env != "" {
		return env
	}
	return DefaultAPIBaseURL
}

// resolveSandboxDomain returns the sandbox domain from the config or the environment.
func resolveSandboxDomain(domain string) string {
	if domain != "" {
		return domain
	}
	if env := os.Getenv(sandboxURLEnv); env != "" {
		return env
	}
	return defaultSandboxDomain
}
