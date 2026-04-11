package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
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
	authSvc  *app.AuthService
	users    auth.UserRepository
	sessions auth.SessionRepository
}

func NewOAuthHandler(authSvc *app.AuthService, users auth.UserRepository, sessions auth.SessionRepository) *OAuthHandler {
	return &OAuthHandler{authSvc: authSvc, users: users, sessions: sessions}
}

// Setup configures OAuth providers from environment variables.
// Returns true if any providers were configured.
func (h *OAuthHandler) Setup() bool {
	callbackBase := envOrDefault("COMPOSER_OAUTH_CALLBACK_URL", "http://localhost:8080")

	var providers []goth.Provider

	if clientID := os.Getenv("COMPOSER_GITHUB_CLIENT_ID"); clientID != "" {
		clientSecret := os.Getenv("COMPOSER_GITHUB_CLIENT_SECRET")
		providers = append(providers, github.New(
			clientID, clientSecret,
			callbackBase+"/api/v1/auth/oauth/github/callback",
			"user:email",
		))
	}

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

	// Session store for goth OAuth flow state
	key := os.Getenv("COMPOSER_SESSION_SECRET")
	if key == "" {
		// Generate a random key if not provided (safe default, but sessions
		// won't survive restarts unless the env var is set)
		buf := make([]byte, 32)
		rand.Read(buf)
		key = hex.EncodeToString(buf)
	}
	store := sessions.NewCookieStore([]byte(key))
	store.MaxAge(300) // 5 min for OAuth flow
	store.Options.HttpOnly = true
	store.Options.SameSite = http.SameSiteLaxMode
	store.Options.Secure = os.Getenv("COMPOSER_COOKIE_SECURE") != "false"
	gothic.Store = store

	return true
}

// RegisterRaw registers OAuth routes as raw chi handlers.
func (h *OAuthHandler) RegisterRaw(router chi.Router) {
	router.Get("/api/v1/auth/oauth/{provider}", h.Begin)
	router.Get("/api/v1/auth/oauth/{provider}/callback", h.Callback)
}

func (h *OAuthHandler) Begin(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	q := r.URL.Query()
	q.Set("provider", provider)
	r.URL.RawQuery = q.Encode()
	gothic.BeginAuthHandler(w, r)
}

func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	q := r.URL.Query()
	q.Set("provider", provider)
	r.URL.RawQuery = q.Encode()

	gothUser, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		http.Error(w, "OAuth authentication failed", http.StatusUnauthorized)
		return
	}

	email := gothUser.Email
	if email == "" {
		http.Error(w, "OAuth provider did not return an email", http.StatusBadRequest)
		return
	}

	// Check domain allowlist (if configured). Existing users bypass this check
	// so admins can manage access via user management instead of re-checking on every login.
	if allowed := os.Getenv("COMPOSER_OAUTH_ALLOWED_DOMAINS"); allowed != "" {
		domains := strings.Split(allowed, ",")
		emailAllowed := false
		for _, d := range domains {
			d = strings.TrimSpace(d)
			if d != "" && strings.HasSuffix(strings.ToLower(email), "@"+strings.ToLower(d)) {
				emailAllowed = true
				break
			}
		}
		// Allow existing users regardless (admin may have manually created them)
		if !emailAllowed {
			existing, _ := h.users.GetByEmail(r.Context(), email)
			if existing == nil {
				http.Error(w, "Email domain not allowed for auto-provisioning", http.StatusForbidden)
				return
			}
		}
	}

	// Find or create user
	user, err := h.users.GetByEmail(r.Context(), email)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if user == nil {
		count, _ := h.users.Count(r.Context())
		role := auth.RoleViewer
		if count == 0 {
			role = auth.RoleAdmin
		}

		user, err = auth.NewUser(email, generateSecureOAuthPlaceholder(), role)
		if err != nil {
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
		user.AuthProvider = provider // "github" or "google"
		if err := h.users.Create(r.Context(), user); err != nil {
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
	}

	// Revoke existing sessions (session fixation prevention, S4)
	_ = h.sessions.DeleteByUserID(r.Context(), user.ID)

	// Create session and PERSIST IT TO THE DATABASE
	session, err := auth.NewSession(user.ID, user.Role, 24*time.Hour)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	if err := h.sessions.Create(r.Context(), session); err != nil {
		http.Error(w, "Failed to persist session", http.StatusInternalServerError)
		return
	}

	// Set session cookie
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

	http.Redirect(w, r, "/", http.StatusFound)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// generateSecureOAuthPlaceholder creates a cryptographically random placeholder password.
// OAuth users authenticate via their provider, never via this password.
func generateSecureOAuthPlaceholder() string {
	buf := make([]byte, 64)
	rand.Read(buf)
	return fmt.Sprintf("oauth_%s", hex.EncodeToString(buf))
}
