package sensor

import (
	"crypto/rand"
	"time"

	"github.com/coste360/porto/internal/domain/sensor/ingestauth"
	"github.com/coste360/porto/internal/store"
)

// issuedKey is a freshly issued key: the plaintext (returned once, never stored or logged), its public id, its
// hash and its KeyRing-encrypted verifier (m12-plan §3.3).
type issuedKey struct {
	plain, id, hash, verifier string
}

// AAD prefixes of the encrypted verifiers (and of the source configuration).
const (
	aadDevice  = "sensor-key:"
	aadGateway = "gateway-key:"
	aadSource  = "sensor-source:"
)

// issueKey draws a random key with prefix (device or gateway) and encrypts its verifier bound to aad.
func (s *Service) issueKey(prefix, aad string) (issuedKey, error) {
	var kid [10]byte
	var secret [32]byte
	if _, err := rand.Read(kid[:]); err != nil {
		return issuedKey{}, err
	}
	if _, err := rand.Read(secret[:]); err != nil {
		return issuedKey{}, err
	}
	plain, id := ingestauth.FormatKey(prefix, kid, secret)
	verifier, err := s.d.Keys.EncryptString(plain, aad)
	if err != nil {
		return issuedKey{}, err
	}
	return issuedKey{plain: plain, id: id, hash: ingestauth.HashKey(plain), verifier: verifier}, nil
}

// keyCols are the key columns of a device or a gateway, addressed by pointer so rotate and revoke are written
// once for both.
type keyCols struct {
	KeyID, KeyHash, KeyVerifierEnc             *string
	KeyIssuedAt                                **time.Time
	PrevKeyID, PrevKeyHash, PrevKeyVerifierEnc *string
	PrevValidUntil                             **time.Time
}

func deviceKeyCols(d *store.SensorDevice) keyCols {
	return keyCols{&d.KeyID, &d.KeyHash, &d.KeyVerifierEnc, &d.KeyIssuedAt, &d.PrevKeyID, &d.PrevKeyHash, &d.PrevKeyVerifierEnc, &d.PrevValidUntil}
}

func gatewayKeyCols(g *store.SensorGateway) keyCols {
	return keyCols{&g.KeyID, &g.KeyHash, &g.KeyVerifierEnc, &g.KeyIssuedAt, &g.PrevKeyID, &g.PrevKeyHash, &g.PrevKeyVerifierEnc, &g.PrevValidUntil}
}

// set installs k as the current key issued at now (the previous key is left as is).
func (c keyCols) set(k issuedKey, now time.Time) {
	t := store.UTC(now)
	*c.KeyID, *c.KeyHash, *c.KeyVerifierEnc, *c.KeyIssuedAt = k.id, k.hash, k.verifier, &t
}

// rotate moves the current key (if any) to previous with a grace until now + grace (none when grace is 0) and
// installs k.
func (c keyCols) rotate(k issuedKey, now time.Time, grace time.Duration) {
	*c.PrevKeyID, *c.PrevKeyHash, *c.PrevKeyVerifierEnc, *c.PrevValidUntil = "", "", "", nil
	if *c.KeyID != "" && grace > 0 {
		until := store.UTC(now.Add(grace))
		*c.PrevKeyID, *c.PrevKeyHash, *c.PrevKeyVerifierEnc, *c.PrevValidUntil = *c.KeyID, *c.KeyHash, *c.KeyVerifierEnc, &until
	}
	c.set(k, now)
}

// revoke clears the current and the previous key.
func (c keyCols) revoke() {
	*c.KeyID, *c.KeyHash, *c.KeyVerifierEnc, *c.KeyIssuedAt = "", "", "", nil
	*c.PrevKeyID, *c.PrevKeyHash, *c.PrevKeyVerifierEnc, *c.PrevValidUntil = "", "", "", nil
}

// keyStatus is active, rotating (previous key still in grace), revoked (no key) or unusable (the verifier cannot
// be decrypted with this instance's PORTO_SECRET_KEY, e.g. after a restore elsewhere).
func (s *Service) keyStatus(c keyCols, aad string, now time.Time) string {
	if *c.KeyID == "" {
		return KeyRevoked
	}
	if _, err := s.d.Keys.DecryptString(*c.KeyVerifierEnc, aad); err != nil {
		return KeyUnusable
	}
	if *c.PrevKeyID != "" && *c.PrevValidUntil != nil && now.Before(**c.PrevValidUntil) {
		return KeyRotating
	}
	return KeyActive
}

// matchKey returns the verifier and hash of the key id presented, current or previous within its grace ("" when
// neither matches).
func matchKey(c keyCols, keyID string, now time.Time) (verifier, hash string) {
	switch {
	case *c.KeyID != "" && *c.KeyID == keyID:
		return *c.KeyVerifierEnc, *c.KeyHash
	case *c.PrevKeyID != "" && *c.PrevKeyID == keyID && *c.PrevValidUntil != nil && now.Before(**c.PrevValidUntil):
		return *c.PrevKeyVerifierEnc, *c.PrevKeyHash
	}
	return "", ""
}
