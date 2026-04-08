# Development Guide

## Proto Bindings

The SDK communicates with the E2B sandbox daemon (`envd`) using the
[Connect RPC](https://connectrpc.com) protocol. The service is defined in
`proto/envd/process/process.proto` — a vendored copy of the upstream definition
from [e2b-dev/infra](https://github.com/e2b-dev/infra).

Generated Go code lives in `internal/gen/` and is committed to the repository.
**Do not edit files under `internal/gen/` by hand.**

---

## One-time Setup

Install the three tools needed for code generation. These are only required
when you need to regenerate bindings — they are **not** needed to build or use
the SDK.

```sh
# buf — drives the code generation pipeline
brew install bufbuild/buf/buf          # macOS
# or: go install github.com/bufbuild/buf/cmd/buf@latest

# protoc-gen-go — generates Go protobuf message types (.pb.go)
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

# protoc-gen-connect-go — generates the typed Connect client (.connect.go)
go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
```

Verify all three are in your PATH:

```sh
buf --version
protoc-gen-go --version
protoc-gen-connect-go --version
```

---

## Regenerating Bindings

Use this when you have updated `proto/envd/process/process.proto` manually
and want to regenerate:

```sh
make generate
```

This runs `buf generate` and `go mod tidy`.

---

## Syncing proto from upstream (same commit)

To re-fetch the proto at the currently pinned commit and regenerate:

```sh
make proto-sync
```

The pinned commit is stored in `proto/envd/VERSION`.

---

## Upgrading to a newer upstream version

To pull the latest commit of `process.proto` from `e2b-dev/infra`, update the
pin, and regenerate everything:

```sh
make proto-upgrade
```

This will:
1. Query the GitHub API for the latest commit that touched `process.proto`
2. Write the new SHA to `proto/envd/VERSION`
3. Fetch the new proto
4. Regenerate bindings via `buf generate`

Review the diff carefully before committing — breaking changes in the proto
will appear as compile errors or changed generated types.

---

## Pinning a specific upstream commit manually

Edit `proto/envd/VERSION` to contain the desired commit SHA, then run:

```sh
make proto-sync
```

---

## Running tests / lint / security scan

```sh
make test    # go test ./... -race
make lint    # golangci-lint run ./...
make gosec   # gosec (excludes internal/gen)
```

---

## File layout

```
proto/
  envd/
    VERSION                  ← pinned upstream commit SHA
    process/
      process.proto          ← vendored proto (DO NOT edit upstream options)
buf.yaml                     ← buf module config (proto root = proto/)
buf.gen.yaml                 ← buf generation config (output → internal/gen)
internal/
  gen/
    envd/
      process/
        process.pb.go        ← generated message types   (DO NOT EDIT)
        processconnect/
          process.connect.go ← generated Connect client  (DO NOT EDIT)
```

The `go_package` option in the vendored proto is set to
`github.com/matiasinsaurralde/go-e2b/internal/gen/envd/process` so the import
paths in generated code match the module layout. This option is injected by
`make proto-sync` if the upstream file does not carry it.
