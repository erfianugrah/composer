package dto

import "time"

// --- Requests ---

type ListRegistryCredentialsInput struct {
	Stack string `query:"stack" maxLength:"128" doc:"Optional: filter to this stack's per-stack creds. Empty returns all (global + every stack)."`
}

type GetRegistryCredentialInput struct {
	ID int64 `path:"id" doc:"Registry credential ID"`
}

type CreateRegistryCredentialInput struct {
	Body struct {
		Registry  string `json:"registry" minLength:"1" maxLength:"512" doc:"Registry hostname (ghcr.io, docker.io, registry.example.com:5000)"`
		Username  string `json:"username" minLength:"1" maxLength:"256" doc:"Registry username"`
		Secret    string `json:"secret" minLength:"1" maxLength:"4096" doc:"Password or personal access token"`
		Email     string `json:"email,omitempty" maxLength:"256" doc:"Optional email (some legacy registries require it)"`
		StackName string `json:"stack_name,omitempty" maxLength:"128" doc:"Empty = global credential applied to all stacks. Non-empty = per-stack override."`
	}
}

type UpdateRegistryCredentialInput struct {
	ID   int64 `path:"id" doc:"Registry credential ID"`
	Body struct {
		Registry  string `json:"registry" minLength:"1" maxLength:"512"`
		Username  string `json:"username" minLength:"1" maxLength:"256"`
		Secret    string `json:"secret,omitempty" maxLength:"4096" doc:"Leave empty to keep the existing secret"`
		Email     string `json:"email,omitempty" maxLength:"256"`
		StackName string `json:"stack_name,omitempty" maxLength:"128"`
	}
}

type DeleteRegistryCredentialInput struct {
	ID int64 `path:"id"`
}

// --- Responses ---

type RegistryCredentialOutput struct {
	ID            int64     `json:"id"`
	Registry      string    `json:"registry"`
	Username      string    `json:"username"`
	SecretSet     bool      `json:"secret_set" doc:"True when a secret is stored. Plaintext is never returned."`
	SecretPreview string    `json:"secret_preview,omitempty" doc:"Last 4 characters of the secret (debug aid)"`
	Email         string    `json:"email,omitempty"`
	StackName     string    `json:"stack_name,omitempty" doc:"Empty means global credential"`
	IsGlobal      bool      `json:"is_global"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ListRegistryCredentialsOutput struct {
	Body struct {
		Credentials []RegistryCredentialOutput `json:"credentials"`
	}
}

type GetRegistryCredentialOutput struct {
	Body RegistryCredentialOutput
}
