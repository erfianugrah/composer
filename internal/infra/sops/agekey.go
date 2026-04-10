package sops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
)

// GenerateAgeKey creates a new age X25519 identity (private key) and returns
// the private key string and public key (recipient) string.
func GenerateAgeKey() (privateKey, publicKey string, err error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", fmt.Errorf("generating age key: %w", err)
	}
	return identity.String(), identity.Recipient().String(), nil
}

// LoadGlobalAgeKey resolves the global age private key from (in priority order):
// 1. COMPOSER_SOPS_AGE_KEY env var (inline private key)
// 2. SOPS_AGE_KEY env var (standard SOPS convention)
// 3. SOPS_AGE_KEYS env var (multi-line format with comments, user convention)
// 4. COMPOSER_SOPS_AGE_KEY_FILE env var (path to key file)
// 5. SOPS_AGE_KEY_FILE env var (standard SOPS convention)
// 6. COMPOSER_DATA_DIR/age.key
// 7. ~/.config/sops/age/keys.txt (standard SOPS location)
//
// Returns the private key string or empty string if none found.
// Never auto-generates -- the user must provide their key or explicitly generate one.
func LoadGlobalAgeKey(dataDir string) string {
	// 1. Composer-specific env
	if key := os.Getenv("COMPOSER_SOPS_AGE_KEY"); key != "" {
		return extractPrivateKey(key)
	}

	// 2. Standard SOPS env
	if key := os.Getenv("SOPS_AGE_KEY"); key != "" {
		return extractPrivateKey(key)
	}

	// 3. Multi-line SOPS_AGE_KEYS (user convention -- contains comments + key)
	if keys := os.Getenv("SOPS_AGE_KEYS"); keys != "" {
		// Unescape literal \n sequences (common in env vars from .env files / docker)
		keys = strings.ReplaceAll(keys, `\n`, "\n")
		return extractPrivateKey(keys)
	}

	// 4. Composer key file env
	if keyFile := os.Getenv("COMPOSER_SOPS_AGE_KEY_FILE"); keyFile != "" {
		if data, err := os.ReadFile(keyFile); err == nil {
			return extractPrivateKey(string(data))
		}
	}

	// 5. Standard SOPS key file env
	if keyFile := os.Getenv("SOPS_AGE_KEY_FILE"); keyFile != "" {
		if data, err := os.ReadFile(keyFile); err == nil {
			return extractPrivateKey(string(data))
		}
	}

	// 6. Composer data dir
	if dataDir == "" {
		dataDir = os.Getenv("COMPOSER_DATA_DIR")
	}
	if dataDir == "" {
		dataDir = "/opt/composer"
	}
	composerKeyFile := filepath.Join(dataDir, "age.key")
	if data, err := os.ReadFile(composerKeyFile); err == nil {
		return extractPrivateKey(string(data))
	}

	// 7. Standard SOPS location
	home, _ := os.UserHomeDir()
	if home != "" {
		sopsKeyFile := filepath.Join(home, ".config", "sops", "age", "keys.txt")
		if data, err := os.ReadFile(sopsKeyFile); err == nil {
			return extractPrivateKey(string(data))
		}
	}

	return "" // no key found
}

// SaveAgeKey writes an age private key to the data directory.
// Used when the user explicitly generates a new key via the API.
func SaveAgeKey(dataDir, privateKey, publicKey string) error {
	if dataDir == "" {
		dataDir = os.Getenv("COMPOSER_DATA_DIR")
	}
	if dataDir == "" {
		dataDir = "/opt/composer"
	}
	keyFile := filepath.Join(dataDir, "age.key")
	os.MkdirAll(dataDir, 0700)

	content := fmt.Sprintf("# created by Composer -- age private key for SOPS\n# public key: %s\n%s\n", publicKey, privateKey)
	return os.WriteFile(keyFile, []byte(content), 0600)
}

// ResolveAgeKey returns the age key to use for a specific stack.
// Priority: per-stack key > global key.
func ResolveAgeKey(perStackKey, dataDir string) string {
	if perStackKey != "" {
		return extractPrivateKey(perStackKey)
	}
	return LoadGlobalAgeKey(dataDir)
}

// extractPrivateKey extracts the first AGE-SECRET-KEY line from key content.
// Key content can contain comments (# lines), public key annotations, and
// literal \n escape sequences (from env vars).
func extractPrivateKey(content string) string {
	// Unescape literal \n if present
	content = strings.ReplaceAll(content, `\n`, "\n")

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			return line
		}
	}
	// If no AGE-SECRET-KEY line found, return trimmed content as-is
	// (might be just the raw key without comments)
	trimmed := strings.TrimSpace(content)
	if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
		return trimmed
	}
	return ""
}
