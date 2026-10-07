package api

import (
	"errors"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/service"
	"github.com/gofiber/fiber/v3"
)

type IntegrationHandlers struct {
	integService service.IntegrationService
}

func NewIntegrationHandlers(integService service.IntegrationService) *IntegrationHandlers {
	return &IntegrationHandlers{integService: integService}
}

func (h *IntegrationHandlers) List(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	res, err := h.integService.ListIntegrations(c.Context(), projectID)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Create(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	var req dto.CreateIntegrationRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	res, err := h.integService.CreateIntegration(c.Context(), projectID, req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRepoSlug) || errors.Is(err, service.ErrInvalidBaseBranch) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if errors.Is(err, service.ErrProjectNotFound) || errors.Is(err, service.ErrCredentialNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		if errors.Is(err, service.ErrUnauthorizedTenant) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(res)
}

func (h *IntegrationHandlers) Get(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	id := c.Params("id")
	res, err := h.integService.GetIntegration(c.Context(), projectID, id)
	if err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Integration not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Update(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	id := c.Params("id")
	var req dto.UpdateIntegrationRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	res, err := h.integService.UpdateIntegration(c.Context(), projectID, id, req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidBaseBranch) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if errors.Is(err, service.ErrIntegrationNotFound) || errors.Is(err, service.ErrProjectNotFound) || errors.Is(err, service.ErrCredentialNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		if errors.Is(err, service.ErrUnauthorizedTenant) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Delete(c fiber.Ctx) error {
	projectID := c.Params("project_id")
	id := c.Params("id")
	if err := h.integService.DeleteIntegration(c.Context(), projectID, id); err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Integration not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}
