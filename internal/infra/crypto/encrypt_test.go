package crypto

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func resetKey() {
	encKeyOnce = sync.Once{}
	encKey = nil
	encKeyErr = nil
}

func TestEncryptDecryptWithEnvKey(t *testing.T) {
	resetKey()
	os.Setenv("COMPOSER_ENCRYPTION_KEY", "test-secret-key-for-unit-tests")
	defer os.Unsetenv("COMPOSER_ENCRYPTION_KEY")

	plaintext := `{"token":"ghp_abc123","username":"deploy"}`

	encrypted, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if encrypted == plaintext {
		t.Fatal("Encrypt returned plaintext unchanged")
	}
	if len(encrypted) < 4 || encrypted[:4] != "enc:" {
		t.Fatalf("missing enc: prefix: %q", encrypted)
	}

	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestAutoGenerateKey(t *testing.T) {
	resetKey()
	os.Unsetenv("COMPOSER_ENCRYPTION_KEY")

	// Use a temp dir for the key file
	tmpDir := t.TempDir()
	os.Setenv("COMPOSER_DATA_DIR", tmpDir)
	defer os.Unsetenv("COMPOSER_DATA_DIR")

	plaintext := "sensitive-data"
	encrypted, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt with auto key failed: %v", err)
	}
	if encrypted == plaintext {
		t.Fatal("Expected encryption, got plaintext")
	}

	// Key file should exist
	keyFile := filepath.Join(tmpDir, "encryption.key")
	if _, err := os.Stat(keyFile); err != nil {
		t.Fatalf("Key file not created: %v", err)
	}

	// Decrypt should work
	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("roundtrip: got %q, want %q", decrypted, plaintext)
	}
}

func TestAutoGenerateKeyPersists(t *testing.T) {
	os.Unsetenv("COMPOSER_ENCRYPTION_KEY")
	tmpDir := t.TempDir()
	os.Setenv("COMPOSER_DATA_DIR", tmpDir)
	defer os.Unsetenv("COMPOSER_DATA_DIR")

	// First run: encrypt
	resetKey()
	encrypted, err := Encrypt("test-data")
	if err != nil {
		t.Fatalf("Encrypt 1: %v", err)
	}

	// Second run: new process simulation (reset singleton)
	resetKey()
	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt 2: %v", err)
	}
	if decrypted != "test-data" {
		t.Fatalf("persistence roundtrip: got %q", decrypted)
	}
}

func TestDecryptUnencryptedData(t *testing.T) {
	raw := `{"token":"ghp_abc123"}`
	result, err := Decrypt(raw)
	if err != nil {
		t.Fatalf("Decrypt unencrypted: %v", err)
	}
	if result != raw {
		t.Fatalf("Expected passthrough, got %q", result)
	}
}

func TestEncryptEmpty(t *testing.T) {
	result, err := Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	if result != "" {
		t.Fatalf("Expected empty, got %q", result)
	}
}
