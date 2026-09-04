package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/erfianugrah/composer/internal/domain/stack"
	"github.com/erfianugrah/composer/internal/infra/sops"
)

// --- test doubles for the SOPS restore lifecycle ---------------------------
//
// Both repos honour the request ctx like the real SQL repositories do, so a
// cancelled ctx makes lookups fail - which is exactly the condition the
// 2026-09-04 edge-services incident was hit under.

type sopsTestStackRepo struct {
	byName map[string]*stack.Stack
}

func (r *sopsTestStackRepo) Create(ctx context.Context, s *stack.Stack) error {
	r.byName[s.Name] = s
	return nil
}

func (r *sopsTestStackRepo) GetByName(ctx context.Context, name string) (*stack.Stack, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s, ok := r.byName[name]; ok {
		return s, nil
	}
	return nil, ErrNotFound
}

func (r *sopsTestStackRepo) List(ctx context.Context) ([]*stack.Stack, error) {
	return nil, nil
}

func (r *sopsTestStackRepo) Update(ctx context.Context, s *stack.Stack) error { return nil }

func (r *sopsTestStackRepo) Delete(ctx context.Context, name string) error { return nil }

type sopsTestGitCfgRepo struct {
	byName map[string]*stack.GitSource
}

func (r *sopsTestGitCfgRepo) Upsert(ctx context.Context, stackName string, cfg *stack.GitSource) error {
	r.byName[stackName] = cfg
	return nil
}

func (r *sopsTestGitCfgRepo) GetByStackName(ctx context.Context, stackName string) (*stack.GitSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c, ok := r.byName[stackName]; ok {
		return c, nil
	}
	return nil, ErrNotFound
}

func (r *sopsTestGitCfgRepo) Delete(ctx context.Context, stackName string) error { return nil }

func (r *sopsTestGitCfgRepo) UpdateSyncStatus(ctx context.Context, stackName string, status stack.GitSyncStatus, commitSHA string) error {
	return nil
}

// TestPrepareAction_CleanupRestoresEnvWithCancelledCtx is the regression test
// for the 2026-09-04 edge-services incident: the WS client disconnects after
// the streamed restart completes, the request ctx is cancelled, and the
// deferred ActionContext.Cleanup still must restore the SOPS-encrypted .env.
// Before the fix, resolveEnvFile's GitSource lookup failed on the cancelled
// ctx, envFile fell back to the stack root, and ReEncryptEnvFile silently
// no-opped - leaving the decrypted .env (plaintext secrets) on disk with the
// .sops backup orphaned next to it.
func TestPrepareAction_CleanupRestoresEnvWithCancelledCtx(t *testing.T) {
	oldAvail, oldDecEnv, oldDecComp := sopsAvailable, sopsDecryptEnv, sopsDecryptComp
	t.Cleanup(func() {
		sopsAvailable, sopsDecryptEnv, sopsDecryptComp = oldAvail, oldDecEnv, oldDecComp
	})
	sopsAvailable = func() bool { return true }
	sopsDecryptEnv = func(envPath, ageKey string) (bool, error) {
		// Emulate sops.DecryptEnvFile: back up the encrypted original, then
		// write the plaintext in its place.
		data, err := os.ReadFile(envPath)
		if err != nil {
			return false, nil
		}
		if err := os.WriteFile(envPath+".sops", data, 0600); err != nil {
			return false, err
		}
		return true, os.WriteFile(envPath, []byte("PLAINTEXT-VALUE=1"), 0600)
	}
	sopsDecryptComp = func(composePath, ageKey string) (bool, error) { return false, nil }

	stackPath := t.TempDir()
	envRel := filepath.Join("deploy", "edge", ".env")
	envPath := filepath.Join(stackPath, envRel)
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		t.Fatal(err)
	}
	encrypted := []byte("sops_version=9.0.0\nTOKEN=ENC[AES256_GCM,data=fake]\n")
	if err := os.WriteFile(envPath, encrypted, 0600); err != nil {
		t.Fatal(err)
	}

	st, err := stack.NewStack("edge-services", stackPath, stack.SourceGit)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewStackService(
		&sopsTestStackRepo{byName: map[string]*stack.Stack{"edge-services": st}},
		&sopsTestGitCfgRepo{byName: map[string]*stack.GitSource{"edge-services": {
			RepoURL:     "git@example.com:repo.git",
			Branch:      "main",
			EnvPath:     envRel,
			Credentials: &stack.GitCredentials{AgeKey: "age-test-key"},
		}}},
		nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil,
	)

	ctx, cancel := context.WithCancel(context.Background())
	ac, err := svc.PrepareAction(ctx, "edge-services")
	if err != nil {
		t.Fatalf("PrepareAction: %v", err)
	}
	if ac == nil {
		t.Fatal("PrepareAction returned nil ActionContext")
	}

	// Sanity: the fake decrypt left the env plaintext with a .sops backup.
	if got, _ := os.ReadFile(envPath); !bytes.Equal(got, []byte("PLAINTEXT-VALUE=1")) {
		t.Fatalf("env not decrypted by PrepareAction, got %d bytes", len(got))
	}
	if _, err := os.Stat(envPath + ".sops"); err != nil {
		t.Fatalf("expected .sops backup after decrypt: %v", err)
	}

	// The WS client disconnects after the streamed action: the request ctx
	// dies before the deferred Cleanup runs.
	cancel()

	ac.Cleanup()

	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("env missing after cleanup: %v", err)
	}
	if !bytes.Equal(got, encrypted) {
		t.Fatalf("env not restored from .sops backup despite cancelled ctx: got %d bytes", len(got))
	}
	if _, err := os.Stat(envPath + ".sops"); !os.IsNotExist(err) {
		t.Fatal(".sops backup not removed by restore")
	}
}

// TestPrepareAction_RepairsPlaintextEnv verifies the repair path: when an
// age key is available but the .env is already plaintext (e.g. left behind
// by a prior ctx-cancelled re-encrypt, the 2026-09-04 incident), the decrypt
// phase encrypts it in place so the normal decrypt-backup-restore cycle can
// run and the file ends up as ciphertext after the action completes.
func TestPrepareAction_RepairsPlaintextEnv(t *testing.T) {
	if !sops.IsAvailable() {
		t.Skip("sops binary not in PATH")
	}

	oldAvail, oldDecEnv, oldDecComp := sopsAvailable, sopsDecryptEnv, sopsDecryptComp
	t.Cleanup(func() {
		sopsAvailable, sopsDecryptEnv, sopsDecryptComp = oldAvail, oldDecEnv, oldDecComp
	})
	sopsAvailable = func() bool { return true }
	sopsDecryptEnv = func(envPath, ageKey string) (bool, error) {
		data, err := os.ReadFile(envPath)
		if err != nil {
			return false, nil
		}
		if !sops.IsSopsEncrypted(data) {
			return false, nil
		}
		if err := os.WriteFile(envPath+".sops", data, 0600); err != nil {
			return false, err
		}
		return true, os.WriteFile(envPath, []byte("PLAINTEXT-VALUE=1"), 0600)
	}
	sopsDecryptComp = func(composePath, ageKey string) (bool, error) { return false, nil }

	key, _, err := sops.GenerateAgeKey()
	if err != nil {
		t.Fatalf("GenerateAgeKey: %v", err)
	}

	stackPath := t.TempDir()
	envRel := filepath.Join("deploy", "edge", ".env")
	envPath := filepath.Join(stackPath, envRel)
	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		t.Fatal(err)
	}
	// Start PLAINTEXT -- the exact state left by the old ctx-lifetime bug.
	if err := os.WriteFile(envPath, []byte("TOKEN=original-value\n"), 0600); err != nil {
		t.Fatal(err)
	}

	st, err := stack.NewStack("edge-services", stackPath, stack.SourceGit)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewStackService(
		&sopsTestStackRepo{byName: map[string]*stack.Stack{"edge-services": st}},
		&sopsTestGitCfgRepo{byName: map[string]*stack.GitSource{"edge-services": {
			RepoURL:     "git@example.com:repo.git",
			Branch:      "main",
			EnvPath:     envRel,
			Credentials: &stack.GitCredentials{AgeKey: key},
		}}},
		nil, nil, nil, nil, t.TempDir(), t.TempDir(), NewStackLocks(), nil, nil,
	)

	ac, err := svc.PrepareAction(context.Background(), "edge-services")
	if err != nil {
		t.Fatalf("PrepareAction: %v", err)
	}
	if ac == nil {
		t.Fatal("PrepareAction returned nil ActionContext")
	}

	// Sanity: after the repair + decrypt, the .env is plaintext with a real
	// SOPS-encrypted .sops backup created during the decrypt step.
	got, _ := os.ReadFile(envPath)
	if !bytes.Equal(got, []byte("PLAINTEXT-VALUE=1")) {
		t.Fatalf("env not decrypted after repair: got %d bytes", len(got))
	}
	backup, err := os.ReadFile(envPath + ".sops")
	if err != nil {
		t.Fatalf("expected .sops backup after decrypt repair: %v", err)
	}
	if !sops.IsSopsEncrypted(backup) {
		t.Fatal(".sops backup is not SOPS ciphertext")
	}

	ac.Cleanup()

	got, err = os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("env missing after cleanup: %v", err)
	}
	if !sops.IsSopsEncrypted(got) {
		t.Fatal("env not restored to ciphertext after repair cleanup")
	}
	if _, err := os.Stat(envPath + ".sops"); !os.IsNotExist(err) {
		t.Fatal(".sops backup not removed by restore")
	}
}
