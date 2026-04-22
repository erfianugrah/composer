package dto

// DiffOutput returns a compose diff result.
type DiffOutput struct {
	Body struct {
		HasChanges bool       `json:"has_changes"`
		Summary    string     `json:"summary"`
		Lines      []DiffLine `json:"lines"`
	}
}

type DiffLine struct {
	Type    string `json:"type" enum:"context,added,removed"`
	Content string `json:"content"`
	OldLine int    `json:"old_line,omitempty"`
	NewLine int    `json:"new_line,omitempty"`
}

// --- System info & version ---

// SystemInfoOutput is the response body for GET /api/v1/system/info.
type SystemInfoOutput struct {
	Body struct {
		Docker struct {
			Version    string `json:"version"`
			APIVersion string `json:"api_version"`
			Runtime    string `json:"runtime"`
			OS         string `json:"os"`
			Arch       string `json:"arch"`
			Containers int    `json:"containers"`
			Images     int    `json:"images"`
		} `json:"docker"`
	}
}

// VersionOutput is the response body for GET /api/v1/system/version.
type VersionOutput struct {
	Body struct {
		Version   string `json:"version" doc:"Composer semver"`
		GoVersion string `json:"go_version" doc:"Go toolchain version"`
		OS        string `json:"os" doc:"GOOS (linux, darwin, ...)"`
		Arch      string `json:"arch" doc:"GOARCH (amd64, arm64, ...)"`
		Uptime    string `json:"uptime" doc:"Human-readable uptime (e.g. 26h53m57s)"`
	}
}

// BootstrapStatusOutput is the response body for GET /api/v1/auth/bootstrap.
type BootstrapStatusOutput struct {
	Body struct {
		Needed bool `json:"needed" doc:"True if no users exist and bootstrap is required"`
	}
}

// --- Global config ---

// SSHKeyInfo describes an SSH key file detected in the composer home directory.
type SSHKeyInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Encrypted bool   `json:"encrypted" doc:"True if encrypted at rest with enc: prefix"`
}

// ConfigOutput is the response body for GET /api/v1/system/config.
type ConfigOutput struct {
	Body struct {
		SSHKeys         []SSHKeyInfo `json:"ssh_keys" doc:"SSH keys detected on the system"`
		EncryptionKey   string       `json:"encryption_key" enum:"env,file,none" doc:"Source of encryption key"`
		SopsAvailable   bool         `json:"sops_available" doc:"Whether sops binary is in PATH"`
		AgeKeyLoaded    bool         `json:"age_key_loaded" doc:"Whether a global age key was found"`
		AgeKeySource    string       `json:"age_key_source" doc:"Where the age key was loaded from"`
		AgePublicKey    string       `json:"age_public_key,omitempty" doc:"Age public key (recipient) for encrypting new secrets"`
		GitTokenSet     bool         `json:"git_token_set" doc:"Whether a global git token is configured"`
		GitTokenPreview string       `json:"git_token_preview,omitempty" doc:"First 8 chars of the token"`
		NotifyURL       string       `json:"notify_url,omitempty" doc:"Webhook notification URL (redacted)"`
		SlackWebhook    bool         `json:"slack_webhook" doc:"Whether Slack webhook is configured"`
		TrustedProxies  bool         `json:"trusted_proxies" doc:"Whether X-Real-IP headers are trusted"`
		CookieSecure    string       `json:"cookie_secure"`
		DatabaseType    string       `json:"database_type" enum:"sqlite,postgres"`
	}
}

// UpdateAgeKeyInput is the request body for PUT /api/v1/system/config/age-key.
type UpdateAgeKeyInput struct {
	Body struct {
		AgeKey string `json:"age_key" maxLength:"4096" doc:"Age private key (AGE-SECRET-KEY-...) or empty to remove"`
	}
}

// UpdateAgeKeyOutput is the response for a successful age key update.
type UpdateAgeKeyOutput struct {
	Body struct {
		PublicKey string `json:"public_key,omitempty" doc:"Corresponding age public key"`
		Saved     bool   `json:"saved"`
	}
}

// GenerateAgeKeyOutput is the response for age key generation.
// PrivateKey is shown exactly once; subsequent GETs only expose PublicKey.
type GenerateAgeKeyOutput struct {
	Body struct {
		PrivateKey string `json:"private_key" doc:"Generated age private key (shown once)"`
		PublicKey  string `json:"public_key" doc:"Corresponding age public key (recipient)"`
	}
}

// AddSSHKeyInput is the request body for POST /api/v1/system/config/ssh-keys.
type AddSSHKeyInput struct {
	Body struct {
		Name    string `json:"name" minLength:"1" maxLength:"64" pattern:"^[A-Za-z0-9_-]+$" doc:"Key file name (e.g. id_ed25519, id_github). Alphanumerics, dashes, and underscores only."`
		Content string `json:"content" minLength:"1" maxLength:"16384" doc:"PEM-encoded SSH private key content"`
	}
}

// AddSSHKeyOutput describes where the key was stored.
type AddSSHKeyOutput struct {
	Body struct {
		Path      string `json:"path"`
		Encrypted bool   `json:"encrypted"`
	}
}

// DeleteSSHKeyInput is the path parameter for key deletion.
type DeleteSSHKeyInput struct {
	Name string `path:"name" maxLength:"64" doc:"Key file name"`
}

// UpdateGitTokenInput is the request body for PUT /api/v1/system/config/git-token.
type UpdateGitTokenInput struct {
	Body struct {
		Token string `json:"token" maxLength:"512" doc:"Global git access token (GitHub PAT, GitLab token, etc.). Empty to remove."`
	}
}

// GitTokenOutput describes the configured global git token (redacted).
type GitTokenOutput struct {
	Body struct {
		Configured bool   `json:"configured"`
		Preview    string `json:"preview,omitempty" doc:"First 8 chars of token for identification"`
	}
}
