package crypto

import (
	stdcrypto "crypto"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"
)

// pemArmorRe matches a PEM private-key block even when every newline has
// been collapsed to a space or removed entirely. RE2 has no backreferences,
// so the END marker is captured separately and compared by the caller.
var pemArmorRe = regexp.MustCompile(`-----BEGIN ([A-Z0-9 ]*PRIVATE KEY)-----\s*(.+?)\s*-----END ([A-Z0-9 ]*PRIVATE KEY)-----`)

// NormalizeSSHPrivateKey validates and canonicalizes SSH private key content
// received from UI paste paths.
//
// Paste paths mangle keys in two common ways:
//   - CRLF line endings (Windows clipboards)
//   - fully flattened whitespace (keys round-tripped through single-line
//     storage fields, e.g. password-manager custom fields, which collapse
//     or drop newlines)
//
// Both are repaired, then the result must parse as a real private key via
// x/crypto/ssh. Returns the canonical armored form (LF endings, trailing
// newline) or an error describing why the content is unusable.
func NormalizeSSHPrivateKey(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errors.New("empty key content")
	}
	if strings.HasPrefix(content, "enc:") {
		return "", errors.New("content is a composer-encrypted blob (enc:...); paste the plaintext PEM key instead")
	}

	// CRLF / CR normalization (Windows clipboards)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	if _, err := ssh.ParseRawPrivateKey([]byte(ensureTrailingNewline(content))); err == nil {
		return ensureTrailingNewline(content), nil
	}

	// Repair attempt: re-armor a flattened key. Single-line storage fields
	// (password managers, some browsers' autofill) collapse the newlines,
	// which pem.Decode cannot read - but the armor is trivially recoverable.
	repaired, rerr := rearmorPEM(content)
	if rerr != nil {
		return "", fmt.Errorf("not a recognizable PEM private key: %v", rerr)
	}
	if _, err := ssh.ParseRawPrivateKey([]byte(repaired)); err != nil {
		var passErr *ssh.PassphraseMissingError
		if errors.As(err, &passErr) {
			return "", errors.New("passphrase-protected keys are not supported; decrypt the key first (ssh-keygen -p)")
		}
		return "", fmt.Errorf("key does not parse even after whitespace repair: %w", err)
	}
	return repaired, nil
}

// SSHPublicKey derives the authorized_keys-form public key and its SHA256
// fingerprint from a normalized private key. Used to surface the fingerprint
// in the API response and to drop a .pub alongside the stored key.
func SSHPublicKey(normalizedPrivateKey string) (authorizedKey string, fingerprint string, err error) {
	raw, err := ssh.ParseRawPrivateKey([]byte(normalizedPrivateKey))
	if err != nil {
		return "", "", fmt.Errorf("parsing private key: %w", err)
	}
	signer, ok := raw.(stdcrypto.Signer)
	if !ok {
		return "", "", fmt.Errorf("unsupported key type %T: cannot derive public key", raw)
	}
	pub, err := ssh.NewPublicKey(signer.Public())
	if err != nil {
		return "", "", fmt.Errorf("deriving public key: %w", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))), ssh.FingerprintSHA256(pub), nil
}

func ensureTrailingNewline(s string) string {
	if !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

// rearmorPEM rebuilds canonical PEM armor from content whose newlines were
// collapsed or deleted. The base64 body has all internal whitespace stripped
// and is re-wrapped at 70 columns (OpenSSH convention).
func rearmorPEM(content string) (string, error) {
	flat := strings.Join(strings.Fields(content), " ")
	m := pemArmorRe.FindStringSubmatch(flat)
	if m == nil {
		return "", errors.New("no BEGIN/END PRIVATE KEY armor found")
	}
	if m[1] != m[3] {
		return "", fmt.Errorf("mismatched armor: BEGIN %q vs END %q", m[1], m[3])
	}
	body := strings.Join(strings.Fields(m[2]), "")
	if body == "" {
		return "", errors.New("empty key body")
	}

	var b strings.Builder
	b.WriteString("-----BEGIN " + m[1] + "-----\n")
	for len(body) > 70 {
		b.WriteString(body[:70] + "\n")
		body = body[70:]
	}
	b.WriteString(body + "\n")
	b.WriteString("-----END " + m[1] + "-----\n")
	return b.String(), nil
}
