package api

import (
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/models"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// Server encapsulates the Fiber application and dependencies.
type Server struct {
	App            *fiber.App
	Config         *config.Config
	DB             *gorm.DB
	SessionManager *auth.SessionManager
	Authenticator  auth.Authenticator
	Cipher         *crypto.SecretCipher
}

// NewServer creates and configures a Fiber v3 app.
func NewServer(cfg *config.Config, db *gorm.DB, authenticator auth.Authenticator) *Server {
	app := fiber.New(fiber.Config{
		StructValidator: NewStructValidator(),
	})

	sessionSecret := []byte(cfg.SessionSecret)
	if len(sessionSecret) == 0 {
		sessionSecret = []byte("default-development-session-secret-key-32b!!")
	}

	secureCookies := cfg.AppEnv == "production"
	sessionManager := auth.NewSessionManager(sessionSecret, 24*60*60*1e9, secureCookies) // 24h

	var cipher *crypto.SecretCipher
	if len(cfg.SecretEncryptionKey) == 32 {
		cipher, _ = crypto.NewSecretCipher([]byte(cfg.SecretEncryptionKey))
	}

	server := &Server{
		App:            app,
		Config:         cfg,
		DB:             db,
		SessionManager: sessionManager,
		Authenticator:  authenticator,
		Cipher:         cipher,
	}

	server.setupRoutes()

	return server
}

func (s *Server) setupRoutes() {
	authHandlers := NewAuthHandlers(s.DB, s.SessionManager, s.Authenticator, s.Config.AppEnv == "production")
	credHandlers := NewCredentialHandlers(s.DB, s.Cipher)

	v1 := s.App.Group("/api/v1")
	authGroup := v1.Group("/auth")

	authGroup.Get("/login", authHandlers.Login)
	authGroup.Get("/callback", authHandlers.Callback)
	authGroup.Post("/logout", authHandlers.Logout)
	authGroup.Get("/me", authHandlers.Me)

	// Team credential routes protected by RequireAuth and RequireTeamRole(owner)
	teamsGroup := v1.Group("/teams/:team_id", RequireAuth(s.DB, s.SessionManager))
	credentialsGroup := teamsGroup.Group("/credentials", RequireTeamRole(s.DB, models.RoleOwner))
	credentialsGroup.Get("", credHandlers.List)
	credentialsGroup.Post("", credHandlers.Create)
	credentialsGroup.Delete("/:credential_id", credHandlers.Delete)
}
