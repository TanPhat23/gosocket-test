package api

import (
	"errors"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// TaskHandlers handles task CRUD endpoints.
type TaskHandlers struct {
	taskService service.TaskService
}

// TaskIDParam defines the path parameter validation for task endpoints.
type TaskIDParam struct {
	ID string `validate:"required"`
}

func parseTaskID(c fiber.Ctx) (string, error) {
	id := c.Params("id")
	if id == "" {
		id = c.Params("task_id")
	}
	param := TaskIDParam{ID: id}
	if err := models.Validate.Struct(&param); err != nil {
		return "", err
	}
	return param.ID, nil
}

// isPassthroughTaskError reports errors that must keep their central HTTP mapping instead of a generic 400.
func isPassthroughTaskError(err error) bool {
	return errors.Is(err, common.ErrNotFound) ||
		errors.Is(err, gorm.ErrRecordNotFound) ||
		errors.Is(err, common.ErrRuntimeQuarantined) ||
		errors.Is(err, common.ErrForbidden) ||
		errors.Is(err, common.ErrServiceUnavailable)
}

// NewTaskHandlers creates a new TaskHandlers instance.
func NewTaskHandlers(taskService service.TaskService) *TaskHandlers {
	return &TaskHandlers{
		taskService: taskService,
	}
}

// List handles GET /api/v1/tasks?project_id=...&parent_task_id=...
func (h *TaskHandlers) List(c fiber.Ctx) error {
	userID := currentUserID(c)
	parentTaskID := c.Query("parent_task_id")
	var tasks []models.Task
	var err error

	if parentTaskID != "" {
		tasks, err = h.taskService.ListSubtasks(c.Context(), parentTaskID, userID)
	} else {
		projectID := c.Query("project_id")
		tasks, err = h.taskService.ListTasks(c.Context(), projectID, userID)
	}

	if err != nil {
		return CentralErrorHandler(c, err)
	}

	responses := make([]dto.TaskResponse, len(tasks))
	for i := range tasks {
		responses[i] = dto.ToTaskResponse(&tasks[i])
	}

	return c.JSON(responses)
}

// Get handles GET /api/v1/tasks/:id (and :task_id)
func (h *TaskHandlers) Get(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}

	userID := currentUserID(c)
	task, err := h.taskService.GetTask(c.Context(), taskID, userID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	// If task is in review and has a PR, check if PR status changed on GitHub
	if task.Status == models.TaskStatusReview && task.PRURL != nil && *task.PRURL != "" {
		if updated, syncErr := h.taskService.SyncTaskPRStatus(c.Context(), taskID, userID); syncErr == nil && updated != nil {
			task = updated
		}
	}

	return c.JSON(dto.ToTaskResponse(task))
}

// Create handles POST /api/v1/tasks
func (h *TaskHandlers) Create(c fiber.Ctx) error {
	req, err := bindBody[dto.CreateTaskRequest](c)
	if err != nil {
		return badRequest(c, err)
	}

	task, err := h.taskService.CreateTask(c.Context(), &req, currentUserID(c))
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(dto.ToTaskResponse(task))
}

// Cancel handles POST /api/v1/tasks/:id/cancel (and :task_id/cancel)
func (h *TaskHandlers) Cancel(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}

	userID := currentUserID(c)

	if err := h.taskService.CancelTask(c.Context(), taskID, userID); err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{"status": "cancelled"})
}

// CreatePR handles POST /api/v1/tasks/:id/pr
func (h *TaskHandlers) CreatePR(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}

	task, err := h.taskService.CreatePullRequestForTask(c.Context(), taskID, currentUserID(c))
	if err != nil {
		if isPassthroughTaskError(err) {
			return CentralErrorHandler(c, err)
		}
		return badRequest(c, err)
	}

	prURL := ""
	if task.PRURL != nil {
		prURL = *task.PRURL
	}
	return c.JSON(fiber.Map{
		"task_id":          task.ID,
		"pull_request_url": prURL,
		"pr_url":           prURL,
	})
}

// SyncPR handles POST /api/v1/tasks/:id/sync-pr
func (h *TaskHandlers) SyncPR(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}

	userID := currentUserID(c)

	task, err := h.taskService.SyncTaskPRStatus(c.Context(), taskID, userID)
	if err != nil {
		if isPassthroughTaskError(err) {
			return CentralErrorHandler(c, err)
		}
		return badRequest(c, err)
	}

	return c.JSON(dto.ToTaskResponse(task))
}

// GetSession handles GET /api/v1/tasks/:id/session
// Protected by RequireAuth.
func (h *TaskHandlers) GetSession(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}

	userID := currentUserID(c)

	sessionDTO, err := h.taskService.GetTaskSession(c.Context(), taskID, userID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(sessionDTO)
}

// PromptSession handles POST /api/v1/tasks/:id/session/prompt
// Protected by RequireAuth.
func (h *TaskHandlers) PromptSession(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}

	req, err := bindBody[dto.TaskSessionPromptRequest](c)
	if err != nil {
		return badRequest(c, err)
	}

	userID := currentUserID(c)

	if err := h.taskService.PromptTaskSession(c.Context(), taskID, userID, &req); err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// CompactSession handles POST /api/v1/tasks/:id/session/compact
// Protected by RequireAuth.
func (h *TaskHandlers) CompactSession(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}

	userID := currentUserID(c)

	if err := h.taskService.CompactTaskSession(c.Context(), taskID, userID); err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{"status": "compacted"})
}

// InterruptSession handles POST /api/v1/tasks/:id/session/interrupt
// Protected by RequireAuth. Stops the running turn; the task itself is not cancelled.
func (h *TaskHandlers) InterruptSession(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}

	if err := h.taskService.InterruptTaskSession(c.Context(), taskID, currentUserID(c)); err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{"status": "interrupted"})
}

// GetDiff returns the unified git diff for a task.
func (h *TaskHandlers) GetDiff(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}
	if taskID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "task id is required"})
	}

	userID := currentUserID(c)

	diff, err := h.taskService.GetTaskDiff(c.Context(), taskID, userID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{"diff": diff})
}

// ListFiles handles GET /api/v1/tasks/:id/files and lists the files of the task's workspace.
func (h *TaskHandlers) ListFiles(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}
	resp, err := h.taskService.ListTaskFiles(c.Context(), taskID, currentUserID(c))
	if err != nil {
		return CentralErrorHandler(c, err)
	}
	return c.JSON(resp)
}

// ReadFile handles GET /api/v1/tasks/:id/files/content?path= and returns one file of the task's workspace.
func (h *TaskHandlers) ReadFile(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return badRequest(c, err)
	}
	resp, err := h.taskService.ReadTaskFile(c.Context(), taskID, currentUserID(c), c.Query("path"))
	if err != nil {
		if errors.Is(err, common.ErrInvalidPath) {
			return badRequest(c, err)
		}
		return CentralErrorHandler(c, err)
	}
	return c.JSON(resp)
}
