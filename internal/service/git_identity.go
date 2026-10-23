package service

import (
	"net/mail"
	"strings"
	"unicode"

	"agent-task-manager/internal/common"
)

const (
	maxGitNameLen  = 100
	maxGitEmailLen = 254
)

// NormalizeGitIdentity trims and checks a git author name and email. Both empty is valid and means "no identity of
// my own"; one without the other is not, because git would then fall back to a half-set author.
func NormalizeGitIdentity(name, email string) (string, string, error) {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" && email == "" {
		return "", "", nil
	}
	if name == "" || email == "" {
		return "", "", common.NewAppError(400, "git name and email must be set together", nil)
	}
	if len(name) > maxGitNameLen {
		return "", "", common.NewAppError(400, "git name is too long", nil)
	}
	// Angle brackets delimit the email in a commit header, and git drops them from a name, so a name with one would
	// not round-trip.
	if strings.ContainsAny(name, "<>") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", "", common.NewAppError(400, "git name must not contain angle brackets or control characters", nil)
	}
	if len(email) > maxGitEmailLen || strings.IndexFunc(email, unicode.IsSpace) >= 0 || strings.IndexFunc(email, unicode.IsControl) >= 0 {
		return "", "", common.NewAppError(400, "git email is not a valid address", nil)
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || addr.Name != "" {
		return "", "", common.NewAppError(400, "git email is not a valid address", nil)
	}
	return name, email, nil
}
