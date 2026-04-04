package e2b

import (
	"os"
	"testing"
)

func TestResolveAPIKey(t *testing.T) {
	t.Run("explicit value", func(t *testing.T) {
		got := resolveAPIKey("my-key")
		if got != "my-key" {
			t.Errorf("resolveAPIKey = %q, want %q", got, "my-key")
		}
	})

	t.Run("from env", func(t *testing.T) {
		t.Setenv(apiKeyEnv, "env-key")
		got := resolveAPIKey("")
		if got != "env-key" {
			t.Errorf("resolveAPIKey = %q, want %q", got, "env-key")
		}
	})

	t.Run("empty", func(t *testing.T) {
		// Ensure the env var is unset for this subtest.
		if _, ok := os.LookupEnv(apiKeyEnv); ok {
			t.Setenv(apiKeyEnv, "")
		}
		got := resolveAPIKey("")
		if got != "" {
			t.Errorf("resolveAPIKey = %q, want %q", got, "")
		}
	})
}

func TestResolveAPIBaseURL(t *testing.T) {
	t.Run("explicit value", func(t *testing.T) {
		got := resolveAPIBaseURL("https://custom.api")
		if got != "https://custom.api" {
			t.Errorf("got %q, want %q", got, "https://custom.api")
		}
	})

	t.Run("from env", func(t *testing.T) {
		t.Setenv(apiURLEnv, "https://env.api")
		got := resolveAPIBaseURL("")
		if got != "https://env.api" {
			t.Errorf("got %q, want %q", got, "https://env.api")
		}
	})

	t.Run("default", func(t *testing.T) {
		got := resolveAPIBaseURL("")
		if got != DefaultAPIBaseURL {
			t.Errorf("got %q, want %q", got, DefaultAPIBaseURL)
		}
	})
}

func TestResolveSandboxDomain(t *testing.T) {
	t.Run("explicit value", func(t *testing.T) {
		got := resolveSandboxDomain("custom.domain")
		if got != "custom.domain" {
			t.Errorf("got %q, want %q", got, "custom.domain")
		}
	})

	t.Run("from env", func(t *testing.T) {
		t.Setenv(sandboxURLEnv, "env.domain")
		got := resolveSandboxDomain("")
		if got != "env.domain" {
			t.Errorf("got %q, want %q", got, "env.domain")
		}
	})

	t.Run("default", func(t *testing.T) {
		got := resolveSandboxDomain("")
		if got != defaultSandboxDomain {
			t.Errorf("got %q, want %q", got, defaultSandboxDomain)
		}
	})
}
