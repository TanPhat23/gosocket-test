package auth_test

import (
	"testing"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"
)

func TestRoleRank(t *testing.T) {
	tests := []struct {
		name string
		role models.TeamRole
		want int
	}{
		{"viewer", models.RoleViewer, 10},
		{"member", models.RoleMember, 20},
		{"admin", models.RoleAdmin, 30},
		{"owner", models.RoleOwner, 40},
		{"empty role is zero value", models.TeamRole(0), 0},
		{"undefined role 999", models.TeamRole(999), 0},
		{"negative role", models.TeamRole(-1), 0},
		{"between defined roles", models.TeamRole(15), 0},
		{"one above owner", models.TeamRole(41), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := auth.RoleRank(tc.role); got != tc.want {
				t.Errorf("RoleRank(%d) = %d, want %d", tc.role, got, tc.want)
			}
		})
	}
}

func TestRoleRankOrdering(t *testing.T) {
	ordered := []models.TeamRole{models.RoleViewer, models.RoleMember, models.RoleAdmin, models.RoleOwner}

	for _, role := range ordered {
		if auth.RoleRank(role) <= 0 {
			t.Errorf("defined role %d has non-positive rank %d", role, auth.RoleRank(role))
		}
	}
	for i := 1; i < len(ordered); i++ {
		lower, higher := ordered[i-1], ordered[i]
		if !(auth.RoleRank(lower) < auth.RoleRank(higher)) {
			t.Errorf("RoleRank(%d)=%d must be < RoleRank(%d)=%d",
				lower, auth.RoleRank(lower), higher, auth.RoleRank(higher))
		}
	}
}
