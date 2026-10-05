// internal/models/models_test.go
package models_test

import (
	"testing"
	"time"

	"agent-task-manager/internal/models"
)

func TestEnumsAndValidation(t *testing.T) {
	// Verify TeamRole enum
	if !models.RoleOwner.IsValid() {
		t.Errorf("expected RoleOwner to be valid")
	}
	if !models.RoleAdmin.IsValid() || !models.RoleMember.IsValid() || !models.RoleViewer.IsValid() {
		t.Errorf("expected all defined roles to be valid")
	}
	if models.TeamRole(999).IsValid() {
		t.Errorf("expected 999 to be invalid TeamRole")
	}

	// Verify IntegrationProvider enum
	if !models.IntegrationTrello.IsValid() || !models.IntegrationGitHub.IsValid() || !models.IntegrationJira.IsValid() {
		t.Errorf("expected Integration providers to be valid")
	}
	if models.IntegrationProvider("gitlab").IsValid() {
		t.Errorf("expected gitlab to be invalid IntegrationProvider")
	}

	// Verify CredentialProvider enum
	if !models.ProviderGitHub.IsValid() || !models.ProviderJira.IsValid() || !models.ProviderTrello.IsValid() ||
		!models.ProviderOIDC.IsValid() || !models.ProviderGeneric.IsValid() {
		t.Errorf("expected Credential providers to be valid")
	}
	if models.CredentialProvider("invalid").IsValid() {
		t.Errorf("expected invalid CredentialProvider")
	}

	// Verify TaskStatus enum
	if !models.TaskStatusBacklog.IsValid() || !models.TaskStatusQueued.IsValid() ||
		!models.TaskStatusInProgress.IsValid() || !models.TaskStatusReview.IsValid() ||
		!models.TaskStatusDone.IsValid() || !models.TaskStatusFailed.IsValid() || !models.TaskStatusCancelled.IsValid() {
		t.Errorf("expected all task statuses to be valid")
	}
	if models.TaskStatus("unknown").IsValid() {
		t.Errorf("expected unknown to be invalid TaskStatus")
	}

	// Verify TaskPriority enum
	if !models.PriorityUrgent.IsValid() || !models.PriorityHigh.IsValid() ||
		!models.PriorityNormal.IsValid() || !models.PriorityLow.IsValid() {
		t.Errorf("expected all task priorities to be valid")
	}
	if models.TaskPriority(999).IsValid() {
		t.Errorf("expected 999 to be invalid TaskPriority")
	}

	// Verify AssigneeType enum
	if !models.AssigneeAgent.IsValid() || !models.AssigneeHuman.IsValid() {
		t.Errorf("expected agent and human to be valid AssigneeType")
	}
	if models.AssigneeType("robot").IsValid() {
		t.Errorf("expected robot to be invalid AssigneeType")
	}

	// Verify AttemptStatus enum
	if !models.AttemptQueued.IsValid() || !models.AttemptRunning.IsValid() ||
		!models.AttemptSucceeded.IsValid() || !models.AttemptFailed.IsValid() || !models.AttemptCancelled.IsValid() {
		t.Errorf("expected all attempt statuses to be valid")
	}
	if models.AttemptStatus("pending").IsValid() {
		t.Errorf("expected pending to be invalid AttemptStatus")
	}

	// Verify WebhookStatus enum
	if !models.WebhookReceived.IsValid() || !models.WebhookProcessed.IsValid() || !models.WebhookFailed.IsValid() {
		t.Errorf("expected all webhook statuses to be valid")
	}
	if models.WebhookStatus("dropped").IsValid() {
		t.Errorf("expected dropped to be invalid WebhookStatus")
	}

	// Verify validator instance on valid team
	team := models.Team{
		ID:   "team-1",
		Name: "Core Team",
		Slug: "core-team",
	}
	if err := models.Validate.Struct(team); err != nil {
		t.Fatalf("expected valid team, got: %v", err)
	}

	// Verify validator catches invalid membership role
	mem := models.TeamMembership{
		TeamID: "team-1",
		UserID: "user-1",
		Role:   models.TeamRole(999),
	}
	if err := models.Validate.Struct(mem); err == nil {
		t.Errorf("expected validation error on invalid TeamRole, got nil")
	}

	// Verify validator on User
	validUser := models.User{
		ID:      "user-1",
		Issuer:  "https://accounts.google.com",
		Subject: "sub-12345",
		Email:   "user@example.com",
	}
	if err := models.Validate.Struct(validUser); err != nil {
		t.Errorf("expected valid user, got: %v", err)
	}
	invalidUser := validUser
	invalidUser.Issuer = "not-a-url"
	if err := models.Validate.Struct(invalidUser); err == nil {
		t.Errorf("expected invalid user issuer URL error")
	}

	// Verify validator on Project with embedded Team
	validProj := models.Project{
		ID:       "proj-1",
		TeamID:   "team-1",
		Name:     "Project Alpha",
		RepoPath: "/srv/agent-tasks/repos/proj-1",
		Team:     team,
	}
	if err := models.Validate.Struct(validProj); err != nil {
		t.Errorf("expected valid project, got: %v", err)
	}

	// Verify validator on Credential
	validCred := models.Credential{
		ID:       "cred-1",
		TeamID:   "team-1",
		Provider: models.ProviderGitHub,
		Label:    "Team GitHub App",
		Team:     team,
	}
	if err := models.Validate.Struct(validCred); err != nil {
		t.Errorf("expected valid credential, got: %v", err)
	}
	invalidCred := validCred
	invalidCred.Provider = models.CredentialProvider("invalid_prov")
	if err := models.Validate.Struct(invalidCred); err == nil {
		t.Errorf("expected invalid credential provider error")
	}

	// Verify validator on Task
	validTask := models.Task{
		ID:           "task-1",
		ProjectID:    "proj-1",
		Title:        "Implement feature",
		Prompt:       "Do work",
		AssigneeID:   "agent-1",
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityNormal,
		Project:      validProj,
	}
	if err := models.Validate.Struct(validTask); err != nil {
		t.Errorf("expected valid task, got: %v", err)
	}
	invalidTaskStatus := validTask
	invalidTaskStatus.Status = models.TaskStatus("not_real")
	if err := models.Validate.Struct(invalidTaskStatus); err == nil {
		t.Errorf("expected invalid task status error")
	}
}

func TestEnumScanAndValue(t *testing.T) {
	// TeamRole
	r := models.RoleOwner
	val, err := r.Value()
	if err != nil || val != int64(40) {
		t.Errorf("unexpected RoleOwner value: %v, %v", val, err)
	}
	var scannedRole models.TeamRole
	if err := scannedRole.Scan(int64(30)); err != nil || scannedRole != models.RoleAdmin {
		t.Errorf("unexpected scanned role: %v, %v", scannedRole, err)
	}
	if err := scannedRole.Scan(int(10)); err != nil || scannedRole != models.RoleViewer {
		t.Errorf("unexpected scanned role int: %v, %v", scannedRole, err)
	}
	if err := scannedRole.Scan(int64(999)); err == nil {
		t.Errorf("expected error scanning invalid role")
	}
	if _, err := models.TeamRole(999).Value(); err == nil {
		t.Errorf("expected error calling Value() on invalid role")
	}

	// TaskPriority
	p := models.PriorityHigh
	pVal, err := p.Value()
	if err != nil || pVal != int64(75) {
		t.Errorf("unexpected PriorityHigh value: %v, %v", pVal, err)
	}
	var scannedPriority models.TaskPriority
	if err := scannedPriority.Scan(int64(100)); err != nil || scannedPriority != models.PriorityUrgent {
		t.Errorf("unexpected scanned priority: %v, %v", scannedPriority, err)
	}
	if err := scannedPriority.Scan(999); err == nil {
		t.Errorf("expected error scanning invalid priority")
	}
}

func TestJSONMapValueAndScan(t *testing.T) {
	config := models.JSONMap{"board_id": "trello-123", "list_id": "list-456"}
	val, err := config.Value()
	if err != nil {
		t.Fatalf("expected nil error on Value(), got %v", err)
	}

	var scanned models.JSONMap
	if err := scanned.Scan(val); err != nil {
		t.Fatalf("expected nil error on Scan(), got %v", err)
	}

	if scanned["board_id"] != "trello-123" {
		t.Errorf("unexpected scanned board_id: %v", scanned["board_id"])
	}

	// nil handling
	var nilMap models.JSONMap
	nilVal, err := nilMap.Value()
	if err != nil || nilVal != "{}" {
		t.Errorf("expected empty JSON object for nil JSONMap, got %v", nilVal)
	}
	var scannedNil models.JSONMap
	if err := scannedNil.Scan(nil); err != nil || scannedNil == nil {
		t.Errorf("expected empty JSONMap for nil Scan, got %v", scannedNil)
	}
}

func TestStringArrayValueAndScan(t *testing.T) {
	arr := models.StringArray{"test-driven-development", "systematic-debugging"}
	val, err := arr.Value()
	if err != nil {
		t.Fatalf("expected nil error on Value(), got %v", err)
	}

	var scanned models.StringArray
	if err := scanned.Scan(val); err != nil {
		t.Fatalf("expected nil error on Scan(), got %v", err)
	}

	if len(scanned) != 2 || scanned[0] != "test-driven-development" || scanned[1] != "systematic-debugging" {
		t.Errorf("unexpected scanned value: %+v", scanned)
	}

	// nil handling
	var nilArr models.StringArray
	nilVal, err := nilArr.Value()
	if err != nil || nilVal != "[]" {
		t.Errorf("expected empty JSON array for nil StringArray, got %v", nilVal)
	}
	var scannedNil models.StringArray
	if err := scannedNil.Scan(nil); err != nil || scannedNil == nil {
		t.Errorf("expected empty slice for nil Scan, got %v", scannedNil)
	}
}

func TestModelCreationSanity(t *testing.T) {
	now := time.Now()
	team := models.Team{
		ID:   "team-1",
		Name: "Core Team",
		Slug: "core-team",
	}
	user := models.User{
		ID:      "user-1",
		Issuer:  "https://issuer.example.com",
		Subject: "subject-1",
	}
	proj := models.Project{
		ID:       "proj-1",
		TeamID:   "team-1",
		Name:     "Project A",
		RepoPath: "/repos/proj-1",
		Team:     team,
	}
	cred := models.Credential{
		ID:       "cred-1",
		TeamID:   "team-1",
		Provider: models.ProviderGitHub,
		Label:    "Cred GitHub",
		Team:     team,
	}
	task := models.Task{
		ID:           "task-1",
		ProjectID:    "proj-1",
		Title:        "Title",
		Prompt:       "Prompt",
		AssigneeID:   "agent-1",
		Status:       models.TaskStatusQueued,
		AssigneeType: models.AssigneeAgent,
		Priority:     models.PriorityNormal,
		Project:      proj,
	}

	token := models.APIToken{
		ID:        "tok-1",
		UserID:    "user-1",
		TeamID:    "team-1",
		Name:      "test-token",
		TokenHash: "hash-xyz",
		CreatedAt: now,
		User:      user,
		Team:      team,
	}
	if err := models.Validate.Struct(token); err != nil {
		t.Errorf("expected valid APIToken, got %v", err)
	}

	integ := models.ProjectIntegration{
		ID:           "integ-1",
		ProjectID:    "proj-1",
		CredentialID: "cred-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "github-repo-123",
		Config:       models.JSONMap{"repo": "owner/repo"},
		Enabled:      true,
		Project:      proj,
		Credential:   cred,
	}
	if err := models.Validate.Struct(integ); err != nil {
		t.Errorf("expected valid ProjectIntegration, got %v", err)
	}

	attempt := models.TaskAttempt{
		ID:            "att-1",
		TaskID:        "task-1",
		AttemptNumber: 1,
		Status:        models.AttemptRunning,
		Task:          task,
	}
	if err := models.Validate.Struct(attempt); err != nil {
		t.Errorf("expected valid TaskAttempt, got %v", err)
	}

	log := models.TaskLog{
		TaskID:   "task-1",
		LogChunk: "worker started",
		Task:     task,
	}
	if err := models.Validate.Struct(log); err != nil {
		t.Errorf("expected valid TaskLog, got %v", err)
	}

	webhook := models.WebhookEvent{
		Provider:        "github",
		ProjectID:       "proj-1",
		ProviderEventID: "evt-99",
		Status:          models.WebhookReceived,
		Project:         proj,
	}
	if err := models.Validate.Struct(webhook); err != nil {
		t.Errorf("expected valid WebhookEvent, got %v", err)
	}
}
