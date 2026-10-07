package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/crypto"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/repository"
	"gorm.io/gorm"
)

// ErrCredentialNotFound indicates that the requested credential was not found.
var ErrCredentialNotFound = errors.New("credential not found")

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
	Delete(ctx context.Context, teamID, credID string) error
}

type credentialService struct {
	credRepo repository.CredentialRepository
	cipher   *crypto.SecretCipher
}

// NewCredentialService creates a new CredentialService.
func NewCredentialService(credRepo repository.CredentialRepository, cipher *crypto.SecretCipher) CredentialService {
	return &credentialService{
		credRepo: credRepo,
		cipher:   cipher,
	}
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

func (s *credentialService) Delete(ctx context.Context, teamID, credID string) error {
	err := s.credRepo.Delete(ctx, credID, teamID)
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCredentialNotFound
	}
	return err
}
