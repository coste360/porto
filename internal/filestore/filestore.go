// Package filestore keeps uploaded files (map backgrounds, document files) as encrypted blobs on disk under
// PORTO_DATA_DIR/files/<marina_id>/<file_id>.bin (m1-plan §7.2, D19). Each blob is the KeyRing ciphertext of the
// file, bound to its marina and id through the AAD "file:<marina_id>:<file_id>", so a blob copied to another
// marina or id does not decrypt. The rows describing the files (stored_files) are written by the callers'
// store functions; this package does no SQL. Standard library and internal/crypto only.
package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/coste360/porto/internal/crypto"
)

// MaxFileBytes is the largest file accepted (15 MiB, D19).
const MaxFileBytes int64 = 15 << 20

// Errors.
var (
	// ErrTooLarge reports a file above the limit given to Put.
	ErrTooLarge = errors.New("filestore: file too large")
	// ErrCorrupt reports a blob that is missing, unreadable or fails authentication.
	ErrCorrupt = errors.New("filestore: blob corrupt or missing")
	// ErrInvalidKey reports a marina id, file id or storage key that is not a safe path component.
	ErrInvalidKey = errors.New("filestore: invalid key")
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// Store is the encrypted file store rooted at one directory.
type Store struct {
	dir  string
	keys *crypto.KeyRing
}

// New returns a store rooted at dir, creating it with mode 0700. A directory that cannot be created is reported
// by the first Put.
func New(dir string, keys *crypto.KeyRing) *Store {
	_ = os.MkdirAll(dir, 0o700)
	return &Store{dir: dir, keys: keys}
}

// Dir returns the root directory (backup and restore copy its content).
func (s *Store) Dir() string { return s.dir }

// Blob describes a stored blob.
type Blob struct {
	StorageKey string // "<marina_id>/<file_id>.bin", relative to Dir
	Size       int64  // plaintext size in bytes
	SHA256     string // hex SHA-256 of the plaintext
}

// StorageKey returns the storage key of a file.
func StorageKey(marinaID, fileID string) string { return marinaID + "/" + fileID + ".bin" }

func aad(marinaID, fileID string) []byte { return []byte("file:" + marinaID + ":" + fileID) }

func (s *Store) path(storageKey string) (string, error) {
	marina, file, ok := strings.Cut(storageKey, "/")
	if !ok || !idPattern.MatchString(marina) || !strings.HasSuffix(file, ".bin") || !idPattern.MatchString(strings.TrimSuffix(file, ".bin")) {
		return "", ErrInvalidKey
	}
	return filepath.Join(s.dir, marina, file), nil
}

// Put reads at most max bytes from r, encrypts them and writes the blob atomically (temporary file + rename).
// More than max bytes is ErrTooLarge and nothing is written. An existing blob with the same key is replaced.
func (s *Store) Put(ctx context.Context, marinaID, fileID string, r io.Reader, maxBytes int64) (Blob, error) {
	if !idPattern.MatchString(marinaID) || !idPattern.MatchString(fileID) {
		return Blob{}, ErrInvalidKey
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return Blob{}, fmt.Errorf("filestore: read: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return Blob{}, ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return Blob{}, err
	}
	sum := sha256.Sum256(data)
	ct, err := s.keys.Encrypt(data, aad(marinaID, fileID))
	if err != nil {
		return Blob{}, fmt.Errorf("filestore: encrypt: %w", err)
	}
	key := StorageKey(marinaID, fileID)
	dst, err := s.path(key)
	if err != nil {
		return Blob{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return Blob{}, fmt.Errorf("filestore: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return Blob{}, fmt.Errorf("filestore: create: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if _, err := tmp.WriteString(ct); err != nil {
		_ = tmp.Close()
		return Blob{}, fmt.Errorf("filestore: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return Blob{}, fmt.Errorf("filestore: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Blob{}, fmt.Errorf("filestore: close: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return Blob{}, fmt.Errorf("filestore: rename: %w", err)
	}
	return Blob{StorageKey: key, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}, nil
}

// Open reads and decrypts a blob. The storage key must be the one of (marinaID, fileID).
func (s *Store) Open(ctx context.Context, marinaID, fileID, storageKey string) ([]byte, error) {
	if storageKey != StorageKey(marinaID, fileID) {
		return nil, ErrInvalidKey
	}
	p, err := s.path(storageKey)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err) //nolint:errorlint // one wrapped sentinel is enough
	}
	data, err := s.keys.Decrypt(string(bytes.TrimSpace(raw)), aad(marinaID, fileID))
	if err != nil {
		return nil, ErrCorrupt
	}
	return data, nil
}

// Remove deletes a blob. Removing a missing blob is not an error.
func (s *Store) Remove(storageKey string) error {
	p, err := s.path(storageKey)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("filestore: remove: %w", err)
	}
	return nil
}
