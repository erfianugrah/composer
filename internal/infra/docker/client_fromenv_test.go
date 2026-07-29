package docker

// Behavioral unit tests for the FromEnv wiring in NewClient (mTLS support,
// e.g. a TLS-terminated remote engine). No docker daemon needed: we stand up a real TLS server
// that requires client certificates and speaks just enough Engine API for
// NewClient's runtime detection (Info) to succeed. Runtime() is the
// observable signal: "docker" = mTLS handshake + API call succeeded,
// "unknown" = connection failed.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCertBundle writes ca.pem, cert.pem, key.pem into dir with the naming
// WithTLSClientConfigFromEnv expects. One self-signed cert serves as CA,
// server leaf, and client leaf (nil ExtKeyUsage = valid for any usage; the
// IP SAN covers server verification).
func writeCertBundle(t *testing.T, dir, cn string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	write := func(name, typ string, der []byte) {
		t.Helper()
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := pem.Encode(f, &pem.Block{Type: typ, Bytes: der}); err != nil {
			t.Fatal(err)
		}
	}
	write("ca.pem", "CERTIFICATE", der)
	write("cert.pem", "CERTIFICATE", der)
	write("key.pem", "EC PRIVATE KEY", keyDER)
}

// mtlsEngineServer returns a TLS server that requires a client cert signed
// by the CA in certDir and answers /_ping + /info. host is its "tcp://..." URL.
func mtlsEngineServer(t *testing.T, certDir string) (host string) {
	t.Helper()

	caPEM, err := os.ReadFile(filepath.Join(certDir, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("ca.pem not parseable")
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(certDir, "cert.pem"), filepath.Join(certDir, "key.pem"))
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("Api-Version", "1.47")
			w.Header().Set("Docker-Experimental", "false")
			w.Header().Set("Ostype", "linux")
			fmt.Fprint(w, "OK")
		case strings.HasSuffix(r.URL.Path, "/info"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ID":"test","OperatingSystem":"linux","Name":"fake-engine"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	return "tcp://" + strings.TrimPrefix(srv.URL, "https://")
}

// Explicit host must always win over DOCKER_HOST: WithHost is applied after
// FromEnv inside NewClient.
func TestNewClient_ExplicitHostBeatsDockerHostEnv(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://10.255.255.1:2375")
	t.Setenv("COMPOSER_DOCKER_HOST", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	c, err := NewClient("tcp://127.0.0.1:1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := c.cli.DaemonHost(); got != "tcp://127.0.0.1:1" {
		t.Errorf("DaemonHost = %q", got)
	}
}

// With DOCKER_CERT_PATH + DOCKER_TLS_VERIFY, the client must complete an
// mTLS handshake and reach the API (runtime detection succeeds).
func TestNewClient_mTLSConnectsWithEnv(t *testing.T) {
	dir := t.TempDir()
	writeCertBundle(t, dir, "test-ca")
	host := mtlsEngineServer(t, dir)

	t.Setenv("DOCKER_CERT_PATH", dir)
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_HOST", "")

	c, err := NewClient(host, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.Runtime() != "docker" {
		t.Errorf("Runtime() = %q, want docker (mTLS handshake should have succeeded)", c.Runtime())
	}
}

// Without the env vars the same server must be unreachable (it requires a
// client certificate) - proves the env, not luck, drove the previous test.
func TestNewClient_mTLSRejectedWithoutEnv(t *testing.T) {
	dir := t.TempDir()
	writeCertBundle(t, dir, "test-ca")
	host := mtlsEngineServer(t, dir)

	t.Setenv("DOCKER_CERT_PATH", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_HOST", "")

	c, err := NewClient(host, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.Runtime() != "unknown" {
		t.Errorf("Runtime() = %q, want unknown (server requires client cert)", c.Runtime())
	}
}

// A client cert from a DIFFERENT CA must be rejected - guards against
// accidentally configuring InsecureSkipVerify-style trust.
func TestNewClient_mTLSRejectedWrongCA(t *testing.T) {
	serverDir := t.TempDir()
	writeCertBundle(t, serverDir, "server-ca")
	host := mtlsEngineServer(t, serverDir)

	clientDir := t.TempDir()
	writeCertBundle(t, clientDir, "other-ca")

	t.Setenv("DOCKER_CERT_PATH", clientDir)
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_HOST", "")

	c, err := NewClient(host, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.Runtime() != "unknown" {
		t.Errorf("Runtime() = %q, want unknown (client cert not under server CA)", c.Runtime())
	}
}
