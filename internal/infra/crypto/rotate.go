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
)

// ErrInvalidKey is returned when a key string is not a valid 64-hex-char
// (32-byte) encryption key.
var ErrInvalidKey = errors.New("invalid encryption key: must be 64 hex characters (32 bytes)")

// KeyFromHex derives the effective 32-byte AES key from a 64-hex-char key
// string, exactly the way deriveKey does for the persistent key file
// (SHA-256 of the hex string's bytes). Rotation writes newKeyHex to the key
// file and installs KeyFromHex(newKeyHex) into the singleton, so a process
// restart re-derives the identical key from the file it just read back.
func KeyFromHex(keyHex string) ([]byte, error) {
	if len(keyHex) != 64 {
		return nil, ErrInvalidKey
	}
	if _, err := hex.DecodeString(keyHex); err != nil {
		return nil, ErrInvalidKey
	}
	h := sha256.Sum256([]byte(keyHex))
	return h[:], nil
}

// GenerateKeyHex generates a fresh 32-byte random key and returns it as a
// 64-hex-char string.
func GenerateKeyHex() (string, error) {
	var buf [32]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		return "", fmt.Errorf("generating encryption key: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// EncryptWith encrypts plaintext with the given key using AES-256-GCM and
// returns base64-encoded ciphertext with the "enc:" prefix (nonce prepended).
// This is the key-parameterized core of Encrypt; rotation drives it with two
// explicit keys at once instead of going through the singleton.
func EncryptWith(key []byte, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
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

// DecryptWith decrypts a value produced by EncryptWith using the given key.
// Values without the "enc:" prefix pass through unchanged (backwards compat
// with pre-encryption data).
func DecryptWith(key []byte, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	// Not encrypted -- return as-is (backwards compat with pre-encryption data)
	if len(encoded) < 4 || encoded[:4] != "enc:" {
		return encoded, nil
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

// ReencryptValue re-encrypts one stored value from oldKey to newKey
// (DecryptWith old -> EncryptWith new). Values without the "enc:" prefix
// pass through unchanged; empty stays empty. A value that cannot be
// decrypted with oldKey is an error: the caller must treat it as a rotation
// failure and roll back.
func ReencryptValue(oldKey, newKey []byte, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) < 4 || value[:4] != "enc:" {
		return value, nil
	}
	plain, err := DecryptWith(oldKey, value)
	if err != nil {
		return "", fmt.Errorf("reencrypt: %w", err)
	}
	return EncryptWith(newKey, plain)
}
