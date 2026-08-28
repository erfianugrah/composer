package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/erfianugrah/composer/internal/api/dto"
	authmw "github.com/erfianugrah/composer/internal/api/middleware"
	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/host"
	"github.com/erfianugrah/composer/internal/infra/docker"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// HostHandler registers /api/v1/hosts endpoints for docker host management.
// List/Get require viewer; Create/Update/Delete require admin. Cert
// endpoints: GET is viewer, PUT/DELETE are admin. GET /certs returns
// metadata only - key material is never returned.
type HostHandler struct {
	svc         *app.HostService
	certs       *store.HostCertsRepo // nil disables cert endpoints (dumper only)
	invalidator app.HostCacheInvalidator
	dataDir     string // COMPOSER_DATA_DIR; materialized certs live under <dataDir>/certs
}

func NewHostHandler(svc *app.HostService) *HostHandler {
	return &HostHandler{svc: svc}
}

// SetCerts wires optional cert storage, the factory cache invalidator, and
// the data dir the materialized certs live under. Zero values are safe: cert
// endpoints still register (the OpenAPI dumper relies on that), GET reports
// has_certs=false, and PUT/DELETE report the store as unavailable.
func (h *HostHandler) SetCerts(certs *store.HostCertsRepo, invalidator app.HostCacheInvalidator, dataDir string) {
	h.certs = certs
	h.invalidator = invalidator
	h.dataDir = dataDir
}

func (h *HostHandler) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listHosts",
		Method:      http.MethodGet,
		Path:        "/api/v1/hosts",
		Summary:     "List docker hosts",
		Tags:        []string{"hosts"},
		Errors:      errsViewer,
	}, h.List)

	huma.Register(api, huma.Operation{
		OperationID: "createHost",
		Method:      http.MethodPost,
		Path:        "/api/v1/hosts",
		Summary:     "Create a docker host",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.Create)

	huma.Register(api, huma.Operation{
		OperationID: "getHost",
		Method:      http.MethodGet,
		Path:        "/api/v1/hosts/{id}",
		Summary:     "Get a docker host",
		Tags:        []string{"hosts"},
		Errors:      errsViewerNotFound,
	}, h.Get)

	huma.Register(api, huma.Operation{
		OperationID: "updateHost",
		Method:      http.MethodPut,
		Path:        "/api/v1/hosts/{id}",
		Summary:     "Update a docker host",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.Update)

	huma.Register(api, huma.Operation{
		OperationID: "deleteHost",
		Method:      http.MethodDelete,
		Path:        "/api/v1/hosts/{id}",
		Summary:     "Delete a docker host",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.Delete)

	huma.Register(api, huma.Operation{
		OperationID: "getHostCerts",
		Method:      http.MethodGet,
		Path:        "/api/v1/hosts/{id}/certs",
		Summary:     "Get docker host certificate metadata",
		Description: "Returns fingerprint + expiry only. Key material is never returned.",
		Tags:        []string{"hosts"},
		Errors:      errsViewerNotFound,
	}, h.GetCerts)

	huma.Register(api, huma.Operation{
		OperationID: "putHostCerts",
		Method:      http.MethodPut,
		Path:        "/api/v1/hosts/{id}/certs",
		Summary:     "Store docker host mTLS certificates",
		Description: "Replaces the host's stored mTLS triple. The triple is validated (PEM parse, key matches cert, cert chains to CA) before storage and stored encrypted. 422 on invalid material.",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.PutCerts)

	huma.Register(api, huma.Operation{
		OperationID: "deleteHostCerts",
		Method:      http.MethodDelete,
		Path:        "/api/v1/hosts/{id}/certs",
		Summary:     "Delete docker host certificates",
		Description: "Removes the stored certs and the materialized on-disk cert dir, and evicts the cached host client.",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.DeleteCerts)

	huma.Register(api, huma.Operation{
		OperationID: "testHost",
		Method:      http.MethodPost,
		Path:        "/api/v1/hosts/{id}/test",
		Summary:     "Test a docker host connection",
		Description: "Builds a throwaway docker client (DB certs > cert_dir > plain) and pings the daemon with a 3s timeout. Never touches the factory cache. Returns ok=false with the error when the daemon is unreachable; key material is never returned.",
		Tags:        []string{"hosts"},
		Errors:      errsAdminMutation,
	}, h.Test)
}

// --- Handler methods ---

func (h *HostHandler) List(ctx context.Context, _ *struct{}) (*dto.ListHostsOutput, error) {
	hosts, err := h.svc.List(ctx)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	meta := h.certMetaMap(ctx)
	resp := &dto.ListHostsOutput{}
	resp.Body.Hosts = make([]dto.DockerHostOutput, len(hosts))
	for i, host := range hosts {
		resp.Body.Hosts[i] = toHostOutput(host, meta[host.ID])
	}
	return resp, nil
}

func (h *HostHandler) Create(ctx context.Context, input *dto.CreateHostInput) (*dto.CreateHostOutput, error) {
	host := &host.Host{
		Name:     input.Body.Name,
		Endpoint: input.Body.Endpoint,
		CertDir:  input.Body.CertDir,
	}
	if err := h.svc.Create(ctx, host); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	resp := &dto.CreateHostOutput{}
	resp.Body.Host = toHostOutput(host, h.certMeta(ctx, host.ID))
	return resp, nil
}

func (h *HostHandler) Get(ctx context.Context, input *dto.HostIDInput) (*dto.GetHostOutput, error) {
	host, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if host == nil {
		return nil, huma.Error404NotFound("host not found")
	}

	resp := &dto.GetHostOutput{}
	resp.Body.Host = toHostOutput(host, h.certMeta(ctx, host.ID))
	return resp, nil
}

func (h *HostHandler) Update(ctx context.Context, input *dto.UpdateHostInput) (*dto.GetHostOutput, error) {
	host, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if host == nil {
		return nil, huma.Error404NotFound("host not found")
	}

	host.Name = input.Body.Name
	host.Endpoint = input.Body.Endpoint
	host.CertDir = input.Body.CertDir

	if err := h.svc.Update(ctx, host); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}

	resp := &dto.GetHostOutput{}
	resp.Body.Host = toHostOutput(host, h.certMeta(ctx, host.ID))
	return resp, nil
}

func (h *HostHandler) Delete(ctx context.Context, input *dto.HostIDInput) (*struct{}, error) {
	_, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}

	if err := h.svc.Delete(ctx, input.ID); err != nil {
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	return nil, nil
}

// --- Cert endpoints ---

func (h *HostHandler) GetCerts(ctx context.Context, input *dto.HostIDInput) (*dto.GetHostCertsOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleViewer); err != nil {
		return nil, err
	}
	host, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if host == nil {
		return nil, huma.Error404NotFound("host not found")
	}

	resp := &dto.GetHostCertsOutput{}
	resp.Body.Certs = h.certsOutput(ctx, input.ID)
	return resp, nil
}

func (h *HostHandler) PutCerts(ctx context.Context, input *dto.PutHostCertsInput) (*dto.PutHostCertsOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	host, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if host == nil {
		return nil, huma.Error404NotFound("host not found")
	}
	if h.certs == nil {
		return nil, serverError(ctx, fmt.Errorf("host certs store unavailable"))
	}

	fingerprint, notAfter, err := docker.ValidateHostCerts(input.Body.CACert, input.Body.Cert, input.Body.Key)
	if err != nil {
		// Validation errors never contain PEM material - safe to surface.
		return nil, huma.Error422UnprocessableEntity(err.Error())
	}
	notAfterStr := notAfter.UTC().Format(time.RFC3339)
	if err := h.certs.Upsert(ctx, input.ID, input.Body.CACert, input.Body.Cert, input.Body.Key, fingerprint, notAfterStr); err != nil {
		return nil, serverError(ctx, fmt.Errorf("storing host certs: %w", err))
	}
	h.invalidateHost(input.ID)

	resp := &dto.PutHostCertsOutput{}
	resp.Body.Certs = dto.HostCertsOutput{HasCerts: true, Fingerprint: fingerprint, NotAfter: notAfterStr}
	return resp, nil
}

func (h *HostHandler) DeleteCerts(ctx context.Context, input *dto.HostIDInput) (*struct{}, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	host, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if host == nil {
		return nil, huma.Error404NotFound("host not found")
	}
	if h.certs != nil {
		if err := h.certs.Delete(ctx, input.ID); err != nil {
			return nil, serverError(ctx, fmt.Errorf("deleting host certs: %w", err))
		}
	}
	// Remove the materialized dir (no-op when absent).
	if h.dataDir != "" {
		if err := os.RemoveAll(docker.HostCertsDir(h.dataDir, input.ID)); err != nil {
			serverError(ctx, fmt.Errorf("removing materialized certs: %w", err))
		}
	}
	h.invalidateHost(input.ID)
	return nil, nil
}

// --- Test connection ---

// testHostTimeout bounds the daemon ping on the test endpoint.
const testHostTimeout = 3 * time.Second

// Test probes a host with a throwaway docker client: DB certs (materialized) >
// host.CertDir > plain. The factory cache is never touched; the client is
// closed on every path. Connection/ping failures are returned as
// ok=false with the error, not as HTTP errors.
func (h *HostHandler) Test(ctx context.Context, input *dto.HostIDInput) (*dto.TestHostOutput, error) {
	if err := authmw.CheckRole(ctx, auth.RoleAdmin); err != nil {
		return nil, err
	}
	host, err := h.svc.Get(ctx, input.ID)
	if err != nil {
		return nil, serverError(ctx, err)
	}
	if host == nil {
		return nil, huma.Error404NotFound("host not found")
	}

	var tls *docker.TLSConfig
	if h.certs != nil && h.dataDir != "" {
		if ca, cert, key, err := h.certs.GetHostCerts(ctx, input.ID); err != nil {
			return nil, serverError(ctx, fmt.Errorf("loading host certs: %w", err))
		} else if cert != "" {
			if dir, err := docker.MaterializeHostCerts(h.dataDir, input.ID, []byte(ca), []byte(cert), []byte(key)); err != nil {
				return nil, serverError(ctx, fmt.Errorf("materializing host certs: %w", err))
			} else {
				tls = &docker.TLSConfig{CertDir: dir}
			}
		}
	}
	if tls == nil && host.CertDir != "" {
		tls = &docker.TLSConfig{CertDir: host.CertDir}
	}

	client, err := docker.NewClient(host.Endpoint, tls)
	if err != nil {
		return testHostOutput(false, err.Error(), 0), nil
	}
	defer client.Close()

	pingCtx, cancel := context.WithTimeout(ctx, testHostTimeout)
	defer cancel()
	start := time.Now()
	if err := client.Ping(pingCtx); err != nil {
		// Docker client/ping errors carry daemon-side details, never PEM material.
		return testHostOutput(false, err.Error(), 0), nil
	}
	return testHostOutput(true, "", time.Since(start).Milliseconds()), nil
}

func testHostOutput(ok bool, errMsg string, latencyMs int64) *dto.TestHostOutput {
	return &dto.TestHostOutput{Body: dto.TestHostOutputBody{OK: ok, Error: errMsg, LatencyMs: latencyMs}}
}

// --- Helpers ---

// toHostOutput maps a host + optional cert metadata to the API output.
func toHostOutput(h *host.Host, meta *store.HostCertMeta) dto.DockerHostOutput {
	out := dto.DockerHostOutput{
		ID:        h.ID,
		Name:      h.Name,
		Endpoint:  h.Endpoint,
		CertDir:   h.CertDir,
		TLS:       h.CertDir != "" || meta != nil,
		HasCerts:  meta != nil,
		CreatedAt: h.CreatedAt.Format(time.RFC3339),
		UpdatedAt: h.UpdatedAt.Format(time.RFC3339),
	}
	if meta != nil {
		out.CertNotAfter = meta.NotAfter
	}
	return out
}

// certMeta loads one host's cert metadata (never key material). Returns nil
// when the store is unavailable, has no row, or errors, so host lookups never
// fail on a certs lookup.
func (h *HostHandler) certMeta(ctx context.Context, hostID int64) *store.HostCertMeta {
	if h.certs == nil {
		return nil
	}
	m, err := h.certs.Meta(ctx, hostID)
	if err != nil {
		serverError(ctx, err)
		return nil
	}
	return m
}

// certMetaMap loads cert metadata for every stored host (no decryption).
func (h *HostHandler) certMetaMap(ctx context.Context) map[int64]*store.HostCertMeta {
	if h.certs == nil {
		return nil
	}
	m, err := h.certs.ListMeta(ctx)
	if err != nil {
		serverError(ctx, err)
		return nil
	}
	return m
}

// certsOutput builds the metadata-only cert status for a host.
func (h *HostHandler) certsOutput(ctx context.Context, hostID int64) dto.HostCertsOutput {
	meta := h.certMeta(ctx, hostID)
	if meta == nil {
		return dto.HostCertsOutput{HasCerts: false}
	}
	return dto.HostCertsOutput{HasCerts: true, Fingerprint: meta.Fingerprint, NotAfter: meta.NotAfter}
}

// invalidateHost evicts the host's cached docker client/compose after cert
// changes (nil-tolerant).
func (h *HostHandler) invalidateHost(hostID int64) {
	if h.invalidator != nil {
		h.invalidator.Invalidate(hostID)
	}
}
