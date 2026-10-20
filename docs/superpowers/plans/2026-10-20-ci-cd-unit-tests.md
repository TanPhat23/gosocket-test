# CI/CD Pipeline for Unit Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub Actions CI workflow to run unit tests and code verification on push and pull requests, along with unit tests for the Go WebSocket codebase.

**Architecture:** 
1. Add unit/integration tests for the Go websocket server components (`Hub`, `Client`, and HTTP `/ws` handler) using Go's built-in `testing` package and `net/http/httptest`.
2. Configure a GitHub Actions workflow (`.github/workflows/ci.yml`) triggering on `push` and `pull_request` targeting standard branches (`main`, `master`, and feature branches), setting up Go, downloading dependencies, and executing `go test -v -race ./...`.

**Tech Stack:** Go 1.25+ (`testing`, `net/http/httptest`), GitHub Actions (`actions/checkout@v4`, `actions/setup-go@v5`).

**Spec:** The prompt requests: "add a ci/cd pipeline for unit test".

## Global Constraints

- Must work with Go 1.25+ as specified in `go.mod`.
- GitHub Actions workflow must pin official verified actions (`actions/checkout@v4`, `actions/setup-go@v5`).
- Testing must include race detector checks (`go test -race ./...`).
- Tests must clean up goroutines and channels cleanly without deadlocks or resource leaks.

## Review Focus

- Goroutine leak on hub / client shutdown: Hub and test clients must terminate cleanly during test teardown.
- Concurrency race conditions: Broadcast and registration channel operations tested with race detector (`-race`).
- Edge cases in Hub message delivery: Broadcast when buffer is full triggers client drop without panic.
- Client closure handling: Writing to a closed client terminates cleanly without double-close panic.
- CI workflow triggers: Trigger correctly on both push and pull_request events.

---

### Task 1: Add Unit Tests for Hub and Client

**Files:**
- Create: `hub_test.go`
- Create: `client_test.go`
- Modify: `main_test.go` (or `main.go` if test helpers needed)

**Interfaces:**
- Consumes: `NewHub() *Hub`, `hub.Run()`, `NewClient(conn) *Client`, `client.Read(hub)`, `client.Write()`
- Produces: Test suites running via `go test ./...`

- [ ] **Step 1: Write unit tests for Hub lifecycle, registration, and broadcast**

Create `hub_test.go` testing:
1. Hub creation and client registration/unregistration.
2. Hub broadcast to registered clients.
3. Hub buffer overflow handling (dropping slow client).

- [ ] **Step 2: Run Hub unit tests to verify they pass**

Run: `go test -v -race -run TestHub ./...`
Expected: PASS

- [ ] **Step 3: Write tests for Client and WebSocket endpoint**

Create `client_test.go` and/or `main_test.go` testing:
1. Client read/write loop with mock/in-memory WebSocket connection using `httptest.NewServer`.
2. HTTP `/ws` endpoint connection upgrade and message echo/broadcast.

- [ ] **Step 4: Run all tests to verify they pass**

Run: `go test -v -race ./...`
Expected: PASS with full suite coverage.

- [ ] **Step 5: Commit unit tests**

```bash
git add hub_test.go client_test.go
git commit -m "test: add unit tests for hub, client, and websocket handler"
```

---

### Task 2: Add GitHub Actions CI/CD Pipeline Workflow

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: Go repository files, `go.mod`, unit tests
- Produces: GitHub Actions CI workflow triggered on push and PR

- [ ] **Step 1: Create `.github/workflows/ci.yml`**

Workflow specification:
- Name: `CI` (or `Unit Tests`)
- On: `push` and `pull_request` (branches: `[ main, master ]` or any branch)
- Jobs:
  - `test`:
    - OS: `ubuntu-latest`
    - Steps:
      - Checkout repository: `actions/checkout@v4`
      - Setup Go: `actions/setup-go@v5` with `go-version-file: 'go.mod'` or `go-version: '1.25'`
      - Verify dependencies: `go mod verify`
      - Run tests with race detection and coverage: `go test -v -race -cover ./...`

- [ ] **Step 2: Validate the workflow file syntax**

Verify file exists, valid YAML structure, and valid actions schema.
Run yaml parser check or linting.

- [ ] **Step 3: Run the local test command matching CI**

Run: `go test -v -race -cover ./...`
Expected: PASS

- [ ] **Step 4: Commit CI workflow**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add GitHub Actions workflow for unit tests"
```
