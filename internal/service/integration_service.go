package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
)

var (
	ErrInvalidRepoSlug      = errors.New("invalid repository slug (expected owner/repo)")
	ErrInvalidBaseBranch    = errors.New("invalid base branch")
	ErrInvalidWebhookCallbackURL = errors.New("invalid webhook callback url (expected absolute http(s) url without credentials)")
	ErrProjectNotFound      = errors.New("project not found")
	ErrUnauthorizedTenant   = errors.New("credential does not belong to project team")
)

// Security Fix 4: Strictly validate repo slug: ^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}/[a-zA-Z0-9_.-]{1,100}$ (rejecting .., leading/trailing slashes)
var repoSlugRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}/[a-zA-Z0-9_.-]{1,100}$`)

func validateRepoSlug(repo string) error {
	if strings.Contains(repo, "..") || !repoSlugRegex.MatchString(repo) {
		return ErrInvalidRepoSlug
	}
	return nil
}

// Validate base_branch: reject leading '-' or '..'
func validateBaseBranch(branch string) error {
	if branch == "" {
		return nil
	}
	if strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") || strings.ContainsAny(branch, " \t\n\r~^:?*[\x00-\x1f") {
		return ErrInvalidBaseBranch
	}
	return nil
}

// validateWebhookCallbackURL accepts an empty value (unset) or an absolute http(s) URL with a host
// and no embedded credentials.
func validateWebhookCallbackURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return ErrInvalidWebhookCallbackURL
	}
	return nil
}

type IntegrationService interface {
	CreateIntegration(ctx context.Context, projectID string, req dto.CreateIntegrationRequest) (*dto.IntegrationResponse, error)
	ListIntegrations(ctx context.Context, projectID string) ([]dto.IntegrationResponse, error)
	GetIntegration(ctx context.Context, projectID, id string) (*dto.IntegrationResponse, error)
	UpdateIntegration(ctx context.Context, projectID, id string, req dto.UpdateIntegrationRequest) (*dto.IntegrationResponse, error)
	DeleteIntegration(ctx context.Context, projectID, id string) error
}

type integrationService struct {
	integRepo repository.IntegrationRepository
	projRepo  repository.ProjectRepository
	credRepo  repository.CredentialRepository
}

func NewIntegrationService(
	integRepo repository.IntegrationRepository,
	projRepo repository.ProjectRepository,
	credRepo repository.CredentialRepository,
) IntegrationService {
	return &integrationService{
		integRepo: integRepo,
		projRepo:  projRepo,
		credRepo:  credRepo,
	}
}

func (s *integrationService) CreateIntegration(ctx context.Context, projectID string, req dto.CreateIntegrationRequest) (*dto.IntegrationResponse, error) {
	trimmedRepo := strings.TrimSpace(req.Repo)
	if err := validateRepoSlug(trimmedRepo); err != nil {
		return nil, err
	}

	if err := validateBaseBranch(req.BaseBranch); err != nil {
		return nil, err
	}
	if err := validateWebhookCallbackURL(req.WebhookCallbackURL); err != nil {
		return nil, err
	}

	proj, err := s.loadProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	if err := s.checkCredentialTenant(ctx, req.CredentialID, proj.TeamID); err != nil {
		return nil, err
	}

	baseBranch := "main"
	if req.BaseBranch != "" {
		baseBranch = req.BaseBranch
	}
	autoPR := true
	if req.AutoPR != nil {
		autoPR = *req.AutoPR
	}

	cfg := models.JSONMap{
		"base_branch": baseBranch,
		"auto_pr":     autoPR,
	}
	if req.WebhookSecret != "" {
		cfg["webhook_secret"] = req.WebhookSecret
	}
	if req.WebhookCallbackURL != "" {
		cfg["webhook_callback_url"] = req.WebhookCallbackURL
	}
	if req.FilterLabel != "" {
		cfg["filter_label"] = req.FilterLabel
	}

	extURL := fmt.Sprintf("https://github.com/%s", trimmedRepo)
	now := time.Now().UTC()
	integ := &models.ProjectIntegration{
		ID:           common.NewID(),
		ProjectID:    projectID,
		CredentialID: req.CredentialID,
		Provider:     models.IntegrationProvider(req.Provider),
		ExternalID:   trimmedRepo,
		ExternalURL:  &extURL,
		Enabled:      true,
		Config:       cfg,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.integRepo.Create(ctx, integ); err != nil {
		return nil, fmt.Errorf("failed to save integration: %w", err)
	}

	return s.toResponse(integ), nil
}

func (s *integrationService) ListIntegrations(ctx context.Context, projectID string) ([]dto.IntegrationResponse, error) {
	if _, err := s.loadProject(ctx, projectID); err != nil {
		return nil, err
	}

	list, err := s.integRepo.ListByProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res := make([]dto.IntegrationResponse, len(list))
	for i, item := range list {
		res[i] = *s.toResponse(&item)
	}
	return res, nil
}

func (s *integrationService) GetIntegration(ctx context.Context, projectID, id string) (*dto.IntegrationResponse, error) {
	integ, err := s.loadScopedIntegration(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	return s.toResponse(integ), nil
}

func (s *integrationService) UpdateIntegration(ctx context.Context, projectID, id string, req dto.UpdateIntegrationRequest) (*dto.IntegrationResponse, error) {
	proj, err := s.loadProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	integ, err := s.loadScopedIntegration(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	if req.WebhookCallbackURL != nil {
		if err := validateWebhookCallbackURL(*req.WebhookCallbackURL); err != nil {
			return nil, err
		}
	}

	if req.CredentialID != nil {
		if err := s.checkCredentialTenant(ctx, *req.CredentialID, proj.TeamID); err != nil {
			return nil, err
		}
		integ.CredentialID = *req.CredentialID
	}

	if req.BaseBranch != nil {
		if err := validateBaseBranch(*req.BaseBranch); err != nil {
			return nil, err
		}
	}

	if req.Enabled != nil {
		integ.Enabled = *req.Enabled
	}
	if integ.Config == nil {
		integ.Config = models.JSONMap{}
	}
	if req.BaseBranch != nil {
		integ.Config["base_branch"] = *req.BaseBranch
	}
	if req.AutoPR != nil {
		integ.Config["auto_pr"] = *req.AutoPR
	}
	if req.WebhookSecret != nil {
		integ.Config["webhook_secret"] = *req.WebhookSecret
	}
	if req.WebhookCallbackURL != nil {
		integ.Config["webhook_callback_url"] = *req.WebhookCallbackURL
	}
	if req.FilterLabel != nil {
		integ.Config["filter_label"] = *req.FilterLabel
	}
	integ.UpdatedAt = time.Now().UTC()

	if err := s.integRepo.Update(ctx, integ); err != nil {
		return nil, err
	}
	return s.toResponse(integ), nil
}

func (s *integrationService) DeleteIntegration(ctx context.Context, projectID, id string) error {
	if _, err := s.loadScopedIntegration(ctx, projectID, id); err != nil {
		return err
	}
	return s.integRepo.Delete(ctx, id)
}

// loadProject fetches a project, wrapping lookup failures in ErrProjectNotFound.
func (s *integrationService) loadProject(ctx context.Context, projectID string) (*models.Project, error) {
	proj, err := s.projRepo.GetByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	}
	return proj, nil
}

// loadScopedIntegration fetches an integration, returning ErrIntegrationNotFound
// when it is missing or belongs to a different project.
func (s *integrationService) loadScopedIntegration(ctx context.Context, projectID, id string) (*models.ProjectIntegration, error) {
	integ, err := s.integRepo.GetByID(ctx, id)
	if err != nil || integ.ProjectID != projectID {
		return nil, ErrIntegrationNotFound
	}
	return integ, nil
}

// checkCredentialTenant verifies the credential exists and belongs to teamID.
// Security Fix 1 (Tenant Isolation / BOLA): cred.TeamID must equal proj.TeamID.
func (s *integrationService) checkCredentialTenant(ctx context.Context, credentialID, teamID string) error {
	cred, err := s.credRepo.GetByID(ctx, credentialID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCredentialNotFound, err)
	}
	if cred.TeamID != teamID {
		return ErrUnauthorizedTenant
	}
	return nil
}

// Security Fix 2 (Secret Masking): mask webhook_secret in Config
func (s *integrationService) toResponse(m *models.ProjectIntegration) *dto.IntegrationResponse {
	extURL := ""
	if m.ExternalURL != nil {
		extURL = *m.ExternalURL
	}

	maskedConfig := models.JSONMap{}
	for k, v := range m.Config {
		if k == "webhook_secret" {
			maskedConfig[k] = "********"
		} else {
			maskedConfig[k] = v
		}
	}

	return &dto.IntegrationResponse{
		ID:           m.ID,
		ProjectID:    m.ProjectID,
		CredentialID: m.CredentialID,
		Provider:     string(m.Provider),
		ExternalID:   m.ExternalID,
		ExternalURL:  extURL,
		Enabled:      m.Enabled,
		Config:       maskedConfig,
		LastSyncedAt: m.LastSyncedAt,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}
