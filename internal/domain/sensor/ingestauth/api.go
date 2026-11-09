package ingestauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Protocol constants (m12-plan §4.2).
const (
	// Tolerance is the accepted |now - t| of a signature.
	Tolerance = 300 * time.Second
	// MaxSignatures is the number of v1= values accepted in one Porto-Signature (vendor key rollover).
	MaxSignatures = 2
	// DevicePrefix and GatewayPrefix start the key ids and the keys.
	DevicePrefix  = "po_sns_"
	GatewayPrefix = "po_gw_"
	// KidLen is the length of the lowercase base32 key id part (10 random bytes); SecretLen of the base64url secret
	// (32 random bytes, no padding).
	KidLen    = 16
	SecretLen = 43
)

// Errors. ErrUnauthorized covers every failure but the timestamp; callers answer both without telling which check
// failed (except the timestamp, which carries the server time).
var (
	ErrUnauthorized        = errors.New("ingestauth: unauthorized")
	ErrTimestampOutOfRange = errors.New("ingestauth: timestamp out of range")
)

// Mode is how a request authenticates.
type Mode int

// Modes.
const (
	ModeHMAC Mode = iota + 1
	ModeBearer
)

// Headers are the authentication headers of a request.
type Headers struct {
	PortoKey       string // po_sns_<kid> or po_gw_<kid>
	PortoSignature string // t=<unix>,v1=<hex>[,v1=<hex>]
	Authorization  string // Bearer po_gw_<kid>_<secret>
}

// Parsed is the parsed authentication of a request.
type Parsed struct {
	Mode       Mode
	KeyID      string // po_sns_<kid> or po_gw_<kid>
	Gateway    bool
	Timestamp  int64    // HMAC: t
	Signatures [][]byte // HMAC: decoded v1 values (1..MaxSignatures)
	Bearer     string   // bearer: the full presented key
}

// kidEncoding is lowercase RFC 4648 base32 without padding: 10 bytes give exactly 16 characters.
var kidEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// hmacCount counts the HMAC computations of VerifyHMAC (TestIngestAuth_ConstantTime asserts that an unknown key
// still computes one).
var hmacCount atomic.Int64

// maxHeaderLen bounds the parsed headers (a garbled header is refused before any work).
const maxHeaderLen = 512

// ParseHeaders parses the headers: HMAC when Porto-Key is present, bearer otherwise (gateway keys only). Garbled,
// missing or mixed headers are ErrUnauthorized.
func ParseHeaders(h Headers) (Parsed, error) {
	key, sig, authz := strings.TrimSpace(h.PortoKey), strings.TrimSpace(h.PortoSignature), strings.TrimSpace(h.Authorization)
	if len(key) > maxHeaderLen || len(sig) > maxHeaderLen || len(authz) > maxHeaderLen {
		return Parsed{}, ErrUnauthorized
	}
	if key != "" {
		if authz != "" || !ValidKeyID(key) {
			return Parsed{}, ErrUnauthorized
		}
		t, sigs, ok := parseSignature(sig)
		if !ok {
			return Parsed{}, ErrUnauthorized
		}
		return Parsed{Mode: ModeHMAC, KeyID: key, Gateway: strings.HasPrefix(key, GatewayPrefix), Timestamp: t, Signatures: sigs}, nil
	}
	if sig != "" || authz == "" {
		return Parsed{}, ErrUnauthorized
	}
	scheme, token, ok := strings.Cut(authz, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return Parsed{}, ErrUnauthorized
	}
	token = strings.TrimSpace(token)
	id, ok := KeyIDOf(token)
	if !ok || !strings.HasPrefix(id, GatewayPrefix) {
		// A device key used as bearer is refused: devices must sign.
		return Parsed{}, ErrUnauthorized
	}
	return Parsed{Mode: ModeBearer, KeyID: id, Gateway: true, Bearer: token}, nil
}

// parseSignature parses "t=<unix>,v1=<hex>[,v1=<hex>]": exactly one t, 1..MaxSignatures v1 of 64 hex characters.
func parseSignature(s string) (int64, [][]byte, bool) {
	if s == "" {
		return 0, nil, false
	}
	var t int64
	seenT := false
	var sigs [][]byte
	for part := range strings.SplitSeq(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return 0, nil, false
		}
		switch k {
		case "t":
			if seenT || v == "" || len(v) > 12 {
				return 0, nil, false
			}
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n <= 0 {
				return 0, nil, false
			}
			t, seenT = n, true
		case "v1":
			if len(v) != 2*sha256.Size || len(sigs) == MaxSignatures {
				return 0, nil, false
			}
			b, err := hex.DecodeString(v)
			if err != nil {
				return 0, nil, false
			}
			sigs = append(sigs, b)
		default:
			return 0, nil, false
		}
	}
	if !seenT || len(sigs) == 0 {
		return 0, nil, false
	}
	return t, sigs, true
}

// Canonical returns the signed string "v1\n<t>\n<METHOD>\n<path>\n<hex sha256(body)>" (no trailing newline).
func Canonical(t int64, method, path string, body []byte) string {
	sum := sha256.Sum256(body)
	return "v1\n" + strconv.FormatInt(t, 10) + "\n" + method + "\n" + path + "\n" + hex.EncodeToString(sum[:])
}

func mac(key string, canonical string) []byte {
	hmacCount.Add(1)
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(canonical))
	return m.Sum(nil)
}

// Sign returns the lowercase hex HMAC-SHA256 of Canonical with the ASCII bytes of the full key.
func Sign(key string, t int64, method, path string, body []byte) string {
	return hex.EncodeToString(mac(key, Canonical(t, method, path, body)))
}

// dummyKey is the HMAC key of an unknown key id (comparable work; never matches a real key's verifier).
const dummyKey = "po_sns_aaaaaaaaaaaaaaaa_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// VerifyHMAC checks a parsed HMAC request against the full key: any of the signatures must match (hmac.Equal),
// then |now - t| <= Tolerance (ErrTimestampOutOfRange only for a valid signature). With key == "" (unknown key id)
// it still computes one HMAC over the canonical string and returns ErrUnauthorized (comparable work).
func VerifyHMAC(key string, p Parsed, method, path string, body []byte, now time.Time) error {
	if p.Mode != ModeHMAC || len(p.Signatures) == 0 {
		return ErrUnauthorized
	}
	known := key != ""
	if !known {
		key = dummyKey
	}
	want := mac(key, Canonical(p.Timestamp, method, path, body))
	match := 0
	for _, s := range p.Signatures {
		if hmac.Equal(want, s) {
			match = 1
		}
	}
	if !known || match == 0 {
		return ErrUnauthorized
	}
	d := now.Unix() - p.Timestamp
	if d < 0 {
		d = -d
	}
	if d > int64(Tolerance/time.Second) {
		return ErrTimestampOutOfRange
	}
	return nil
}

// VerifyBearer compares SHA-256(presented) with the stored hex hash in constant time.
func VerifyBearer(presented, storedHash string) bool {
	got := HashKey(presented)
	ok := subtle.ConstantTimeCompare([]byte(got), []byte(storedHash)) == 1
	return ok && storedHash != ""
}

// HashKey returns the lowercase hex SHA-256 of the full key (the stored key_hash).
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// FormatKey builds a key from its random parts: prefix (DevicePrefix or GatewayPrefix), kid (10 bytes, lowercase
// base32 without padding) and secret (32 bytes, base64url without padding). It returns the full key and its key id.
func FormatKey(prefix string, kid [10]byte, secret [32]byte) (key, keyID string) {
	keyID = prefix + kidEncoding.EncodeToString(kid[:])
	return keyID + "_" + base64.RawURLEncoding.EncodeToString(secret[:]), keyID
}

// KeyIDOf returns the key id of a full key and whether the key is well formed.
func KeyIDOf(key string) (keyID string, ok bool) {
	var prefix string
	switch {
	case strings.HasPrefix(key, DevicePrefix):
		prefix = DevicePrefix
	case strings.HasPrefix(key, GatewayPrefix):
		prefix = GatewayPrefix
	default:
		return "", false
	}
	if len(key) != len(prefix)+KidLen+1+SecretLen {
		return "", false
	}
	id := key[:len(prefix)+KidLen]
	if !ValidKeyID(id) || key[len(id)] != '_' {
		return "", false
	}
	secret := key[len(id)+1:]
	if _, err := base64.RawURLEncoding.Strict().DecodeString(secret); err != nil {
		return "", false
	}
	return id, true
}

// ValidKeyID reports whether s is a well-formed key id (prefix plus KidLen base32 characters).
func ValidKeyID(s string) bool {
	var rest string
	switch {
	case strings.HasPrefix(s, DevicePrefix):
		rest = s[len(DevicePrefix):]
	case strings.HasPrefix(s, GatewayPrefix):
		rest = s[len(GatewayPrefix):]
	default:
		return false
	}
	if len(rest) != KidLen {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}
