package api

import (
	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/config"

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

	server := &Server{
		App:            app,
		Config:         cfg,
		DB:             db,
		SessionManager: sessionManager,
		Authenticator:  authenticator,
	}

	server.setupRoutes()

	return server
}

func (s *Server) setupRoutes() {
	authHandlers := NewAuthHandlers(s.DB, s.SessionManager, s.Authenticator, s.Config.AppEnv == "production")

	v1 := s.App.Group("/api/v1")
	authGroup := v1.Group("/auth")

	authGroup.Get("/login", authHandlers.Login)
	authGroup.Get("/callback", authHandlers.Callback)
	authGroup.Post("/logout", authHandlers.Logout)
	authGroup.Get("/me", authHandlers.Me)
}
