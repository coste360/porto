package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are the Argon2id cost parameters.
type Argon2Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	KeyLen      uint32
	SaltLen     uint32
}

// DefaultArgon2Params: m=64 MiB, t=3, p=2, 32-byte key, 16-byte salt.
var DefaultArgon2Params = Argon2Params{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2, KeyLen: 32, SaltLen: 16}

// TestArgon2Params are cheap parameters for tests only.
var TestArgon2Params = Argon2Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1, KeyLen: 32, SaltLen: 16}

// Hasher hashes passwords with Argon2id into PHC strings:
// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash> (unpadded standard base64).
type Hasher struct {
	Params Argon2Params
}

var b64 = base64.RawStdEncoding

// Hash returns the PHC string of pw.
func (h *Hasher) Hash(pw string) (string, error) {
	salt := make([]byte, h.Params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	p := h.Params
	key := argon2.IDKey([]byte(pw), salt, p.Iterations, p.MemoryKiB, p.Parallelism, p.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.MemoryKiB, p.Iterations,
		p.Parallelism, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

type phc struct {
	params Argon2Params
	salt   []byte
	key    []byte
}

func parsePHC(s string) (phc, error) {
	parts := strings.Split(s, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash
	if len(parts) != 6 || parts[1] != "argon2id" {
		return phc{}, fmt.Errorf("auth: unsupported password hash")
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return phc{}, fmt.Errorf("auth: unsupported argon2 version")
	}
	var out phc
	var par uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &out.params.MemoryKiB, &out.params.Iterations, &par); err != nil ||
		par == 0 || par > 255 || out.params.Iterations == 0 || out.params.MemoryKiB == 0 {
		return phc{}, fmt.Errorf("auth: malformed argon2 parameters")
	}
	out.params.Parallelism = uint8(par)
	var err error
	if out.salt, err = b64.DecodeString(parts[4]); err != nil || len(out.salt) == 0 {
		return phc{}, fmt.Errorf("auth: malformed salt")
	}
	if out.key, err = b64.DecodeString(parts[5]); err != nil || len(out.key) == 0 {
		return phc{}, fmt.Errorf("auth: malformed hash")
	}
	out.params.SaltLen = uint32(len(out.salt))
	out.params.KeyLen = uint32(len(out.key))
	return out, nil
}

// Verify reports whether pw matches the PHC string (constant-time comparison).
func (h *Hasher) Verify(pw, encoded string) (bool, error) {
	p, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	key := argon2.IDKey([]byte(pw), p.salt, p.params.Iterations, p.params.MemoryKiB, p.params.Parallelism, p.params.KeyLen)
	return subtle.ConstantTimeCompare(key, p.key) == 1, nil
}

// NeedsRehash reports whether a hash was made with different parameters than the current ones.
func (h *Hasher) NeedsRehash(encoded string) bool {
	p, err := parsePHC(encoded)
	if err != nil {
		return true
	}
	c := h.Params
	return p.params.MemoryKiB != c.MemoryKiB || p.params.Iterations != c.Iterations ||
		p.params.Parallelism != c.Parallelism || p.params.KeyLen != c.KeyLen || p.params.SaltLen != c.SaltLen
}

// Password policy.
const (
	MinPasswordRunes = 12
	MaxPasswordRunes = 256
)

// ValidatePassword enforces the password policy: 12 to 256 characters, not a single repeated character.
func ValidatePassword(pw string) error {
	if !utf8.ValidString(pw) {
		return ErrWeakPassword
	}
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordRunes || n > MaxPasswordRunes {
		return ErrWeakPassword
	}
	first, _ := utf8.DecodeRuneInString(pw)
	if strings.Trim(pw, string(first)) == "" {
		return ErrWeakPassword
	}
	return nil
}
