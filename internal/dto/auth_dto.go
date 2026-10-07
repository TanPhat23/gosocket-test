package dto

// UserResponseDTO represents the safe user profile output.
type UserResponseDTO struct {
	ID          string `json:"id"`
	Issuer      string `json:"issuer"`
	Subject     string `json:"subject"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
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
