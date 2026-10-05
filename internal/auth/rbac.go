package auth

import (
	"agent-task-manager/internal/models"
)

// roleHierarchy defines the integer rank of each team role.
// Higher rank grants strictly greater or equal permissions.
var roleHierarchy = map[models.TeamRole]int{
	models.RoleOwner:  4,
	models.RoleAdmin:  3,
	models.RoleMember: 2,
	models.RoleViewer: 1,
}

// RoleRank returns the integer hierarchy rank for a TeamRole (1..4), or 0 if invalid.
func RoleRank(role models.TeamRole) int {
	return roleHierarchy[role]
}

// HasRolePermission returns true if userRole satisfies the minimum requiredRole.
func HasRolePermission(userRole, requiredRole models.TeamRole) bool {
	userRank, userOk := roleHierarchy[userRole]
	reqRank, reqOk := roleHierarchy[requiredRole]
	if !userOk || !reqOk {
		return false
	}
	return userRank >= reqRank
}
