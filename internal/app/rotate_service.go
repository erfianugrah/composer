package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/erfianugrah/composer/internal/infra/crypto"
)

// RotateService rotates the master encryption key: every stored "enc:" value
// is re-encrypted old -> new in ONE database transaction (rollback on any
// error -- a half-rotated DB is the brick scenario this exists to prevent),
// then the new key is persisted to the key file and the crypto singleton is
// swapped AFTER commit.
type RotateService struct {
	db      *sql.DB
	dataDir string
	sshDirs []string
	logger  *zap.Logger

	mu sync.Mutex // serializes concurrent rotations
}

// NewRotateService creates a RotateService. sshDirs are the SSH key
// directories whose enc: files get re-encrypted (mirrors the boot-time
// EncryptSSHKeys hook: /home/composer/.ssh + $COMPOSER_SSH_DIR).
func NewRotateService(db *sql.DB, dataDir string, sshDirs []string, logger *zap.Logger) *RotateService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RotateService{db: db, dataDir: dataDir, sshDirs: sshDirs, logger: logger}
}

// Encrypted storage covered by a rotation.
//
// DB tables/columns (all "enc:"-prefixed via crypto.Encrypt):
//   - registry_credentials.secret_enc              (store/registry_repo.go)
//   - stack_git_configs.credentials                (store/stack_repo.go marshalCredentials)
//   - webhooks.secret                              (store/webhook_repo.go)
//   - docker_host_certs.ca_cert_enc / cert_enc / key_enc (store/host_certs_repo.go)
//
// Encrypted FILES (re-encrypted after commit):
//   - SSH deploy keys in sshDirs: /home/composer/.ssh + $COMPOSER_SSH_DIR
//     (written by the AddSSHKey handler and the boot-time EncryptSSHKeys
//     hook; per-stack git creds.SSHKeyFile paths are confined to these dirs
//     by isAllowedSSHKeyPath in infra/git)
//   - $COMPOSER_DATA_DIR/git-token (global git token, crypto.WriteEncrypted)
//
// NOT re-encrypted: $COMPOSER_DATA_DIR/certs/<host_id>/ materialized host
// certs -- plaintext PEM written from DB rows when a docker client is built;
// no key material is embedded in them.
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

	// Rotate every encrypted file IN MEMORY first. Nothing is written yet, so
	// a failure here (e.g. an SSH key file that no longer decrypts with the
	// current key) leaves every stored value exactly as it was.
	rotatedFiles, err := s.rotateFilesInMemory(oldKey, newKey)
	if err != nil {
		return "", err
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

	// Post-commit order: rotated files -> key file -> singleton swap. Every
	// step before the swap can fail without stranding the process: the
	// singleton still holds the old key, so the old-key files on disk stay
	// readable and the operator can fix and retry. Once all writes succeed,
	// the swap is the single atomic transition to the new key (it must come
	// after commit, never before -- a mid-flight request must never decrypt
	// an unrewritten row with the new key).
	if err := s.writeRotatedFiles(rotatedFiles); err != nil {
		return "", fmt.Errorf("writing rotated key files: %w", err)
	}
	if err := s.writeKeyFile(newKeyHex); err != nil {
		return "", fmt.Errorf("writing new key file: %w", err)
	}
	crypto.SetKeyForRotation(newKey)
	s.logger.Info("encryption key rotated",
		zap.String("key_file", s.keyFilePath()),
		zap.Int("files_reencrypted", len(rotatedFiles)))
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

var nonKeyFiles = map[string]bool{
	"known_hosts":     true,
	"config":          true,
	"authorized_keys": true,
}

// rotateFilesInMemory re-encrypts the contents of every encrypted on-disk
// key file (in memory, no writes) and returns path -> rotated content.
func (s *RotateService) rotateFilesInMemory(oldKey, newKey []byte) (map[string]string, error) {
	rotated := make(map[string]string)
	for _, dir := range s.sshDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scanning SSH dir %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || nonKeyFiles[e.Name()] || strings.HasSuffix(e.Name(), ".pub") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			content, err := s.rotateFileContent(path, oldKey, newKey)
			if err != nil {
				return nil, err
			}
			if content != "" {
				rotated[path] = content
			}
		}
	}

	// Global git token (single file in the data dir)
	tokenPath := filepath.Join(s.dataDir, "git-token")
	if content, err := s.rotateFileContent(tokenPath, oldKey, newKey); err != nil {
		return nil, err
	} else if content != "" {
		rotated[tokenPath] = content
	}
	return rotated, nil
}

// rotateFileContent re-encrypts one on-disk file's contents in memory (no
// write). Returns "" when the file is absent or not "enc:"-prefixed.
func (s *RotateService) rotateFileContent(path string, oldKey, newKey []byte) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	content := string(data)
	if len(content) < 4 || content[:4] != "enc:" {
		return "", nil // plaintext -- leave as-is
	}
	rotated, err := crypto.ReencryptValue(oldKey, newKey, content)
	if err != nil {
		return "", fmt.Errorf("re-encrypting %s: %w", path, err)
	}
	if rotated == content {
		return "", nil
	}
	return rotated, nil
}

// writeRotatedFiles writes the in-memory rotated contents back to disk,
// each via temp-file + rename (atomic per file, 0600).
func (s *RotateService) writeRotatedFiles(files map[string]string) error {
	for path, content := range files {
		if err := writeAtomic(path, content); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
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
