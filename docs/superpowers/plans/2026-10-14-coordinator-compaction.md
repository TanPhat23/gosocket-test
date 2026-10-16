# Architectural Plan & Feasibility: Coordinator Auto-Compaction at 272,000 Active Tokens

**Status:** `BLOCKED` (Core primitives missing for per-turn active context measurement and caller-agent identity in hooks)  
**Date:** 2026-10-14  
**Target File:** `docs/superpowers/plans/2026-10-14-coordinator-compaction.md`  
**Host Version Inspected:** OpenCode V2 (`@opencode/plugin@2.0.23`, `@opencode/schema@2.0.23`, `@opencode/client@2.0.23`, `@opencode/protocol@2.0.23`)  
**Reference Installations:** `~/.config/opencode/plugins/project-search/node_modules/@opencode/*`  
**Documentation Base:** `https://opencode.ai/v2/docs/build/plugins/`

---

## 1. Executive Summary & Gate Status

The objective requested an architectural feasibility evaluation and implementation plan for an automatic compaction plugin triggering at **272,000 active tokens**, scoped **strictly to the current coordinator session** (`currentcoordinatoronly`, top-level root session where `parentID` is absent, excluding subagents like `backend`, `frontend`, `explore`, `quality`, etc.).

### Verdict: `BLOCKED`

The feasibility analysis reveals fundamental technical blockers in the authoritative `@opencode/plugin@2.0.23` and `@opencode/schema@2.0.23` SDK surfaces that make safe, faithful implementation of the requested behavior **impossible without inventing non-existent APIs or causing context corruption / runaway compaction storms**:

1. **Absence of Current Active Context Token Metric in Available APIs:**
   Neither `Session.Info` nor `session.usage.updated` exposes the *current turn active prompt context size*. `Session.Info.tokens` and `session.usage.updated` record cumulative session token spend (`input`, `output`, `reasoning`, `cache.read`, `cache.write`). While `session.step.ended` records the last step's usage, summing step tokens compounds lifetime accumulation and cache double-counting, whereas measuring active context requires either tokenizer inspection of assembled prompts or engine-level active window reporting. Neither exists as an un-gated, safe metadata field on `ctx.session`.
2. **Cannot Determine Active Calling Agent in Session Hooks:**
   In `@opencode/plugin@2.0.23`, `ctx.session.hook("prompt", ...)` receives `SessionPrompt` (`{ sessionID, messageID, prompt, delivery, metadata }`). It does **not** expose `agent`. `Session.Info` (`ctx.session.get`) contains an `agent` field, but it is optional and reflects the session-level default agent configured at creation, not the dynamic agent dispatched on child turns or subagents running within or under that hierarchy without manual state tracking.
3. **No Safe Synchronous Pre-Turn Context Interception / Dynamic Scope:**
   While `ctx.session.compact({ sessionID })` is a valid client endpoint in the SDK (`SessionApi.compact`), invoking it asynchronously during an active turn or from an uncoordinated event stream creates race conditions with the LLM generation loop (single-flight conflicts, interleaving compaction during active execution).

Therefore, this document establishes the full factual findings, proves the unsupported primitives from authoritative code signatures, specifies the target architecture, and sets forth the gate requirements before any code may be authored.

---

## 2. Authoritative OpenCode V2 SDK Findings

All findings below were extracted directly from the installed, authoritative `@opencode/` 2.0.23 packages in `~/.config/opencode/plugins/project-search/node_modules/@opencode/`.

### 2.1 Hook Interfaces (`@opencode/plugin/dist/promise/session.d.ts`)

```typescript
export interface SessionPrompt {
    readonly sessionID: Session.ID;
    readonly messageID: SessionMessage.ID;
    prompt: Types.DeepMutable<PromptInput.Prompt>;
    metadata?: Record<string, unknown>;
    delivery: SessionInbox.Delivery;
}

export interface SessionContext extends SessionRequest {
    readonly agent: Agent.ID;
    tools: Record<string, {
        description: string;
        input: JsonSchema.JsonSchema;
    }>;
}

export interface SessionCompactionResult {
    summary: string;
    providerState?: SessionMessage.ProviderState;
    metadata?: Record<string, unknown>;
    tokens?: TokenUsage.Info;
}

export interface SessionCompaction extends SessionContext {
    /** Set to use this compaction and skip the model request. */
    result?: SessionCompactionResult;
}

export interface SessionHooks {
    readonly prompt: SessionPrompt;
    readonly context: SessionContext;
    readonly compaction: SessionCompaction;
    readonly generate: SessionGenerate;
    readonly title: SessionTitle;
    readonly "model.request": SessionModelRequest;
    readonly "http.request": SessionHttpRequest;
    readonly "http.response": SessionHttpResponse;
    readonly "experimental.ws.handshake": SessionWebSocketHandshake;
    readonly "experimental.ws.send": SessionWebSocketSend;
    readonly "experimental.ws.receive": SessionWebSocketReceive;
    readonly retry: SessionRetry;
}

export type SessionDomain = Pick<SessionApi, "create" | "get" | "remove" | "switchAgent" | "switchModel" | "prompt" | "generate" | "command" | "compact" | "synthetic" | "interrupt" | "update" | "move" | "wait" | "context"> & {
    readonly hook: ModelHooks<SessionHooks>;
};
```

**Key Factual Observations:**
- `ctx.session.hook("prompt", ...)` gives `SessionPrompt`. **Notice that `agent` is missing from `SessionPrompt`**.
- `ctx.session.hook("context", ...)` contains `readonly agent: Agent.ID`, but this hook is called when preparing LLM tools/context, **not** as a pre-prompt gate that can asynchronously pause or abort the turn to enqueue a host compaction. Mutating `SessionCompaction.result` only works when OpenCode *already* decided to run a compaction request.
- `SessionApi` contains `compact: (input: SessionCompactInput) => Promise<SessionInboxCompaction>`.
- V1-style names like `client.session.compact` or `session.next.compaction.*` are obsolete; the SDK uses `ctx.session.compact({ sessionID })` and event stream types `session.compaction.*`.

### 2.2 Session Data Structure (`@opencode/schema/dist/session.d.ts`)

```typescript
export declare const Info: Schema.Struct<{
    readonly id: Schema.brand<Schema.String, "SessionID">;
    readonly parentID: Schema.optional<Schema.brand<Schema.String, "SessionID">>;
    readonly projectID: Schema.brand<Schema.String, "Project.ID">;
    readonly agent: Schema.optional<Schema.brand<Schema.String, "Agent.ID">>;
    readonly model: Schema.optional<...>;
    readonly cost: Schema.brand<Schema.Finite, "Money.USD">;
    readonly tokens: Schema.Struct<{
        readonly input: Schema.Finite;
        readonly output: Schema.Finite;
        readonly reasoning: Schema.Finite;
        readonly cache: Schema.Struct<{
            readonly read: Schema.Finite;
            readonly write: Schema.Finite;
        }>;
    }>;
    readonly outcome: Schema.optional<Schema.Literals<["succeeded", "failed", "interrupted"]>>;
    readonly time: Schema.Struct<{
        readonly created: Schema.DateTimeUtc;
        readonly updated: Schema.DateTimeUtc;
        readonly idle: Schema.optional<Schema.DateTimeUtc>;
    }>;
    // ...
}>;
```

**Key Factual Observations:**
- Top-level root sessions have `parentID === undefined` (absent). Subagents have `parentID` populated with the parent session ID. This allows determining if a session is a root session.
- `tokens` on `Session.Info` records:
  $$\text{tokens} = \text{input} + \text{output} + \text{reasoning} + \text{cache.read} + \text{cache.write}$$
  This is the **lifetime cumulative token consumption** across all turns of the session, not the *active context length*. Triggering compaction at 272,000 on this number would cause a session with 10 small turns of 30k tokens to compact prematurely, even though its active context is under 40k tokens!

### 2.3 Event Stream Types (`@opencode/schema/dist/session-event.d.ts`)

```typescript
// session.step.ended
readonly type: Schema.Literal<"session.step.ended">;
readonly data: Schema.Struct<{
    readonly sessionID: string;
    readonly assistantMessageID: string;
    readonly finish: "stop" | "length" | "tool-calls" | "content-filter" | "error" | "unknown";
    readonly cost: MoneyUSD;
    readonly tokens: TokenUsageInfo;
}>;

// session.idle
readonly type: Schema.Literal<"session.idle">;
readonly data: Schema.Struct<{
    readonly sessionID: string;
}>;

// session.compaction.started / ended / failed
readonly type: Schema.Literal<"session.compaction.started">;
readonly type: Schema.Literal<"session.compaction.ended">;
readonly type: Schema.Literal<"session.compaction.failed">;
```

---

## 3. Detailed Blocker Analysis

| Blocker ID | Requirement | Authoritative V2 Reality | Status |
| :--- | :--- | :--- | :--- |
| **BLK-01** | Accurate Active Context Metric | `tokens` in `Session.Info` and `session.usage.updated` is cumulative spend. `tokens` in `session.step.ended` represents single-step generation input/output tokens. Active context window size is maintained inside provider driver engines and not exposed as a clean metadata scalar. Using cumulative spend causes false-positive triggers; summing step tokens causes double-count cache amplification. | **FATAL** |
| **BLK-02** | Coordinator Scoping (`currentcoordinatoronly`) | `SessionPrompt` in `ctx.session.hook("prompt")` does not contain `agent`. `Session.Info` contains `agent?: Agent.ID`, but this is only the initial session default. Subagents spawned as top-level sessions or dynamically switched sessions cannot be distinguished solely by `parentID` without inspecting agent configuration, which can be modified or omitted. | **MAJOR** |
| **BLK-03** | Safe In-Flight Compaction Gating | In OpenCode V2, compaction is an asynchronous inbox item (`ctx.session.compact({ sessionID })`). If triggered while a turn is active, it runs into concurrency hazards with active tools or generation loops unless delayed until `session.idle`. But triggering at `session.idle` means the *next* turn may start before compaction finishes unless single-flight queued. | **MODERATE** |

---

## 4. Scope & Permission Architecture (When Blockers Resolved)

### 4.1 Strict Session & Role Resolution
To guarantee that subagents (`backend`, `frontend`, `delivery`, `security`, `quality`, `explore`) are **never** compacted by this plugin:
1. **Target Session Allowlist:** Do not silently install globally across all coordinator sessions. The plugin must require an explicit, user-configured `targetSessionID` or accept a registration handshake where the user chooses the active session ID.
2. **Root Verification:** Ensure `session.parentID === undefined`. If `parentID` is present, reject immediately.
3. **Role Verification:** Query `session = await ctx.session.get({ sessionID })`. Validate that `session.agent === "coordinator"`. If `session.agent` is absent or not `"coordinator"`, reject.

### 4.2 Threshold Semantics (Exact 272,000 Tokens)
The user specified a 272,000 token threshold. The gating engine logic must strictly adhere to:
- Tokens $\le 271,999$: **No Action** (pass through).
- Tokens $== 272,000$: **Trigger Compaction** (exact boundary).
- Tokens $\ge 272,001$: **Trigger Compaction** (overflow boundary).

### 4.3 Native Precedence & Clean Watermarking
- **Defer to Native:** If built-in compaction is enabled (`config.compaction.auto === true` or provider-level native compaction triggers earlier), this plugin must defer and not double-compact.
- **Single-Flight & Deduplication:**
  - Watermark tracking: `lastCompactedMessageID` or `lastCompactedTokenCount`.
  - Once compaction is requested via `ctx.session.compact`, state transitions to `PENDING`.
  - While `PENDING`, subsequent triggers are discarded (single-flight deduplication).
  - On `session.compaction.ended`: state returns to `IDLE`, watermark is updated.
  - On `session.compaction.failed`: state returns to `IDLE` with bounded exponential backoff (maximum 3 retries, then disabled for the session to prevent storms).
- **Privacy and Metadata Only:** Zero message/prompt text logging or exporting. Only numeric token counts, session IDs, and timestamps are processed.

---

## 5. Proposed Isolated Staging & Verification Plan

### 5.1 Small Package Stage
The proposed plugin will reside in `/tmp/opencode/coordinator-compaction-stage`:
```
/tmp/opencode/coordinator-compaction-stage/
├── package.json
├── index.js
├── lib/
│   ├── gate.js
│   └── watermark.js
└── test/
    ├── gate.test.js
    └── watermark.test.js
```

### 5.2 Test Specifications (TDD, Node.js Built-in Test Runner)
No synthetic massive 272k LLM prompts. Test suites will test pure discrete logic:
1. `gate.test.js`:
   - `evalGate(271999)` returns `false`.
   - `evalGate(272000)` returns `true`.
   - `evalGate(272001)` returns `true`.
   - Rejects if `parentID` is present.
   - Rejects if agent is `backend` or `frontend`.
   - Rejects if sessionID $\ne$ `targetSessionID`.
2. `watermark.test.js`:
   - Enqueues exactly one compaction call when crossing threshold.
   - Suppresses duplicate calls while `status === "pending"`.
   - Resets state on `session.compaction.ended`.
   - Halts after 3 consecutive `session.compaction.failed` events.

### 5.3 Execution Commands (Dry-Run / Non-Mutating)
```bash
# Staging test execution (future, once approved and unblocked)
cd /tmp/opencode/coordinator-compaction-stage && node --test test/*.test.js
```

---

## 6. Security & Requirements Resolution Matrix

| Requirement | Proposed Solution / Resolution | Verification Standard |
| :--- | :--- | :--- |
| **Scoped to Coordinator Only** | Dual check: `parentID == null` AND `agent == "coordinator"` AND optional `targetSessionID`. | Unit test with subagent fixture sessions. |
| **No Global Silently Installed Hooks** | Plugin requires explicit `options.targetSessionID` or user opt-in config. | Unit test verifying inactive sessions are ignored. |
| **Active Context Token Threshold** | **BLOCKED:** Must obtain verified engine active context hook or tokenizer scalar from OpenCode team. | Awaiting upstream OpenCode API resolution. |
| **No Prompt Content Reading** | Metadata-only inspections (`tokens`, `sessionID`, `messageID`). Zero access to `prompt.text` or message body. | Code inspection; no `prompt` content inspection in source. |
| **No Storms / Watermark Dedupe** | Single-flight state machine with state `IDLE -> PENDING -> IDLE` and max 3 failure retries. | Watermark test asserting 1 dispatch across 10 rapid event firings. |
| **Preserve Existing Permissions** | Zero edits to `~/.config/opencode/opencode.jsonc` permissions or existing plugins. | `git status` clean in root worktree; zero host config modifications. |

---

## 7. Conclusion & Next Steps

This plan is classified as **`BLOCKED`**. Implementation cannot proceed until OpenCode core exposes either:
1. An active prompt window token metric on `SessionPrompt` or `Session.Info`, OR
2. An upstream hook specifically designed for pre-turn coordinator context compaction gating.

A copy of this architectural design document has been written to `docs/superpowers/plans/2026-10-14-coordinator-compaction.md` in adherence to the objective boundary. No configuration files, packages, or background daemons were modified.
