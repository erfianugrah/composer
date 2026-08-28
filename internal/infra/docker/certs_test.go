package docker

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// genTestCerts builds a self-signed CA + client leaf (signed by the CA) and
// returns the PEMs. A separate keypair (leaf2) is returned for mismatch tests.
func genTestCerts(t *testing.T) (caPEM, certPEM, keyPEM, otherKeyPEM string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTmpl, &caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	mkLeaf := func(serial int64) (leafDER []byte, keyDER []byte) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		tmpl := x509.Certificate{
			SerialNumber: big.NewInt(serial),
			Subject:      pkix.Name{CommonName: "client"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		keyDer, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			t.Fatal(err)
		}
		return der, keyDer
	}

	leafDER, leafKeyDER := mkLeaf(2)
	_, otherKeyDER := mkLeaf(3)

	enc := func(der []byte, typ string) string {
		return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
	}
	return enc(caDER, "CERTIFICATE"),
		enc(leafDER, "CERTIFICATE"),
		enc(leafKeyDER, "PRIVATE KEY"),
		enc(otherKeyDER, "PRIVATE KEY")
}

func TestValidateHostCertsValid(t *testing.T) {
	caPEM, certPEM, keyPEM, _ := genTestCerts(t)

	fp, notAfter, err := ValidateHostCerts(caPEM, certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp) != 64 {
		t.Fatalf("fingerprint = %q, want 64 hex chars", fp)
	}
	if _, err := hex.DecodeString(fp); err != nil {
		t.Fatalf("fingerprint not hex: %v", err)
	}
	// Fingerprint is sha256 of the leaf DER.
	kp, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(kp.Certificate[0])
	if fp != hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint %q != sha256(DER) %q", fp, hex.EncodeToString(sum[:]))
	}
	if !notAfter.After(time.Now()) {
		t.Fatalf("notAfter %v not in the future", notAfter)
	}
}

func TestValidateHostCertsKeyMismatch(t *testing.T) {
	caPEM, certPEM, _, otherKeyPEM := genTestCerts(t)
	_, _, err := ValidateHostCerts(caPEM, certPEM, otherKeyPEM)
	if err == nil {
		t.Fatal("expected error for non-matching key")
	}
}

func TestValidateHostCertsChainFailure(t *testing.T) {
	// Leaf from one CA, pool from another.
	_, certPEM, keyPEM, _ := genTestCerts(t)
	caPEM2, _, _, _ := genTestCerts(t)
	_, _, err := ValidateHostCerts(caPEM2, certPEM, keyPEM)
	if err == nil {
		t.Fatal("expected chain-verification error")
	}
}

func TestValidateHostCertsBadPEM(t *testing.T) {
	if _, _, err := ValidateHostCerts("garbage", "garbage", "garbage"); err == nil {
		t.Fatal("expected error for garbage PEM")
	}
}

func TestMaterializeHostCerts(t *testing.T) {
	dataDir := t.TempDir()
	ca, cert, key := []byte("ca-pem"), []byte("cert-pem"), []byte("key-pem")

	dir, err := MaterializeHostCerts(dataDir, 42, ca, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(dataDir, "certs", "42") {
		t.Fatalf("dir = %q", dir)
	}

	want := map[string][]byte{"ca.pem": ca, "cert.pem": cert, "key.pem": key}
	for name, content := range want {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if fi.Mode().Perm() != 0600 {
			t.Errorf("%s mode = %v, want 0600", name, fi.Mode().Perm())
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(content) {
			t.Errorf("%s content mismatch", name)
		}
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0700 {
		t.Errorf("dir mode = %v, want 0700", fi.Mode().Perm())
	}

	// Re-materialize with the same content: no error, same dir.
	if dir2, err := MaterializeHostCerts(dataDir, 42, ca, cert, key); err != nil || dir2 != dir {
		t.Fatalf("re-materialize: %q, %v", dir2, err)
	}

	// Different content replaces files.
	if _, err := MaterializeHostCerts(dataDir, 42, []byte("new-ca"), []byte("new-cert"), []byte("new-key")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if string(got) != "new-ca" {
		t.Fatalf("ca.pem not updated: %q", got)
	}
}
