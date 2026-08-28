package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/erfianugrah/composer/internal/infra/crypto"
)

// HostCertMeta is the non-secret certificate metadata for one docker host.
// Key material never lives in this struct.
type HostCertMeta struct {
	HostID      int64
	Fingerprint string // sha256 hex of the client cert DER
	NotAfter    string // client cert expiry, RFC3339
}

// HostCertsRepo persists per-host mTLS material. All three PEMs are stored
// encrypted with crypto.Encrypt (AES-256-GCM, "enc:" prefixed) - fail closed
// on encryption errors, never write plaintext.
type HostCertsRepo struct {
	db *sql.DB
}

// NewHostCertsRepo creates a HostCertsRepo.
func NewHostCertsRepo(db *sql.DB) *HostCertsRepo { return &HostCertsRepo{db: db} }

// GetHostCerts returns the decrypted mTLS triple for a host, or
// ("", "", "", nil) when none is stored. Satisfies docker.HostCertStore.
func (r *HostCertsRepo) GetHostCerts(ctx context.Context, hostID int64) (ca, cert, key string, err error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT ca_cert_enc, cert_enc, key_enc
		 FROM docker_host_certs WHERE host_id = $1`, hostID)
	var caEnc, certEnc, keyEnc string
	if err := row.Scan(&caEnc, &certEnc, &keyEnc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", "", nil
		}
		return "", "", "", fmt.Errorf("loading host certs for host %d: %w", hostID, err)
	}
	ca, err = crypto.Decrypt(caEnc)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypting CA cert for host %d: %w", hostID, err)
	}
	cert, err = crypto.Decrypt(certEnc)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypting client cert for host %d: %w", hostID, err)
	}
	key, err = crypto.Decrypt(keyEnc)
	if err != nil {
		return "", "", "", fmt.Errorf("decrypting client key for host %d: %w", hostID, err)
	}
	return ca, cert, key, nil
}

// Upsert stores the encrypted mTLS triple + metadata for a host, replacing
// any existing row.
func (r *HostCertsRepo) Upsert(ctx context.Context, hostID int64, ca, cert, key, fingerprint, notAfter string) error {
	caEnc, err := crypto.Encrypt(ca)
	if err != nil {
		return fmt.Errorf("encrypting CA cert for host %d: %w", hostID, err)
	}
	certEnc, err := crypto.Encrypt(cert)
	if err != nil {
		return fmt.Errorf("encrypting client cert for host %d: %w", hostID, err)
	}
	keyEnc, err := crypto.Encrypt(key)
	if err != nil {
		return fmt.Errorf("encrypting client key for host %d: %w", hostID, err)
	}
	now := time.Now().UTC()
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO docker_host_certs (host_id, ca_cert_enc, cert_enc, key_enc, cert_fingerprint, cert_not_after, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT(host_id) DO UPDATE SET
		   ca_cert_enc=$2, cert_enc=$3, key_enc=$4,
		   cert_fingerprint=$5, cert_not_after=$6, updated_at=$7`,
		hostID, caEnc, certEnc, keyEnc, fingerprint, notAfter, now)
	if err != nil {
		return fmt.Errorf("upserting host certs for host %d: %w", hostID, err)
	}
	return nil
}

// Delete removes the stored certs for a host.
func (r *HostCertsRepo) Delete(ctx context.Context, hostID int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM docker_host_certs WHERE host_id = $1`, hostID)
	if err != nil {
		return fmt.Errorf("deleting host certs for host %d: %w", hostID, err)
	}
	return nil
}

// Meta returns the certificate metadata for one host (no decryption), or
// nil, nil when none is stored.
func (r *HostCertsRepo) Meta(ctx context.Context, hostID int64) (*HostCertMeta, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT host_id, cert_fingerprint, cert_not_after
		 FROM docker_host_certs WHERE host_id = $1`, hostID)
	var m HostCertMeta
	if err := row.Scan(&m.HostID, &m.Fingerprint, &m.NotAfter); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("loading host cert metadata for host %d: %w", hostID, err)
	}
	return &m, nil
}

// ListMeta returns the certificate metadata for every host that has stored
// certs (no decryption).
func (r *HostCertsRepo) ListMeta(ctx context.Context) (map[int64]*HostCertMeta, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT host_id, cert_fingerprint, cert_not_after FROM docker_host_certs`)
	if err != nil {
		return nil, fmt.Errorf("listing host cert metadata: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]*HostCertMeta)
	for rows.Next() {
		var m HostCertMeta
		if err := rows.Scan(&m.HostID, &m.Fingerprint, &m.NotAfter); err != nil {
			return nil, fmt.Errorf("scanning host cert metadata: %w", err)
		}
		out[m.HostID] = &m
	}
	return out, rows.Err()
}
