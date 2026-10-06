package api

import (
	"errors"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/service"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// TaskHandlers handles task CRUD endpoints.
type TaskHandlers struct {
	taskService service.TaskService
}

// NewTaskHandlers creates a new TaskHandlers instance.
func NewTaskHandlers(taskService service.TaskService) *TaskHandlers {
	return &TaskHandlers{
		taskService: taskService,
	}
}

// List handles GET /api/v1/tasks?project_id=...
func (h *TaskHandlers) List(c fiber.Ctx) error {
	projectID := c.Query("project_id")
	tasks, err := h.taskService.ListTasks(c.Context(), projectID)
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

// Get handles GET /api/v1/tasks/:task_id
func (h *TaskHandlers) Get(c fiber.Ctx) error {
	taskID := c.Params("task_id")
	if taskID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "task_id is required"})
	}

	task, err := h.taskService.GetTask(c.Context(), taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to get task"})
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

// Cancel handles POST /api/v1/tasks/:task_id/cancel
func (h *TaskHandlers) Cancel(c fiber.Ctx) error {
	taskID := c.Params("task_id")
	if taskID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "task_id is required"})
	}

	if err := h.taskService.CancelTask(c.Context(), taskID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Task not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to cancel task"})
	}

	return c.JSON(fiber.Map{"status": "cancelled"})
}
