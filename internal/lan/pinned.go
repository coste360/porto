package lan

import (
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrPinMismatch: the server certificate does not have the pinned fingerprint.
var ErrPinMismatch = errors.New("lan: certificate fingerprint mismatch")

// PinnedClientConfig returns a TLS client configuration that trusts exactly the leaf whose SHA-256 is fingerprint
// (lowercase or uppercase hex, with or without ':' separators, or base64url as in the pairing QR). It is the seam
// for native and remote clients (M6b) and what the tests use instead of skipping verification.
func PinnedClientConfig(fingerprint string) *tls.Config {
	want := decodeFingerprint(fingerprint)
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// The chain is self-issued (local CA): trust comes from the pin checked below, not from a root store.
		InsecureSkipVerify: true, //nolint:gosec // G402: replaced by VerifyConnection (fingerprint pin)
		// VerifyConnection also runs on resumed sessions, unlike VerifyPeerCertificate.
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(want) != 32 || len(cs.PeerCertificates) == 0 {
				return ErrPinMismatch
			}
			got, err := hex.DecodeString(FingerprintHex(cs.PeerCertificates[0].Raw))
			if err != nil || subtle.ConstantTimeCompare(got, want) != 1 {
				return ErrPinMismatch
			}
			return nil
		},
	}
}

func decodeFingerprint(fp string) []byte {
	fp = strings.TrimSpace(fp)
	if b, err := hex.DecodeString(strings.ReplaceAll(strings.ReplaceAll(fp, ":", ""), " ", "")); err == nil {
		return b
	}
	if b, err := base64.RawURLEncoding.DecodeString(fp); err == nil {
		return b
	}
	return nil
}
