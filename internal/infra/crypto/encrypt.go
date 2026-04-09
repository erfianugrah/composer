package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	encKey     []byte
	encKeyOnce sync.Once
	encKeyErr  error
)

// ErrNoKey is returned when COMPOSER_ENCRYPTION_KEY is not set.
var ErrNoKey = errors.New("COMPOSER_ENCRYPTION_KEY not set -- credentials will not be encrypted")

// deriveKey derives a 32-byte AES-256 key from the env var using SHA-256.
func deriveKey() ([]byte, error) {
	raw := os.Getenv("COMPOSER_ENCRYPTION_KEY")
	if raw == "" {
		return nil, ErrNoKey
	}
	h := sha256.Sum256([]byte(raw))
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
// If COMPOSER_ENCRYPTION_KEY is not set, returns plaintext unchanged (backwards compat).
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	key, err := getKey()
	if err != nil {
		// No key configured -- store unencrypted (backwards compatible)
		return plaintext, nil
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
		return "", fmt.Errorf("decryption requires COMPOSER_ENCRYPTION_KEY: %w", err)
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
