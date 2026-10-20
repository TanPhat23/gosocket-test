# Gosocket Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform the prototype gosocket server into a production-ready, reliable, leak-free, clean-shutting-down WebSocket service with full test coverage and ping/pong heartbeats.

**Architecture:** 
1. Fix connection and goroutine lifecycle in `Client`: implement heartbeats (ping/pong deadlines), proper teardown without double-close races, and robust reader/writer goroutines.
2. Fix `Hub` concurrency and lifecycle: support graceful shutdown via `context.Context` (or close chan), clean unregistration, non-blocking broadcast handling, and thread-safe operations.
3. Server improvements in `main.go`: graceful HTTP server shutdown (`os.Interrupt`, `SIGTERM`), configurable settings, Origin checking options, and structured endpoints (e.g. `/ws` and health check `/healthz`).
4. Comprehensive unit & integration tests covering concurrent connections, message broadcasts, slow clients, and graceful shutdown.

**Tech Stack:** Go (1.25+), `github.com/gorilla/websocket`, standard library (`net/http`, `sync`, `context`, `time`).

**Spec:** Codebase inspection of `client.go`, `hub.go`, `main.go`, `dockerfile`.

## Global Constraints
- Do not introduce unnecessary third-party dependencies beyond standard library and `github.com/gorilla/websocket`.
- Ensure zero goroutine leaks on client disconnect or server shutdown.
- Ensure all tests pass under `-race` (`go test -race ./...`).

## Review Focus
- Concurrency & double close panic on `client.msg` or `client.done`.
- Slow client write queue blockage or dropped client cleanup without deadlock.
- Ping/pong heartbeat timeouts detecting stale or abruptly severed connections.
- Clean shutdown terminating all client sockets with WebSocket Close frames.
- Race detector validation under concurrent connects/disconnects/broadcasts.

---

### Task 1: Client Lifecycle & Heartbeat Management

**Files:**
- Modify: `client.go`
- Create: `client_test.go`

**Interfaces:**
- `NewClient(conn *websocket.Conn, hub *Hub) *Client`
- `(c *Client) ReadPump()`
- `(c *Client) WritePump()`
- `(c *Client) Close()`

- [ ] **Step 1: Write tests for client lifecycle and close handling in `client_test.go`**
- [ ] **Step 2: Run tests to verify failure/missing behavior**
- [ ] **Step 3: Refactor `client.go` to implement standard gorilla websocket pump pattern with read/write deadlines, ping/pong handlers, max message size limits, and safe close**
- [ ] **Step 4: Run tests and ensure `go test -race -v -run TestClient` passes**
- [ ] **Step 5: Commit changes**

---

### Task 2: Hub Lifecycle, Context & Graceful Shutdown

**Files:**
- Modify: `hub.go`
- Create: `hub_test.go`

**Interfaces:**
- `NewHub() *Hub`
- `(h *Hub) Run(ctx context.Context)`
- `(h *Hub) Broadcast(msg []byte)`

- [ ] **Step 1: Write unit tests in `hub_test.go` testing client register, unregister, broadcast, slow client eviction, and context cancellation shutdown**
- [ ] **Step 2: Run tests to verify failures**
- [ ] **Step 3: Update `hub.go` to support context-based cancellation, safe unregistration, and clean resource cleanup**
- [ ] **Step 4: Run `go test -race -v -run TestHub`**
- [ ] **Step 5: Commit changes**

---

### Task 3: HTTP Server, Handlers, Health Check & Graceful Termination

**Files:**
- Modify: `main.go`
- Create: `main_test.go`
- Modify: `dockerfile`

**Interfaces:**
- `NewServer(addr string, hub *Hub) *http.Server`
- Graceful shutdown signal handling in `main()`
- Endpoint `/healthz` for readiness/liveness

- [ ] **Step 1: Write integration tests in `main_test.go` testing `/ws` connection handshake, echo/broadcast, and `/healthz` endpoint**
- [ ] **Step 2: Run tests to verify failures**
- [ ] **Step 3: Implement `main.go` with proper Upgrader check (CheckOrigin configurable or safe default), `/healthz`, and graceful signal shutdown**
- [ ] **Step 4: Update `dockerfile` with correct build & version tags**
- [ ] **Step 5: Run full test suite with race detector: `go test -race -v ./...`**
- [ ] **Step 6: Commit changes**
