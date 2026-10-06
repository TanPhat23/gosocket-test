package api

import (
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

// Server encapsulates the Fiber application and dependencies.
type Server struct {
	App               *fiber.App
	Config            *config.Config
	SessionManager    *auth.SessionManager
	AuthService       service.AuthService
	CredentialService service.CredentialService
	WebhookService    service.WebhookService
	TaskService       service.TaskService
	TeamRepo          repository.TeamRepository
	ProjectRepo       repository.ProjectRepository
	WSHandler         *WSHandler
}

// ServerOptions allows optional configuration or dependencies when constructing Server.
type ServerOptions struct {
	TaskService service.TaskService
	TeamRepo    repository.TeamRepository
	ProjectRepo repository.ProjectRepository
	WSHandler   *WSHandler
}

// Option configures Server.
type Option func(*Server)

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

// WithWSHandler sets the WSHandler.
func WithWSHandler(wsh *WSHandler) Option {
	return func(s *Server) {
		s.WSHandler = wsh
	}
}

// NewServer creates and configures a Fiber v3 app.
func NewServer(
	cfg *config.Config,
	sessionManager *auth.SessionManager,
	authService service.AuthService,
	credService service.CredentialService,
	webhookService service.WebhookService,
	opts ...Option,
) *Server {
	app := fiber.New(fiber.Config{
		StructValidator: NewStructValidator(),
	})

	server := &Server{
		App:               app,
		Config:            cfg,
		SessionManager:    sessionManager,
		AuthService:       authService,
		CredentialService: credService,
		WebhookService:    webhookService,
	}

	for _, opt := range opts {
		opt(server)
	}

	server.setupRoutes()

	return server
}

func (s *Server) setupRoutes() {
	authHandlers := NewAuthHandlers(s.AuthService, s.SessionManager, s.Config != nil && s.Config.AppEnv == "production")
	credHandlers := NewCredentialHandlers(s.CredentialService)
	webhookHandlers := NewWebhookHandlers(s.WebhookService)

	v1 := s.App.Group("/api/v1")
	authGroup := v1.Group("/auth")

	authGroup.Get("/login", authHandlers.Login)
	authGroup.Get("/callback", authHandlers.Callback)
	authGroup.Post("/logout", authHandlers.Logout)
	authGroup.Get("/me", authHandlers.Me)

	// Webhook endpoint
	v1.Post("/webhooks/:provider", webhookHandlers.HandleWebhook)

	// Model discovery endpoint
	modelHandlers := NewModelHandlers(s.Config)
	v1.Get("/models", modelHandlers.ListModels)

	// Task routes
	if s.TaskService != nil {
		taskHandlers := NewTaskHandlers(s.TaskService)
		v1.Get("/tasks", taskHandlers.List)
		v1.Get("/tasks/:task_id", taskHandlers.Get)
		v1.Post("/tasks", taskHandlers.Create)
		v1.Post("/tasks/:task_id/cancel", taskHandlers.Cancel)
	}

	// Team & Project routes
	if s.TeamRepo != nil || s.ProjectRepo != nil {
		teamHandlers := NewTeamHandlers(s.TeamRepo, s.ProjectRepo)
		if s.TeamRepo != nil {
			v1.Get("/teams", teamHandlers.ListTeams)
		}
		if s.ProjectRepo != nil {
			v1.Get("/projects", teamHandlers.ListProjects)
		}
	}

	// WebSocket terminal logs route
	if s.WSHandler != nil {
		v1.Get("/ws/terminal/:task_id", s.WSHandler.HandleTerminalWS)
	}

	// Team credential routes protected by RequireAuth and RequireTeamRole(owner)
	teamsGroup := v1.Group("/teams/:team_id", RequireAuth(s.AuthService, s.SessionManager))
	credentialsGroup := teamsGroup.Group("/credentials", RequireTeamRole(s.AuthService, models.RoleOwner))
	credentialsGroup.Get("", credHandlers.List)
	credentialsGroup.Post("", credHandlers.Create)
	credentialsGroup.Delete("/:credential_id", credHandlers.Delete)
}
