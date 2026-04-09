package crypto

import (
	"os"
	"sync"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	// Reset the singleton key for this test
	encKeyOnce = sync.Once{}
	encKey = nil
	encKeyErr = nil

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
		t.Fatalf("Encrypt output missing enc: prefix: %q", encrypted)
	}

	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("Roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptUnencryptedData(t *testing.T) {
	// Unencrypted data (no "enc:" prefix) should pass through unchanged
	raw := `{"token":"ghp_abc123"}`
	result, err := Decrypt(raw)
	if err != nil {
		t.Fatalf("Decrypt of unencrypted data failed: %v", err)
	}
	if result != raw {
		t.Fatalf("Expected passthrough, got %q", result)
	}
}

func TestEncryptWithoutKey(t *testing.T) {
	// Reset the singleton
	encKeyOnce = sync.Once{}
	encKey = nil
	encKeyErr = nil

	os.Unsetenv("COMPOSER_ENCRYPTION_KEY")

	plaintext := "sensitive-data"
	result, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt without key should not error: %v", err)
	}
	// Without key, should return plaintext unchanged (backwards compat)
	if result != plaintext {
		t.Fatalf("Expected plaintext passthrough without key, got %q", result)
	}
}

func TestEncryptEmpty(t *testing.T) {
	result, err := Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt empty should not error: %v", err)
	}
	if result != "" {
		t.Fatalf("Expected empty string, got %q", result)
	}
}
