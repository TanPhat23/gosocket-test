package auth_test

import (
	"testing"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/models"
)

func TestRoleRanksAndPermissions(t *testing.T) {
	tests := []struct {
		name         string
		userRole     models.TeamRole
		requiredRole models.TeamRole
		expected     bool
	}{
		{"owner satisfies owner", models.RoleOwner, models.RoleOwner, true},
		{"owner satisfies admin", models.RoleOwner, models.RoleAdmin, true},
		{"owner satisfies member", models.RoleOwner, models.RoleMember, true},
		{"owner satisfies viewer", models.RoleOwner, models.RoleViewer, true},

		{"admin satisfies admin", models.RoleAdmin, models.RoleAdmin, true},
		{"admin satisfies member", models.RoleAdmin, models.RoleMember, true},
		{"admin satisfies viewer", models.RoleAdmin, models.RoleViewer, true},
		{"admin does NOT satisfy owner", models.RoleAdmin, models.RoleOwner, false},

		{"member satisfies member", models.RoleMember, models.RoleMember, true},
		{"member satisfies viewer", models.RoleMember, models.RoleViewer, true},
		{"member does NOT satisfy admin", models.RoleMember, models.RoleAdmin, false},
		{"member does NOT satisfy owner", models.RoleMember, models.RoleOwner, false},

		{"viewer satisfies viewer", models.RoleViewer, models.RoleViewer, true},
		{"viewer does NOT satisfy member", models.RoleViewer, models.RoleMember, false},
		{"viewer does NOT satisfy admin", models.RoleViewer, models.RoleAdmin, false},
		{"viewer does NOT satisfy owner", models.RoleViewer, models.RoleOwner, false},

		{"unknown user role returns false", models.TeamRole("invalid"), models.RoleViewer, false},
		{"unknown required role returns false", models.RoleOwner, models.TeamRole("superadmin"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.HasRolePermission(tc.userRole, tc.requiredRole)
			if got != tc.expected {
				t.Errorf("HasRolePermission(%q, %q) = %v; want %v", tc.userRole, tc.requiredRole, got, tc.expected)
			}
		})
	}
}
