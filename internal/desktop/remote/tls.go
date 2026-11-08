package remote

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/coste360/porto/internal/desktopport"
)

// TLS policy of the remote mode (m6b-plan §7.3, D12): a pairing payload with a fingerprint (self-signed or LAN
// target) pins the leaf; at pairing the issuer of the presented chain is recorded as the pinned CA when the leaf
// verifies against it. Afterwards the pinned leaf, or any leaf issued by the pinned CA for the host, is accepted;
// anything else is refused with a *desktopport.PinMismatchError. Without a fingerprint the system roots and the
// usual hostname verification apply (public CAs rotate leaves often).

// Pins are the pinned fingerprints, lowercase hex SHA-256 ("" = none).
type Pins struct {
	LeafFP string
	CAFP   string
}

// Fingerprint returns the SHA-256 of der in lowercase hex.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// NormalizeFingerprint accepts hex (with or without ':' separators, any case) or base64url (the pairing QR's fp)
// and returns lowercase hex, or "" when fp is not a SHA-256.
func NormalizeFingerprint(fp string) string {
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return ""
	}
	if b, err := hex.DecodeString(strings.ReplaceAll(fp, ":", "")); err == nil && len(b) == sha256.Size {
		return hex.EncodeToString(b)
	}
	if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(fp, "=")); err == nil && len(b) == sha256.Size {
		return hex.EncodeToString(b)
	}
	return ""
}

func equalFP(a, b string) bool {
	return a != "" && len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// PairingObservation records what the pairing connection presented.
type PairingObservation struct {
	mu     sync.Mutex
	leafFP string
	caFP   string
}

// Pins returns the observed pins (the CA only when the leaf verified against the presented issuer).
func (o *PairingObservation) Pins() Pins {
	o.mu.Lock()
	defer o.mu.Unlock()
	return Pins{LeafFP: o.leafFP, CAFP: o.caFP}
}

// PairingTLSConfig accepts exactly the leaf whose fingerprint is leafFP (hex) and records the issuing CA.
func PairingTLSConfig(leafFP string, obs *PairingObservation) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Trust comes from the fingerprint pin checked in VerifyConnection, not from a root store.
		InsecureSkipVerify: true, //nolint:gosec // G402: replaced by VerifyConnection (fingerprint pin)
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return &desktopport.PinMismatchError{Expected: leafFP}
			}
			leaf := cs.PeerCertificates[0]
			got := Fingerprint(leaf.Raw)
			if !equalFP(got, leafFP) {
				return &desktopport.PinMismatchError{Expected: leafFP, Presented: got}
			}
			ca := ""
			if len(cs.PeerCertificates) > 1 {
				issuer := cs.PeerCertificates[1]
				if issuer.IsCA && leaf.CheckSignatureFrom(issuer) == nil {
					ca = Fingerprint(issuer.Raw)
				}
			}
			if obs != nil {
				obs.mu.Lock()
				obs.leafFP, obs.caFP = got, ca
				obs.mu.Unlock()
			}
			return nil
		},
	}
}

// ClientTLSConfig is the TLS policy after pairing for host (the URL host without port). With no pins it is the
// default verification against the system roots.
func ClientTLSConfig(host string, p Pins) *tls.Config {
	if p.LeafFP == "" && p.CAFP == "" {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, //nolint:gosec // G402: replaced by VerifyConnection (leaf or CA pin)
		VerifyConnection: func(cs tls.ConnectionState) error {
			return verifyPinned(cs.PeerCertificates, host, p)
		},
	}
}

func verifyPinned(chain []*x509.Certificate, host string, p Pins) error {
	if len(chain) == 0 {
		return &desktopport.PinMismatchError{Expected: p.LeafFP}
	}
	leaf := chain[0]
	got := Fingerprint(leaf.Raw)
	if equalFP(got, p.LeafFP) {
		return nil
	}
	if p.CAFP != "" {
		for _, c := range chain[1:] {
			if !c.IsCA || !equalFP(Fingerprint(c.Raw), p.CAFP) {
				continue
			}
			roots := x509.NewCertPool()
			roots.AddCert(c)
			if _, err := leaf.Verify(x509.VerifyOptions{
				Roots: roots, DNSName: host, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			}); err == nil {
				return nil
			}
		}
	}
	return &desktopport.PinMismatchError{Expected: p.LeafFP, Presented: got}
}

// FingerprintB64 returns the SHA-256 of der in base64url without padding (the pairing QR form).
func FingerprintB64(der []byte) string {
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
