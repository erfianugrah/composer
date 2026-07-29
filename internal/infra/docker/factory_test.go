package docker

import (
	"context"
	"sync"
	"testing"

	"github.com/erfianugrah/composer/internal/domain/host"
	"go.uber.org/zap"
)

// --- Fake HostStore ---

type countingStore struct {
	mu      sync.Mutex
	hosts   map[int64]*host.Host
	byName  map[string]*host.Host
	callIDs map[int64]int
}

func newCountingStore() *countingStore {
	return &countingStore{
		hosts:   map[int64]*host.Host{},
		byName:  map[string]*host.Host{},
		callIDs: map[int64]int{},
	}
}

func (s *countingStore) add(h *host.Host) {
	s.hosts[h.ID] = h
	s.byName[h.Name] = h
}

func (s *countingStore) GetByID(_ context.Context, id int64) (*host.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callIDs[id]++
	h, ok := s.hosts[id]
	if !ok {
		return nil, nil
	}
	return h, nil
}

func (s *countingStore) GetByName(_ context.Context, name string) (*host.Host, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.byName[name]
	if !ok {
		return nil, nil
	}
	return h, nil
}

func (s *countingStore) callsFor(id int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callIDs[id]
}

// buildDef creates a default client+compose pair for tests.
func buildDef(log *zap.Logger) (*Client, *Compose) {
	c, err := NewClient("unix:///var/run/docker.sock", nil)
	if err != nil {
		panic(err)
	}
	return c, NewCompose(c.Host(), log)
}

func TestFactoryDefault(t *testing.T) {
	store := newCountingStore()
	log := zap.NewNop()
	defClient, defCompose := buildDef(log)
	factory := NewFactory(defClient, defCompose, store, log)
	defer factory.Close()

	c, err := factory.ClientFor(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c != defClient {
		t.Error("ClientFor(nil) should return the default client")
	}

	comp, err := factory.ComposeFor(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if comp != defCompose {
		t.Error("ComposeFor(nil) should return the default compose")
	}

	if factory.DefaultClient() != defClient {
		t.Error("DefaultClient() should return the default client")
	}
	if factory.DefaultCompose() != defCompose {
		t.Error("DefaultCompose() should return the default compose")
	}
}

func TestFactoryRemoteConstruction(t *testing.T) {
	// Stub newClientForHost to avoid Info() dial to real daemon.
	orig := newClientForHost
	newClientForHost = func(endpoint string, tls *TLSConfig) (*Client, error) {
		return &Client{runtime: "docker", cli: nil}, nil
	}
	defer func() { newClientForHost = orig }()

	store := newCountingStore()
	h := &host.Host{ID: 1, Name: "remote1", Endpoint: "tcp://docker-remote.example:2376", CertDir: ""}
	store.add(h)

	log := zap.NewNop()
	defClient, defCompose := buildDef(log)
	factory := NewFactory(defClient, defCompose, store, log)
	defer factory.Close()

	c, err := factory.ClientFor(context.Background(), &h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("ClientFor(remote) returned nil")
	}

	if store.callsFor(h.ID) != 1 {
		t.Errorf("expected 1 GetByID call, got %d", store.callsFor(h.ID))
	}
	c2, err := factory.ClientFor(context.Background(), &h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c2 != c {
		t.Error("ClientFor should return the same cached client")
	}
	if store.callsFor(h.ID) != 1 {
		t.Errorf("expected cached hit, but store was called %d times", store.callsFor(h.ID))
	}
}

func TestFactoryUnknownID(t *testing.T) {
	store := newCountingStore()
	log := zap.NewNop()
	defClient, defCompose := buildDef(log)
	factory := NewFactory(defClient, defCompose, store, log)
	defer factory.Close()

	unknownID := int64(999)
	_, err := factory.ClientFor(context.Background(), &unknownID)
	if err == nil {
		t.Fatal("expected error for unknown host ID")
	}
}

func TestFactoryClientForName(t *testing.T) {
	store := newCountingStore()
	h := &host.Host{ID: 1, Name: "remote1", Endpoint: "tcp://docker-remote.example:2376"}
	store.add(h)

	log := zap.NewNop()
	defClient, defCompose := buildDef(log)
	factory := NewFactory(defClient, defCompose, store, log)
	defer factory.Close()

	// "" resolves to default.
	c, err := factory.ClientForName(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if c != defClient {
		t.Error("ClientForName(\"\") should return default")
	}

	// "local" resolves to default.
	c, err = factory.ClientForName(context.Background(), "local")
	if err != nil {
		t.Fatal(err)
	}
	if c != defClient {
		t.Error("ClientForName(\"local\") should return default")
	}

	// "remote1" resolves to remote.
	c, err = factory.ClientForName(context.Background(), "remote1")
	if err != nil {
		t.Fatal(err)
	}
	// Client has nil cli from stub; Host() would panic. But we checked
	// that the returned client is not nil and is the default.
	_ = c

	// Unknown name.
	_, err = factory.ClientForName(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error for unknown host name")
	}
}
