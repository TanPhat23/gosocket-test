package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/db"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/integrations/jira"
	"agent-task-manager/internal/integrations/trello"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	fmt.Printf("Starting Agent Task Manager Control Plane on port %d (env: %s)\n", cfg.Port, cfg.AppEnv)

	// Database connection (migrations handled separately via cmd/migrate)
	gormDB, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// Seed default team and project if none exist
	var teamCount int64
	gormDB.Model(&models.Team{}).Count(&teamCount)
	if teamCount == 0 {
		teamID := common.NewID()
		projectID := common.NewID()
		defaultTeam := &models.Team{
			ID:   teamID,
			Name: "Core Team",
			Slug: "core-team",
		}
		if err := gormDB.Create(defaultTeam).Error; err == nil {
			defaultProject := &models.Project{
				ID:       projectID,
				TeamID:   teamID,
				Name:     "Core Workspace",
				RepoPath: "/tmp/agent-tasks/repos/core",
			}
			_ = gormDB.Create(defaultProject)
			log.Printf("Seeded default team (%s) and project (%s)", teamID, projectID)
		}
	}

	// Task Queue Client
	queueClient, err := queue.NewClient(cfg, gormDB)
	if err != nil {
		log.Fatalf("failed to initialize queue client: %v", err)
	}
	defer func() {
		_ = queueClient.Close()
	}()

	// WebSocket Hub
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	// Repositories
	userRepo := repository.NewUserRepository(gormDB)
	tokenRepo := repository.NewTokenRepository(gormDB)
	teamRepo := repository.NewTeamRepository(gormDB)
	projectRepo := repository.NewProjectRepository(gormDB)
	credRepo := repository.NewCredentialRepository(gormDB)
	taskRepo := repository.NewTaskRepository(gormDB)
	attemptRepo := repository.NewAttemptRepository(gormDB)
	logRepo := repository.NewLogRepository(gormDB)
	integrationRepo := repository.NewIntegrationRepository(gormDB)

	// Cryptographic Cipher
	masterKey := []byte(cfg.SecretEncryptionKey)
	if len(masterKey) != 32 {
		log.Fatalf("SECRET_ENCRYPTION_KEY must be exactly 32 bytes (got %d)", len(masterKey))
	}
	cipher, err := crypto.NewGCMCipher(masterKey)
	if err != nil {
		log.Fatalf("failed to initialize secret cipher: %v", err)
	}

	// Idempotency store and integration registry
	idempotencyStore := integrations.NewDBIdempotencyStore(gormDB)
	integrationRegistry := integrations.NewRegistry()
	_ = integrationRegistry.Register(github.NewProvider())
	_ = integrationRegistry.Register(jira.NewProvider())
	_ = integrationRegistry.Register(trello.NewProvider())

	// OIDC Authenticator if configured
	var authenticator auth.Authenticator
	if cfg.OIDCIssuerURL != "" && cfg.OIDCClientID != "" {
		oidcCfg := auth.OIDCConfig{
			IssuerURL:         cfg.OIDCIssuerURL,
			InternalIssuerURL: cfg.OIDCInternalIssuerURL,
			ClientID:          cfg.OIDCClientID,
			ClientSecret:      cfg.OIDCClientSecret,
			RedirectURL:       cfg.OIDCRedirectURL,
			CookieSecret:      []byte(cfg.SessionSecret),
			SecureCookie:      cfg.AppEnv == "production",
		}
		for attempt := 1; attempt <= 10; attempt++ {
			oidcClient, err := auth.NewOIDCClient(context.Background(), oidcCfg)
			if err == nil {
				authenticator = oidcClient
				log.Printf("OIDC client successfully initialized with issuer: %s", cfg.OIDCIssuerURL)
				break
			}
			if attempt == 10 {
				log.Printf("warning: failed to initialize OIDC client after 10 attempts: %v", err)
			} else {
				log.Printf("OIDC initialization attempt %d waiting for provider (%v), retrying in 1s...", attempt, err)
				time.Sleep(1 * time.Second)
			}
		}
	}

	// Session Manager (30 days TTL)
	sessionManager := auth.NewSessionManager([]byte(cfg.SessionSecret), 30*24*time.Hour, cfg.AppEnv == "production")

	// Services
	authService := service.NewAuthService(userRepo, teamRepo, tokenRepo, authenticator)
	credService := service.NewCredentialService(credRepo, cipher)
	webhookService := service.NewWebhookService(integrationRepo, credRepo, taskRepo, idempotencyStore, cipher, queueClient)
	taskService := service.NewTaskService(taskRepo, attemptRepo, queueClient, hub)
	logService := service.NewLogService(logRepo, hub)

	// WebSocket handler
	wsHandler := api.NewWSHandler(hub, api.WithWSServices(taskService, logService))

	// Fiber Server
	server := api.NewServer(
		cfg,
		sessionManager,
		authService,
		credService,
		webhookService,
		api.WithTaskService(taskService),
		api.WithTeamRepository(teamRepo),
		api.WithProjectRepository(projectRepo),
		api.WithWSHandler(wsHandler),
	)

	// Server start and graceful shutdown
	serverErrCh := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Port)
		log.Printf("Listening and serving on %s", addr)
		if err := server.App.Listen(addr); err != nil {
			serverErrCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("Received shutdown signal: %v. Initiating graceful shutdown...", sig)
	case err := <-serverErrCh:
		if !errors.Is(err, os.ErrClosed) {
			log.Fatalf("Server listen error: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.App.ShutdownWithContext(shutdownCtx); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}

	log.Println("Server gracefully stopped")
}
