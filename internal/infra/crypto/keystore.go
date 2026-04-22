package crypto

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isSSHPrivateKey inspects a file's first line and decides whether it is
// a PEM/OpenSSH-formatted SSH private key. Used by EncryptSSHKeys to avoid
// touching files that merely live in an SSH directory (e.g. `known_hosts`,
// `known_hosts.old`, notes the user dropped in there, random text).
//
// This is a content sniff, not a filename check — the previous filename
// allowlist missed `known_hosts.old` and would happily encrypt any arbitrary
// file a user happened to put in ~/.ssh/.
func isSSHPrivateKey(content string) bool {
	// OpenSSH and traditional PEM formats both start with "-----BEGIN ".
	// The middle token tells us what kind of key this is.
	if !strings.HasPrefix(content, "-----BEGIN ") {
		return false
	}
	firstLineEnd := strings.IndexByte(content, '\n')
	if firstLineEnd < 0 {
		firstLineEnd = len(content)
	}
	header := content[:firstLineEnd]
	for _, marker := range []string{
		"OPENSSH PRIVATE KEY",
		"RSA PRIVATE KEY",
		"DSA PRIVATE KEY",
		"EC PRIVATE KEY",
		"ED25519 PRIVATE KEY",
		"ENCRYPTED PRIVATE KEY",
		"PRIVATE KEY",
	} {
		if strings.Contains(header, marker) {
			return true
		}
	}
	return false
}

// EncryptSSHKeys encrypts all SSH private key files in the given directory.
//
// A file is eligible for encryption only when its first line contains a
// recognised PEM/OpenSSH private-key BEGIN marker. Files that are:
//   - already encrypted (have an `enc:` prefix)
//   - public keys (`.pub` extension)
//   - `known_hosts`, `known_hosts.old`, `config`, `authorized_keys`, or other
//     non-key text the user has dropped in the directory
//
// are left alone. Returns the number of files encrypted.
//
// WARNING: this mutates files in place. Callers must pass only the directory
// they actually own — composerd's startup hook targets the container's
// composer user home (/home/composer/.ssh) by default. Passing an operator's
// personal ~/.ssh will encrypt every private key they have.
func EncryptSSHKeys(sshDir string) (int, error) {
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // no SSH dir, nothing to encrypt
		}
		return 0, fmt.Errorf("reading SSH directory: %w", err)
	}

	encrypted := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Filename allowlist short-circuit — skip obviously-not-keys before
		// even reading the file content.
		if name == "config" || name == "authorized_keys" {
			continue
		}
		if strings.HasPrefix(name, "known_hosts") {
			continue
		}
		if filepath.Ext(name) == ".pub" {
			continue
		}

		keyPath := filepath.Join(sshDir, name)
		data, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}
		content := string(data)

		// Already encrypted — nothing to do
		if len(content) >= 4 && content[:4] == "enc:" {
			continue
		}

		// Empty file — skip
		if len(content) == 0 {
			continue
		}

		// Content sniff — only touch actual private keys, regardless of filename
		if !isSSHPrivateKey(content) {
			continue
		}

		if err := EncryptFile(keyPath); err != nil {
			return encrypted, fmt.Errorf("encrypting %s: %w", name, err)
		}
		encrypted++
	}

	return encrypted, nil
}
