package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var (
	// keyMu guards encKey/encKeyErr: rotation swaps the cached key at
	// runtime (SetKeyForRotation), so the cache cannot be once-only.
	keyMu      sync.RWMutex
	encKey     []byte
	encKeyOnce sync.Once
	encKeyErr  error
)

// ErrNoKey is returned when no encryption key could be resolved.
var ErrNoKey = errors.New("no encryption key available")

// encKeyFile is the data-dir key file written by an explicit UI action (the
// encryption-key rotation endpoint). Present and non-empty, the file wins
// over env vars: it is the newest expression of key intent.
const encKeyFile = "encryption.key"

// deriveKey resolves the encryption key from (in priority order):
//  1. COMPOSER_DATA_DIR/encryption.key (non-empty, >= 32 bytes) -- the key
//     saved via the UI. It wins over env vars: the file was written by an
//     explicit user action (save or rotate) and is the newest expression of
//     key intent.
//  2. COMPOSER_ENCRYPTION_KEY env var
//  3. Auto-generate a new key and save it to the key file
func deriveKey() ([]byte, error) {
	// 1. Data-dir key file (explicit UI save) wins over env vars
	dataDir := os.Getenv("COMPOSER_DATA_DIR")
	if dataDir == "" {
		dataDir = "/opt/composer"
	}
	keyFile := filepath.Join(dataDir, encKeyFile)

	if data, err := os.ReadFile(keyFile); err == nil && len(data) >= 32 {
		// Key file exists and has content -- use it
		h := sha256.Sum256(data)
		return h[:], nil
	}

	// 2. Env var (bootstrap override for installs without a saved key)
	if raw := os.Getenv("COMPOSER_ENCRYPTION_KEY"); raw != "" {
		h := sha256.Sum256([]byte(raw))
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
	keyMu.RLock()
	if encKey != nil || encKeyErr != nil {
		key, err := encKey, encKeyErr
		keyMu.RUnlock()
		return key, err
	}
	keyMu.RUnlock()

	keyMu.Lock()
	defer keyMu.Unlock()
	encKeyOnce.Do(func() {
		encKey, encKeyErr = deriveKey()
	})
	return encKey, encKeyErr
}

// CurrentKey returns a copy of the key currently cached in the singleton.
// Rotation captures this before re-encrypting: it is the key every stored
// "enc:" value is currently decryptable with.
func CurrentKey() ([]byte, error) {
	key, err := getKey()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), key...), nil
}

// SetKeyForRotation atomically swaps the singleton key. Called by the
// app-layer rotation service only AFTER its transaction re-encrypting every
// stored value under the new key has committed -- never before, so a
// mid-flight request can never decrypt an unrewritten row with the new key.
func SetKeyForRotation(key []byte) {
	keyMu.Lock()
	encKey = append([]byte(nil), key...)
	encKeyErr = nil
	keyMu.Unlock()
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
	return EncryptWith(key, plaintext)
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
	return DecryptWith(key, encoded)
}
