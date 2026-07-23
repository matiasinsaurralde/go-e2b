// Command fork demonstrates forking one sandbox into several identical copies
// and fanning a command out across the forks.
//
// A fork boots from a snapshot of the source sandbox, so every fork starts with
// the same filesystem and state. This is useful for running the same setup once
// and then branching into parallel, independent workers.
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

	// Create the source sandbox and seed some state that every fork inherits.
	source, err := client.NewSandbox(ctx, e2b.SandboxConfig{Template: "base"})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer source.Close()

	fmt.Printf("Source sandbox: %s\n", source.ID)

	if _, err := source.Filesystem.WriteString(ctx, "/home/user/seed.txt", "shared state\n"); err != nil {
		log.Fatalf("Failed to seed source: %v", err)
	}

	// Fork the source into 3 independent copies.
	forks, err := source.Fork(ctx, e2b.WithForkCount(3))
	if err != nil {
		log.Fatalf("Fork failed: %v", err)
	}

	// Each ForkResult is either a running Sandbox or an Err — check both.
	// A fork that started must still be closed to release its resources.
	for i, f := range forks {
		if f.Err != nil {
			fmt.Printf("fork %d: failed to start: %v\n", i, f.Err)
			continue
		}
		fork := f.Sandbox
		defer fork.Close()

		// The forked filesystem carries the seed written before forking.
		seed, err := fork.Filesystem.ReadString(ctx, "/home/user/seed.txt")
		if err != nil {
			fmt.Printf("fork %d (%s): read seed failed: %v\n", i, fork.ID, err)
			continue
		}

		// Run work unique to this fork; forks do not share state after branching.
		result, err := fork.Commands.Run(ctx, fmt.Sprintf("echo worker %d reporting", i))
		if err != nil {
			fmt.Printf("fork %d (%s): command failed: %v\n", i, fork.ID, err)
			continue
		}

		fmt.Printf("fork %d (%s): seed=%q -> %s", i, fork.ID, seed, result.Stdout)
	}
}
