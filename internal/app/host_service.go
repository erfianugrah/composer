package app

import (
	"context"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/domain/host"
	"go.uber.org/zap"
)

// HostService orchestrates docker host CRUD and name-to-ID resolution.
type HostService struct {
	repo host.Repository
	log  *zap.Logger
}

// NewHostService creates a HostService.
func NewHostService(repo host.Repository, log *zap.Logger) *HostService {
	return &HostService{repo: repo, log: log}
}

// List returns all registered docker hosts.
func (s *HostService) List(ctx context.Context) ([]*host.Host, error) {
	return s.repo.List(ctx)
}

// Get returns a single host by primary key.
func (s *HostService) Get(ctx context.Context, id int64) (*host.Host, error) {
	return s.repo.GetByID(ctx, id)
}

// Create validates and persists a new docker host.
func (s *HostService) Create(ctx context.Context, h *host.Host) error {
	if err := h.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	h.CreatedAt, h.UpdatedAt = now, now
	return s.repo.Create(ctx, h)
}

// Update validates and persists changes to a docker host.
func (s *HostService) Update(ctx context.Context, h *host.Host) error {
	if err := h.Validate(); err != nil {
		return err
	}
	h.UpdatedAt = time.Now().UTC()
	return s.repo.Update(ctx, h)
}

// Delete removes a host by ID after verifying no stacks still reference it.
func (s *HostService) Delete(ctx context.Context, id int64) error {
	n, err := s.repo.CountStacks(ctx, id)
	if err != nil {
		return fmt.Errorf("checking stack references: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("host still has %d stack(s) assigned; reassign them first", n)
	}
	return s.repo.Delete(ctx, id)
}

// ResolveHostID maps an API-facing host name to a docker_hosts.id.
// "" and host.DefaultName both mean the default host -> nil, nil.
func (s *HostService) ResolveHostID(ctx context.Context, name string) (*int64, error) {
	if name == "" || name == host.DefaultName {
		return nil, nil
	}
	h, err := s.repo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolving docker host: %w", err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host %q", name)
	}
	return &h.ID, nil
}

// ResolveHostIDVia is the free-function form of HostService.ResolveHostID
// for services that hold host.Repository directly.
func ResolveHostIDVia(ctx context.Context, repo host.Repository, name string) (*int64, error) {
	if name == "" || name == host.DefaultName {
		return nil, nil
	}
	h, err := repo.GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolving docker host: %w", err)
	}
	if h == nil {
		return nil, fmt.Errorf("unknown docker host %q", name)
	}
	return &h.ID, nil
}
