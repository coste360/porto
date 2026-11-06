package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses HMAC-SHA1; authenticator apps expect it
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/coste360/porto/internal/crypto"
)

// TOTP parameters (RFC 6238): HMAC-SHA1, 6 digits, 30 s steps, ±1 step of clock drift.
const (
	totpPeriod = 30
	totpDigits = 6
	totpSkew   = 1
	totpIssuer = "Porto"
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a random 160-bit secret in base32 (no padding).
func newTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return totpEncoding.EncodeToString(b), nil
}

// totpStep returns the time step of an instant.
func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpCode computes the code of a step (RFC 4226 dynamic truncation).
func totpCode(secret string, step int64) (string, error) {
	key, err := totpEncoding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("auth: bad totp secret: %w", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", bin%1_000_000), nil
}

// matchTOTP returns the step that code matches around now (±skew), or false.
func matchTOTP(secret, code string, now time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	center := totpStep(now)
	for d := -totpSkew; d <= totpSkew; d++ {
		step := center + int64(d)
		want, err := totpCode(secret, step)
		if err != nil {
			return 0, false
		}
		if crypto.ConstantTimeEqual(want, code) {
			return step, true
		}
	}
	return 0, false
}

// otpauthURL is the key URI understood by authenticator apps.
func otpauthURL(secret, account string) string {
	label := url.PathEscape(totpIssuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// Recovery codes: 10 codes XXXX-XXXX in Crockford base32, stored as SHA-256, single use.
const recoveryCodeCount = 10

func newRecoveryCodes() (codes, hashes []string, err error) {
	for range recoveryCodeCount {
		raw, err := crypto.RandomBase32(8)
		if err != nil {
			return nil, nil, err
		}
		code := raw[:4] + "-" + raw[4:]
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return codes, hashes, nil
}

// normalizeCrockford uppercases, drops separators and maps the Crockford aliases (I, L → 1; O → 0).
func normalizeCrockford(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case '-', ' ':
			continue
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func hashRecoveryCode(code string) string {
	return crypto.SHA256Hex("recovery:" + normalizeCrockford(code))
}
