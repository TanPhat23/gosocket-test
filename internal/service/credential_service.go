package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/integrations/github"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"gorm.io/gorm"
)

// ErrCredentialNotFound indicates that the requested credential was not found.
var ErrCredentialNotFound = errors.New("credential not found")

// ErrInvalidProvider indicates that the credential is not for the expected provider.
var ErrInvalidProvider = errors.New("credential provider must be github")

// GitHubRepoLister provides an interface to list repositories and branches from GitHub.
type GitHubRepoLister interface {
	ListRepositories(ctx context.Context, token string) ([]dto.GitHubRepositoryDTO, error)
	ListBranches(ctx context.Context, token, owner, repo string) ([]string, error)
}

func toCredentialDTO(c *models.Credential) dto.CredentialResponseDTO {
	return dto.CredentialResponseDTO{
		ID:        c.ID,
		TeamID:    c.TeamID,
		Provider:  c.Provider,
		Label:     c.Label,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// CredentialService manages credentials business logic and encryption.
type CredentialService interface {
	List(ctx context.Context, teamID string) ([]dto.CredentialResponseDTO, error)
	Create(ctx context.Context, teamID string, req dto.CreateCredentialRequest) (*dto.CredentialResponseDTO, error)
	Update(ctx context.Context, teamID, credID string, req dto.UpdateCredentialRequest) (*dto.CredentialResponseDTO, error)
	Delete(ctx context.Context, teamID, credID string) error
	ListGitHubRepositories(ctx context.Context, teamID, credentialID string) ([]dto.GitHubRepositoryDTO, error)
	ListGitHubBranches(ctx context.Context, teamID, credentialID, repoSlug string) ([]string, error)
}

type credentialService struct {
	credRepo       repository.CredentialRepository
	cipher         *crypto.SecretCipher
	githubProvider GitHubRepoLister
}

// CredentialServiceOption configures optional dependencies for CredentialService.
type CredentialServiceOption func(*credentialService)

// WithGitHubProvider overrides the default GitHub provider.
func WithGitHubProvider(provider GitHubRepoLister) CredentialServiceOption {
	return func(s *credentialService) {
		s.githubProvider = provider
	}
}

// NewCredentialService creates a new CredentialService.
func NewCredentialService(credRepo repository.CredentialRepository, cipher *crypto.SecretCipher, opts ...CredentialServiceOption) CredentialService {
	s := &credentialService{
		credRepo:       credRepo,
		cipher:         cipher,
		githubProvider: github.NewProvider(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *credentialService) List(ctx context.Context, teamID string) ([]dto.CredentialResponseDTO, error) {
	creds, err := s.credRepo.ListByTeamID(ctx, teamID)
	if err != nil {
		return nil, err
	}

	dtos := make([]dto.CredentialResponseDTO, 0, len(creds))
	for i := range creds {
		dtos = append(dtos, toCredentialDTO(&creds[i]))
	}

	return dtos, nil
}

func (s *credentialService) Create(ctx context.Context, teamID string, req dto.CreateCredentialRequest) (*dto.CredentialResponseDTO, error) {
	if s.cipher == nil {
		return nil, errors.New("secret encryption service is not configured")
	}

	encryptedData, err := s.cipher.Encrypt(req.Secret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}

	now := time.Now()
	cred := &models.Credential{
		ID:             common.NewID(),
		TeamID:         teamID,
		Provider:       req.Provider,
		Label:          req.Label,
		EncryptedData:  encryptedData,
		KeyFingerprint: s.cipher.KeyFingerprint(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.credRepo.Create(ctx, cred); err != nil {
		return nil, err
	}

	dto := toCredentialDTO(cred)
	return &dto, nil
}

func (s *credentialService) Update(ctx context.Context, teamID, credID string, req dto.UpdateCredentialRequest) (*dto.CredentialResponseDTO, error) {
	cred, err := s.credRepo.GetByIDAndTeam(ctx, credID, teamID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	if req.Label != nil && *req.Label != "" {
		cred.Label = *req.Label
	}

	if req.Secret != nil && *req.Secret != "" {
		if s.cipher == nil {
			return nil, errors.New("secret encryption service is not configured")
		}
		encryptedData, err := s.cipher.Encrypt(*req.Secret)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt secret: %w", err)
		}
		cred.EncryptedData = encryptedData
		cred.KeyFingerprint = s.cipher.KeyFingerprint()
	}

	cred.UpdatedAt = time.Now()
	if err := s.credRepo.Update(ctx, cred); err != nil {
		return nil, err
	}

	dto := toCredentialDTO(cred)
	return &dto, nil
}

func (s *credentialService) Delete(ctx context.Context, teamID, credID string) error {
	err := s.credRepo.Delete(ctx, credID, teamID)
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCredentialNotFound
	}
	return err
}

func (s *credentialService) ListGitHubRepositories(ctx context.Context, teamID, credentialID string) ([]dto.GitHubRepositoryDTO, error) {
	if s.cipher == nil {
		return nil, errors.New("secret encryption service is not configured")
	}

	cred, err := s.credRepo.GetByIDAndTeam(ctx, credentialID, teamID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	if cred.Provider != models.ProviderGitHub {
		return nil, ErrInvalidProvider
	}

	decryptedToken, err := s.cipher.Decrypt(cred.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credential secret: %w", err)
	}

	ghProvider := s.githubProvider
	if ghProvider == nil {
		ghProvider = github.NewProvider()
	}

	repos, err := ghProvider.ListRepositories(ctx, decryptedToken)
	// Clear the decrypted token in memory
	decryptedToken = ""
	_ = decryptedToken

	if err != nil {
		return nil, err
	}

	return repos, nil
}

func (s *credentialService) ListGitHubBranches(ctx context.Context, teamID, credentialID, repoSlug string) ([]string, error) {
	if s.cipher == nil {
		return nil, errors.New("secret encryption service is not configured")
	}

	cred, err := s.credRepo.GetByIDAndTeam(ctx, credentialID, teamID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	if cred.Provider != models.ProviderGitHub {
		return nil, ErrInvalidProvider
	}

	decryptedToken, err := s.cipher.Decrypt(cred.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credential secret: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(repoSlug), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("invalid repo format, expected owner/repo")
	}

	ghProvider := s.githubProvider
	if ghProvider == nil {
		ghProvider = github.NewProvider()
	}

	branches, err := ghProvider.ListBranches(ctx, decryptedToken, parts[0], parts[1])
	decryptedToken = ""
	_ = decryptedToken

	if err != nil {
		return nil, err
	}

	return branches, nil
}
