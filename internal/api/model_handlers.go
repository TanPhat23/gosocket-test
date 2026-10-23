package api

import (
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/runner"
	"github.com/gofiber/fiber/v3"
)

// ModelHandlers handles AI model discovery and querying endpoints.
type ModelHandlers struct {
	cfg *config.Config
}

// NewModelHandlers creates a new ModelHandlers instance.
func NewModelHandlers(cfg *config.Config) *ModelHandlers {
	return &ModelHandlers{cfg: cfg}
}

// ListModels handles GET /api/v1/models
func (h *ModelHandlers) ListModels(c fiber.Ctx) error {
	models := runner.ListAvailableModels(h.cfg)
	return c.JSON(models)
}
