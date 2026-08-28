package app_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/erfianugrah/composer/internal/app"
	"github.com/erfianugrah/composer/internal/domain/auth"
	"github.com/erfianugrah/composer/internal/domain/registry"
	"github.com/erfianugrah/composer/internal/infra/crypto"
	"github.com/erfianugrah/composer/internal/infra/store"
)

// seededSecrets carries the raw ciphertexts captured before rotation so the
// tests can assert old-key failure / rollback untouched-ness.
type seededSecrets struct {
	registryEnc string
	gitEnc      string
	webhookEnc  string
	caEnc       string
	certEnc     string
	keyEnc      string
}

func randomKeyHex(t *testing.T) string {
	t.Helper()
	var buf [32]byte
	_, err := rand.Read(buf[:])
	require.NoError(t, err)
	return hex.EncodeToString(buf[:])
}

// plainSSHDeployKey is a fake plaintext private-key stand-in seeded into the
// SSH dir; rotation must leave it byte-identical.
const plainSSHDeployKey = "FAKE-PLAINTEXT-OPENSSH-PRIVATE-KEY-MATERIALIZATION"

// seedAllEncryptedStorage populates every encrypted DB table (values
// encrypted under the CURRENT singleton key, which the test must set to the
// "old" key before calling this) plus the plaintext on-disk files, which
// rotation must leave untouched.
func seedAllEncryptedStorage(t *testing.T, db *store.DB, dataDir string, sshDir string) seededSecrets {
	t.Helper()
	ctx := context.Background()
	oldKey, err := crypto.CurrentKey()
	require.NoError(t, err)

	// users row (webhooks.created_by FK) -- via the real repo path
	u, err := auth.NewUser("rotate-user@test.com", "password123", auth.RoleAdmin)
	require.NoError(t, err)
	require.NoError(t, store.NewUserRepo(db.SQL).Create(ctx, u))

	// 1. registry_credentials
	registryRepo := store.NewRegistryCredentialRepo(db.SQL)
	cred := &registry.Credential{
		Registry: "registry.erfi.io",
		Username: "deploy-bot",
		Secret:   "reg-secret-1",
	}
	require.NoError(t, registryRepo.Upsert(ctx, cred))

	var seeded seededSecrets
	row := db.SQL.QueryRowContext(ctx, `SELECT secret_enc FROM registry_credentials WHERE id=$1`, cred.ID)
	require.NoError(t, row.Scan(&seeded.registryEnc))

	// 2. stack_git_configs (raw insert -- marshalCredentials is unexported)
	_, err = db.SQL.ExecContext(ctx, `INSERT OR IGNORE INTO stacks (name, path) VALUES ('gitstack', '/opt/stacks/gitstack')`)
	require.NoError(t, err)
	gitCredsEnc, err := crypto.EncryptWith(oldKey, `{"token":"glt_123","username":"x"}`)
	require.NoError(t, err)
	_, err = db.SQL.ExecContext(ctx,
		`INSERT OR REPLACE INTO stack_git_configs (stack_name, repo_url, credentials, auth_method)
		 VALUES ('gitstack', 'git@github.com:example/x.git', $1, 'token')`, gitCredsEnc)
	require.NoError(t, err)
	seeded.gitEnc = gitCredsEnc

	// 3. webhooks
	webhookRepo := store.NewWebhookRepo(db.SQL)
	wh := &store.Webhook{
		ID:        "wh-rotate-1",
		StackName: "gitstack",
		Provider:  "github",
		Secret:    "hook-secret-1",
		CreatedBy: u.ID,
	}
	require.NoError(t, webhookRepo.Create(ctx, wh))
	row = db.SQL.QueryRowContext(ctx, `SELECT secret FROM webhooks WHERE id='wh-rotate-1'`)
	require.NoError(t, row.Scan(&seeded.webhookEnc))

	// 4. docker_host_certs
	var hostID int64
	err = db.SQL.QueryRowContext(ctx,
		`INSERT INTO docker_hosts (name, endpoint) VALUES ('rotate-host', 'tcp://127.0.0.1:2376') RETURNING id`).Scan(&hostID)
	require.NoError(t, err)
	certsRepo := store.NewHostCertsRepo(db.SQL)
	require.NoError(t, certsRepo.Upsert(ctx, hostID, "ca-pem-material", "cert-pem-material", "key-pem-material", "fp-abc", "2031-01-01T00:00:00Z"))
	row = db.SQL.QueryRowContext(ctx, `SELECT ca_cert_enc, cert_enc, key_enc FROM docker_host_certs WHERE host_id=$1`, hostID)
	require.NoError(t, row.Scan(&seeded.caEnc, &seeded.certEnc, &seeded.keyEnc))

	// 5. files: plaintext on-disk materializations (SSH deploy key, a
	//    non-key file, and the global git token). Rotation must leave
	//    every one of them untouched.
	require.NoError(t, os.MkdirAll(sshDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "id_deploy"), []byte(plainSSHDeployKey), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte("10.0.0.1 ssh-rsa AAAA"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(sshDir, "plain_key"), []byte("plaintext-deploy-key"), 0600))

	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "git-token"), []byte("ghp_gittoken123"), 0600))

	return seeded
}

func fetchRaw(t *testing.T, db *store.DB, query string) string {
	t.Helper()
	var raw string
	require.NoError(t, db.SQL.QueryRow(query).Scan(&raw))
	return raw
}

// assertRotated checks the two ciphertexts around a rotation:
// post-rotation blob decrypts with the NEW key and fails with the OLD key;
// pre-rotation blob still decrypts with the OLD key and fails with the NEW.
func assertRotated(t *testing.T, preRaw, postRaw string, newKey, oldKey []byte, original string) {
	t.Helper()
	if preRaw == postRaw {
		t.Fatal("stored ciphertext must change after rotation")
	}
	plain, err := crypto.DecryptWith(newKey, postRaw)
	require.NoError(t, err, "new key must decrypt the rotated value")
	assert.Equal(t, original, plain)
	_, err = crypto.DecryptWith(oldKey, postRaw)
	assert.Error(t, err, "old key must FAIL on the rotated value")

	plain, err = crypto.DecryptWith(oldKey, preRaw)
	require.NoError(t, err, "old key must still decrypt the pre-rotation value")
	assert.Equal(t, original, plain)
	_, err = crypto.DecryptWith(newKey, preRaw)
	assert.Error(t, err, "new key must FAIL on the pre-rotation value")
}

// TestRotateEncryptionKeyFull verifies rotation correctness end to end:
// every DB row decrypts with the NEW key and fails with the OLD key, the key
// file is written, the singleton is swapped, and no on-disk file is touched.
func TestRotateEncryptionKeyFull(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	sshDir := t.TempDir()

	db, err := store.New(ctx, "", dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	prevKey, err := crypto.CurrentKey()
	require.NoError(t, err)
	t.Cleanup(func() { crypto.SetKeyForRotation(prevKey) })

	oldHex, newHex := randomKeyHex(t), randomKeyHex(t)
	oldKey, err := crypto.KeyFromHex(oldHex)
	require.NoError(t, err)
	newKey, err := crypto.KeyFromHex(newHex)
	require.NoError(t, err)
	crypto.SetKeyForRotation(oldKey)

	seeded := seedAllEncryptedStorage(t, db, dataDir, sshDir)

	svc := app.NewRotateService(db.SQL, dataDir, zaptest.NewLogger(t))
	out, err := svc.RotateEncryptionKey(ctx, newHex)
	require.NoError(t, err)
	assert.Equal(t, newHex, out)

	// Singleton swapped to the new key (and the written key file re-derives
	// the same effective key -- restart consistency)
	keyFileData, err := os.ReadFile(filepath.Join(dataDir, "encryption.key"))
	require.NoError(t, err)
	assert.Equal(t, newHex, string(keyFileData))
	cur, err := crypto.CurrentKey()
	require.NoError(t, err)
	assert.Equal(t, newKey, cur)
	assert.Equal(t, cur, keyFromHexMust(t, string(keyFileData)))

	// --- DB rows: rotated ciphertexts decrypt with the new key, fail with
	// --- the old key (pre-rotation blobs are captured in `seeded`) ---
	var postCA, postCert, postKeyEnc string
	require.NoError(t, db.SQL.QueryRowContext(ctx,
		`SELECT ca_cert_enc, cert_enc, key_enc FROM docker_host_certs WHERE host_id=1`).
		Scan(&postCA, &postCert, &postKeyEnc))
	assertRotated(t, seeded.registryEnc, fetchRaw(t, db, `SELECT secret_enc FROM registry_credentials ORDER BY id LIMIT 1`), newKey, oldKey, "reg-secret-1")
	assertRotated(t, seeded.gitEnc, fetchRaw(t, db, `SELECT credentials FROM stack_git_configs WHERE stack_name='gitstack'`), newKey, oldKey, `{"token":"glt_123","username":"x"}`)
	assertRotated(t, seeded.webhookEnc, fetchRaw(t, db, `SELECT secret FROM webhooks WHERE id='wh-rotate-1'`), newKey, oldKey, "hook-secret-1")
	assertRotated(t, seeded.caEnc, postCA, newKey, oldKey, "ca-pem-material")
	assertRotated(t, seeded.certEnc, postCert, newKey, oldKey, "cert-pem-material")
	assertRotated(t, seeded.keyEnc, postKeyEnc, newKey, oldKey, "key-pem-material")

	// --- Re-read through the REAL repos (they use the singleton = new key) ---
	creds, err := store.NewRegistryCredentialRepo(db.SQL).List(ctx)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	assert.Equal(t, "reg-secret-1", creds[0].Secret)

	wh, err := store.NewWebhookRepo(db.SQL).GetByID(ctx, "wh-rotate-1")
	require.NoError(t, err)
	require.NotNil(t, wh)
	assert.Equal(t, "hook-secret-1", wh.Secret)

	// --- Files: rotation is DB-only -- on-disk plaintext materializations
	// --- are NOT re-encrypted; every seeded file stays byte-identical ---
	sshRaw, err := os.ReadFile(filepath.Join(sshDir, "id_deploy"))
	require.NoError(t, err)
	assert.Equal(t, plainSSHDeployKey, string(sshRaw),
		"plaintext SSH deploy key file must be byte-identical after rotation")
	plainFile, err := os.ReadFile(filepath.Join(sshDir, "plain_key"))
	require.NoError(t, err)
	assert.Equal(t, "plaintext-deploy-key", string(plainFile),
		"plaintext file in SSH dir must be unmodified by rotation")
	kh, err := os.ReadFile(filepath.Join(sshDir, "known_hosts"))
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1 ssh-rsa AAAA", string(kh))

	// git token file untouched (plaintext materialization, not re-encrypted)
	tokenRaw, err := os.ReadFile(filepath.Join(dataDir, "git-token"))
	require.NoError(t, err)
	assert.Equal(t, "ghp_gittoken123", string(tokenRaw),
		"git-token file must be unmodified by rotation")
}

func keyFromHexMust(t *testing.T, keyHex string) []byte {
	t.Helper()
	key, err := crypto.KeyFromHex(keyHex)
	require.NoError(t, err)
	return key
}

// TestRotateEncryptionKeyGeneratesWhenEmpty: empty input = server generates
// a fresh key, returned to the caller once.
func TestRotateEncryptionKeyGeneratesWhenEmpty(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	db, err := store.New(ctx, "", dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	prevKey, err := crypto.CurrentKey()
	require.NoError(t, err)
	t.Cleanup(func() { crypto.SetKeyForRotation(prevKey) })

	oldHex := randomKeyHex(t)
	oldKey, err := crypto.KeyFromHex(oldHex)
	require.NoError(t, err)
	crypto.SetKeyForRotation(oldKey)
	seedAllEncryptedStorage(t, db, dataDir, t.TempDir())

	svc := app.NewRotateService(db.SQL, dataDir, zaptest.NewLogger(t))
	out, err := svc.RotateEncryptionKey(ctx, "")
	require.NoError(t, err)
	assert.Len(t, out, 64)
	_, err = hex.DecodeString(out)
	assert.NoError(t, err)
	assert.NotEqual(t, oldHex, out)

	// Singleton now equals the key derived from the returned hex
	cur, err := crypto.CurrentKey()
	require.NoError(t, err)
	assert.Equal(t, keyFromHexMust(t, out), cur)
}

// TestRotateEncryptionKeyRollbackOnBadRow: a row undecryptable under the
// current key fails the whole rotation; the DB is untouched, no key file,
// no singleton swap.
func TestRotateEncryptionKeyRollbackOnBadRow(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	sshDir := t.TempDir()

	db, err := store.New(ctx, "", dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	prevKey, err := crypto.CurrentKey()
	require.NoError(t, err)
	t.Cleanup(func() { crypto.SetKeyForRotation(prevKey) })

	oldHex, newHex := randomKeyHex(t), randomKeyHex(t)
	oldKey, err := crypto.KeyFromHex(oldHex)
	require.NoError(t, err)
	crypto.SetKeyForRotation(oldKey)

	seeded := seedAllEncryptedStorage(t, db, dataDir, sshDir)

	// Corrupt ONE webhook row: enc:-prefixed but undecryptable under oldKey.
	_, err = db.SQL.ExecContext(ctx,
		`UPDATE webhooks SET secret='enc:AAAA' WHERE id='wh-rotate-1'`)
	require.NoError(t, err)

	svc := app.NewRotateService(db.SQL, dataDir, zaptest.NewLogger(t))
	_, err = svc.RotateEncryptionKey(ctx, newHex)
	require.Error(t, err, "rotation must fail on an undecryptable row")

	// Atomicity: the registry row (processed before the failing webhook row)
	// must be byte-identical to its pre-rotation ciphertext.
	var raw string
	err = db.SQL.QueryRowContext(ctx, `SELECT secret_enc FROM registry_credentials ORDER BY id LIMIT 1`).Scan(&raw)
	require.NoError(t, err)
	assert.Equal(t, seeded.registryEnc, raw, "rolled-back row must be untouched")

	// No key file, no singleton swap
	_, err = os.Stat(filepath.Join(dataDir, "encryption.key"))
	assert.True(t, os.IsNotExist(err), "key file must not be written on rollback")
	cur, err := crypto.CurrentKey()
	require.NoError(t, err)
	assert.Equal(t, oldKey, cur, "singleton must be unchanged on rollback")

	// Existing data still decrypts under the old key (nothing was lost)
	plain, err := crypto.DecryptWith(oldKey, seeded.registryEnc)
	require.NoError(t, err)
	assert.Equal(t, "reg-secret-1", plain)
}

// TestRotateEncryptionKeyInvalidInput: malformed key hex is rejected before
// anything is touched.
func TestRotateEncryptionKeyInvalidInput(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	db, err := store.New(ctx, "", dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	prevKey, err := crypto.CurrentKey()
	require.NoError(t, err)
	t.Cleanup(func() { crypto.SetKeyForRotation(prevKey) })
	oldKey, err := crypto.KeyFromHex(randomKeyHex(t))
	require.NoError(t, err)
	crypto.SetKeyForRotation(oldKey)

	svc := app.NewRotateService(db.SQL, dataDir, zaptest.NewLogger(t))

	for _, bad := range []string{"not-hex", strings.Repeat("z", 64), "abc", randomKeyHex(t)[:31]} {
		_, err := svc.RotateEncryptionKey(ctx, bad)
		assert.ErrorIs(t, err, crypto.ErrInvalidKey, "input %q", bad)
	}

	// Nothing changed
	cur, err := crypto.CurrentKey()
	require.NoError(t, err)
	assert.Equal(t, oldKey, cur)
	_, err = os.Stat(filepath.Join(dataDir, "encryption.key"))
	assert.True(t, os.IsNotExist(err))
}

// TestRotateEncryptionKeyConcurrent: two rotations in flight serialize on the
// service mutex; both succeed, and the singleton always agrees with the key
// file (the last rotation to commit wins).
func TestRotateEncryptionKeyConcurrent(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	db, err := store.New(ctx, "", dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	prevKey, err := crypto.CurrentKey()
	require.NoError(t, err)
	t.Cleanup(func() { crypto.SetKeyForRotation(prevKey) })

	oldKey, err := crypto.KeyFromHex(randomKeyHex(t))
	require.NoError(t, err)
	crypto.SetKeyForRotation(oldKey)
	seedAllEncryptedStorage(t, db, dataDir, t.TempDir())

	svc := app.NewRotateService(db.SQL, dataDir, zaptest.NewLogger(t))

	var wg sync.WaitGroup
	var firstErr, secondErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, firstErr = svc.RotateEncryptionKey(ctx, randomKeyHex(t))
	}()
	_, secondErr = func() (string, error) {
		defer wg.Done()
		return svc.RotateEncryptionKey(ctx, randomKeyHex(t))
	}()
	wg.Wait()
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)

	// Singleton and key file must agree no matter which rotation won
	cur, err := crypto.CurrentKey()
	require.NoError(t, err)
	fileData, err := os.ReadFile(filepath.Join(dataDir, "encryption.key"))
	require.NoError(t, err)
	assert.Equal(t, keyFromHexMust(t, string(fileData)), cur, "singleton and key file must agree")
}
