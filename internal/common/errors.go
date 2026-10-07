package common

import "errors"

// Standard system errors
var (
	ErrForbidden          = errors.New("insufficient permissions")
	ErrNotFound           = errors.New("resource not found")
	ErrServiceUnavailable = errors.New("Agent execution service unavailable")
)
