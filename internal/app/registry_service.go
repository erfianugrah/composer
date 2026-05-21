package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"

	"github.com/erfianugrah/composer/internal/domain/registry"
)

// RegistryService is the orchestration layer for Docker registry credentials.
// Thin wrapper over the repository — the domain model already enforces
// validation; this layer adds logging and a stable API surface.
type RegistryService struct {
	repo registry.Repository
	log  *zap.Logger
}

func NewRegistryService(repo registry.Repository, log *zap.Logger) *RegistryService {
	if log == nil {
		log = zap.NewNop()
	}
	return &RegistryService{repo: repo, log: log.Named("registry")}
}

// Repo exposes the underlying repository so other services can wire it into
// their own withRegistryAuth helpers without a second injection point.
func (s *RegistryService) Repo() registry.Repository { return s.repo }

func (s *RegistryService) List(ctx context.Context) ([]*registry.Credential, error) {
	return s.repo.List(ctx)
}

func (s *RegistryService) ListForStack(ctx context.Context, stackName string) ([]*registry.Credential, error) {
	return s.repo.ListForStack(ctx, stackName)
}

func (s *RegistryService) Get(ctx context.Context, id int64) (*registry.Credential, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrNotFound
	}
	return c, nil
}

func (s *RegistryService) Upsert(ctx context.Context, c *registry.Credential) error {
	if err := s.repo.Upsert(ctx, c); err != nil {
		return fmt.Errorf("registry upsert: %w", err)
	}
	s.log.Info("registry credential upserted",
		zap.Int64("id", c.ID),
		zap.String("registry", c.Registry),
		zap.String("stack", c.StackName),
		zap.Bool("global", c.IsGlobal()),
	)
	return nil
}

func (s *RegistryService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("registry delete: %w", err)
	}
	s.log.Info("registry credential deleted", zap.Int64("id", id))
	return nil
}

// envSeed is the JSON shape accepted by COMPOSER_REGISTRY_AUTHS /
// COMPOSER_REGISTRY_AUTHS_FILE. One entry per registry. stack_name is
// optional — omit for a global credential.
type envSeed struct {
	Registry  string `json:"registry"`
	Username  string `json:"username"`
	Secret    string `json:"secret"`
	Email     string `json:"email,omitempty"`
	StackName string `json:"stack_name,omitempty"`
}

// BootstrapFromEnv seeds registry credentials from env vars on startup.
// Mirrors the SOPS age-key bootstrap convention so headless / immutable
// deploys (Unraid, k8s, fly.io machines) can provision creds without
// going through the UI.
//
// Priority:
//  1. COMPOSER_REGISTRY_AUTHS         inline JSON array of envSeed entries
//  2. COMPOSER_REGISTRY_AUTHS_FILE    path to a JSON file with the same shape
//
// Idempotent by default: existing rows with the same (registry, stack_name)
// are left untouched. Set COMPOSER_REGISTRY_AUTHS_OVERWRITE=true to force
// reseeding on every boot — useful when the env is the source of truth.
func (s *RegistryService) BootstrapFromEnv(ctx context.Context) error {
	raw := os.Getenv("COMPOSER_REGISTRY_AUTHS")
	if raw == "" {
		if path := os.Getenv("COMPOSER_REGISTRY_AUTHS_FILE"); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading COMPOSER_REGISTRY_AUTHS_FILE %q: %w", path, err)
			}
			raw = string(data)
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var seeds []envSeed
	if err := json.Unmarshal([]byte(raw), &seeds); err != nil {
		return fmt.Errorf("parsing COMPOSER_REGISTRY_AUTHS JSON: %w", err)
	}
	if len(seeds) == 0 {
		return nil
	}

	overwrite := strings.EqualFold(os.Getenv("COMPOSER_REGISTRY_AUTHS_OVERWRITE"), "true")

	// Snapshot existing rows so we can decide insert-vs-skip per (registry, stack).
	existing, err := s.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("listing existing registry credentials: %w", err)
	}
	key := func(reg, stack string) string { return reg + "\x00" + stack }
	have := make(map[string]*registry.Credential, len(existing))
	for _, c := range existing {
		have[key(c.Registry, c.StackName)] = c
	}

	var seeded, skipped int
	for i, sd := range seeds {
		c := &registry.Credential{
			Registry:  sd.Registry,
			Username:  sd.Username,
			Secret:    sd.Secret,
			Email:     sd.Email,
			StackName: sd.StackName,
		}
		if err := c.Validate(); err != nil {
			s.log.Warn("registry bootstrap: invalid seed entry",
				zap.Int("index", i),
				zap.String("registry", sd.Registry),
				zap.Error(err))
			continue
		}
		if prior, ok := have[key(c.Registry, c.StackName)]; ok && !overwrite {
			skipped++
			s.log.Debug("registry bootstrap: keeping existing row",
				zap.Int64("id", prior.ID),
				zap.String("registry", prior.Registry),
				zap.String("stack", prior.StackName))
			continue
		} else if ok {
			c.ID = prior.ID // overwrite-in-place
		}
		if err := s.repo.Upsert(ctx, c); err != nil {
			s.log.Error("registry bootstrap: upsert failed",
				zap.String("registry", c.Registry),
				zap.Error(err))
			continue
		}
		seeded++
	}
	s.log.Info("registry bootstrap complete",
		zap.Int("seeded", seeded),
		zap.Int("skipped_existing", skipped),
		zap.Bool("overwrite", overwrite))
	return nil
}
