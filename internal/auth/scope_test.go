package auth_test

import (
	"context"
	"testing"

	"agent-task-manager/internal/auth"
)

func TestTeamAllowed(t *testing.T) {
	base := context.Background()
	cases := []struct {
		name   string
		ctx    context.Context
		teamID string
		want   bool
	}{
		{"unscoped permits any team", base, "team-a", true},
		{"scope permits its team", auth.WithTeamScope(base, "team-a"), "team-a", true},
		{"scope denies other team", auth.WithTeamScope(base, "team-a"), "team-b", false},
		{"scope denies empty team id", auth.WithTeamScope(base, "team-a"), "", false},
		{"empty scope denies everything", auth.WithTeamScope(base, ""), "", false},
		{"inner scope wins", auth.WithTeamScope(auth.WithTeamScope(base, "team-a"), "team-b"), "team-a", false},
	}
	for _, tc := range cases {
		if got := auth.TeamAllowed(tc.ctx, tc.teamID); got != tc.want {
			t.Errorf("%s: TeamAllowed = %v, want %v", tc.name, got, tc.want)
		}
	}
}
