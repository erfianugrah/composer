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
	"github.com/erfianugrah/composer/internal/api/dto"
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
		Path:        "/api/v1/system/info",
		Summary:     "Docker engine info",
		Description: "Returns Docker daemon version, runtime, OS/arch, and counts of containers and images. Requires Docker to be available.",
		Tags:        []string{"system"},
		Errors:      errsDockerDependent,
	}, h.Info)

	huma.Register(api, huma.Operation{
		OperationID: "systemVersion", Method: http.MethodGet,
		Path:        "/api/v1/system/version",
		Summary:     "Composer version info",
		Description: "Returns Composer version, Go runtime version, OS/arch, and process uptime. Viewer+.",
		Tags:        []string{"system"},
		Errors:      errsViewer,
	}, h.Version)

	huma.Register(api, huma.Operation{
		OperationID: "systemConfig", Method: http.MethodGet,
		Path:        "/api/v1/system/config",
		Summary:     "Global config status",
		Description: "Reports SSH keys, SOPS age key, global git token, encryption key source, and database type. Secret material is always redacted. Admin only.",
		Tags:        []string{"system"},
		Errors:      errsAdminMutation,
	}, h.Config)

	huma.Register(api, huma.Operation{
		OperationID: "updateAgeKey", Method: http.MethodPut,
		Path:        "/api/v1/system/config/age-key",
		Summary:     "Set or update the global age key for SOPS",
		Description: "Stores a provided age private key as the global SOPS decryption key. Send empty string to remove. Admin only.",
		Tags:        []string{"system"},
		Errors:      errsAdminMutation,
	}, h.UpdateAgeKey)

	huma.Register(api, huma.Operation{
		OperationID: "generateAgeKey", Method: http.MethodPost,
		Path:        "/api/v1/system/config/age-key/generate",
		Summary:     "Generate a new age key pair and save",
		Description: "Generates and persists a fresh age keypair for SOPS. The private key is returned ONCE in the response; save or encrypt it externally. Admin only.",
		Tags:        []string{"system"},
		Errors:      errsAdminMutation,
	}, h.GenerateAgeKey)

	huma.Register(api, huma.Operation{
		OperationID: "getGitToken", Method: http.MethodGet,
		Path:        "/api/v1/system/config/git-token",
		Summary:     "Get global git token status",
		Description: "Returns whether a global git token is configured and a preview (first 8 chars). Admin only.",
		Tags:        []string{"system"},
		Errors:      errsAdminMutation,
	}, h.GetGitToken)
	huma.Register(api, huma.Operation{
		OperationID: "updateGitToken", Method: http.MethodPut,
		Path:        "/api/v1/system/config/git-token",
		Summary:     "Set or remove global git access token",
		Description: "Stores a plaintext git token encrypted at rest. Empty value removes the token. Admin only.",
		Tags:        []string{"system"},
		Errors:      errsAdminMutation,
	}, h.UpdateGitToken)

	huma.Register(api, huma.Operation{
		OperationID: "addSSHKey", Method: http.MethodPost,
		Path:        "/api/v1/system/config/ssh-keys",
		Summary:     "Add an SSH key by pasting content",
		Description: "Writes a PEM-encoded SSH private key to the composer SSH dir and encrypts it at rest. Admin only.",
		Tags:        []string{"system"},
		Errors:      errsAdminMutation,
	}, h.AddSSHKey)

	huma.Register(api, huma.Operation{
		OperationID: "deleteSSHKey", Method: http.MethodDelete,
		Path:        "/api/v1/system/config/ssh-keys/{name}",
		Summary:     "Delete an SSH key file",
		Description: "Removes the SSH key file (and its .pub counterpart if present). Admin only.",
		Tags:        []string{"system"},
		Errors:      errsAdminMutation,
	}, h.DeleteSSHKey)
}

func (h *SystemHandler) Info(ctx context.Context, input *struct{}) (*dto.SystemInfoOutput, error) {
	if h.docker == nil {
		return nil, huma.Error503ServiceUnavailable("docker not available")
	}

	info, err := h.docker.Info(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.SystemInfoOutput{}
	out.Body.Docker.Version = info.ServerVersion
	out.Body.Docker.APIVersion = info.Driver
	out.Body.Docker.Runtime = h.docker.Runtime()
	out.Body.Docker.OS = info.OperatingSystem
	out.Body.Docker.Arch = info.Architecture
	out.Body.Docker.Containers = info.Containers
	out.Body.Docker.Images = info.Images
	return out, nil
}

func (h *SystemHandler) Version(ctx context.Context, input *struct{}) (*dto.VersionOutput, error) {
	out := &dto.VersionOutput{}
	out.Body.Version = composer.Version
	out.Body.GoVersion = runtime.Version()
	out.Body.OS = runtime.GOOS
	out.Body.Arch = runtime.GOARCH
	out.Body.Uptime = time.Since(startTime).Round(time.Second).String()
	return out, nil
}

// --- Global Config ---

func (h *SystemHandler) Config(ctx context.Context, input *struct{}) (*dto.ConfigOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	out := &dto.ConfigOutput{}

	// SSH keys (never nil -- empty array for JSON serialization)
	out.Body.SSHKeys = listSSHKeys()
	if out.Body.SSHKeys == nil {
		out.Body.SSHKeys = []dto.SSHKeyInfo{}
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

func (h *SystemHandler) UpdateAgeKey(ctx context.Context, input *dto.UpdateAgeKeyInput) (*dto.UpdateAgeKeyOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	out := &dto.UpdateAgeKeyOutput{}

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
		return nil, serverError(ctx, err)
	}

	out.Body.PublicKey = pubKey
	out.Body.Saved = true
	return out, nil
}

func (h *SystemHandler) GenerateAgeKey(ctx context.Context, input *struct{}) (*dto.GenerateAgeKeyOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}

	privKey, pubKey, err := sops.GenerateAgeKey()
	if err != nil {
		return nil, serverError(ctx, err)
	}

	if err := sops.SaveAgeKey(h.dataDir, privKey, pubKey); err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.GenerateAgeKeyOutput{}
	out.Body.PrivateKey = privKey
	out.Body.PublicKey = pubKey
	return out, nil
}

func (h *SystemHandler) AddSSHKey(ctx context.Context, input *dto.AddSSHKeyInput) (*dto.AddSSHKeyOutput, error) {
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
		return nil, serverError(ctx, err)
	}

	// Encrypt at rest using our AES-256-GCM layer
	crypto.EncryptFile(keyPath)

	out := &dto.AddSSHKeyOutput{}
	out.Body.Path = keyPath
	out.Body.Encrypted = true
	return out, nil
}

func (h *SystemHandler) DeleteSSHKey(ctx context.Context, input *dto.DeleteSSHKeyInput) (*struct{}, error) {
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

func (h *SystemHandler) GetGitToken(ctx context.Context, input *struct{}) (*dto.GitTokenOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	out := &dto.GitTokenOutput{}
	tokenPath := filepath.Join(h.dataDir, "git-token")
	if data, err := crypto.DecryptFile(tokenPath); err == nil && data != "" {
		out.Body.Configured = true
		if len(data) > 8 {
			out.Body.Preview = data[:8] + "..."
		}
	}
	return out, nil
}

func (h *SystemHandler) UpdateGitToken(ctx context.Context, input *dto.UpdateGitTokenInput) (*dto.GitTokenOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	tokenPath := filepath.Join(h.dataDir, "git-token")
	token := strings.TrimSpace(input.Body.Token)

	if token == "" {
		os.Remove(tokenPath)
		return &dto.GitTokenOutput{}, nil
	}

	if err := crypto.WriteEncrypted(tokenPath, token); err != nil {
		return nil, serverError(ctx, err)
	}

	out := &dto.GitTokenOutput{}
	out.Body.Configured = true
	if len(token) > 8 {
		out.Body.Preview = token[:8] + "..."
	}
	return out, nil
}

// --- helpers ---

func listSSHKeys() []dto.SSHKeyInfo {
	var keys []dto.SSHKeyInfo
	seen := make(map[string]bool) // resolved path -> already listed

	for _, dir := range []string{
		filepath.Join(os.Getenv("HOME"), ".ssh"),
		"/home/composer/.ssh",
	} {
		// Resolve symlinks and normalize to deduplicate when $HOME/.ssh == /home/composer/.ssh
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			resolved = dir
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name == "known_hosts" || name == "config" || name == "authorized_keys" ||
				strings.HasSuffix(name, ".pub") {
				continue
			}
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			encrypted := err == nil && len(data) > 4 && string(data[:4]) == "enc:"
			keys = append(keys, dto.SSHKeyInfo{
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
