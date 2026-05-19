package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/matiasinsaurralde/go-e2b"
)

func main() {
	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		log.Fatal("E2B_API_KEY environment variable is not set")
	}

	client, err := e2b.NewClient(e2b.ClientConfig{APIKey: apiKey})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Create a sandbox
	ctx := context.Background()
	sandbox, err := client.NewSandbox(ctx, e2b.SandboxConfig{
		Template: "base",
		Timeout:  600,
		EnvVars:  map[string]string{"DEMO": "true"},
	})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer sandbox.Close()

	fmt.Printf("✓ Sandbox created: %s\n\n", sandbox.ID)

	// Get sandbox information
	info, err := sandbox.Info()
	if err != nil {
		log.Fatalf("Failed to get sandbox info: %v", err)
	}

	fmt.Println("Sandbox Information:")
	fmt.Println("====================")
	fmt.Printf("ID:           %s\n", info.ID)
	fmt.Printf("Template:     %s\n", info.Template)
	fmt.Printf("State:        %s\n", info.State)
	fmt.Printf("CPU Cores:    %d\n", info.CPUCount)
	fmt.Printf("Memory:       %d MB\n", info.MemoryMB)
	fmt.Printf("Disk:         %d MB\n", info.DiskSizeMB)
	fmt.Printf("Started:      %s\n", info.StartedAt)
	fmt.Printf("Envd Version: %s\n", info.EnvdVersion)

	if info.Lifecycle.OnTimeout != "" {
		fmt.Printf("On Timeout:   %s\n", info.Lifecycle.OnTimeout)
	}

	if len(info.VolumeMounts) > 0 {
		fmt.Println("\nVolume Mounts:")
		for _, vm := range info.VolumeMounts {
			fmt.Printf("  %s -> %s\n", vm.Name, vm.Path)
		}
	}

	// You can also use InfoWithContext for custom context control
	ctx2, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info2, err := sandbox.InfoWithContext(ctx2)
	if err != nil {
		log.Fatalf("Failed to get sandbox info with context: %v", err)
	}
	fmt.Printf("\n✓ Verified sandbox state: %s\n", info2.State)
}
