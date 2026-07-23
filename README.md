# go-e2b

[![CI](https://github.com/matiasinsaurralde/go-e2b/actions/workflows/ci.yml/badge.svg)](https://github.com/matiasinsaurralde/go-e2b/actions/workflows/ci.yml)
[![Lint](https://github.com/matiasinsaurralde/go-e2b/actions/workflows/lint.yml/badge.svg)](https://github.com/matiasinsaurralde/go-e2b/actions/workflows/lint.yml)
[![Security](https://github.com/matiasinsaurralde/go-e2b/actions/workflows/security.yml/badge.svg)](https://github.com/matiasinsaurralde/go-e2b/actions/workflows/security.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/matiasinsaurralde/go-e2b.svg)](https://pkg.go.dev/github.com/matiasinsaurralde/go-e2b)
[![License: MIT](https://img.shields.io/github/license/matiasinsaurralde/go-e2b)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/matiasinsaurralde/go-e2b)](go.mod)

A Go SDK for the [E2B](https://e2b.dev) cloud sandbox API. E2B provides lightweight microVMs you can use to safely run arbitrary code in ephemeral environments.

## Installation

```sh
go get github.com/matiasinsaurralde/go-e2b
```

## Requirements

- Go 1.25+
- An [E2B API key](https://e2b.dev/dashboard)

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    e2b "github.com/matiasinsaurralde/go-e2b"
)

func main() {
    client, err := e2b.NewClient(e2b.ClientConfig{
        APIKey: os.Getenv("E2B_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }

    sandbox, err := client.NewSandbox(context.Background(), e2b.SandboxConfig{
        Template: "base",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer sandbox.Close()

    result, err := sandbox.Commands.Run(context.Background(), "echo hello, world")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Stdout)     // hello, world
    fmt.Println(result.ExitCode)   // 0
}
```

See [examples/](examples/) for runnable programs:

| Example | Shows |
|---------|-------|
| [basic](examples/basic/) | Create a sandbox and run a command |
| [envvars](examples/envvars/) | Passing environment variables |
| [files](examples/files/) | Read/write text and binary files |
| [sandbox-info](examples/sandbox-info/) | Inspecting sandbox metadata |
| [fork](examples/fork/) | Fork a sandbox into N copies and fan work out |
| [volumes](examples/volumes/) | Persistent volumes and mounting them |
| [lifecycle](examples/lifecycle/) | Pause/resume, timeout, and snapshots |
| [network](examples/network/) | Outbound network policy and allowlists |

## Usage

### Creating a Client and Sandbox

```go
client, err := e2b.NewClient(e2b.ClientConfig{
    APIKey:        "your-api-key",       // or set E2B_API_KEY env var
    APIBaseURL:    "https://api.e2b.app", // optional
    SandboxDomain: "e2b.app",             // optional
})
if err != nil {
    log.Fatal(err)
}

sandbox, err := client.NewSandbox(context.Background(), e2b.SandboxConfig{
    Template: "base",               // sandbox template (default: "base")
    Timeout:  300,                  // lifetime in seconds (default: 300)
    EnvVars:  map[string]string{    // environment variables
        "MY_VAR": "value",
    },
})
```

The `base` template includes Python 3.11, Node.js 20, npm, Yarn, git, and the GitHub CLI.

### Running Commands

`Run` takes a `context.Context` and a single shell command string, executed
through a login shell (`/bin/bash -l -c`), so pipes, redirection, and
environment expansion all work. It blocks until the command finishes.

```go
ctx := context.Background()

// Simple command
result, err := sandbox.Commands.Run(ctx, "python3 -c 'print(1 + 1)'")

// Shell features
result, err = sandbox.Commands.Run(ctx, "echo one two three | wc -w")

// With options
result, err = sandbox.Commands.Run(ctx, "echo $FOO",
    e2b.WithEnv(map[string]string{"FOO": "bar"}),
    e2b.WithCwd("/tmp"),
    e2b.WithUser("root"),
    e2b.WithTimeout(30*time.Second),
)

fmt.Println(result.Stdout)
fmt.Println(result.Stderr)
fmt.Println(result.ExitCode)
```

A non-zero exit code returns a `*e2b.CommandExitError` (the `*CommandResult` is
still returned so you can inspect the output):

```go
result, err := sandbox.Commands.Run(ctx, "exit 3")
var exitErr *e2b.CommandExitError
if errors.As(err, &exitErr) {
    fmt.Println(exitErr.ExitCode) // 3
}
```

### Streaming Output

Stream stdout/stderr as it is produced via callbacks:

```go
handle, err := sandbox.Commands.Start(ctx, "for i in 1 2 3; do echo $i; sleep 1; done",
    e2b.WithOnStdout(func(b []byte) { fmt.Print(string(b)) }),
    e2b.WithOnStderr(func(b []byte) { fmt.Fprint(os.Stderr, string(b)) }),
)
if err != nil {
    log.Fatal(err)
}
result, err := handle.Wait(ctx) // drives the stream, returns the final result
```

### Background Commands, stdin, and Process Management

```go
// Start a command in the background.
handle, err := sandbox.Commands.Start(ctx, "cat", e2b.WithStdin(true))

// Send data to its stdin, then signal EOF.
handle.SendStdin(ctx, []byte("hello\n"))
handle.CloseStdin(ctx)

// List running processes and kill one by PID.
procs, _ := sandbox.Commands.List(ctx)
ok, _ := sandbox.Commands.Kill(ctx, procs[0].PID) // false if not found

// Detach without killing, then reattach later by PID.
handle.Disconnect()
reattached, _ := sandbox.Commands.Connect(ctx, handle.PID())

result, err := handle.Wait(ctx)
```

### Interactive Terminals (PTY)

```go
pty, err := sandbox.Pty.Create(ctx, 80, 24, // cols, rows
    e2b.WithPtyOnData(func(b []byte) { os.Stdout.Write(b) }),
)
if err != nil {
    log.Fatal(err)
}
sandbox.Pty.SendInput(ctx, pty.PID(), []byte("echo $TERM\n"))
sandbox.Pty.Resize(ctx, pty.PID(), 120, 40)
sandbox.Pty.SendInput(ctx, pty.PID(), []byte("exit\n"))
pty.Wait(ctx)
```

### Filesystem

`sandbox.Filesystem` provides read/write plus directory and metadata operations.
Text and byte helpers wrap the streaming `Read`/`Write` methods.

```go
fs := sandbox.Filesystem

fs.WriteString(ctx, "/home/user/hello.txt", "hi\n")
content, _ := fs.ReadString(ctx, "/home/user/hello.txt")

entries, _ := fs.List(ctx, "/home/user")   // []FileInfo
info, _ := fs.Stat(ctx, "/home/user/hello.txt")
ok, _ := fs.Exists(ctx, "/home/user/hello.txt")

fs.MakeDir(ctx, "/home/user/work")         // no-op if it already exists
fs.Rename(ctx, "/home/user/hello.txt", "/home/user/work/hi.txt")
fs.Remove(ctx, "/home/user/work/hi.txt")

// Watch a directory for changes.
watch, _ := fs.WatchDir(ctx, "/home/user", true)
events, _ := watch.GetEvents(ctx)
watch.Stop(ctx)
```

### Lifecycle: Pause, Resume, and Snapshots

A paused sandbox retains its filesystem and memory, so work resumes exactly
where it stopped.

```go
sandbox.SetTimeout(600)             // extend lifetime to 10 minutes

sandbox.Pause()                     // stop running, keep state
running, _ := sandbox.IsRunning()   // false
sandbox.Resume(300)                 // resume with a fresh 5-minute lifetime

// Point-in-time snapshot you can reference later.
snap, _ := sandbox.CreateSnapshot(ctx, "checkpoint")

// Observability.
logs, _ := sandbox.Logs()
metrics, _ := sandbox.Metrics()
```

### Forking

Fork clones a sandbox from a snapshot into one or more independent copies. Every
fork boots from the same state; after branching they no longer share anything.

```go
forks, err := sandbox.Fork(ctx, e2b.WithForkCount(3))
if err != nil {
    log.Fatal(err)
}
for i, f := range forks {
    if f.Err != nil {          // this fork failed to start
        continue
    }
    defer f.Sandbox.Close()    // each started fork must be closed
    f.Sandbox.Commands.Run(ctx, fmt.Sprintf("echo worker %d", i))
}
```

### Volumes

Volumes are persistent storage that outlives any single sandbox. Write to a
volume directly through its content API, or mount it into a sandbox.

```go
vol, _ := client.CreateVolume(ctx, "my-volume")
vol.WriteFileString(ctx, "/config.txt", "data\n")
got, _ := vol.ReadFileString(ctx, "/config.txt")

// Mount it into a sandbox — files appear on the sandbox filesystem.
sandbox, _ := client.NewSandbox(ctx, e2b.SandboxConfig{
    Template:     "base",
    VolumeMounts: []e2b.VolumeMount{vol.AsMount("/mnt/data")},
})

client.DestroyVolume(ctx, vol.VolumeID)
```

### Network Policy

Restrict a sandbox's outbound access at creation, or change it on a running
sandbox. `UpdateNetwork` replaces the entire mutable config rather than merging.

```go
// Allow only example.com; deny everything else.
sandbox, _ := client.NewSandbox(ctx, e2b.SandboxConfig{
    Template: "base",
    Network:  e2b.AllowOutbound("example.com"),
})

// Later, deny all outbound traffic.
sandbox.UpdateNetwork(e2b.NetworkUpdateConfig{
    DenyOut: []string{e2b.AllTraffic},
})
```

### Command Options

| Option | Description |
|--------|-------------|
| `WithEnv(map[string]string)` | Set environment variables for the command |
| `WithCwd(string)` | Set the working directory |
| `WithUser(string)` | Set the user to run the command as (default: `user`) |
| `WithTimeout(time.Duration)` | Set the command's maximum lifetime (default: 60s; ≤0 disables) |
| `WithStdin(bool)` | Keep stdin open for `SendStdin` |
| `WithOnStdout(func([]byte))` | Stream decoded stdout chunks |
| `WithOnStderr(func([]byte))` | Stream decoded stderr chunks |

## Configuration

Configuration can be provided via `ClientConfig` / `SandboxConfig` fields or environment variables:

| Field | Env Var | Default | Description |
|-------|---------|---------|-------------|
| `ClientConfig.APIKey` | `E2B_API_KEY` | — | E2B API key (required) |
| `ClientConfig.APIBaseURL` | `E2B_API_URL` | `https://api.e2b.app` | API base URL |
| `ClientConfig.SandboxDomain` | `E2B_SANDBOX_URL` | `e2b.app` | Sandbox domain |
| `SandboxConfig.Template` | — | `base` | Sandbox template ID |
| `SandboxConfig.Timeout` | — | `300` | Sandbox lifetime in seconds |

## Error Handling

```go
import e2b "github.com/matiasinsaurralde/go-e2b"

_, err := e2b.NewClient(e2b.ClientConfig{APIKey: apiKey})
switch {
case errors.As(err, &e2b.SandboxNotFoundError{}):
    // sandbox not found
case errors.As(err, &e2b.CommandExitError{}):
    // command ran but exited non-zero
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
