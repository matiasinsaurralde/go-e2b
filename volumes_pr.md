# PR Proposal — Volumes

## Title

```
feat(volumes): add Volume management and content API with full parity
```

## Body

```markdown
Implements the E2B Volumes module for the Go SDK, reaching feature parity with
the JS and Python reference SDKs. Volumes are a standalone product with two HTTP
surfaces on the same host but different auth, both wired onto the existing
`Client.httpClient` (so a user-supplied transport/proxy is honored):

- **Management API** (`X-API-Key`) — create / connect / info / list / destroy.
- **Content API** (`Authorization: Bearer <volume-token>`) — list, mkdir, stat,
  exists, update-metadata, read, write, remove.

### Management — `*Client` methods (`volume_client.go`)

- `CreateVolume(ctx, name)` — `POST /volumes`; returns a token-bearing `*Volume`.
- `ConnectVolume(ctx, volumeID)` — `GetVolumeInfo` + construct handle.
- `GetVolumeInfo(ctx, volumeID)` — 404 → `*VolumeNotFoundError`.
- `ListVolumes(ctx)` — `[]VolumeInfo`.
- `DestroyVolume(ctx, volumeID)` — `true` / `false` on 404 / `*VolumeError`.

### Content — `*Volume` methods (`volume.go`)

- `List` (depth defaults to 1 server-side; sent only when set via `WithVolumeDepth`)
- `MakeDir`, `WriteFile*` (`WithVolumeUID/GID/Mode/Force`)
- `GetInfo`, `Exists` (404 → `false`)
- `UpdateMetadata` (PATCH JSON `uid/gid/mode`)
- `ReadFile` / `ReadFileBytes` / `ReadFileString` (streaming, 1 h file timeout)
- `WriteFile` / `WriteFileBytes` / `WriteFileString` (streamed octet-stream body)
- `Remove` (path 404 → `*FileNotFoundError`)

### Design notes

- Optional query params (`uid`, `gid`, `mode`, `force`, `depth`) are backed by
  `*int`/`*uint32`/`*bool` so an explicit zero (`uid=0`, `mode=0`) is
  distinguishable from "unset" and is sent, while unset params are omitted.
- `VolumeEntryStat` parses ISO-8601 timestamps (RFC3339 / RFC3339Nano) into
  `time.Time` and carries the symlink `target`.
- Timeouts match the reference: 60 s request timeout, 1 h file-transfer timeout.
- Errors: `VolumeError` (generic non-2xx), `VolumeNotFoundError` (volume-level
  404), reused `FileNotFoundError` (content path 404), and a token guard that
  returns `*AuthenticationError` when a content call is made without a token.
- `Volume.AsMount(path)` bridges to the existing `SandboxConfig.VolumeMounts`.
- Adds an SDK `User-Agent` (`e2b-go-sdk/<version>`) on volume requests, matching
  the reference SDKs' header format.

### Tests

- `volume_test.go` — unit tests via `httptest.Server` covering both surfaces:
  auth headers, method + path + query, omit-when-unset vs. zero-value-sent
  semantics, stat parsing (incl. symlink target and fractional seconds), error
  mapping, and the token guard.
- `volume_integration_test.go` (build tag `integration`) — full live lifecycle:
  create → write → info/exists → read round-trip → list (with depth) → mkdir →
  update-metadata → remove → list-volumes → destroy (true) → destroy (false) →
  get-info (`*VolumeNotFoundError`).

`go build`, `go vet`, `go test ./...`, and `golangci-lint run ./...` are clean.

> Note: the live integration test was exercised against a real API key and
> reached the endpoint successfully; the account used does not currently have
> volumes enabled (`403 "use of volumes is not enabled"`), which the code
> correctly surfaces as `*VolumeError`. The test skips when `E2B_API_KEY` is unset.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
```
