package dto

import "time"

// --- User DTOs ---

type CreateUserInput struct {
	Body struct {
		Email    string `json:"email" minLength:"3" doc:"User email"`
		Password string `json:"password" minLength:"8" maxLength:"72" doc:"Password (8-72 characters)"`
		Role     string `json:"role" enum:"admin,operator,viewer" doc:"User role"`
	}
}

type UpdateUserInput struct {
	ID   string `path:"id" doc:"User ID"`
	Body struct {
		Email string `json:"email,omitempty" doc:"Updated email"`
		Role  string `json:"role,omitempty" enum:"admin,operator,viewer" doc:"Updated role"`
	}
}

type ChangePasswordInput struct {
	ID   string `path:"id" doc:"User ID"`
	Body struct {
		OldPassword string `json:"old_password" minLength:"1" doc:"Current password"`
		NewPassword string `json:"new_password" minLength:"8" maxLength:"72" doc:"New password"`
	}
}

type UserIDInput struct {
	ID string `path:"id" doc:"User ID"`
}

type UserOutput struct {
	Body struct {
		ID          string     `json:"id"`
		Email       string     `json:"email"`
		Role        string     `json:"role"`
		CreatedAt   time.Time  `json:"created_at"`
		UpdatedAt   time.Time  `json:"updated_at"`
		LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	}
}

type UserListOutput struct {
	Body struct {
		Users []UserSummary `json:"users"`
	}
}

type UserSummary struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

// --- API Key DTOs ---

type CreateKeyInput struct {
	Body struct {
		Name      string  `json:"name" minLength:"1" doc:"Key name (human-readable label)"`
		Role      string  `json:"role" enum:"admin,operator,viewer" doc:"Key role"`
		ExpiresAt *string `json:"expires_at,omitempty" doc:"Expiry time (RFC3339) or null for never"`
	}
}

type KeyIDInput struct {
	ID string `path:"id" doc:"API key ID"`
}

type KeyCreatedOutput struct {
	Body struct {
		ID           string     `json:"id"`
		Name         string     `json:"name"`
		Role         string     `json:"role"`
		PlaintextKey string     `json:"plaintext_key" doc:"Full key (shown once, save it now)"`
		ExpiresAt    *time.Time `json:"expires_at,omitempty"`
		CreatedAt    time.Time  `json:"created_at"`
	}
}

type KeyListOutput struct {
	Body struct {
		Keys []KeySummary `json:"keys"`
	}
}

type KeySummary struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Role       string     `json:"role"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}
