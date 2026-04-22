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
	content := "-----BEGIN RSA PRIVATE KEY-----\nfake-body\n-----END RSA PRIVATE KEY-----\n"
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

// TestEncryptSSHKeys exercises the realistic layout of an SSH directory:
// two valid private keys plus a mix of files that must be left alone
// (public keys, known_hosts and its .old backup, config, authorized_keys,
// and — critically — arbitrary non-key text files a user may have dropped
// in the directory).
func TestEncryptSSHKeys(t *testing.T) {
	setupTestEnv(t)

	sshDir := t.TempDir()

	// Eligible: private keys with recognised BEGIN markers
	ed25519Key := "-----BEGIN OPENSSH PRIVATE KEY-----\nbody\n-----END OPENSSH PRIVATE KEY-----\n"
	rsaKey := "-----BEGIN RSA PRIVATE KEY-----\nbody\n-----END RSA PRIVATE KEY-----\n"
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte(ed25519Key), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_rsa"), []byte(rsaKey), 0600))

	// Ineligible: all the non-key noise an SSH dir routinely accumulates
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), []byte("ssh-ed25519 AAAA"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte("github.com ssh-rsa AAAA"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "known_hosts.old"), []byte("legacy entries"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host *\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "authorized_keys"), []byte("ssh-ed25519 AAAA"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "notes.txt"), []byte("random note, not a key"), 0600))

	n, err := EncryptSSHKeys(sshDir)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "only the two real private keys should be encrypted")

	// Private keys: encrypted
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		data, _ := os.ReadFile(filepath.Join(sshDir, name))
		assert.True(t, strings.HasPrefix(string(data), "enc:"), "%s should be encrypted", name)
	}

	// Everything else: untouched bit-for-bit
	cases := map[string]string{
		"id_ed25519.pub":   "ssh-ed25519 AAAA",
		"known_hosts":      "github.com ssh-rsa AAAA",
		"known_hosts.old":  "legacy entries",
		"config":           "Host *\n",
		"authorized_keys":  "ssh-ed25519 AAAA",
		"notes.txt":        "random note, not a key",
	}
	for name, want := range cases {
		got, _ := os.ReadFile(filepath.Join(sshDir, name))
		assert.Equal(t, want, string(got), "%s must be left alone", name)
	}
}

// TestEncryptSSHKeys_IgnoresNonKeyFiles guards against the exact regression
// that cost this project's author 10 real SSH keys: a file living in the
// SSH dir whose filename doesn't match the skip-list (known_hosts, config,
// authorized_keys, *.pub) but whose content is NOT a private key. The old
// implementation would happily encrypt it. The new content-sniff refuses.
func TestEncryptSSHKeys_IgnoresNonKeyFiles(t *testing.T) {
	setupTestEnv(t)

	sshDir := t.TempDir()

	noise := map[string]string{
		"random_file":      "just some text the user dropped here",
		"known_hosts.old":  "legacy known hosts",
		"todo.md":          "# reminder to self",
		"binary.bin":       "\x00\x01\x02not a key",
		"empty":            "",
		"starts_with_dash": "--- not a BEGIN header ---",
	}
	for name, content := range noise {
		require.NoError(t, os.WriteFile(filepath.Join(sshDir, name), []byte(content), 0600))
	}

	n, err := EncryptSSHKeys(sshDir)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "no non-key files should be encrypted")

	for name, want := range noise {
		got, _ := os.ReadFile(filepath.Join(sshDir, name))
		assert.Equal(t, want, string(got), "%s must be left alone", name)
	}
}

func TestEncryptSSHKeys_Idempotent(t *testing.T) {
	setupTestEnv(t)

	sshDir := t.TempDir()
	pemKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nbody\n-----END OPENSSH PRIVATE KEY-----\n"
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte(pemKey), 0600))

	// First pass
	n1, err := EncryptSSHKeys(sshDir)
	require.NoError(t, err)
	assert.Equal(t, 1, n1)

	// Second pass (already encrypted)
	n2, err := EncryptSSHKeys(sshDir)
	require.NoError(t, err)
	assert.Equal(t, 0, n2, "should not re-encrypt already encrypted files")
}

func TestIsSSHPrivateKey(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"openssh", "-----BEGIN OPENSSH PRIVATE KEY-----\n", true},
		{"rsa", "-----BEGIN RSA PRIVATE KEY-----\n", true},
		{"ec", "-----BEGIN EC PRIVATE KEY-----\n", true},
		{"ed25519", "-----BEGIN ED25519 PRIVATE KEY-----\n", true},
		{"encrypted", "-----BEGIN ENCRYPTED PRIVATE KEY-----\n", true},
		{"generic private key", "-----BEGIN PRIVATE KEY-----\n", true},
		{"no newline still OK", "-----BEGIN OPENSSH PRIVATE KEY-----", true},

		{"public key", "ssh-ed25519 AAAA user@host", false},
		{"certificate", "-----BEGIN CERTIFICATE-----\n", false},
		{"public key PEM", "-----BEGIN PUBLIC KEY-----\n", false},
		{"random text", "hello world", false},
		{"starts with dashes but wrong", "--- just dashes ---", false},
		{"empty", "", false},
		{"only BEGIN prefix", "-----BEGIN something else-----\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, isSSHPrivateKey(c.content))
		})
	}
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
