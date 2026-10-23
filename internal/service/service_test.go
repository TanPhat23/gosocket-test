package service_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type mockAuthenticator struct {
	authCodeURLFunc func(state, codeChallenge, nonce string) string
	exchangeFunc    func(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error)
	verifyIDToken   func(ctx context.Context, rawIDToken string) (*oidc.IDToken, error)
	state           *auth.OIDCState
	challenge       string
	issuerURL       string
}

func (m *mockAuthenticator) AuthCodeURL(state, codeChallenge, nonce string) string {
	if m.authCodeURLFunc != nil {
		return m.authCodeURLFunc(state, codeChallenge, nonce)
	}
	return "https://idp.example.com/auth?state=" + state
}

func (m *mockAuthenticator) Exchange(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error) {
	if m.exchangeFunc != nil {
		return m.exchangeFunc(ctx, code, codeVerifier)
	}
	token := &oauth2.Token{
		AccessToken: "mock-access-token",
		TokenType:   "Bearer",
	}
	return token.WithExtra(map[string]any{
		"id_token": "mock-raw-id-token",
	}), nil
}

func (m *mockAuthenticator) VerifyIDToken(ctx context.Context, rawIDToken string) (*oidc.IDToken, error) {
	if m.verifyIDToken != nil {
		return m.verifyIDToken(ctx, rawIDToken)
	}
	return &oidc.IDToken{
		Issuer:  m.IssuerURL(),
		Subject: "sub-12345",
		Nonce:   m.state.Nonce,
	}, nil
}

func (m *mockAuthenticator) GenerateOIDCState() (*auth.OIDCState, string, error) {
	if m.state == nil {
		m.state = &auth.OIDCState{
			State:        "test-state-abc",
			Nonce:        "test-nonce-def",
			CodeVerifier: "test-verifier-ghi",
			ExpiresAt:    time.Now().Add(10 * time.Minute),
		}
		m.challenge = auth.GenerateCodeChallenge(m.state.CodeVerifier)
	}
	return m.state, m.challenge, nil
}

func (m *mockAuthenticator) SignOIDCState(state *auth.OIDCState) (string, error) {
	mac := auth.NewSessionManager([]byte("mock-32-byte-secret-for-testing!"), 10*time.Minute, false)
	claims := auth.SessionClaims{
		IssuedAt: time.Now(),
		UserID:    state.State,
		Issuer:    state.Nonce,
		Subject:   state.CodeVerifier,
		ExpiresAt: state.ExpiresAt,
	}
	return mac.SignPayload(claims)
}

func (m *mockAuthenticator) VerifyOIDCState(signedState string) (*auth.OIDCState, error) {
	mac := auth.NewSessionManager([]byte("mock-32-byte-secret-for-testing!"), 10*time.Minute, false)
	claims, err := mac.VerifyPayload(signedState)
	if err != nil {
		return nil, auth.ErrInvalidState
	}
	return &auth.OIDCState{
		State:        claims.UserID,
		Nonce:        claims.Issuer,
		CodeVerifier: claims.Subject,
		ExpiresAt:    claims.ExpiresAt,
	}, nil
}

func (m *mockAuthenticator) IssuerURL() string {
	if m.issuerURL != "" {
		return m.issuerURL
	}
	return "https://idp.example.com"
}

func TestAuthService_FullLifecycle(t *testing.T) {
	db := setupTestDB(t)
	userRepo := repository.NewUserRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	tokenRepo := repository.NewTokenRepository(db)

	mockAuth := &mockAuthenticator{}
	authSvc := service.NewAuthService(userRepo, teamRepo, tokenRepo, mockAuth)
	ctx := context.Background()

	// 1. GenerateLoginURL
	authURL, signedState, state, err := authSvc.GenerateLoginURL()
	if err != nil {
		t.Fatalf("GenerateLoginURL failed: %v", err)
	}
	if authURL == "" || signedState == "" || state == nil {
		t.Fatalf("unexpected nil or empty in login url generation")
	}

	// 2. HandleCallback
	user, err := authSvc.HandleCallback(ctx, signedState, state.State, "mock-code")
	if err != nil {
		t.Fatalf("HandleCallback failed: %v", err)
	}
	if user.Subject != "sub-12345" {
		t.Errorf("expected subject sub-12345, got %s", user.Subject)
	}

	// 3. GetUserByID
	fetchedUser, err := authSvc.GetUserByID(ctx, user.ID)
	if err != nil || fetchedUser.ID != user.ID {
		t.Fatalf("GetUserByID failed: %v", err)
	}

	// 4. AuthenticatePAT
	rawToken, tokenHash, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("failed to generate PAT: %v", err)
	}
	apiToken := models.APIToken{
		ID:        "tok-test",
		UserID:    user.ID,
		TeamID:    "team-1",
		Name:      "test pat",
		TokenHash: tokenHash,
	}
	db.Create(&apiToken)

	patUser, patToken, err := authSvc.AuthenticatePAT(ctx, rawToken)
	if err != nil {
		t.Fatalf("AuthenticatePAT failed: %v", err)
	}
	if patUser.ID != user.ID || patToken.ID != "tok-test" {
		t.Fatalf("unexpected user/token from AuthenticatePAT")
	}

	// 5. AuthorizeTeamRole
	team := models.Team{ID: "team-1", Name: "Engineering", Slug: "engineering"}
	db.Create(&team)
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: user.ID, Role: models.RoleAdmin})

	role, err := authSvc.AuthorizeTeamRole(ctx, "team-1", user.ID, models.RoleMember)
	if err != nil {
		t.Fatalf("AuthorizeTeamRole failed: %v", err)
	}
	if role != models.RoleAdmin {
		t.Errorf("expected RoleAdmin, got %v", role)
	}

	_, err = authSvc.AuthorizeTeamRole(ctx, "team-1", user.ID, models.RoleOwner)
	if err == nil {
		t.Fatalf("expected forbidden error for owner requirement")
	}
}

func TestCredentialService_EncryptionAndSecurityInvariants(t *testing.T) {
	db := setupTestDB(t)
	credRepo := repository.NewCredentialRepository(db)
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))
	credSvc := service.NewCredentialService(credRepo, cipher)
	ctx := context.Background()

	// Create
	createdDTO, err := credSvc.Create(ctx, "team-1", dto.CreateCredentialRequest{
		Provider: models.ProviderGitHub,
		Label:    "GitHub Token",
		Secret:   "ghp_secrettoken123",
	})
	if err != nil {
		t.Fatalf("Create credential failed: %v", err)
	}
	if createdDTO.Label != "GitHub Token" || createdDTO.TeamID != "team-1" {
		t.Fatalf("unexpected dto fields: %+v", createdDTO)
	}

	// Verify DB record has encrypted data, not plain text
	var cred models.Credential
	if err := db.First(&cred, "id = ?", createdDTO.ID).Error; err != nil {
		t.Fatalf("failed to query credential from DB: %v", err)
	}
	if cred.EncryptedData == "" || cred.EncryptedData == "ghp_secrettoken123" {
		t.Fatalf("security violation: raw secret stored unencrypted")
	}

	// List
	list, err := credSvc.List(ctx, "team-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("List failed: %v, len %d", err, len(list))
	}
	if list[0].ID != createdDTO.ID {
		t.Fatalf("mismatched id in list")
	}

	// Delete
	if err := credSvc.Delete(ctx, "team-1", createdDTO.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	listAfter, _ := credSvc.List(ctx, "team-1")
	if len(listAfter) != 0 {
		t.Fatalf("expected 0 credentials after delete")
	}

	t.Run("ListGitHubRepositories validates provider and decrypts token", func(t *testing.T) {
		// Non-existent credential
		_, err := credSvc.ListGitHubRepositories(ctx, "team-1", "non-existent")
		if !errors.Is(err, service.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound, got %v", err)
		}

		// Create Jira credential
		jiraCred, err := credSvc.Create(ctx, "team-1", dto.CreateCredentialRequest{
			Provider: models.ProviderJira,
			Label:    "Jira Token",
			Secret:   "jira_secret_token",
		})
		if err != nil {
			t.Fatalf("failed to create jira cred: %v", err)
		}

		// List repos on Jira cred should fail
		_, err = credSvc.ListGitHubRepositories(ctx, "team-1", jiraCred.ID)
		if !errors.Is(err, service.ErrInvalidProvider) {
			t.Errorf("expected ErrInvalidProvider, got %v", err)
		}

		// Create GitHub credential
		ghCred, err := credSvc.Create(ctx, "team-1", dto.CreateCredentialRequest{
			Provider: models.ProviderGitHub,
			Label:    "GitHub PAT",
			Secret:   "ghp_test_pat_secret",
		})
		if err != nil {
			t.Fatalf("failed to create gh cred: %v", err)
		}

		// Mock GitHub provider
		mockGH := &mockGitHubRepoLister{
			fn: func(ctx context.Context, token string) ([]dto.GitHubRepositoryDTO, error) {
				if token != "ghp_test_pat_secret" {
					return nil, errors.New("decrypted token mismatch")
				}
				return []dto.GitHubRepositoryDTO{
					{
						ID:       101,
						Name:     "atm",
						FullName: "test/atm",
					},
				}, nil
			},
		}

		ghCredSvc := service.NewCredentialService(credRepo, cipher, service.WithGitHubProvider(mockGH))
		repos, err := ghCredSvc.ListGitHubRepositories(ctx, "team-1", ghCred.ID)
		if err != nil {
			t.Fatalf("unexpected error listing repos: %v", err)
		}
		if len(repos) != 1 || repos[0].Name != "atm" {
			t.Errorf("expected 1 repo atm, got %+v", repos)
		}

		// Tenant isolation: team-2 cannot access team-1's credential
		_, err = ghCredSvc.ListGitHubRepositories(ctx, "team-2", ghCred.ID)
		if !errors.Is(err, service.ErrCredentialNotFound) {
			t.Errorf("expected ErrCredentialNotFound for different team, got %v", err)
		}
	})
}

type mockGitHubRepoLister struct {
	fn         func(ctx context.Context, token string) ([]dto.GitHubRepositoryDTO, error)
	branchesFn func(ctx context.Context, token, owner, repo string) ([]string, error)
}

func (m *mockGitHubRepoLister) ListRepositories(ctx context.Context, token string) ([]dto.GitHubRepositoryDTO, error) {
	if m.fn != nil {
		return m.fn(ctx, token)
	}
	return nil, nil
}

func (m *mockGitHubRepoLister) ListBranches(ctx context.Context, token, owner, repo string) ([]string, error) {
	if m.branchesFn != nil {
		return m.branchesFn(ctx, token, owner, repo)
	}
	return []string{"main", "develop"}, nil
}

func TestWebhookService_GitHubHMACAndIdempotency(t *testing.T) {
	db := setupTestDB(t)
	integRepo := repository.NewIntegrationRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	idempStore := integrations.NewMemoryIdempotencyStore()
	cipher, _ := crypto.NewSecretCipher([]byte("01234567890123456789012345678901"))

	mockQueue := &mockQueueClient{}
	webhookSvc := service.NewWebhookService(integRepo, credRepo, taskRepo, idempStore, cipher, mockQueue)
	ctx := context.Background()

	// Seed team, project, credential, integration
	team := models.Team{ID: "team-1", Name: "Team 1", Slug: "team-1"}
	db.Create(&team)
	project := models.Project{ID: "proj-1", TeamID: "team-1", Name: "Project 1", RepoPath: "/tmp/repo"}
	db.Create(&project)

	secret := "webhook-secret-xyz"
	encrypted, _ := cipher.Encrypt(secret)
	cred := models.Credential{
		ID:            "cred-1",
		TeamID:        "team-1",
		Provider:      models.ProviderGitHub,
		Label:         "GH Secret",
		EncryptedData: encrypted,
	}
	db.Create(&cred)

	integ := models.ProjectIntegration{
		ID:           "integ-1",
		ProjectID:    "proj-1",
		CredentialID: "cred-1",
		Provider:     models.IntegrationGitHub,
		ExternalID:   "owner/repo",
		Enabled:      true,
	}
	db.Create(&integ)

	payload := []byte(`{"action":"opened","repository":{"full_name":"owner/repo"},"issue":{"number":99,"title":"Issue 99","body":"Fix it","html_url":"https://github.com/owner/repo/issues/99"}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Invalid signature
	_, err := webhookSvc.HandleGitHubWebhook(ctx, payload, "del-1", "sha256=invalid")
	if err == nil {
		t.Fatalf("expected error for invalid signature")
	}

	// Valid signature
	res, err := webhookSvc.HandleGitHubWebhook(ctx, payload, "del-1", validSig)
	if err != nil {
		t.Fatalf("HandleGitHubWebhook failed: %v", err)
	}
	if res.Status != "processed" {
		t.Errorf("expected status processed, got %s", res.Status)
	}

	// Verify task created
	createdTask, err := taskRepo.GetByProjectAndExternalID(ctx, "proj-1", "99")
	if err != nil || createdTask.Title != "Issue 99" {
		t.Fatalf("task was not created properly: %v", err)
	}

	// Duplicate delivery
	resDup, err := webhookSvc.HandleGitHubWebhook(ctx, payload, "del-1", validSig)
	if err != nil {
		t.Fatalf("duplicate webhook failed: %v", err)
	}
	if resDup.Status != "duplicate" {
		t.Errorf("expected duplicate status, got %s", resDup.Status)
	}
}
