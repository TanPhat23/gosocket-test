package api

import (
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
		return badRequest(c, err)
	}
	res, err := h.integService.ListIntegrations(c.Context(), params.ProjectID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Create(c fiber.Ctx) error {
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return badRequest(c, err)
	}
	req, err := bindBody[dto.CreateIntegrationRequest](c)
	if err != nil {
		return badRequest(c, err)
	}

	res, err := h.integService.CreateIntegration(c.Context(), params.ProjectID, req)
	if err != nil {
		return CentralErrorHandler(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(res)
}

func (h *IntegrationHandlers) Get(c fiber.Ctx) error {
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return badRequest(c, err)
	}
	res, err := h.integService.GetIntegration(c.Context(), params.ProjectID, params.ID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Update(c fiber.Ctx) error {
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return badRequest(c, err)
	}
	req, err := bindBody[dto.UpdateIntegrationRequest](c)
	if err != nil {
		return badRequest(c, err)
	}

	res, err := h.integService.UpdateIntegration(c.Context(), params.ProjectID, params.ID, req)
	if err != nil {
		return CentralErrorHandler(c, err)
	}
	return c.JSON(res)
}

func (h *IntegrationHandlers) Delete(c fiber.Ctx) error {
	params, err := parseProjectIntegrationParams(c)
	if err != nil {
		return badRequest(c, err)
	}
	if err := h.integService.DeleteIntegration(c.Context(), params.ProjectID, params.ID); err != nil {
		return CentralErrorHandler(c, err)
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}
