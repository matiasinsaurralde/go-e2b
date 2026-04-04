package main

import (
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
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer sandbox.Close()

	fmt.Printf("Sandbox created: %s\n", sandbox.ID)

	// Per-command environment variables using WithEnv.
	result, err := sandbox.Commands.Run("bash", []string{"-c", "echo FOO=$FOO BAR=$BAR"},
		e2b.WithEnv(map[string]string{"FOO": "hello", "BAR": "world"}),
	)
	if err != nil {
		log.Fatalf("Failed to run command: %v", err)
	}

	fmt.Printf("Exit code: %d\n", result.ExitCode)
	fmt.Printf("Stdout: %s", result.Stdout)
}
