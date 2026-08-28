package docker

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ValidateHostCerts checks an mTLS triple before it is stored:
//   - the CA PEM parses into a cert pool,
//   - the client cert + key parse and the key matches the cert (tls.X509KeyPair),
//   - the client cert chains to the CA pool (x509 Verify with Roots;
//     intermediate certs from the same bundle are accepted).
//
// Returns the client cert's sha256 fingerprint (hex of the DER) and NotAfter.
// Errors are descriptive and never contain PEM material, so they are safe to
// surface as 422 responses.
func ValidateHostCerts(caPEM, certPEM, keyPEM string) (fingerprint string, notAfter time.Time, err error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return "", time.Time{}, fmt.Errorf("CA certificate: no valid PEM certificate found")
	}

	kp, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("client certificate/key pair: %w", err)
	}
	if len(kp.Certificate) == 0 {
		return "", time.Time{}, fmt.Errorf("client certificate: no PEM certificate block found")
	}
	leaf, err := x509.ParseCertificate(kp.Certificate[0])
	if err != nil {
		return "", time.Time{}, fmt.Errorf("client certificate: %w", err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         pool,
		Intermediates: pool,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("client certificate does not chain to the CA: %w", err)
	}

	sum := sha256.Sum256(leaf.Raw)
	return hex.EncodeToString(sum[:]), leaf.NotAfter, nil
}

// HostCertsDir returns the on-disk directory a host's mTLS material is
// materialized to: <dataDir>/certs/<hostID>.
func HostCertsDir(dataDir string, hostID int64) string {
	return filepath.Join(dataDir, "certs", strconv.FormatInt(hostID, 10))
}

// MaterializeHostCerts writes the mTLS triple to HostCertsDir(dataDir, hostID)
// as ca.pem / cert.pem / key.pem (files 0600, dir 0700) - the exact filenames
// docker.TLSConfig expects. Files whose content is already correct are left
// untouched, so repeated materialization is cheap and does not churn mtimes.
func MaterializeHostCerts(dataDir string, hostID int64, ca, cert, key []byte) (string, error) {
	dir := HostCertsDir(dataDir, hostID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("creating cert dir %s: %w", dir, err)
	}
	for _, f := range []struct {
		name    string
		content []byte
	}{
		{"ca.pem", ca},
		{"cert.pem", cert},
		{"key.pem", key},
	} {
		if err := writeFileIfChanged(filepath.Join(dir, f.name), f.content, 0600); err != nil {
			return "", fmt.Errorf("writing %s: %w", f.name, err)
		}
	}
	return dir, nil
}

// writeFileIfChanged writes content to path with the given mode, skipping the
// write when the existing content already matches (mode is still enforced).
func writeFileIfChanged(path string, content []byte, mode os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, content) {
			if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() != mode {
				return os.Chmod(path, mode)
			}
			return nil
		}
	}
	return os.WriteFile(path, content, mode)
}
