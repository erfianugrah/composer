package git

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// WebhookProvider identifies the git hosting platform.
type WebhookProvider string

const (
	ProviderGitHub    WebhookProvider = "github"
	ProviderGitLab    WebhookProvider = "gitlab"
	ProviderGitea     WebhookProvider = "gitea"
	ProviderBitbucket WebhookProvider = "bitbucket"
	ProviderGeneric   WebhookProvider = "generic"
)

// WebhookPayload is the parsed result of a webhook delivery.
type WebhookPayload struct {
	Provider  WebhookProvider
	Event     string // "push", "ping", etc.
	Branch    string // extracted branch name
	CommitSHA string // head commit SHA
	Ref       string // full ref (refs/heads/main)
}

// ValidateSignature verifies the webhook signature using HMAC-SHA256.
// Each provider has a different header and format.
func ValidateSignature(provider WebhookProvider, secret string, headers map[string]string, body []byte) bool {
	switch provider {
	case ProviderGitHub:
		return validateGitHub(secret, headers, body)
	case ProviderGitLab:
		return validateGitLab(secret, headers)
	case ProviderGitea:
		return validateGitea(secret, headers, body)
	case ProviderGeneric:
		return validateGeneric(secret, headers, body)
	default:
		return false
	}
}

// ParsePayload extracts branch and commit info from the webhook body.
func ParsePayload(provider WebhookProvider, headers map[string]string, body []byte) (*WebhookPayload, error) {
	payload := &WebhookPayload{Provider: provider}

	switch provider {
	case ProviderGitHub:
		payload.Event = headers["x-github-event"]
		return parseGitHubPayload(payload, body)
	case ProviderGitLab:
		payload.Event = headers["x-gitlab-event"]
		return parseGitLabPayload(payload, body)
	case ProviderGitea:
		payload.Event = headers["x-gitea-event"]
		return parseGiteaPayload(payload, body)
	default:
		return parseGenericPayload(payload, body)
	}
}

// --- GitHub ---

func validateGitHub(secret string, headers map[string]string, body []byte) bool {
	sig := headers["x-hub-signature-256"]
	if sig == "" {
		return false
	}
	// Format: sha256=<hex>
	sig = strings.TrimPrefix(sig, "sha256=")
	return verifyHMACSHA256(secret, body, sig)
}

func parseGitHubPayload(p *WebhookPayload, body []byte) (*WebhookPayload, error) {
	var data struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		HeadCommit struct {
			ID string `json:"id"`
		} `json:"head_commit"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parsing github payload: %w", err)
	}
	p.Ref = data.Ref
	p.Branch = strings.TrimPrefix(data.Ref, "refs/heads/")
	p.CommitSHA = data.HeadCommit.ID
	if p.CommitSHA == "" {
		p.CommitSHA = data.After
	}
	return p, nil
}

// --- GitLab ---

func validateGitLab(secret string, headers map[string]string) bool {
	// GitLab uses a plain token in X-Gitlab-Token header (not HMAC)
	return headers["x-gitlab-token"] == secret
}

func parseGitLabPayload(p *WebhookPayload, body []byte) (*WebhookPayload, error) {
	var data struct {
		Ref   string `json:"ref"`
		After string `json:"after"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parsing gitlab payload: %w", err)
	}
	p.Ref = data.Ref
	p.Branch = strings.TrimPrefix(data.Ref, "refs/heads/")
	p.CommitSHA = data.After
	return p, nil
}

// --- Gitea ---

func validateGitea(secret string, headers map[string]string, body []byte) bool {
	// Gitea uses same HMAC-SHA256 as GitHub but in X-Gitea-Signature header
	sig := headers["x-gitea-signature"]
	if sig == "" {
		return false
	}
	return verifyHMACSHA256(secret, body, sig)
}

func parseGiteaPayload(p *WebhookPayload, body []byte) (*WebhookPayload, error) {
	// Gitea payload format is identical to GitHub
	return parseGitHubPayload(p, body)
}

// --- Generic ---

func validateGeneric(secret string, headers map[string]string, body []byte) bool {
	// Generic: check X-Webhook-Signature header with HMAC-SHA256
	sig := headers["x-webhook-signature"]
	if sig == "" {
		return false
	}
	sig = strings.TrimPrefix(sig, "sha256=")
	return verifyHMACSHA256(secret, body, sig)
}

func parseGenericPayload(p *WebhookPayload, body []byte) (*WebhookPayload, error) {
	var data struct {
		Ref       string `json:"ref"`
		After     string `json:"after"`
		CommitSHA string `json:"commit_sha"`
		Branch    string `json:"branch"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parsing generic payload: %w", err)
	}
	p.Ref = data.Ref
	p.Branch = data.Branch
	if p.Branch == "" {
		p.Branch = strings.TrimPrefix(data.Ref, "refs/heads/")
	}
	p.CommitSHA = data.CommitSHA
	if p.CommitSHA == "" {
		p.CommitSHA = data.After
	}
	return p, nil
}

// --- HMAC ---

func verifyHMACSHA256(secret string, body []byte, expectedHex string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	return hmac.Equal(mac.Sum(nil), expected)
}
