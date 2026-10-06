package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrInvalidWebhookPayload = errors.New("invalid webhook payload")
	ErrIntegrationNotFound   = errors.New("active integration not found")
	ErrInvalidSignature       = errors.New("invalid webhook signature")
)

// WebhookService encapsulates external webhook verification, idempotency, and task creation.
type WebhookService interface {
	HandleGitHubWebhook(ctx context.Context, payload []byte, deliveryID, signature string) (*dto.WebhookResultDTO, error)
	HandleJiraWebhook(ctx context.Context, payload []byte, eventID string) (*dto.WebhookResultDTO, error)
	HandleTrelloWebhook(ctx context.Context, payload []byte, isHead bool) (*dto.WebhookResultDTO, error)
}

type webhookService struct {
	integrationRepo  repository.IntegrationRepository
	taskRepo         repository.TaskRepository
	idempotencyStore integrations.IdempotencyStore
	cipher           *crypto.SecretCipher
	queueClient      queue.Client
}

// NewWebhookService creates a new WebhookService implementation.
func NewWebhookService(
	integrationRepo repository.IntegrationRepository,
	taskRepo repository.TaskRepository,
	idempotencyStore integrations.IdempotencyStore,
	cipher *crypto.SecretCipher,
	queueClient queue.Client,
) WebhookService {
	return &webhookService{
		integrationRepo:  integrationRepo,
		taskRepo:         taskRepo,
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

func (s *webhookService) HandleGitHubWebhook(ctx context.Context, payload []byte, deliveryID, signature string) (*dto.WebhookResultDTO, error) {
	var ghPayload ghWebhookPayload
	if err := json.Unmarshal(payload, &ghPayload); err != nil {
		return nil, ErrInvalidWebhookPayload
	}

	repoName := ghPayload.Repository.FullName
	if repoName == "" {
		return nil, fmt.Errorf("%w: missing repository full_name", ErrInvalidWebhookPayload)
	}

	integration, err := s.integrationRepo.GetActiveByProviderAndExternalID(ctx, models.IntegrationGitHub, repoName)
	if err != nil {
		return nil, ErrIntegrationNotFound
	}

	if s.cipher == nil {
		return nil, errors.New("cipher not configured")
	}

	plainSecret, err := s.cipher.Decrypt(integration.Credential.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt integration secret: %w", err)
	}

	// Verify HMAC signature BEFORE any mutation or idempotency check
	if !github.VerifySignature(plainSecret, payload, signature) {
		return nil, ErrInvalidSignature
	}

	if deliveryID == "" {
		deliveryID = uuid.NewString()
	}

	if s.idempotencyStore != nil {
		firstSeen, err := s.idempotencyStore.RecordEvent(ctx, string(models.IntegrationGitHub), integration.ProjectID, deliveryID)
		if err != nil {
			return nil, fmt.Errorf("idempotency check failed: %w", err)
		}
		if !firstSeen {
			return &dto.WebhookResultDTO{
				Status:     "duplicate",
				DeliveryID: deliveryID,
			}, nil
		}
	}

	var taskIDCreated string
	if ghPayload.Issue != nil && (ghPayload.Action == "opened" || ghPayload.Action == "reopened" || ghPayload.Action == "labeled") {
		issue := ghPayload.Issue
		externalID := fmt.Sprintf("%d", issue.Number)

		existingTask, err := s.taskRepo.GetByProjectAndExternalID(ctx, integration.ProjectID, externalID)
		if err == nil && existingTask != nil {
			existingTask.Title = issue.Title
			existingTask.Prompt = issue.Body
			_ = s.taskRepo.Update(ctx, existingTask)
			taskIDCreated = existingTask.ID
		} else {
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

			if err := s.taskRepo.Create(ctx, &task); err != nil {
				if s.idempotencyStore != nil {
					_ = s.idempotencyStore.MarkFailed(ctx, string(models.IntegrationGitHub), integration.ProjectID, deliveryID, err.Error())
				}
				return nil, fmt.Errorf("failed to create task: %w", err)
			}
			taskIDCreated = task.ID

			if s.queueClient != nil {
				_, _ = s.queueClient.EnqueueTask(ctx, &task)
			}
		}
	}

	if s.idempotencyStore != nil {
		_ = s.idempotencyStore.MarkProcessed(ctx, string(models.IntegrationGitHub), integration.ProjectID, deliveryID)
	}

	return &dto.WebhookResultDTO{
		Status:     "processed",
		DeliveryID: deliveryID,
		TaskID:     taskIDCreated,
	}, nil
}

func (s *webhookService) HandleJiraWebhook(ctx context.Context, payload []byte, eventID string) (*dto.WebhookResultDTO, error) {
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
		return nil, ErrInvalidWebhookPayload
	}

	projectKey := jiraPayload.Issue.Fields.Project.Key
	if projectKey == "" {
		return &dto.WebhookResultDTO{
			Status:  "ignored",
			EventID: eventID,
			Reason:  "missing project key",
		}, nil
	}

	integration, err := s.integrationRepo.GetActiveByProviderAndExternalID(ctx, models.IntegrationJira, projectKey)
	if err != nil {
		return &dto.WebhookResultDTO{
			Status:  "ignored",
			EventID: eventID,
			Reason:  "no active integration",
		}, nil
	}

	if s.idempotencyStore != nil {
		firstSeen, err := s.idempotencyStore.RecordEvent(ctx, string(models.IntegrationJira), integration.ProjectID, eventID)
		if err != nil {
			return nil, fmt.Errorf("idempotency check failed: %w", err)
		}
		if !firstSeen {
			return &dto.WebhookResultDTO{
				Status:  "duplicate",
				EventID: eventID,
			}, nil
		}
		_ = s.idempotencyStore.MarkProcessed(ctx, string(models.IntegrationJira), integration.ProjectID, eventID)
	}

	return &dto.WebhookResultDTO{
		Status:  "processed",
		EventID: eventID,
	}, nil
}

func (s *webhookService) HandleTrelloWebhook(ctx context.Context, payload []byte, isHead bool) (*dto.WebhookResultDTO, error) {
	if isHead {
		return &dto.WebhookResultDTO{
			Status: "processed",
		}, nil
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
		return nil, ErrInvalidWebhookPayload
	}

	boardID := trelloPayload.Model.IDBoard
	if boardID == "" {
		boardID = trelloPayload.Model.ID
	}

	if boardID == "" {
		return &dto.WebhookResultDTO{
			Status: "ignored",
			Reason: "missing board id",
		}, nil
	}

	eventID := trelloPayload.Action.ID
	if eventID == "" {
		eventID = uuid.NewString()
	}

	integration, err := s.integrationRepo.GetActiveByProviderAndExternalID(ctx, models.IntegrationTrello, boardID)
	if err != nil {
		return &dto.WebhookResultDTO{
			Status:  "ignored",
			EventID: eventID,
			Reason:  "no active integration",
		}, nil
	}

	if s.idempotencyStore != nil {
		firstSeen, err := s.idempotencyStore.RecordEvent(ctx, string(models.IntegrationTrello), integration.ProjectID, eventID)
		if err != nil {
			return nil, fmt.Errorf("idempotency check failed: %w", err)
		}
		if !firstSeen {
			return &dto.WebhookResultDTO{
				Status:  "duplicate",
				EventID: eventID,
			}, nil
		}
		_ = s.idempotencyStore.MarkProcessed(ctx, string(models.IntegrationTrello), integration.ProjectID, eventID)
	}

	return &dto.WebhookResultDTO{
		Status:  "processed",
		EventID: eventID,
	}, nil
}
