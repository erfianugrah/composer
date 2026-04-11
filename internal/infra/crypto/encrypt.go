package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var (
	encKey     []byte
	encKeyOnce sync.Once
	encKeyErr  error
)

// ErrNoKey is returned when no encryption key could be resolved.
var ErrNoKey = errors.New("no encryption key available")

// deriveKey resolves the encryption key from (in priority order):
// 1. COMPOSER_ENCRYPTION_KEY env var
// 2. Persistent key file at COMPOSER_DATA_DIR/encryption.key
// 3. Auto-generate a new key and save it to the key file
func deriveKey() ([]byte, error) {
	// 1. Env var takes priority (explicit override)
	if raw := os.Getenv("COMPOSER_ENCRYPTION_KEY"); raw != "" {
		h := sha256.Sum256([]byte(raw))
		return h[:], nil
	}

	// 2. Try persistent key file
	dataDir := os.Getenv("COMPOSER_DATA_DIR")
	if dataDir == "" {
		dataDir = "/opt/composer"
	}
	keyFile := filepath.Join(dataDir, "encryption.key")

	if data, err := os.ReadFile(keyFile); err == nil && len(data) >= 32 {
		// Key file exists and has content -- use it
		h := sha256.Sum256(data)
		return h[:], nil
	}

	// 3. Auto-generate and persist
	var buf [32]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		return nil, fmt.Errorf("generating encryption key: %w", err)
	}
	keyHex := hex.EncodeToString(buf[:])

	// Ensure directory exists
	os.MkdirAll(dataDir, 0700)
	if err := os.WriteFile(keyFile, []byte(keyHex), 0600); err != nil {
		// Can't persist -- use the key in memory only this run
		// (won't be able to decrypt on restart, but at least this run works)
		fmt.Fprintf(os.Stderr, "WARNING: could not persist encryption key to %s: %v — encrypted data will be lost on restart\n", keyFile, err)
		h := sha256.Sum256([]byte(keyHex))
		return h[:], nil
	}

	h := sha256.Sum256([]byte(keyHex))
	return h[:], nil
}

func getKey() ([]byte, error) {
	encKeyOnce.Do(func() {
		encKey, encKeyErr = deriveKey()
	})
	return encKey, encKeyErr
}

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns base64-encoded ciphertext (nonce prepended).
// Key is auto-generated and persisted if not explicitly set.
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	key, err := getKey()
	if err != nil {
		return "", fmt.Errorf("encryption key unavailable: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// EncryptFile reads a plaintext file, encrypts its contents, and writes
// the encrypted data back with an "enc:" prefix. If the file is already
// encrypted (starts with "enc:"), it is left unchanged.
func EncryptFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}
	content := string(data)

	// Already encrypted
	if len(content) >= 4 && content[:4] == "enc:" {
		return nil
	}

	encrypted, err := Encrypt(content)
	if err != nil {
		return fmt.Errorf("encrypting: %w", err)
	}

	return os.WriteFile(path, []byte(encrypted), 0600)
}

// DecryptFile reads an encrypted file and returns the plaintext contents.
// If the file is not encrypted (no "enc:" prefix), returns contents as-is.
func DecryptFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	return Decrypt(string(data))
}

// WriteEncrypted writes content to a file, encrypting it first.
// The file is written with mode 0600 (owner read/write only).
func WriteEncrypted(path, content string) error {
	encrypted, err := Encrypt(content)
	if err != nil {
		return fmt.Errorf("encrypting: %w", err)
	}
	return os.WriteFile(path, []byte(encrypted), 0600)
}

// Decrypt decrypts a value produced by Encrypt.
// If the value doesn't have the "enc:" prefix, returns it unchanged (unencrypted data).
func Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	// Not encrypted -- return as-is (backwards compat with pre-encryption data)
	if len(encoded) < 4 || encoded[:4] != "enc:" {
		return encoded, nil
	}

	key, err := getKey()
	if err != nil {
		return "", fmt.Errorf("decryption requires encryption key: %w", err)
	}

	data, err := base64.StdEncoding.DecodeString(encoded[4:])
	if err != nil {
		return "", fmt.Errorf("decoding: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}

	return string(plaintext), nil
}
