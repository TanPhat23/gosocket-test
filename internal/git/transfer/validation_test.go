package transfer_test

import (
	"errors"
	"testing"

	"agent-task-manager/internal/git/transfer"
)

func TestValidation_ValidateIdentifiers(t *testing.T) {
	tests := []struct {
		name    string
		fn      func(string) error
		input   string
		wantErr error
	}{
		// Repo ID validation
		{"valid repo id", transfer.ValidateRepoID, "repo-123_abc", nil},
		{"empty repo id", transfer.ValidateRepoID, "", transfer.ErrInvalidIdentifier},
		{"repo id traversal", transfer.ValidateRepoID, "../repo", transfer.ErrInvalidIdentifier},
		{"repo id slash", transfer.ValidateRepoID, "repo/sub", transfer.ErrInvalidIdentifier},
		{"repo id null byte", transfer.ValidateRepoID, "repo\x00bad", transfer.ErrInvalidIdentifier},
		{"repo id space", transfer.ValidateRepoID, "repo bad", transfer.ErrInvalidIdentifier},

		// Task ID validation
		{"valid task id", transfer.ValidateTaskID, "task-456_def", nil},
		{"empty task id", transfer.ValidateTaskID, "", transfer.ErrInvalidIdentifier},
		{"task id traversal", transfer.ValidateTaskID, "../../etc", transfer.ErrInvalidIdentifier},
		{"task id backslash", transfer.ValidateTaskID, "task\\bad", transfer.ErrInvalidIdentifier},

		// Ref name validation
		{"valid branch ref", transfer.ValidateRefName, "refs/heads/main", nil},
		{"valid task branch ref", transfer.ValidateRefName, "refs/heads/task/task-123", nil},
		{"empty ref", transfer.ValidateRefName, "", transfer.ErrInvalidRefName},
		{"ref with double dot", transfer.ValidateRefName, "refs/heads/../main", transfer.ErrInvalidRefName},
		{"ref with at brace", transfer.ValidateRefName, "refs/heads/main@{1}", transfer.ErrInvalidRefName},
		{"ref with colon", transfer.ValidateRefName, "refs/heads/main:main", transfer.ErrInvalidRefName},
		{"ref with flag dash", transfer.ValidateRefName, "--upload-pack=evil", transfer.ErrInvalidRefName},
		{"ref with space", transfer.ValidateRefName, "refs/heads/my branch", transfer.ErrInvalidRefName},
		{"ref with control char", transfer.ValidateRefName, "refs/heads/main\n", transfer.ErrInvalidRefName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn(tc.input)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil error, got: %v", err)
				}
			} else {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected error wrapping %v, got: %v", tc.wantErr, err)
				}
			}
		})
	}
}
