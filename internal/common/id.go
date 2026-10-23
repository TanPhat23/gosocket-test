package common

import (
	"uuid"
)

// NewID generates a new RFC 9562 time-ordered UUIDv7 string.
func NewID() string {
	return uuid.NewV7().String()
}

// IsValidUUID returns true if s is a valid RFC 9562 UUID.
func IsValidUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// ParseUUID parses a UUID string into a uuid.UUID value.
func ParseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
