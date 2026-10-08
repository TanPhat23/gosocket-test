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

// NewTaskHandlers creates a new TaskHandlers instance.
func NewTaskHandlers(taskService service.TaskService) *TaskHandlers {
	return &TaskHandlers{
		taskService: taskService,
	}
}

// List handles GET /api/v1/tasks?project_id=...&parent_task_id=...
func (h *TaskHandlers) List(c fiber.Ctx) error {
	parentTaskID := c.Query("parent_task_id")
	var tasks []models.Task
	var err error

	if parentTaskID != "" {
		tasks, err = h.taskService.ListSubtasks(c.Context(), parentTaskID)
	} else {
		projectID := c.Query("project_id")
		tasks, err = h.taskService.ListTasks(c.Context(), projectID)
	}

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list tasks"})
	}

	responses := make([]dto.TaskResponse, len(tasks))
	for i := range tasks {
		responses[i] = dto.ToTaskResponse(&tasks[i])
	}
	if responses == nil {
		responses = []dto.TaskResponse{}
	}

	return c.JSON(responses)
}

// Get handles GET /api/v1/tasks/:id (and :task_id)
func (h *TaskHandlers) Get(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	task, err := h.taskService.GetTask(c.Context(), taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get task"})
	}

	// If task is in review and has a PR, check if PR status changed on GitHub
	if task.Status == models.TaskStatusReview && task.PRURL != nil && *task.PRURL != "" {
		user := CurrentUser(c)
		userID := ""
		if user != nil {
			userID = user.ID
		}
		if updated, syncErr := h.taskService.SyncTaskPRStatus(c.Context(), taskID, userID); syncErr == nil && updated != nil {
			task = updated
		}
	}

	return c.JSON(dto.ToTaskResponse(task))
}

// Create handles POST /api/v1/tasks
func (h *TaskHandlers) Create(c fiber.Ctx) error {
	var req dto.CreateTaskRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	task, err := h.taskService.CreateTask(c.Context(), &req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create task"})
	}

	return c.Status(fiber.StatusCreated).JSON(dto.ToTaskResponse(task))
}

// Cancel handles POST /api/v1/tasks/:id/cancel (and :task_id/cancel)
func (h *TaskHandlers) Cancel(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if err := h.taskService.CancelTask(c.Context(), taskID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to cancel task"})
	}

	return c.JSON(fiber.Map{"status": "cancelled"})
}

// CreatePR handles POST /api/v1/tasks/:id/pr
func (h *TaskHandlers) CreatePR(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	task, err := h.taskService.CreatePullRequestForTask(c.Context(), taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task not found"})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
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
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	user := CurrentUser(c)
	userID := ""
	if user != nil {
		userID = user.ID
	}

	task, err := h.taskService.SyncTaskPRStatus(c.Context(), taskID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task not found"})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(dto.ToTaskResponse(task))
}

// GetSession handles GET /api/v1/tasks/:id/session
// Protected by RequireAuth.
func (h *TaskHandlers) GetSession(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	user := CurrentUser(c)
	userID := ""
	if user != nil {
		userID = user.ID
	}

	sessionDTO, err := h.taskService.GetTaskSession(c.Context(), taskID, userID)
	if err != nil {
		if errors.Is(err, common.ErrForbidden) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden: insufficient permissions"})
		}
		if errors.Is(err, common.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task or session not found"})
		}
		if errors.Is(err, common.ErrServiceUnavailable) {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": common.ErrServiceUnavailable.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(sessionDTO)
}

// PromptSession handles POST /api/v1/tasks/:id/session/prompt
// Protected by RequireAuth.
func (h *TaskHandlers) PromptSession(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var req dto.TaskSessionPromptRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	user := CurrentUser(c)
	userID := ""
	if user != nil {
		userID = user.ID
	}

	if err := h.taskService.PromptTaskSession(c.Context(), taskID, userID, &req); err != nil {
		if errors.Is(err, common.ErrForbidden) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden: insufficient permissions"})
		}
		if errors.Is(err, common.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task or session not found"})
		}
		if errors.Is(err, common.ErrServiceUnavailable) {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": common.ErrServiceUnavailable.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// CompactSession handles POST /api/v1/tasks/:id/session/compact
// Protected by RequireAuth.
func (h *TaskHandlers) CompactSession(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	user := CurrentUser(c)
	userID := ""
	if user != nil {
		userID = user.ID
	}

	if err := h.taskService.CompactTaskSession(c.Context(), taskID, userID); err != nil {
		if errors.Is(err, common.ErrForbidden) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden: insufficient permissions"})
		}
		if errors.Is(err, common.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task or session not found"})
		}
		if errors.Is(err, common.ErrServiceUnavailable) {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": common.ErrServiceUnavailable.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"status": "compacted"})
}

// GetDiff returns the unified git diff for a task.
func (h *TaskHandlers) GetDiff(c fiber.Ctx) error {
	taskID, err := parseTaskID(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if taskID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "task id is required"})
	}

	user := CurrentUser(c)
	userID := ""
	if user != nil {
		userID = user.ID
	}

	diff, err := h.taskService.GetTaskDiff(c.Context(), taskID, userID)
	if err != nil {
		if errors.Is(err, common.ErrForbidden) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden: insufficient permissions"})
		}
		if errors.Is(err, common.ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"diff": diff})
}
