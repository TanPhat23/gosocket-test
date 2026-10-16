# OpenCode Project-Scoped Search Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-orchestration to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a new, independent developer tooling plugin for OpenCode (`project_search`) enabling agents to execute bounded, read-only literal searches across both repository code and project documentation within strict project confinement and deterministic resource budgets.

**Status: IMPLEMENTED & VERIFIED IN STAGING** at `/tmp/opencode/project-search-stage` (ready for quality sign-off; global installation deferred pending approval).
- **Security Authorization:** User formally approved Choice A (autonomous read-only host filesystem access bounded by strict internal confinement, root rejection, and credential exclusion).
- **Scope Alignment:** User corrected target scope to GLOBAL (`~/.config/opencode/plugins/project-search`), resolving project roots dynamically per session via `ctx.session.get({ sessionID })`.
- **Runtime Discovery & Loader Verified:** Verified on installed `opencode v2.0.23` in an isolated disposable environment that directory package plugins under `~/.config/opencode/plugins/<name>/` (containing `package.json` and `index.js`) are automatically discovered and loaded during server startup, registering `project_search_search` in the active tool registry and successfully searching the real workspace marker while refusing missing session locations.
- **Fail-Closed Safe Publication:** Installation procedure fails closed if the target directory or symlink exists (`-e || -L`), staging into an isolated sibling directory via `mktemp -d` and atomically publishing with `mv -T -n`.

---

## Security Preflight & User Authorization Recorded

### 1. Permission Mediation Reality vs. Boundary Defense
- **Authoritative Findings in SDK (@opencode/plugin@2.0.23):**
  - Tool registration accepts `options: { namespace?: string, permission?: string, codemode?: boolean, pinned?: boolean }` (from `@opencode/schema/tool`).
  - `ctx.permission` exposes `list`, `get`, `reply`, and hook `evaluate` (`PermissionHooks: { evaluate: PermissionEvaluation }`), which evaluates server-level permission requests. It does **not** provide a plugin tool execution hook to prompt the user interactively or intercept Node.js filesystem calls.
  - Plugins execute as trusted host JavaScript/TypeScript in the server's Node/Bun process.
  - OpenCode provides **no built-in host sandbox or automatic filesystem mediation** for custom Node `node:fs` calls.
- **Fail-Closed Stance & User Authorization Recorded:**
  - Hardcoded filename denylists and root checks are internal defensive boundaries, **not** equivalent to OpenCode permission mediation.
  - User explicitly selected and authorized **Choice A (Autonomous Read-Only Tool within Strict Confinement)**: `project_search` operates as an unprompted, read-only host tool confined to the session project root with strict hardcoded exclusions and resource bounds. No native permission sandbox is falsely claimed.

### 2. Cross-Session & Workspace Relocation Refusal
- On every invocation of `execute(rawInput, context)`:
  - Dynamically fetches `const session = await ctx.session.get({ sessionID: context.sessionID })`.
  - Rejects missing sessions, missing `session.location.directory`, broad roots (`/`, `os.homedir()`), and non-directories.
  - Error messages returned to the model/agent are generic (`"Search refused: invalid session workspace"`) and **never expose host absolute paths**.

### 3. Filesystem Traversal, Symlinks, Race Conditions & Hardlinks Caveat
- **Path Resolution & Normalization:** Target prefixes are resolved via `path.resolve(sessionRoot, prefix)`. Boundary verification requires `resolved === sessionRoot || resolved.startsWith(sessionRoot + path.sep)`. Rejects null bytes (`\0`) and traversal (`..`).
- **Ancestor Verification:** Every directory component from `sessionRoot` down to the target is verified to ensure no intermediate symlinks and no ignored entries (like `.git`).
- **Entry Skipping:** Any file or directory named `.git`, `.hg`, `.svn`, `node_modules`, `vendor`, `.venv`, `target`, `dist`, `build` is skipped **before reading**, preventing leaks from `.git` worktree pointer files.
- **File Descriptor Defense:** Files are opened with `O_RDONLY | O_NOFOLLOW | O_NONBLOCK`, verified with `fstat.isFile()`, and inspected for null bytes in the first 512 bytes to reject binaries and FIFOs.
- **Hardlinks & Race Caveat:** Realpath and `O_NOFOLLOW` mitigate symlink swaps. Directory replacement races across concurrent host processes cannot be completely eliminated in user space on shared filesystems. The security boundary assumes a stable, non-adversarial local project checkout.

### 4. Hardcoded Exclusion List (Zero Arbitrary Secrets Guarantee)
The plugin explicitly ignores:
- Version control metadata: `.git`, `.hg`, `.svn` (both directories and worktree pointer files).
- Dependency and build outputs: `node_modules`, `vendor`, `.venv`, `target`, `dist`, `build`.
- Secret and credential files:
  - Environments: `.env`, `.env.*` (e.g., `.env.local`, `.env.production`).
  - Private keys and certificates: `*.pem`, `*.key`, `*.crt`, `*.cert`, `*.pfx`, `*.p12`, `*.pkcs12`.
  - SSH keys: `id_rsa`, `id_ed25519`, `id_dsa`, `id_ecdsa` (and corresponding `.pub` or known variants).
  - Credentials & Package auth: `credentials.json`, `*service-account*.json`, `.npmrc`, `.netrc`.
- *Note:* This denylist does not guarantee complete secret redaction for embedded credentials inside arbitrary source files.

---

## Architecture & Interfaces

### 1. Architecture Overview
```
OpenCode Server (Host Node/Bun runtime)
  └── ~/.config/opencode/plugins/project-search/
        ├── index.js               (OpenCode V2 Plugin adapter)
        ├── search.js              (Pure Node.js traversal & literal search engine)
        ├── test/                  (Native unit, boundary, and loader smoke tests)
        └── package.json           (Self-contained package pinned to @opencode/plugin@2.0.23)
```

### 2. Search Engine Budget Ceiling
- Fixed deadline: 5,000 ms (5s).
- Max single file size: 1 MiB (1,048,576 bytes).
- Max aggregate read: 10 MiB (10,485,760 bytes).
- Max output budget: 16 KiB (16,384 bytes) calculated on the **entire serialized outer tool envelope**.
- Max scan files: 5,000 files; max directory entries: 10,000 entries.
- Results default: 50, ceiling: 200. Max snippet chars: 200.

---

## Verification Evidence on Installed OpenCode v2.0.23

### 1. Automated Test Suite (18/18 Passing)
Executed via `npm test` in `/tmp/opencode/project-search-stage`:
```bash
cd /tmp/opencode/project-search-stage && npm test
```
- **Exit Status:** `0`
- **Output:**
  ```text
  ▶ OpenCode Plugin Adapter TDD
    ✔ Plugin definition exports valid OpenCode plugin shape
    ✔ Plugin registers project_search namespace and search tool
  ✔ OpenCode Plugin Adapter TDD
  ▶ Isolated OpenCode v2.0.23 Discovery and Loader Verification
    ✔ OpenCode server discovers staged plugin, executes real search engine on workspace, and verifies missing session refusal
  ✔ Isolated OpenCode v2.0.23 Discovery and Loader Verification
  ▶ Project Search Engine TDD
    ✔ validateSessionRoot rejects root, home, missing, or non-directory
    ✔ executeSearch isolates session roots and prevents cross-project leakage
    ✔ executeSearch rejects pathPrefix escapes, null bytes, and traversal
    ✔ executeSearch rejects pathPrefix targeting ignored entries or symlink ancestors
    ✔ executeSearch ignores .git worktree file in both content and filename modes
    ✔ executeSearch basic literal search finds matches in code and docs
    ✔ executeSearch scope: 'docs' returns only documentation files
    ✔ executeSearch scope: 'code' returns only code files
    ✔ executeSearch filename mode matches file names instead of content
    ✔ executeSearch strictly skips sensitive files and keys in both modes
    ✔ executeSearch skips symlinks and non-regular files (FIFOs)
    ✔ executeSearch skips binary files and oversized files (> 1 MiB)
    ✔ executeSearch respects maxResults and sets truncationReason
    ✔ executeSearch strictly enforces 16 KiB total serialized output budget
    ✔ executeSearch respects AbortSignal (cancellation)
  ✔ Project Search Engine TDD
  ℹ tests 18, suites 3, pass 18, fail 0
  ```

### 2. Real Staged Artifact Execution Proof in OpenCode v2.0.23 Server
Confirmed in `test/opencode-loader-smoke.test.js`:
- OpenCode boots in temporary disposable `HOME` with isolated XDG directories.
- Staged plugin directory under `.config/opencode/plugins/project-search` is automatically discovered by OpenCode's loader.
- Wrapper imports actual staged `index.js` and `search.js` logic and forwards `setup(ctx)`.
- `ctx.tool.transform(...)` is executed and tool `project_search_search` is registered in `ctx.tool.list()`.
- Real search execution against workspace marker finds `REAL_WORKSPACE_SEARCH_MARKER_CONTENT` in `README.md`.
- Real session validation refuses execution when session location is missing with `"Search refused: invalid session workspace (missing session location)"`.

---

## Safe Non-Clobbering Installation Procedure (Pending Quality Sign-Off)

> **Notice:** Do NOT install globally until quality sign-off is granted.

```bash
set -euo pipefail

DEST_DIR="$HOME/.config/opencode/plugins/project-search"

# 1. Fail closed if destination exists or is a dangling symlink
if [ -e "$DEST_DIR" ] || [ -L "$DEST_DIR" ]; then
  echo "Error: Installation target $DEST_DIR already exists or is a symlink. Refusing to overwrite." >&2
  exit 1
fi

mkdir -p "$HOME/.config/opencode/plugins"

# 2. Stage into a unique isolated sibling directory using mktemp -d
TMP_STAGE=$(mktemp -d "$HOME/.config/opencode/plugins/.project-search-stage.XXXXXX")
cleanup() {
  if [ -d "$TMP_STAGE" ]; then
    rm -rf "$TMP_STAGE"
  fi
}
trap cleanup EXIT INT TERM

# 3. Copy staged contents into temporary directory
cp -r /tmp/opencode/project-search-stage/. "$TMP_STAGE/"

# 4. Atomically publish without clobbering (-T treats dest as normal file, -n avoids overwrite)
mv -T -n "$TMP_STAGE" "$DEST_DIR"

# 5. Verify source was moved and target now exists
if [ -d "$TMP_STAGE" ] || [ ! -d "$DEST_DIR" ]; then
  echo "Error: Atomic move failed or target was not created cleanly." >&2
  exit 1
fi

echo "Successfully installed project-search plugin to $DEST_DIR"
```
