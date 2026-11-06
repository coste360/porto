// Package crypto holds Porto's symmetric encryption at rest (AES-256-GCM with a rotating key ring), key
// derivation (HKDF-SHA256) and random token helpers. It only uses the standard library.
//
// Ciphertext format: "v1.<keyid>.<base64url(nonce || sealed)>" where keyid is the first 8 hex characters of
// SHA-256(key). The version and key id are authenticated together with the caller's AAD.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the size in bytes of every key in the ring.
const KeySize = 32

const version = "v1"

var (
	// ErrInvalidKey reports a key that is not base64 of exactly 32 bytes, or a duplicate key.
	ErrInvalidKey = errors.New("crypto: invalid key")
	// ErrNoKeys reports an empty key ring.
	ErrNoKeys = errors.New("crypto: no keys")
	// ErrUnknownKey reports a ciphertext encrypted with a key that is not in the ring.
	ErrUnknownKey = errors.New("crypto: unknown key")
	// ErrMalformed reports a ciphertext that does not follow the v1 format.
	ErrMalformed = errors.New("crypto: malformed ciphertext")
	// ErrDecrypt reports an authentication failure (wrong key, wrong AAD or tampered data).
	ErrDecrypt = errors.New("crypto: decryption failed")
)

type key struct {
	id   string
	raw  []byte
	aead cipher.AEAD
}

// KeyRing is an ordered set of keys: the first one encrypts and derives, all of them decrypt.
type KeyRing struct {
	keys []key
}

// ParseKeyRing parses PORTO_SECRET_KEY: comma-separated base64 (standard or URL alphabet, padded or not)
// 32-byte keys, the active key first.
func ParseKeyRing(s string) (*KeyRing, error) {
	var ring KeyRing
	seen := map[string]bool{}
	for i, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		raw, err := decodeKey(part)
		if err != nil {
			return nil, fmt.Errorf("%w: key %d: %w", ErrInvalidKey, i+1, err)
		}
		k, err := newKey(raw)
		if err != nil {
			return nil, err
		}
		if seen[k.id] {
			return nil, fmt.Errorf("%w: key %d is a duplicate", ErrInvalidKey, i+1)
		}
		seen[k.id] = true
		ring.keys = append(ring.keys, k)
	}
	if len(ring.keys) == 0 {
		return nil, ErrNoKeys
	}
	return &ring, nil
}

func decodeKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := enc.Strict().DecodeString(s); err == nil {
			if len(raw) != KeySize {
				return nil, fmt.Errorf("decoded length %d, want %d", len(raw), KeySize)
			}
			return raw, nil
		}
	}
	return nil, errors.New("not base64")
}

func newKey(raw []byte) (key, error) {
	block, err := aes.NewCipher(raw)
	if err != nil {
		return key{}, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return key{}, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	sum := sha256.Sum256(raw)
	return key{id: hex.EncodeToString(sum[:4]), raw: raw, aead: aead}, nil
}

// GenerateKey returns a new random key, standard base64 encoded, suitable for PORTO_SECRET_KEY.
func GenerateKey() (string, error) {
	b := make([]byte, KeySize)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ActiveKeyID returns the id of the key used for encryption.
func (k *KeyRing) ActiveKeyID() string { return k.keys[0].id }

func additionalData(keyID string, aad []byte) []byte {
	out := make([]byte, 0, len(version)+1+len(keyID)+1+len(aad))
	out = append(out, version...)
	out = append(out, '.')
	out = append(out, keyID...)
	out = append(out, 0)
	return append(out, aad...)
}

// Encrypt seals plaintext with the active key. aad binds the ciphertext to its context (for example
// "webhook_endpoint:<id>:secret"); the same aad is required to decrypt.
func (k *KeyRing) Encrypt(plaintext, aad []byte) (string, error) {
	active := k.keys[0]
	nonce := make([]byte, active.aead.NonceSize(), active.aead.NonceSize()+len(plaintext)+active.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := active.aead.Seal(nonce, nonce, plaintext, additionalData(active.id, aad))
	return version + "." + active.id + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func parseCiphertext(c string) (keyID string, payload []byte, err error) {
	parts := strings.Split(c, ".")
	if len(parts) != 3 || parts[0] != version || len(parts[1]) != 8 {
		return "", nil, ErrMalformed
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", nil, ErrMalformed
	}
	payload, err = base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil {
		return "", nil, ErrMalformed
	}
	return parts[1], payload, nil
}

func (k *KeyRing) find(id string) (key, bool) {
	for _, kk := range k.keys {
		if kk.id == id {
			return kk, true
		}
	}
	return key{}, false
}

// Decrypt opens a ciphertext produced by Encrypt with any key in the ring.
func (k *KeyRing) Decrypt(ciphertext string, aad []byte) ([]byte, error) {
	id, payload, err := parseCiphertext(ciphertext)
	if err != nil {
		return nil, err
	}
	kk, ok := k.find(id)
	if !ok {
		return nil, ErrUnknownKey
	}
	ns := kk.aead.NonceSize()
	if len(payload) < ns+kk.aead.Overhead() {
		return nil, ErrMalformed
	}
	plain, err := kk.aead.Open(nil, payload[:ns], payload[ns:], additionalData(id, aad))
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}

// EncryptString is Encrypt for string values.
func (k *KeyRing) EncryptString(s, aad string) (string, error) {
	return k.Encrypt([]byte(s), []byte(aad))
}

// DecryptString is Decrypt for string values.
func (k *KeyRing) DecryptString(c, aad string) (string, error) {
	b, err := k.Decrypt(c, []byte(aad))
	return string(b), err
}

// NeedsRotation reports whether a well-formed ciphertext was sealed with a key other than the active one.
// Malformed input returns false: it cannot be re-encrypted.
func (k *KeyRing) NeedsRotation(ciphertext string) bool {
	id, _, err := parseCiphertext(ciphertext)
	return err == nil && id != k.keys[0].id
}

// Derive returns n bytes derived from the active key with HKDF-SHA256 and info = purpose (for example
// "session-csrf"). Different purposes give independent keys.
func (k *KeyRing) Derive(purpose string, n int) ([]byte, error) {
	if purpose == "" {
		return nil, errors.New("crypto: empty derivation purpose")
	}
	if n <= 0 || n > 255*sha256.Size {
		return nil, fmt.Errorf("crypto: invalid derived key length %d", n)
	}
	return hkdf.Key(sha256.New, k.keys[0].raw, nil, purpose, n)
}

// RandomToken returns nBytes of randomness, base64url encoded without padding.
func RandomToken(nBytes int) (string, error) {
	if nBytes <= 0 {
		return "", fmt.Errorf("crypto: invalid token size %d", nBytes)
	}
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CrockfordAlphabet is the Crockford base32 alphabet (no I, L, O, U).
const CrockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// RandomBase32 returns n random characters of the Crockford base32 alphabet (uppercase), for codes typed or
// read by humans such as pairing codes. Each character carries 5 unbiased bits.
func RandomBase32(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("crypto: invalid code length %d", n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = CrockfordAlphabet[b[i]&31] // 256 is a multiple of 32: no modulo bias
	}
	return string(b), nil
}

// SHA256Hex returns the lowercase hex SHA-256 of s (token lookup hashes).
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ConstantTimeEqual compares two secrets in constant time. Both are hashed first, so neither the content nor
// the length leaks through timing.
func ConstantTimeEqual(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
