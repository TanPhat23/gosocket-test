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

// PullRequestStatus represents the status of a pull request in an external system.
type PullRequestStatus struct {
	Number   int    `json:"number"`
	State    string `json:"state"` // "open" or "closed"
	Merged   bool   `json:"merged"`
	MergedAt string `json:"merged_at,omitempty"`
	HTMLURL  string `json:"html_url"`
}

// PullRequestProvider defines the interface for creating and inspecting pull requests in external systems.
type PullRequestProvider interface {
	CreatePullRequest(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, branch, title, body string) (string, error)
	GetPullRequestStatus(ctx context.Context, integration *models.ProjectIntegration, plainSecret string, prNumberOrURL string) (*PullRequestStatus, error)
}

// PullRequestStatusResult is the outcome of reading one pull request in a batch: Status on success, Err otherwise.
type PullRequestStatusResult struct {
	Status *PullRequestStatus
	Err    error
}

// PullRequestStatusBatcher is implemented by providers that can read several pull request statuses in one
// request. The result map is keyed by the URLs as they were passed in. A provider that does not implement it
// is read one pull request at a time.
type PullRequestStatusBatcher interface {
	GetPullRequestStatuses(ctx context.Context, plainSecret string, prURLs []string) map[string]PullRequestStatusResult
}
