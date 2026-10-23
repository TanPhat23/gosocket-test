# Task Creation Image Attachments Spec

**Goal:** Allow users to attach image files or paste screenshots when creating a new task, matching the existing steer image attachment capabilities.

## Context & Architecture
Users can already attach screenshots and images when steering an agent session (`SteerComposer.tsx` using `useSteerDraft` and `readSteerImages` from `steerImages.ts`). However, when creating a new task in `TaskModalContent.tsx`, users can currently only input text prompt.
Furthermore, in the frontend data layer:
- `CreateTaskRequest` schema and `taskApi.createTask` pass `CreateTaskInput` to the backend.
- In backend `internal/dto/task_dto.go`, `CreateTaskRequest` accepts parameters for task dispatch.
- When creating a task, images can be provided as `[]SessionImageDTO` in `CreateTaskRequest`.
- On the backend, `CreateTask` initializes the task and if images are attached, they are validated via `steerFiles(req.Images)` to ensure limits (PNG, JPEG, GIF, WebP, <= 5MB each, max 5 images).
- In the task runner / session prompt delivery, initial task prompts with images will be passed or preserved, and the frontend UI allows attaching/pasting images with thumbnails and removal.

## Frontend Requirements
1. In `TaskModalContent.tsx`:
   - Support pasting images from clipboard into prompt textarea.
   - Provide an attachment button (`ImagePlus` icon) to trigger file picker accepting `image/png,image/jpeg,image/gif,image/webp`.
   - Display image thumbnails with remove (`X`) buttons before submission.
   - Show counter indicator `n/5` when images are attached.
   - Display attach errors (e.g. invalid type, size > 5MB, > 5 images) matching `readSteerImages`.
   - On submit, include `images` in `taskData` and reset attached images upon modal close or successful dispatch.
2. Update schemas and api:
   - `web/src/features/tasks/schemas/taskSchemas.ts`: update `CreateTaskRequestSchema` to accept optional `images: z.array(...)`.
   - `web/src/features/tasks/utils/steerImages.ts`: reusable types and helpers for both steer and task creation.

## Backend Requirements
1. In `internal/dto/task_dto.go`:
   - Add `Images []SessionImageDTO` to `CreateTaskRequest`.
2. In `internal/service/task_service.go`:
   - Validate `req.Images` in `CreateTask` using `steerFiles(req.Images)`. Return a bad request error if invalid.
3. Tests:
   - Unit tests for backend `CreateTask` with valid and invalid images.
   - Frontend tests in `TaskModal.test.tsx` for attaching images, displaying thumbnails, and passing images to `createTask`.
