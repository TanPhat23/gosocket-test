package persistence

import (
	"gorm.io/gorm"

	"agent-task-manager/internal/repository"
)

// Repositories groups the persistence layer the control plane reads and writes through.
type Repositories struct {
	User        repository.UserRepository
	Token       repository.TokenRepository
	Team        repository.TeamRepository
	Project     repository.ProjectRepository
	Cred        repository.CredentialRepository
	Task        repository.TaskRepository
	Attempt     repository.AttemptRepository
	Log         repository.LogRepository
	Integration repository.IntegrationRepository
}

// NewRepositories builds every repository on the same database.
func NewRepositories(gormDB *gorm.DB) Repositories {
	return Repositories{
		User:        repository.NewUserRepository(gormDB),
		Token:       repository.NewTokenRepository(gormDB),
		Team:        repository.NewTeamRepository(gormDB),
		Project:     repository.NewProjectRepository(gormDB),
		Cred:        repository.NewCredentialRepository(gormDB),
		Task:        repository.NewTaskRepository(gormDB),
		Attempt:     repository.NewAttemptRepository(gormDB),
		Log:         repository.NewLogRepository(gormDB),
		Integration: repository.NewIntegrationRepository(gormDB),
	}
}
