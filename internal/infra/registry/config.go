// Package registry builds an ephemeral DOCKER_CONFIG directory from a set
// of credentials so `docker compose pull/up` can authenticate against
// private registries without polluting the host's ~/.docker/config.json.
//
// Lifecycle:
//   dir, cleanup, err := registry.BuildConfigDir(creds)
//   if err != nil { ... }
//   defer cleanup()
//   ctx = docker.WithDockerConfigDir(ctx, dir)
//   compose.Pull(ctx, ...)
package registry

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/erfianugrah/composer/internal/domain/registry"
)

// dockerConfig is the subset of ~/.docker/config.json we write. The "auths"
// map is keyed by registry URL; each value's "auth" field is base64("user:pw")
// per the Docker engine spec.
type dockerConfig struct {
	Auths map[string]dockerAuth `json:"auths"`
}

type dockerAuth struct {
	Auth     string `json:"auth"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Email    string `json:"email,omitempty"`
}

// BuildConfigDir writes a config.json with the given credentials to a fresh
// tempdir and returns (dir, cleanup, err). If creds is empty, returns
// ("", noopCleanup, nil) — callers should not set DOCKER_CONFIG in that case.
//
// The returned cleanup function removes the tempdir and is safe to call
// multiple times. Always defer it: secrets sit on disk while it's alive.
func BuildConfigDir(creds []*registry.Credential) (string, func(), error) {
	if len(creds) == 0 {
		return "", func() {}, nil
	}

	cfg := dockerConfig{Auths: make(map[string]dockerAuth, len(creds))}
	for _, c := range creds {
		if c == nil || c.Registry == "" {
			continue
		}
		// Some credentials may have empty Secret if decryption failed at load
		// time — skip them rather than write an empty auth that would fail at
		// runtime with a confusing error.
		if c.Secret == "" {
			continue
		}
		token := base64.StdEncoding.EncodeToString([]byte(c.Username + ":" + c.Secret))
		cfg.Auths[c.Registry] = dockerAuth{
			Auth:  token,
			Email: c.Email,
		}
	}
	if len(cfg.Auths) == 0 {
		return "", func() {}, nil
	}

	dir, err := os.MkdirTemp("", "composer-docker-config-")
	if err != nil {
		return "", func() {}, fmt.Errorf("mkdir DOCKER_CONFIG: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	data, err := json.Marshal(&cfg)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("marshal docker config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0600); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write docker config.json: %w", err)
	}
	return dir, cleanup, nil
}
