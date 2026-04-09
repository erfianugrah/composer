package crypto

import (
	"fmt"
	"os"
	"path/filepath"
)

// EncryptSSHKeys encrypts all SSH private key files in the given directory.
// Skips files that are already encrypted (have "enc:" prefix), public keys
// (.pub), and known_hosts/config files.
// Returns the number of files encrypted.
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

		// Skip non-key files
		if name == "known_hosts" || name == "config" || name == "authorized_keys" {
			continue
		}
		// Skip public keys
		if filepath.Ext(name) == ".pub" {
			continue
		}

		keyPath := filepath.Join(sshDir, name)
		data, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}
		content := string(data)

		// Already encrypted
		if len(content) >= 4 && content[:4] == "enc:" {
			continue
		}

		// Skip empty files
		if len(content) == 0 {
			continue
		}

		// Encrypt in place
		if err := EncryptFile(keyPath); err != nil {
			return encrypted, fmt.Errorf("encrypting %s: %w", name, err)
		}
		encrypted++
	}

	return encrypted, nil
}
