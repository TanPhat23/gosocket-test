# Project Runtime Idle Shutdown Specification

## Goal

Reclaim memory from warmed project runtime containers when they have been idle, without stopping a runtime while a task is executing or deleting project state.

## Behavior

- The application default idle timeout is 15 minutes, configured by `ATM_PROJECT_RUNTIME_IDLE_TIMEOUT` using Go duration syntax.
- `0s` disables idle shutdown. Invalid and negative values fail configuration loading; positive values below 30 seconds are rejected to prevent accidental rapid stop/start loops.
- Starting or accessing a project runtime refreshes its idle timer. A task execution holds an activity lease for its complete `RunTask` lifetime; idle shutdown cannot stop a runtime with an active lease.
- Arm the timer only after container startup/readiness has completed. Its callback captures the exact container ID and route generation and verifies both under lock before stopping.
- When the timeout expires, the manager gracefully stops the exact tracked container, releases its in-memory route/gateway lease only after Docker confirms stop (or reports not found), and retains persistent project volumes and task data. The runtime can be started again through existing runtime/task flows.
- Manual stop cancels the timer. If Docker stop fails for a still-existing container, keep its route and gateway lease and re-arm a delayed retry; do not report it as stopped.
- The timeout applies to runtimes tracked by the current control-plane process. Runtime discovery/reaping after a control-plane restart is out of scope for this first version.

## Out of Scope

- Changing OpenCode/MCP configuration or task MCP availability.
- Changing task concurrency defaults.
- Managing host applications such as Firefox or Discord.
- Removing containers, volumes, worktrees, task records, or OpenCode state.

## Acceptance Criteria

1. Default configuration yields a 15-minute timeout; `0s` disables it; malformed, negative, and positive sub-30-second values are rejected.
2. An inactive tracked runtime is stopped after its configured timeout.
3. Runtime route access resets the timeout.
4. A runtime with an active task lease is not stopped; releasing its last lease starts a fresh timeout.
5. A callback for an old container ID/generation cannot stop or clean up a replacement container.
6. Manual stop cancels any pending timer and does not cause a second stop; a failed stop preserves route and gateway lease state.
7. Idle stop retains project data and releases the gateway lease only after stop succeeds or the container is confirmed absent.
8. Timeout zero creates no idle timer and preserves current lifecycle behavior.

## Safety Constraints

- All timer/map/route state transitions are synchronized; an idle-stop callback must recheck timeout and active leases atomically before stopping.
- Task activity release is deferred so success, error, and cancellation all release the lease.
- Automatic stop does not remove the container or volumes.
- Existing MCP configuration and task concurrency behavior remain unchanged.
