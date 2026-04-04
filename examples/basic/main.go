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
		APIKey:   apiKey,
		Template: "base",
	})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer sandbox.Close()

	fmt.Printf("Sandbox created: %s\n", sandbox.ID)

	result, err := sandbox.Commands.Run("python3", []string{"-c", "print(1+1)"})
	if err != nil {
		log.Fatalf("Failed to run command: %v", err)
	}

	fmt.Printf("Exit code: %d\n", result.ExitCode)
	fmt.Printf("Stdout: %s", result.Stdout)
	if result.Stderr != "" {
		fmt.Printf("Stderr: %s", result.Stderr)
	}
}
