# Production Reliability, Lifecycles, and Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden `gosocket` for production by implementing ping/pong heartbeats, connection deadlines, message size limits, context-aware graceful shutdown, safe client lifecycle management (preventing panics and goroutine leaks), and deterministic unit/integration tests.

**Architecture:**
1. In `client.go`: Introduce standard WebSocket lifecycle constants (`writeWait`, `pongWait`, `pingPeriod`, `maxMessageSize`). Implement `sync.Once` for safe idempotent connection closing, add ping/pong heartbeat ticker in `Write()` / `WritePump()`, configure read limits and pong handler in `Read()` / `ReadPump()`, and ensure unregistration doesn't leak or block indefinitely.
2. In `hub.go`: Update `Hub.Run(ctx context.Context)` to accept a context for clean termination, close all active client connections cleanly upon context cancellation, and safely manage client registration, unregistration, and broadcast.
3. In `main.go`: Add signal-based graceful shutdown (`SIGINT`, `SIGTERM`), context cancellation for `Hub`, and graceful HTTP server shutdown (`server.Shutdown(ctx)`). Add healthcheck `/healthz` endpoint.
4. In test files (`client_test.go`, `hub_test.go`, `main_test.go`): Update existing tests to reflect new signatures, replace flaky `time.Sleep` calls with deterministic channel synchronization, and add tests for heartbeats, context shutdown, and concurrent safe closes with `-race` detection.

**Tech Stack:** Go 1.25+, `github.com/gorilla/websocket` v1.5.3, standard library (`context`, `sync`, `net/http`, `os/signal`, `testing`).

**Spec:** Option 1: Production Reliability, Lifecycles, and Hardening.

## Global Constraints

- Must work with Go 1.25+ as specified in `go.mod`.
- Must pass `go test -v -race ./...` with zero race conditions or deadlocks.
- All network read/write operations must be protected by explicit deadlines (`SetReadDeadline`, `SetWriteDeadline`).
- Closing a client must be completely idempotent and never cause a panic on double close.
- Preserves existing public APIs or provides backward-compatible adapters where appropriate.

## Review Focus

1. Deadlock when unregistering during hub shutdown: Client read loop attempting to unregister must not hang indefinitely if the hub context has already been canceled.
2. Double close panic: If both hub eviction (buffer full) and client read failure occur concurrently, neither `c.msg`, `c.done`, nor `c.conn` can panic with `close of closed channel`.
3. Ping/Pong timeout: A stalled client failing to respond to Ping within `pongWait` must be disconnected and cleaned up.
4. Graceful HTTP & Hub shutdown: Server shutdown signals must cause HTTP listener to stop accepting new requests, all clients to receive close messages, and background goroutines to exit cleanly.
5. Message size overflow: Payloads larger than `maxMessageSize` (512KB) must be rejected by the reader without crashing the server.

---

### Task 1: Harden Client Lifecycle, Heartbeats, and Deadlines

**Files:**
- Modify: `client.go`
- Test: `client_test.go`

**Interfaces:**
- Consumes: `Hub`, `*websocket.Conn`
- Produces:
  ```go
  type Client struct {
      conn      *websocket.Conn
      msg       chan []byte
      done      chan struct{}
      closeOnce sync.Once
  }
  func NewClient(conn *websocket.Conn) *Client
  func (c *Client) Close()
  func (c *Client) Read(hub *Hub)
  func (c *Client) Write()
  ```

- [ ] **Step 1: Write failing tests for Client safe Close, ping/pong ticker, and message size limits**

In `client_test.go`, add:
- `TestClient_ConcurrentClose`: Call `c.Close()` from multiple concurrent goroutines to verify idempotency and absence of panics.
- `TestClient_Heartbeat`: Verify `c.Write()` periodically sends WebSocket Ping frames when idle.
- `TestClient_MessageSizeLimit`: Verify reader enforces `maxMessageSize`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `TMPDIR=/workspaces/01a124db-d4e2-7ce1-87c1-92902a2028f8/tmp go test -v -race -run TestClient_ConcurrentClose ./...`
Expected: FAIL (Close method does not exist or lacks sync.Once).

- [ ] **Step 3: Implement Client enhancements in `client.go`**

- Add timing constants:
  - `writeWait = 10 * time.Second`
  - `pongWait = 60 * time.Second`
  - `pingPeriod = (pongWait * 9) / 10`
  - `maxMessageSize = 512 * 1024`
- Add `closeOnce sync.Once` to `Client` struct.
- Implement `(c *Client) Close()` method to safely close `c.conn`, `c.done`, and coordinate cleanup without double close.
- In `Read(hub *Hub)`:
  - Set `c.conn.SetReadLimit(maxMessageSize)`.
  - Set read deadline to `now + pongWait` and configure `SetPongHandler` to extend read deadline on incoming Pong.
  - In `defer`, unregister safely using non-blocking select or check against hub/done to avoid deadlocks.
- In `Write()`:
  - Set ticker for `pingPeriod`.
  - Handle `case <-ticker.C`: Set write deadline and send `websocket.PingMessage`.
  - Handle `case msg, ok := <-c.msg`: Set write deadline before writing `websocket.TextMessage`.
  - Handle `case <-c.done`: Send `websocket.CloseMessage` with write deadline and exit.

- [ ] **Step 4: Run client tests to verify they pass**

Run: `TMPDIR=/workspaces/01a124db-d4e2-7ce1-87c1-92902a2028f8/tmp go test -v -race -run TestClient ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add client.go client_test.go
git commit -m "feat(client): implement heartbeats, read/write deadlines, and safe lifecycle management"
```

---

### Task 2: Implement Hub Context-Aware Graceful Shutdown and Deterministic Tests

**Files:**
- Modify: `hub.go`
- Test: `hub_test.go`

**Interfaces:**
- Consumes: `Client`, `context.Context`
- Produces:
  ```go
  func (h *Hub) Run(ctx context.Context) // accepts ctx for lifecycle termination
  func (h *Hub) Broadcast(message []byte)
  func (h *Hub) ClientCount() int
  ```

- [ ] **Step 1: Write failing tests for Hub context cancellation and deterministic broadcast**

In `hub_test.go`, add:
- `TestHub_GracefulShutdown`: Verify that canceling `ctx` exits `h.Run(ctx)` and closes/cleans up registered clients.
- `TestHub_ClientCount`: Thread-safe verification of active connected clients count.
- Refactor existing `hub_test.go` to remove brittle `time.Sleep` delays and replace with deterministic channel or synchronization assertions.

- [ ] **Step 2: Run Hub tests to verify failure**

Run: `TMPDIR=/workspaces/01a124db-d4e2-7ce1-87c1-92902a2028f8/tmp go test -v -race -run TestHub_GracefulShutdown ./...`
Expected: FAIL (`h.Run` signature doesn't take context or method missing).

- [ ] **Step 3: Implement Hub enhancements in `hub.go`**

- Update `(h *Hub) Run(ctx context.Context)`:
  - Select across `<-ctx.Done()`, `<-h.register`, `<-h.unregister`, `<-h.broadcast`.
  - On `<-ctx.Done()`: Loop through all `h.clients`, safely call `client.Close()`, delete entries, and return.
- Add `(h *Hub) ClientCount() int` with a read lock or synchronized query if needed.
- Add `(h *Hub) Broadcast(message []byte)` helper method.
- Update buffer overflow eviction to safely call client cleanup without double close.

- [ ] **Step 4: Run Hub tests to verify they pass**

Run: `TMPDIR=/workspaces/01a124db-d4e2-7ce1-87c1-92902a2028f8/tmp go test -v -race -run TestHub ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add hub.go hub_test.go
git commit -m "feat(hub): add context-aware graceful shutdown and thread-safe helpers"
```

---

### Task 3: Graceful HTTP Server Lifecycle, Health Endpoint, and Integration Tests

**Files:**
- Modify: `main.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `Hub`, `Client`, `http.Server`
- Produces:
  ```go
  func NewServer(addr string, hub *Hub) *http.Server
  func HandleHealthz(w http.ResponseWriter, r *http.Request)
  func HandleSocket(hub *Hub) http.HandlerFunc
  ```

- [ ] **Step 1: Write integration tests for Graceful Server Shutdown and Healthcheck**

In `main_test.go`:
- Test `GET /healthz` returns `200 OK`.
- Test `NewServer` starts, serves `/ws`, echoes broadcast, and shuts down cleanly with `server.Shutdown(ctx)`.

- [ ] **Step 2: Run integration tests to verify failure**

Run: `TMPDIR=/workspaces/01a124db-d4e2-7ce1-87c1-92902a2028f8/tmp go test -v -race -run TestHealthCheck ./...`
Expected: FAIL

- [ ] **Step 3: Implement server restructuring and graceful shutdown in `main.go`**

- Implement `NewServer(addr string, hub *Hub) *http.Server` to decouple server instantiation from `main()` for testability.
- Add `GET /healthz` handler.
- In `main()`:
  - Create root cancellable context with `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`.
  - Run `go hub.Run(ctx)`.
  - Start HTTP server in a separate goroutine.
  - Wait for shutdown signal via `<-ctx.Done()`.
  - Perform graceful `server.Shutdown(shutdownCtx)` with a 5-second timeout.
  - Log shutdown completion.

- [ ] **Step 4: Run all integration tests**

Run: `TMPDIR=/workspaces/01a124db-d4e2-7ce1-87c1-92902a2028f8/tmp go test -v -race ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat(server): add graceful shutdown, healthcheck endpoint, and integration tests"
```

---

### Task 4: Full Suite Verification and CI Pipeline Validation

**Files:**
- Modify: `.github/workflows/ci.yml` (if needed to verify flags and Go version)

- [ ] **Step 1: Run full test suite with race detector and coverage**

Run: `TMPDIR=/workspaces/01a124db-d4e2-7ce1-87c1-92902a2028f8/tmp go test -v -race -cover ./...`
Expected: All tests PASS with 0 race warnings.

- [ ] **Step 2: Run linter and formatting checks**

Run: `go vet ./...` and `gofmt -s -w .`
Expected: Clean output.

- [ ] **Step 3: Verify CI workflow runs cleanly**

Verify `.github/workflows/ci.yml` uses the appropriate test command (`go test -v -race -cover ./...`).
