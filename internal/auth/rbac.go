package auth

import (
	"agent-task-manager/internal/models"
)

// HasRolePermission returns true if userRole satisfies the minimum requiredRole.
func HasRolePermission(userRole, requiredRole models.TeamRole) bool {
	return userRole.IsValid() && requiredRole.IsValid() && userRole >= requiredRole
}
