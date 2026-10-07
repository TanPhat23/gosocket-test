package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/git"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/ws"
)

// TaskRunner is aliased from queue.TaskRunner.
type TaskRunner = queue.TaskRunner

// TaskService defines the business operations for tasks.
type TaskService interface {
	GetTask(ctx context.Context, taskID string) (*models.Task, error)
	ListTasks(ctx context.Context, projectID string) ([]models.Task, error)
	ListSubtasks(ctx context.Context, parentTaskID string) ([]models.Task, error)
	CreateTask(ctx context.Context, req *dto.CreateTaskRequest) (*models.Task, error)
	UpdateStatus(ctx context.Context, taskID string, status models.TaskStatus) error
	CancelTask(ctx context.Context, taskID string) error
	ExecuteTask(ctx context.Context, taskID string, runner TaskRunner) error
	CreatePullRequestForTask(ctx context.Context, taskID string) (*models.Task, error)
	GetTaskSession(ctx context.Context, taskID, userID string) (*dto.TaskSessionResponseDTO, error)
	PromptTaskSession(ctx context.Context, taskID, userID string, req *dto.TaskSessionPromptRequest) error
	CompactTaskSession(ctx context.Context, taskID, userID string) error
}

type taskService struct {
	taskRepo        repository.TaskRepository
	attemptRepo     repository.AttemptRepository
	queueClient     queue.Client
	hub             *ws.Hub
	integRepo       repository.IntegrationRepository
	credRepo        repository.CredentialRepository
	cipher          *crypto.SecretCipher
	ghProvider      integrations.PullRequestProvider
	projectRepo     repository.ProjectRepository
	teamRepo        repository.TeamRepository
	openCodeClient  runner.OpenCodeClient
}

// TaskServiceOption allows configuring optional dependencies for TaskService.
type TaskServiceOption func(*taskService)

// WithTaskGitHubIntegration configures GitHub PR dependencies.
func WithTaskGitHubIntegration(
	integRepo repository.IntegrationRepository,
	credRepo repository.CredentialRepository,
	cipher *crypto.SecretCipher,
	ghProvider integrations.PullRequestProvider,
) TaskServiceOption {
	return func(s *taskService) {
		s.integRepo = integRepo
		s.credRepo = credRepo
		s.cipher = cipher
		s.ghProvider = ghProvider
	}
}

// WithTaskSessionDependencies configures OpenCode client, project repository, and team repository for session operations.
func WithTaskSessionDependencies(
	openCodeClient runner.OpenCodeClient,
	projectRepo repository.ProjectRepository,
	teamRepo repository.TeamRepository,
) TaskServiceOption {
	return func(s *taskService) {
		s.openCodeClient = openCodeClient
		s.projectRepo = projectRepo
		s.teamRepo = teamRepo
	}
}

// NewTaskService creates a new TaskService implementation.
func NewTaskService(
	taskRepo repository.TaskRepository,
	attemptRepo repository.AttemptRepository,
	queueClient queue.Client,
	hub *ws.Hub,
	opts ...TaskServiceOption,
) TaskService {
	svc := &taskService{
		taskRepo:    taskRepo,
		attemptRepo: attemptRepo,
		queueClient: queueClient,
		hub:         hub,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// GetTask retrieves a task by ID.
func (s *taskService) GetTask(ctx context.Context, taskID string) (*models.Task, error) {
	return s.taskRepo.GetByID(ctx, taskID)
}

// ListTasks lists tasks, optionally filtered by project ID.
func (s *taskService) ListTasks(ctx context.Context, projectID string) ([]models.Task, error) {
	return s.taskRepo.List(ctx, projectID)
}

// ListSubtasks lists subtasks for a given parent task ID.
func (s *taskService) ListSubtasks(ctx context.Context, parentTaskID string) ([]models.Task, error) {
	return s.taskRepo.ListByParentID(ctx, parentTaskID)
}

// CreateTask validates input, creates a new task in DB, enqueues to Asynq queue if queued, and broadcasts status via WebSocket.
func (s *taskService) CreateTask(ctx context.Context, req *dto.CreateTaskRequest) (*models.Task, error) {
	if req == nil {
		return nil, errors.New("request cannot be nil")
	}

	priority := models.TaskPriority(req.Priority)
	if !priority.IsValid() {
		priority = models.PriorityNormal
	}

	taskID := common.NewID()
	sessionID := fmt.Sprintf("ses_atm_%s", taskID)

	task := &models.Task{
		ID:           taskID,
		ProjectID:    req.ProjectID,
		ParentTaskID: req.ParentTaskID,
		Title:        req.Title,
		Prompt:       req.Prompt,
		AssigneeID:   req.AssigneeID,
		AssigneeType: req.AssigneeType,
		Priority:     priority,
		Model:        req.Model,
		Skills:       models.StringArray(req.Skills),
		MCPServers:   models.StringArray(req.MCPServers),
		SessionID:    &sessionID,
		Status:       models.TaskStatusQueued,
	}

	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	if s.queueClient != nil {
		asynqID, err := s.queueClient.EnqueueTask(ctx, task)
		if err == nil {
			task.AsynqTaskID = &asynqID
			_ = s.taskRepo.Update(ctx, task)
		}
	}

	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(task.ID, task.Status)
	}

	return task, nil
}

// UpdateStatus updates the task status in the database and broadcasts the change via WebSocket.
func (s *taskService) UpdateStatus(ctx context.Context, taskID string, status models.TaskStatus) error {
	if !status.IsValid() {
		return fmt.Errorf("invalid status: %s", status)
	}

	if err := s.taskRepo.UpdateStatus(ctx, taskID, status); err != nil {
		return fmt.Errorf("task not found or update failed: %w", err)
	}

	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(taskID, status)
	}

	return nil
}

// CancelTask cancels an ongoing or queued task, notifying the queue client and updating DB status.
func (s *taskService) CancelTask(ctx context.Context, taskID string) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if s.queueClient != nil && task.AsynqTaskID != nil && *task.AsynqTaskID != "" {
		tier := queue.ResolveQueueTier(task.Priority)
		_ = s.queueClient.CancelTask(ctx, tier, *task.AsynqTaskID)
	}

	return s.UpdateStatus(ctx, taskID, models.TaskStatusCancelled)
}

// ExecuteTask orchestrates the task execution lifecycle: status transition, attempt tracking, runner execution, and status completion.
func (s *taskService) ExecuteTask(ctx context.Context, taskID string, runner TaskRunner) error {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to find task %q: %w", taskID, err)
	}

	// If cancelled, skip execution
	if task.Status == models.TaskStatusCancelled {
		return nil
	}

	// Ensure task.SessionID is initialized
	if task.SessionID == nil || *task.SessionID == "" {
		sesID := fmt.Sprintf("ses_atm_%s", task.ID)
		task.SessionID = &sesID
	}

	// Update task status to in_progress and record heartbeat
	now := time.Now().UTC()
	task.Status = models.TaskStatusInProgress
	task.HeartbeatAt = &now
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return fmt.Errorf("failed to mark task as in_progress: %w", err)
	}
	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(task.ID, models.TaskStatusInProgress)
	}

	// Count existing attempts for this task
	count, err := s.attemptRepo.CountByTaskID(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("failed to count task attempts: %w", err)
	}

	startedAt := time.Now().UTC()
	attempt := &models.TaskAttempt{
		ID:            common.NewID(),
		TaskID:        task.ID,
		AttemptNumber: int(count) + 1,
		Status:        models.AttemptRunning,
		StartedAt:     &startedAt,
		SessionID:     task.SessionID,
	}
	if err := s.attemptRepo.Create(ctx, attempt); err != nil {
		return fmt.Errorf("failed to create task attempt: %w", err)
	}

	var runErr error
	if runner != nil {
		runErr = runner.RunTask(ctx, task, attempt)
	}

	finishedAt := time.Now().UTC()
	attempt.FinishedAt = &finishedAt

	if runErr == nil {
		attempt.Status = models.AttemptSucceeded
		_ = s.attemptRepo.Update(ctx, attempt)
		_ = s.UpdateStatus(ctx, task.ID, models.TaskStatusReview)
		return nil
	}

	failReason := runErr.Error()
	attempt.Status = models.AttemptFailed
	attempt.FailureReason = &failReason
	_ = s.attemptRepo.Update(ctx, attempt)
	_ = s.UpdateStatus(ctx, task.ID, models.TaskStatusFailed)

	return runErr
}

// CreatePullRequestForTask manually pushes the task's worktree branch to remote origin and opens a Pull Request.
func (s *taskService) CreatePullRequestForTask(ctx context.Context, taskID string) (*models.Task, error) {
	if s.integRepo == nil || s.credRepo == nil || s.cipher == nil || s.ghProvider == nil {
		return nil, errors.New("github integration dependencies not configured")
	}

	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	if task.PRURL != nil && *task.PRURL != "" {
		task.PullRequestURL = task.PRURL
		return task, nil
	}

	if task.WorktreePath == nil || *task.WorktreePath == "" {
		return nil, errors.New("task has no associated worktree")
	}

	// Lookup active GitHub integration for project
	integrationsList, err := s.integRepo.ListByProjectID(ctx, task.ProjectID)
	if err != nil || len(integrationsList) == 0 {
		return nil, errors.New("no integrations found for project")
	}

	var ghInteg *models.ProjectIntegration
	for i := range integrationsList {
		if integrationsList[i].Provider == models.IntegrationGitHub && integrationsList[i].Enabled {
			ghInteg = &integrationsList[i]
			break
		}
	}
	if ghInteg == nil {
		return nil, errors.New("no active GitHub integration found for project")
	}

	// Fetch & Decrypt PAT
	cred, err := s.credRepo.GetByID(ctx, ghInteg.CredentialID)
	if err != nil {
		return nil, fmt.Errorf("credential lookup failed: %w", err)
	}

	token, err := s.cipher.Decrypt(cred.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt PAT: %w", err)
	}

	branchName := fmt.Sprintf("task/%s", task.ID)
	if task.BranchName != nil && *task.BranchName != "" {
		branchName = *task.BranchName
	}
	remoteURL := fmt.Sprintf("https://github.com/%s.git", ghInteg.ExternalID)

	// Push branch
	if err := git.PushBranch(ctx, *task.WorktreePath, remoteURL, token, branchName); err != nil {
		return nil, fmt.Errorf("git push failed: %w", err)
	}

	// PR title & body
	prTitle := task.Title
	if prTitle == "" {
		prTitle = fmt.Sprintf("Task #%s", task.ID)
	}

	var prBody strings.Builder
	prBody.WriteString(fmt.Sprintf("## Summary\nAutonomous implementation generated by Agent Task Manager for task `#%s`.\n\n", task.ID))
	prBody.WriteString(fmt.Sprintf("**Prompt**:\n> %s\n\n", task.Prompt))
	if task.ExternalID != nil && *task.ExternalID != "" {
		prBody.WriteString(fmt.Sprintf("Resolves #%s\n", *task.ExternalID))
	}

	prURL, err := s.ghProvider.CreatePullRequest(ctx, ghInteg, token, branchName, prTitle, prBody.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create Pull Request: %w", err)
	}

	task.PRURL = &prURL
	task.PullRequestURL = &prURL
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to update task with PR URL: %w", err)
	}

	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(task.ID, task.Status)
	}

	// If linked to an issue, post comment and update label
	if task.ExternalID != nil && *task.ExternalID != "" {
		if syncProv, ok := s.ghProvider.(integrations.SyncProvider); ok {
			commentMsg := fmt.Sprintf("Autonomous agent finished implementation. Pull Request: %s", prURL)
			_ = syncProv.AddComment(ctx, ghInteg, token, *task.ExternalID, commentMsg)
			_ = syncProv.UpdateIssueStatus(ctx, ghInteg, token, *task.ExternalID, "review:agent")
		}
	}

	return task, nil
}

// authorizeUserForTask verifies that task exists, resolves its project and team,
// and confirms the requesting user has at least the requiredRole on that team.
func (s *taskService) authorizeUserForTask(ctx context.Context, taskID, userID string, requiredRole models.TeamRole) (*models.Task, error) {
	if taskID == "" {
		return nil, errors.New("task ID is required")
	}
	if userID == "" {
		return nil, common.ErrForbidden
	}

	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}

	if s.projectRepo != nil && s.teamRepo != nil {
		project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("failed to load project for task: %w", err)
		}

		membership, err := s.teamRepo.GetMembership(ctx, project.TeamID, userID)
		if err != nil {
			return nil, common.ErrForbidden
		}

		if !auth.HasRolePermission(membership.Role, requiredRole) {
			return nil, common.ErrForbidden
		}
	}

	return task, nil
}

// GetTaskSession retrieves metadata and message history for an ongoing or completed task session.
// AC-SEC-02: Requires RoleViewer or higher.
func (s *taskService) GetTaskSession(ctx context.Context, taskID, userID string) (*dto.TaskSessionResponseDTO, error) {
	task, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleViewer)
	if err != nil {
		return nil, err
	}

	if s.openCodeClient == nil {
		return nil, common.ErrServiceUnavailable
	}

	if task.SessionID == nil || *task.SessionID == "" {
		return nil, errors.New("task has no associated session")
	}

	sessionID := *task.SessionID
	sessDTO, err := s.openCodeClient.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	messages, err := s.openCodeClient.GetSessionMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	res := &dto.TaskSessionResponseDTO{
		SessionID: sessionID,
		TaskID:    task.ID,
		Title:     sessDTO.Title,
		Directory: sessDTO.Directory,
		CreatedAt: sessDTO.CreatedAt,
		UpdatedAt: sessDTO.UpdatedAt,
		Messages:  messages,
	}

	return res, nil
}

// PromptTaskSession sends a targeted prompt into the task's existing OpenCode session.
// AC-SEC-02: Requires RoleMember or higher.
func (s *taskService) PromptTaskSession(ctx context.Context, taskID, userID string, req *dto.TaskSessionPromptRequest) error {
	if req == nil || strings.TrimSpace(req.Prompt) == "" {
		return errors.New("prompt cannot be empty")
	}

	task, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleMember)
	if err != nil {
		return err
	}

	if s.openCodeClient == nil {
		return common.ErrServiceUnavailable
	}

	if task.SessionID == nil || *task.SessionID == "" {
		return errors.New("task has no associated session")
	}

	return s.openCodeClient.PromptSession(ctx, *task.SessionID, req.Prompt)
}

// CompactTaskSession triggers context compaction on the task's OpenCode session.
// AC-SEC-02: Requires RoleMember or higher.
func (s *taskService) CompactTaskSession(ctx context.Context, taskID, userID string) error {
	task, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleMember)
	if err != nil {
		return err
	}

	if s.openCodeClient == nil {
		return common.ErrServiceUnavailable
	}

	if task.SessionID == nil || *task.SessionID == "" {
		return errors.New("task has no associated session")
	}

	return s.openCodeClient.CompactSession(ctx, *task.SessionID)
}

// LogService defines the business operations for appending and streaming task logs.
type LogService interface {
	AppendLog(ctx context.Context, taskID string, attemptID *string, chunk string) error
	GetLogs(ctx context.Context, taskID string) ([]models.TaskLog, error)
}

type logService struct {
	logRepo repository.LogRepository
	hub     *ws.Hub
}

// NewLogService creates a new LogService implementation.
func NewLogService(logRepo repository.LogRepository, hub *ws.Hub) LogService {
	return &logService{
		logRepo: logRepo,
		hub:     hub,
	}
}

// AppendLog persists a chunk of log to TaskLog and broadcasts to WebSocket subscribers.
func (s *logService) AppendLog(ctx context.Context, taskID string, attemptID *string, chunk string) error {
	logEntry := models.TaskLog{
		TaskID:    taskID,
		AttemptID: attemptID,
		LogChunk:  chunk,
		Timestamp: time.Now().UTC(),
	}

	if err := s.logRepo.Append(ctx, &logEntry); err != nil {
		return fmt.Errorf("failed to persist log entry: %w", err)
	}

	if s.hub != nil {
		_ = s.hub.BroadcastTaskLog(taskID, chunk)
	}

	return nil
}

// GetLogs returns all persisted log entries for a given task ID ordered by timestamp.
func (s *logService) GetLogs(ctx context.Context, taskID string) ([]models.TaskLog, error) {
	return s.logRepo.ListByTaskID(ctx, taskID)
}
