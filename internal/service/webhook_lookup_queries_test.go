package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"gorm.io/gorm"
)

// taskReadCounter counts SELECTs against the tasks table and the rows they returned to Go.
type taskReadCounter struct {
	selects int
	rows    int
}

// newLookupFixture is newHardeningFixture plus a query callback that measures task reads.
func newLookupFixture(t *testing.T) (*hardeningFixture, *taskReadCounter) {
	t.Helper()
	db := setupTestDB(t)
	cipher, err := crypto.NewSecretCipher([]byte(hardeningCipherKey))
	if err != nil {
		t.Fatalf("failed to init cipher: %v", err)
	}
	counter := &taskReadCounter{}
	if err := db.Callback().Query().After("gorm:query").Register("test:task_reads", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			counter.selects++
			counter.rows += int(tx.RowsAffected)
		}
	}); err != nil {
		t.Fatalf("register task read counter: %v", err)
	}
	f := &hardeningFixture{
		ctx:       context.Background(),
		cipher:    cipher,
		integRepo: repository.NewIntegrationRepository(db),
		credRepo:  repository.NewCredentialRepository(db),
		taskRepo:  repository.NewTaskRepository(db),
	}
	f.svc = service.NewWebhookService(f.integRepo, f.credRepo, f.taskRepo,
		integrations.NewMemoryIdempotencyStore(), cipher, nil)
	return f, counter
}

// seedDecoys adds n tasks in projectID whose branch and PR URL match nothing the tests send.
func seedDecoys(t *testing.T, f *hardeningFixture, projectID string, n int, base time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		f.addTask(t, models.Task{
			ID:         fmt.Sprintf("decoy-%s-%03d", projectID, i),
			ProjectID:  projectID,
			BranchName: hardeningStr(fmt.Sprintf("feature/decoy-%03d", i)),
			PRURL:      hardeningStr(fmt.Sprintf("https://github.com/owner/repo/pull/%d", 1000+i)),
			Title:      "decoy", Prompt: "p", Status: models.TaskStatusReview,
			CreatedAt: base.Add(time.Duration(i) * time.Second), UpdatedAt: base,
		})
	}
}

// TestCheckRunBranchLookup_ReadsOnlyTheMatchingTask fails while findTaskByBranch loads every task in the project.
// With 40 decoy tasks, the lookup must return only the matching row to Go.
func TestCheckRunBranchLookup_ReadsOnlyTheMatchingTask(t *testing.T) {
	const secret = "lookup-branch-secret"
	const repo = "owner/lookup-branch"
	f, reads := newLookupFixture(t)
	f.addGitHub(t, "proj-lookup-branch", "integ-lookup-branch", repo, secret)
	base := time.Now().UTC().Add(-time.Hour)
	seedDecoys(t, f, "proj-lookup-branch", 40, base)
	f.addTask(t, models.Task{
		ID: "task-branch-target", ProjectID: "proj-lookup-branch", BranchName: hardeningStr("feature/target"),
		Title: "target", Prompt: "p", Status: models.TaskStatusReview, CreatedAt: base, UpdatedAt: base,
	})

	reads.selects, reads.rows = 0, 0
	body := fmt.Sprintf(`{"action":"completed","repository":{"full_name":%q},"check_run":{"name":"ci","conclusion":"failure","check_suite":{"head_branch":"feature/target"},"output":{"text":"x"}}}`, repo)
	payload := []byte(body)
	res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-lookup-branch", hardeningGitHubSig(secret, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TaskID != "task-branch-target" {
		t.Fatalf("TaskID = %q, want task-branch-target", res.TaskID)
	}
	t.Logf("branch lookup: %d task rows across %d SELECTs (40 decoys in project)", reads.rows, reads.selects)
	if reads.rows > 2 {
		t.Errorf("branch lookup returned %d task rows to Go across %d SELECTs; want only the matching row", reads.rows, reads.selects)
	}
}

// TestPRMergeLookup_ReadsOnlyTheMatchingTask fails while the PR URL lookup loads every task in the project.
func TestPRMergeLookup_ReadsOnlyTheMatchingTask(t *testing.T) {
	const secret = "lookup-pr-secret"
	const repo = "owner/lookup-pr"
	const prURL = "https://github.com/owner/lookup-pr/pull/77"
	f, reads := newLookupFixture(t)
	f.addGitHub(t, "proj-lookup-pr", "integ-lookup-pr", repo, secret)
	base := time.Now().UTC().Add(-time.Hour)
	seedDecoys(t, f, "proj-lookup-pr", 40, base)
	f.addTask(t, models.Task{
		ID: "task-pr-target", ProjectID: "proj-lookup-pr", PRURL: hardeningStr(prURL),
		BranchName: hardeningStr("feature/other"), Title: "target", Prompt: "p",
		Status: models.TaskStatusReview, CreatedAt: base, UpdatedAt: base,
	})

	reads.selects, reads.rows = 0, 0
	body := fmt.Sprintf(`{"action":"closed","repository":{"full_name":%q},"pull_request":{"number":77,"merged":true,"html_url":%q,"head":{"ref":"feature/unrelated"}}}`, repo, prURL)
	payload := []byte(body)
	res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-lookup-pr", hardeningGitHubSig(secret, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TaskID != "task-pr-target" {
		t.Fatalf("TaskID = %q, want task-pr-target", res.TaskID)
	}
	t.Logf("PR lookup: %d task rows across %d SELECTs (40 decoys in project)", reads.rows, reads.selects)
	if reads.rows > 2 {
		t.Errorf("PR lookup returned %d task rows to Go across %d SELECTs; want only the matching row", reads.rows, reads.selects)
	}
}

// scanOnlyTaskRepo hides the concrete repository's targeted lookups, so the service must use its fallback scan.
type scanOnlyTaskRepo struct {
	repository.TaskRepository
}

// TestCheckRunBranchLookup_FallbackScanMatchesSameRules keeps the scan path, used when the repository does not offer
// a targeted lookup, on the same matching rules: project scope, BranchName equality, and the task/<id> convention.
func TestCheckRunBranchLookup_FallbackScanMatchesSameRules(t *testing.T) {
	const secret = "lookup-fallback-secret"
	const repo = "owner/lookup-fallback"
	f, _ := newLookupFixture(t)
	f.addGitHub(t, "proj-lookup-fallback", "integ-lookup-fallback", repo, secret)
	base := time.Now().UTC().Add(-time.Hour)
	f.addTask(t, models.Task{
		ID: "task-fallback-target", ProjectID: "proj-lookup-fallback", BranchName: hardeningStr("feature/fallback"),
		Title: "target", Prompt: "p", Status: models.TaskStatusReview, CreatedAt: base, UpdatedAt: base,
	})
	f.addTask(t, models.Task{
		ID: "task-fallback-other-project", ProjectID: "proj-other", BranchName: hardeningStr("feature/cross"),
		Title: "other", Prompt: "p", Status: models.TaskStatusReview, CreatedAt: base, UpdatedAt: base,
	})

	cipher := f.cipher
	svc := service.NewWebhookService(f.integRepo, f.credRepo, scanOnlyTaskRepo{f.taskRepo},
		integrations.NewMemoryIdempotencyStore(), cipher, nil)

	send := func(branch, deliveryID string) string {
		body := fmt.Sprintf(`{"action":"completed","repository":{"full_name":%q},"check_run":{"name":"ci","conclusion":"failure","check_suite":{"head_branch":%q},"output":{"text":"x"}}}`, repo, branch)
		payload := []byte(body)
		res, err := svc.HandleGitHubWebhook(f.ctx, payload, deliveryID, hardeningGitHubSig(secret, payload))
		if err != nil {
			t.Fatalf("branch %q: unexpected error: %v", branch, err)
		}
		return res.TaskID
	}

	if got := send("feature/fallback", "del-fallback-1"); got != "task-fallback-target" {
		t.Errorf("BranchName match: TaskID = %q, want task-fallback-target", got)
	}
	if got := send("feature/cross", "del-fallback-2"); got != "" {
		t.Errorf("cross-project branch matched TaskID %q, want no match", got)
	}
	if got := send("feature/none", "del-fallback-3"); got != "" {
		t.Errorf("unknown branch matched TaskID %q, want no match", got)
	}
}

// TestCheckRunBranchLookup_NewestMatchInProjectWins pins the tie rule: when several tasks in the project share a
// branch, the newest (created_at DESC, as the list order was) is chosen, and a same-branch task in another project never is.
func TestCheckRunBranchLookup_NewestMatchInProjectWins(t *testing.T) {
	const secret = "lookup-newest-secret"
	const repo = "owner/lookup-newest"
	f, _ := newLookupFixture(t)
	f.addGitHub(t, "proj-lookup-newest", "integ-lookup-newest", repo, secret)
	base := time.Now().UTC().Add(-time.Hour)
	f.addTask(t, models.Task{
		ID: "task-older", ProjectID: "proj-lookup-newest", BranchName: hardeningStr("feature/dup"),
		Title: "old", Prompt: "p", Status: models.TaskStatusReview, CreatedAt: base, UpdatedAt: base,
	})
	f.addTask(t, models.Task{
		ID: "task-newer", ProjectID: "proj-lookup-newest", BranchName: hardeningStr("feature/dup"),
		Title: "new", Prompt: "p", Status: models.TaskStatusReview, CreatedAt: base.Add(time.Minute), UpdatedAt: base,
	})
	f.addTask(t, models.Task{
		ID: "task-newest-other-project", ProjectID: "proj-elsewhere", BranchName: hardeningStr("feature/dup"),
		Title: "elsewhere", Prompt: "p", Status: models.TaskStatusReview, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base,
	})

	body := fmt.Sprintf(`{"action":"completed","repository":{"full_name":%q},"check_run":{"name":"ci","conclusion":"failure","check_suite":{"head_branch":"feature/dup"},"output":{"text":"x"}}}`, repo)
	payload := []byte(body)
	res, err := f.svc.HandleGitHubWebhook(f.ctx, payload, "del-newest", hardeningGitHubSig(secret, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TaskID != "task-newer" {
		t.Errorf("TaskID = %q, want task-newer (newest in project)", res.TaskID)
	}
}
