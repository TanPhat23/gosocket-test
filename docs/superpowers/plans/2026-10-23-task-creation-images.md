# Task Creation Image Attachments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add image attachment support (file picker, clipboard paste, thumbnail previews, validation) when creating a task in `TaskModalContent`, matching the existing image attachment flow.

**Architecture:** Extend backend `CreateTaskRequest` DTO and service validation with `SessionImageDTO` and `steerFiles` validation. Update frontend schemas, types, and `TaskModalContent` to use `readSteerImages` / image thumbnail previews, and send images with `createTask`.

**Tech Stack:** Go (Fiber, GORM, validator), React 19, TanStack Form, Vitest, Testing Library.

**Spec:** `docs/superpowers/specs/2026-10-23-task-creation-images.md`

## Global Constraints
- Maximum 5 images per request.
- Maximum 5 MB per image.
- Allowed MIME types: `image/png`, `image/jpeg`, `image/gif`, `image/webp`.
- Images transferred as standard base64 strings without `data:...` prefix.

## Review Focus
1. User pastes multiple non-image files into prompt textarea - only valid image files processed, non-images produce a clean error message.
2. User attaches more than 5 images - capped at 5 images with warning message.
3. Submitting task with attached images sends clean base64 data to API.
4. Modal reset / cancel clears all attached images and draft state.
5. Backend rejects malformed or oversized images with HTTP 400.

---

### Task 1: Backend DTO and Service Validation for Task Creation Images

**Files:**
- Modify: `internal/dto/task_dto.go`
- Modify: `internal/service/task_service.go`
- Test: `internal/service/task_service_test.go`

**Interfaces:**
- Consumes: `dto.SessionImageDTO`, `steerFiles([]dto.SessionImageDTO)`
- Produces: `dto.CreateTaskRequest.Images` field validated during `CreateTask`

- [ ] **Step 1: Write the failing test in `internal/service/task_service_test.go`**
Add tests asserting that `CreateTask` rejects invalid images (unsupported MIME, bad base64, too large) with HTTP 400, and accepts valid images.

- [ ] **Step 2: Run test to verify it fails**
Run: `export TMPDIR=/workspaces/tmp GOTMPDIR=/workspaces/tmp GOCACHE=/workspaces/tmp/go-build && go test ./internal/service -run TestCreateTask_Images`
Expected: FAIL

- [ ] **Step 3: Update `CreateTaskRequest` and `CreateTask` in `internal/dto/task_dto.go` and `internal/service/task_service.go`**
Add `Images []SessionImageDTO` to `CreateTaskRequest`. In `CreateTask`, call `steerFiles(req.Images)` to validate before persisting.

- [ ] **Step 4: Run test to verify it passes**
Run: `export TMPDIR=/workspaces/tmp GOTMPDIR=/workspaces/tmp GOCACHE=/workspaces/tmp/go-build && go test ./internal/service -run TestCreateTask_Images`
Expected: PASS

- [ ] **Step 5: Commit changes**
Commit backend changes: `git commit -m "feat(api): support and validate images in CreateTaskRequest"`

---

### Task 2: Frontend Schemas and API Types for Task Creation Images

**Files:**
- Modify: `web/src/features/tasks/schemas/taskSchemas.ts`
- Modify: `web/src/features/tasks/api/taskApi.ts`
- Test: `web/src/lib/api.test.ts`

**Interfaces:**
- Consumes: `SteerImage` from `web/src/features/tasks/utils/steerImages.ts`
- Produces: `CreateTaskRequestSchema` with optional `images`, `TaskDTO` payload mapping

- [ ] **Step 1: Write the failing test in `web/src/lib/api.test.ts`**
Add test verifying that calling `apiClient.createTask` with images includes the mapped image payload.

- [ ] **Step 2: Run test to verify it fails**
Run: `cd web && npx vitest run --no-file-parallelism src/lib/api.test.ts`
Expected: FAIL

- [ ] **Step 3: Implement schema and API updates**
Update `CreateTaskRequestSchema` in `web/src/features/tasks/schemas/taskSchemas.ts` to include optional `images: z.array(...)`. Update `taskApi.createTask` if necessary.

- [ ] **Step 4: Run test to verify it passes**
Run: `cd web && npx vitest run --no-file-parallelism src/lib/api.test.ts`
Expected: PASS

- [ ] **Step 5: Commit changes**
Commit schema changes: `git commit -m "feat(web): update task creation schemas to support images"`

---

### Task 3: Task Creation Modal UI for Image Attachments

**Files:**
- Modify: `web/src/features/tasks/components/TaskModalContent.tsx`
- Test: `web/src/features/tasks/components/TaskModal.test.tsx`

**Interfaces:**
- Consumes: `readSteerImages`, `SteerImage`, `MAX_STEER_IMAGES`, `STEER_IMAGE_TYPES` from `web/src/features/tasks/utils/steerImages.ts`
- Produces: Interactive image upload button, drag/paste image handler, thumbnail previews, counter, and removal in `TaskModalContent`.

- [ ] **Step 1: Write the failing tests in `web/src/features/tasks/components/TaskModal.test.tsx`**
Add tests verifying:
- Attaching image via file input displays image preview and counter.
- Removing an image thumbnail removes it from the list.
- Pasting an image into the prompt attaches the image.
- Submitting the form calls `createTask` with attached images.
- Closing or cancelling the modal resets attached images.

- [ ] **Step 2: Run test to verify it fails**
Run: `cd web && npx vitest run --no-file-parallelism src/features/tasks/components/TaskModal.test.tsx`
Expected: FAIL

- [ ] **Step 3: Implement image attachment UI in `TaskModalContent.tsx`**
- Integrate image state (`images`, `attachImages`, `removeImage`, `error`) using `readSteerImages`.
- Add paste handler on Prompt textarea.
- Add file input and trigger button (`ImagePlus` icon) next to the form/prompt.
- Render thumbnail previews with remove button and count badge.
- Pass mapped images in `createTask` payload.
- Reset images on modal close/cancel.

- [ ] **Step 4: Run test to verify it passes**
Run: `cd web && npx vitest run --no-file-parallelism src/features/tasks/components/TaskModal.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit changes**
Commit frontend changes: `git commit -m "feat(web): add image attachment options to task creation modal"`
