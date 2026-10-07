package repository_test

import (
	"context"
	"testing"
	"time"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to connect sqlite test db: %v", err)
	}

	tables := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			email TEXT,
			display_name TEXT
		);`,
		`CREATE TABLE teams (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			name TEXT NOT NULL,
			slug TEXT NOT NULL
		);`,
		`CREATE TABLE team_memberships (
			team_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (team_id, user_id)
		);`,
		`CREATE TABLE api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			team_id TEXT NOT NULL,
			name TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			expires_at DATETIME,
			last_used_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE credentials (
			id TEXT PRIMARY KEY,
			team_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			label TEXT NOT NULL,
			encrypted_data TEXT NOT NULL,
			key_fingerprint TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE project_integrations (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			credential_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			external_id TEXT NOT NULL,
			external_url TEXT,
			config TEXT NOT NULL DEFAULT '{}',
			enabled BOOLEAN NOT NULL DEFAULT 1,
			last_synced_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			heartbeat_at DATETIME,
			integration_id TEXT,
			asynq_task_id TEXT,
			session_id TEXT,
			external_id TEXT,
			external_url TEXT,
			pr_url TEXT,
			worktree_path TEXT,
			branch_name TEXT,
			model TEXT,
			container_id TEXT,
			exit_code INTEGER,
			skills TEXT NOT NULL DEFAULT '[]',
			mcp_servers TEXT NOT NULL DEFAULT '[]',
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			prompt TEXT NOT NULL,
			assignee_id TEXT NOT NULL,
			status TEXT NOT NULL,
			assignee_type TEXT NOT NULL DEFAULT 'agent',
			priority INTEGER NOT NULL DEFAULT 50,
			parent_task_id TEXT
		);`,
		`CREATE TABLE task_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			task_id TEXT NOT NULL,
			attempt_id TEXT,
			log_chunk TEXT NOT NULL
		);`,
	}

	for _, stmt := range tables {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create table: %v", err)
		}
	}

	return db
}

func TestUserRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewUserRepository(db)
	ctx := context.Background()

	user := &models.User{
		ID:          "usr-1",
		Issuer:      "https://auth.example.com",
		Subject:     "sub-1",
		Email:       "user1@example.com",
		DisplayName: "User One",
	}

	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create user failed: %v", err)
	}

	byID, err := repo.GetByID(ctx, "usr-1")
	if err != nil || byID.Email != "user1@example.com" {
		t.Fatalf("GetByID failed: got %v, err %v", byID, err)
	}

	byIssuerSub, err := repo.GetByIssuerSubject(ctx, "https://auth.example.com", "sub-1")
	if err != nil || byIssuerSub.ID != "usr-1" {
		t.Fatalf("GetByIssuerSubject failed: got %v, err %v", byIssuerSub, err)
	}

	err = repo.Update(ctx, byID, map[string]any{"display_name": "Updated One"})
	if err != nil {
		t.Fatalf("Update user failed: %v", err)
	}

	updated, _ := repo.GetByID(ctx, "usr-1")
	if updated.DisplayName != "Updated One" {
		t.Errorf("expected Updated One, got %s", updated.DisplayName)
	}
}

func TestTeamRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTeamRepository(db)
	ctx := context.Background()

	team := &models.Team{
		ID:   "team-1",
		Name: "Alpha Team",
		Slug: "alpha",
	}
	db.Create(team)

	member := &models.TeamMembership{
		TeamID: "team-1",
		UserID: "usr-1",
		Role:   models.RoleAdmin,
	}
	db.Create(member)

	gotTeam, err := repo.GetByID(ctx, "team-1")
	if err != nil || gotTeam.Name != "Alpha Team" {
		t.Fatalf("GetByID team failed: got %v, err %v", gotTeam, err)
	}

	gotMember, err := repo.GetMembership(ctx, "team-1", "usr-1")
	if err != nil || gotMember.Role != models.RoleAdmin {
		t.Fatalf("GetMembership failed: got %v, err %v", gotMember, err)
	}
}

func TestTokenRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTokenRepository(db)
	ctx := context.Background()

	token := &models.APIToken{
		ID:        "tok-1",
		UserID:    "usr-1",
		TeamID:    "team-1",
		Name:      "test-token",
		TokenHash: "hash-1234",
	}
	db.Create(token)

	gotToken, err := repo.GetByHash(ctx, "hash-1234")
	if err != nil || gotToken.ID != "tok-1" {
		t.Fatalf("GetByHash failed: got %v, err %v", gotToken, err)
	}

	now := time.Now().Truncate(time.Second)
	if err := repo.UpdateLastUsed(ctx, "tok-1", now); err != nil {
		t.Fatalf("UpdateLastUsed failed: %v", err)
	}

	gotToken2, _ := repo.GetByHash(ctx, "hash-1234")
	if gotToken2.LastUsedAt == nil {
		t.Fatalf("expected last used at to be set")
	}
}

func TestCredentialRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewCredentialRepository(db)
	ctx := context.Background()

	cred := &models.Credential{
		ID:             "cred-1",
		TeamID:         "team-1",
		Provider:       models.ProviderGitHub,
		Label:          "GH Secret",
		EncryptedData:  "enc-data",
		KeyFingerprint: "fp-1",
	}

	if err := repo.Create(ctx, cred); err != nil {
		t.Fatalf("Create credential failed: %v", err)
	}

	list, err := repo.ListByTeamID(ctx, "team-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByTeamID failed: %v, len %d", err, len(list))
	}

	gotCred, err := repo.GetByIDAndTeam(ctx, "cred-1", "team-1")
	if err != nil || gotCred.Label != "GH Secret" {
		t.Fatalf("GetByIDAndTeam failed: %v, got %v", err, gotCred)
	}

	if err := repo.Delete(ctx, "cred-1", "team-1"); err != nil {
		t.Fatalf("Delete credential failed: %v", err)
	}

	_, err = repo.GetByIDAndTeam(ctx, "cred-1", "team-1")
	if err == nil {
		t.Fatalf("expected error for deleted credential")
	}
}

func TestIntegrationRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewIntegrationRepository(db)
	ctx := context.Background()

	cred := &models.Credential{
		ID:             "cred-1",
		TeamID:         "team-1",
		Provider:       models.ProviderGitHub,
		Label:          "GH Secret",
		EncryptedData:  "enc-data",
		KeyFingerprint: "fp-1",
	}
	db.Create(cred)

	integ := &models.ProjectIntegration{
		ID:           "integ-1",
		ProjectID:    "proj-1",
		CredentialID: "cred-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/repo",
		Enabled:      true,
	}
	db.Create(integ)

	got, err := repo.GetActiveByProviderAndExternalID(ctx, models.IntegrationGitHub, "owner/repo")
	if err != nil {
		t.Fatalf("GetActiveByProviderAndExternalID failed: %v", err)
	}
	if got.CredentialID != "cred-1" {
		t.Errorf("expected CredentialID cred-1, got %s", got.CredentialID)
	}
}

func TestTaskRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewTaskRepository(db)
	ctx := context.Background()

	extID := "ext-100"
	task := &models.Task{
		ID:           "task-1",
		ProjectID:    "proj-1",
		ExternalID:   &extID,
		Title:        "Title",
		Prompt:       "Prompt",
		AssigneeID:   "agent",
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Skills:       models.StringArray{},
		MCPServers:   models.StringArray{},
	}

	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create task failed: %v", err)
	}

	byID, err := repo.GetByID(ctx, "task-1")
	if err != nil || byID.Title != "Title" {
		t.Fatalf("GetByID failed: %v, got %v", err, byID)
	}

	byExt, err := repo.GetByProjectAndExternalID(ctx, "proj-1", "ext-100")
	if err != nil || byExt.ID != "task-1" {
		t.Fatalf("GetByProjectAndExternalID failed: %v, got %v", err, byExt)
	}

	task.Title = "Updated Title"
	if err := repo.Update(ctx, task); err != nil {
		t.Fatalf("Update task failed: %v", err)
	}

	if err := repo.UpdateStatus(ctx, "task-1", models.TaskStatusInProgress); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	reloaded, _ := repo.GetByID(ctx, "task-1")
	if reloaded.Status != models.TaskStatusInProgress || reloaded.Title != "Updated Title" {
		t.Errorf("unexpected task fields: %+v", reloaded)
	}
}

func TestLogRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewLogRepository(db)
	ctx := context.Background()

	log1 := &models.TaskLog{
		TaskID:    "task-1",
		LogChunk:  "first line\n",
		Timestamp: time.Now().Add(-1 * time.Minute),
	}
	log2 := &models.TaskLog{
		TaskID:    "task-1",
		LogChunk:  "second line\n",
		Timestamp: time.Now(),
	}

	if err := repo.Append(ctx, log1); err != nil {
		t.Fatalf("Append log1 failed: %v", err)
	}
	if err := repo.Append(ctx, log2); err != nil {
		t.Fatalf("Append log2 failed: %v", err)
	}

	logs, err := repo.ListByTaskID(ctx, "task-1")
	if err != nil || len(logs) != 2 {
		t.Fatalf("ListByTaskID failed: %v, len %d", err, len(logs))
	}
	if logs[0].LogChunk != "first line\n" || logs[1].LogChunk != "second line\n" {
		t.Errorf("unexpected log chunks order: %+v", logs)
	}
}
