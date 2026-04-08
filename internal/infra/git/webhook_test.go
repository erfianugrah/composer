package git_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/erfianugrah/composer/internal/infra/git"
)

func computeHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestValidateSignature_GitHub(t *testing.T) {
	secret := "my-webhook-secret"
	body := []byte(`{"ref":"refs/heads/main","after":"abc123"}`)
	sig := computeHMAC(secret, body)

	headers := map[string]string{
		"x-hub-signature-256": "sha256=" + sig,
		"x-github-event":      "push",
	}

	assert.True(t, git.ValidateSignature(git.ProviderGitHub, secret, headers, body))
	assert.False(t, git.ValidateSignature(git.ProviderGitHub, "wrong-secret", headers, body))
	assert.False(t, git.ValidateSignature(git.ProviderGitHub, secret, map[string]string{}, body))
}

func TestValidateSignature_GitLab(t *testing.T) {
	secret := "my-gitlab-token"
	body := []byte(`{"ref":"refs/heads/main"}`)

	headers := map[string]string{
		"x-gitlab-token": secret,
		"x-gitlab-event": "Push Hook",
	}

	assert.True(t, git.ValidateSignature(git.ProviderGitLab, secret, headers, body))
	assert.False(t, git.ValidateSignature(git.ProviderGitLab, "wrong", headers, body))
}

func TestValidateSignature_Gitea(t *testing.T) {
	secret := "my-gitea-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)
	sig := computeHMAC(secret, body)

	headers := map[string]string{
		"x-gitea-signature": sig,
		"x-gitea-event":     "push",
	}

	assert.True(t, git.ValidateSignature(git.ProviderGitea, secret, headers, body))
	assert.False(t, git.ValidateSignature(git.ProviderGitea, "wrong", headers, body))
}

func TestValidateSignature_Generic(t *testing.T) {
	secret := "generic-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)
	sig := computeHMAC(secret, body)

	headers := map[string]string{
		"x-webhook-signature": "sha256=" + sig,
	}

	assert.True(t, git.ValidateSignature(git.ProviderGeneric, secret, headers, body))
	assert.False(t, git.ValidateSignature(git.ProviderGeneric, "wrong", headers, body))
}

func TestParsePayload_GitHub(t *testing.T) {
	body := []byte(`{
		"ref": "refs/heads/main",
		"after": "abc123def456",
		"head_commit": {"id": "abc123def456"}
	}`)
	headers := map[string]string{"x-github-event": "push"}

	payload, err := git.ParsePayload(git.ProviderGitHub, headers, body)
	require.NoError(t, err)
	assert.Equal(t, "push", payload.Event)
	assert.Equal(t, "main", payload.Branch)
	assert.Equal(t, "abc123def456", payload.CommitSHA)
	assert.Equal(t, "refs/heads/main", payload.Ref)
}

func TestParsePayload_GitHub_FeatureBranch(t *testing.T) {
	body := []byte(`{
		"ref": "refs/heads/feature/new-thing",
		"after": "def789",
		"head_commit": {"id": "def789"}
	}`)
	headers := map[string]string{"x-github-event": "push"}

	payload, err := git.ParsePayload(git.ProviderGitHub, headers, body)
	require.NoError(t, err)
	assert.Equal(t, "feature/new-thing", payload.Branch)
}

func TestParsePayload_GitLab(t *testing.T) {
	body := []byte(`{
		"ref": "refs/heads/develop",
		"after": "gitlab123"
	}`)
	headers := map[string]string{"x-gitlab-event": "Push Hook"}

	payload, err := git.ParsePayload(git.ProviderGitLab, headers, body)
	require.NoError(t, err)
	assert.Equal(t, "Push Hook", payload.Event)
	assert.Equal(t, "develop", payload.Branch)
	assert.Equal(t, "gitlab123", payload.CommitSHA)
}

func TestParsePayload_InvalidJSON(t *testing.T) {
	_, err := git.ParsePayload(git.ProviderGitHub, map[string]string{}, []byte("not json"))
	assert.Error(t, err)
}

func TestValidateSignature_EmptyBody(t *testing.T) {
	secret := "secret"
	body := []byte("")
	sig := computeHMAC(secret, body)

	headers := map[string]string{"x-hub-signature-256": "sha256=" + sig}
	assert.True(t, git.ValidateSignature(git.ProviderGitHub, secret, headers, body))
}

func TestValidateSignature_UnknownProvider(t *testing.T) {
	assert.False(t, git.ValidateSignature("unknown", "secret", map[string]string{}, []byte("")))
}
