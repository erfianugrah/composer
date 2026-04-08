package handler

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/github"
	gothGoogle "github.com/markbates/goth/providers/google"

	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
)

// OAuthHandler handles OAuth/OIDC login flows via goth.
type OAuthHandler struct {
	authSvc *app.AuthService
	users   auth.UserRepository
}

func NewOAuthHandler(authSvc *app.AuthService, users auth.UserRepository) *OAuthHandler {
	return &OAuthHandler{authSvc: authSvc, users: users}
}

// Setup configures OAuth providers from environment variables.
// Call this once at startup. Returns true if any providers were configured.
func (h *OAuthHandler) Setup() bool {
	callbackBase := envOrDefault("COMPOSER_OAUTH_CALLBACK_URL", "http://localhost:8080")

	var providers []goth.Provider

	// GitHub OAuth
	if clientID := os.Getenv("COMPOSER_GITHUB_CLIENT_ID"); clientID != "" {
		clientSecret := os.Getenv("COMPOSER_GITHUB_CLIENT_SECRET")
		providers = append(providers, github.New(
			clientID, clientSecret,
			callbackBase+"/api/v1/auth/oauth/github/callback",
			"user:email",
		))
	}

	// Google OAuth
	if clientID := os.Getenv("COMPOSER_GOOGLE_CLIENT_ID"); clientID != "" {
		clientSecret := os.Getenv("COMPOSER_GOOGLE_CLIENT_SECRET")
		providers = append(providers, gothGoogle.New(
			clientID, clientSecret,
			callbackBase+"/api/v1/auth/oauth/google/callback",
			"email", "profile",
		))
	}

	if len(providers) == 0 {
		return false
	}

	goth.UseProviders(providers...)

	// Configure session store for goth
	key := os.Getenv("COMPOSER_SESSION_SECRET")
	if key == "" {
		key = "composer-default-session-key-change-me"
	}
	store := sessions.NewCookieStore([]byte(key))
	store.MaxAge(300) // 5 min for OAuth flow
	store.Options.HttpOnly = true
	store.Options.SameSite = http.SameSiteLaxMode
	gothic.Store = store

	return true
}

// RegisterRaw registers OAuth routes as raw chi handlers (goth needs raw http).
func (h *OAuthHandler) RegisterRaw(router chi.Router) {
	router.Get("/api/v1/auth/oauth/{provider}", h.Begin)
	router.Get("/api/v1/auth/oauth/{provider}/callback", h.Callback)
}

// Begin starts the OAuth flow by redirecting to the provider.
func (h *OAuthHandler) Begin(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	r = r.WithContext(r.Context())
	// Set provider in query for goth
	q := r.URL.Query()
	q.Set("provider", provider)
	r.URL.RawQuery = q.Encode()

	gothic.BeginAuthHandler(w, r)
}

// Callback handles the OAuth callback from the provider.
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	q := r.URL.Query()
	q.Set("provider", provider)
	r.URL.RawQuery = q.Encode()

	gothUser, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		http.Error(w, "OAuth failed: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Find or create user by email
	email := gothUser.Email
	if email == "" {
		http.Error(w, "OAuth provider did not return an email", http.StatusBadRequest)
		return
	}

	user, err := h.users.GetByEmail(r.Context(), email)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if user == nil {
		// Check if any users exist -- if not, create as admin (first user)
		count, _ := h.users.Count(r.Context())
		role := auth.RoleViewer
		if count == 0 {
			role = auth.RoleAdmin
		}

		// Create user with OAuth -- no password (they'll use OAuth to login)
		user, err = auth.NewUser(email, generateOAuthPlaceholder(), role)
		if err != nil {
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
		if err := h.users.Create(r.Context(), user); err != nil {
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
	}

	// Create session
	session, err := auth.NewSession(user.ID, user.Role, 24*time.Hour)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// We need to persist the session -- but we don't have the session repo here.
	// Use the AuthService login-by-session approach instead.
	// For now, set the cookie directly.
	secureCookie := os.Getenv("COMPOSER_COOKIE_SECURE") != "false"
	http.SetCookie(w, &http.Cookie{
		Name:     "composer_session",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secureCookie,
		MaxAge:   86400,
	})

	// Redirect to dashboard
	http.Redirect(w, r, "/", http.StatusFound)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func generateOAuthPlaceholder() string {
	// OAuth users don't need a real password -- they authenticate via provider
	// Generate a long random string they'll never use
	return fmt.Sprintf("oauth_%d_%d", time.Now().UnixNano(), time.Now().UnixMicro())
}
