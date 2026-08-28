package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeriveKeyFileWinsOverEnv pins the precedence fix: with BOTH a key file
// and the env var set, the key file (the explicit UI save) wins.
func TestDeriveKeyFileWinsOverEnv(t *testing.T) {
	resetKey()
	t.Setenv("COMPOSER_ENCRYPTION_KEY", "env-key-value")
	tmp := t.TempDir()
	t.Setenv("COMPOSER_DATA_DIR", tmp)

	fileKey := strings.Repeat("a", 64) // 64 hex chars, as written by rotation
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "encryption.key"), []byte(fileKey), 0600))

	key, err := deriveKey()
	require.NoError(t, err)

	want := sha256.Sum256([]byte(fileKey))
	assert.Equal(t, want[:], key, "key file must win over the env var")

	envWant := sha256.Sum256([]byte("env-key-value"))
	assert.NotEqual(t, envWant[:], key, "env-derived key must NOT be used")
}

func TestKeyFromHex(t *testing.T) {
	keyHex := strings.Repeat("0123456789abcdef", 4)
	key, err := KeyFromHex(keyHex)
	require.NoError(t, err)
	assert.Len(t, key, 32)

	// Must match what deriveKey derives from a key file with this content:
	// SHA-256 of the hex string's bytes.
	want := sha256.Sum256([]byte(keyHex))
	assert.Equal(t, want[:], key)

	// Invalid lengths
	for _, bad := range []string{"", "abc", strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		_, err := KeyFromHex(bad)
		assert.ErrorIs(t, err, ErrInvalidKey, "len=%d", len(bad))
	}
	// Invalid hex chars
	_, err = KeyFromHex(strings.Repeat("z", 64))
	assert.ErrorIs(t, err, ErrInvalidKey)
}

func TestGenerateKeyHex(t *testing.T) {
	hex1, err := GenerateKeyHex()
	require.NoError(t, err)
	hex2, err := GenerateKeyHex()
	require.NoError(t, err)
	assert.Len(t, hex1, 64)
	_, err = hex.DecodeString(hex1)
	assert.NoError(t, err)
	assert.NotEqual(t, hex1, hex2, "two generated keys must differ")
}

func TestEncryptWithDecryptWith(t *testing.T) {
	oldKey, err := KeyFromHex(strings.Repeat("ab", 32))
	require.NoError(t, err)

	enc, err := EncryptWith(oldKey, `{"token":"ghp_abc"}`)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(enc, "enc:"))

	plain, err := DecryptWith(oldKey, enc)
	require.NoError(t, err)
	assert.Equal(t, `{"token":"ghp_abc"}`, plain)

	// Wrong key must fail (GCM auth tag)
	wrongKey, err := KeyFromHex(strings.Repeat("cd", 32))
	require.NoError(t, err)
	_, err = DecryptWith(wrongKey, enc)
	assert.Error(t, err)
}

// TestReencryptValueRoundTrip: encrypt with old, re-encrypt to new, decrypt
// with new == original, and the re-encrypted blob must FAIL under the old key.
func TestReencryptValueRoundTrip(t *testing.T) {
	oldKey, err := KeyFromHex(strings.Repeat("11", 32))
	require.NoError(t, err)
	newKey, err := KeyFromHex(strings.Repeat("22", 32))
	require.NoError(t, err)

	original := "registry-secret-1"
	encOld, err := EncryptWith(oldKey, original)
	require.NoError(t, err)

	reenc, err := ReencryptValue(oldKey, newKey, encOld)
	require.NoError(t, err)
	assert.NotEqual(t, encOld, reenc, "re-encrypted blob must differ (new nonce+key)")
	assert.True(t, strings.HasPrefix(reenc, "enc:"))

	plain, err := DecryptWith(newKey, reenc)
	require.NoError(t, err)
	assert.Equal(t, original, plain)

	_, err = DecryptWith(oldKey, reenc)
	assert.Error(t, err, "old key must no longer decrypt the re-encrypted value")
}

// TestReencryptValuePassthrough: non-enc: values (and empty) pass through
// unchanged.
func TestReencryptValuePassthrough(t *testing.T) {
	oldKey, err := KeyFromHex(strings.Repeat("11", 32))
	require.NoError(t, err)
	newKey, err := KeyFromHex(strings.Repeat("22", 32))
	require.NoError(t, err)

	plain := `{"plaintext":"no-enc-prefix"}`
	out, err := ReencryptValue(oldKey, newKey, plain)
	require.NoError(t, err)
	assert.Equal(t, plain, out)

	out, err = ReencryptValue(oldKey, newKey, "")
	require.NoError(t, err)
	assert.Equal(t, "", out)
}

func TestReencryptValueBadCiphertext(t *testing.T) {
	oldKey, err := KeyFromHex(strings.Repeat("11", 32))
	require.NoError(t, err)
	newKey, err := KeyFromHex(strings.Repeat("22", 32))
	require.NoError(t, err)

	// "enc:" prefix but undecryptable under the old key: rotation must fail
	// loudly (the caller rolls back).
	_, err = ReencryptValue(oldKey, newKey, "enc:AAAA")
	assert.Error(t, err)
}
