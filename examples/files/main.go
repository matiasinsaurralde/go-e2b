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

	sandbox, err := e2b.NewSandbox(e2b.SandboxConfig{
		APIKey:   apiKey,
		Template: "base",
	})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer sandbox.Close()

	fmt.Printf("Sandbox created: %s\n", sandbox.ID)

	ctx := context.Background()

	// Write a text file.
	const path = "/home/user/hello.txt"
	const content = "Hello from go-e2b!\n"

	info, err := sandbox.Filesystem.WriteString(ctx, path, content)
	if err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}
	fmt.Printf("Wrote file: %s (%s)\n", info.Name, info.Path)

	// Read it back as a string.
	got, err := sandbox.Filesystem.ReadString(ctx, path)
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}
	fmt.Printf("Read file content: %q\n", got)

	// Verify round-trip.
	if got != content {
		log.Fatalf("Content mismatch: got %q, want %q", got, content)
	}
	fmt.Println("Content matches — round-trip OK")

	// Write binary data.
	const binPath = "/tmp/data.bin"
	binData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}

	binInfo, err := sandbox.Filesystem.WriteBytes(ctx, binPath, binData)
	if err != nil {
		log.Fatalf("Failed to write binary file: %v", err)
	}
	fmt.Printf("Wrote binary file: %s (%d bytes)\n", binInfo.Name, len(binData))

	readBack, err := sandbox.Filesystem.ReadBytes(ctx, binPath)
	if err != nil {
		log.Fatalf("Failed to read binary file: %v", err)
	}
	fmt.Printf("Read binary file: %d bytes\n", len(readBack))

	// Confirm the sandbox can see the file via a command.
	result, err := sandbox.Commands.Run("cat", []string{path})
	if err != nil {
		log.Fatalf("Failed to run cat: %v", err)
	}
	fmt.Printf("cat output: %q\n", result.Stdout)
}
