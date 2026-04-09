package handler

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	ageLib "filippo.io/age"

	composer "github.com/erfianugrah/composer"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/infra/crypto"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/sops"
)

var startTime = time.Now()

// SystemHandler registers system endpoints.
type SystemHandler struct {
	docker  *docker.Client
	dataDir string
}

func NewSystemHandler(docker *docker.Client, dataDir string) *SystemHandler {
	return &SystemHandler{docker: docker, dataDir: dataDir}
}

func (h *SystemHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "systemInfo", Method: http.MethodGet,
		Path: "/api/v1/system/info", Summary: "Docker engine info", Tags: []string{"system"},
	}, h.Info)

	huma.Register(api, huma.Operation{
		OperationID: "systemVersion", Method: http.MethodGet,
		Path: "/api/v1/system/version", Summary: "Composer version info", Tags: []string{"system"},
	}, h.Version)

	huma.Register(api, huma.Operation{
		OperationID: "systemConfig", Method: http.MethodGet,
		Path: "/api/v1/system/config", Summary: "Global config status (SSH keys, SOPS, encryption)", Tags: []string{"system"},
	}, h.Config)

	huma.Register(api, huma.Operation{
		OperationID: "updateAgeKey", Method: http.MethodPut,
		Path: "/api/v1/system/config/age-key", Summary: "Set or update the global age key for SOPS", Tags: []string{"system"},
	}, h.UpdateAgeKey)

	huma.Register(api, huma.Operation{
		OperationID: "generateAgeKey", Method: http.MethodPost,
		Path: "/api/v1/system/config/age-key/generate", Summary: "Generate a new age key pair and save", Tags: []string{"system"},
	}, h.GenerateAgeKey)

	huma.Register(api, huma.Operation{
		OperationID: "getGitToken", Method: http.MethodGet,
		Path: "/api/v1/system/config/git-token", Summary: "Get global git token status", Tags: []string{"system"},
	}, h.GetGitToken)
	huma.Register(api, huma.Operation{
		OperationID: "updateGitToken", Method: http.MethodPut,
		Path: "/api/v1/system/config/git-token", Summary: "Set or remove global git access token", Tags: []string{"system"},
	}, h.UpdateGitToken)

	huma.Register(api, huma.Operation{
		OperationID: "addSSHKey", Method: http.MethodPost,
		Path: "/api/v1/system/config/ssh-keys", Summary: "Add an SSH key by pasting content", Tags: []string{"system"},
	}, h.AddSSHKey)

	huma.Register(api, huma.Operation{
		OperationID: "deleteSSHKey", Method: http.MethodDelete,
		Path: "/api/v1/system/config/ssh-keys/{name}", Summary: "Delete an SSH key file", Tags: []string{"system"},
	}, h.DeleteSSHKey)
}

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

func (h *SystemHandler) Info(ctx context.Context, input *struct{}) (*SystemInfoOutput, error) {
	if h.docker == nil {
		return nil, huma.Error503ServiceUnavailable("docker not available")
	}

	info, err := h.docker.Info(ctx)
	if err != nil {
		return nil, serverError(err)
	}

	out := &SystemInfoOutput{}
	out.Body.Docker.Version = info.ServerVersion
	out.Body.Docker.APIVersion = info.Driver
	out.Body.Docker.Runtime = h.docker.Runtime()
	out.Body.Docker.OS = info.OperatingSystem
	out.Body.Docker.Arch = info.Architecture
	out.Body.Docker.Containers = info.Containers
	out.Body.Docker.Images = info.Images
	return out, nil
}

type VersionOutput struct {
	Body struct {
		Version   string `json:"version"`
		GoVersion string `json:"go_version"`
		OS        string `json:"os"`
		Arch      string `json:"arch"`
		Uptime    string `json:"uptime"`
	}
}

func (h *SystemHandler) Version(ctx context.Context, input *struct{}) (*VersionOutput, error) {
	out := &VersionOutput{}
	out.Body.Version = composer.Version
	out.Body.GoVersion = runtime.Version()
	out.Body.OS = runtime.GOOS
	out.Body.Arch = runtime.GOARCH
	out.Body.Uptime = time.Since(startTime).Round(time.Second).String()
	return out, nil
}

// --- Global Config ---

type SSHKeyInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Encrypted bool   `json:"encrypted" doc:"True if encrypted at rest with enc: prefix"`
}

type ConfigOutput struct {
	Body struct {
		SSHKeys         []SSHKeyInfo `json:"ssh_keys" doc:"SSH keys detected on the system"`
		EncryptionKey   string       `json:"encryption_key" doc:"Source of encryption key: env, file, or none"`
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
		DatabaseType    string       `json:"database_type" doc:"sqlite or postgres"`
	}
}

func (h *SystemHandler) Config(ctx context.Context, input *struct{}) (*ConfigOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	out := &ConfigOutput{}

	// SSH keys (never nil -- empty array for JSON serialization)
	out.Body.SSHKeys = listSSHKeys()
	if out.Body.SSHKeys == nil {
		out.Body.SSHKeys = []SSHKeyInfo{}
	}

	// Encryption key source
	if os.Getenv("COMPOSER_ENCRYPTION_KEY") != "" {
		out.Body.EncryptionKey = "env"
	} else {
		dataDir := h.dataDir
		if dataDir == "" {
			dataDir = "/opt/composer"
		}
		keyFile := filepath.Join(dataDir, "encryption.key")
		if _, err := os.Stat(keyFile); err == nil {
			out.Body.EncryptionKey = "file"
		} else {
			out.Body.EncryptionKey = "none"
		}
	}

	// SOPS/age
	// Git token
	tokenPath := filepath.Join(h.dataDir, "git-token")
	if data, err := crypto.DecryptFile(tokenPath); err == nil && data != "" {
		out.Body.GitTokenSet = true
		if len(data) > 8 {
			out.Body.GitTokenPreview = data[:8] + "..."
		}
	}

	out.Body.SopsAvailable = sops.IsAvailable()
	ageKey := sops.LoadGlobalAgeKey(h.dataDir)
	out.Body.AgeKeyLoaded = ageKey != ""
	if ageKey != "" {
		out.Body.AgeKeySource = detectAgeKeySource(h.dataDir)
		// Extract public key from private key
		identities, err := parseAgeIdentities(ageKey)
		if err == nil && identities != "" {
			out.Body.AgePublicKey = identities
		}
	}

	// Notification URLs (redact to boolean/partial)
	if url := os.Getenv("COMPOSER_NOTIFY_URL"); url != "" {
		if len(url) > 20 {
			out.Body.NotifyURL = url[:20] + "..."
		} else {
			out.Body.NotifyURL = url
		}
	}
	out.Body.SlackWebhook = os.Getenv("COMPOSER_SLACK_WEBHOOK") != ""
	out.Body.TrustedProxies = os.Getenv("COMPOSER_TRUSTED_PROXIES") != ""
	out.Body.CookieSecure = os.Getenv("COMPOSER_COOKIE_SECURE")
	if out.Body.CookieSecure == "" {
		out.Body.CookieSecure = "true"
	}

	// Database
	if dbURL := os.Getenv("COMPOSER_DB_URL"); strings.HasPrefix(dbURL, "postgres") {
		out.Body.DatabaseType = "postgres"
	} else {
		out.Body.DatabaseType = "sqlite"
	}

	return out, nil
}

type UpdateAgeKeyInput struct {
	Body struct {
		AgeKey string `json:"age_key" doc:"Age private key (AGE-SECRET-KEY-...) or empty to remove"`
	}
}

type UpdateAgeKeyOutput struct {
	Body struct {
		PublicKey string `json:"public_key,omitempty" doc:"Corresponding age public key"`
		Saved     bool   `json:"saved"`
	}
}

func (h *SystemHandler) UpdateAgeKey(ctx context.Context, input *UpdateAgeKeyInput) (*UpdateAgeKeyOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	out := &UpdateAgeKeyOutput{}

	if input.Body.AgeKey == "" {
		// Remove the key file
		dataDir := h.dataDir
		if dataDir == "" {
			dataDir = "/opt/composer"
		}
		os.Remove(filepath.Join(dataDir, "age.key"))
		out.Body.Saved = true
		return out, nil
	}

	// Validate it looks like an age key
	key := strings.TrimSpace(input.Body.AgeKey)
	if !strings.HasPrefix(key, "AGE-SECRET-KEY-") {
		return nil, huma.Error422UnprocessableEntity("invalid age key format -- must start with AGE-SECRET-KEY-")
	}

	// Extract public key
	pubKey, err := parseAgeIdentities(key)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("invalid age key: " + err.Error())
	}

	if err := sops.SaveAgeKey(h.dataDir, key, pubKey); err != nil {
		return nil, serverError(err)
	}

	out.Body.PublicKey = pubKey
	out.Body.Saved = true
	return out, nil
}

type GenerateAgeKeyOutput struct {
	Body struct {
		PrivateKey string `json:"private_key" doc:"Generated age private key (shown once)"`
		PublicKey  string `json:"public_key" doc:"Corresponding age public key (recipient)"`
	}
}

func (h *SystemHandler) GenerateAgeKey(ctx context.Context, input *struct{}) (*GenerateAgeKeyOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	privKey, pubKey, err := sops.GenerateAgeKey()
	if err != nil {
		return nil, serverError(err)
	}

	if err := sops.SaveAgeKey(h.dataDir, privKey, pubKey); err != nil {
		return nil, serverError(err)
	}

	out := &GenerateAgeKeyOutput{}
	out.Body.PrivateKey = privKey
	out.Body.PublicKey = pubKey
	return out, nil
}

type AddSSHKeyInput struct {
	Body struct {
		Name    string `json:"name" minLength:"1" doc:"Key file name (e.g. id_ed25519, id_github)"`
		Content string `json:"content" minLength:"1" doc:"PEM-encoded SSH private key content"`
	}
}

type AddSSHKeyOutput struct {
	Body struct {
		Path      string `json:"path"`
		Encrypted bool   `json:"encrypted"`
	}
}

func (h *SystemHandler) AddSSHKey(ctx context.Context, input *AddSSHKeyInput) (*AddSSHKeyOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(input.Body.Name)
	if strings.ContainsAny(name, "/\\.. ") {
		return nil, huma.Error422UnprocessableEntity("invalid key name: must not contain slashes, dots, or spaces")
	}

	sshDir := "/home/composer/.ssh"
	os.MkdirAll(sshDir, 0700)

	keyPath := filepath.Join(sshDir, name)
	content := strings.TrimSpace(input.Body.Content)

	// Write the key, then encrypt it at rest
	if err := os.WriteFile(keyPath, []byte(content+"\n"), 0600); err != nil {
		return nil, serverError(err)
	}

	// Encrypt at rest using our AES-256-GCM layer
	crypto.EncryptFile(keyPath)

	out := &AddSSHKeyOutput{}
	out.Body.Path = keyPath
	out.Body.Encrypted = true
	return out, nil
}

type DeleteSSHKeyInput struct {
	Name string `path:"name" doc:"Key file name"`
}

func (h *SystemHandler) DeleteSSHKey(ctx context.Context, input *DeleteSSHKeyInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(input.Name)
	if strings.ContainsAny(name, "/\\.. ") {
		return nil, huma.Error422UnprocessableEntity("invalid key name")
	}

	keyPath := filepath.Join("/home/composer/.ssh", name)
	if err := os.Remove(keyPath); err != nil {
		return nil, huma.Error404NotFound("key not found: " + err.Error())
	}
	// Also remove .pub if it exists
	os.Remove(keyPath + ".pub")

	return nil, nil
}

// --- Global Git Token ---

type UpdateGitTokenInput struct {
	Body struct {
		Token string `json:"token" doc:"Global git access token (GitHub PAT, GitLab token, etc.). Empty to remove."`
	}
}

type GitTokenOutput struct {
	Body struct {
		Configured bool   `json:"configured"`
		Preview    string `json:"preview,omitempty" doc:"First 8 chars of token for identification"`
	}
}

func (h *SystemHandler) GetGitToken(ctx context.Context, input *struct{}) (*GitTokenOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	out := &GitTokenOutput{}
	tokenPath := filepath.Join(h.dataDir, "git-token")
	if data, err := crypto.DecryptFile(tokenPath); err == nil && data != "" {
		out.Body.Configured = true
		if len(data) > 8 {
			out.Body.Preview = data[:8] + "..."
		}
	}
	return out, nil
}

func (h *SystemHandler) UpdateGitToken(ctx context.Context, input *UpdateGitTokenInput) (*GitTokenOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	tokenPath := filepath.Join(h.dataDir, "git-token")
	token := strings.TrimSpace(input.Body.Token)

	if token == "" {
		os.Remove(tokenPath)
		return &GitTokenOutput{}, nil
	}

	if err := crypto.WriteEncrypted(tokenPath, token); err != nil {
		return nil, serverError(err)
	}

	out := &GitTokenOutput{}
	out.Body.Configured = true
	if len(token) > 8 {
		out.Body.Preview = token[:8] + "..."
	}
	return out, nil
}

// --- helpers ---

func listSSHKeys() []SSHKeyInfo {
	var keys []SSHKeyInfo
	for _, dir := range []string{
		filepath.Join(os.Getenv("HOME"), ".ssh"),
		"/home/composer/.ssh",
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// Skip non-key files
			if name == "known_hosts" || name == "config" || name == "authorized_keys" ||
				strings.HasSuffix(name, ".pub") {
				continue
			}
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			encrypted := err == nil && len(data) > 4 && string(data[:4]) == "enc:"
			keys = append(keys, SSHKeyInfo{
				Name:      name,
				Path:      path,
				Encrypted: encrypted,
			})
		}
	}
	return keys
}

func detectAgeKeySource(dataDir string) string {
	if os.Getenv("COMPOSER_SOPS_AGE_KEY") != "" {
		return "COMPOSER_SOPS_AGE_KEY env"
	}
	if os.Getenv("SOPS_AGE_KEY") != "" {
		return "SOPS_AGE_KEY env"
	}
	if os.Getenv("SOPS_AGE_KEYS") != "" {
		return "SOPS_AGE_KEYS env"
	}
	if os.Getenv("COMPOSER_SOPS_AGE_KEY_FILE") != "" {
		return "COMPOSER_SOPS_AGE_KEY_FILE env"
	}
	if os.Getenv("SOPS_AGE_KEY_FILE") != "" {
		return "SOPS_AGE_KEY_FILE env"
	}
	if dataDir == "" {
		dataDir = os.Getenv("COMPOSER_DATA_DIR")
	}
	if dataDir == "" {
		dataDir = "/opt/composer"
	}
	if _, err := os.Stat(filepath.Join(dataDir, "age.key")); err == nil {
		return "data dir (age.key)"
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		if _, err := os.Stat(filepath.Join(home, ".config", "sops", "age", "keys.txt")); err == nil {
			return "~/.config/sops/age/keys.txt"
		}
	}
	return "unknown"
}

// parseAgeIdentities extracts the public key from an age private key string.
func parseAgeIdentities(privateKey string) (string, error) {
	identities, err := ageLib.ParseIdentities(strings.NewReader(privateKey))
	if err != nil || len(identities) == 0 {
		return "", err
	}
	if xi, ok := identities[0].(*ageLib.X25519Identity); ok {
		return xi.Recipient().String(), nil
	}
	return "", nil
}
