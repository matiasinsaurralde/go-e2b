# Sandbox Management API Endpoints

This document tracks the sandbox management API endpoints and their implementation status in the Go SDK.

## Endpoints Overview

| Status | Priority | Endpoint | Method | Purpose | SDK Method |
|--------|----------|----------|--------|---------|------------|
| ✅ | **HIGH** | `/sandboxes` | POST | Create a new sandbox | `client.NewSandbox(ctx, cfg)` |
| ✅ | **HIGH** | `/sandboxes/{id}` | DELETE | Destroy a sandbox | `sandbox.Close()` |
| ✅ | **HIGH** | `/sandboxes/{id}` | GET | Get sandbox details | `sandbox.Info()` |
| ✅ | **HIGH** | `/sandboxes` | GET | List all sandboxes | `client.ListSandboxes(ctx)` |
| ✅ | **HIGH** | `/sandboxes/{id}/timeout` | POST | Update sandbox lifetime | `sandbox.SetTimeout(secs)` |
| ✅ | **MEDIUM** | `/sandboxes/{id}/metrics` | GET | Get resource usage | `sandbox.Metrics()` |
| ❌ | **MEDIUM** | `/sandboxes/{id}/logs/v2` | GET | Access sandbox logs | — |
| ❌ | **LOW** | `/sandboxes/{id}/pause` | POST | Pause sandbox | — |
| ❌ | **LOW** | `/sandboxes/{id}/snapshots` | POST | Create snapshot | — |
| ❌ | **LOW** | `/sandboxes/{id}/snapshots` | GET | List snapshots | — |

---

## Implemented Endpoints

### 1. Create Sandbox (✅ Implemented)

**Endpoint:** `POST /sandboxes`

```go
client, _ := e2b.NewClient(e2b.ClientConfig{APIKey: "your-key"})

sandbox, err := client.NewSandbox(ctx, e2b.SandboxConfig{
    Template: "base",
    Timeout:  600,
    EnvVars:  map[string]string{"KEY": "value"},
})
```

### 2. Destroy Sandbox (✅ Implemented)

**Endpoint:** `DELETE /sandboxes/{id}`

```go
err := sandbox.Close()
// or with context:
err := sandbox.CloseWithContext(ctx)
```

### 3. Get Sandbox Details (✅ Implemented)

**Endpoint:** `GET /sandboxes/{id}`

Returns `SandboxInfo` with ID, template, state, CPU/memory/disk, lifecycle, volume mounts, etc.

```go
info, err := sandbox.Info()
// or with context:
info, err := sandbox.InfoWithContext(ctx)

fmt.Printf("State: %s, CPU: %d, Memory: %d MB\n", info.State, info.CPUCount, info.MemoryMB)
```

### 4. List Sandboxes (✅ Implemented)

**Endpoint:** `GET /sandboxes`

Returns `[]SandboxInfo` for all sandboxes associated with the API key.

```go
sandboxes, err := client.ListSandboxes(ctx)
for _, sbx := range sandboxes {
    fmt.Printf("Sandbox %s: %s\n", sbx.ID, sbx.State)
}
```

### 5. Update Sandbox Timeout (✅ Implemented)

**Endpoint:** `POST /sandboxes/{id}/timeout`

Updates the sandbox lifetime. The API returns `204 No Content` on success with an empty body.

```go
err := sandbox.SetTimeout(600) // extend to 10 minutes
// or with context:
err := sandbox.SetTimeoutWithContext(ctx, 600)
```

### 6. Get Sandbox Metrics (✅ Implemented)

**Endpoint:** `GET /sandboxes/{id}/metrics`

Returns `[]SandboxMetric` with periodic resource usage snapshots (CPU, memory, disk). The API returns `200 OK` with an empty array `[]` when no metrics are available yet (including for nonexistent sandbox IDs — no 404 is returned).

```go
metrics, err := sandbox.Metrics()
// or with context:
metrics, err := sandbox.MetricsWithContext(ctx)

for _, m := range metrics {
    fmt.Printf("CPU: %.1f%%, Mem: %d/%d bytes, Disk: %d/%d bytes\n",
        m.CPUUsedPct, m.MemUsed, m.MemTotal, m.DiskUsed, m.DiskTotal)
}
```

**API response shape** (verified via curl):
```json
[
  {
    "cpuCount": 2,
    "cpuUsedPct": 13.43,
    "memTotal": 505417728,
    "memUsed": 49197056,
    "memCache": 69632000,
    "diskTotal": 22772514816,
    "diskUsed": 1681707008,
    "timestamp": "2026-05-19T07:11:20Z",
    "timestampUnix": 1779174680
  }
]
```

---

## Missing Endpoints

### 7. Get Sandbox Logs (❌ MEDIUM PRIORITY)

**Endpoint:** `GET /sandboxes/{id}/logs/v2`

**Purpose:** Retrieve system logs from the sandbox.

**Proposed API:**

```go
logs, err := sandbox.Logs(ctx, WithLogLines(50))
```

**Note:** Need to verify actual API response shape and query parameters before implementing.

---

### 8. Pause Sandbox (❌ LOW PRIORITY)

**Endpoint:** `POST /sandboxes/{id}/pause`

**Purpose:** Pause sandbox execution (stops billing, preserves state).

**Proposed API:**

```go
err := sandbox.Pause(ctx)
```

---

### 9. Create Snapshot (❌ LOW PRIORITY)

**Endpoint:** `POST /sandboxes/{id}/snapshots`

**Purpose:** Save sandbox state for later restoration.

**Proposed API:**

```go
snapshot, err := sandbox.CreateSnapshot(ctx)
```

---

### 10. List Snapshots (❌ LOW PRIORITY)

**Endpoint:** `GET /sandboxes/{id}/snapshots`

**Purpose:** List all snapshots for a sandbox.

**Proposed API:**

```go
snapshots, err := sandbox.ListSnapshots(ctx)
```

---

## Implementation Process

Steps for implementing a new endpoint in this SDK:

1. **Curl the live API** — Create a sandbox, then hit the target endpoint with `curl -sv` to capture the exact request/response (method, headers, body, status code). Test both success and error cases (e.g., 404 for nonexistent sandbox).
2. **Implement the method** — Add `Method()` and `MethodWithContext(ctx)` variants on `Sandbox` (or `Client` for collection endpoints). Use `s.client.*` for API key, base URL, and HTTP client.
3. **Write unit tests** — Use `httptest.NewServer` to mock the API. Cover: success, not-found (404 → `SandboxNotFoundError`), server error (500 → `Error`), canceled context, and request validation (method, path, headers, body).
4. **Run full test suite** — `go test ./... -count=1` to ensure nothing is broken.
5. **Update this document** — Mark the endpoint as ✅ in the overview table, move it to the "Implemented" section, and check it off in the checklist.

---

## Implementation Patterns

All methods follow the established SDK conventions:

- **Client-centric:** All authenticated operations go through `Client`
- **Context support:** Methods accept `context.Context` or provide `WithContext` variants
- **Error handling:** Use `SandboxNotFoundError` for 404, generic `Error` for other HTTP errors
- **Functional options:** Use option functions for configurable endpoints (e.g., `WithLogLines`)

---

## Implementation Checklist

- [x] Create sandbox (`client.NewSandbox`)
- [x] Destroy sandbox (`sandbox.Close`)
- [x] Get sandbox details (`sandbox.Info`)
- [x] List sandboxes (`client.ListSandboxes`)
- [x] Update sandbox timeout (`sandbox.SetTimeout`)
- [x] Get sandbox metrics (`sandbox.Metrics`)
- [ ] Get sandbox logs (`sandbox.Logs`)
- [ ] Pause sandbox (`sandbox.Pause`)
- [ ] Create snapshot (`sandbox.CreateSnapshot`)
- [ ] List snapshots (`sandbox.ListSnapshots`)
