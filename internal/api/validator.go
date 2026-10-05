package api

import (
	"agent-task-manager/internal/models"
)

// StructValidator wraps models.Validate to satisfy Fiber v3 StructValidator interface.
type StructValidator struct{}

// NewStructValidator creates a new StructValidator instance.
func NewStructValidator() *StructValidator {
	return &StructValidator{}
}

// Validate validates out using models.Validate.
func (v *StructValidator) Validate(out any) error {
	return models.Validate.Struct(out)
}
