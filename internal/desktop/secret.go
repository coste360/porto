package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"

	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/desktopport"
)

// KeyringService is the service name of the secret key in the OS keychain.
const KeyringService = "it.coste360.porto"

// Errors of the secret key resolution.
var (
	// ErrSecretNotFound: the store has no secret key.
	ErrSecretNotFound = errors.New("desktop: secret key not found")
	// ErrSecretMissing: a database exists but its secret key is nowhere; a new key is never generated, because the
	// encrypted columns and files would be lost.
	ErrSecretMissing = errors.New("desktop: the secret key of the existing database is missing")
	// ErrSecretInvalid: the stored key is not a valid PORTO_SECRET_KEY.
	ErrSecretInvalid = errors.New("desktop: the stored secret key is not valid")
)

// SecretStore keeps the secret key.
type SecretStore interface {
	// Get returns the key or ErrSecretNotFound.
	Get() (string, error)
	Set(key string) error
	// Kind is desktopport.SecretStoreKeyring or desktopport.SecretStoreFile.
	Kind() string
}

// KeyringStore keeps the key in the OS keychain (macOS Keychain, Windows Credential Manager, Secret Service on
// Linux) under KeyringService and an account derived from the data folder, so two data folders never share a key.
type KeyringStore struct {
	Account string
}

// NewKeyringStore returns the keychain store of a data folder.
func NewKeyringStore(dataDir string) KeyringStore {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	sum := sha256.Sum256([]byte(abs))
	return KeyringStore{Account: "secret-key/" + hex.EncodeToString(sum[:])[:12]}
}

// Get implements SecretStore.
func (k KeyringStore) Get() (string, error) {
	v, err := keyring.Get(KeyringService, k.Account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrSecretNotFound
	}
	if err != nil {
		return "", fmt.Errorf("desktop: keychain: %w", err)
	}
	return v, nil
}

// Set implements SecretStore.
func (k KeyringStore) Set(key string) error {
	if err := keyring.Set(KeyringService, k.Account, key); err != nil {
		return fmt.Errorf("desktop: keychain: %w", err)
	}
	return nil
}

// Kind implements SecretStore.
func (KeyringStore) Kind() string { return desktopport.SecretStoreKeyring }

// FileStore keeps the key in a 0600 file of the data folder (fallback when no keychain is available).
type FileStore struct {
	Path string
}

// Get implements SecretStore.
func (f FileStore) Get() (string, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrSecretNotFound
	}
	if err != nil {
		return "", fmt.Errorf("desktop: read secret file: %w", err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", ErrSecretNotFound
	}
	return v, nil
}

// Set implements SecretStore.
func (f FileStore) Set(key string) error {
	return writeFileAtomic(f.Path, []byte(key+"\n"), 0o600)
}

// Kind implements SecretStore.
func (FileStore) Kind() string { return desktopport.SecretStoreFile }

// Remove deletes the file.
func (f FileStore) Remove() error {
	err := os.Remove(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ResolvedSecret is the outcome of ResolveSecretKey.
type ResolvedSecret struct {
	Key       string
	Kind      string // where the key lives now (desktopport.SecretStore*)
	Generated bool
}

// ResolveSecretKey finds the secret key (m6a-plan §2.5): envKey (PORTO_SECRET_KEY) wins; then the keychain; then
// the file, which is moved into the keychain when possible (the file is deleted only after a successful read
// back). With no key anywhere a key is generated only when no database exists, and stored in the keychain, else
// in the file. kr may be nil (PORTO_DESKTOP_KEYRING=file).
func ResolveSecretKey(envKey string, kr SecretStore, file FileStore, dbExists bool) (ResolvedSecret, error) {
	if envKey != "" {
		if _, err := crypto.ParseKeyRing(envKey); err != nil {
			return ResolvedSecret{}, ErrSecretInvalid
		}
		return ResolvedSecret{Key: envKey, Kind: desktopport.SecretStoreKeyring}, nil
	}
	if kr != nil {
		v, err := kr.Get()
		if err == nil {
			return checked(v, kr.Kind())
		}
		if !errors.Is(err, ErrSecretNotFound) {
			kr = nil // keychain unavailable (no Secret Service, locked): fall back to the file
		}
	}
	v, err := file.Get()
	switch {
	case err == nil:
		res, err := checked(v, file.Kind())
		if err != nil {
			return res, err
		}
		if kr != nil && kr.Set(v) == nil {
			if back, err := kr.Get(); err == nil && back == v {
				_ = file.Remove()
				res.Kind = kr.Kind()
			}
		}
		return res, nil
	case !errors.Is(err, ErrSecretNotFound):
		return ResolvedSecret{}, err
	}
	if dbExists {
		return ResolvedSecret{}, ErrSecretMissing
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		return ResolvedSecret{}, err
	}
	if kr != nil && kr.Set(key) == nil {
		if back, err := kr.Get(); err == nil && back == key {
			return ResolvedSecret{Key: key, Kind: kr.Kind(), Generated: true}, nil
		}
	}
	if err := file.Set(key); err != nil {
		return ResolvedSecret{}, fmt.Errorf("desktop: store secret key: %w", err)
	}
	return ResolvedSecret{Key: key, Kind: file.Kind(), Generated: true}, nil
}

func checked(v, kind string) (ResolvedSecret, error) {
	if _, err := crypto.ParseKeyRing(v); err != nil {
		return ResolvedSecret{}, ErrSecretInvalid
	}
	return ResolvedSecret{Key: v, Kind: kind}, nil
}
