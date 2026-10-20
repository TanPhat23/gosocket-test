# Kanban Task Side-Peek Specification

## Goal

Let users quickly inspect and move between tasks from the Kanban board without repeatedly opening and closing the full task inspector.

## Behavior

- Keep clicking the body of a task card opening the existing full `TaskInspectorModal`; do not change existing task-detail routes or selection behavior.
- Add a separate, always-visible, keyboard-accessible preview action on each card. Activating it opens a right-side task peek sheet and leaves the Kanban board mounted behind it.
- Show the task title, short ID, status, priority, prompt preview, and available repository/model/pull-request context. Do not duplicate the full inspector's session, terminal, diff, or file-explorer tabs.
- The preview sheet has visible previous/next controls that step through the currently visible, filtered tasks in the same lane. Disable controls at either end. Keep the current task visible if it moves out of the lane while the sheet is open; in that case, disable lane navigation until the user closes or reopens a preview.
- While the peek is open, `j` moves to the next task and `k` to the previous task. Ignore shortcuts while a text input, textarea, select, or contenteditable element has focus, and when a modifier key is pressed.
- Provide an “Open full inspector” action that closes the peek and opens the existing inspector for the same task.
- `Escape` closes the peek and returns focus to its preview trigger when it remains mounted; if a live task/filter update removed that trigger, return focus to the originating lane heading. Standard dialog focus management and screen-reader title/description are required.
- On desktop, render a right-side panel that leaves the board visible. At narrow/mobile widths, use a full-screen sheet; all navigation must remain available through visible buttons without relying on hover or keyboard shortcuts.

## Accessibility and Responsive Requirements

- The preview trigger is a real button with an accessible name containing the task identity. It is visible on touch devices and is not only revealed by hover.
- The peek uses the existing accessible Sheet/Dialog primitives, with a programmatic title and description, managed focus, and Escape dismissal. A polite live region announces task changes.
- Previous/next and full-inspector actions are native buttons with clear labels and disabled states; announce task changes to assistive technology without moving focus unexpectedly.
- Respect reduced-motion preferences and keep controls usable at 320 CSS pixels wide and at desktop widths.
- Keyboard shortcuts are scoped to the open peek; do not install a workspace-wide key handler.

## Constraints

- No backend/API changes or new dependencies.
- Do not change card-body click behavior, task status, task mutation semantics, filters, or task ordering.
- Navigation order is the lane's current filtered order as rendered by the board.
- Preserve existing uncommitted runtime/config changes; do not edit their files.

## Acceptance Criteria

1. Existing card-body click still opens the full inspector.
2. The card preview button opens the side sheet without opening the full inspector.
3. The sheet displays the selected task's preview data and an action to open its full inspector.
4. Previous/next buttons and `j`/`k` step through currently visible task IDs in the same lane, with correct disabled behavior at the ends.
5. Typing targets and modified key presses do not trigger navigation; Escape closes the sheet.
6. The sheet closes if the task is removed from the store, and remains stable if its status/filter causes it to leave the original lane.
7. At desktop widths the sheet is a right-side panel with board context visible; at mobile widths it fills the viewport and controls remain accessible.
8. Keyboard focus returns to the preview trigger after dismissal, or the originating lane heading if the trigger was removed; keyboard and screen-reader users can identify the trigger, sheet title, navigation controls, and current task.
9. Existing task-inspector tests continue to pass; no backend, task-status, or API behavior changes.
