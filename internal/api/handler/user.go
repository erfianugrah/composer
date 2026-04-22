package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
)

// UserHandler registers user management endpoints (admin only).
type UserHandler struct {
	users    auth.UserRepository
	sessions auth.SessionRepository // optional, for session invalidation on role change
}

func NewUserHandler(users auth.UserRepository) *UserHandler {
	return &UserHandler{users: users}
}

// SetSessionRepo enables session invalidation on role changes.
func (h *UserHandler) SetSessionRepo(sessions auth.SessionRepository) {
	h.sessions = sessions
}

func (h *UserHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listUsers", Method: http.MethodGet,
		Path:        "/api/v1/users",
		Summary:     "List all users",
		Description: "Returns every user account with role and last-login timestamp. Admin only.",
		Tags:        []string{"users"},
		Errors:      errsAdminMutation,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "createUser", Method: http.MethodPost,
		Path:        "/api/v1/users",
		Summary:     "Create a new user",
		Description: "Creates a local user with email + bcrypt-hashed password and assigns a role (admin / operator / viewer). Admin only.",
		Tags:        []string{"users"},
		Errors:      errsAdminMutation,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "getUser", Method: http.MethodGet,
		Path:        "/api/v1/users/{id}",
		Summary:     "Get user by ID",
		Description: "Returns a user's public profile. Admin only.",
		Tags:        []string{"users"},
		Errors:      errsAdminMutation,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "updateUser", Method: http.MethodPut,
		Path:        "/api/v1/users/{id}",
		Summary:     "Update user",
		Description: "Updates a user's email and/or role. Changing role revokes all existing sessions so the new role takes effect on next login. Admin only.",
		Tags:        []string{"users"},
		Errors:      errsAdminMutation,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "deleteUser", Method: http.MethodDelete,
		Path:        "/api/v1/users/{id}",
		Summary:     "Delete user",
		Description: "Deletes the user and cascades their sessions and API keys. Admin only.",
		Tags:        []string{"users"},
		Errors:      errsAdminMutation,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "changePassword", Method: http.MethodPut,
		Path:        "/api/v1/users/{id}/password",
		Summary:     "Change password",
		Description: "Admins can change any user's password; non-admins can only change their own (and must supply the old password). Successful rotation invalidates all active sessions for the user.",
		Tags:        []string{"users"},
		Errors:      errsAdminMutation,
	}, h.ChangePassword)
}

func (h *UserHandler) List(ctx context.Context, input *struct{}) (*dto.UserListOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	users, err := h.users.List(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.UserListOutput{}
	out.Body.Users = make([]dto.UserSummary, 0, len(users))
	for _, u := range users {
		out.Body.Users = append(out.Body.Users, dto.UserSummary{
			ID: u.ID, Email: u.Email, Role: string(u.Role),
			CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt,
		})
	}
	return out, nil
}

func (h *UserHandler) Create(ctx context.Context, input *dto.CreateUserInput) (*dto.UserOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	role, err := auth.ParseRole(input.Body.Role)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	user, err := auth.NewUser(input.Body.Email, input.Body.Password, role)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	if err := h.users.Create(ctx, user); err != nil {
		return nil, serverError(ctx, err)
	}

	return userToOutput(user), nil
}

func (h *UserHandler) Get(ctx context.Context, input *dto.UserIDInput) (*dto.UserOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	user, err := h.users.GetByID(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if user == nil {
		return nil, huma.Error404NotFound("user not found")
	}
	return userToOutput(user), nil
}

func (h *UserHandler) Update(ctx context.Context, input *dto.UpdateUserInput) (*dto.UserOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	user, err := h.users.GetByID(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if user == nil {
		return nil, huma.Error404NotFound("user not found")
	}

	if input.Body.Email != "" {
		user.Email = input.Body.Email
	}
	roleChanged := false
	if input.Body.Role != "" {
		role, err := auth.ParseRole(input.Body.Role)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		if role != user.Role {
			roleChanged = true
		}
		user.UpdateRole(role)
	}

	if err := h.users.Update(ctx, user); err != nil {
		return nil, serverError(ctx, err)
	}

	// Invalidate sessions when role changes so they pick up the new role
	if roleChanged && h.sessions != nil {
		_ = h.sessions.DeleteByUserID(ctx, user.ID) // best-effort
	}

	return userToOutput(user), nil
}

func (h *UserHandler) Delete(ctx context.Context, input *dto.UserIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	// Cascade: revoke all sessions before deleting (S9)
	if h.sessions != nil {
		_ = h.sessions.DeleteByUserID(ctx, input.ID)
	}
	if err := h.users.Delete(ctx, input.ID); err != nil {
		return nil, serverError(ctx, err)
	}
	return nil, nil
}

func (h *UserHandler) ChangePassword(ctx context.Context, input *dto.ChangePasswordInput) (*struct{}, error) {
	// Admin can change any user's password; users can change their own
	callerID := authmw.UserIDFromContext(ctx)
	callerRole := authmw.RoleFromContext(ctx)
	if input.ID != callerID && !callerRole.AtLeast(auth.RoleAdmin) {
		return nil, huma.Error403Forbidden("can only change your own password")
	}

	user, err := h.users.GetByID(ctx, input.ID)
	if err != nil || user == nil {
		return nil, huma.Error404NotFound("user not found")
	}

	if err := user.ChangePassword(input.Body.OldPassword, input.Body.NewPassword); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	if err := h.users.Update(ctx, user); err != nil {
		return nil, serverError(ctx, err)
	}

	// Invalidate all sessions so compromised tokens are revoked (S8)
	if h.sessions != nil {
		_ = h.sessions.DeleteByUserID(ctx, user.ID)
	}

	return nil, nil
}

func userToOutput(u *auth.User) *dto.UserOutput {
	out := &dto.UserOutput{}
	out.Body.ID = u.ID
	out.Body.Email = u.Email
	out.Body.Role = string(u.Role)
	out.Body.CreatedAt = u.CreatedAt
	out.Body.UpdatedAt = u.UpdatedAt
	out.Body.LastLoginAt = u.LastLoginAt
	return out
}
