package handler_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/erfianugrah/composer/internal/api/handler"
	"github.com/erfianugrah/composer/internal/app"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/host"
	"github.com/erfianugrah/composer/internal/infra/store"
	"go.uber.org/zap/zaptest"
)

// fakeHostRepo is a minimal in-memory host.Repository for handler tests.
type fakeHostRepo struct {
	hosts       map[int64]*host.Host
	byName      map[string]*host.Host
	next        int64
	stackCounts map[int64]int
}

func newFakeHostRepo() *fakeHostRepo {
	return &fakeHostRepo{
		hosts:       map[int64]*host.Host{},
		byName:      map[string]*host.Host{},
		stackCounts: map[int64]int{},
	}
}

func (r *fakeHostRepo) Create(_ context.Context, h *host.Host) error {
	r.next++
	h.ID = r.next
	r.hosts[h.ID] = h
	r.byName[h.Name] = h
	return nil
}

func (r *fakeHostRepo) GetByID(_ context.Context, id int64) (*host.Host, error) {
	return r.hosts[id], nil
}

func (r *fakeHostRepo) GetByName(_ context.Context, name string) (*host.Host, error) {
	return r.byName[name], nil
}

func (r *fakeHostRepo) List(_ context.Context) ([]*host.Host, error) {
	out := make([]*host.Host, 0, len(r.hosts))
	for _, h := range r.hosts {
		out = append(out, h)
	}
	return out, nil
}

func (r *fakeHostRepo) Update(_ context.Context, h *host.Host) error {
	delete(r.byName, r.hosts[h.ID].Name)
	r.hosts[h.ID] = h
	r.byName[h.Name] = h
	return nil
}

func (r *fakeHostRepo) Delete(_ context.Context, id int64) error {
	if h, ok := r.hosts[id]; ok {
		delete(r.byName, h.Name)
	}
	delete(r.hosts, id)
	return nil
}

func (r *fakeHostRepo) CountStacks(_ context.Context, id int64) (int, error) {
	return r.stackCounts[id], nil
}

func setupHostAPI(t *testing.T) (huma.API, http.Handler) {
	t.Helper()
	repo := newFakeHostRepo()
	svc := app.NewHostService(repo, zaptest.NewLogger(t))
	router := chi.NewMux()
	cfg := huma.DefaultConfig("test", "0.0.0")
	cfg.OpenAPI.Security = []map[string][]string{} // no auth for tests
	api := humachi.New(router, cfg)
	handler.NewHostHandler(svc).Register(api)
	return api, router
}

func TestListHosts(t *testing.T) {
	_, router := setupHostAPI(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/hosts", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHost(t *testing.T) {
	_, router := setupHostAPI(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/hosts",
		body(`{"name":"remote1","endpoint":"tcp://docker-remote.example:2376"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("expected 200/201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateHostInvalidEndpoint(t *testing.T) {
	_, router := setupHostAPI(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/hosts",
		body(`{"name":"bad","endpoint":"http://x"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func body(s string) *bytes.Reader { return bytes.NewReader([]byte(s)) }

// --- Cert endpoints (real HostCertsRepo on a migrated SQLite DB) ---

// setupCertAPI wires the host handler with a real store.HostCertsRepo and a
// temp data dir for materialized certs. The DB is opened through the same
// production path as main.go (store.New with sqlite://) so all migrations -
// including the Go-coded docker_host_certs migration - run; a temp file is
// used because store.New appends its own DSN pragmas that a raw :memory:
// URI would not survive.
func setupCertAPI(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("COMPOSER_ENCRYPTION_KEY", "test-handler-certs-key")
	dataDir := t.TempDir()
	db, err := store.New(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "handler.db"), dataDir)
	if err != nil {
		t.Fatalf("opening test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	svc := app.NewHostService(store.NewHostRepo(db.SQL), zaptest.NewLogger(t))
	hh := handler.NewHostHandler(svc)
	hh.SetCerts(store.NewHostCertsRepo(db.SQL), nil, dataDir)

	router := chi.NewMux()
	// The cert and test endpoints call authmw.CheckRole, which reads the role
	// the Auth middleware would store. Inject an admin role so the handler's
	// RBAC path runs without session plumbing. Must precede humachi.New (chi
	// forbids middleware after any route, and the adapter registers routes).
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), authmw.TestRoleKey(), auth.RoleAdmin)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	cfg := huma.DefaultConfig("test", "0.0.0")
	cfg.OpenAPI.Security = []map[string][]string{}
	api := humachi.New(router, cfg)
	hh.Register(api)
	return router
}

// genCertTestTriple builds a self-signed CA and a client leaf it signs, as
// PEM strings (key is PKCS#8). Compact stdlib copy of the docker package's
// unexported test generator.
func genCertTestTriple(t *testing.T) (caPEM, certPEM, keyPEM string) {
	t.Helper()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	caTmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "handler-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTmpl, &caTmpl, &caKey.PublicKey, caKey)
	must(err)
	caCert, err := x509.ParseCertificate(caDER)
	must(err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	leafTmpl := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "handler-test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, &leafTmpl, caCert, &leafKey.PublicKey, caKey)
	must(err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	must(err)

	enc := func(der []byte, typ string) string {
		return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
	}
	return enc(caDER, "CERTIFICATE"), enc(leafDER, "CERTIFICATE"), enc(keyDER, "PRIVATE KEY")
}

// createHost POSTs a new host through the API and returns its ID.
func createHost(t *testing.T, router http.Handler, name, endpoint string) int64 {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/hosts",
		body(fmt.Sprintf(`{"name":%q,"endpoint":%q}`, name, endpoint)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("create host: got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Host struct {
			ID int64 `json:"id"`
		} `json:"host"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if out.Host.ID == 0 {
		t.Fatalf("no host id in response: %s", w.Body.String())
	}
	return out.Host.ID
}

// putCerts PUTs an mTLS triple through the API and returns the status code.
func putCerts(t *testing.T, router http.Handler, hostID int64, caPEM, certPEM, keyPEM string) int {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"ca_cert": caPEM, "cert": certPEM, "key": keyPEM})
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/hosts/%d/certs", hostID), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code
}

// getCerts GETs the cert status through the API.
func getCerts(t *testing.T, router http.Handler, hostID int64) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/hosts/%d/certs", hostID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

type certStatus struct {
	HasCerts    bool   `json:"has_certs"`
	Fingerprint string `json:"fingerprint"`
	NotAfter    string `json:"not_after"`
}

func decodeCerts(t *testing.T, raw string) certStatus {
	t.Helper()
	var out struct {
		Certs certStatus `json:"certs"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal certs response: %v (body: %s)", err, raw)
	}
	return out.Certs
}

func TestPutHostCertsValid(t *testing.T) {
	router := setupCertAPI(t)
	id := createHost(t, router, "remote1", "tcp://127.0.0.1:2376")
	caPEM, certPEM, keyPEM := genCertTestTriple(t)

	if code := putCerts(t, router, id, caPEM, certPEM, keyPEM); code != http.StatusOK {
		t.Fatalf("PUT valid certs: got %d", code)
	}

	code, raw := getCerts(t, router, id)
	if code != http.StatusOK {
		t.Fatalf("GET certs: got %d: %s", code, raw)
	}
	st := decodeCerts(t, raw)
	if !st.HasCerts {
		t.Fatalf("expected has_certs=true: %s", raw)
	}
	if len(st.Fingerprint) != 64 {
		t.Fatalf("fingerprint = %q, want 64 hex chars", st.Fingerprint)
	}
	if st.NotAfter == "" {
		t.Fatalf("not_after empty: %s", raw)
	}
}

func TestPutHostCertsGarbagePEM(t *testing.T) {
	router := setupCertAPI(t)
	id := createHost(t, router, "remote1", "tcp://127.0.0.1:2376")

	if code := putCerts(t, router, id, "not a pem", "garbage", "also garbage"); code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT garbage PEM: got %d, want 422", code)
	}

	code, raw := getCerts(t, router, id)
	if code != http.StatusOK {
		t.Fatalf("GET certs: got %d: %s", code, raw)
	}
	if st := decodeCerts(t, raw); st.HasCerts {
		t.Fatalf("garbage must not be stored: %s", raw)
	}
}

func TestGetHostCertsNoPrivateKeyLeak(t *testing.T) {
	router := setupCertAPI(t)
	id := createHost(t, router, "remote1", "tcp://127.0.0.1:2376")
	caPEM, certPEM, keyPEM := genCertTestTriple(t)
	if code := putCerts(t, router, id, caPEM, certPEM, keyPEM); code != http.StatusOK {
		t.Fatalf("PUT valid certs: got %d", code)
	}

	code, raw := getCerts(t, router, id)
	if code != http.StatusOK {
		t.Fatalf("GET certs: got %d: %s", code, raw)
	}
	st := decodeCerts(t, raw)
	if !st.HasCerts || st.Fingerprint == "" || st.NotAfter == "" {
		t.Fatalf("expected metadata (has_certs, fingerprint, not_after): %s", raw)
	}
	// Leak assertion: no PEM armor of any key type may appear in the response.
	if strings.Contains(raw, "PRIVATE KEY") {
		t.Fatalf("GET /certs response leaked key material: %s", raw)
	}
	if strings.Contains(raw, "BEGIN CERTIFICATE") {
		t.Fatalf("GET /certs response leaked certificate PEM: %s", raw)
	}
}

func TestDeleteHostCertsRoundTrip(t *testing.T) {
	router := setupCertAPI(t)
	id := createHost(t, router, "remote1", "tcp://127.0.0.1:2376")
	caPEM, certPEM, keyPEM := genCertTestTriple(t)

	if code := putCerts(t, router, id, caPEM, certPEM, keyPEM); code != http.StatusOK {
		t.Fatalf("PUT valid certs: got %d", code)
	}
	if code, raw := getCerts(t, router, id); code != http.StatusOK || decodeCerts(t, raw).HasCerts != true {
		t.Fatalf("certs present after PUT: %d %s", code, raw)
	}

	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/hosts/%d/certs", id), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("DELETE certs: got %d: %s", w.Code, w.Body.String())
	}

	code, raw := getCerts(t, router, id)
	if code != http.StatusOK {
		t.Fatalf("GET certs after DELETE: got %d: %s", code, raw)
	}
	if st := decodeCerts(t, raw); st.HasCerts {
		t.Fatalf("expected has_certs=false after DELETE: %s", raw)
	}
}

// --- Test connection endpoint ---

func postHostTest(t *testing.T, router http.Handler, hostID int64) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/hosts/%d/test", hostID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func TestHostTestConnectionUnreachable(t *testing.T) {
	router := setupCertAPI(t)
	id := createHost(t, router, "remote1", "tcp://127.0.0.1:1")

	code, raw := postHostTest(t, router, id)
	if code != http.StatusOK {
		t.Fatalf("POST /test: got %d, want 200 with ok=false: %s", code, raw)
	}
	var out struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Latency int64  `json:"latency_ms"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal test response: %v (body: %s)", err, raw)
	}
	if out.OK {
		t.Fatalf("expected ok=false for unreachable daemon: %s", raw)
	}
	if out.Error == "" {
		t.Fatalf("expected non-empty error: %s", raw)
	}
}

func TestHostTestConnectionNotFound(t *testing.T) {
	router := setupCertAPI(t)

	code, raw := postHostTest(t, router, 999)
	if code != http.StatusNotFound {
		t.Fatalf("POST /test for missing host: got %d, want 404: %s", code, raw)
	}
}
