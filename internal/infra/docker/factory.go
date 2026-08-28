package docker

import (
	"context"
	"fmt"
	"sync"

	"github.com/erfianugrah/composer/internal/domain/host"
	"go.uber.org/zap"
)

// HostStore is the slice of host.Repository the Factory needs.
type HostStore interface {
	GetByID(ctx context.Context, id int64) (*host.Host, error)
	GetByName(ctx context.Context, name string) (*host.Host, error)
}

// HostCertStore is the slice of host-certs storage the Factory needs.
// Implemented by store.HostCertsRepo; nil disables DB-stored certs. When a
// host has stored certs they are materialized to disk and WIN over the host's
// static cert_dir.
type HostCertStore interface {
	GetHostCerts(ctx context.Context, hostID int64) (ca, cert, key string, err error)
}

// Factory resolves per-host docker access. The default host's client/compose
// are built once at boot and shared; remote hosts are constructed lazily on
// first use and cached by docker_hosts.id. Thread-safe.
type Factory struct {
	defClient  *Client
	defCompose *Compose
	store      HostStore
	certs      HostCertStore // optional; DB-stored mTLS triples (win over cert_dir)
	dataDir    string        // materialized cert root; "" disables materialization
	log        *zap.Logger

	mu       sync.Mutex
	clients  map[int64]*Client
	composes map[int64]*Compose
}

// NewFactory creates a Factory with the default host's client and compose.
func NewFactory(defClient *Client, defCompose *Compose, store HostStore, log *zap.Logger) *Factory {
	return &Factory{
		defClient: defClient, defCompose: defCompose, store: store,
		log: log,
		clients: map[int64]*Client{}, composes: map[int64]*Compose{},
	}
}

// SetCerts wires optional DB-stored mTLS material: certs may be nil (no
// DB-stored certs) and dataDir may be "" (in which case only the hosts' static
// cert_dir is used). Wired from main after construction.
func (f *Factory) SetCerts(certs HostCertStore, dataDir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.certs = certs
	f.dataDir = dataDir
}

// DefaultClient returns the default host's client (never nil).
func (f *Factory) DefaultClient() *Client { return f.defClient }

// DefaultCompose returns the default host's compose (never nil).
func (f *Factory) DefaultCompose() *Compose { return f.defCompose }

// ClientFor returns a client for the given host ID. nil hostID means default.
func (f *Factory) ClientFor(ctx context.Context, hostID *int64) (*Client, error) {
	if hostID == nil {
		return f.defClient, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[*hostID]; ok {
		return c, nil
	}
	h, err := f.store.GetByID(ctx, *hostID)
	if err != nil {
		return nil, fmt.Errorf("loading docker host %d: %w", *hostID, err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host id %d", *hostID)
	}
	tls, err := f.tlsForHost(ctx, h)
	if err != nil {
		return nil, err
	}
	c, err := newClientForHost(h.Endpoint, tls)
	if err != nil {
		return nil, err
	}
	f.clients[*hostID] = c
	return c, nil
}

// ComposeFor returns a compose wrapper for the given host ID. nil means default.
func (f *Factory) ComposeFor(ctx context.Context, hostID *int64) (*Compose, error) {
	if hostID == nil {
		return f.defCompose, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.composes[*hostID]; ok {
		return c, nil
	}
	h, err := f.store.GetByID(ctx, *hostID)
	if err != nil {
		return nil, fmt.Errorf("loading docker host %d: %w", *hostID, err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host id %d", *hostID)
	}
	tls, err := f.tlsForHost(ctx, h)
	if err != nil {
		return nil, err
	}
	// Validate TLS material eagerly.
	if _, err := newClientForHost(h.Endpoint, tls); err != nil {
		return nil, err
	}
	c := NewComposeTLS(h.Endpoint, tls, f.log)
	f.composes[*hostID] = c
	return c, nil
}

// ClientForName resolves API-facing names ("" / "local" = default).
func (f *Factory) ClientForName(ctx context.Context, name string) (*Client, error) {
	if name == "" || name == host.DefaultName {
		return f.defClient, nil
	}
	h, err := f.store.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolving docker host: %w", err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host %q", name)
	}
	return f.ClientFor(ctx, &h.ID)
}

// Invalidate closes and drops the cached client and compose for the given
// host ID. Call after the host's config (endpoint, cert_dir) or its stored
// certs change, so the next use rebuilds against the new material. Safe to
// call for unknown or never-cached IDs.
func (f *Factory) Invalidate(hostID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[hostID]; ok {
		if c.cli != nil {
			_ = c.Close()
		}
		delete(f.clients, hostID)
	}
	delete(f.composes, hostID)
}

// Close shuts the default client and every cached remote client.
func (f *Factory) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.clients {
		if c.cli != nil {
			_ = c.Close()
		}
	}
	if f.defClient != nil && f.defClient.cli != nil {
		_ = f.defClient.Close()
	}
}

// newClientForHost is a package-level var so unit tests can stub construction.
var newClientForHost = func(endpoint string, tls *TLSConfig) (*Client, error) {
	return NewClient(endpoint, tls)
}

// tlsForHost resolves per-host TLS material. DB-stored certs (HostCertStore)
// win over the host's static cert_dir: they are materialized under
// HostCertsDir first. Returns nil (plain transport) when no TLS material is
// configured.
func (f *Factory) tlsForHost(ctx context.Context, h *host.Host) (*TLSConfig, error) {
	if f.certs != nil && f.dataDir != "" {
		if ca, cert, key, err := f.certs.GetHostCerts(ctx, h.ID); err != nil {
			return nil, fmt.Errorf("loading stored certs for host %d: %w", h.ID, err)
		} else if ca != "" || cert != "" || key != "" {
			dir, err := MaterializeHostCerts(f.dataDir, h.ID, []byte(ca), []byte(cert), []byte(key))
			if err != nil {
				return nil, fmt.Errorf("materializing stored certs for host %d: %w", h.ID, err)
			}
			return &TLSConfig{CertDir: dir}, nil
		}
	}
	if h.CertDir == "" {
		return nil, nil
	}
	return &TLSConfig{CertDir: h.CertDir}, nil
}
