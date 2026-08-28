package store

import (
	"context"
	"testing"
	"time"

	"github.com/erfianugrah/composer/internal/domain/host"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostCertsRepoRoundTrip(t *testing.T) {
	t.Setenv("COMPOSER_ENCRYPTION_KEY", "test-host-certs-repo-key")
	db := newTestDB(t)
	repo := NewHostCertsRepo(db)
	ctx := context.Background()

	// The certs table FK-references docker_hosts, so a host row is required.
	now := time.Now().UTC()
	host := &host.Host{Name: "remote1", Endpoint: "tcp://docker-remote.example:2376", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, NewHostRepo(db).Create(ctx, host))
	hostID := host.ID

	// No row yet -> empty triple, no error.
	ca, cert, key, err := repo.GetHostCerts(ctx, hostID)
	require.NoError(t, err)
	assert.Empty(t, ca)
	assert.Empty(t, cert)
	assert.Empty(t, key)

	// Upsert then round-trip.
	require.NoError(t, repo.Upsert(ctx, hostID, "ca-pem", "cert-pem", "key-pem", "aa11", "2027-01-01T00:00:00Z"))
	ca, cert, key, err = repo.GetHostCerts(ctx, hostID)
	require.NoError(t, err)
	assert.Equal(t, "ca-pem", ca)
	assert.Equal(t, "cert-pem", cert)
	assert.Equal(t, "key-pem", key)

	// Encrypted at rest: the stored column carries the "enc:" prefix and no
	// plaintext.
	var certEnc, keyEnc string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT cert_enc, key_enc FROM docker_host_certs WHERE host_id = $1`, hostID).
		Scan(&certEnc, &keyEnc))
	assert.Contains(t, certEnc, "enc:")
	assert.NotContains(t, certEnc, "cert-pem")
	assert.Contains(t, keyEnc, "enc:")
	assert.NotContains(t, keyEnc, "key-pem")

	// Upsert replaces in place (still one row).
	require.NoError(t, repo.Upsert(ctx, hostID, "ca2", "cert2", "key2", "bb22", "2028-01-01T00:00:00Z"))
	ca, cert, key, err = repo.GetHostCerts(ctx, hostID)
	require.NoError(t, err)
	assert.Equal(t, "ca2", ca)
	assert.Equal(t, "cert2", cert)
	assert.Equal(t, "key2", key)

	var rows int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM docker_host_certs`).Scan(&rows))
	assert.Equal(t, 1, rows)

	// Meta (no decryption) reflects the upsert.
	meta, err := repo.Meta(ctx, hostID)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "bb22", meta.Fingerprint)
	assert.Equal(t, "2028-01-01T00:00:00Z", meta.NotAfter)

	missing, err := repo.Meta(ctx, 99)
	require.NoError(t, err)
	assert.Nil(t, missing)

	// ListMeta covers every stored host.
	require.NoError(t, repo.Upsert(ctx, hostID, "ca3", "cert3", "key3", "cc33", "2029-01-01T00:00:00Z"))
	all, err := repo.ListMeta(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "cc33", all[hostID].Fingerprint)

	// Delete removes.
	require.NoError(t, repo.Delete(ctx, hostID))
	ca, _, _, err = repo.GetHostCerts(ctx, hostID)
	require.NoError(t, err)
	assert.Empty(t, ca)
	_, err = repo.Meta(ctx, hostID)
	require.NoError(t, err)

	// ON DELETE CASCADE: deleting the host removes its certs row too.
	require.NoError(t, repo.Upsert(ctx, hostID, "ca4", "cert4", "key4", "dd44", "2030-01-01T00:00:00Z"))
	require.NoError(t, NewHostRepo(db).Delete(ctx, hostID))
	ca, _, _, err = repo.GetHostCerts(ctx, hostID)
	require.NoError(t, err)
	assert.Empty(t, ca, "certs row should cascade-delete with the host")
}
