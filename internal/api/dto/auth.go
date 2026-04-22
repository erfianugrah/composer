package dto

import "time"

// BootstrapInput is the request body for creating the first admin user.
type BootstrapInput struct {
	Body struct {
		Email    string `json:"email" format:"email" minLength:"3" maxLength:"320" doc:"Admin email address"`
		Password string `json:"password" minLength:"8" maxLength:"72" doc:"Admin password (8-72 characters, bcrypt limit)"`
	}
}

// BootstrapOutput is the response after creating the first admin user.
type BootstrapOutput struct {
	Body struct {
		ID    string `json:"id" doc:"User ID"`
		Email string `json:"email" format:"email" doc:"User email"`
		Role  string `json:"role" enum:"admin" doc:"Always 'admin' for bootstrap"`
	}
}

// LoginInput is the request body for logging in.
type LoginInput struct {
	Body struct {
		Email    string `json:"email" format:"email" minLength:"3" maxLength:"320" doc:"User email"`
		Password string `json:"password" minLength:"1" maxLength:"72" doc:"User password"`
	}
}

// LoginOutput is returned after successful login. The session cookie is set in the handler.
type LoginOutput struct {
	SetCookie []*SetCookieValue `header:"Set-Cookie"`
	Body      struct {
		UserID    string    `json:"user_id" doc:"Authenticated user ID"`
		Email     string    `json:"email" format:"email" doc:"User email address"`
		Role      string    `json:"role" enum:"admin,operator,viewer" doc:"User role"`
		ExpiresAt time.Time `json:"expires_at" doc:"Session expiry time"`
	}
}

// LogoutOutput clears the session cookie on successful logout.
type LogoutOutput struct {
	SetCookie []*SetCookieValue `header:"Set-Cookie"`
}

// SetCookieValue wraps a cookie string for huma header output.
type SetCookieValue string

func (s SetCookieValue) String() string { return string(s) }

// SessionOutput is the response for session validation.
type SessionOutput struct {
	Body struct {
		UserID string `json:"user_id" doc:"Authenticated user ID"`
		Role   string `json:"role" enum:"admin,operator,viewer" doc:"User role"`
	}
}

// ErrorOutput is a standard error response (RFC 9457).
type ErrorOutput struct {
	Body struct {
		Status int    `json:"status" doc:"HTTP status code"`
		Title  string `json:"title" doc:"Error title"`
		Detail string `json:"detail" doc:"Error detail message"`
	}
}
