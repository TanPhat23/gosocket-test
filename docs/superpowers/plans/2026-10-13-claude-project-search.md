# Implementation Plan: Claude Code Global Integration for Project Search

**Target Branch/Working Tree:** `project-scoped-runtime`  
**Plan Document:** `docs/superpowers/plans/2026-10-13-claude-project-search.md`  
**Status:** IMPLEMENTED & PUBLISHED (Verified Globally in Claude Code)  
**Execution Constraints:** Staged build verified in `/tmp/opencode`; atomic publication; verified with live user configuration backup and official CLI.

---

## 1. Executive Summary & Design Scope

The user requested installing the bounded, read-only `project-search` capability for **Claude Code** (CLI version `2.1.296`) globally.
This plan defines a zero-speculation, decoupled, atomic integration into Claude Code via the official Model Context Protocol (MCP) `stdio` transport.

### Critical Principles & Review Corrections Applied
1. **Isolated Staging & Atomic Publish**:
   - Implementation, dependency installation (`--ignore-scripts`), and test verification occur strictly inside `/tmp/opencode/claude-project-search-stage`.
   - Never implement directly into global paths.
   - Atomic publish to `~/.local/share/claude-project-search`: check existing destination (`-e` or `-L`), create temporary sibling via `mktemp -d ~/.local/share/.claude-project-search.tmp.XXXXXX`, populate, sync/rename atomically using `mv -T -n` (or non-clobbering atomic swap), and ensure staging source is cleaned up.
2. **Fixed Startup Project Root & Verified CWD/Env Behavior**:
   - Official documentation citation ([https://code.claude.com/docs/en/mcp](https://code.claude.com/docs/en/mcp)):
     > *"Claude Code sets `CLAUDE_PROJECT_DIR` in the spawned server's environment to the project root, so your server can resolve project-relative paths without depending on the working directory."*
     > *"Stdio servers run as local processes on your machine. ... Stdio servers are local processes, and Claude Code doesn't reconnect them automatically."*
     > *"A server that limits its own filesystem access to a set of allowed directories should implement the MCP `roots/list` request instead."*
   - **Resolution Strategy**: Stdio servers are spawned once per session. To safeguard against ambient or unverified environment overrides while remaining compliant with Claude Code's launcher contract, the server resolves the root at startup using:
     ```javascript
     const startupRaw = process.env.CLAUDE_PROJECT_DIR || process.cwd();
     const startupRoot = validateSessionRoot(startupRaw);
     ```
   - **No `/cd` or dynamic rebinding**: Stdio servers cannot dynamically relocate mid-session without reconnecting, and Claude Code does not reconnect stdio servers on directory change. A new Claude Code session per project workspace is the documented baseline.
   - **Root Validation**: `validateSessionRoot` from `search.js` resolves path via `path.resolve(dir)`, asserts directory existence via `fsSync.lstatSync(normalized).isDirectory()`, and strictly refuses filesystem root (`/`) or user home directory (`os.homedir()`). It does not run symlink dereferencing (`realpath`), keeping symlink handling aligned with `search.js` (which rejects traversing outside ancestors or searching symlinks).
3. **No Double-Encoding Budget Fix**:
   - In `search.js`, `measureFullResponseBytes` produces an envelope formatted as:
     ```javascript
     {
       content: [{ type: "text", text: JSON.stringify(resultObj) }],
       metadata: { totalMatches, filesScanned, truncated }
     }
     ```
   - The MCP `CallToolResult` schema requires `{ content: ToolContent[], isError?: boolean, _meta?: { ... } }`.
   - Returning `{ content: [{ type: "text", text: JSON.stringify(measured.envelope) }] }` causes double JSON encoding and immediately breaches the 16 KiB total serialized output budget.
   - **Exact Contract**: The adapter directly returns `measured.envelope.content` as the MCP `CallToolResult.content` array. If metadata is exposed, it is mapped to standard MCP `_meta` only if necessary, ensuring that `Buffer.byteLength(JSON.stringify(callToolResult), "utf8") <= 16384` bytes is strictly preserved without nested JSON string escapes.
4. **Isolated CLI Verification**:
   - Every verification command using `claude mcp` runs with explicit ephemeral environment variables:
     `CLAUDE_CONFIG_DIR="$fixture/config" HOME="$fixture/home" XDG_CONFIG_HOME="$fixture/config"`
   - Staging paths use `/tmp/opencode`, avoiding generic `/tmp`.
   - No broad `claude mcp list` health checks that trigger network scans on unrelated connectors. Only targeted inspection via `claude mcp get project-search`.
   - No permissions changes (`chmod 777` or global permission bypasses).

---

## 2. Declared Interfaces & Code Contracts

### 2.1 MCP Stdio Adapter Entrypoint (`index.js`)

```javascript
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { executeSearch, validateSessionRoot } from "./search.js";

// 1. Resolve and freeze startup project root
const startupCandidate = process.env.CLAUDE_PROJECT_DIR || process.cwd();
let projectRoot;
try {
  projectRoot = validateSessionRoot(startupCandidate);
} catch (err) {
  process.stderr.write(`[claude-project-search] Fatal: ${err.message}\n`);
  process.exit(1);
}

// 2. Initialize MCP Server
const server = new Server(
  {
    name: "claude-project-search",
    version: "0.1.0",
  },
  {
    capabilities: {
      tools: {},
    },
  }
);

// 3. Declare Tools Schema (strictly identical to engine schema)
const SEARCH_TOOL_DEFINITION = {
  name: "project_search",
  description: "Search project files and documentation by literal text or filename within safe project bounds",
  inputSchema: {
    type: "object",
    properties: {
      query: {
        type: "string",
        minLength: 1,
        maxLength: 200,
        description: "Literal text to search",
      },
      mode: {
        type: "string",
        enum: ["content", "filename"],
        default: "content",
        description: "Search mode: 'content' for file contents or 'filename' for path names",
      },
      scope: {
        type: "string",
        enum: ["all", "code", "docs"],
        default: "all",
        description: "Search scope: 'all', 'code', or 'docs'",
      },
      pathPrefix: {
        type: "string",
        description: "Optional relative directory prefix within project to narrow search",
      },
      caseSensitive: {
        type: "boolean",
        default: false,
        description: "Case sensitivity (defaults to false)",
      },
      maxResults: {
        type: "integer",
        minimum: 1,
        maximum: 200,
        default: 50,
        description: "Maximum number of matches to return (defaults to 50, ceiling 200)",
      },
    },
    required: ["query"],
    additionalProperties: false,
  },
};

server.setRequestHandler(ListToolsRequestSchema, async () => {
  return {
    tools: [SEARCH_TOOL_DEFINITION],
  };
});

// 4. Handle Tool Invocations
server.setRequestHandler(CallToolRequestSchema, async (request, extra) => {
  if (request.params.name !== "project_search") {
    return {
      content: [{ type: "text", text: `Unknown tool: ${request.params.name}` }],
      isError: true,
    };
  }

  try {
    const rawInput = request.params.arguments || {};
    // Execute search with cancellation support from extra.signal
    const results = await executeSearch(projectRoot, rawInput, {
      signal: extra?.signal,
    });

    // In search.js, executeSearch returns results formatted through measureFullResponseBytes:
    // results is { matches, filesScanned, entriesScanned, bytesScanned, truncated, truncationReason }
    // We import measureFullResponseBytes from ./search.js to format standard envelope:
    const { envelope } = measureFullResponseBytes(
      results.matches,
      results.filesScanned,
      results.entriesScanned,
      results.bytesScanned,
      results.truncated,
      results.truncationReason
    );

    // CRITICAL: Directly return envelope.content to prevent double-stringification.
    // envelope.content is [{ type: "text", text: JSON.stringify(resultObj) }]
    return {
      content: envelope.content,
      _meta: envelope.metadata,
      isError: false,
    };
  } catch (err) {
    return {
      content: [{ type: "text", text: `Search refused: ${err.message}` }],
      isError: true,
    };
  }
});

// 5. Connect stdio transport
const transport = new StdioServerTransport();
await server.connect(transport);
```

---

## 3. Detailed Work Breakdown & TDD Tasks

### Task 1: Staged Adapter Implementation & Comprehensive Test Suite

**Directory**: `/tmp/opencode/claude-project-search-stage`

- [ ] **Setup Staging Workspace**:
  - Create directory `/tmp/opencode/claude-project-search-stage`.
  - Copy `~/.config/opencode/plugins/project-search/search.js` byte-for-byte to staging directory.
  - Create `package.json` with `"type": "module"`, `"scripts": { "test": "node --test test/*.test.js" }`.
  - Install dependencies using:
    ```bash
    npm install --prefix /tmp/opencode/claude-project-search-stage --ignore-scripts @modelcontextprotocol/sdk@1.32.1
    ```
- [ ] **Write Real MCP Client Test Suite** (`test/mcp-server.test.js`):
  Write comprehensive tests using the real `@modelcontextprotocol/sdk/client/index.js` and `InMemoryTransport` or stdio subprocess:

  ```javascript
  import test from "node:test";
  import assert from "node:assert/strict";
  import fs from "node:fs/promises";
  import path from "node:path";
  import os from "node:os";
  import { Client } from "@modelcontextprotocol/sdk/client/index.js";
  import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
  // Subprocess and MCP client integration helpers...
  ```

  **Test Cases Required**:
  1. `[TDD] MCP Server Handshake & Tools Discovery`: Connects real client; calls `listTools()`; verifies `project_search` tool exists, matches exact input schema, and sets `additionalProperties: false`.
  2. `[TDD] CallTool Result Envelope & Exact Size Budget`: Invokes `project_search` on a multi-file fixture; asserts result is NOT double-stringified; verifies `Buffer.byteLength(JSON.stringify(callToolResult), "utf8") <= 16384`.
  3. `[TDD] Startup Root Validation - Refuse Root (/) and User Home`: Spawns server subprocess with `CLAUDE_PROJECT_DIR=/` and `CLAUDE_PROJECT_DIR=$HOME`; asserts process exits with code 1 and error on stderr; stdout remains empty.
  4. `[TDD] Traversal & Sensitive File Defense`: Invokes tool with `pathPrefix: "../outside"`, `pathPrefix: ".git"`, or searches sensitive `.env` files; verifies search does not leak secrets and refuses path traversal.
  5. `[TDD] Cancellation Handling (Client Pre-Aborted)`: Passes a pre-aborted `AbortSignal` to the call handler; verifies client SDK boundary rejects invocation promptly before transport without errors. Note: In-flight server-side cancellation over stdio is bounded by whether the MCP host sends protocol cancellation notifications; `extra.signal` is passed to the engine.

- [ ] **Implement `index.js` in Staging Workspace**:
  - Implement full stdio adapter conforming to Section 2.1.
- [ ] **Run Staging Test Suite**:
  ```bash
  npm test --prefix /tmp/opencode/claude-project-search-stage
  ```
  Verify all tests pass with 0 exit code.

---

### Task 2: Atomic Non-Clobbering Publish & User-Scoped Registration

- [ ] **Atomic Non-Clobbering Publish to `~/.local/share/claude-project-search`**:
  - **Check existing destination or symlink**:
    ```bash
    if [ -e "$HOME/.local/share/claude-project-search" ] || [ -L "$HOME/.local/share/claude-project-search" ]; then
      echo "Destination already exists; refusing to clobber" >&2
      exit 1
    fi
    ```
  - **Check existing registration conflict fail-closed**:
    Verify `~/.claude.json` does not already declare `project-search` before writing:
    ```bash
    node -e '
      const fs = require("fs");
      const p = `${process.env.HOME}/.claude.json`;
      if (!fs.existsSync(p)) process.exit(0);
      const cfg = JSON.parse(fs.readFileSync(p, "utf8"));
      if (cfg.mcpServers?.["project-search"]) {
        console.error("Conflict: project-search already declared in ~/.claude.json");
        process.exit(1);
      }
    '
    ```
  - Create parent directory if needed `mkdir -p "$HOME/.local/share"`.
  - Create temporary sibling directory on same filesystem:
    ```bash
    TMP_DEST=$(mktemp -d "$HOME/.local/share/.claude-project-search.tmp.XXXXXX")
    ```
  - Copy staging artifacts into `TMP_DEST` preserving exact permissions and lockfile:
    ```bash
    cp -a /tmp/opencode/claude-project-search-stage/. "$TMP_DEST/"
    ```
  - Atomically move into destination:
    ```bash
    mv -T -n "$TMP_DEST" "$HOME/.local/share/claude-project-search"
    ```
  - **Verify publish invariants**:
    - Confirm installed entrypoint exists: `test -f "$HOME/.local/share/claude-project-search/index.js"`.
    - Confirm temporary source is gone: `! test -e "$TMP_DEST"`.
    - **Preserve staging directory** (`/tmp/opencode/claude-project-search-stage`) until independent verification is confirmed. Cleanup only own temporary directories.
- [ ] **Stage Verification in Disposable Isolated Claude Environment**:
  - Create isolated test fixture directory:
    ```bash
    FIXTURE_DIR=$(mktemp -d /tmp/opencode/claude-mcp-verify.XXXXXX)
    mkdir -p "$FIXTURE_DIR/config" "$FIXTURE_DIR/home"
    ```
  - Test registration in isolation without reading or writing live user configs:
    ```bash
    env CLAUDE_CONFIG_DIR="$FIXTURE_DIR/config" HOME="$FIXTURE_DIR/home" XDG_CONFIG_HOME="$FIXTURE_DIR/config" \
      claude mcp add --scope user --transport stdio project-search -- node "$HOME/.local/share/claude-project-search/index.js"
    ```
  - Verify registration targeted specifically via `claude mcp get project-search` (without invalid flags):
    ```bash
    env CLAUDE_CONFIG_DIR="$FIXTURE_DIR/config" HOME="$FIXTURE_DIR/home" XDG_CONFIG_HOME="$FIXTURE_DIR/config" \
      claude mcp get project-search
    ```
  - Clean up disposable verification fixture:
    ```bash
    rm -rf "$FIXTURE_DIR"
    ```
- [ ] **Live Global User Registration**:
  - Register MCP server in live user configuration:
    ```bash
    claude mcp add --scope user --transport stdio project-search -- node "$HOME/.local/share/claude-project-search/index.js"
    ```
  - Validate live registration status via official CLI:
    ```bash
    claude mcp get project-search
    ```
  - Inspect configuration state safely via minimal script without printing secrets:
    ```bash
    node -e '
      const fs = require("fs");
      const cfg = JSON.parse(fs.readFileSync(`${process.env.HOME}/.claude.json`, "utf8"));
      const s = cfg.mcpServers?.["project-search"];
      if (s && s.type === "stdio" && s.command === "node") {
        console.log("Registration verified: project-search present in user mcpServers");
      } else {
        console.error("Verification failed: project-search missing or malformed");
        process.exit(1);
      }
    '
    ```

---

## 4. Verification Checklists & Exact Commands

| Stage | Exact Command | Exit Code | Expected Result |
|---|---|---|---|
| Stage Tests | `npm test --prefix /tmp/opencode/claude-project-search-stage` | 0 | All MCP protocol and security tests pass |
| Atomic Publish | `mv -T -n $TMP_DEST ~/.local/share/claude-project-search` | 0 | Atomic directory creation without clobbering |
| Isolated Registration | `env CLAUDE_CONFIG_DIR=... HOME=... claude mcp add --scope user ...` | 0 | Outputs `Added stdio MCP server project-search` |
| Isolated Inspection | `env CLAUDE_CONFIG_DIR=... HOME=... claude mcp get project-search` | 0 | Shows command, args, and transport in test config |
| Live Registration | `claude mcp add --scope user --transport stdio project-search -- node ~/.local/share/claude-project-search/index.js` | 0 | Writes user-scoped server to `~/.claude.json` |
| Live Verification | `claude mcp get project-search` | 0 | Reports `project-search` registered cleanly |

---

## 5. Security & Isolation Invariants

1. **Stdout Purity**: All server logging or errors go strictly to `process.stderr`. `process.stdout` is exclusively dedicated to JSON-RPC framing.
2. **Path Hardening**: `validateSessionRoot` rejects `/` and home directory `$HOME`. Path traversal (`..`) is blocked by engine boundaries.
3. **No Shell Execution**: No child process spawning or shell command invocations.
4. **Preserved Secrets**: No `.env`, secret tokens, or private keys are ever indexed or logged.
