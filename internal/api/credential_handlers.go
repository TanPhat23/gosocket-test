package api

import (
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

// CredentialHandlers handles credential management endpoints.
type CredentialHandlers struct {
	credService service.CredentialService
}

// TeamCredentialParams validates team and credential URI parameters.
type TeamCredentialParams struct {
	TeamID       string `validate:"required"`
	CredentialID string `validate:"omitempty"`
}

func parseTeamCredentialParams(c fiber.Ctx) (TeamCredentialParams, error) {
	p := TeamCredentialParams{
		TeamID:       c.Params("team_id"),
		CredentialID: c.Params("credential_id"),
	}
	if err := models.Validate.Struct(&p); err != nil {
		return p, err
	}
	return p, nil
}

// NewCredentialHandlers creates a new CredentialHandlers instance.
func NewCredentialHandlers(credService service.CredentialService) *CredentialHandlers {
	return &CredentialHandlers{
		credService: credService,
	}
}

// List handles GET /api/v1/teams/:team_id/credentials.
func (h *CredentialHandlers) List(c fiber.Ctx) error {
	params, err := parseTeamCredentialParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	credentials, err := h.credService.List(c.Context(), params.TeamID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{"credentials": credentials})
}

// Create handles POST /api/v1/teams/:team_id/credentials.
func (h *CredentialHandlers) Create(c fiber.Ctx) error {
	params, err := parseTeamCredentialParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var req dto.CreateCredentialRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	credDTO, err := h.credService.Create(c.Context(), params.TeamID, req)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"credential": credDTO,
	})
}

// Update handles PUT/PATCH /api/v1/teams/:team_id/credentials/:credential_id.
func (h *CredentialHandlers) Update(c fiber.Ctx) error {
	params, err := parseTeamCredentialParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var req dto.UpdateCredentialRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	credDTO, err := h.credService.Update(c.Context(), params.TeamID, params.CredentialID, req)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{
		"credential": credDTO,
	})
}

// Delete handles DELETE /api/v1/teams/:team_id/credentials/:credential_id.
func (h *CredentialHandlers) Delete(c fiber.Ctx) error {
	params, err := parseTeamCredentialParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if err := h.credService.Delete(c.Context(), params.TeamID, params.CredentialID); err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{"status": "deleted"})
}

// ListGitHubRepositories handles GET /api/v1/teams/:team_id/credentials/:credential_id/github/repos.
func (h *CredentialHandlers) ListGitHubRepositories(c fiber.Ctx) error {
	params, err := parseTeamCredentialParams(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	repos, err := h.credService.ListGitHubRepositories(c.Context(), params.TeamID, params.CredentialID)
	if err != nil {
		return CentralErrorHandler(c, err)
	}

	return c.JSON(fiber.Map{"repositories": repos})
}
