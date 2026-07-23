// Command network demonstrates outbound network policy for a sandbox: creating
// a sandbox that can only reach an allowlisted host, and tightening the policy
// on a running sandbox with UpdateNetwork.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	e2b "github.com/matiasinsaurralde/go-e2b"
)

func main() {
	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		log.Fatal("E2B_API_KEY is not set")
	}

	client, err := e2b.NewClient(e2b.ClientConfig{APIKey: apiKey})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	ctx := context.Background()

	// Create a sandbox that denies all outbound traffic except example.com.
	// AllowOutbound sets DenyOut to 0.0.0.0/0 and allows only the given hosts.
	sandbox, err := client.NewSandbox(ctx, e2b.SandboxConfig{
		Template: "base",
		Network:  e2b.AllowOutbound("example.com"),
	})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer sandbox.Close()

	fmt.Printf("Sandbox created with allowlist: %s\n", sandbox.ID)

	// Allowed host succeeds.
	if r, err := sandbox.Commands.Run(ctx, "curl -sS -o /dev/null -w '%{http_code}' https://example.com"); err != nil {
		fmt.Printf("example.com request failed: %v\n", err)
	} else {
		fmt.Printf("example.com -> HTTP %s\n", r.Stdout)
	}

	// A host not on the allowlist is blocked. Run returns a *CommandExitError
	// for the non-zero curl exit; the result still carries stderr.
	if r, _ := sandbox.Commands.Run(ctx, "curl -sS --max-time 10 https://api.github.com"); r != nil {
		fmt.Printf("api.github.com (blocked) exit=%d stderr=%q\n", r.ExitCode, r.Stderr)
	}

	// Tighten the policy on the running sandbox: deny all outbound traffic.
	// UpdateNetwork replaces the entire mutable config, it does not merge.
	if err := sandbox.UpdateNetwork(e2b.NetworkUpdateConfig{
		DenyOut: []string{e2b.AllTraffic},
	}); err != nil {
		log.Fatalf("Failed to update network: %v", err)
	}
	fmt.Println("Network policy updated: all outbound traffic denied")
}
