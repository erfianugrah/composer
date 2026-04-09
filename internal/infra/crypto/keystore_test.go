package crypto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestEnv(t *testing.T) {
	t.Helper()
	resetKey()
	os.Setenv("COMPOSER_ENCRYPTION_KEY", "test-key-for-keystore-tests")
	t.Cleanup(func() {
		os.Unsetenv("COMPOSER_ENCRYPTION_KEY")
		resetKey()
	})
}

func TestEncryptFile(t *testing.T) {
	setupTestEnv(t)

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "id_ed25519")
	content := "-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key-content\n-----END OPENSSH PRIVATE KEY-----\n"
	require.NoError(t, os.WriteFile(keyFile, []byte(content), 0600))

	// Encrypt
	require.NoError(t, EncryptFile(keyFile))

	// Verify file is now encrypted
	data, err := os.ReadFile(keyFile)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(data), "enc:"), "file should start with enc: prefix")
	assert.NotContains(t, string(data), "PRIVATE KEY", "plaintext should not be in file")

	// Encrypt again (idempotent)
	require.NoError(t, EncryptFile(keyFile))
	data2, _ := os.ReadFile(keyFile)
	assert.Equal(t, string(data), string(data2), "double-encrypt should be idempotent")
}

func TestDecryptFile(t *testing.T) {
	setupTestEnv(t)

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "id_rsa")
	content := "ssh-rsa AAAA... fake key content"
	require.NoError(t, os.WriteFile(keyFile, []byte(content), 0600))

	// Decrypt unencrypted file returns content as-is
	dec, err := DecryptFile(keyFile)
	require.NoError(t, err)
	assert.Equal(t, content, dec)

	// Encrypt then decrypt
	require.NoError(t, EncryptFile(keyFile))
	dec, err = DecryptFile(keyFile)
	require.NoError(t, err)
	assert.Equal(t, content, dec)
}

func TestWriteEncrypted(t *testing.T) {
	setupTestEnv(t)

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "secret_key")
	content := "super-secret-key-material"

	require.NoError(t, WriteEncrypted(keyFile, content))

	// Raw file should be encrypted
	raw, err := os.ReadFile(keyFile)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(raw), "enc:"))

	// DecryptFile should return original
	dec, err := DecryptFile(keyFile)
	require.NoError(t, err)
	assert.Equal(t, content, dec)

	// File permissions should be 0600
	info, err := os.Stat(keyFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestEncryptSSHKeys(t *testing.T) {
	setupTestEnv(t)

	sshDir := t.TempDir()

	// Create test key files
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("private-key-1"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_rsa"), []byte("private-key-2"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), []byte("public-key"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte("github.com ssh-rsa AAAA"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host *\n"), 0644))

	n, err := EncryptSSHKeys(sshDir)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "should encrypt 2 private key files")

	// Private keys should be encrypted
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		data, _ := os.ReadFile(filepath.Join(sshDir, name))
		assert.True(t, strings.HasPrefix(string(data), "enc:"), "%s should be encrypted", name)
	}

	// Public key, known_hosts, config should be untouched
	pubData, _ := os.ReadFile(filepath.Join(sshDir, "id_ed25519.pub"))
	assert.Equal(t, "public-key", string(pubData))

	khData, _ := os.ReadFile(filepath.Join(sshDir, "known_hosts"))
	assert.Equal(t, "github.com ssh-rsa AAAA", string(khData))

	configData, _ := os.ReadFile(filepath.Join(sshDir, "config"))
	assert.Equal(t, "Host *\n", string(configData))
}

func TestEncryptSSHKeys_Idempotent(t *testing.T) {
	setupTestEnv(t)

	sshDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("private-key"), 0600))

	// First pass
	n1, err := EncryptSSHKeys(sshDir)
	require.NoError(t, err)
	assert.Equal(t, 1, n1)

	// Second pass (already encrypted)
	n2, err := EncryptSSHKeys(sshDir)
	require.NoError(t, err)
	assert.Equal(t, 0, n2, "should not re-encrypt already encrypted files")
}

func TestEncryptSSHKeys_NonexistentDir(t *testing.T) {
	n, err := EncryptSSHKeys("/nonexistent/path/ssh")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestEncryptSSHKeys_EmptyDir(t *testing.T) {
	sshDir := t.TempDir()
	n, err := EncryptSSHKeys(sshDir)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestDecryptFile_NonexistentFile(t *testing.T) {
	_, err := DecryptFile("/nonexistent/file")
	assert.Error(t, err)
}
