package api

import (
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/models"
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
}

// NewServer creates and configures a Fiber v3 app.
func NewServer(
	cfg *config.Config,
	sessionManager *auth.SessionManager,
	authService service.AuthService,
	credService service.CredentialService,
	webhookService service.WebhookService,
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

	server.setupRoutes()

	return server
}

func (s *Server) setupRoutes() {
	authHandlers := NewAuthHandlers(s.AuthService, s.SessionManager, s.Config.AppEnv == "production")
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

	// Team credential routes protected by RequireAuth and RequireTeamRole(owner)
	teamsGroup := v1.Group("/teams/:team_id", RequireAuth(s.AuthService, s.SessionManager))
	credentialsGroup := teamsGroup.Group("/credentials", RequireTeamRole(s.AuthService, models.RoleOwner))
	credentialsGroup.Get("", credHandlers.List)
	credentialsGroup.Post("", credHandlers.Create)
	credentialsGroup.Delete("/:credential_id", credHandlers.Delete)
}
