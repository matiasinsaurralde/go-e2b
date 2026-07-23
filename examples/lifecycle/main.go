// Command lifecycle demonstrates pausing and resuming a sandbox, extending its
// timeout, and taking a snapshot. A paused sandbox keeps its filesystem and
// memory state, so work resumes exactly where it left off.
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

	sandbox, err := client.NewSandbox(ctx, e2b.SandboxConfig{Template: "base"})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer sandbox.Close()

	fmt.Printf("Sandbox created: %s\n", sandbox.ID)

	// Write some state we expect to survive a pause/resume cycle.
	if _, err := sandbox.Filesystem.WriteString(ctx, "/home/user/state.txt", "before pause\n"); err != nil {
		log.Fatalf("Failed to write state: %v", err)
	}

	// Extend the sandbox lifetime to 10 minutes.
	if err := sandbox.SetTimeout(600); err != nil {
		log.Fatalf("Failed to set timeout: %v", err)
	}
	fmt.Println("Timeout extended to 600s")

	// Pause the sandbox. It stops running but retains its state.
	if err := sandbox.Pause(); err != nil {
		log.Fatalf("Failed to pause: %v", err)
	}
	fmt.Println("Sandbox paused")

	running, err := sandbox.IsRunning()
	if err != nil {
		log.Fatalf("Failed to check running state: %v", err)
	}
	fmt.Printf("Running after pause: %v\n", running)

	// Resume with a fresh 5-minute lifetime.
	if err := sandbox.Resume(300); err != nil {
		log.Fatalf("Failed to resume: %v", err)
	}
	fmt.Println("Sandbox resumed")

	// The state written before the pause is still present.
	got, err := sandbox.Filesystem.ReadString(ctx, "/home/user/state.txt")
	if err != nil {
		log.Fatalf("Failed to read state after resume: %v", err)
	}
	fmt.Printf("State after resume: %q\n", got)

	// Take a named snapshot of the current state.
	snap, err := sandbox.CreateSnapshot(ctx, "example-checkpoint")
	if err != nil {
		log.Fatalf("Failed to create snapshot: %v", err)
	}
	fmt.Printf("Snapshot created: %s (names: %v)\n", snap.SnapshotID, snap.Names)
}
