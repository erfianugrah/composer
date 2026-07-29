package docker

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewClientTLSMissingCerts: TLSConfig with empty cert dir -> error.
func TestNewClientTLSMissingCerts(t *testing.T) {
	dir := t.TempDir() // empty: no ca.pem/cert.pem/key.pem
	_, err := NewClient("tcp://docker-remote.example:2376", &TLSConfig{CertDir: dir})
	if err == nil {
		t.Fatal("expected error for missing cert files")
	}
}

// TestNewClientTLSNilKeepsEnvBehaviour: nil TLSConfig = legacy env path.
func TestNewClientTLSNilKeepsEnvBehaviour(t *testing.T) {
	c, err := NewClient("tcp://docker-remote.example:2376", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()
	if c.Host() != "tcp://docker-remote.example:2376" {
		t.Fatalf("Host() = %q", c.Host())
	}
}

// writeTestCerts generates a self-signed CA+client cert triplet into dir
// (ca.pem/cert.pem/key.pem). Reuses the helper from client_fromenv_test.go.
func TestNewClientTLSWithCerts(t *testing.T) {
	dir := t.TempDir()
	writeCertBundle(t, dir, "test-cn")
	for _, f := range []string{"ca.pem", "cert.pem", "key.pem"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("fixture missing %s: %v", f, err)
		}
	}
	c, err := NewClient("tcp://docker-remote.example:2376", &TLSConfig{CertDir: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()
}
