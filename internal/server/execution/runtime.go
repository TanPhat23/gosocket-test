// Package execution assembles the agent runner and the project sandboxes it runs tasks in.
package execution

import (
	"context"
	"log"
	"os"

	dockerclient "github.com/docker/docker/client"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/devruntime"
	"agent-task-manager/internal/gateway"
	"agent-task-manager/internal/git"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/sandbox"
	"agent-task-manager/internal/server/persistence"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
)

// TaskRuntime is the execution side of the control plane: the agent runner and the project sandboxes it runs in.
type TaskRuntime struct {
	Runner *runner.AgentTaskRunner
	// Sandboxes manages the project containers. It is nil when no project runtime is configured.
	Sandboxes runner.ProjectSandboxManager
}

// BuildTaskRuntime assembles the runner from the environment. Docker, the git transfer adapter, and the project runtime
// are each optional and enabled by their own flags, so a missing piece leaves that feature off rather than failing.
func BuildTaskRuntime(
	cfg *config.Config,
	repos persistence.Repositories,
	cipher *crypto.SecretCipher,
	ghProvider *github.Provider,
	gatewaySvc *gateway.Service,
	hub *ws.Hub,
) (*TaskRuntime, error) {
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

	// The git transfer adapter is built only when ATM_GIT_TRANSFER_ENABLED=true.
	repoTransfer, err := runner.TaskRepoTransferFromEnv(os.Getenv, dockerCli)
	if err != nil {
		return nil, err
	}
	if repoTransfer != nil {
		log.Printf("git transfer adapter enabled (image %s)", os.Getenv("ATM_GIT_TRANSFER_IMAGE"))
	}

	// The project runtime (sandbox manager with gateway tokens and firewall proof, image builder with the gateway CA)
	// is assembled only when ATM_PROJECT_RUNTIME_ENABLED=true.
	runtimeCatalog := runner.NewDefaultRuntimeCatalog()
	projectRuntime, err := runner.ProjectRuntimeFromEnv(os.Getenv, os.ReadFile, dockerCli, runtimeCatalog, gatewaySvc)
	if err != nil {
		return nil, err
	}

	runnerOpts := []runner.RunnerOption{
		runner.WithRunnerGitHubIntegration(repos.Integration, repos.Cred, cipher, ghProvider),
		runner.WithRunnerTaskRepoTransfer(repoTransfer),
	}
	var sandboxes runner.ProjectSandboxManager
	switch {
	case projectRuntime != nil:
		sandboxes = projectRuntime.Manager
		runnerOpts = append(runnerOpts,
			runner.WithRunnerProjectSandboxManager(projectRuntime.Manager),
			runner.WithRunnerRuntimeImage(runner.NewRuntimeResolver(runtimeCatalog), projectRuntime.Builder),
		)
		log.Printf("project runtime configured (gateway CA baked into images, firewall proof before sandbox start)")
	case cfg.DevHostAuth && dockerCli != nil:
		hostAuthSandboxMgr := runner.NewProjectSandboxManagerWithDevHostAuth(
			dockerCli,
			runner.DevHostAuthOptions{
				Enabled:     true,
				OpenCodeBin: cfg.OpenCodeBinPath,
				ConfigDir:   cfg.OpenCodeConfigDir,
				DataDir:     cfg.OpenCodeDataDir,
			},
		)
		sandboxes = hostAuthSandboxMgr
		builder := runner.NewRuntimeImageBuilder(dockerCli, runtimeCatalog)
		runnerOpts = append(runnerOpts,
			runner.WithRunnerProjectSandboxManager(hostAuthSandboxMgr),
			runner.WithRunnerRuntimeImage(runner.NewRuntimeResolver(runtimeCatalog), builder),
		)
		log.Printf("dev host-auth project runtime configured (host OpenCode authentication mounted into container sandboxes)")
	}

	agentRunner := runner.NewAgentTaskRunner(
		cfg,
		repos.Project,
		repos.Task,
		repos.Log,
		worktreeMgr,
		dockerRunner,
		dockerCli,
		hub,
		runnerOpts...,
	)
	return &TaskRuntime{Runner: agentRunner, Sandboxes: sandboxes}, nil
}

// FakeRuntimeEnabled reports whether tasks run on the simulated runtime. Host auth development turns it off.
func FakeRuntimeEnabled(cfg *config.Config) (bool, error) {
	fakeRuntime, err := devruntime.EnabledFromEnv(os.Getenv)
	if err != nil {
		return false, err
	}
	if cfg.DevHostAuth && os.Getenv("ATM_DEV_FAKE_RUNTIME") == "" {
		fakeRuntime = false
	}
	return fakeRuntime, nil
}

// NewSessionClientResolver returns the OpenCode client for a project's running sandbox. It reports the runtime as
// unavailable when no sandbox manager is configured, and the service as unavailable when the project has no container.
func NewSessionClientResolver(sandboxes runner.ProjectSandboxManager) service.SessionClientResolver {
	return func(ctx context.Context, projectID string) (runner.OpenCodeClient, error) {
		if sandboxes == nil {
			return nil, runner.ErrRuntimeExecutionUnavailable
		}
		route, ok, err := sandboxes.GetProjectRoute(ctx, projectID)
		if err != nil {
			return nil, err
		}
		if !ok || route == nil {
			return nil, common.ErrServiceUnavailable
		}
		return runner.NewProjectOpenCodeClient(route.EndpointURL, route.AuthPassword, nil)
	}
}
