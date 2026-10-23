// Command server runs the agent task manager control plane. This file only composes the packages under internal/server
// and the services they feed; each responsibility lives in its own package.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/devruntime"
	"agent-task-manager/internal/gateway"
	"agent-task-manager/internal/integrations"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/queue"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/server/execution"
	"agent-task-manager/internal/server/lifecycle"
	"agent-task-manager/internal/server/persistence"
	"agent-task-manager/internal/server/security"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run wires the control plane, starts its background work and HTTP server, and blocks until a shutdown signal. The
// defers release what was opened, in reverse order, on every return path.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	if err := config.ValidateRawProductionConfig(cfg.ToRaw()); err != nil {
		return fmt.Errorf("production preflight validation failed: %w", err)
	}
	fmt.Printf("Starting Agent Task Manager Control Plane on port %d (env: %s)\n", cfg.Port, cfg.AppEnv)

	gormDB, err := persistence.Open(cfg)
	if err != nil {
		return err
	}

	queueClient, err := queue.NewClient(cfg, gormDB)
	if err != nil {
		return fmt.Errorf("failed to initialize queue client: %w", err)
	}
	defer func() {
		_ = queueClient.Close()
	}()

	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	repos := persistence.NewRepositories(gormDB)
	cipher, err := security.NewSecretCipher(cfg)
	if err != nil {
		return err
	}

	authService := service.NewAuthService(repos.User, repos.Team, repos.Token, security.NewAuthenticator(cfg))
	sessionManager := auth.NewSessionManager([]byte(cfg.SessionSecret), 30*24*time.Hour, cfg.AppEnv == "production")
	credService := service.NewCredentialService(repos.Cred, cipher)

	ghProvider := github.NewProvider()
	webhookService := service.NewWebhookService(
		repos.Integration, repos.Cred, repos.Task, integrations.NewDBIdempotencyStore(gormDB), cipher, queueClient,
		service.WithWebhookHub(hub),
		service.WithWebhookSyncProvider(ghProvider),
	)

	fakeRuntime, err := execution.FakeRuntimeEnabled(cfg)
	if err != nil {
		return err
	}

	// The credential gateway is off unless ATM_GATEWAY_ENABLED=true. It only serves; no sandbox receives a token until
	// the project sandbox manager is built with a broker.
	gatewaySvc, err := gateway.ServiceFromEnv(os.Getenv, os.ReadFile, log.Printf)
	if err != nil {
		return err
	}
	if gatewaySvc != nil {
		stopGateway, err := lifecycle.StartGateway(gatewaySvc)
		if err != nil {
			return err
		}
		defer stopGateway()
	}

	// The task runtime does not depend on the task service, so it is built first and the service's hooks use it directly.
	taskRt, err := execution.BuildTaskRuntime(cfg, repos, cipher, ghProvider, gatewaySvc, hub)
	if err != nil {
		return err
	}

	taskService := service.NewTaskService(
		repos.Task, repos.Attempt, queueClient, hub,
		service.WithTaskGitHubIntegration(repos.Integration, repos.Cred, cipher, ghProvider),
		service.WithTaskSessionDependencies(runner.NewOpenCodeClient(cfg.OpenCodeServerURL, cfg.OpenCodePassword, nil), repos.Project, repos.Team),
		service.WithTaskLogRepository(repos.Log),
		service.WithTaskSimulatedRuntime(fakeRuntime),
		// The session tunnel talks only to the task's own project sandbox.
		service.WithTaskSessionClientResolver(execution.NewSessionClientResolver(taskRt.Sandboxes)),
		service.WithTaskSteerFinalizer(func(ctx context.Context, task *models.Task, before *time.Time, logf func(string)) error {
			_, err := taskRt.Runner.FinalizeSteeredTurn(ctx, task, before, logf)
			return err
		}),
		service.WithTaskPullRequestPublisher(taskRt.Runner.PublishTaskPullRequest, taskRt.Runner.TaskDiff),
		service.WithTaskWorkspaceCleaner(taskRt.Runner.RemoveTaskWorkspace),
		service.WithTaskDependencyCleaner(taskRt.Runner.RemoveTaskDependencies),
		service.WithTaskFileBrowser(taskRt.Runner.TaskFiles, taskRt.Runner.TaskFile),
		// Opening a project starts its container in the background, so the first task does not pay for the start.
		service.WithTaskProjectRuntimeWarmer(taskRt.Runner.WarmProjectRuntime),
		service.WithTaskProjectRuntimeManager(taskRt.Runner),
	)
	logService := service.NewLogService(repos.Log, hub)
	integrationService := service.NewIntegrationService(repos.Integration, repos.Project, repos.Cred)

	var taskRunner queue.TaskRunner = taskRt.Runner
	if fakeRuntime {
		log.Printf("WARNING: ATM_DEV_FAKE_RUNTIME is on: tasks are simulated. Set it to false (docker-compose.runtime.yml) for real runs.")
		taskRunner = devruntime.NewRunner(repos.Log, hub, 700*time.Millisecond)
	}
	workerServer := queue.NewWorkerServer(cfg, taskService, taskRunner)
	if err := workerServer.Start(); err != nil {
		return fmt.Errorf("failed to start Asynq worker server: %w", err)
	}
	log.Printf("Asynq worker server started with %d workers", workerServer.Concurrency())
	defer workerServer.Shutdown()

	wsHandler := api.NewWSHandler(hub, api.WithWSServices(taskService, logService))

	reconcileCtx, cancelReconcile := context.WithCancel(context.Background())
	defer cancelReconcile()
	lifecycle.StartReconciler(reconcileCtx, taskService)

	server := api.NewServer(
		cfg,
		sessionManager,
		authService,
		credService,
		webhookService,
		api.WithTaskService(taskService),
		api.WithIntegrationService(integrationService),
		api.WithTeamRepository(repos.Team),
		api.WithProjectRepository(repos.Project),
		api.WithWSHandler(wsHandler),
	)

	return lifecycle.ServeUntilSignal(server.App, cfg.Port)
}
