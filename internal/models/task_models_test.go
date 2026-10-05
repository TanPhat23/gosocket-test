// internal/models/task_models_test.go
package models_test

import (
	"testing"
	"time"

	"agent-task-manager/internal/models"
)

func TestTaskInitialStateAndValidation(t *testing.T) {
	task := models.Task{
		ID:           "task-1",
		ProjectID:    "proj-1",
		Title:        "Implement feature",
		Prompt:       "Write tests and code",
		Status:       models.TaskStatusQueued,
		Priority:     models.PriorityHigh,
		AssigneeType: models.AssigneeAgent,
		AssigneeID:   "opencode",
	}

	if task.Priority != 75 {
		t.Errorf("expected PriorityHigh to be 75, got %d", task.Priority)
	}
	if task.Status != "queued" {
		t.Errorf("expected status queued, got %s", task.Status)
	}

	// Verify validator accepts valid task
	if err := models.Validate.Struct(task); err != nil {
		t.Fatalf("expected valid task, got error: %v", err)
	}

	// Verify validator catches invalid status
	invalidTask := task
	invalidTask.Status = models.TaskStatus("nonexistent_state")
	if err := models.Validate.Struct(invalidTask); err == nil {
		t.Errorf("expected validation failure on invalid TaskStatus, got nil")
	}
}

func TestTaskMetadataAndRelationships(t *testing.T) {
	now := time.Now()
	heartbeat := now.Add(-5 * time.Minute)
	asynqID := "asynq-task-123"
	integID := "integ-1"
	extID := "GH-101"
	extURL := "https://github.com/org/repo/issues/101"
	prURL := "https://github.com/org/repo/pull/102"
	worktree := "/tmp/worktrees/task-1"
	branch := "feat/task-1"
	model := "anthropic/claude-3-7-sonnet"
	containerID := "docker-cid-999"
	exitCode := 0

	task := models.Task{
		ID:            "task-meta-1",
		ProjectID:     "proj-1",
		Title:         "Refactor queue handler",
		Prompt:        "Fix queue handler logic",
		AssigneeID:    "agent-runner",
		Status:        models.TaskStatusInProgress,
		AssigneeType:  models.AssigneeAgent,
		Priority:      models.PriorityUrgent,
		HeartbeatAt:   &heartbeat,
		IntegrationID: &integID,
		AsynqTaskID:   &asynqID,
		ExternalID:    &extID,
		ExternalURL:   &extURL,
		PRURL:         &prURL,
		WorktreePath:  &worktree,
		BranchName:    &branch,
		Model:         &model,
		ContainerID:   &containerID,
		ExitCode:      &exitCode,
		Skills:        models.StringArray{"test-driven-development"},
		MCPServers:    models.StringArray{"github", "context7"},
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := models.Validate.Struct(task); err != nil {
		t.Fatalf("expected valid task with metadata, got: %v", err)
	}

	// Test TaskAttempt fields and validation
	attemptStarted := now.Add(-2 * time.Minute)
	attemptFinished := now
	attemptFailReason := "process crashed"
	attempt := models.TaskAttempt{
		ID:            "att-1",
		TaskID:        task.ID,
		AttemptNumber: 1,
		Status:        models.AttemptFailed,
		CreatedAt:     now,
		StartedAt:     &attemptStarted,
		FinishedAt:    &attemptFinished,
		ContainerID:   &containerID,
		WorktreePath:  &worktree,
		BranchName:    &branch,
		ExitCode:      &exitCode,
		FailureReason: &attemptFailReason,
		Task:          task,
	}

	if err := models.Validate.Struct(attempt); err != nil {
		t.Fatalf("expected valid attempt, got: %v", err)
	}

	// Verify attempt validation rules (e.g. invalid attempt number <= 0)
	invalidAttempt := attempt
	invalidAttempt.AttemptNumber = 0
	if err := models.Validate.Struct(invalidAttempt); err == nil {
		t.Errorf("expected validation failure for AttemptNumber 0, got nil")
	}

	// Test TaskLog fields and validation with AttemptID pointer
	attemptID := attempt.ID
	log := models.TaskLog{
		ID:        1,
		TaskID:    task.ID,
		AttemptID: &attemptID,
		LogChunk:  "Build started successfully\n",
		Timestamp: now,
		Task:      task,
		Attempt:   &attempt,
	}

	if err := models.Validate.Struct(log); err != nil {
		t.Fatalf("expected valid task log, got: %v", err)
	}

	// Test WebhookEvent with ErrorMessage pointer
	errMsg := "webhook verification failed"
	processedAt := now
	webhook := models.WebhookEvent{
		Provider:        "github",
		ProjectID:       "proj-1",
		ProviderEventID: "evt-github-456",
		Status:          models.WebhookFailed,
		ReceivedAt:      now,
		ProcessedAt:     &processedAt,
		ErrorMessage:    &errMsg,
	}

	if err := models.Validate.Struct(webhook); err != nil {
		t.Fatalf("expected valid webhook event, got: %v", err)
	}
}
