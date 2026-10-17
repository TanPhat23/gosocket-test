package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/devruntime"
	"agent-task-manager/internal/db"
	"agent-task-manager/internal/gateway"
	"agent-task-manager/internal/git"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/integrations/jira"
	"agent-task-manager/internal/integrations/trello"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/sandbox"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"

	dockerclient "github.com/docker/docker/client"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	if err := config.ValidateRawProductionConfig(cfg.ToRaw()); err != nil {
		log.Fatalf("production preflight validation failed: %v", err)
	}

	fmt.Printf("Starting Agent Task Manager Control Plane on port %d (env: %s)\n", cfg.Port, cfg.AppEnv)

	// Database connection and auto-migration
	gormDB, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// Auto-migrate schema on launch
	_ = gormDB.AutoMigrate(
		&models.Team{},
		&models.User{},
		&models.TeamMembership{},
		&models.APIToken{},
		&models.Project{},
		&models.ProjectIntegration{},
		&models.Credential{},
		&models.Task{},
		&models.TaskAttempt{},
		&models.WebhookEvent{},
		&models.TaskLog{},
	)

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
				RepoPath: filepath.Join(cfg.HostRepoRoot, "core"),
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
	masterKey, err := config.NormalizeEncryptionKey(cfg.SecretEncryptionKey)
	if err != nil {
		log.Fatalf("failed to initialize encryption key: %v", err)
	}
	cipher, err := crypto.NewGCMCipher(masterKey.RawBytes[:])
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
	ghProvider := github.NewProvider()
	webhookService := service.NewWebhookService(
		integrationRepo, credRepo, taskRepo, idempotencyStore, cipher, queueClient,
		service.WithWebhookHub(hub),
		service.WithWebhookSyncProvider(ghProvider),
	)
	fakeRuntime, err := devruntime.EnabledFromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if cfg.DevHostAuth && os.Getenv("ATM_DEV_FAKE_RUNTIME") == "" {
		fakeRuntime = false
	}
	// Gate 0G: the credential gateway is off unless ATM_GATEWAY_ENABLED=true. It only serves; no sandbox
	// receives a token until the project sandbox manager is built with a broker.
	gatewaySvc, err := gateway.ServiceFromEnv(os.Getenv, os.ReadFile, log.Printf)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if gatewaySvc != nil {
		gwLn, err := gatewaySvc.Listen()
		if err != nil {
			log.Fatalf("gateway: listen: %v", err)
		}
		gwCtx, gwCancel := context.WithCancel(context.Background())
		defer gwCancel()
		go func() {
			if err := gatewaySvc.Run(gwCtx, gwLn); err != nil {
				log.Printf("gateway: stopped: %v", err)
			}
		}()
		log.Printf("LLM gateway listening on %s", gwLn.Addr())
	}
	openCodeClient := runner.NewOpenCodeClient(cfg.OpenCodeServerURL, cfg.OpenCodePassword, nil)
	// The session tunnel talks only to the project's own sandbox. The manager is assigned once the project
	// runtime is built below, before the HTTP server starts; with no runtime the tunnel reports unavailable.
	var sessionSandboxMgr runner.ProjectSandboxManager
	var steerRunner *runner.AgentTaskRunner // bound after the runner is built
	sessionClients := func(ctx context.Context, projectID string) (runner.OpenCodeClient, error) {
		if sessionSandboxMgr == nil {
			return nil, runner.ErrRuntimeExecutionUnavailable
		}
		route, ok, err := sessionSandboxMgr.GetProjectRoute(ctx, projectID)
		if err != nil {
			return nil, err
		}
		if !ok || route == nil {
			return nil, common.ErrServiceUnavailable
		}
		return runner.NewProjectOpenCodeClient(route.EndpointURL, route.AuthPassword, nil)
	}
	taskService := service.NewTaskService(
		taskRepo,
		attemptRepo,
		queueClient,
		hub,
		service.WithTaskGitHubIntegration(integrationRepo, credRepo, cipher, ghProvider),
		service.WithTaskSessionDependencies(openCodeClient, projectRepo, teamRepo),
		service.WithTaskLogRepository(logRepo),
		service.WithTaskSimulatedRuntime(fakeRuntime),
		service.WithTaskSessionClientResolver(sessionClients),
		service.WithTaskSteerFinalizer(func(ctx context.Context, task *models.Task, before *time.Time, logf func(string)) error {
			if steerRunner == nil {
				return errors.New("agent runner is not initialized")
			}
			_, err := steerRunner.FinalizeSteeredTurn(ctx, task, before, logf)
			return err
		}),
	)
	logService := service.NewLogService(logRepo, hub)
	integrationService := service.NewIntegrationService(integrationRepo, projectRepo, credRepo)

	// Worktree Manager & Sandbox Docker Runner
	worktreeMgr := git.NewWorktreeManager(cfg.HostWorktreeRoot)
	var dockerCli sandbox.DockerClient
	var dockerRunner *sandbox.Runner
	if dCli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation()); err == nil {
		dockerCli = dCli
		dockerRunner = sandbox.NewRunner(dCli)
		log.Println("Docker client initialized successfully for containerized agent execution")
	} else {
		log.Printf("warning: Docker client initialization failed: %v", err)
	}

	// Gate 0C: the git transfer adapter is built only when ATM_GIT_TRANSFER_ENABLED=true. The runner holds
	// it and RunTask uses it (quarantine lifted 2026-10-16).
	repoTransfer, err := runner.TaskRepoTransferFromEnv(os.Getenv, dockerCli)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if repoTransfer != nil {
		log.Printf("git transfer adapter enabled (image %s)", os.Getenv("ATM_GIT_TRANSFER_IMAGE"))
	}

	// Gates 0D/0G: the project runtime (sandbox manager with gateway tokens and firewall proof, image builder
	// with the gateway CA) is assembled only when ATM_PROJECT_RUNTIME_ENABLED=true and is used by RunTask.
	runtimeCatalog := runner.NewDefaultRuntimeCatalog()
	projectRuntime, err := runner.ProjectRuntimeFromEnv(os.Getenv, os.ReadFile, dockerCli, runtimeCatalog, gatewaySvc)
	if err != nil {
		log.Fatalf("%v", err)
	}
	runnerOpts := []runner.RunnerOption{
		runner.WithRunnerGitHubIntegration(integrationRepo, credRepo, cipher, ghProvider),
		runner.WithRunnerTaskRepoTransfer(repoTransfer),
	}
	if projectRuntime != nil {
		sessionSandboxMgr = projectRuntime.Manager
		runnerOpts = append(runnerOpts,
			runner.WithRunnerProjectSandboxManager(projectRuntime.Manager),
			runner.WithRunnerRuntimeImage(runner.NewRuntimeResolver(runtimeCatalog), projectRuntime.Builder),
		)
		log.Printf("project runtime configured (gateway CA baked into images, firewall proof before sandbox start)")
	} else if cfg.DevHostAuth && dockerCli != nil {
		hostAuthSandboxMgr := runner.NewProjectSandboxManagerWithDevHostAuth(
			dockerCli,
			runner.DevHostAuthOptions{
				Enabled:     true,
				OpenCodeBin: cfg.OpenCodeBinPath,
				ConfigDir:   cfg.OpenCodeConfigDir,
				DataDir:     cfg.OpenCodeDataDir,
			},
		)
		sessionSandboxMgr = hostAuthSandboxMgr
		builder := runner.NewRuntimeImageBuilder(dockerCli, runtimeCatalog)
		runnerOpts = append(runnerOpts,
			runner.WithRunnerProjectSandboxManager(hostAuthSandboxMgr),
			runner.WithRunnerRuntimeImage(runner.NewRuntimeResolver(runtimeCatalog), builder),
		)
		log.Printf("dev host-auth project runtime configured (host OpenCode authentication mounted into container sandboxes)")
	}

	// Concrete Agent Task Runner (orchestrating worktrees, OpenCode, and diff snapshots)
	agentRunner := runner.NewAgentTaskRunner(
		cfg,
		projectRepo,
		taskRepo,
		logRepo,
		worktreeMgr,
		dockerRunner,
		dockerCli,
		hub,
		runnerOpts...,
	)
	steerRunner = agentRunner

	// Concurrency-bounded Asynq Worker Server
	var taskRunner queue.TaskRunner = agentRunner
	if fakeRuntime {
		log.Printf("WARNING: ATM_DEV_FAKE_RUNTIME is on: tasks are simulated. Set it to false (docker-compose.runtime.yml) for real runs.")
		taskRunner = devruntime.NewRunner(logRepo, hub, 700*time.Millisecond)
	}
	workerServer := queue.NewWorkerServer(cfg, taskService, taskRunner)
	if err := workerServer.Start(); err != nil {
		log.Fatalf("failed to start Asynq worker server: %v", err)
	}
	log.Printf("Asynq worker server started with %d workers", workerServer.Concurrency())
	defer workerServer.Shutdown()

	// WebSocket handler
	wsHandler := api.NewWSHandler(hub, api.WithWSServices(taskService, logService))

	// Background PR status reconciler (checks tasks in review every 30 seconds)
	prReconcileCtx, prReconcileCancel := context.WithCancel(context.Background())
	defer prReconcileCancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-prReconcileCtx.Done():
				return
			case <-ticker.C:
				_ = taskService.ReconcileActivePRs(prReconcileCtx)
			}
		}
	}()

	// Fiber Server
	server := api.NewServer(
		cfg,
		sessionManager,
		authService,
		credService,
		webhookService,
		api.WithTaskService(taskService),
		api.WithIntegrationService(integrationService),
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
