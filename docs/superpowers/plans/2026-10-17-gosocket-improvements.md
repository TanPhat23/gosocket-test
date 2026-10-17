# Gosocket Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix concurrency/channel panic bugs, add ping/pong heartbeats and timeouts, support graceful hub/server shutdown, configure CORS/origin checking, and add comprehensive unit/integration tests with race detection.

**Architecture:** Refactor `Client` and `Hub` lifecycle to guarantee single, coordinated closure of channels (`sync.Once`), implement standard Gorilla WebSocket ping/pong heartbeat patterns with read/write deadlines, add context-driven shutdown to `Hub`, and wrap server configuration cleanly.

**Tech Stack:** Go (1.25+), `github.com/gorilla/websocket`, standard library `net/http`, `testing`.

**Spec:** Current codebase analysis of `gosocket` (`main.go`, `client.go`, `hub.go`).

## Global Constraints
- Preserve existing public/expected behavior (`/ws` endpoint, hub broadcast, read/write concurrency separation).
- Clean exit without panics (specifically `send on closed channel` or `close of closed channel`).
- Race detector clean: `go test -race ./...` must pass with zero data races.

## Review Focus
1. Rapid disconnect while broadcast in progress (should not panic or deadlock).
2. Unregister called multiple times for the same client (idempotent cleanup).
3. Ping/pong heartbeat timeout terminates stalled client connections promptly.
4. Broadcast buffer saturation removes slow clients safely without panicking write pump.
5. Server and Hub shutdown terminates all active client connections and goroutines cleanly.

---

### Task 1: Comprehensive Concurrency & Lifecycle Fixes for Client and Hub

**Files:**
- Modify: `client.go`
- Modify: `hub.go`
- Test: `hub_test.go`
- Test: `client_test.go`

**Interfaces:**
- `Client`:
  - `NewClient(conn *websocket.Conn)`
  - `(c *Client) Read(hub *Hub)`
  - `(c *Client) Write()`
  - `(c *Client) Close()`
- `Hub`:
  - `NewHub() *Hub`
  - `(h *Hub) Run(ctx context.Context)` or `(h *Hub) Stop()`

- [ ] **Step 1: Write unit and concurrency tests exposing race/channel closing edge cases**
- [ ] **Step 2: Run tests to verify failure/coverage (`go test -race -v ./...`)**
- [ ] **Step 3: Implement `sync.Once` cleanup, safe unregister, and context cancellation on Hub**
- [ ] **Step 4: Run tests to verify they pass under `-race`**
- [ ] **Step 5: Commit changes**

---

### Task 2: Ping/Pong Heartbeat and Timeout Deadlines

**Files:**
- Modify: `client.go`
- Test: `client_test.go`

**Interfaces:**
- Constants:
  - `writeWait = 10 * time.Second`
  - `pongWait = 60 * time.Second`
  - `pingPeriod = (pongWait * 9) / 10`
  - `maxMessageSize = 512` (or configurable)

- [ ] **Step 1: Write tests for client ping/pong and read deadline timeout**
- [ ] **Step 2: Run test to verify behavior**
- [ ] **Step 3: Implement ping ticker in write pump and pong handler / read limit in read pump**
- [ ] **Step 4: Run tests to verify heartbeat handling passes**
- [ ] **Step 5: Commit changes**

---

### Task 3: HTTP Server Configuration, Origin Check & Graceful Shutdown

**Files:**
- Modify: `main.go`
- Modify: `dockerfile` (fix Go version)
- Test: `main_test.go`

**Interfaces:**
- `CheckOrigin` configuration on `websocket.Upgrader`
- Graceful shutdown handling `os.Interrupt` / `SIGTERM` via `http.Server.Shutdown(ctx)`

- [ ] **Step 1: Write integration test for `/ws` upgrade, origin handling, and shutdown**
- [ ] **Step 2: Run test to verify**
- [ ] **Step 3: Implement upgrader origin validation and graceful signal handling in `main.go`**
- [ ] **Step 4: Verify full test suite passes with `go test -v -race ./...`**
- [ ] **Step 5: Commit changes**
