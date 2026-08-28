package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"

	"github.com/erfianugrah/composer/internal/infra/crypto"
)

// RotateService rotates the master encryption key: every stored "enc:" value
// is re-encrypted old -> new in ONE database transaction (rollback on any
// error -- a half-rotated DB is the brick scenario this exists to prevent),
// then the new key is persisted to the key file and the crypto singleton is
// swapped AFTER commit. Rotation is DB-only: on-disk key files (SSH deploy
// keys, git token) are plaintext materializations read at runtime and are
// never touched.
type RotateService struct {
	db      *sql.DB
	dataDir string
	logger  *zap.Logger

	mu sync.Mutex // serializes concurrent rotations
}

// NewRotateService creates a RotateService. dataDir is where the new
// encryption.key file is written after a successful rotation.
//
// The third argument is a retained signature slot: rotation used to take the
// SSH deploy-key dirs and re-encrypt the on-disk files there. That file-side
// rotation was removed (DB-only model), so the value is ignored; the slot
// stays until the next breaking release so existing callers still compile.
func NewRotateService(db *sql.DB, dataDir string, logger *zap.Logger) *RotateService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RotateService{db: db, dataDir: dataDir, logger: logger}
}

// Encrypted storage covered by a rotation. The DB is the ONLY encrypted-at-rest
// store; on-disk key files (SSH deploy keys in /home/composer/.ssh,
// $COMPOSER_DATA_DIR/git-token) are plaintext materializations recreated from
// the DB on demand and are never re-encrypted by rotation.
//
// DB tables/columns (all "enc:"-prefixed via crypto.Encrypt):
//   - registry_credentials.secret_enc              (store/registry_repo.go)
//   - stack_git_configs.credentials                (store/stack_repo.go marshalCredentials)
//   - webhooks.secret                              (store/webhook_repo.go)
//   - docker_host_certs.ca_cert_enc / cert_enc / key_enc (store/host_certs_repo.go)
var encryptedColumns = []struct {
	table  string
	column string
	pk     string
}{
	{"registry_credentials", "secret_enc", "id"},
	{"stack_git_configs", "credentials", "stack_name"},
	{"webhooks", "secret", "id"},
	{"docker_host_certs", "ca_cert_enc", "host_id"},
	{"docker_host_certs", "cert_enc", "host_id"},
	{"docker_host_certs", "key_enc", "host_id"},
}

// RotateEncryptionKey re-encrypts every stored secret from the current key
// to newKeyHex (64 hex chars; empty = generate a fresh one) and returns the
// effective new key hex. The caller returns it to the operator exactly once;
// it is never logged.
func (s *RotateService) RotateEncryptionKey(ctx context.Context, newKeyHex string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if newKeyHex == "" {
		generated, err := crypto.GenerateKeyHex()
		if err != nil {
			return "", fmt.Errorf("generating new encryption key: %w", err)
		}
		newKeyHex = generated
	}
	newKey, err := crypto.KeyFromHex(newKeyHex)
	if err != nil {
		return "", err // crypto.ErrInvalidKey -- no key material in the message
	}
	oldKey, err := crypto.CurrentKey()
	if err != nil {
		return "", fmt.Errorf("resolving current encryption key: %w", err)
	}

	// ONE transaction over all encrypted rows: any error rolls the whole
	// thing back, so the DB is never half-rotated.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("beginning rotation transaction: %w", err)
	}
	if err := s.rotateDatabase(ctx, tx, oldKey, newKey); err != nil {
		tx.Rollback()
		return "", err
	}
	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return "", fmt.Errorf("committing rotation transaction: %w", err)
	}

	// Post-commit order: key file -> singleton swap. Every step before the
	// swap can fail without stranding the process: the singleton still holds
	// the old key, so the old-key DB stays readable and the operator can fix
	// and retry. Once the key file is written, the swap is the single atomic
	// transition to the new key (it must come after commit, never before --
	// a mid-flight request must never decrypt an unrewritten row with the
	// new key). No on-disk file is touched: rotation is DB-only.
	if err := s.writeKeyFile(newKeyHex); err != nil {
		return "", fmt.Errorf("writing new key file: %w", err)
	}
	crypto.SetKeyForRotation(newKey)
	s.logger.Info("encryption key rotated",
		zap.String("key_file", s.keyFilePath()))
	return newKeyHex, nil
}

func (s *RotateService) rotateDatabase(ctx context.Context, tx *sql.Tx, oldKey, newKey []byte) error {
	for _, col := range encryptedColumns {
		if err := s.rotateColumn(ctx, tx, col, oldKey, newKey); err != nil {
			return fmt.Errorf("rotating %s.%s: %w", col.table, col.column, err)
		}
	}
	return nil
}

func (s *RotateService) rotateColumn(ctx context.Context, tx *sql.Tx, col struct {
	table, column, pk string
}, oldKey, newKey []byte) error {
	rows, err := tx.QueryContext(ctx,
		fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s LIKE 'enc:%%'", col.pk, col.column, col.table, col.column))
	if err != nil {
		return fmt.Errorf("querying: %w", err)
	}
	type row struct {
		id    any
		value string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.value); err != nil {
			rows.Close()
			return fmt.Errorf("scanning: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterating: %w", err)
	}
	rows.Close()

	for _, r := range pending {
		newValue, err := crypto.ReencryptValue(oldKey, newKey, r.value)
		if err != nil {
			return fmt.Errorf("row %v: %w", r.id, err)
		}
		if newValue != r.value {
			if _, err := tx.ExecContext(ctx,
				fmt.Sprintf("UPDATE %s SET %s=$1 WHERE %s=$2", col.table, col.column, col.pk),
				newValue, r.id); err != nil {
				return fmt.Errorf("updating row %v: %w", r.id, err)
			}
		}
	}
	return nil
}

func (s *RotateService) keyFilePath() string {
	return filepath.Join(s.dataDir, "encryption.key")
}

func (s *RotateService) writeKeyFile(keyHex string) error {
	if err := os.MkdirAll(s.dataDir, 0700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	return writeAtomic(s.keyFilePath(), keyHex)
}

func writeAtomic(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".rotate-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("setting permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}
