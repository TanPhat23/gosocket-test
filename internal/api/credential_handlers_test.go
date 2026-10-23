package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func setupCredentialTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory db: %v", err)
	}

	tables := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			email TEXT,
			display_name TEXT,
			git_name TEXT,
			git_email TEXT,
			sessions_valid_after DATETIME
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
	}

	for _, stmt := range tables {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to execute schema stmt %s: %v", stmt, err)
		}
	}

	return db
}

func TestCredentialHandlers(t *testing.T) {
	db := setupCredentialTestDB(t)
	masterKey := []byte("01234567890123456789012345678901")
	cipher, err := crypto.NewSecretCipher(masterKey)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	sessionSecret := []byte("secret-key-32-bytes-long-for-test!")
	sm := auth.NewSessionManager(sessionSecret, 24*time.Hour, false)

	// Seed user and team
	owner := models.User{ID: "usr_owner", Issuer: "https://idp.example.com", Subject: "owner"}
	member := models.User{ID: "usr_member", Issuer: "https://idp.example.com", Subject: "member"}
	viewer := models.User{ID: "usr_viewer", Issuer: "https://idp.example.com", Subject: "viewer"}
	outsider := models.User{ID: "usr_outsider", Issuer: "https://idp.example.com", Subject: "outsider"}
	db.Create(&owner)
	db.Create(&member)
	db.Create(&viewer)
	db.Create(&outsider)

	team := models.Team{ID: "team_1", Name: "Team One", Slug: "team-one"}
	db.Create(&team)

	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: owner.ID, Role: models.RoleOwner})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: member.ID, Role: models.RoleMember})
	db.Create(&models.TeamMembership{TeamID: team.ID, UserID: viewer.ID, Role: models.RoleViewer})

	app := fiber.New(fiber.Config{
		StructValidator: api.NewStructValidator(),
	})

	userRepo := repository.NewUserRepository(db)
	teamRepo := repository.NewTeamRepository(db)
	tokenRepo := repository.NewTokenRepository(db)
	credRepo := repository.NewCredentialRepository(db)

	authService := service.NewAuthService(userRepo, teamRepo, tokenRepo, nil)
	credService := service.NewCredentialService(credRepo, cipher)

	credHandlers := api.NewCredentialHandlers(credService)
	teamsGroup := app.Group("/api/v1/teams/:team_id", api.RequireAuth(authService, sm))
	credentialsGroup := teamsGroup.Group("/credentials", api.RequireTeamRole(authService, models.RoleOwner))
	credentialsGroup.Get("", credHandlers.List)
	credentialsGroup.Post("", credHandlers.Create)
	credentialsGroup.Put("/:credential_id", credHandlers.Update)
	credentialsGroup.Patch("/:credential_id", credHandlers.Update)
	credentialsGroup.Delete("/:credential_id", credHandlers.Delete)
	teamsGroup.Get("/credentials/:credential_id/github/repos", api.RequireTeamRole(authService, models.RoleViewer), credHandlers.ListGitHubRepositories)
	teamsGroup.Get("/credentials/:credential_id/github/branches", api.RequireTeamRole(authService, models.RoleViewer), credHandlers.ListGitHubBranches)

	makeAuthCookie := func(userID string) *http.Cookie {
		claims := auth.SessionClaims{IssuedAt: time.Now(), UserID: userID, ExpiresAt: time.Now().Add(1 * time.Hour)}
		tok, _ := sm.SignPayload(claims)
		return &http.Cookie{Name: auth.SessionCookieName, Value: tok}
	}

	t.Run("Create credential encrypts data and omits encrypted_data from JSON", func(t *testing.T) {
		payload := map[string]any{
			"provider": "github",
			"label":    "GitHub PAT for repo access",
			"secret":   "ghp_super_secret_pat_9999",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/v1/teams/team_1/credentials", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(makeAuthCookie(owner.ID))

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
		}

		var respBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&respBody)
		credData, ok := respBody["credential"].(map[string]any)
		if !ok {
			t.Fatalf("expected credential map in response: %+v", respBody)
		}

		// Verify encrypted_data and key_fingerprint are NEVER returned in response
		if _, exists := credData["encrypted_data"]; exists {
			t.Errorf("encrypted_data must NOT be exposed in JSON response")
		}
		if _, exists := credData["EncryptedData"]; exists {
			t.Errorf("EncryptedData must NOT be exposed in JSON response")
		}
		if _, exists := credData["key_fingerprint"]; exists {
			t.Errorf("key_fingerprint must NOT be exposed in JSON response")
		}
		if credData["label"] != "GitHub PAT for repo access" {
			t.Errorf("expected label to match")
		}
		if credData["provider"] != "github" {
			t.Errorf("expected provider github")
		}

		credID := credData["id"].(string)

		// Verify database row contains encrypted secret (not plaintext)
		var stored models.Credential
		err = db.First(&stored, "id = ?", credID).Error
		if err != nil {
			t.Fatalf("credential not found in db: %v", err)
		}

		if stored.EncryptedData == "ghp_super_secret_pat_9999" {
			t.Fatalf("stored credential data must NOT be plaintext")
		}

		// Decrypt and verify matching original secret
		decrypted, err := cipher.Decrypt(stored.EncryptedData)
		if err != nil {
			t.Fatalf("failed to decrypt stored credential: %v", err)
		}
		if decrypted != "ghp_super_secret_pat_9999" {
			t.Errorf("expected decrypted %s, got %s", "ghp_super_secret_pat_9999", decrypted)
		}
		if stored.KeyFingerprint != cipher.KeyFingerprint() {
			t.Errorf("expected key fingerprint %s, got %s", cipher.KeyFingerprint(), stored.KeyFingerprint)
		}
	})

	t.Run("Member cannot create credential (403)", func(t *testing.T) {
		payload := map[string]any{
			"provider": "jira",
			"label":    "Jira API Token",
			"secret":   "jira_token_123",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest("POST", "/api/v1/teams/team_1/credentials", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(makeAuthCookie(member.ID))

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for member, got %d", resp.StatusCode)
		}
	})

	t.Run("List credentials returns safe response without secrets", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/teams/team_1/credentials", nil)
		req.AddCookie(makeAuthCookie(owner.ID))

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
		}

		var listResp map[string][]map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&listResp)
		creds := listResp["credentials"]
		if len(creds) == 0 {
			t.Fatalf("expected credentials in list")
		}

		for _, c := range creds {
			if _, exists := c["encrypted_data"]; exists {
				t.Errorf("encrypted_data must not be in list response")
			}
			if _, exists := c["secret"]; exists {
				t.Errorf("secret must not be in list response")
			}
		}
	})

	t.Run("Update credential updates label and encrypted secret", func(t *testing.T) {
		var cred models.Credential
		db.First(&cred, "team_id = ?", team.ID)

		payload := map[string]any{
			"label":  "Updated GitHub PAT",
			"secret": "ghp_new_updated_token_12345",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest("PUT", "/api/v1/teams/team_1/credentials/"+cred.ID, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(makeAuthCookie(owner.ID))

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on update, got %d", resp.StatusCode)
		}

		var updated models.Credential
		if err := db.First(&updated, "id = ?", cred.ID).Error; err != nil {
			t.Fatalf("failed to find updated cred: %v", err)
		}
		if updated.Label != "Updated GitHub PAT" {
			t.Errorf("expected label 'Updated GitHub PAT', got %s", updated.Label)
		}
		decrypted, err := cipher.Decrypt(updated.EncryptedData)
		if err != nil || decrypted != "ghp_new_updated_token_12345" {
			t.Errorf("expected decrypted secret 'ghp_new_updated_token_12345', got %s (err: %v)", decrypted, err)
		}
	})

	t.Run("Delete credential works for owner", func(t *testing.T) {
		var cred models.Credential
		db.First(&cred, "team_id = ?", team.ID)

		req := httptest.NewRequest("DELETE", "/api/v1/teams/team_1/credentials/"+cred.ID, nil)
		req.AddCookie(makeAuthCookie(owner.ID))

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on delete, got %d", resp.StatusCode)
		}

		var check models.Credential
		err = db.First(&check, "id = ?", cred.ID).Error
		if err == nil {
			t.Errorf("expected credential to be deleted from db")
		}
	})

	t.Run("Delete nonexistent credential returns 404", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/teams/team_1/credentials/cred_nonexistent", nil)
		req.AddCookie(makeAuthCookie(owner.ID))

		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found on delete nonexistent, got %d", resp.StatusCode)
		}
	})

	t.Run("ListGitHubRepositories endpoint", func(t *testing.T) {
		// Create a GitHub credential
		ghEncrypted, _ := cipher.Encrypt("ghp_valid_test_token")
		ghCred := models.Credential{
			ID:             "cred_gh_test",
			TeamID:         team.ID,
			Provider:       models.ProviderGitHub,
			Label:          "GitHub PAT for tests",
			EncryptedData:  ghEncrypted,
			KeyFingerprint: cipher.KeyFingerprint(),
		}
		db.Create(&ghCred)

		// Create mock HTTP server simulating GitHub API
		ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer ghp_valid_test_token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.URL.Path == "/user/repos" {
				repos := []map[string]any{
					{
						"id":             999,
						"name":           "my-repo",
						"full_name":      "team/my-repo",
						"private":        false,
						"description":    "Test repo",
						"html_url":       "https://github.com/team/my-repo",
						"default_branch": "main",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(repos)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/branches") {
				branches := []map[string]any{
					{"name": "main"},
					{"name": "develop"},
					{"name": "feat/autonomous"},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(branches)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ghServer.Close()

		// Rebuild credService with github provider pointing to test server
		ghProvider := github.NewProvider(github.WithBaseURL(ghServer.URL), github.WithHTTPClient(ghServer.Client()))
		appWithGH := fiber.New(fiber.Config{
			StructValidator: api.NewStructValidator(),
		})
		customCredSvc := service.NewCredentialService(credRepo, cipher, service.WithGitHubProvider(ghProvider))
		customHandlers := api.NewCredentialHandlers(customCredSvc)

		tg := appWithGH.Group("/api/v1/teams/:team_id", api.RequireAuth(authService, sm))
		tg.Get("/credentials/:credential_id/github/repos", api.RequireTeamRole(authService, models.RoleViewer), customHandlers.ListGitHubRepositories)
		tg.Get("/credentials/:credential_id/github/branches", api.RequireTeamRole(authService, models.RoleViewer), customHandlers.ListGitHubBranches)

		// 1. Team Viewer gets 200 OK and repositories
		req := httptest.NewRequest("GET", "/api/v1/teams/team_1/credentials/cred_gh_test/github/repos", nil)
		req.AddCookie(makeAuthCookie(viewer.ID))
		resp, err := appWithGH.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK for viewer, got %d", resp.StatusCode)
		}
		var respData map[string][]map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&respData)
		if len(respData["repositories"]) != 1 || respData["repositories"][0]["name"] != "my-repo" {
			t.Fatalf("unexpected repositories in response: %+v", respData)
		}

		// 1b. Team Viewer gets branches for a repository
		reqBranches := httptest.NewRequest("GET", "/api/v1/teams/team_1/credentials/cred_gh_test/github/branches?repo=team/my-repo", nil)
		reqBranches.AddCookie(makeAuthCookie(viewer.ID))
		respB, err := appWithGH.Test(reqBranches)
		if err != nil || respB.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK on branches, got %d (err: %v)", respB.StatusCode, err)
		}
		var branchData map[string][]string
		_ = json.NewDecoder(respB.Body).Decode(&branchData)
		if len(branchData["branches"]) != 3 || branchData["branches"][0] != "main" {
			t.Fatalf("unexpected branches in response: %+v", branchData)
		}

		// 2. Outsider gets 403 Forbidden
		reqOutsider := httptest.NewRequest("GET", "/api/v1/teams/team_1/credentials/cred_gh_test/github/repos", nil)
		reqOutsider.AddCookie(makeAuthCookie(outsider.ID))
		respOutsider, err := appWithGH.Test(reqOutsider)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if respOutsider.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for outsider, got %d", respOutsider.StatusCode)
		}

		// 3. Unauthenticated gets 401 Unauthorized
		reqUnauth := httptest.NewRequest("GET", "/api/v1/teams/team_1/credentials/cred_gh_test/github/repos", nil)
		respUnauth, err := appWithGH.Test(reqUnauth)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if respUnauth.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for unauthenticated, got %d", respUnauth.StatusCode)
		}

		// 4. Non-existent credential returns 404 Not Found
		req404 := httptest.NewRequest("GET", "/api/v1/teams/team_1/credentials/non_existent/github/repos", nil)
		req404.AddCookie(makeAuthCookie(viewer.ID))
		resp404, err := appWithGH.Test(req404)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp404.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for non-existent credential, got %d", resp404.StatusCode)
		}
	})
}
