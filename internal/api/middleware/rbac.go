package middleware

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/domain/auth"
)

// CheckRole verifies the authenticated user has at least the given role.
// Returns a huma error if insufficient permissions.
// Use this at the start of huma handlers for role-based access control.
func CheckRole(ctx context.Context, minRole auth.Role) error {
	role := RoleFromContext(ctx)
	if role == "" {
		return huma.Error401Unauthorized("not authenticated")
	}
	if !role.AtLeast(minRole) {
		return huma.Error403Forbidden("insufficient permissions: requires " + string(minRole))
	}
	return nil
}
