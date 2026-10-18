package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"sync"
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
	"gorm.io/gorm"
)

// TaskRunner is aliased from queue.TaskRunner.
type TaskRunner = queue.TaskRunner

// TaskService defines the business operations for tasks.
type TaskService interface {
	GetTask(ctx context.Context, taskID, userID string) (*models.Task, error)
	ListTasks(ctx context.Context, projectID, userID string) ([]models.Task, error)
	ListSubtasks(ctx context.Context, parentTaskID, userID string) ([]models.Task, error)
	CreateTask(ctx context.Context, req *dto.CreateTaskRequest, userID string) (*models.Task, error)
	UpdateStatus(ctx context.Context, taskID string, status models.TaskStatus) error
	CancelTask(ctx context.Context, taskID, userID string) error
	ExecuteTask(ctx context.Context, taskID string, runner TaskRunner) error
	CreatePullRequestForTask(ctx context.Context, taskID, userID string) (*models.Task, error)
	GetTaskSession(ctx context.Context, taskID, userID string) (*dto.TaskSessionResponseDTO, error)
	PromptTaskSession(ctx context.Context, taskID, userID string, req *dto.TaskSessionPromptRequest) error
	CompactTaskSession(ctx context.Context, taskID, userID string) error
	InterruptTaskSession(ctx context.Context, taskID, userID string) error
	GetTaskDiff(ctx context.Context, taskID, userID string) (string, error)
	SyncTaskPRStatus(ctx context.Context, taskID, userID string) (*models.Task, error)
	ReconcileActivePRs(ctx context.Context) error
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
	sessionClients  SessionClientResolver
	steerFinalizer  SteerFinalizer
	publishPR       PullRequestPublisher
	readTaskDiff    TaskDiffReader
	logRepo         repository.LogRepository
	simulated       bool

	activeCancels *common.SafeMap[string, context.CancelFunc]
	completionMu  sync.Mutex
}

// TaskServiceOption allows configuring optional dependencies for TaskService.
type TaskServiceOption func(*taskService)

// WithTaskSimulatedRuntime marks the service as running against the development fake runtime, so
// session reads report a simulated run instead of failing as unavailable.
func WithTaskSimulatedRuntime(simulated bool) TaskServiceOption {
	return func(s *taskService) { s.simulated = simulated }
}

// SessionClientResolver returns an OpenCode client bound to the running sandbox of a project. It must fail
// when the project has no running container; the control plane never talks to a host OpenCode.
type SessionClientResolver func(ctx context.Context, projectID string) (runner.OpenCodeClient, error)

// WithTaskSessionClientResolver wires the per-project OpenCode client used by the session tunnel.
func WithTaskSessionClientResolver(resolve SessionClientResolver) TaskServiceOption {
	return func(s *taskService) { s.sessionClients = resolve }
}

// SteerFinalizer finishes a steered turn: it waits for the turn that started after `before`, commits and
// pushes the result. logf receives terminal-formatted progress lines. It must not change the task status.
type SteerFinalizer func(ctx context.Context, task *models.Task, before *time.Time, logf func(string)) error

// WithTaskSteerFinalizer wires the post-turn work for steering. Without it a steer only delivers the prompt.
func WithTaskSteerFinalizer(fin SteerFinalizer) TaskServiceOption {
	return func(s *taskService) { s.steerFinalizer = fin }
}

// PullRequestPublisher pushes a finished task's branch and opens its pull request, returning the PR URL.
// It runs in the project container's runtime; logf receives terminal-formatted progress lines.
type PullRequestPublisher func(ctx context.Context, task *models.Task, logf func(string)) (string, error)

// TaskDiffReader returns the task's unified diff, read in the project container.
type TaskDiffReader func(ctx context.Context, task *models.Task) (string, error)

// WithTaskPullRequestPublisher wires the manual pull request and diff paths to the task runner. Without it
// both refuse with the runtime-unavailable error.
func WithTaskPullRequestPublisher(publish PullRequestPublisher, diff TaskDiffReader) TaskServiceOption {
	return func(s *taskService) {
		s.publishPR = publish
		s.readTaskDiff = diff
	}
}

// steerTimeout bounds one steered turn including its commit and push.
const steerTimeout = 30 * time.Minute

// WithTaskLogRepository configures the log repository for task log persistence.
func WithTaskLogRepository(logRepo repository.LogRepository) TaskServiceOption {
	return func(s *taskService) {
		s.logRepo = logRepo
	}
}

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
		taskRepo:      taskRepo,
		attemptRepo:   attemptRepo,
		queueClient:   queueClient,
		hub:           hub,
		activeCancels: common.NewSafeMap[string, context.CancelFunc](),
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// GetTask retrieves a task by ID. Requires RoleViewer or higher on the task's team.
func (s *taskService) GetTask(ctx context.Context, taskID, userID string) (*models.Task, error) {
	task, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleViewer)
	if err != nil {
		return nil, err
	}
	if task.Status == models.TaskStatusFailed && s.attemptRepo != nil {
		// Best effort: a missing reason must not hide the task itself.
		if reason, rerr := s.attemptRepo.LatestFailureReason(ctx, task.ID); rerr == nil {
			task.FailureReason = reason
		}
	}
	return task, nil
}

// ListTasks lists tasks visible to userID (RoleViewer or higher on the project's team).
// With a projectID it authorizes that project's team and fails with ErrForbidden otherwise.
// Without a projectID it returns only tasks of projects whose team the user belongs to.
func (s *taskService) ListTasks(ctx context.Context, projectID, userID string) ([]models.Task, error) {
	if projectID != "" {
		if _, err := s.authorizeUserForProject(ctx, projectID, userID, models.RoleViewer); err != nil {
			return nil, err
		}
		return s.taskRepo.List(ctx, projectID)
	}

	if userID == "" {
		return nil, common.ErrForbidden
	}
	if s.projectRepo == nil || s.teamRepo == nil {
		return nil, common.ErrServiceUnavailable
	}

	teams, err := s.teamRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list teams: %w", err)
	}

	tasks := []models.Task{}
	for i := range teams {
		if !auth.TeamAllowed(ctx, teams[i].ID) {
			continue
		}
		membership, mErr := s.teamRepo.GetMembership(ctx, teams[i].ID, userID)
		if mErr != nil {
			if errors.Is(mErr, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, fmt.Errorf("failed to load team membership: %w", mErr)
		}
		if !auth.HasRolePermission(membership.Role, models.RoleViewer) {
			continue
		}
		projects, pErr := s.projectRepo.List(ctx, teams[i].ID)
		if pErr != nil {
			return nil, fmt.Errorf("failed to list projects: %w", pErr)
		}
		for j := range projects {
			projectTasks, tErr := s.taskRepo.List(ctx, projects[j].ID)
			if tErr != nil {
				return nil, fmt.Errorf("failed to list tasks: %w", tErr)
			}
			tasks = append(tasks, projectTasks...)
		}
	}

	sort.SliceStable(tasks, func(a, b int) bool { return tasks[a].CreatedAt.After(tasks[b].CreatedAt) })
	return tasks, nil
}

// ListSubtasks lists subtasks of a parent task. Authorization is resolved through the parent task's project.
func (s *taskService) ListSubtasks(ctx context.Context, parentTaskID, userID string) ([]models.Task, error) {
	parent, err := s.authorizeUserForTask(ctx, parentTaskID, userID, models.RoleViewer)
	if err != nil {
		return nil, err
	}

	children, err := s.taskRepo.ListByParentID(ctx, parent.ID)
	if err != nil {
		return nil, err
	}

	// Never return children that live in another project than the authorized parent.
	visible := make([]models.Task, 0, len(children))
	for i := range children {
		if children[i].ProjectID == parent.ProjectID {
			visible = append(visible, children[i])
		}
	}
	return visible, nil
}

// CreateTask validates input via models.Validate, authorizes RoleMember on the project's team, creates a new
// task in DB, enqueues to Asynq queue if queued, and broadcasts status via WebSocket.
// Authorization happens before any write, enqueue or broadcast.
func (s *taskService) CreateTask(ctx context.Context, req *dto.CreateTaskRequest, userID string) (*models.Task, error) {
	if userID == "" {
		return nil, common.ErrForbidden
	}
	if req == nil {
		return nil, errors.New("request cannot be nil")
	}

	if req.Priority == 0 {
		req.Priority = int(models.PriorityNormal)
	}

	if err := models.Validate.Struct(req); err != nil {
		return nil, fmt.Errorf("invalid create task request: %w", err)
	}

	if _, err := s.authorizeUserForProject(ctx, req.ProjectID, userID, models.RoleMember); err != nil {
		return nil, err
	}

	if req.ParentTaskID != nil && *req.ParentTaskID != "" {
		parent, err := s.taskRepo.GetByID(ctx, *req.ParentTaskID)
		if err != nil || parent.ProjectID != req.ProjectID {
			return nil, common.NewAppError(400, "parent task does not belong to the project", nil)
		}
	}
	if req.IntegrationID != nil && *req.IntegrationID != "" {
		if s.integRepo == nil {
			return nil, common.ErrServiceUnavailable
		}
		integ, err := s.integRepo.GetByID(ctx, *req.IntegrationID)
		if err != nil || integ.ProjectID != req.ProjectID {
			return nil, common.NewAppError(400, "integration does not belong to the project", nil)
		}
	}

	taskID := common.NewID()
	sessionID := fmt.Sprintf("ses_atm_%s", taskID)

	task := &models.Task{
		ID:            taskID,
		ProjectID:     req.ProjectID,
		IntegrationID: req.IntegrationID,
		ParentTaskID:  req.ParentTaskID,
		Title:        req.Title,
		Prompt:       req.Prompt,
		AssigneeID:   req.AssigneeID,
		AssigneeType: req.AssigneeType,
		Priority:     models.TaskPriority(req.Priority),
		Model:        req.Model,
		Skills:       models.StringArray(req.Skills),
		MCPServers:   models.StringArray(req.MCPServers),
		SessionID:    &sessionID,
		Status:       models.TaskStatusQueued,
	}

	if err := models.Validate.Struct(task); err != nil {
		return nil, fmt.Errorf("invalid task entity: %w", err)
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
func (s *taskService) CancelTask(ctx context.Context, taskID, userID string) error {
	if userID == "" {
		return common.ErrForbidden
	}

	task, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleMember)
	if err != nil {
		return err
	}

	// 1. Coordinated lifecycle boundary under completionMu:
	// Signal in-flight execution context, update status in DB, and guarantee that
	// ExecuteTask registration/completion cannot race with cancellation persistence.
	s.completionMu.Lock()
	if cancel, ok := s.activeCancels.LoadAndDelete(taskID); ok && cancel != nil {
		cancel()
	}
	err = s.UpdateStatus(ctx, taskID, models.TaskStatusCancelled)
	s.completionMu.Unlock()
	if err != nil {
		return err
	}

	// 2. Interrupt the OpenCode session tree in the task's own sandbox (best effort, outside completionMu),
	// so a cancelled task stops spending model budget instead of only being forgotten.
	if s.sessionClients != nil && task.SessionID != nil && *task.SessionID != "" && !s.simulated {
		if oc, cerr := s.sessionClients(ctx, task.ProjectID); cerr == nil {
			intCtx, intCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if intErr := oc.InterruptSessionTree(intCtx, *task.SessionID); intErr != nil {
				log.Printf("warning: error interrupting OpenCode session tree for task %s: %v", taskID, intErr)
			}
			intCancel()
		}
	}

	// 3. Cancel Asynq queue job if queued (outside completionMu)
	if s.queueClient != nil && task.AsynqTaskID != nil && *task.AsynqTaskID != "" {
		tier := queue.ResolveQueueTier(task.Priority)
		_ = s.queueClient.CancelTask(ctx, tier, *task.AsynqTaskID)
	}

	return nil
}

// interruptSessionTreeDisabledDraft is the original CancelTask step 2, intentionally unreferenced.
func (s *taskService) interruptSessionTreeDisabledDraft(taskID string, task *models.Task) {
	// 2. Interrupt OpenCode session tree outside completionMu without holding mutex during HTTP calls
	if s.openCodeClient != nil && task.SessionID != nil && *task.SessionID != "" {
		interruptCtx, intCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if intErr := s.openCodeClient.InterruptSessionTree(interruptCtx, *task.SessionID); intErr != nil {
			log.Printf("warning: error interrupting OpenCode session tree for task %s: %v", taskID, intErr)
		}
		intCancel()
	}
}

// ExecuteTask orchestrates the task execution lifecycle: status transition, attempt tracking, runner execution, and status completion.
func (s *taskService) ExecuteTask(ctx context.Context, taskID string, taskRunner TaskRunner) error {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to find task %q: %w", taskID, err)
	}

	// Coordinated start check & execution context registration under completionMu:
	// Prevents:
	// - Overwriting an earlier canceled row with in_progress
	// - Cancellation arriving just before execution registration with no context signal
	s.completionMu.Lock()
	currentTask, getErr := s.taskRepo.GetByID(ctx, task.ID)
	if (getErr == nil && currentTask.Status == models.TaskStatusCancelled) || task.Status == models.TaskStatusCancelled {
		s.completionMu.Unlock()
		return nil
	}

	runCtx, runCancel := context.WithCancel(ctx)
	s.activeCancels.Store(task.ID, runCancel)

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
		s.activeCancels.Delete(task.ID)
		runCancel()
		s.completionMu.Unlock()
		return fmt.Errorf("failed to mark task as in_progress: %w", err)
	}
	s.completionMu.Unlock()

	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(task.ID, models.TaskStatusInProgress)
	}

	// Count existing attempts for this task
	count, err := s.attemptRepo.CountByTaskID(ctx, task.ID)
	if err != nil {
		s.activeCancels.Delete(task.ID)
		runCancel()
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
		s.activeCancels.Delete(task.ID)
		runCancel()
		return fmt.Errorf("failed to create task attempt: %w", err)
	}

	defer func() {
		runCancel()
	}()

	var runErr error
	if isNilTaskRunner(taskRunner) {
		// Fail closed: a missing runner must never record a succeeded attempt.
		runErr = runner.ErrRuntimeExecutionUnavailable
	} else {
		runErr = taskRunner.RunTask(runCtx, task, attempt)
	}

	finishedAt := time.Now().UTC()
	attempt.FinishedAt = &finishedAt

	// Completion and state transition are protected under completionMu to guarantee that
	// a concurrent cancellation cannot be overwritten by completion logic.
	s.completionMu.Lock()
	defer s.completionMu.Unlock()
	s.activeCancels.Delete(task.ID)

	reloadedTask, reloadErr := s.taskRepo.GetByID(context.Background(), task.ID)
	// Fail closed on reload error: if we cannot reload the task status, treat as cancelled to avoid resurrecting
	isCancelled := reloadErr != nil ||
		reloadedTask.Status == models.TaskStatusCancelled ||
		errors.Is(runErr, context.Canceled) ||
		(runCtx.Err() != nil && ctx.Err() == nil) // runCtx cancelled but parent ctx is alive

	if isCancelled {
		attempt.Status = models.AttemptCancelled
		failReason := "task execution cancelled"
		attempt.FailureReason = &failReason
		_ = s.attemptRepo.Update(context.Background(), attempt)
		_ = s.UpdateStatus(context.Background(), task.ID, models.TaskStatusCancelled)
		return nil
	}

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

// isNilTaskRunner reports whether r is nil, including a typed nil pointer wrapped in the interface.
func isNilTaskRunner(r TaskRunner) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	}
	return false
}

// CreatePullRequestForTask pushes the task's branch and opens its pull request through the project container.
// Requires RoleMember; an empty userID is forbidden. Only a finished task has a branch to publish. The simulated
// runtime never pushes.
func (s *taskService) CreatePullRequestForTask(ctx context.Context, taskID, userID string) (*models.Task, error) {
	task, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleMember)
	if err != nil {
		return nil, err
	}
	if s.simulated || s.publishPR == nil {
		return nil, runner.ErrRuntimeExecutionUnavailable
	}
	if task.Status != models.TaskStatusDone && task.Status != models.TaskStatusReview {
		return nil, errors.New("only a finished task can have a pull request")
	}

	prURL, err := s.publishPR(ctx, task, func(line string) { s.broadcastTaskLog(ctx, task.ID, line) })
	if err != nil {
		return nil, err
	}
	task.PRURL = &prURL
	task.PullRequestURL = &prURL
	return task, nil
}

// createPullRequestForTaskDisabledDraft is the original implementation, intentionally unreferenced.
func (s *taskService) createPullRequestForTaskDisabledDraft(ctx context.Context, taskID string) (*models.Task, error) {
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
	if task.IntegrationID != nil && *task.IntegrationID != "" {
		for i := range integrationsList {
			if integrationsList[i].ID == *task.IntegrationID && integrationsList[i].Enabled {
				ghInteg = &integrationsList[i]
				break
			}
		}
	}
	// Fallback to match by worktree git remote
	if ghInteg == nil && task.WorktreePath != nil && *task.WorktreePath != "" {
		if out, err := exec.CommandContext(ctx, "git", "-C", *task.WorktreePath, "remote", "get-url", "origin").Output(); err == nil {
			remoteStr := strings.TrimSpace(string(out))
			for i := range integrationsList {
				if integrationsList[i].Enabled && strings.Contains(remoteStr, integrationsList[i].ExternalID) {
					ghInteg = &integrationsList[i]
					break
				}
			}
		}
	}
	if ghInteg == nil {
		for i := range integrationsList {
			if integrationsList[i].Provider == models.IntegrationGitHub && integrationsList[i].Enabled {
				ghInteg = &integrationsList[i]
				break
			}
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

// authorizeUserForProject confirms userID has at least requiredRole on the team owning projectID.
// It fails closed: empty userID, unknown project, a team outside ctx's PAT team scope and
// non-membership all yield ErrForbidden (no project existence oracle), and missing repository
// wiring yields ErrServiceUnavailable.
func (s *taskService) authorizeUserForProject(ctx context.Context, projectID, userID string, requiredRole models.TeamRole) (*models.Project, error) {
	if userID == "" || projectID == "" {
		return nil, common.ErrForbidden
	}
	if s.projectRepo == nil || s.teamRepo == nil {
		return nil, common.ErrServiceUnavailable
	}

	project, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, common.ErrNotFound) {
			return nil, common.ErrForbidden
		}
		return nil, fmt.Errorf("failed to load project: %w", err)
	}
	if !auth.TeamAllowed(ctx, project.TeamID) {
		return nil, common.ErrForbidden
	}

	membership, err := s.teamRepo.GetMembership(ctx, project.TeamID, userID)
	if err != nil {
		return nil, common.ErrForbidden
	}
	if !auth.HasRolePermission(membership.Role, requiredRole) {
		return nil, common.ErrForbidden
	}

	return project, nil
}

// authorizeUserForTask verifies that task exists, resolves its project and team, and confirms the
// team is within ctx's PAT team scope and the requesting user has at least requiredRole on it.
// Missing repository wiring yields ErrServiceUnavailable before the task is read (fail closed).
func (s *taskService) authorizeUserForTask(ctx context.Context, taskID, userID string, requiredRole models.TeamRole) (*models.Task, error) {
	if taskID == "" {
		return nil, errors.New("task ID is required")
	}
	if userID == "" {
		return nil, common.ErrForbidden
	}
	if s.projectRepo == nil || s.teamRepo == nil {
		return nil, common.ErrServiceUnavailable
	}

	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}

	project, err := s.projectRepo.GetByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to load project for task: %w", err)
	}
	if !auth.TeamAllowed(ctx, project.TeamID) {
		return nil, common.ErrForbidden
	}

	membership, err := s.teamRepo.GetMembership(ctx, project.TeamID, userID)
	if err != nil {
		return nil, common.ErrForbidden
	}

	if !auth.HasRolePermission(membership.Role, requiredRole) {
		return nil, common.ErrForbidden
	}

	return task, nil
}

// GetTaskSession retrieves metadata and message history for an ongoing or completed task session.
// AC-SEC-02: Requires RoleViewer or higher.
//
// Disabled pending Phase 1B/Gates 0C/0D/0G: after the read-only authorization check, it refuses
// before any OpenCode call.
func (s *taskService) GetTaskSession(ctx context.Context, taskID, userID string) (*dto.TaskSessionResponseDTO, error) {
	if _, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleViewer); err != nil {
		return nil, err
	}

	if s.simulated {
		return &dto.TaskSessionResponseDTO{TaskID: taskID, Simulated: true, Messages: []dto.OpenCodeMessageDTO{}}, nil
	}

	oc, sessionID, task, err := s.projectSession(ctx, taskID, userID, models.RoleViewer)
	if err != nil {
		return nil, err
	}
	sessDTO, err := oc.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	messages, err := oc.GetSessionMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	// Subagent sessions are best effort: the transcript is still useful without them.
	var children []dto.SessionChildDTO
	if kids, kerr := oc.ListChildSessions(ctx, sessionID); kerr == nil {
		for _, k := range kids {
			children = append(children, dto.SessionChildDTO{ID: k.ID, Title: k.Title, Status: k.Status, Cost: k.Cost, UpdatedAt: k.UpdatedAt})
		}
	}
	return &dto.TaskSessionResponseDTO{
		Children:  children,
		SessionID: sessionID,
		TaskID:    task.ID,
		Status:    sessDTO.Status,
		Model:     sessDTO.Model,
		Cost:      sessDTO.Cost,
		Tokens:    sessDTO.Tokens,
		Title:     sessDTO.Title,
		Directory: sessDTO.Directory,
		CreatedAt: sessDTO.CreatedAt,
		UpdatedAt: sessDTO.UpdatedAt,
		Messages:  messages,
	}, nil
}

// projectSession authorizes the caller for the task and returns the OpenCode client of the task's project
// sandbox plus the task's session ID. It is the only way the session tunnel reaches OpenCode, so every call
// is authorized per task and goes to that project's own container.
func (s *taskService) projectSession(ctx context.Context, taskID, userID string, role models.TeamRole) (runner.OpenCodeClient, string, *models.Task, error) {
	task, err := s.authorizeUserForTask(ctx, taskID, userID, role)
	if err != nil {
		return nil, "", nil, err
	}
	if s.sessionClients == nil {
		return nil, "", nil, runner.ErrRuntimeExecutionUnavailable
	}
	if task.SessionID == nil || *task.SessionID == "" {
		return nil, "", nil, errors.New("task has no associated session")
	}
	oc, err := s.sessionClients(ctx, task.ProjectID)
	if err != nil {
		return nil, "", nil, err
	}
	return oc, *task.SessionID, task, nil
}

// InterruptTaskSession stops the task's running OpenCode turn (the session and its subagents) without
// cancelling the task. Requires RoleMember or higher.
func (s *taskService) InterruptTaskSession(ctx context.Context, taskID, userID string) error {
	if s.simulated {
		if _, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleMember); err != nil {
			return err
		}
		return nil
	}
	oc, sessionID, _, err := s.projectSession(ctx, taskID, userID, models.RoleMember)
	if err != nil {
		return err
	}
	return oc.InterruptSessionTree(ctx, sessionID)
}

// getTaskSessionDisabledDraft is the original implementation, intentionally unreferenced.
func (s *taskService) getTaskSessionDisabledDraft(ctx context.Context, taskID, userID string) (*dto.TaskSessionResponseDTO, error) {
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
		Status:    sessDTO.Status,
		Model:     sessDTO.Model,
		Cost:      sessDTO.Cost,
		Tokens:    sessDTO.Tokens,
		Title:     sessDTO.Title,
		Directory: sessDTO.Directory,
		CreatedAt: sessDTO.CreatedAt,
		UpdatedAt: sessDTO.UpdatedAt,
		Messages:  messages,
	}

	return res, nil
}

// broadcastTaskLog broadcasts a log chunk to subscribers and saves it to task_logs repository if available.
func (s *taskService) broadcastTaskLog(ctx context.Context, taskID, chunk string) {
	if s.hub != nil {
		_ = s.hub.BroadcastTaskLog(taskID, chunk)
	}
	if s.logRepo != nil {
		_ = s.logRepo.Append(ctx, &models.TaskLog{
			TaskID:    taskID,
			LogChunk:  chunk,
			Timestamp: time.Now().UTC(),
		})
	}
}

// PromptTaskSession sends a targeted prompt into the task's existing OpenCode session.
// AC-SEC-02: Requires RoleMember or higher.
//
// The prompt goes to the task's own project sandbox. It only delivers the prompt: it does not change the
// task status or start a completion watcher.
func (s *taskService) PromptTaskSession(ctx context.Context, taskID, userID string, req *dto.TaskSessionPromptRequest) error {
	if req == nil || strings.TrimSpace(req.Prompt) == "" {
		return errors.New("prompt cannot be empty")
	}

	oc, sessionID, task, err := s.projectSession(ctx, taskID, userID, models.RoleMember)
	if err != nil {
		return err
	}
	if task.Status == models.TaskStatusCancelled {
		return &common.AppError{Code: 409, Message: "task is cancelled"}
	}

	// A steer takes the task's single active-work slot, so it cannot overlap a run or another steer, and
	// CancelTask cancels it like a run.
	var steerCtx context.Context
	var steerCancel context.CancelFunc
	if s.steerFinalizer != nil {
		steerCtx, steerCancel = context.WithTimeout(context.Background(), steerTimeout)
		if _, busy := s.activeCancels.LoadOrStore(task.ID, context.CancelFunc(steerCancel)); busy {
			steerCancel()
			return &common.AppError{Code: 409, Message: "the task is already running a turn"}
		}
	}
	release := func() {
		if steerCancel != nil {
			steerCancel()
			s.activeCancels.Delete(task.ID)
		}
	}

	var before *time.Time
	if sess, err := oc.GetSession(ctx, sessionID); err == nil {
		before = sess.UpdatedAt
	}
	if err := oc.PromptSession(ctx, sessionID, req.Prompt); err != nil {
		release()
		return err
	}
	s.broadcastTaskLog(ctx, task.ID, fmt.Sprintf("\r\n\x1b[36m➜ [User Steer Prompt]\x1b[0m %s\r\n", req.Prompt))
	if s.steerFinalizer == nil {
		return nil
	}

	_ = s.UpdateStatus(ctx, task.ID, models.TaskStatusInProgress)
	go func() {
		defer release()
		logf := func(chunk string) { s.broadcastTaskLog(context.Background(), task.ID, chunk) }
		err := s.steerFinalizer(steerCtx, task, before, logf)
		// A cancelled task stays cancelled; any other ending returns the task to review so the user can
		// inspect the result or steer again.
		if cur, getErr := s.taskRepo.GetByID(context.Background(), task.ID); getErr == nil && cur.Status == models.TaskStatusCancelled {
			return
		}
		if err != nil {
			logf(fmt.Sprintf("\r\n\x1b[31m[Agent Control Plane] Steering turn did not complete: %v\x1b[0m\r\n", err))
		} else {
			logf("\x1b[32m✔ Steering turn finished. Ready for review.\x1b[0m\r\n")
		}
		_ = s.UpdateStatus(context.Background(), task.ID, models.TaskStatusReview)
	}()
	return nil
}

// promptTaskSessionDisabledDraft is the original implementation, intentionally unreferenced.
func (s *taskService) promptTaskSessionDisabledDraft(ctx context.Context, taskID, userID string, req *dto.TaskSessionPromptRequest) error {
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

	promptTime := time.Now()
	if err := s.openCodeClient.PromptSession(ctx, *task.SessionID, req.Prompt); err != nil {
		return err
	}

	// Move task back to working (in_progress) when steered
	if task.Status != models.TaskStatusInProgress {
		_ = s.UpdateStatus(ctx, task.ID, models.TaskStatusInProgress)
	}

	// Announce prompt and engagement in terminal log stream
	s.broadcastTaskLog(ctx, task.ID, fmt.Sprintf("\r\n\x1b[36m➜ [User Steer Prompt]\x1b[0m %s\r\n\x1b[90mEngaging OpenCode agent session...\x1b[0m\r\n", req.Prompt))

	// Start background watcher to stream tools and commits when turn finishes
	if task.WorktreePath != nil && *task.WorktreePath != "" {
		go s.watchSteeringTurn(task.ID, *task.SessionID, *task.WorktreePath, promptTime)
	}

	return nil
}

// watchSteeringTurn monitors an OpenCode session after steering, streaming execution progress and capturing commits when the turn completes.
func (s *taskService) watchSteeringTurn(taskID, sessionID, worktreePath string, promptTime time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	seenParts := make(map[string]bool)
	minTurnDuration := 3 * time.Second
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Printf("warning: steering completion watcher timed out for task %s", taskID)
			s.broadcastTaskLog(context.Background(), taskID, "\r\n\x1b[31m✖ Steering turn timed out.\x1b[0m\r\n")
			return
		case <-ticker.C:
			// Fetch current messages to stream progress
			msgs, msgErr := s.openCodeClient.GetSessionMessages(ctx, sessionID)
			hasActiveOrRecentTurn := false
			latestTurnCompleted := false

			if msgErr == nil && len(msgs) > 0 {
				for i := 0; i < len(msgs); i++ {
					msg := msgs[i]
					if msg.CreatedAt == nil || msg.CreatedAt.Before(promptTime.Add(-2*time.Second)) {
						continue
					}

					if msg.Role == "assistant" || msg.Type == "assistant" {
						hasActiveOrRecentTurn = true
						if msg.CompletedAt != nil {
							latestTurnCompleted = true
						} else {
							latestTurnCompleted = false
						}

						if contentList, ok := msg.Content.([]any); ok {
							for partIdx, item := range contentList {
								itemMap, ok := item.(map[string]any)
								if !ok {
									continue
								}
								partType, _ := itemMap["type"].(string)
								partID := fmt.Sprintf("%s_%d", msg.ID, partIdx)

								if partType == "tool" {
									toolName, _ := itemMap["name"].(string)
									toolCallID, _ := itemMap["id"].(string)
									if toolCallID != "" {
										partID = toolCallID
									}

									state, _ := itemMap["state"].(map[string]any)
									status := ""
									if state != nil {
										status, _ = state["status"].(string)
									}

									startKey := partID + "_start"
									if !seenParts[startKey] {
										seenParts[startKey] = true
										cmdSummary := ""
										if state != nil {
											if input, ok := state["input"].(map[string]any); ok {
												if cmd, ok := input["command"].(string); ok {
													cmdSummary = cmd
												} else if p, ok := input["path"].(string); ok {
													cmdSummary = p
												}
											}
										}
										if s.hub != nil {
											var toolInput any
											if state != nil {
												toolInput = state["input"]
											}
											_ = s.hub.BroadcastTaskEvent(taskID, map[string]any{
												"eventType":  "tool_use",
												"sessionID":  sessionID,
												"toolName":   toolName,
												"toolStatus": "running",
												"toolInput":  toolInput,
											})
										}
										if cmdSummary != "" {
											if len(cmdSummary) > 80 {
												cmdSummary = cmdSummary[:77] + "..."
											}
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\x1b[33m→ [%s]\x1b[0m \x1b[90m%s\x1b[0m\r\n", toolName, cmdSummary))
										} else {
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\x1b[33m→ [%s]\x1b[0m\r\n", toolName))
										}
									}

									doneKey := partID + "_done"
									if (status == "completed" || status == "failed") && !seenParts[doneKey] {
										seenParts[doneKey] = true
										if s.hub != nil {
											var toolOutput any
											if state != nil {
												toolOutput = state["output"]
											}
											_ = s.hub.BroadcastTaskEvent(taskID, map[string]any{
												"eventType":  "tool_use",
												"sessionID":  sessionID,
												"toolName":   toolName,
												"toolStatus": status,
												"toolOutput": toolOutput,
											})
										}
										if state != nil {
											if contents, ok := state["content"].([]any); ok {
												for _, c := range contents {
													if cMap, ok := c.(map[string]any); ok {
														if txt, ok := cMap["text"].(string); ok && txt != "" {
															lines := strings.Split(strings.TrimSpace(txt), "\n")
															maxLines := 4
															if len(lines) < maxLines {
																maxLines = len(lines)
															}
															for lIdx := 0; lIdx < maxLines; lIdx++ {
																s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("  \x1b[90m%s\x1b[0m\r\n", lines[lIdx]))
															}
															if len(lines) > 4 {
																s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("  \x1b[90m... (%d lines)\x1b[0m\r\n", len(lines)))
															}
														}
													}
												}
											}
										}
										if status == "completed" {
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\x1b[32m✔ Completed tool: %s\x1b[0m\r\n", toolName))
										} else {
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\x1b[31m✖ Tool failed: %s\x1b[0m\r\n", toolName))
										}
									}
								} else if partType == "text" {
									textKey := partID + "_text"
									if !seenParts[textKey] {
										if textVal, ok := itemMap["text"].(string); ok && textVal != "" {
											seenParts[textKey] = true
											agentName := msg.Agent
											if agentName == "" {
												agentName = "agent"
											}
											if s.hub != nil {
												_ = s.hub.BroadcastTaskEvent(taskID, map[string]any{
													"eventType": "text",
													"sessionID": sessionID,
													"text":      textVal,
													"agent":     agentName,
												})
											}
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\r\n\x1b[36m> %s\x1b[0m\r\n%s\r\n", agentName, textVal))
										}
									}
								}
							}
						}
					}
				}
			}

			// Also inspect child subagent sessions to stream their tool calls and output
			if childSessions, err := s.openCodeClient.ListChildSessions(ctx, sessionID); err == nil && len(childSessions) > 0 {
				for _, child := range childSessions {
					cMsgs, cErr := s.openCodeClient.GetSessionMessages(ctx, child.ID)
					if cErr != nil || len(cMsgs) == 0 {
						continue
					}
					agentLabel := child.Model
					if agentLabel == "" {
						agentLabel = "subagent"
					}

					for _, cMsg := range cMsgs {
						if cContentList, ok := cMsg.Content.([]any); ok {
							for cPartIdx, cItem := range cContentList {
								cItemMap, ok := cItem.(map[string]any)
								if !ok {
									continue
								}
								cPartType, _ := cItemMap["type"].(string)
								cPartID := fmt.Sprintf("%s_%d", cMsg.ID, cPartIdx)

								if cPartType == "tool" {
									cToolName, _ := cItemMap["name"].(string)
									if cid, ok := cItemMap["id"].(string); ok && cid != "" {
										cPartID = cid
									}
									cState, _ := cItemMap["state"].(map[string]any)
									cStatus := ""
									if cState != nil {
										cStatus, _ = cState["status"].(string)
									}

									cStartKey := cPartID + "_start"
									if !seenParts[cStartKey] {
										seenParts[cStartKey] = true
										cCmd := ""
										if cState != nil {
											if input, ok := cState["input"].(map[string]any); ok {
												if cmd, ok := input["command"].(string); ok {
													cCmd = cmd
												} else if p, ok := input["path"].(string); ok {
													cCmd = p
												}
											}
										}
										if s.hub != nil {
											var cInput any
											if cState != nil {
												cInput = cState["input"]
											}
											_ = s.hub.BroadcastTaskEvent(taskID, map[string]any{
												"eventType":       "tool_use",
												"sessionID":       child.ID,
												"parentSessionID": sessionID,
												"subagent":        agentLabel,
												"toolName":        cToolName,
												"toolStatus":      "running",
												"toolInput":       cInput,
											})
										}
										if cCmd != "" {
											if len(cCmd) > 70 {
												cCmd = cCmd[:67] + "..."
											}
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m \x1b[33m→ [%s]\x1b[0m \x1b[90m%s\x1b[0m\r\n", agentLabel, cToolName, cCmd))
										} else {
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m \x1b[33m→ [%s]\x1b[0m\r\n", agentLabel, cToolName))
										}
									}

									cDoneKey := cPartID + "_done"
									if (cStatus == "completed" || cStatus == "failed") && !seenParts[cDoneKey] {
										seenParts[cDoneKey] = true
										if s.hub != nil {
											var cOutput any
											if cState != nil {
												cOutput = cState["output"]
											}
											_ = s.hub.BroadcastTaskEvent(taskID, map[string]any{
												"eventType":       "tool_use",
												"sessionID":       child.ID,
												"parentSessionID": sessionID,
												"subagent":        agentLabel,
												"toolName":        cToolName,
												"toolStatus":      cStatus,
												"toolOutput":      cOutput,
											})
										}
										if cStatus == "completed" {
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m \x1b[32m✔ Completed tool: %s\x1b[0m\r\n", agentLabel, cToolName))
										} else {
											s.broadcastTaskLog(context.Background(), taskID, fmt.Sprintf("\x1b[35m[subagent: %s]\x1b[0m \x1b[31m✖ Tool failed: %s\x1b[0m\r\n", agentLabel, cToolName))
										}
									}
								}
							}
						}
					}
				}
			}

			// Also check session status directly in case tool completed without message completed timestamp
			sessDTO, err := s.openCodeClient.GetSession(ctx, sessionID)
			sessIdle := err == nil && (sessDTO.Status == "succeeded" || sessDTO.Status == "idle")

			if time.Since(startTime) > minTurnDuration && hasActiveOrRecentTurn && (latestTurnCompleted || sessIdle) {
				if s.hub != nil {
					_ = s.hub.BroadcastTaskEvent(taskID, map[string]any{
						"eventType": "step_finish",
						"sessionID": sessionID,
						"reason":    "stop",
					})
				}
				s.broadcastTaskLog(context.Background(), taskID, "\r\n\x1b[32m✔ Agent response completed.\x1b[0m\r\n")
				s.finalizeSteeringTurn(ctx, taskID, worktreePath)
				return
			}
		}
	}
}

// finalizeSteeringTurn creates a local commit for uncommitted changes, broadcasts diff, pushes branch, and moves task back to review.
func (s *taskService) finalizeSteeringTurn(ctx context.Context, taskID, worktreePath string) {
	// 1. Capture snapshot (stages changes, creates commit, computes diff)
	diffRes, diffErr := git.CaptureSnapshot(ctx, worktreePath, "main")
	if diffErr == nil && diffRes != nil {
		if s.hub != nil {
			_ = s.hub.BroadcastTaskDiff(taskID, diffRes.RawDiff)
		}
		if len(diffRes.Files) > 0 {
			s.broadcastTaskLog(ctx, taskID, fmt.Sprintf("\r\n\x1b[32m✔ Steered changes committed: %d files changed (+%d/-%d)\x1b[0m\r\n",
				len(diffRes.Files), diffRes.TotalAdditions, diffRes.TotalDeletions))
		}
	} else if diffErr != nil {
		log.Printf("warning: failed to capture git snapshot for steered task %s: %v", taskID, diffErr)
	}

	// 2. Push branch if PR or remote integration exists
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err == nil && task != nil {
		s.maybePushBranchForSteering(ctx, task, worktreePath)
	}

	// 3. Move task back to review
	_ = s.UpdateStatus(ctx, taskID, models.TaskStatusReview)
	s.broadcastTaskLog(ctx, taskID, "\x1b[32m✔ Steering turn finished. Ready for review.\x1b[0m\r\n")
}

// maybePushBranchForSteering pushes the updated branch to remote git repository if GitHub integration is enabled.
func (s *taskService) maybePushBranchForSteering(ctx context.Context, task *models.Task, worktreePath string) {
	if s.integRepo == nil || s.credRepo == nil || s.cipher == nil {
		return
	}

	integrationsList, err := s.integRepo.ListByProjectID(ctx, task.ProjectID)
	if err != nil || len(integrationsList) == 0 {
		return
	}

	var ghInteg *models.ProjectIntegration
	if task.IntegrationID != nil && *task.IntegrationID != "" {
		for i := range integrationsList {
			if integrationsList[i].ID == *task.IntegrationID && integrationsList[i].Enabled {
				ghInteg = &integrationsList[i]
				break
			}
		}
	}
	if ghInteg == nil && worktreePath != "" {
		if out, err := exec.CommandContext(ctx, "git", "-C", worktreePath, "remote", "get-url", "origin").Output(); err == nil {
			remoteStr := strings.TrimSpace(string(out))
			for i := range integrationsList {
				if integrationsList[i].Enabled && strings.Contains(remoteStr, integrationsList[i].ExternalID) {
					ghInteg = &integrationsList[i]
					break
				}
			}
		}
	}
	if ghInteg == nil {
		for i := range integrationsList {
			if integrationsList[i].Provider == models.IntegrationGitHub && integrationsList[i].Enabled {
				ghInteg = &integrationsList[i]
				break
			}
		}
	}
	if ghInteg == nil {
		return
	}

	cred, err := s.credRepo.GetByID(ctx, ghInteg.CredentialID)
	if err != nil {
		return
	}

	token, err := s.cipher.Decrypt(cred.EncryptedData)
	if err != nil {
		return
	}

	branchName := fmt.Sprintf("task/%s", task.ID)
	if task.BranchName != nil && *task.BranchName != "" {
		branchName = *task.BranchName
	}
	remoteURL := git.RemoteURL(ghInteg.ExternalID)

	if pushErr := git.PushBranch(ctx, worktreePath, remoteURL, token, branchName); pushErr == nil {
		s.broadcastTaskLog(ctx, task.ID, fmt.Sprintf("\x1b[32m✔ Steered changes pushed to branch %s\x1b[0m\r\n", branchName))
	} else {
		log.Printf("warning: failed to push steered branch for task %s: %v", task.ID, pushErr)
	}
}

// CompactTaskSession triggers context compaction on the task's OpenCode session.
// AC-SEC-02: Requires RoleMember or higher.
func (s *taskService) CompactTaskSession(ctx context.Context, taskID, userID string) error {
	oc, sessionID, _, err := s.projectSession(ctx, taskID, userID, models.RoleMember)
	if err != nil {
		return err
	}
	return oc.CompactSession(ctx, sessionID)
}

// compactTaskSessionDisabledDraft is the original implementation, intentionally unreferenced.
func (s *taskService) compactTaskSessionDisabledDraft(ctx context.Context, taskID, userID string) error {
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

// GetTaskDiff returns the task's unified diff against the integration's base branch, read in the project
// container. AC-SEC-02: Requires RoleViewer or higher; an empty userID is forbidden.
func (s *taskService) GetTaskDiff(ctx context.Context, taskID, userID string) (string, error) {
	task, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleViewer)
	if err != nil {
		return "", err
	}
	if s.readTaskDiff == nil {
		return "", runner.ErrRuntimeExecutionUnavailable
	}
	return s.readTaskDiff(ctx, task)
}

// getTaskDiffDisabledDraft is the original implementation, intentionally unreferenced.
func (s *taskService) getTaskDiffDisabledDraft(ctx context.Context, taskID, userID string) (string, error) {
	var task *models.Task
	var err error
	if userID != "" {
		task, err = s.authorizeUserForTask(ctx, taskID, userID, models.RoleViewer)
		if err != nil {
			return "", err
		}
	} else {
		task, err = s.taskRepo.GetByID(ctx, taskID)
		if err != nil {
			return "", err
		}
	}

	worktreePath := ""
	if task.WorktreePath != nil && *task.WorktreePath != "" {
		worktreePath = *task.WorktreePath
	}

	if worktreePath == "" {
		return "", nil
	}

	res, err := git.CaptureSnapshot(ctx, worktreePath, "main")
	if err == nil && res != nil && res.RawDiff != "" {
		return res.RawDiff, nil
	}

	// Fallback to git diff origin/main...HEAD
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "diff", "origin/main...HEAD")
	if out, dErr := cmd.Output(); dErr == nil && len(out) > 0 {
		return string(out), nil
	}

	// Fallback to git diff HEAD~1...HEAD
	cmdLast := exec.CommandContext(ctx, "git", "-C", worktreePath, "diff", "HEAD~1...HEAD")
	if out, lErr := cmdLast.Output(); lErr == nil && len(out) > 0 {
		return string(out), nil
	}

	return "", nil
}

// SyncTaskPRStatus checks the external PR status on GitHub and marks the task as done if merged.
// AC-SEC-02: Requires RoleMember or higher; an empty userID is forbidden. Without GitHub dependencies it
// refuses with the runtime-unavailable error.
func (s *taskService) SyncTaskPRStatus(ctx context.Context, taskID, userID string) (*models.Task, error) {
	task, err := s.authorizeUserForTask(ctx, taskID, userID, models.RoleMember)
	if err != nil {
		return nil, err
	}
	if !s.hasGitHubSync() {
		return nil, runner.ErrRuntimeExecutionUnavailable
	}
	return s.syncPRStatus(ctx, task)
}

// hasGitHubSync reports whether the GitHub status and issue paths are wired.
func (s *taskService) hasGitHubSync() bool {
	return s.integRepo != nil && s.credRepo != nil && s.cipher != nil && s.ghProvider != nil
}

// syncPRStatus marks a review task done when GitHub reports its pull request merged, then closes the linked
// issue. The caller must have authorized the request; the reconciler batches its own reads instead.
func (s *taskService) syncPRStatus(ctx context.Context, task *models.Task) (*models.Task, error) {
	if task.PRURL == nil || *task.PRURL == "" || task.Status == models.TaskStatusDone {
		return task, nil
	}

	integrationsList, err := s.integRepo.ListByProjectID(ctx, task.ProjectID)
	if err != nil {
		return task, fmt.Errorf("failed to list project integrations: %w", err)
	}
	ghInteg := selectGitHubIntegration(task, integrationsList)
	if ghInteg == nil {
		return task, errors.New("no enabled GitHub integration for project")
	}

	token, err := s.gitHubToken(ctx, ghInteg)
	if err != nil {
		return task, err
	}
	prStatus, err := s.ghProvider.GetPullRequestStatus(ctx, ghInteg, token, *task.PRURL)
	if err != nil {
		return task, fmt.Errorf("failed to check PR status: %w", err)
	}
	if prStatus == nil || !prStatus.Merged {
		return task, nil
	}
	if err := s.completeMergedTask(ctx, task, ghInteg, token); err != nil {
		return nil, err
	}
	return task, nil
}

// gitHubToken decrypts the credential of a GitHub integration.
func (s *taskService) gitHubToken(ctx context.Context, ghInteg *models.ProjectIntegration) (string, error) {
	cred, err := s.credRepo.GetByID(ctx, ghInteg.CredentialID)
	if err != nil {
		return "", fmt.Errorf("failed to load GitHub credential: %w", err)
	}
	token, err := s.cipher.Decrypt(cred.EncryptedData)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt GitHub credential: %w", err)
	}
	return token, nil
}

// completeMergedTask marks a task done after its pull request merged, and closes its linked issue.
func (s *taskService) completeMergedTask(ctx context.Context, task *models.Task, ghInteg *models.ProjectIntegration, token string) error {
	task.Status = models.TaskStatusDone
	task.UpdatedAt = time.Now().UTC()
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	if s.hub != nil {
		_ = s.hub.BroadcastTaskStatus(task.ID, models.TaskStatusDone)
	}

	if task.ExternalID != nil && *task.ExternalID != "" {
		if syncProv, ok := s.ghProvider.(integrations.SyncProvider); ok {
			_ = syncProv.UpdateIssueStatus(ctx, ghInteg, token, *task.ExternalID, "closed")
			_ = syncProv.AddComment(ctx, ghInteg, token, *task.ExternalID, fmt.Sprintf("Pull Request %s was merged. Task marked as completed.", *task.PRURL))
		}
	}
	return nil
}

// selectGitHubIntegration picks the project's GitHub integration for a task: the one the task is linked to,
// then the enabled one whose external ID appears in the PR URL, then any enabled GitHub integration.
func selectGitHubIntegration(task *models.Task, list []models.ProjectIntegration) *models.ProjectIntegration {
	if task.IntegrationID != nil && *task.IntegrationID != "" {
		for i := range list {
			if list[i].ID == *task.IntegrationID && list[i].Enabled {
				return &list[i]
			}
		}
	}
	if task.PRURL != nil && *task.PRURL != "" {
		for i := range list {
			if list[i].Enabled && strings.Contains(*task.PRURL, list[i].ExternalID) {
				return &list[i]
			}
		}
	}
	for i := range list {
		if list[i].Provider == models.IntegrationGitHub && list[i].Enabled {
			return &list[i]
		}
	}
	return nil
}

// syncTaskPRStatusDisabledDraft is the original implementation, intentionally unreferenced.
func (s *taskService) syncTaskPRStatusDisabledDraft(ctx context.Context, taskID, userID string) (*models.Task, error) {
	var task *models.Task
	var err error
	if userID != "" {
		task, err = s.authorizeUserForTask(ctx, taskID, userID, models.RoleViewer)
		if err != nil {
			return nil, err
		}
	} else {
		task, err = s.taskRepo.GetByID(ctx, taskID)
		if err != nil {
			return nil, err
		}
	}

	if task.PRURL == nil || *task.PRURL == "" {
		return task, nil
	}

	if task.Status == models.TaskStatusDone {
		return task, nil
	}

	if s.integRepo == nil || s.credRepo == nil || s.cipher == nil || s.ghProvider == nil {
		return task, nil
	}

	integrationsList, err := s.integRepo.ListByProjectID(ctx, task.ProjectID)
	if err != nil || len(integrationsList) == 0 {
		return task, nil
	}

	var ghInteg *models.ProjectIntegration
	if task.IntegrationID != nil && *task.IntegrationID != "" {
		for i := range integrationsList {
			if integrationsList[i].ID == *task.IntegrationID && integrationsList[i].Enabled {
				ghInteg = &integrationsList[i]
				break
			}
		}
	}
	// Fallback to match by PR URL domain / slug if present
	if ghInteg == nil && task.PRURL != nil && *task.PRURL != "" {
		for i := range integrationsList {
			if integrationsList[i].Enabled && strings.Contains(*task.PRURL, integrationsList[i].ExternalID) {
				ghInteg = &integrationsList[i]
				break
			}
		}
	}
	if ghInteg == nil {
		for i := range integrationsList {
			if integrationsList[i].Provider == models.IntegrationGitHub && integrationsList[i].Enabled {
				ghInteg = &integrationsList[i]
				break
			}
		}
	}
	if ghInteg == nil {
		return task, nil
	}

	cred, err := s.credRepo.GetByID(ctx, ghInteg.CredentialID)
	if err != nil {
		return task, nil
	}

	token, err := s.cipher.Decrypt(cred.EncryptedData)
	if err != nil {
		return task, nil
	}

	prStatus, err := s.ghProvider.GetPullRequestStatus(ctx, ghInteg, token, *task.PRURL)
	if err != nil {
		return task, fmt.Errorf("failed to check PR status: %w", err)
	}

	if prStatus != nil && prStatus.Merged {
		task.Status = models.TaskStatusDone
		task.UpdatedAt = time.Now().UTC()
		if err := s.taskRepo.Update(ctx, task); err != nil {
			return nil, fmt.Errorf("failed to update task status: %w", err)
		}

		if s.hub != nil {
			_ = s.hub.BroadcastTaskStatus(task.ID, models.TaskStatusDone)
		}

		// Close linked issue if present
		if task.ExternalID != nil && *task.ExternalID != "" {
			if syncProv, ok := s.ghProvider.(integrations.SyncProvider); ok {
				_ = syncProv.UpdateIssueStatus(ctx, ghInteg, token, *task.ExternalID, "closed")
				_ = syncProv.AddComment(ctx, ghInteg, token, *task.ExternalID, fmt.Sprintf("Pull Request %s was merged. Task marked as completed.", *task.PRURL))
			}
		}
	}

	return task, nil
}

// ReconcileActivePRs inspects tasks in review status with a PR URL and marks them done if merged. A provider that
// can batch reads gets one request per integration; any other provider is read one pull request at a time.
// Without GitHub dependencies it does nothing. A task that fails to sync is logged and retried on the next tick.
func (s *taskService) ReconcileActivePRs(ctx context.Context) error {
	if !s.hasGitHubSync() {
		return nil
	}

	tasks, err := s.taskRepo.List(ctx, "")
	if err != nil {
		return err
	}
	var pending []*models.Task
	for i := range tasks {
		if tasks[i].Status == models.TaskStatusReview && tasks[i].PRURL != nil && *tasks[i].PRURL != "" {
			pending = append(pending, &tasks[i])
		}
	}

	if batcher, ok := s.ghProvider.(integrations.PullRequestStatusBatcher); ok {
		s.reconcileBatched(ctx, batcher, pending)
		return nil
	}
	for _, task := range pending {
		if _, err := s.syncPRStatus(ctx, task); err != nil {
			log.Printf("warning: PR status sync failed for task %s: %v", task.ID, err)
		}
	}
	return nil
}

// reconcileBatched reads the pending pull requests with one batched request per GitHub integration, then completes
// the merged ones. Each task is attributed to the integration selected for it, so one token serves its whole batch.
func (s *taskService) reconcileBatched(ctx context.Context, batcher integrations.PullRequestStatusBatcher, pending []*models.Task) {
	type batch struct {
		integ *models.ProjectIntegration
		tasks []*models.Task
	}
	integrationsByProject := make(map[string][]models.ProjectIntegration)
	batches := make(map[string]*batch)
	var order []string
	for _, task := range pending {
		list, cached := integrationsByProject[task.ProjectID]
		if !cached {
			var err error
			list, err = s.integRepo.ListByProjectID(ctx, task.ProjectID)
			if err != nil {
				log.Printf("warning: PR status sync for task %s: failed to list project integrations: %v", task.ID, err)
			}
			integrationsByProject[task.ProjectID] = list
		}
		integ := selectGitHubIntegration(task, list)
		if integ == nil {
			log.Printf("warning: PR status sync for task %s: no enabled GitHub integration for project", task.ID)
			continue
		}
		b, ok := batches[integ.ID]
		if !ok {
			b = &batch{integ: integ}
			batches[integ.ID] = b
			order = append(order, integ.ID)
		}
		b.tasks = append(b.tasks, task)
	}

	for _, id := range order {
		b := batches[id]
		token, err := s.gitHubToken(ctx, b.integ)
		if err != nil {
			log.Printf("warning: PR status sync for integration %s: %v", id, err)
			continue
		}
		urls := make([]string, 0, len(b.tasks))
		for _, task := range b.tasks {
			urls = append(urls, *task.PRURL)
		}
		results := batcher.GetPullRequestStatuses(ctx, token, urls)
		for _, task := range b.tasks {
			res, ok := results[*task.PRURL]
			if !ok {
				res.Err = errors.New("pull request was not read")
			}
			if res.Err != nil {
				log.Printf("warning: PR status sync failed for task %s: %v", task.ID, res.Err)
				continue
			}
			if res.Status != nil && res.Status.Merged {
				if err := s.completeMergedTask(ctx, task, b.integ, token); err != nil {
					log.Printf("warning: PR status sync failed for task %s: %v", task.ID, err)
				}
			}
		}
	}
}

// reconcileActivePRsDisabledDraft is the original implementation, intentionally unreferenced.
func (s *taskService) reconcileActivePRsDisabledDraft(ctx context.Context) error {
	tasks, err := s.taskRepo.List(ctx, "")
	if err != nil {
		return err
	}

	for i := range tasks {
		if tasks[i].Status == models.TaskStatusReview && tasks[i].PRURL != nil && *tasks[i].PRURL != "" {
			_, _ = s.SyncTaskPRStatus(ctx, tasks[i].ID, "")
		}
	}
	return nil
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
