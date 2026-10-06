package api

import (
	"encoding/json"
	"fmt"
	"time"

	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// WebhookHandlers handles inbound webhooks from external providers.
type WebhookHandlers struct {
	db               *gorm.DB
	idempotencyStore integrations.IdempotencyStore
	cipher           *crypto.SecretCipher
	queueClient      queue.Client
}

// NewWebhookHandlers creates a new WebhookHandlers instance.
func NewWebhookHandlers(
	db *gorm.DB,
	idempotencyStore integrations.IdempotencyStore,
	cipher *crypto.SecretCipher,
	queueClient queue.Client,
) *WebhookHandlers {
	return &WebhookHandlers{
		db:               db,
		idempotencyStore: idempotencyStore,
		cipher:           cipher,
		queueClient:      queueClient,
	}
}

type ghWebhookPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Issue *struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
	} `json:"issue"`
}

// HandleWebhook processes inbound webhook requests on /api/v1/webhooks/:provider.
func (h *WebhookHandlers) HandleWebhook(c fiber.Ctx) error {
	provider := c.Params("provider")
	if provider != string(models.IntegrationGitHub) &&
		provider != string(models.IntegrationJira) &&
		provider != string(models.IntegrationTrello) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "unsupported provider: " + provider})
	}

	payload := c.Body()

	switch provider {
	case string(models.IntegrationGitHub):
		return h.handleGitHubWebhook(c, payload)
	case string(models.IntegrationJira):
		return h.handleJiraWebhook(c, payload)
	case string(models.IntegrationTrello):
		return h.handleTrelloWebhook(c, payload)
	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "unsupported provider"})
	}
}

func (h *WebhookHandlers) handleGitHubWebhook(c fiber.Ctx, payload []byte) error {
	deliveryID := c.Get("X-GitHub-Delivery")
	signature := c.Get("X-Hub-Signature-256")

	var ghPayload ghWebhookPayload
	if err := json.Unmarshal(payload, &ghPayload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid json payload"})
	}

	repoName := ghPayload.Repository.FullName
	if repoName == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing repository full_name in payload"})
	}

	// Find ProjectIntegration for this repository
	var integration models.ProjectIntegration
	if err := h.db.Preload("Credential").Where("provider = ? AND external_id = ? AND enabled = ?", models.IntegrationGitHub, repoName, true).First(&integration).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no active integration found for repository " + repoName})
	}

	// Decrypt webhook secret
	plainSecret, err := h.cipher.Decrypt(integration.Credential.EncryptedData)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to decrypt integration credentials"})
	}

	// Verify HMAC signature
	if !github.VerifySignature(plainSecret, payload, signature) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid signature"})
	}

	if deliveryID == "" {
		deliveryID = uuid.NewString()
	}

	// Check idempotency store
	if h.idempotencyStore != nil {
		firstSeen, err := h.idempotencyStore.RecordEvent(c.Context(), string(models.IntegrationGitHub), integration.ProjectID, deliveryID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "idempotency check failed"})
		}
		if !firstSeen {
			// Duplicate delivery - safe acknowledge
			return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "duplicate", "delivery_id": deliveryID})
		}
	}

	// Process Issue opened or reopened
	if ghPayload.Issue != nil && (ghPayload.Action == "opened" || ghPayload.Action == "reopened" || ghPayload.Action == "labeled") {
		issue := ghPayload.Issue
		externalID := fmt.Sprintf("%d", issue.Number)

		// Check if task already exists
		var existingTask models.Task
		err := h.db.Where("project_id = ? AND external_id = ?", integration.ProjectID, externalID).First(&existingTask).Error
		if err == nil {
			// Task already exists, update title/prompt if open
			existingTask.Title = issue.Title
			existingTask.Prompt = issue.Body
			_ = h.db.Save(&existingTask).Error
		} else if err == gorm.ErrRecordNotFound {
			taskID := "task-" + uuid.NewString()
			integID := integration.ID
			task := models.Task{
				ID:            taskID,
				ProjectID:     integration.ProjectID,
				IntegrationID: &integID,
				ExternalID:    &externalID,
				ExternalURL:   &issue.HTMLURL,
				Title:         issue.Title,
				Prompt:        issue.Body,
				AssigneeID:    "agent",
				Status:        models.TaskStatusQueued,
				AssigneeType:  models.AssigneeAgent,
				Priority:      models.PriorityNormal,
				Skills:        models.StringArray{},
				MCPServers:    models.StringArray{},
				CreatedAt:     time.Now().UTC(),
				UpdatedAt:     time.Now().UTC(),
			}

			if err := h.db.Create(&task).Error; err != nil {
				if h.idempotencyStore != nil {
					_ = h.idempotencyStore.MarkFailed(c.Context(), string(models.IntegrationGitHub), integration.ProjectID, deliveryID, err.Error())
				}
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create task"})
			}

			// Enqueue task execution if queue client is configured
			if h.queueClient != nil {
				_, _ = h.queueClient.EnqueueTask(c.Context(), &task)
			}
		}
	}

	if h.idempotencyStore != nil {
		_ = h.idempotencyStore.MarkProcessed(c.Context(), string(models.IntegrationGitHub), integration.ProjectID, deliveryID)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "processed", "delivery_id": deliveryID})
}

func (h *WebhookHandlers) handleJiraWebhook(c fiber.Ctx, payload []byte) error {
	// Basic structure for Jira webhook handling
	eventID := c.Get("X-Atlassian-Webhook-Identifier")
	if eventID == "" {
		eventID = uuid.NewString()
	}

	var jiraPayload struct {
		WebhookEvent string `json:"webhookEvent"`
		Issue        struct {
			ID     string `json:"id"`
			Key    string `json:"key"`
			Fields struct {
				Summary     string `json:"summary"`
				Description any    `json:"description"`
				Project     struct {
					Key string `json:"key"`
				} `json:"project"`
			} `json:"fields"`
		} `json:"issue"`
	}

	if err := json.Unmarshal(payload, &jiraPayload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid json payload"})
	}

	projectKey := jiraPayload.Issue.Fields.Project.Key
	if projectKey == "" {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ignored", "reason": "missing project key"})
	}

	var integration models.ProjectIntegration
	if err := h.db.Where("provider = ? AND external_id = ? AND enabled = ?", models.IntegrationJira, projectKey, true).First(&integration).Error; err != nil {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ignored", "reason": "no active integration"})
	}

	if h.idempotencyStore != nil {
		firstSeen, err := h.idempotencyStore.RecordEvent(c.Context(), string(models.IntegrationJira), integration.ProjectID, eventID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "idempotency check failed"})
		}
		if !firstSeen {
			return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "duplicate", "event_id": eventID})
		}
		_ = h.idempotencyStore.MarkProcessed(c.Context(), string(models.IntegrationJira), integration.ProjectID, eventID)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "processed", "event_id": eventID})
}

func (h *WebhookHandlers) handleTrelloWebhook(c fiber.Ctx, payload []byte) error {
	// Head/Get webhook verification requests sent by Trello
	if c.Method() == fiber.MethodHead {
		return c.SendStatus(fiber.StatusOK)
	}

	var trelloPayload struct {
		Model struct {
			ID      string `json:"id"`
			IDBoard string `json:"idBoard"`
		} `json:"model"`
		Action struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"action"`
	}

	if err := json.Unmarshal(payload, &trelloPayload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid json payload"})
	}

	boardID := trelloPayload.Model.IDBoard
	if boardID == "" {
		boardID = trelloPayload.Model.ID
	}

	if boardID == "" {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ignored"})
	}

	eventID := trelloPayload.Action.ID
	if eventID == "" {
		eventID = uuid.NewString()
	}

	var integration models.ProjectIntegration
	if err := h.db.Where("provider = ? AND external_id = ? AND enabled = ?", models.IntegrationTrello, boardID, true).First(&integration).Error; err != nil {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "ignored", "reason": "no active integration"})
	}

	if h.idempotencyStore != nil {
		firstSeen, err := h.idempotencyStore.RecordEvent(c.Context(), string(models.IntegrationTrello), integration.ProjectID, eventID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "idempotency check failed"})
		}
		if !firstSeen {
			return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "duplicate", "event_id": eventID})
		}
		_ = h.idempotencyStore.MarkProcessed(c.Context(), string(models.IntegrationTrello), integration.ProjectID, eventID)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{"status": "processed", "event_id": eventID})
}
