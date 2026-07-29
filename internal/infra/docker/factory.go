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

// Factory resolves per-host docker access. The default host's client/compose
// are built once at boot and shared; remote hosts are constructed lazily on
// first use and cached by docker_hosts.id. Thread-safe.
type Factory struct {
	defClient  *Client
	defCompose *Compose
	store      HostStore
	log        *zap.Logger

	mu       sync.Mutex
	clients  map[int64]*Client
	composes map[int64]*Compose
}

// NewFactory creates a Factory with the default host's client and compose.
func NewFactory(defClient *Client, defCompose *Compose, store HostStore, log *zap.Logger) *Factory {
	return &Factory{
		defClient: defClient, defCompose: defCompose, store: store, log: log,
		clients: map[int64]*Client{}, composes: map[int64]*Compose{},
	}
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
	c, err := newClientForHost(h.Endpoint, tlsForHost(h))
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
	// Validate TLS material eagerly.
	if _, err := newClientForHost(h.Endpoint, tlsForHost(h)); err != nil {
		return nil, err
	}
	c := NewComposeTLS(h.Endpoint, tlsForHost(h), f.log)
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

func tlsForHost(h *host.Host) *TLSConfig {
	if h.CertDir == "" {
		return nil
	}
	return &TLSConfig{CertDir: h.CertDir}
}
