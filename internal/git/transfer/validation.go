package transfer

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	idRegex  = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
	// Git ref validation adhering strictly to git-check-ref-format(1)
	refRegex = regexp.MustCompile(`^refs/heads/[a-zA-Z0-9_\.\-]+(/[a-zA-Z0-9_\.\-]+)*$`)
)

// ValidateRepoID checks that a repository identifier contains only safe alphanumeric characters.
func ValidateRepoID(repoID string) error {
	if repoID == "" || !idRegex.MatchString(repoID) {
		return fmt.Errorf("%w: repoID %q", ErrInvalidIdentifier, repoID)
	}
	return nil
}

// ValidateTaskID checks that a task identifier contains only safe alphanumeric characters.
func ValidateTaskID(taskID string) error {
	if taskID == "" || !idRegex.MatchString(taskID) {
		return fmt.Errorf("%w: taskID %q", ErrInvalidIdentifier, taskID)
	}
	return nil
}

// ValidateRefName checks that a ref name is fully qualified (refs/heads/...) and safe.
func ValidateRefName(refName string) error {
	if refName == "" {
		return fmt.Errorf("%w: ref name cannot be empty", ErrInvalidRefName)
	}
	if strings.HasPrefix(refName, "-") {
		return fmt.Errorf("%w: ref name cannot start with a dash", ErrInvalidRefName)
	}
	if strings.Contains(refName, "..") ||
		strings.Contains(refName, "@{") ||
		strings.Contains(refName, ":") ||
		strings.Contains(refName, "^") ||
		strings.Contains(refName, "~") ||
		strings.Contains(refName, "*") ||
		strings.Contains(refName, "?") ||
		strings.Contains(refName, "[") ||
		strings.Contains(refName, "\\") ||
		strings.Contains(refName, " ") ||
		strings.ContainsAny(refName, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x7f") {
		return fmt.Errorf("%w: ref contains forbidden git special characters: %q", ErrInvalidRefName, refName)
	}
	if !refRegex.MatchString(refName) {
		return fmt.Errorf("%w: ref name %q does not match canonical refs/heads/ pattern", ErrInvalidRefName, refName)
	}
	return nil
}
