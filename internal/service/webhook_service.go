package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/ws"
	"github.com/google/uuid"
)

var (
	ErrInvalidWebhookPayload = errors.New("invalid webhook payload")
	ErrIntegrationNotFound   = errors.New("active integration not found")
	ErrInvalidSignature       = errors.New("invalid webhook signature")
	ErrMissingDeliveryID      = errors.New("missing webhook delivery id")
)

// WebhookService encapsulates external webhook verification, idempotency, and task creation.
type WebhookService interface {
	HandleGitHubWebhook(ctx context.Context, payload []byte, deliveryID, signature string) (*dto.WebhookResultDTO, error)
	HandleJiraWebhook(ctx context.Context, payload []byte, eventID string) (*dto.WebhookResultDTO, error)
	HandleTrelloWebhook(ctx context.Context, payload []byte, isHead bool) (*dto.WebhookResultDTO, error)
}

type webhookService struct {
	integrationRepo  repository.IntegrationRepository
	credRepo         repository.CredentialRepository
	taskRepo         repository.TaskRepository
	idempotencyStore integrations.IdempotencyStore
	cipher           *crypto.SecretCipher
	queueClient      queue.Client
	hub              *ws.Hub
	ghProvider       integrations.SyncProvider
}

// WebhookOption configures optional WebhookService dependencies.
type WebhookOption func(*webhookService)

// WithWebhookHub configures WebSocket hub broadcasting for task updates.
func WithWebhookHub(hub *ws.Hub) WebhookOption {
	return func(s *webhookService) {
		s.hub = hub
	}
}

// WithWebhookSyncProvider configures external sync provider for updating issues on webhook events.
func WithWebhookSyncProvider(ghProvider integrations.SyncProvider) WebhookOption {
	return func(s *webhookService) {
		s.ghProvider = ghProvider
	}
}

// NewWebhookService creates a new WebhookService implementation.
func NewWebhookService(
	integrationRepo repository.IntegrationRepository,
	credRepo repository.CredentialRepository,
	taskRepo repository.TaskRepository,
	idempotencyStore integrations.IdempotencyStore,
	cipher *crypto.SecretCipher,
	queueClient queue.Client,
	opts ...WebhookOption,
) WebhookService {
	s := &webhookService{
		integrationRepo:  integrationRepo,
		credRepo:         credRepo,
		taskRepo:         taskRepo,
		idempotencyStore: idempotencyStore,
		cipher:           cipher,
		queueClient:      queueClient,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type ghWebhookPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Issue *struct {
		Number  int      `json:"number"`
		Title   string   `json:"title"`
		Body    string   `json:"body"`
		State   string   `json:"state"`
		HTMLURL string   `json:"html_url"`
		Labels  []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
	CheckRun *struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
		CheckSuite struct {
			HeadBranch string `json:"head_branch"`
		} `json:"check_suite"`
		Output struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
			Text    string `json:"text"`
		} `json:"output"`
	} `json:"check_run"`
	WorkflowRun *struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadBranch string `json:"head_branch"`
		HeadSHA    string `json:"head_sha"`
		HTMLURL    string `json:"html_url"`
	} `json:"workflow_run"`
	PullRequest *struct {
		Number   int    `json:"number"`
		Title    string `json:"title"`
		State    string `json:"state"`
		Merged   bool   `json:"merged"`
		MergedAt string `json:"merged_at"`
		HTMLURL  string `json:"html_url"`
		Head     struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
}

type jiraWebhookPayload struct {
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

type trelloWebhookPayload struct {
	Model struct {
		ID      string `json:"id"`
		IDBoard string `json:"idBoard"`
	} `json:"model"`
	Action struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"action"`
}

func (s *webhookService) HandleGitHubWebhook(ctx context.Context, payload []byte, deliveryID, signature string) (*dto.WebhookResultDTO, error) {
	// Security Fix 5: require deliveryID != "" (do NOT generate fresh UUID allowing replay attacks)
	if deliveryID == "" {
		return nil, ErrMissingDeliveryID
	}

	ghPayload, err := common.Unmarshal[ghWebhookPayload](payload)
	if err != nil {
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

	// Secret resolution: check integration.Config["webhook_secret"] first; fallback to credential PAT
	var hmacSecret string
	if integration.Config != nil {
		if sec, ok := integration.Config["webhook_secret"].(string); ok && sec != "" {
			hmacSecret = sec
		}
	}

	if hmacSecret == "" {
		if s.cipher == nil {
			return nil, errors.New("cipher not configured")
		}

		cred, err := s.credRepo.GetByID(ctx, integration.CredentialID)
		if err != nil {
			return nil, fmt.Errorf("failed to load integration credential: %w", err)
		}

		plainSecret, err := s.cipher.Decrypt(cred.EncryptedData)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt integration secret: %w", err)
		}
		hmacSecret = plainSecret
	}

	// Verify HMAC signature BEFORE any mutation or idempotency check
	if !github.VerifySignature(hmacSecret, payload, signature) {
		return nil, ErrInvalidSignature
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

		// Check filter_label if configured in integration config
		filterLabel := ""
		if integration.Config != nil {
			if fl, ok := integration.Config["filter_label"].(string); ok && fl != "" {
				filterLabel = fl
			}
		}

		hasFilterLabel := filterLabel == ""
		if filterLabel != "" {
			for _, lbl := range issue.Labels {
				if lbl.Name == filterLabel {
					hasFilterLabel = true
					break
				}
			}
		}

		if hasFilterLabel {
			externalID := fmt.Sprintf("%d", issue.Number)

			existingTask, err := s.taskRepo.GetByProjectAndExternalID(ctx, integration.ProjectID, externalID)
			if err == nil && existingTask != nil {
				existingTask.Title = issue.Title
				existingTask.Prompt = issue.Body
				_ = s.taskRepo.Update(ctx, existingTask)
				taskIDCreated = existingTask.ID
			} else {
				taskID := common.NewID()
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
	}

	// 2. Handle GitHub Actions check_run failure feedback loop (ADR-004 Phase 3)
	if ghPayload.CheckRun != nil {
		checkRun := ghPayload.CheckRun
		if checkRun.Conclusion != "failure" {
			if s.idempotencyStore != nil {
				_ = s.idempotencyStore.MarkProcessed(ctx, string(models.IntegrationGitHub), integration.ProjectID, deliveryID)
			}
			return &dto.WebhookResultDTO{
				Status:     "ignored",
				DeliveryID: deliveryID,
			}, nil
		}

		headBranch := checkRun.CheckSuite.HeadBranch
		task := s.findTaskByBranch(ctx, integration.ProjectID, headBranch)
		if task != nil {
			diagOutput := checkRun.Output.Text
			if diagOutput == "" {
				diagOutput = checkRun.Output.Summary
			}
			if len(diagOutput) > 16*1024 {
				diagOutput = diagOutput[:16*1024]
			}

			feedbackPrompt := fmt.Sprintf(
				"Remote CI check '%s' failed on commit %s.\n\n"+
					"Title: %s\nSummary: %s\nDetails:\n```\n%s\n```\n\n"+
					"Please review the failure details in the worktree, correct the issues, and ensure all checks succeed.",
				checkRun.Name,
				checkRun.HeadSHA,
				checkRun.Output.Title,
				checkRun.Output.Summary,
				diagOutput,
			)

			task.Prompt = feedbackPrompt
			task.Status = models.TaskStatusQueued
			task.UpdatedAt = time.Now().UTC()
			_ = s.taskRepo.Update(ctx, task)

			if s.queueClient != nil {
				_, _ = s.queueClient.EnqueueTask(ctx, task)
			}

			taskIDCreated = task.ID
		}
	}

	// 3. Handle GitHub Actions workflow_run failure feedback loop (ADR-004 Phase 3)
	if ghPayload.WorkflowRun != nil {
		wfRun := ghPayload.WorkflowRun
		if wfRun.Conclusion != "failure" {
			if s.idempotencyStore != nil {
				_ = s.idempotencyStore.MarkProcessed(ctx, string(models.IntegrationGitHub), integration.ProjectID, deliveryID)
			}
			return &dto.WebhookResultDTO{
				Status:     "ignored",
				DeliveryID: deliveryID,
			}, nil
		}

		headBranch := wfRun.HeadBranch
		task := s.findTaskByBranch(ctx, integration.ProjectID, headBranch)
		if task != nil {
			feedbackPrompt := fmt.Sprintf(
				"Remote CI workflow '%s' failed on commit %s.\n\n"+
					"Workflow URL: %s\n\n"+
					"Please review the workflow logs, correct the issues in the worktree, and ensure all checks succeed.",
				wfRun.Name,
				wfRun.HeadSHA,
				wfRun.HTMLURL,
			)

			task.Prompt = feedbackPrompt
			task.Status = models.TaskStatusQueued
			task.UpdatedAt = time.Now().UTC()
			_ = s.taskRepo.Update(ctx, task)

			if s.queueClient != nil {
				_, _ = s.queueClient.EnqueueTask(ctx, task)
			}

			taskIDCreated = task.ID
		}
	}

	// 4. Handle GitHub Pull Request merge event
	if ghPayload.PullRequest != nil {
		pr := ghPayload.PullRequest
		if (ghPayload.Action == "closed" || ghPayload.Action == "merged") && pr.Merged {
			task := s.findTaskByBranch(ctx, integration.ProjectID, pr.Head.Ref)
			if task == nil && pr.HTMLURL != "" {
				task = s.findTaskByPRURL(ctx, integration.ProjectID, pr.HTMLURL)
			}
			if task != nil {
				task.Status = models.TaskStatusDone
				task.UpdatedAt = time.Now().UTC()
				_ = s.taskRepo.Update(ctx, task)

				if s.hub != nil {
					_ = s.hub.BroadcastTaskStatus(task.ID, models.TaskStatusDone)
				}
				taskIDCreated = task.ID

				// Closing the linked GitHub issue (PAT decrypt + GitHub writes) is disabled pending
				// Phase 1B/Gates 0C/0D/0G; see closeLinkedIssueOnMergeDisabledDraft.
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

// closeLinkedIssueOnMergeDisabledDraft is the original merged-PR issue sync, intentionally unreferenced.
func (s *webhookService) closeLinkedIssueOnMergeDisabledDraft(ctx context.Context, integration *models.ProjectIntegration, task *models.Task, prHTMLURL string) {
	// If linked to an issue, close issue on GitHub and post completion comment
	if task.ExternalID != nil && *task.ExternalID != "" && s.ghProvider != nil && s.cipher != nil {
		cred, _ := s.credRepo.GetByID(ctx, integration.CredentialID)
		if cred != nil {
			token, _ := s.cipher.Decrypt(cred.EncryptedData)
			if token != "" {
				_ = s.ghProvider.UpdateIssueStatus(ctx, integration, token, *task.ExternalID, "closed")
				_ = s.ghProvider.AddComment(ctx, integration, token, *task.ExternalID, fmt.Sprintf("Pull Request %s was merged. Task marked as completed.", prHTMLURL))
			}
		}
	}
}

func (s *webhookService) findTaskByBranch(ctx context.Context, projectID, branchName string) *models.Task {
	if branchName == "" {
		return nil
	}

	// 1. If branch matches convention task/<task_id>
	if strings.HasPrefix(branchName, "task/") {
		taskID := strings.TrimPrefix(branchName, "task/")
		if t, err := s.taskRepo.GetByID(ctx, taskID); err == nil && t != nil {
			if t.ProjectID == projectID {
				return t
			}
		}
	}

	// 2. Scan tasks in project by BranchName
	tasks, err := s.taskRepo.List(ctx, projectID)
	if err == nil {
		for i := range tasks {
			if tasks[i].BranchName != nil && *tasks[i].BranchName == branchName {
				return &tasks[i]
			}
		}
	}

	return nil
}

func (s *webhookService) findTaskByPRURL(ctx context.Context, projectID, prURL string) *models.Task {
	if prURL == "" {
		return nil
	}
	tasks, err := s.taskRepo.List(ctx, projectID)
	if err == nil {
		for i := range tasks {
			if tasks[i].PRURL != nil && *tasks[i].PRURL == prURL {
				return &tasks[i]
			}
		}
	}
	return nil
}

func (s *webhookService) HandleJiraWebhook(ctx context.Context, payload []byte, eventID string) (*dto.WebhookResultDTO, error) {
	if eventID == "" {
		eventID = uuid.NewString()
	}

	jiraPayload, err := common.Unmarshal[jiraWebhookPayload](payload)
	if err != nil {
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

	trelloPayload, err := common.Unmarshal[trelloWebhookPayload](payload)
	if err != nil {
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
