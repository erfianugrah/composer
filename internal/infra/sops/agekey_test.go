package sops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAgeKey(t *testing.T) {
	privKey, pubKey, err := GenerateAgeKey()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(privKey, "AGE-SECRET-KEY-"), "private key should start with AGE-SECRET-KEY-")
	assert.True(t, strings.HasPrefix(pubKey, "age1"), "public key should start with age1")
}

func TestLoadGlobalAgeKey_FromEnv(t *testing.T) {
	// COMPOSER_SOPS_AGE_KEY takes priority
	os.Setenv("COMPOSER_SOPS_AGE_KEY", "AGE-SECRET-KEY-TEST123")
	defer os.Unsetenv("COMPOSER_SOPS_AGE_KEY")

	key := LoadGlobalAgeKey("")
	assert.Equal(t, "AGE-SECRET-KEY-TEST123", key)
}

func TestLoadGlobalAgeKey_FromSOPSAgeKey(t *testing.T) {
	os.Unsetenv("COMPOSER_SOPS_AGE_KEY")
	os.Setenv("SOPS_AGE_KEY", "AGE-SECRET-KEY-SOPS456")
	defer os.Unsetenv("SOPS_AGE_KEY")

	key := LoadGlobalAgeKey("")
	assert.Equal(t, "AGE-SECRET-KEY-SOPS456", key)
}

func TestLoadGlobalAgeKey_FromSOPSAgeKeys(t *testing.T) {
	os.Unsetenv("COMPOSER_SOPS_AGE_KEY")
	os.Unsetenv("SOPS_AGE_KEY")
	// User convention: SOPS_AGE_KEYS with escaped newlines
	os.Setenv("SOPS_AGE_KEYS", `# public key: age1abc\nAGE-SECRET-KEY-MULTI789`)
	defer os.Unsetenv("SOPS_AGE_KEYS")

	key := LoadGlobalAgeKey("")
	assert.Equal(t, "AGE-SECRET-KEY-MULTI789", key)
}

func TestLoadGlobalAgeKey_FromDataDir(t *testing.T) {
	os.Unsetenv("COMPOSER_SOPS_AGE_KEY")
	os.Unsetenv("SOPS_AGE_KEY")
	os.Unsetenv("SOPS_AGE_KEYS")
	os.Unsetenv("COMPOSER_SOPS_AGE_KEY_FILE")
	os.Unsetenv("SOPS_AGE_KEY_FILE")

	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "age.key")
	require.NoError(t, os.WriteFile(keyFile, []byte("# comment\nAGE-SECRET-KEY-FROMFILE\n"), 0600))

	key := LoadGlobalAgeKey(tmpDir)
	assert.Equal(t, "AGE-SECRET-KEY-FROMFILE", key)
}

func TestLoadGlobalAgeKey_NotFound(t *testing.T) {
	os.Unsetenv("COMPOSER_SOPS_AGE_KEY")
	os.Unsetenv("SOPS_AGE_KEY")
	os.Unsetenv("SOPS_AGE_KEYS")
	os.Unsetenv("COMPOSER_SOPS_AGE_KEY_FILE")
	os.Unsetenv("SOPS_AGE_KEY_FILE")

	key := LoadGlobalAgeKey(t.TempDir()) // empty dir
	assert.Equal(t, "", key)
}

func TestResolveAgeKey_PerStackOverride(t *testing.T) {
	os.Setenv("COMPOSER_SOPS_AGE_KEY", "AGE-SECRET-KEY-GLOBAL")
	defer os.Unsetenv("COMPOSER_SOPS_AGE_KEY")

	// Per-stack overrides global
	key := ResolveAgeKey("AGE-SECRET-KEY-PERSTACK", "")
	assert.Equal(t, "AGE-SECRET-KEY-PERSTACK", key)
}

func TestResolveAgeKey_FallbackToGlobal(t *testing.T) {
	os.Setenv("COMPOSER_SOPS_AGE_KEY", "AGE-SECRET-KEY-GLOBAL")
	defer os.Unsetenv("COMPOSER_SOPS_AGE_KEY")

	// Empty per-stack -> falls back to global
	key := ResolveAgeKey("", "")
	assert.Equal(t, "AGE-SECRET-KEY-GLOBAL", key)
}

func TestSaveAgeKey(t *testing.T) {
	tmpDir := t.TempDir()
	err := SaveAgeKey(tmpDir, "AGE-SECRET-KEY-SAVED", "age1pubkey")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(tmpDir, "age.key"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "AGE-SECRET-KEY-SAVED")
	assert.Contains(t, string(data), "age1pubkey")

	// File should be 0600
	info, _ := os.Stat(filepath.Join(tmpDir, "age.key"))
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
