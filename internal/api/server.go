package api

import (
	"os"
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

// Server encapsulates the Fiber application and dependencies.
type Server struct {
	App                *fiber.App
	Config             *config.Config
	Security           config.SecurityConfig
	SessionManager     *auth.SessionManager
	AuthService        service.AuthService
	CredentialService  service.CredentialService
	WebhookService     service.WebhookService
	TaskService        service.TaskService
	IntegrationService service.IntegrationService
	TeamRepo           repository.TeamRepository
	ProjectRepo        repository.ProjectRepository
	WSHandler          *WSHandler
	OpenCodeClient     runner.OpenCodeClient
}

// ServerOptions allows optional configuration or dependencies when constructing Server.
type ServerOptions struct {
	TaskService        service.TaskService
	IntegrationService service.IntegrationService
	TeamRepo           repository.TeamRepository
	ProjectRepo        repository.ProjectRepository
	WSHandler          *WSHandler
}

// Option configures Server.
type Option func(*Server)

// WithIntegrationService sets the IntegrationService.
func WithIntegrationService(is service.IntegrationService) Option {
	return func(s *Server) {
		s.IntegrationService = is
	}
}

// WithTaskService sets the TaskService.
func WithTaskService(ts service.TaskService) Option {
	return func(s *Server) {
		s.TaskService = ts
	}
}

// WithTeamRepository sets the TeamRepository.
func WithTeamRepository(tr repository.TeamRepository) Option {
	return func(s *Server) {
		s.TeamRepo = tr
	}
}

// WithProjectRepository sets the ProjectRepository.
func WithProjectRepository(pr repository.ProjectRepository) Option {
	return func(s *Server) {
		s.ProjectRepo = pr
	}
}

// WithWSHandler sets the WSHandler. NewServer supplies its authentication services (for re-validating
// open sockets) and the configured origin allowlist.
func WithWSHandler(wsh *WSHandler) Option {
	return func(s *Server) {
		s.WSHandler = wsh
	}
}

// WithOpenCodeClient sets the OpenCodeClient.
func WithOpenCodeClient(client runner.OpenCodeClient) Option {
	return func(s *Server) {
		s.OpenCodeClient = client
	}
}

// NewServer creates and configures a Fiber v3 app.
//
// It loads config.SecurityConfig from the environment and panics if it is invalid (a malformed
// ATM_ALLOWED_ORIGINS entry, or ATM_ENABLE_LOCAL_LOGIN=true in production). NewServer runs once at
// startup and cannot return an error without changing its callers; refusing to start is the
// fail-closed choice, whereas silently falling back would hide the misconfiguration.
func NewServer(
	cfg *config.Config,
	sessionManager *auth.SessionManager,
	authService service.AuthService,
	credService service.CredentialService,
	webhookService service.WebhookService,
	opts ...Option,
) *Server {
	appEnv := ""
	if cfg != nil {
		appEnv = cfg.AppEnv
	}
	security, err := config.LoadSecurityConfig(appEnv)
	if err != nil {
		panic("api: invalid security configuration: " + err.Error())
	}

	app := fiber.New(fiber.Config{
		StructValidator: NewStructValidator(),
		ErrorHandler:    CentralErrorHandler,
	})

	server := &Server{
		App:               app,
		Config:            cfg,
		Security:          security,
		SessionManager:    sessionManager,
		AuthService:       authService,
		CredentialService: credService,
		WebhookService:    webhookService,
	}

	for _, opt := range opts {
		opt(server)
	}

	if server.WSHandler != nil {
		server.WSHandler.authService = authService
		server.WSHandler.sessions = sessionManager
		server.WSHandler.allowedOrigins = security.AllowedOrigins
	}

	if server.OpenCodeClient == nil && cfg != nil {
		server.OpenCodeClient = runner.NewOpenCodeClient(cfg.OpenCodeServerURL, cfg.OpenCodePassword, nil)
	}

	server.setupRoutes()

	return server
}

func (s *Server) setupRoutes() {
	authHandlers := NewAuthHandlers(s.AuthService, s.SessionManager, s.Config != nil && s.Config.AppEnv == "production", s.Security.EnableLocalLogin)
	credHandlers := NewCredentialHandlers(s.CredentialService)
	webhookHandlers := NewWebhookHandlers(s.WebhookService)

	// Cookie-authenticated mutations must come from a trusted origin; login and logout always must.
	s.App.Use(CSRFProtection(s.Security.AllowedOrigins))
	trustedOrigin := RequireTrustedOrigin(s.Security.AllowedOrigins)

	v1 := s.App.Group("/api/v1")
	authGroup := v1.Group("/auth")

	authGroup.Get("/login", authHandlers.Login)
	authGroup.Post("/login", trustedOrigin, authHandlers.LoginWithCredentials)
	authGroup.Get("/callback", authHandlers.Callback)
	authGroup.Post("/logout", trustedOrigin, authHandlers.Logout)
	authGroup.Get("/me", authHandlers.Me)

	// Webhook endpoint
	v1.Post("/webhooks/:provider", webhookHandlers.HandleWebhook)

	// Model discovery endpoint
	modelHandlers := NewModelHandlers(s.Config)
	v1.Get("/models", RequireAuth(s.AuthService, s.SessionManager), modelHandlers.ListModels)

	// Operator-only site settings (gateway provider key). Writes need a trusted same-origin session.
	operatorHandlers := NewOperatorHandlers(os.Getenv("ATM_GATEWAY_PROVIDER_KEY_FILE"), s.Security.OperatorEmails)
	v1.Get("/operator/provider-key", RequireAuth(s.AuthService, s.SessionManager), operatorHandlers.GetProviderKeyStatus)
	v1.Put("/operator/provider-key", trustedOrigin, RequireAuth(s.AuthService, s.SessionManager), operatorHandlers.SetProviderKey)

	// Task routes
	if s.TaskService != nil {
		taskHandlers := NewTaskHandlers(s.TaskService)
		v1.Get("/tasks", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.List)
		v1.Get("/tasks/:task_id", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.Get)
		v1.Post("/tasks", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.Create)
		v1.Post("/tasks/:task_id/cancel", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.Cancel)
		v1.Post("/tasks/:id/cancel", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.Cancel)
		v1.Post("/tasks/:id/pr", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.CreatePR)
		v1.Post("/tasks/:id/sync-pr", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.SyncPR)
		v1.Post("/tasks/:id/pr/sync", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.SyncPR)
		v1.Get("/tasks/:id/diff", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.GetDiff)
		v1.Get("/tasks/:id/files", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.ListFiles)
		v1.Get("/tasks/:id/files/content", RequireAuth(s.AuthService, s.SessionManager), taskHandlers.ReadFile)

		// OpenCode session endpoints (AC-SEC-01: Protected by RequireAuth)
		sessionGroup := v1.Group("/tasks/:id/session", RequireAuth(s.AuthService, s.SessionManager))
		sessionGroup.Get("", taskHandlers.GetSession)
		sessionGroup.Post("/prompt", taskHandlers.PromptSession)
		sessionGroup.Post("/compact", taskHandlers.CompactSession)
		sessionGroup.Post("/interrupt", taskHandlers.InterruptSession)
	}

	// Team & Project routes
	if s.TeamRepo != nil || s.ProjectRepo != nil {
		// Listing is filtered to the caller's teams; project creation requires RoleAdmin on the target
		// team (checked in the handler because team_id is in the body); deletion requires RoleAdmin on
		// the project's team.
		teamHandlers := NewTeamHandlers(s.TeamRepo, s.ProjectRepo, s.AuthService)
		if s.TeamRepo != nil {
			v1.Get("/teams", RequireAuth(s.AuthService, s.SessionManager), teamHandlers.ListTeams)
		}
		if s.ProjectRepo != nil {
			v1.Get("/projects", RequireAuth(s.AuthService, s.SessionManager), teamHandlers.ListProjects)
			v1.Post("/projects", RequireAuth(s.AuthService, s.SessionManager), teamHandlers.CreateProject)
			v1.Delete("/projects/:id", RequireAuth(s.AuthService, s.SessionManager),
				RequireProjectRole(s.AuthService, s.ProjectRepo, "id", models.RoleAdmin), teamHandlers.DeleteProject)
		}
	}

	// WebSocket routes: live events & terminal logs. Browsers send the atm_session cookie on the
	// handshake; the handlers additionally enforce same-origin and per-task authorization.
	if s.WSHandler != nil {
		v1.Get("/ws/events", RequireAuth(s.AuthService, s.SessionManager), s.WSHandler.HandleEventsWS)
		v1.Get("/ws/terminal/:task_id", RequireAuth(s.AuthService, s.SessionManager), s.WSHandler.HandleTerminalWS)
		v1.Get("/ws/session/:task_id", RequireAuth(s.AuthService, s.SessionManager), s.WSHandler.HandleSessionWS)
	}

	// Project integrations routes: reads require RoleViewer, writes RoleAdmin on the project's team.
	// The service rejects integration IDs that do not belong to :project_id.
	if s.IntegrationService != nil {
		integHandlers := NewIntegrationHandlers(s.IntegrationService)
		integRead := RequireProjectRole(s.AuthService, s.ProjectRepo, "project_id", models.RoleViewer)
		integWrite := RequireProjectRole(s.AuthService, s.ProjectRepo, "project_id", models.RoleAdmin)
		projIntegGroup := v1.Group("/projects/:project_id/integrations", RequireAuth(s.AuthService, s.SessionManager))
		projIntegGroup.Get("", integRead, integHandlers.List)
		projIntegGroup.Post("", integWrite, integHandlers.Create)
		projIntegGroup.Get("/:id", integRead, integHandlers.Get)
		projIntegGroup.Patch("/:id", integWrite, integHandlers.Update)
		projIntegGroup.Delete("/:id", integWrite, integHandlers.Delete)
	}

	// Team credential routes protected by RequireAuth and RequireTeamRole(owner)
	teamsGroup := v1.Group("/teams/:team_id", RequireAuth(s.AuthService, s.SessionManager))
	credentialsGroup := teamsGroup.Group("/credentials", RequireTeamRole(s.AuthService, models.RoleOwner))
	credentialsGroup.Get("", credHandlers.List)
	credentialsGroup.Post("", credHandlers.Create)
	credentialsGroup.Put("/:credential_id", credHandlers.Update)
	credentialsGroup.Patch("/:credential_id", credHandlers.Update)
	credentialsGroup.Delete("/:credential_id", credHandlers.Delete)

	// GitHub repository and branch endpoints allow team viewers and above
	teamsGroup.Get("/credentials/:credential_id/github/repos", RequireTeamRole(s.AuthService, models.RoleViewer), credHandlers.ListGitHubRepositories)
	teamsGroup.Get("/credentials/:credential_id/github/branches", RequireTeamRole(s.AuthService, models.RoleViewer), credHandlers.ListGitHubBranches)
}
