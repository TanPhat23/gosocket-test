# Kanban Task Side-Peek Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a keyboard-friendly Kanban task preview sheet while preserving the existing full task inspector behavior.

**Architecture:** Each `KanbanLane` owns the currently previewed task and its already-filtered `taskIds`. A new `TaskPeekSheet` renders a concise overview, lane navigation controls, scoped `j`/`k` shortcuts, and a full-inspector handoff. `TaskCard` keeps its current click-to-inspect action and gains a separate visible preview button.

**Tech Stack:** React 19, TypeScript, Zustand task store, existing Base UI Sheet/Dialog components, Tailwind CSS, Vitest, Testing Library, Playwright for browser verification.

**Spec:** `docs/superpowers/specs/2026-10-20-task-side-peek.md`

## Global Constraints

- Task-card body clicks continue to open the existing full inspector.
- Preview is a separate, always-visible native button; it must not rely on hover.
- Navigation is limited to the lane's current filtered task order.
- `j`/`k` shortcuts are active only while the peek is open and ignore editable targets and modifier keys.
- The peek uses existing Sheet/Dialog primitives; do not add a dependency.
- At mobile widths the sheet is full-screen; desktop uses a right-side panel with visible board context.
- Do not change API/backend behavior, task mutations, task ordering, or filters.
- Preserve existing dirty runtime/config work. Baseline captured at `/tmp/opencode/agent-task-manager-ux-baseline.patch`; do not modify out-of-scope files.
- Do not commit.

## File Structure

- `web/src/features/tasks/components/TaskCard.tsx`: preserve current card click and add accessible preview action.
- `web/src/features/tasks/components/KanbanBoard.tsx`: pass the lane's filtered IDs and preview callback; own the per-lane peek state.
- `web/src/features/tasks/components/TaskPeekSheet.tsx` (new): render task summary, navigation, shortcut handling, and inspector handoff using existing Sheet primitives.
- `web/src/features/tasks/components/TaskPeekSheet.test.tsx` (new): cover task content, navigation, keyboard scope, closing, and full-inspector handoff.
- `web/src/features/tasks/components/KanbanBoard.test.tsx`: cover preview trigger wiring and verify existing card selection behavior remains unchanged.
- `docs/superpowers/specs/2026-10-20-task-side-peek.md`, `docs/superpowers/plans/2026-10-20-task-side-peek.md`: feature requirements and implementation record.

## Review Focus

- Clicking the preview button must not bubble to the card and open the full inspector; test with both click paths.
- A live status/filter update can remove the previewed task from its lane; retain the open preview but disable previous/next when the current ID is no longer in the lane.
- `j`/`k` while an editable element or modified shortcut is active must not change the task; test text input, textarea, contenteditable, and modifier cases.
- Rapid previous/next input at the first/last item must not wrap or select an undefined task; test boundaries and empty lanes.
- Closing then reopening another card must not retain a stale task, title, prompt, or lane index; test task changes and removal.

---

### Task 1: Build the Accessible Task Peek Sheet

**Files:**
- Create: `web/src/features/tasks/components/TaskPeekSheet.tsx`
- Create: `web/src/features/tasks/components/TaskPeekSheet.test.tsx`

**Interfaces:**
- `TaskPeekSheetProps` consumes `taskId: string | null`, `taskIds: string[]`, `open: boolean`, `onOpenChange(open: boolean): void`, `onTaskChange(taskId: string): void`, `onOpenInspector(taskId: string): void`, and `finalFocus: () => HTMLElement | null`.
- `TaskPeekSheet` reads current task data via `useTask(taskId)`; it does not own or alter task selection.
- Navigation calls `onTaskChange` with the adjacent ID from `taskIds`; it does not reorder or mutate the task store.
- The lane supplies `finalFocus`, returning the preview button if connected to the document, otherwise the originating lane heading.

- [ ] Add a fixture-driven test verifying the title, status, priority, prompt excerpt, and available repository/PR context are rendered.
- [ ] Add tests for previous/next controls at both boundaries, current task leaving `taskIds`, removed task, full-inspector handoff, and controlled close/Escape behavior.
- [ ] Add keyboard tests proving `j`/`k` navigate only while open and are ignored for inputs, textareas, contenteditable elements, and modified keys.
- [ ] Add an accessibility assertion for a polite live announcement when previous/next changes the current task.
- [ ] Run `cd web && npm test -- src/features/tasks/components/TaskPeekSheet.test.tsx` and confirm the new tests fail before implementation.
- [ ] Implement the component with the existing `Sheet`, `SheetContent`, `SheetTitle`, and `SheetDescription`; use a scrollable body, stable task identity, visible previous/next controls, and a full-inspector action.
- [ ] Scope the keyboard listener to the open sheet, clean it up on close/unmount, ignore editable targets/modifiers, and ensure `Escape` follows the Sheet primitive's close/focus-return behavior.
- [ ] Add a visually hidden `aria-live="polite"` message for the current task title/ID and configure final focus to use the originating lane heading if the preview trigger was removed while open.
- [ ] Use `onTaskChange` for both visible controls and the scoped `j`/`k` listener; the editable-target check must exclude inputs, textareas, selects, contenteditable, and `[role="textbox"]`:

```tsx
const isEditableTarget = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.isContentEditable || target.matches('input, textarea, select, [role="textbox"]'));
```

- [ ] Compute navigation with the lane's filtered order and treat an absent current ID as having no adjacent items:

```tsx
const index = taskIds.indexOf(taskId ?? '');
const previousTaskId = index > 0 ? taskIds[index - 1] : null;
const nextTaskId = index >= 0 && index < taskIds.length - 1 ? taskIds[index + 1] : null;
```

- [ ] Make the sheet full-width/full-height on narrow screens and a bounded right-side panel on desktop; do not hide controls behind hover.
- [ ] Rerun `cd web && npm test -- src/features/tasks/components/TaskPeekSheet.test.tsx`.

### Task 2: Integrate Preview into the Kanban Cards

**Files:**
- Modify: `web/src/features/tasks/components/TaskCard.tsx`
- Modify: `web/src/features/tasks/components/KanbanBoard.tsx`
- Modify: `web/src/features/tasks/components/KanbanBoard.test.tsx`

**Interfaces:**
- Add optional `onPreview?: (taskId: string, trigger: HTMLButtonElement) => void` to `TaskCardProps`.
- Each `KanbanLane` passes its existing filtered `taskIds` into one controlled `TaskPeekSheet` and owns `peekTaskId: string | null`.
- Lane `onTaskChange(taskId)` updates only that lane's `peekTaskId`; invalid IDs are ignored.
- Each lane retains the preview button element and a `tabIndex={-1}` lane heading ref; provide both through `finalFocus` so focus restoration has a fallback if the card moves or is filtered away.
- Existing `handleCardClick` still calls `selectTask(task.id)`; preview sets only the lane's `peekTaskId`.

- [ ] Add tests that clicking the card body selects the task for the existing full inspector and clicking the preview button opens the peek without selecting the task.
- [ ] Add a test showing preview navigation receives the filtered lane order, not all tasks from the store.
- [ ] Run `cd web && npm test -- src/features/tasks/components/KanbanBoard.test.tsx src/features/tasks/components/TaskPeekSheet.test.tsx` and confirm the integration test fails before implementation.
- [ ] Add a native `type="button"` preview control with a task-specific accessible name, visible hit target, focus-visible styling, and propagation stopped; keep the existing card click/cancel/link behaviors intact.
- [ ] Wire the button callback without activating the parent card:

```tsx
<button
  type="button"
  aria-label={`Preview task #${task.id.slice(0, 8)}: ${task.title}`}
  onClick={(event) => {
    event.stopPropagation();
    onPreview?.(task.id, event.currentTarget);
  }}
>
  <PanelRightOpen aria-hidden="true" />
</button>
```

- [ ] Connect lane-local preview state to `TaskPeekSheet`; on full-inspector handoff close the peek and then call the existing `selectTask(taskId)` action.
- [ ] Rerun the focused test command and `cd web && npm run build`.

### Task 3: Browser, Responsive, and Accessibility Verification

**Files:**
- Review only: files in Tasks 1–2; edit only to repair a verified defect within the same frontend boundary.

- [ ] Run `cd web && npm test` and `cd web && npm run build`.
- [ ] Run the app in a real browser without restarting Docker services; verify preview opening, card-click compatibility, inspector handoff, `j`/`k`, Escape dismissal, and focus return.
- [ ] Record Responsive Decisions and a Responsive Matrix at 320px, 390px, 768px, 1024px, and 1440px; verify no horizontal overflow, full-screen mobile sheet, desktop board context, and reachable controls.
- [ ] Check keyboard-only operation and accessibility semantics: button names, dialog title/description, focus containment/return, disabled navigation, and screen-reader announcement when the previewed task changes.
- [ ] Run `git diff --check`; inspect modified/untracked paths and confirm no out-of-scope backend/runtime/config files were changed.
- [ ] Do not commit or restart/rebuild/stop Docker services or containers; report any live verification limitation honestly.
