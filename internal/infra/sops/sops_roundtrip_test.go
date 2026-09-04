package sops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncryptDecryptRestoreRoundtrip exercises the real sops binary end to
// end: encrypt with a fresh age key, decrypt in place (backup + plaintext),
// restore from the backup, and prove the restored file is byte-identical to
// the original ciphertext with the backup removed. This is the exact file
// sequence the 2026-09-04 edge-services incident failed at - the restore half
// is what silently no-opped when the env file path resolved to the wrong
// location.
func TestEncryptDecryptRestoreRoundtrip(t *testing.T) {
	if !IsAvailable() {
		t.Skip("sops binary not in PATH")
	}

	key, _, err := GenerateAgeKey()
	require.NoError(t, err, "generating age key")

	plaintext := []byte("MEMLEDGER_TOKEN=roundtrip-test-value\nEMAIL=roundtrip@example.com\n")
	cipher, err := Encrypt(plaintext, "dotenv", key)
	require.NoError(t, err, "sops encrypt")
	require.True(t, IsSopsEncrypted(cipher), "expected SOPS ciphertext")

	envPath := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(envPath, cipher, 0600))

	dec, err := DecryptEnvFile(envPath, key)
	require.NoError(t, err, "decrypt")
	assert.True(t, dec, "env should decrypt")

	got, err := os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, string(plaintext), string(got), "decrypted content should be plaintext")

	backup, err := os.ReadFile(envPath + ".sops")
	require.NoError(t, err, ".sops backup must exist after decrypt")
	assert.Equal(t, cipher, backup, "backup must be the original ciphertext")

	restored, err := ReEncryptEnvFile(envPath)
	require.NoError(t, err, "restore")
	assert.True(t, restored, "restore should report success")

	got, err = os.ReadFile(envPath)
	require.NoError(t, err)
	assert.Equal(t, cipher, got, "restored file must be byte-identical to the original ciphertext")

	_, err = os.Stat(envPath + ".sops")
	assert.True(t, os.IsNotExist(err), "backup must be removed after restore")
}
