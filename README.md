# go-e2b

A Go SDK for the [E2B](https://e2b.dev) cloud sandbox API. E2B provides lightweight microVMs you can use to safely run arbitrary code in ephemeral environments.

## Installation

```sh
go get github.com/matiasinsaurralde/go-e2b
```

## Requirements

- Go 1.21+
- An [E2B API key](https://e2b.dev/dashboard)

## Quick Start

```go
package main

import (
    "fmt"
    "log"
    "os"

    e2b "github.com/matiasinsaurralde/go-e2b"
)

func main() {
    sandbox, err := e2b.NewSandbox(e2b.SandboxConfig{
        APIKey: os.Getenv("E2B_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }
    defer sandbox.Close()

    result, err := sandbox.Commands.Run("echo", []string{"hello, world"})
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Stdout)   // hello, world
    fmt.Println(result.ExitCode) // 0
}
```

## Usage

### Creating a Sandbox

```go
sandbox, err := e2b.NewSandbox(e2b.SandboxConfig{
    APIKey:   "your-api-key",       // or set E2B_API_KEY env var
    Template: "base",               // sandbox template (default: "base")
    Timeout:  300,                  // lifetime in seconds (default: 300)
    EnvVars:  map[string]string{    // environment variables
        "MY_VAR": "value",
    },
})
```

The `base` template includes Python 3.11, Node.js 20, npm, Yarn, git, and the GitHub CLI.

### Running Commands

```go
// Simple command
result, err := sandbox.Commands.Run("python3", []string{"-c", "print('hello')"})

// With options
result, err := sandbox.Commands.Run(
    "bash", []string{"-c", "echo $FOO"},
    e2b.WithEnv(map[string]string{"FOO": "bar"}),
    e2b.WithCwd("/tmp"),
    e2b.WithUser("ubuntu"),
    e2b.WithTimeout(30 * time.Second),
)

fmt.Println(result.Stdout)
fmt.Println(result.Stderr)
fmt.Println(result.ExitCode)
```

### Context Support

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

sandbox, err := e2b.NewSandboxWithContext(ctx, e2b.SandboxConfig{
    APIKey: apiKey,
})

result, err := sandbox.Commands.RunWithContext(ctx, "sleep", []string{"5"})
```

### Command Options

| Option | Description |
|--------|-------------|
| `WithEnv(map[string]string)` | Set environment variables for the command |
| `WithCwd(string)` | Set the working directory |
| `WithUser(string)` | Set the user to run the command as |
| `WithTimeout(time.Duration)` | Set a per-command execution timeout |

## Configuration

Configuration can be provided via `SandboxConfig` fields or environment variables:

| Field | Env Var | Default | Description |
|-------|---------|---------|-------------|
| `APIKey` | `E2B_API_KEY` | — | E2B API key (required) |
| `APIBaseURL` | `E2B_API_URL` | `https://api.e2b.app` | API base URL |
| `SandboxDomain` | `E2B_SANDBOX_URL` | `e2b.app` | Sandbox domain |
| `Template` | — | `base` | Sandbox template ID |
| `Timeout` | — | `300` | Sandbox lifetime in seconds |

## Error Handling

```go
import e2b "github.com/matiasinsaurralde/go-e2b"

_, err := e2b.NewSandbox(cfg)
switch {
case errors.As(err, &e2b.SandboxNotFoundError{}):
    // sandbox not found
case errors.As(err, &e2b.TimeoutError{}):
    // operation timed out
case errors.As(err, &e2b.Error{}):
    // generic E2B error
}
```

## Development

### Regenerating Proto Bindings

The Connect RPC client is generated from [e2b-dev/infra](https://github.com/e2b-dev/infra) proto definitions using [buf](https://buf.build).

```sh
# Install tools
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest

# Regenerate
make generate

# Sync proto from upstream
make proto-sync

# Upgrade to latest upstream commit
make proto-upgrade
```

See [DEVELOPMENT.md](DEVELOPMENT.md) for details.

## License

[MIT](LICENSE)
