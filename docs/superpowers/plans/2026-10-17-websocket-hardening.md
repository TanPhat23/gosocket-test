# WebSocket Production Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden the WebSocket server with concurrency safety, ping/pong heartbeats, connection timeouts, message size limits, origin checks, graceful shutdown, and comprehensive test coverage.

**Architecture:** Refactor `Client` and `Hub` into safe, leak-free components:
- `Client` manages its own connection lifecycle with `sync.Once` to prevent panics and double channel closures.
- Ping/pong heartbeats and write deadlines ensure stale/hung connections get cleaned up.
- `Hub` supports buffered control channels, thread-safe client tracking, and a clean shutdown context to disconnect all clients gracefully.
- `main.go` wires signal handling (`SIGINT`/`SIGTERM`) to trigger graceful shutdown of the HTTP server and WebSocket hub.

**Tech Stack:** Go 1.25+, `github.com/gorilla/websocket`, standard library (`sync`, `net/http`, `context`, `os/signal`).

**Spec:** Hardening requirements:
- Safe channel and connection closing without double-close panics.
- Read deadline reset on pong; periodic ping ticker on write loop.
- Write deadline on all outgoing frames.
- Read limit to prevent oversized payload attacks.
- Configurable origin checking.
- Clean shutdown for `Hub` and `http.Server`.
- Comprehensive automated test suite with race detector (`go test -race`).

## Global Constraints

- Go standard library conventions and idiomatic Go.
- Zero race conditions (`go test -race ./...` must pass).
- No panics on abnormal or concurrent disconnects.

## Review Focus

1. Client disconnects abruptly while Hub is broadcasting to it: Hub handles dropped client without panic or blocking other clients.
2. Slow consumer fills send channel buffer: Hub drops slow client safely without panicking on closed channel.
3. Client stops sending pings/pongs: Read timeout fires, connection closes, resources released.
4. Server receives SIGINT/SIGTERM: HTTP server stops accepting connections, Hub unregisters all clients, closes active connections, goroutines exit cleanly.
5. Inbound message exceeds size limit: Reader rejects frame with error and terminates connection cleanly.

---

### Task 1: Client Lifecycle & Heartbeat Hardening

**Files:**
- Modify: `client.go`
- Test: `client_test.go`

**Interfaces:**
- Consumes: `Hub` register/unregister channels, `websocket.Conn`.
- Produces:
  ```go
  const (
      writeWait = 10 * time.Second
      pongWait = 60 * time.Second
      pingPeriod = (pongWait * 9) / 10
      maxMessageSize = 512 * 1024 // 512KB
  )
  type Client struct {
      hub  *Hub
      conn *websocket.Conn
      send chan []byte
      closeOnce sync.Once
  }
  func NewClient(hub *Hub, conn *websocket.Conn) *Client
  func (c *Client) ReadPump()
  func (c *Client) WritePump()
  func (c *Client) Close()
  ```

- [ ] **Step 1: Write failing client lifecycle tests in `client_test.go`**
  Test concurrent calls to `Close()`, buffer overflow handling, and ping/pong deadlines.

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v -run TestClientCloseOnce`
  Expected: FAIL (types/functions not matching or failing)

- [ ] **Step 3: Implement client hardening in `client.go`**
  Implement `ReadPump` with `c.conn.SetReadLimit(maxMessageSize)`, `c.conn.SetReadDeadline`, pong handler, and `WritePump` with `pingPeriod` ticker, `writeWait`, and text/ping frame dispatch. Guard channel closing with `sync.Once`.

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v -race ./...`
  Expected: PASS

- [ ] **Step 5: Commit**
  ```bash
  git add client.go client_test.go
  git commit -m "fix(client): harden client lifecycle, heartbeats, and timeouts"
  ```

---

### Task 2: Hub Concurrency & Graceful Shutdown

**Files:**
- Modify: `hub.go`
- Test: `hub_test.go`

**Interfaces:**
- Consumes: `Client` instances.
- Produces:
  ```go
  type Hub struct {
      clients    map[*Client]bool
      broadcast  chan []byte
      register   chan *Client
      unregister chan *Client
      shutdown   chan struct{}
      done       chan struct{}
  }
  func NewHub() *Hub
  func (h *Hub) Run()
  func (h *Hub) Stop()
  ```

- [ ] **Step 1: Write failing Hub tests in `hub_test.go`**
  Test registering multiple clients, broadcasting messages, unregistering clients, slow client eviction without panic, and `Stop()` shutting down all clients.

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v -run TestHub`
  Expected: FAIL

- [ ] **Step 3: Implement `Hub` improvements in `hub.go`**
  Add buffered channels (`broadcast`, `register`, `unregister`), `shutdown` channel, handle unregistration cleanly through `client.Close()`, and iterate clients on shutdown to cleanly close connections.

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v -race -run TestHub`
  Expected: PASS

- [ ] **Step 5: Commit**
  ```bash
  git add hub.go hub_test.go
  git commit -m "feat(hub): add graceful hub shutdown and safe channel management"
  ```

---

### Task 3: HTTP Server Integration, Origin Checks & Graceful OS Signal Handling

**Files:**
- Modify: `main.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `Hub`, `Client`, `http.Server`.
- Produces:
  ```go
  type Server struct {
      hub        *Hub
      httpServer *http.Server
  }
  func NewServer(addr string) *Server
  func (s *Server) Start() error
  func (s *Server) Shutdown(ctx context.Context) error
  ```

- [ ] **Step 1: Write integration tests in `main_test.go`**
  Test end-to-end WebSocket connection over HTTP, message echo/broadcast between two connected test clients, oversized payload rejection, and graceful server shutdown.

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test -v -run TestServerE2E`
  Expected: FAIL

- [ ] **Step 3: Refactor `main.go`**
  Implement `Server` struct with `CheckOrigin` policy (safe default allowing localhost or configurable via env/parameter), route handler, and signal catching in `main()` with `context.WithTimeout`.

- [ ] **Step 4: Run test to verify it passes**
  Run: `go test -v -race ./...`
  Expected: PASS

- [ ] **Step 5: Commit**
  ```bash
  git add main.go main_test.go
  git commit -m "feat(server): add origin check, server wrapper, and graceful shutdown"
  ```
