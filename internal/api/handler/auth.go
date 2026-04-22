package handler

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
)

const defaultSessionTTL = 7 * 24 * time.Hour

// AuthHandler registers auth-related API endpoints.
type AuthHandler struct {
	auth           *app.AuthService
	loginLimiter   *authmw.RateLimiter // per-email
	loginIPLimiter *authmw.RateLimiter // per-IP (S20)
}

func NewAuthHandler(auth *app.AuthService) *AuthHandler {
	return &AuthHandler{
		auth:           auth,
		loginLimiter:   authmw.LoginRateLimit(),
		loginIPLimiter: authmw.LoginRateLimit(), // same config, keyed differently
	}
}

// Register registers all auth routes on the huma API.
func (h *AuthHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "bootstrapStatus",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/bootstrap",
		Summary:     "Check if bootstrap is needed (no users exist)",
		Description: "Returns {needed: true} while the user table is empty. Used by the setup wizard to decide whether to prompt for first-admin creation. Public endpoint.",
		Tags:        []string{"auth"},
		Security:    []map[string][]string{}, // public
	}, h.BootstrapStatus)

	huma.Register(api, huma.Operation{
		OperationID: "bootstrapUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/bootstrap",
		Summary:     "Create the first admin user",
		Description: "Creates the initial admin account. Rejected with 409 once any user exists. Public endpoint (this is the only way to escape the chicken-and-egg auth requirement).",
		Tags:        []string{"auth"},
		Security:    []map[string][]string{}, // public
		Errors:      errsAuthBootstrap,
	}, h.Bootstrap)

	huma.Register(api, huma.Operation{
		OperationID: "loginUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "Log in with email and password",
		Description: "Authenticates against the user table and issues a session. Sets the `composer_session` cookie on success. Rate-limited per email and per IP. Public endpoint.",
		Tags:        []string{"auth"},
		Security:    []map[string][]string{}, // public
		Errors:      errsAuthLogin,
	}, h.Login)

	huma.Register(api, huma.Operation{
		OperationID: "logoutUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/logout",
		Summary:     "Destroy the current session",
		Description: "Revokes the current session token and clears the `composer_session` cookie. Requires an active session.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized},
	}, h.Logout)

	huma.Register(api, huma.Operation{
		OperationID: "getSession",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/session",
		Summary:     "Validate current session",
		Description: "Returns the authenticated user's ID and role when the session or API key is valid. Used by SPAs to populate the current-user UI state.",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized},
	}, h.Session)
}

func (h *AuthHandler) BootstrapStatus(ctx context.Context, input *struct{}) (*dto.BootstrapStatusOutput, error) {
	needed, err := h.auth.IsBootstrapNeeded(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	resp := &dto.BootstrapStatusOutput{}
	resp.Body.Needed = needed
	return resp, nil
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
	// Rate limit by IP first (prevents password spray across many emails from single IP)
	clientIP := authmw.RemoteIPFromContext(ctx)
	if clientIP != "" && !h.loginIPLimiter.Allow(clientIP) {
		return nil, huma.Error429TooManyRequests("too many login attempts from this address, try again later")
	}

	// Rate limit by email (prevents brute-force per account without enabling lockout DoS)
	if !h.loginLimiter.Allow(input.Body.Email) {
		return nil, huma.Error429TooManyRequests("too many login attempts for this account, try again later")
	}

	session, err := h.auth.Login(ctx, input.Body.Email, input.Body.Password, defaultSessionTTL)
	if err != nil {
		if errors.Is(err, app.ErrInvalidCredentials) {
			return nil, huma.Error401Unauthorized("invalid email or password")
		}
		return nil, serverError(ctx, err)
	}

	// Auto-detect TLS: if COMPOSER_COOKIE_SECURE is set, use it;
	// otherwise default to true (assume production behind TLS).
	// Only set false for local development without TLS.
	secureCookie := os.Getenv("COMPOSER_COOKIE_SECURE") != "false"

	cookie := &http.Cookie{
		Name:     "composer_session",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secureCookie,
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

func (h *AuthHandler) Logout(ctx context.Context, input *struct{}) (*dto.LogoutOutput, error) {
	sessionID := authmw.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	// Destroy session in DB
	if err := h.auth.Logout(ctx, sessionID); err != nil {
		return nil, serverError(ctx, err)
	}

	// Clear the cookie
	clearCookie := &http.Cookie{
		Name:     "composer_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   os.Getenv("COMPOSER_COOKIE_SECURE") != "false",
		MaxAge:   -1, // delete immediately
	}

	cookieVal := dto.SetCookieValue(clearCookie.String())
	return &dto.LogoutOutput{
		SetCookie: []*dto.SetCookieValue{&cookieVal},
	}, nil
}

func (h *AuthHandler) Session(ctx context.Context, input *struct{}) (*dto.SessionOutput, error) {
	role := authmw.RoleFromContext(ctx)
	userID := authmw.UserIDFromContext(ctx)

	if role == "" || userID == "" {
		return nil, huma.Error401Unauthorized("not authenticated")
	}

	resp := &dto.SessionOutput{}
	resp.Body.UserID = userID
	resp.Body.Role = string(role)
	return resp, nil
}
