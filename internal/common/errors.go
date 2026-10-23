package common

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Standard system errors
var (
	ErrForbidden          = errors.New("insufficient permissions")
	ErrNotFound           = errors.New("resource not found")
	ErrServiceUnavailable = errors.New("agent execution service unavailable")
	// ErrInvalidPath marks a request path that is not a plain relative path inside a task workspace.
	ErrInvalidPath = errors.New("invalid path")
)

// AppError represents a typed application error carrying an HTTP status code and safe message.
type AppError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// NewAppError creates a new AppError with the given status code, client message, and underlying cause.
func NewAppError(code int, message string, underlying error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     underlying,
	}
}


// IsNotFound reports whether err is, or wraps, a "not found" condition from
// either the domain layer (ErrNotFound) or the persistence layer
// (gorm.ErrRecordNotFound).
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound)
}
