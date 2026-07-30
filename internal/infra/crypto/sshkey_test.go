package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// generateTestKey produces a real OpenSSH-armored ed25519 private key.
func generateTestKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "test-comment")
	require.NoError(t, err)
	return string(pem.EncodeToMemory(block))
}

func TestNormalizeSSHPrivateKey_ValidPassesThrough(t *testing.T) {
	key := generateTestKey(t)
	out, err := NormalizeSSHPrivateKey(key)
	require.NoError(t, err)
	assert.Equal(t, key, out)
}

func TestNormalizeSSHPrivateKey_CRLFNormalized(t *testing.T) {
	key := generateTestKey(t)
	crlf := strings.ReplaceAll(key, "\n", "\r\n")
	out, err := NormalizeSSHPrivateKey(crlf)
	require.NoError(t, err)
	assert.Equal(t, key, out)
}

func TestNormalizeSSHPrivateKey_MissingTrailingNewline(t *testing.T) {
	key := generateTestKey(t)
	out, err := NormalizeSSHPrivateKey(strings.TrimRight(key, "\n"))
	require.NoError(t, err)
	assert.Equal(t, key, out)
}

// The failure mode that motivated this helper: a key round-tripped through a
// single-line storage field arrives with every newline collapsed to a space.
func TestNormalizeSSHPrivateKey_FlattenedToSpaces(t *testing.T) {
	key := generateTestKey(t)
	flat := strings.ReplaceAll(strings.TrimSpace(key), "\n", " ")
	out, err := NormalizeSSHPrivateKey(flat)
	require.NoError(t, err)
	// Canonical form must parse and round-trip to the same key material.
	want, err := ssh.ParseRawPrivateKey([]byte(key))
	require.NoError(t, err)
	got, err := ssh.ParseRawPrivateKey([]byte(out))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// Harder variant: newlines deleted outright, no spaces at all.
func TestNormalizeSSHPrivateKey_FlattenedNoSpaces(t *testing.T) {
	key := generateTestKey(t)
	flat := strings.ReplaceAll(strings.TrimSpace(key), "\n", "")
	out, err := NormalizeSSHPrivateKey(flat)
	require.NoError(t, err)
	want, err := ssh.ParseRawPrivateKey([]byte(key))
	require.NoError(t, err)
	got, err := ssh.ParseRawPrivateKey([]byte(out))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestNormalizeSSHPrivateKey_RejectsGarbage(t *testing.T) {
	_, err := NormalizeSSHPrivateKey("this is not a key at all")
	assert.Error(t, err)
}

func TestNormalizeSSHPrivateKey_RejectsEmpty(t *testing.T) {
	_, err := NormalizeSSHPrivateKey("   ")
	assert.Error(t, err)
}

func TestNormalizeSSHPrivateKey_RejectsEncryptedBlob(t *testing.T) {
	_, err := NormalizeSSHPrivateKey("enc:8L+wZJBiBc4VjYKdQDxd5SVgXyBO88Hm9tjUeRm6NiQ=")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enc:")
}

func TestNormalizeSSHPrivateKey_RejectsTruncatedKey(t *testing.T) {
	key := generateTestKey(t)
	// Chop the last third of the base64 body but keep the armor.
	lines := strings.Split(strings.TrimSpace(key), "\n")
	truncated := strings.Join(lines[:len(lines)-2], "\n") + "\n-----END OPENSSH PRIVATE KEY-----\n"
	_, err := NormalizeSSHPrivateKey(truncated)
	assert.Error(t, err)
}

func TestSSHPublicKey_DerivesPubAndFingerprint(t *testing.T) {
	key := generateTestKey(t)
	pub, fp, err := SSHPublicKey(key)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(pub, "ssh-ed25519 "), pub)
	assert.True(t, strings.HasPrefix(fp, "SHA256:"), fp)
}
