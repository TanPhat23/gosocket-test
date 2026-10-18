package service_test

import (
	"context"
	"errors"
	"testing"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
)

// batchingPRProvider reads pull requests in batches. Merged URLs report merged; batchErr fails every batch.
type batchingPRProvider struct {
	merged      map[string]bool
	batchErr    error
	batchCalls  int
	batchSizes  []int
	singleCalls int
}

func (p *batchingPRProvider) CreatePullRequest(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, branch, title, body string) (string, error) {
	return "", errors.New("not used")
}

func (p *batchingPRProvider) GetPullRequestStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, prNumberOrURL string) (*integrations.PullRequestStatus, error) {
	p.singleCalls++
	return &integrations.PullRequestStatus{State: "open", Merged: p.merged[prNumberOrURL]}, nil
}

func (p *batchingPRProvider) GetPullRequestStatuses(ctx context.Context, plainSecret string, prURLs []string) map[string]integrations.PullRequestStatusResult {
	p.batchCalls++
	p.batchSizes = append(p.batchSizes, len(prURLs))
	out := make(map[string]integrations.PullRequestStatusResult, len(prURLs))
	for _, u := range prURLs {
		if p.batchErr != nil {
			out[u] = integrations.PullRequestStatusResult{Err: p.batchErr}
			continue
		}
		out[u] = integrations.PullRequestStatusResult{
			Status: &integrations.PullRequestStatus{State: "open", Merged: p.merged[u]},
		}
	}
	return out
}

// newReviewTaskWithPRURL adds a review task in the fixture's project, linked to prURL and without an external issue.
func newReviewTaskWithPRURL(t *testing.T, f *quarantineFixture, prURL string) *models.Task {
	t.Helper()
	sessionID := "ses_batch_" + common.NewID()
	task := &models.Task{
		ID: common.NewID(), ProjectID: f.project.ID, Title: "Batch", Prompt: "p",
		PRURL: &prURL, SessionID: &sessionID,
		Status: models.TaskStatusReview, Priority: models.PriorityNormal,
		Skills: models.StringArray{}, MCPServers: models.StringArray{},
	}
	if err := f.db.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

func TestTaskService_ReconcileActivePRs_BatchesPerIntegration(t *testing.T) {
	f := newQuarantineFixture(t)
	open := f.createReviewTaskWithPR(t) // pull/7
	merged := newReviewTaskWithPRURL(t, f, "https://github.com/owner/rq-repo/pull/8")
	stillOpen := newReviewTaskWithPRURL(t, f, "https://github.com/owner/rq-repo/pull/9")

	provider := &batchingPRProvider{merged: map[string]bool{*merged.PRURL: true}}
	svc := service.NewTaskService(f.taskRepo, repository.NewAttemptRepository(f.db), &mockQueueClient{}, nil,
		service.WithTaskGitHubIntegration(f.integRepo, f.credRepo, f.cipher, provider),
	)

	if err := svc.ReconcileActivePRs(context.Background()); err != nil {
		t.Fatalf("ReconcileActivePRs: %v", err)
	}

	if provider.batchCalls != 1 {
		t.Fatalf("expected one batched request for one integration, got %d", provider.batchCalls)
	}
	if provider.batchSizes[0] != 3 {
		t.Errorf("expected the batch to carry all three pull requests, got %d", provider.batchSizes[0])
	}
	if provider.singleCalls != 0 {
		t.Errorf("batched providers must not be read one at a time, got %d single calls", provider.singleCalls)
	}
	if n := f.credRepo.getByID.Load(); n != 1 {
		t.Errorf("expected one credential decrypt per integration, got %d", n)
	}

	reloaded, _ := f.taskRepo.GetByID(context.Background(), merged.ID)
	if reloaded.Status != models.TaskStatusDone {
		t.Errorf("merged task: expected %s, got %s", models.TaskStatusDone, reloaded.Status)
	}
	for _, id := range []string{open.ID, stillOpen.ID} {
		task, _ := f.taskRepo.GetByID(context.Background(), id)
		if task.Status != models.TaskStatusReview {
			t.Errorf("open task %s: expected %s, got %s", id, models.TaskStatusReview, task.Status)
		}
	}
}

func TestTaskService_ReconcileActivePRs_FailedBatchLeavesTasksInReview(t *testing.T) {
	f := newQuarantineFixture(t)
	task := f.createReviewTaskWithPR(t)

	provider := &batchingPRProvider{
		merged:   map[string]bool{*task.PRURL: true},
		batchErr: errors.New("github graphql returned 502: bad gateway"),
	}
	svc := service.NewTaskService(f.taskRepo, repository.NewAttemptRepository(f.db), &mockQueueClient{}, nil,
		service.WithTaskGitHubIntegration(f.integRepo, f.credRepo, f.cipher, provider),
	)

	if err := svc.ReconcileActivePRs(context.Background()); err != nil {
		t.Fatalf("a failed batch must not fail the reconciler: %v", err)
	}
	reloaded, _ := f.taskRepo.GetByID(context.Background(), task.ID)
	if reloaded.Status != models.TaskStatusReview {
		t.Errorf("expected task to stay %s after a failed batch, got %s", models.TaskStatusReview, reloaded.Status)
	}
}
