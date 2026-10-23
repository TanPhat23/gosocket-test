package dto

import "agent-task-manager/internal/models"

// UserResponseDTO represents the safe user profile output.
type UserResponseDTO struct {
	ID          string `json:"id"`
	Issuer      string `json:"issuer"`
	Subject     string `json:"subject"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	GitName     string `json:"git_name,omitempty"`
	GitEmail    string `json:"git_email,omitempty"`
}

// GitIdentityRequest sets the author name and email the user's tasks are committed as. Both empty clears it.
type GitIdentityRequest struct {
	GitName  string `json:"git_name"`
	GitEmail string `json:"git_email"`
}

// ToUserResponse maps a models.User to its safe response representation.
func ToUserResponse(u *models.User) UserResponseDTO {
	return UserResponseDTO{
		ID:          u.ID,
		Issuer:      u.Issuer,
		Subject:     u.Subject,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		GitName:     u.GitName,
		GitEmail:    u.GitEmail,
	}
}

// LoginRequestDTO represents the credentials payload for direct login.
type LoginRequestDTO struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// LoginResponseDTO represents the response from login/callback.
type LoginResponseDTO struct {
	Status string          `json:"status"`
	User   UserResponseDTO `json:"user"`
}
