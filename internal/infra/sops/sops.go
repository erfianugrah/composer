// Package sops provides SOPS-encrypted file detection and decryption.
// Decryption shells out to the `sops` binary (bundled in the Docker image)
// rather than importing the heavy SOPS Go library with all its cloud KMS deps.
package sops

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsAvailable returns true if the sops binary is in PATH.
func IsAvailable() bool {
	_, err := exec.LookPath("sops")
	return err == nil
}

// IsSopsEncrypted checks if file content appears to be SOPS-encrypted.
// Detects SOPS markers for dotenv, YAML, and JSON formats.
func IsSopsEncrypted(data []byte) bool {
	s := string(data)

	// Dotenv format: contains sops_version= and ENC[ values
	if strings.Contains(s, "sops_version=") {
		return true
	}

	// YAML format: has "sops:" key
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "sops:" || strings.HasPrefix(trimmed, "sops:") {
			return true
		}
	}

	// JSON format: has "sops" key (simple check)
	if strings.Contains(s, `"sops"`) && strings.Contains(s, `"version"`) && strings.Contains(s, `"mac"`) {
		return true
	}

	return false
}

// IsSopsEncryptedFile checks if a file on disk is SOPS-encrypted.
func IsSopsEncryptedFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return IsSopsEncrypted(data)
}

// DetectFormat returns the SOPS format string based on file extension.
func DetectFormat(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".ini":
		return "ini"
	case ".env":
		return "dotenv"
	default:
		// .env files often have no extension -- check the basename
		base := strings.ToLower(filepath.Base(filename))
		if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
			return "dotenv"
		}
		return "binary"
	}
}

// Decrypt decrypts a SOPS-encrypted file using the sops binary.
// The ageKey parameter is the age private key string (AGE-SECRET-KEY-...).
// If ageKey is empty, sops uses its default key resolution
// (SOPS_AGE_KEY env, SOPS_AGE_KEY_FILE, ~/.config/sops/age/keys.txt).
func Decrypt(filePath string, ageKey string) ([]byte, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("sops binary not found in PATH -- install sops or use the Docker image which bundles it")
	}

	args := []string{"--decrypt", filePath}
	cmd := exec.Command("sops", args...)

	// Set age key via environment if provided
	if ageKey != "" {
		cmd.Env = append(os.Environ(), "SOPS_AGE_KEY="+ageKey)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sops decrypt failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}

	return stdout.Bytes(), nil
}

// DecryptData decrypts SOPS-encrypted data (not from a file).
// Writes to a temp file, decrypts, then cleans up.
func DecryptData(data []byte, format, ageKey string) ([]byte, error) {
	if !IsAvailable() {
		return nil, fmt.Errorf("sops binary not found in PATH")
	}

	// sops needs a file on disk with the right extension for format detection
	ext := ".env"
	switch format {
	case "yaml":
		ext = ".yaml"
	case "json":
		ext = ".json"
	case "ini":
		ext = ".ini"
	}

	tmp, err := os.CreateTemp("", "sops-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing temp file: %w", err)
	}
	tmp.Close()

	return Decrypt(tmpPath, ageKey)
}

// DecryptInMemory decrypts a SOPS-encrypted file and returns the plaintext
// without writing anything to disk. Used for displaying decrypted content in the UI.
// Returns the original data unchanged if not SOPS-encrypted.
func DecryptInMemory(filePath, ageKey string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if !IsSopsEncrypted(data) {
		return data, nil
	}
	return Decrypt(filePath, ageKey)
}

// DecryptEnvFile decrypts a SOPS-encrypted .env file in a stack directory.
// Saves the encrypted original as .env.sops before writing decrypted .env.
// This allows ReEncryptEnvFile to restore the encrypted version after deploy.
// Returns false if the file is not SOPS-encrypted or doesn't exist.
func DecryptEnvFile(stackDir, ageKey string) (decrypted bool, err error) {
	envPath := filepath.Join(stackDir, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return false, nil // no .env file
	}

	if !IsSopsEncrypted(data) {
		return false, nil
	}

	plaintext, err := Decrypt(envPath, ageKey)
	if err != nil {
		return false, fmt.Errorf("decrypting .env: %w", err)
	}

	// Save encrypted original so we can restore it later
	sopsPath := envPath + ".sops"
	if err := os.WriteFile(sopsPath, data, 0600); err != nil {
		return false, fmt.Errorf("saving .env.sops backup: %w", err)
	}

	if err := os.WriteFile(envPath, plaintext, 0600); err != nil {
		return false, fmt.Errorf("writing decrypted .env: %w", err)
	}

	return true, nil
}

// ReEncryptEnvFile restores the SOPS-encrypted .env from the .env.sops backup
// created by DecryptEnvFile. Call this after deploy completes so the .env is
// never left decrypted at rest.
func ReEncryptEnvFile(stackDir string) error {
	envPath := filepath.Join(stackDir, ".env")
	sopsPath := envPath + ".sops"

	sopsData, err := os.ReadFile(sopsPath)
	if err != nil {
		return nil // no .sops backup, nothing to restore (file wasn't SOPS-encrypted)
	}

	if err := os.WriteFile(envPath, sopsData, 0600); err != nil {
		return fmt.Errorf("restoring encrypted .env: %w", err)
	}

	os.Remove(sopsPath)
	return nil
}

// DecryptComposeSecrets decrypts a SOPS-encrypted compose file in-place.
// Saves the encrypted original as {file}.sops for restoration after deploy.
func DecryptComposeSecrets(composePath, ageKey string) (decrypted bool, err error) {
	data, err := os.ReadFile(composePath)
	if err != nil {
		return false, nil
	}

	if !IsSopsEncrypted(data) {
		return false, nil
	}

	plaintext, err := Decrypt(composePath, ageKey)
	if err != nil {
		return false, fmt.Errorf("decrypting compose file: %w", err)
	}

	// Save encrypted original
	sopsPath := composePath + ".sops"
	if err := os.WriteFile(sopsPath, data, 0600); err != nil {
		return false, fmt.Errorf("saving compose.sops backup: %w", err)
	}

	if err := os.WriteFile(composePath, plaintext, 0600); err != nil {
		return false, fmt.Errorf("writing decrypted compose: %w", err)
	}

	return true, nil
}

// ReEncryptComposeSecrets restores a SOPS-encrypted compose file from its
// .sops backup. Call after deploy completes.
func ReEncryptComposeSecrets(composePath string) error {
	sopsPath := composePath + ".sops"

	sopsData, err := os.ReadFile(sopsPath)
	if err != nil {
		return nil // no backup
	}

	if err := os.WriteFile(composePath, sopsData, 0600); err != nil {
		return fmt.Errorf("restoring encrypted compose: %w", err)
	}

	os.Remove(sopsPath)
	return nil
}
