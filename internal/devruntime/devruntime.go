// Package devruntime is a development-only stand-in for the task runner. It performs no git, token,
// filesystem or Docker work: it only writes simulated log lines and reports success, so the task
// lifecycle and UI can be exercised while the real runtime is quarantined (Phase 1A). It is refused
// outside APP_ENV=development and never replaces the quarantine itself.
package devruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agent-task-manager/internal/models"
)

// LogAppender stores task log chunks.
type LogAppender interface {
	Append(ctx context.Context, l *models.TaskLog) error
}

// LogBroadcaster pushes a log chunk to live subscribers.
type LogBroadcaster interface {
	BroadcastTaskLog(taskID, content string) error
}

// Runner simulates a task run. It satisfies queue.TaskRunner.
type Runner struct {
	logs LogAppender
	hub  LogBroadcaster
	step time.Duration
}

// NewRunner returns a Runner that pauses step between simulated log lines. logs and hub may be nil.
func NewRunner(logs LogAppender, hub LogBroadcaster, step time.Duration) *Runner {
	return &Runner{logs: logs, hub: hub, step: step}
}

// RunTask writes a few simulated log lines and returns nil, or ctx's error if cancelled.
func (r *Runner) RunTask(ctx context.Context, task *models.Task, attempt *models.TaskAttempt) error {
	if task == nil {
		return errors.New("task cannot be nil")
	}
	attemptID := ""
	if attempt != nil {
		attemptID = attempt.ID
	}
	lines := []string{
		fmt.Sprintf("[dev runtime] SIMULATED run for task %q. No repository, token, worktree or container is used.\r\n", task.Title),
		"[dev runtime] Planning changes (simulated)...\r\n",
		"[dev runtime] Applying changes (simulated)...\r\n",
		"[dev runtime] Done (simulated). The real runtime stays disabled by the Phase 1A quarantine.\r\n",
	}
	for _, line := range lines {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.step):
		}
		if r.hub != nil {
			_ = r.hub.BroadcastTaskLog(task.ID, line)
		}
		if r.logs != nil {
			_ = r.logs.Append(ctx, &models.TaskLog{TaskID: task.ID, AttemptID: &attemptID, LogChunk: line, Timestamp: time.Now().UTC()})
		}
	}
	return nil
}

// EnabledFromEnv reports whether ATM_DEV_FAKE_RUNTIME asks for the fake runner. It returns an error,
// never true, when the flag is set outside APP_ENV=development or holds anything but "true"/"false".
func EnabledFromEnv(getenv func(string) string) (bool, error) {
	switch getenv("ATM_DEV_FAKE_RUNTIME") {
	case "", "false":
		return false, nil
	case "true":
		if getenv("APP_ENV") != "development" {
			return false, errors.New("devruntime: ATM_DEV_FAKE_RUNTIME is only allowed with APP_ENV=development")
		}
		return true, nil
	default:
		return false, errors.New("devruntime: ATM_DEV_FAKE_RUNTIME must be \"true\" or \"false\"")
	}
}
