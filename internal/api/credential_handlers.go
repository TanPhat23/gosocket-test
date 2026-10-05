package api

import (
	"errors"
	"time"

	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/models"

	"github.com/google/uuid"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// CredentialHandlers handles credential management endpoints.
type CredentialHandlers struct {
	db     *gorm.DB
	cipher *crypto.SecretCipher
}

// NewCredentialHandlers creates a new CredentialHandlers instance.
func NewCredentialHandlers(db *gorm.DB, cipher *crypto.SecretCipher) *CredentialHandlers {
	return &CredentialHandlers{
		db:     db,
		cipher: cipher,
	}
}

// CreateCredentialRequest defines the input payload for creating a credential.
type CreateCredentialRequest struct {
	Provider models.CredentialProvider `json:"provider" validate:"required,oneof=github jira trello oidc generic"`
	Label    string                    `json:"label" validate:"required,min=1,max=100"`
	Secret   string                    `json:"secret" validate:"required,min=1"`
}

// CredentialResponse represents the safe JSON response format (never returning EncryptedData).
type CredentialResponse struct {
	ID        string                    `json:"id"`
	TeamID    string                    `json:"team_id"`
	Provider  models.CredentialProvider `json:"provider"`
	Label     string                    `json:"label"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

func toCredentialResponse(c *models.Credential) CredentialResponse {
	return CredentialResponse{
		ID:        c.ID,
		TeamID:    c.TeamID,
		Provider:  c.Provider,
		Label:     c.Label,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// List handles GET /api/v1/teams/:team_id/credentials.
func (h *CredentialHandlers) List(c fiber.Ctx) error {
	teamID := c.Params("team_id")
	if teamID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing team_id"})
	}

	var credentials []models.Credential
	if err := h.db.Where("team_id = ?", teamID).Find(&credentials).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list credentials"})
	}

	resp := make([]CredentialResponse, 0, len(credentials))
	for i := range credentials {
		resp = append(resp, toCredentialResponse(&credentials[i]))
	}

	return c.JSON(fiber.Map{"credentials": resp})
}

// Create handles POST /api/v1/teams/:team_id/credentials.
func (h *CredentialHandlers) Create(c fiber.Ctx) error {
	if h.cipher == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Secret encryption service is not configured",
		})
	}

	teamID := c.Params("team_id")
	if teamID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Missing team_id"})
	}

	var req CreateCredentialRequest
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	encryptedData, err := h.cipher.Encrypt(req.Secret)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to encrypt secret"})
	}

	now := time.Now()
	cred := models.Credential{
		ID:             "cred_" + uuid.New().String(),
		TeamID:         teamID,
		Provider:       req.Provider,
		Label:          req.Label,
		EncryptedData:  encryptedData,
		KeyFingerprint: h.cipher.KeyFingerprint(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := h.db.Create(&cred).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save credential"})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"credential": toCredentialResponse(&cred),
	})
}

// Delete handles DELETE /api/v1/teams/:team_id/credentials/:credential_id.
func (h *CredentialHandlers) Delete(c fiber.Ctx) error {
	teamID := c.Params("team_id")
	credID := c.Params("credential_id")

	var cred models.Credential
	err := h.db.Where("id = ? AND team_id = ?", credID, teamID).First(&cred).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Credential not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	if err := h.db.Delete(&cred).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete credential"})
	}

	return c.JSON(fiber.Map{"status": "deleted"})
}
