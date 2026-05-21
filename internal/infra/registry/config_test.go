package registry_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	domreg "github.com/erfianugrah/composer/internal/domain/registry"
	infreg "github.com/erfianugrah/composer/internal/infra/registry"
)

func TestBuildConfigDir_Empty(t *testing.T) {
	dir, cleanup, err := infreg.BuildConfigDir(nil)
	defer cleanup()
	if err != nil || dir != "" {
		t.Fatalf("BuildConfigDir(nil) = (%q, _, %v), want empty/nil", dir, err)
	}
}

func TestBuildConfigDir_WritesAuths(t *testing.T) {
	creds := []*domreg.Credential{
		{Registry: "ghcr.io", Username: "alice", Secret: "ghp_pat"},
		{Registry: "docker.io", Username: "bob", Secret: "hunter2", Email: "bob@example.com"},
	}
	dir, cleanup, err := infreg.BuildConfigDir(creds)
	defer cleanup()
	if err != nil {
		t.Fatalf("BuildConfigDir err: %v", err)
	}
	if dir == "" {
		t.Fatal("expected non-empty dir")
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}

	var got struct {
		Auths map[string]struct {
			Auth  string `json:"auth"`
			Email string `json:"email,omitempty"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Auths) != 2 {
		t.Fatalf("auths=%d want 2", len(got.Auths))
	}
	wantAuth := base64.StdEncoding.EncodeToString([]byte("alice:ghp_pat"))
	if got.Auths["ghcr.io"].Auth != wantAuth {
		t.Errorf("ghcr.io auth=%q want %q", got.Auths["ghcr.io"].Auth, wantAuth)
	}
	if got.Auths["docker.io"].Email != "bob@example.com" {
		t.Errorf("docker.io email=%q want bob@example.com", got.Auths["docker.io"].Email)
	}
}

func TestBuildConfigDir_SkipsEmptySecret(t *testing.T) {
	// Simulates a cred whose decryption failed — should be filtered, not crash.
	creds := []*domreg.Credential{
		{Registry: "ghcr.io", Username: "alice", Secret: ""},
		{Registry: "docker.io", Username: "bob", Secret: "ok"},
	}
	dir, cleanup, err := infreg.BuildConfigDir(creds)
	defer cleanup()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	var got struct {
		Auths map[string]any `json:"auths"`
	}
	json.Unmarshal(data, &got)
	if _, ok := got.Auths["ghcr.io"]; ok {
		t.Error("ghcr.io should be filtered (empty secret)")
	}
	if _, ok := got.Auths["docker.io"]; !ok {
		t.Error("docker.io should be present")
	}
}

func TestBuildConfigDir_FilePermissions(t *testing.T) {
	creds := []*domreg.Credential{{Registry: "ghcr.io", Username: "a", Secret: "b"}}
	dir, cleanup, _ := infreg.BuildConfigDir(creds)
	defer cleanup()
	info, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("config.json perm=%o want 0600 (secrets on disk)", perm)
	}
}
