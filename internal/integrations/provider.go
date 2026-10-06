package integrations

import (
	"context"

	"agent-task-manager/internal/models"
)

// ExternalIssue represents an issue retrieved from or sent to an external provider.
type ExternalIssue struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Status      string   `json:"status"`
	URL         string   `json:"url"`
	Labels      []string `json:"labels"`
}

// SyncProvider defines the interface for synchronizing issues with external systems.
type SyncProvider interface {
	Provider() models.IntegrationProvider
	FetchIssues(ctx context.Context, integration *models.ProjectIntegration, plainSecret string) ([]ExternalIssue, error)
	UpdateIssueStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, status string) error
	AddComment(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, externalID string, comment string) error
}

// PullRequestProvider defines the interface for creating pull requests in external systems.
type PullRequestProvider interface {
	CreatePullRequest(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, branch, title, body string) (string, error)
}
