package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	"github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
)

const defaultSessionTTL = 24 * time.Hour

// AuthHandler registers auth-related API endpoints.
type AuthHandler struct {
	auth *app.AuthService
}

func NewAuthHandler(auth *app.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// Register registers all auth routes on the huma API.
func (h *AuthHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "bootstrapUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/bootstrap",
		Summary:     "Create the first admin user",
		Description: "Only works when no users exist in the database.",
		Tags:        []string{"auth"},
	}, h.Bootstrap)

	huma.Register(api, huma.Operation{
		OperationID: "loginUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "Log in with email and password",
		Description: "Returns a session cookie on success.",
		Tags:        []string{"auth"},
	}, h.Login)

	huma.Register(api, huma.Operation{
		OperationID: "logoutUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/logout",
		Summary:     "Destroy the current session",
		Tags:        []string{"auth"},
	}, h.Logout)

	huma.Register(api, huma.Operation{
		OperationID: "getSession",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/session",
		Summary:     "Validate current session",
		Description: "Returns user info if the session is valid.",
		Tags:        []string{"auth"},
	}, h.Session)
}

func (h *AuthHandler) Bootstrap(ctx context.Context, input *dto.BootstrapInput) (*dto.BootstrapOutput, error) {
	user, err := h.auth.Bootstrap(ctx, input.Body.Email, input.Body.Password)
	if err != nil {
		if errors.Is(err, app.ErrBootstrapDone) {
			return nil, huma.Error409Conflict("bootstrap already completed, users exist")
		}
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	resp := &dto.BootstrapOutput{}
	resp.Body.ID = user.ID
	resp.Body.Email = user.Email
	resp.Body.Role = string(user.Role)
	return resp, nil
}

func (h *AuthHandler) Login(ctx context.Context, input *dto.LoginInput) (*dto.LoginOutput, error) {
	session, err := h.auth.Login(ctx, input.Body.Email, input.Body.Password, defaultSessionTTL)
	if err != nil {
		if errors.Is(err, app.ErrInvalidCredentials) {
			return nil, huma.Error401Unauthorized("invalid email or password")
		}
		return nil, huma.Error500InternalServerError(fmt.Sprintf("login failed: %v", err))
	}

	cookie := &http.Cookie{
		Name:     "composer_session",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // set true when behind TLS
		MaxAge:   int(defaultSessionTTL.Seconds()),
	}

	cookieVal := dto.SetCookieValue(cookie.String())
	resp := &dto.LoginOutput{
		SetCookie: []*dto.SetCookieValue{&cookieVal},
	}
	resp.Body.UserID = session.UserID
	resp.Body.Email = input.Body.Email
	resp.Body.Role = string(session.Role)
	resp.Body.ExpiresAt = session.ExpiresAt
	return resp, nil
}

// LogoutOutput clears the session cookie.
type LogoutOutput struct {
	SetCookie []*dto.SetCookieValue `header:"Set-Cookie"`
}

func (h *AuthHandler) Logout(ctx context.Context, input *struct{}) (*LogoutOutput, error) {
	sessionID := middleware.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	// Destroy session in DB
	if err := h.auth.Logout(ctx, sessionID); err != nil {
		return nil, huma.Error500InternalServerError("logout failed: " + err.Error())
	}

	// Clear the cookie
	clearCookie := &http.Cookie{
		Name:     "composer_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1, // delete immediately
	}

	cookieVal := dto.SetCookieValue(clearCookie.String())
	return &LogoutOutput{
		SetCookie: []*dto.SetCookieValue{&cookieVal},
	}, nil
}

func (h *AuthHandler) Session(ctx context.Context, input *struct{}) (*dto.SessionOutput, error) {
	role := middleware.RoleFromContext(ctx)
	userID := middleware.UserIDFromContext(ctx)

	if role == "" || userID == "" {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	resp := &dto.SessionOutput{}
	resp.Body.UserID = userID
	resp.Body.Role = string(role)
	return resp, nil
}
