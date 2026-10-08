package api

import (
	"errors"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"
	"github.com/gofiber/fiber/v3"
)

type IntegrationHandlers struct {
	integService service.IntegrationService
}

type ProjectIntegrationParams struct {
	ProjectID string `validate:"required"`
	ID        string `validate:"omitempty"`
}

func parseProjectIntegrationParams(c fiber.Ctx) (ProjectIntegrationParams, error) {
	p := ProjectIntegrationParams{
		ProjectID: c.Params("project_id"),
		ID:        c.Params("id"),
	}
	if err := models.Validate.Struct(&p); err != nil {
		return p, err
	}
	return p, nil
}

func NewIntegrationHandlers(integService service.IntegrationService) *IntegrationHandlers {
	return &IntegrationHandlers{integService: integService}
}

func (h *IntegrationHandlers) List(c fiber.Ctx) error {
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	res, err := h.integService.ListIntegrations(c.Context(), params.ProjectID)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Create(c fiber.Ctx) error {
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	var req dto.CreateIntegrationRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	res, err := h.integService.CreateIntegration(c.Context(), params.ProjectID, req)
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
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	res, err := h.integService.GetIntegration(c.Context(), params.ProjectID, params.ID)
	if err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Integration not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Update(c fiber.Ctx) error {
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	var req dto.UpdateIntegrationRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	res, err := h.integService.UpdateIntegration(c.Context(), params.ProjectID, params.ID, req)
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
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	if err := h.integService.DeleteIntegration(c.Context(), params.ProjectID, params.ID); err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Integration not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}
