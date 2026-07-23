// Command volumes demonstrates creating a persistent volume, writing and
// reading content through the volume content API, and mounting the volume into
// a sandbox so its files are visible on the sandbox filesystem.
//
// Unlike a sandbox filesystem, a volume outlives any single sandbox: data
// written to it persists and can be mounted into other sandboxes later.
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

	// Create a volume. The returned handle carries the volume's auth token, so
	// it is immediately usable for content operations.
	vol, err := client.CreateVolume(ctx, "example-volume")
	if err != nil {
		log.Fatalf("Failed to create volume: %v", err)
	}
	fmt.Printf("Created volume: %s (%s)\n", vol.Name, vol.VolumeID)
	defer func() {
		if _, err := client.DestroyVolume(ctx, vol.VolumeID); err != nil {
			log.Printf("Failed to destroy volume: %v", err)
		}
	}()

	// Write a file directly to the volume via the content API.
	if _, err := vol.WriteFileString(ctx, "/config.txt", "persisted in a volume\n"); err != nil {
		log.Fatalf("Failed to write to volume: %v", err)
	}

	// Read it back.
	got, err := vol.ReadFileString(ctx, "/config.txt")
	if err != nil {
		log.Fatalf("Failed to read from volume: %v", err)
	}
	fmt.Printf("Volume content: %q\n", got)

	// List the volume root.
	entries, err := vol.List(ctx, "/")
	if err != nil {
		log.Fatalf("Failed to list volume: %v", err)
	}
	fmt.Println("Volume entries:")
	for _, e := range entries {
		fmt.Printf("  %s (%d bytes)\n", e.Name, e.Size)
	}

	// Mount the volume into a new sandbox. Files written to the volume appear
	// on the sandbox filesystem at the mount path.
	sandbox, err := client.NewSandbox(ctx, e2b.SandboxConfig{
		Template:     "base",
		VolumeMounts: []e2b.VolumeMount{vol.AsMount("/mnt/data")},
	})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer sandbox.Close()

	result, err := sandbox.Commands.Run(ctx, "cat /mnt/data/config.txt")
	if err != nil {
		log.Fatalf("Failed to read mounted file: %v", err)
	}
	fmt.Printf("Read via sandbox mount: %q\n", result.Stdout)
}
