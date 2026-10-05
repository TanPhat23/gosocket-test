package auth

import (
	"agent-task-manager/internal/models"
)

// RoleRank returns the integer hierarchy rank for a TeamRole (10..40), or 0 if invalid.
func RoleRank(role models.TeamRole) int {
	if !role.IsValid() {
		return 0
	}
	return int(role)
}

// HasRolePermission returns true if userRole satisfies the minimum requiredRole.
func HasRolePermission(userRole, requiredRole models.TeamRole) bool {
	return userRole.IsValid() && requiredRole.IsValid() && userRole >= requiredRole
}
