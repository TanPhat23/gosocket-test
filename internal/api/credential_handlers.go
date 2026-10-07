package api

import (
	"errors"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

// CredentialHandlers handles credential management endpoints.
type CredentialHandlers struct {
	credService service.CredentialService
}

// NewCredentialHandlers creates a new CredentialHandlers instance.
func NewCredentialHandlers(credService service.CredentialService) *CredentialHandlers {
	return &CredentialHandlers{
		credService: credService,
	}
}

// List handles GET /api/v1/teams/:team_id/credentials.
func (h *CredentialHandlers) List(c fiber.Ctx) error {
	teamID := c.Params("team_id")

	credentials, err := h.credService.List(c.Context(), teamID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list credentials"})
	}

	return c.JSON(fiber.Map{"credentials": credentials})
}

// Create handles POST /api/v1/teams/:team_id/credentials.
func (h *CredentialHandlers) Create(c fiber.Ctx) error {
	teamID := c.Params("team_id")

	var req dto.CreateCredentialRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	credDTO, err := h.credService.Create(c.Context(), teamID, req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save credential"})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"credential": credDTO,
	})
}

// Delete handles DELETE /api/v1/teams/:team_id/credentials/:credential_id.
func (h *CredentialHandlers) Delete(c fiber.Ctx) error {
	teamID := c.Params("team_id")
	credID := c.Params("credential_id")

	if err := h.credService.Delete(c.Context(), teamID, credID); err != nil {
		if errors.Is(err, service.ErrCredentialNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Credential not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete credential"})
	}

	return c.JSON(fiber.Map{"status": "deleted"})
}

// ListGitHubRepositories handles GET /api/v1/teams/:team_id/credentials/:credential_id/github/repos.
func (h *CredentialHandlers) ListGitHubRepositories(c fiber.Ctx) error {
	teamID := c.Params("team_id")
	credID := c.Params("credential_id")

	repos, err := h.credService.ListGitHubRepositories(c.Context(), teamID, credID)
	if err != nil {
		if errors.Is(err, service.ErrCredentialNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Credential not found"})
		}
		if errors.Is(err, service.ErrInvalidProvider) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if err.Error() == "invalid or expired github token" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
		}
		if err.Error() == "github api rate limit exceeded" {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list GitHub repositories"})
	}

	return c.JSON(fiber.Map{"repositories": repos})
}
