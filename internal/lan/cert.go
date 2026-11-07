package lan

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
)

// Certificate lifetimes (m6a-plan §2.10, D13).
const (
	CAValidity   = 10 * 365 * 24 * time.Hour
	LeafValidity = 397 * 24 * time.Hour
	// LeafRenewBefore: the leaf is re-issued (same CA) when less than this is left.
	LeafRenewBefore = 30 * 24 * time.Hour
	// backdate absorbs small clock differences between the desktop and the phones.
	backdate = time.Hour
)

// File names under the certificate folder (<data>/lan). Private keys exist on disk only encrypted.
const (
	caCertFile   = "ca.crt"
	caKeyFile    = "ca.key.enc"
	leafCertFile = "leaf.crt"
	leafKeyFile  = "leaf.key.enc"
	aadCA        = "porto-lan-ca"
	aadLeaf      = "porto-lan-leaf"
)

// ErrNoCertificate: Load found no certificate (the local network was never enabled).
var ErrNoCertificate = errors.New("lan: no certificate")

// Names are the subject alternative names the leaf must cover.
type Names struct {
	IPs []netip.Addr
	DNS []string
}

// Certs is the local CA and the leaf in use.
type Certs struct {
	CA   *x509.Certificate
	Leaf *x509.Certificate
	// TLS is the leaf with its chain (leaf, CA) and private key, for the TLS listener.
	TLS tls.Certificate
}

// Fingerprint is the SHA-256 of the leaf DER, lowercase hex.
func (c *Certs) Fingerprint() string { return FingerprintHex(c.Leaf.Raw) }

// FingerprintB64 is the SHA-256 of the leaf DER, base64url without padding (43 characters, the QR's fp).
func (c *Certs) FingerprintB64() string { return FingerprintB64(c.Leaf.Raw) }

// CAFingerprint is the SHA-256 of the CA DER, lowercase hex (M6b installs the CA as a profile).
func (c *Certs) CAFingerprint() string { return FingerprintHex(c.CA.Raw) }

// FingerprintHex returns the SHA-256 of der in lowercase hex.
func FingerprintHex(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// FingerprintB64 returns the SHA-256 of der in base64url without padding.
func FingerprintB64(der []byte) string {
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Covers reports whether the leaf names every address and DNS name of n.
func (c *Certs) Covers(n Names) bool {
	for _, ip := range n.IPs {
		if !slices.ContainsFunc(c.Leaf.IPAddresses, func(x net.IP) bool {
			a, ok := netip.AddrFromSlice(x)
			return ok && a.Unmap() == ip.Unmap()
		}) {
			return false
		}
	}
	for _, d := range n.DNS {
		if !slices.Contains(c.Leaf.DNSNames, strings.ToLower(d)) {
			return false
		}
	}
	return true
}

// CertStore keeps the local CA and leaf in a folder, private keys sealed with the KeyRing (aad "porto-lan-ca" /
// "porto-lan-leaf"), and decides when they are re-issued.
type CertStore struct {
	Dir   string
	Keys  *crypto.KeyRing
	Clock clock.Clock
}

// Load reads the certificates without creating anything; ErrNoCertificate when there are none. A key sealed with
// another secret key fails (crypto.ErrUnknownKey or ErrDecrypt): it is never replaced silently.
func (s *CertStore) Load() (*Certs, error) {
	ca, caKey, err := s.read(caCertFile, caKeyFile, aadCA)
	if err != nil {
		return nil, err
	}
	leaf, leafKey, err := s.read(leafCertFile, leafKeyFile, aadLeaf)
	if err != nil {
		return nil, err
	}
	return assemble(ca, leaf, leafKey, caKey)
}

// Ensure returns certificates that cover names, creating or re-issuing them as needed: the CA when missing or when
// all is true (the "regenerate all" button); the leaf when missing, when reissue or all is true (the "regenerate certificate" button),
// when less than LeafRenewBefore is left, when it does not cover names or when it was not signed by the CA.
// Keys sealed with a retired KeyRing key are sealed again with the active one.
func (s *CertStore) Ensure(names Names, reissue, all bool) (*Certs, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("lan: certificate folder: %w", err)
	}
	now := s.Clock.Now()
	var (
		ca    *x509.Certificate
		caKey *ecdsa.PrivateKey
		err   error
	)
	if !all {
		ca, caKey, err = s.read(caCertFile, caKeyFile, aadCA)
		if err != nil && !errors.Is(err, ErrNoCertificate) {
			return nil, err
		}
		if ca != nil && !now.Before(ca.NotAfter) {
			ca = nil
		}
	}
	if ca == nil {
		if ca, caKey, err = s.createCA(now); err != nil {
			return nil, err
		}
		reissue = true
	}
	var (
		leaf    *x509.Certificate
		leafKey *ecdsa.PrivateKey
	)
	if !reissue {
		leaf, leafKey, err = s.read(leafCertFile, leafKeyFile, aadLeaf)
		if err != nil && !errors.Is(err, ErrNoCertificate) {
			return nil, err
		}
	}
	if leaf != nil {
		c, aerr := assemble(ca, leaf, leafKey, caKey)
		if aerr != nil || leaf.NotAfter.Sub(now) < LeafRenewBefore || !c.Covers(names) {
			leaf = nil
		}
	}
	if leaf == nil {
		if leaf, leafKey, err = s.createLeaf(now, names, ca, caKey); err != nil {
			return nil, err
		}
	}
	if err := s.resealIfRotated(caKeyFile, caKey, aadCA); err != nil {
		return nil, err
	}
	if err := s.resealIfRotated(leafKeyFile, leafKey, aadLeaf); err != nil {
		return nil, err
	}
	return assemble(ca, leaf, leafKey, caKey)
}

func assemble(ca, leaf *x509.Certificate, leafKey, caKey *ecdsa.PrivateKey) (*Certs, error) {
	if err := leaf.CheckSignatureFrom(ca); err != nil {
		return nil, fmt.Errorf("lan: leaf not signed by the local CA: %w", err)
	}
	if !caKey.PublicKey.Equal(ca.PublicKey) || !leafKey.PublicKey.Equal(leaf.PublicKey) {
		return nil, errors.New("lan: certificate and key do not match")
	}
	return &Certs{
		CA: ca, Leaf: leaf,
		TLS: tls.Certificate{Certificate: [][]byte{leaf.Raw, ca.Raw}, PrivateKey: leafKey, Leaf: leaf},
	}, nil
}

func (s *CertStore) read(certFile, keyFile, aad string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	pemBytes, err := os.ReadFile(filepath.Join(s.Dir, certFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrNoCertificate
	}
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, nil, fmt.Errorf("lan: %s is not a PEM certificate", certFile)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("lan: parse %s: %w", certFile, err)
	}
	sealed, err := os.ReadFile(filepath.Join(s.Dir, keyFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, ErrNoCertificate
	}
	if err != nil {
		return nil, nil, err
	}
	der, err := s.Keys.Decrypt(strings.TrimSpace(string(sealed)), []byte(aad))
	if err != nil {
		return nil, nil, fmt.Errorf("lan: open %s: %w", keyFile, err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, nil, fmt.Errorf("lan: parse %s: %w", keyFile, err)
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("lan: %s is not an ECDSA key", keyFile)
	}
	return cert, ek, nil
}

func (s *CertStore) createCA(now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Porto local CA " + serial.Text(16)[:8], Organization: []string{"Porto"}},
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(CAValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("lan: create CA: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	if err := s.write(caCertFile, caKeyFile, aadCA, cert, key); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func (s *CertStore) createLeaf(now time.Time, names Names, ca *x509.Certificate, caKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Porto", Organization: []string{"Porto"}},
		NotBefore:    now.Add(-backdate),
		NotAfter:     now.Add(LeafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, ip := range names.IPs {
		tmpl.IPAddresses = append(tmpl.IPAddresses, net.IP(ip.Unmap().AsSlice()))
	}
	for _, d := range names.DNS {
		if d = strings.ToLower(d); d != "" && !slices.Contains(tmpl.DNSNames, d) {
			tmpl.DNSNames = append(tmpl.DNSNames, d)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("lan: create leaf: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	if err := s.write(leafCertFile, leafKeyFile, aadLeaf, cert, key); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func randomSerial() (*big.Int, error) {
	// 127 bits keep the serial positive and below the 20-octet limit, with the top bit set so its hex is long.
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	if err != nil {
		return nil, err
	}
	return n.SetBit(n, 126, 1), nil
}

// write stores the certificate (PEM) and the sealed PKCS#8 key: key first, so a crash never leaves a
// certificate without its key; both through a temporary file and a rename.
func (s *CertStore) write(certFile, keyFile, aad string, cert *x509.Certificate, key *ecdsa.PrivateKey) error {
	if err := s.writeKey(keyFile, aad, key); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.Dir, certFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
}

func (s *CertStore) writeKey(keyFile, aad string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	sealed, err := s.Keys.Encrypt(der, []byte(aad))
	clear(der)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.Dir, keyFile), []byte(sealed+"\n"))
}

func (s *CertStore) resealIfRotated(keyFile string, key *ecdsa.PrivateKey, aad string) error {
	sealed, err := os.ReadFile(filepath.Join(s.Dir, keyFile))
	if err != nil {
		return err
	}
	if !s.Keys.NeedsRotation(strings.TrimSpace(string(sealed))) {
		return nil
	}
	return s.writeKey(keyFile, aad, key)
}

func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
