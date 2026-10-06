package api

import (
	"errors"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

// WebhookHandlers handles inbound webhooks from external providers.
type WebhookHandlers struct {
	webhookService service.WebhookService
}

// NewWebhookHandlers creates a new WebhookHandlers instance.
func NewWebhookHandlers(webhookService service.WebhookService) *WebhookHandlers {
	return &WebhookHandlers{
		webhookService: webhookService,
	}
}

// HandleWebhook processes inbound webhook requests on /api/v1/webhooks/:provider.
func (h *WebhookHandlers) HandleWebhook(c fiber.Ctx) error {
	provider := c.Params("provider")
	payload := c.Body()

	switch provider {
	case string(models.IntegrationGitHub):
		deliveryID := c.Get("X-GitHub-Delivery")
		signature := c.Get("X-Hub-Signature-256")
		res, err := h.webhookService.HandleGitHubWebhook(c.Context(), payload, deliveryID, signature)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrInvalidWebhookPayload):
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
			case errors.Is(err, service.ErrIntegrationNotFound):
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no active integration found"})
			case errors.Is(err, service.ErrInvalidSignature):
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid signature"})
			default:
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			}
		}
		return c.Status(fiber.StatusOK).JSON(res)

	case string(models.IntegrationJira):
		eventID := c.Get("X-Atlassian-Webhook-Identifier")
		res, err := h.webhookService.HandleJiraWebhook(c.Context(), payload, eventID)
		if err != nil {
			if errors.Is(err, service.ErrInvalidWebhookPayload) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusOK).JSON(res)

	case string(models.IntegrationTrello):
		isHead := c.Method() == fiber.MethodHead
		res, err := h.webhookService.HandleTrelloWebhook(c.Context(), payload, isHead)
		if err != nil {
			if errors.Is(err, service.ErrInvalidWebhookPayload) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
		if isHead {
			return c.SendStatus(fiber.StatusOK)
		}
		var responseDTO *dto.WebhookResultDTO = res
		return c.Status(fiber.StatusOK).JSON(responseDTO)

	default:
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "unsupported provider"})
	}
}
