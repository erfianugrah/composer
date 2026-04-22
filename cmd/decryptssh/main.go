// decryptssh decrypts SSH private keys that composerd's startup hook
// encrypted in-place (files with `enc:` prefix, AES-256-GCM via the project's
// internal/infra/crypto package).
//
// Usage:
//
//	COMPOSER_DATA_DIR=/path/to/encryption.key.dir go run ./cmd/decryptssh/ ~/.ssh/id_foo ~/.ssh/id_bar
//	COMPOSER_ENCRYPTION_KEY=... go run ./cmd/decryptssh/ ~/.ssh/id_foo
//
// Safety:
//   - Only rewrites files that currently start with `enc:`. Already-plaintext
//     files are left alone (no-op).
//   - Writes to a sibling `.tmp` first then os.Rename so a mid-write crash
//     leaves the original encrypted form intact.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/erfianugrah/composer/internal/infra/crypto"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "Report what would be done without writing")
	flag.Parse()

	paths := flag.Args()
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "usage: decryptssh [--dry-run] <file> [<file>...]")
		os.Exit(2)
	}

	failed := 0
	for _, p := range paths {
		if err := decryptInPlace(p, *dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p, err)
			failed++
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func decryptInPlace(path string, dryRun bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) < 4 || string(raw[:4]) != "enc:" {
		fmt.Printf("%s: already plaintext, skipped\n", path)
		return nil
	}

	plain, err := crypto.Decrypt(string(raw))
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	if plain == "" {
		return fmt.Errorf("decrypt returned empty plaintext")
	}

	if dryRun {
		fmt.Printf("%s: would decrypt (%d bytes enc -> %d bytes plain)\n", path, len(raw), len(plain))
		return nil
	}

	// Preserve permissions via stat+chmod after rename
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(plain), 0600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Chmod(tmp, info.Mode().Perm()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}

	abs, _ := filepath.Abs(path)
	fmt.Printf("%s: decrypted in place\n", abs)
	return nil
}
