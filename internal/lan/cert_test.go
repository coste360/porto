package lan

import (
	"bytes"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
)

func testKeys(t *testing.T) *crypto.KeyRing {
	_, kr := testKey(t)
	return kr
}

func testKey(t *testing.T) (string, *crypto.KeyRing) {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	kr, err := crypto.ParseKeyRing(k)
	if err != nil {
		t.Fatal(err)
	}
	return k, kr
}

var t0 = time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)

func TestLANCert_GenerateReloadRotate(t *testing.T) {
	dir := t.TempDir()
	c := clock.NewFake(t0)
	keys := testKeys(t)
	st := &CertStore{Dir: dir, Keys: keys, Clock: c}
	names := Names{IPs: addrs("192.168.1.20", "fd12::20"), DNS: []string{"localhost", "Ufficio.local"}}

	first, err := st.Ensure(names, false, false)
	if err != nil {
		t.Fatal(err)
	}
	ca, leaf := first.CA, first.Leaf
	if !ca.IsCA || !ca.MaxPathLenZero || ca.MaxPathLen != 0 || !strings.HasPrefix(ca.Subject.CommonName, "Porto local CA ") {
		t.Fatalf("CA %+v", ca.Subject)
	}
	if got := ca.NotAfter.Sub(t0); got < CAValidity-time.Hour || got > CAValidity {
		t.Fatalf("CA validity %v", got)
	}
	if got := leaf.NotAfter.Sub(t0); got != LeafValidity {
		t.Fatalf("leaf validity %v", got)
	}
	if leaf.IsCA || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth || !first.Covers(names) {
		t.Fatalf("leaf %v %v", leaf.DNSNames, leaf.IPAddresses)
	}
	if fp := first.Fingerprint(); len(fp) != 64 || len(first.FingerprintB64()) != 43 {
		t.Fatalf("fingerprints %q %q", fp, first.FingerprintB64())
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "192.168.1.20", CurrentTime: t0}); err != nil {
		t.Fatalf("leaf does not chain to the CA: %v", err)
	}

	// Reload: same certificates, stable fingerprint.
	again, err := st.Ensure(names, false, false)
	if err != nil || again.Fingerprint() != first.Fingerprint() || again.CAFingerprint() != first.CAFingerprint() {
		t.Fatalf("reload changed the certificate: %v", err)
	}
	loaded, err := (&CertStore{Dir: dir, Keys: keys, Clock: c}).Load()
	if err != nil || loaded.Fingerprint() != first.Fingerprint() {
		t.Fatalf("load: %v", err)
	}

	// Address change: re-issued, same CA.
	moved, err := st.Ensure(Names{IPs: addrs("10.0.0.20"), DNS: []string{"localhost"}}, false, false)
	if err != nil || moved.Fingerprint() == first.Fingerprint() || moved.CAFingerprint() != first.CAFingerprint() {
		t.Fatalf("address change: %v", err)
	}
	// A subset of the covered names keeps the leaf.
	kept, _ := st.Ensure(Names{IPs: addrs("10.0.0.20")}, false, false)
	if kept.Fingerprint() != moved.Fingerprint() {
		t.Fatal("covered names re-issued the leaf")
	}

	// Less than 30 days left: re-issued, same CA.
	c.Advance(LeafValidity - LeafRenewBefore + time.Minute)
	renewed, err := st.Ensure(Names{IPs: addrs("10.0.0.20")}, false, false)
	if err != nil || renewed.Fingerprint() == moved.Fingerprint() || renewed.CAFingerprint() != first.CAFingerprint() {
		t.Fatalf("renewal: %v", err)
	}
	if renewed.Leaf.NotAfter.Sub(c.Now()) != LeafValidity {
		t.Fatal("renewed leaf validity")
	}

	// On request: leaf only, then everything.
	asked, _ := st.Ensure(Names{IPs: addrs("10.0.0.20")}, true, false)
	if asked.Fingerprint() == renewed.Fingerprint() || asked.CAFingerprint() != first.CAFingerprint() {
		t.Fatal("reissue on request")
	}
	all, _ := st.Ensure(Names{IPs: addrs("10.0.0.20")}, false, true)
	if all.CAFingerprint() == first.CAFingerprint() || all.Fingerprint() == asked.Fingerprint() {
		t.Fatal("regenerate all kept the CA")
	}
	if err := all.Leaf.CheckSignatureFrom(all.CA); err != nil {
		t.Fatal(err)
	}
}

func TestLANCert_KeysEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	oldKey, keys := testKey(t)
	st := &CertStore{Dir: dir, Keys: keys, Clock: clock.NewFake(t0)}
	if _, err := st.Ensure(Names{IPs: addrs("192.168.1.20")}, false, false); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("PRIVATE KEY")) {
			t.Fatalf("%s holds a plaintext private key", e.Name())
		}
		if _, err := x509.ParsePKCS8PrivateKey(b); err == nil {
			t.Fatalf("%s holds a DER private key", e.Name())
		}
		info, _ := e.Info()
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", e.Name(), info.Mode().Perm())
		}
	}
	if strings.Join(names, ",") != "ca.crt,ca.key.enc,leaf.crt,leaf.key.enc" {
		t.Fatalf("files %v", names)
	}

	// Another secret key cannot open them, and Ensure never replaces them silently.
	other := &CertStore{Dir: dir, Keys: testKeys(t), Clock: clock.NewFake(t0)}
	if _, err := other.Load(); err == nil || errors.Is(err, ErrNoCertificate) {
		t.Fatalf("load with another key: %v", err)
	}
	if _, err := other.Ensure(Names{IPs: addrs("192.168.1.20")}, false, false); err == nil {
		t.Fatal("ensure with another key succeeded")
	}

	// Rotation of the secret key: the keys are sealed again with the active one.
	newKey, _ := crypto.GenerateKey()
	rotated, err := crypto.ParseKeyRing(newKey + "," + oldKey)
	if err != nil {
		t.Fatal(err)
	}
	rst := &CertStore{Dir: dir, Keys: rotated, Clock: clock.NewFake(t0)}
	before, _ := os.ReadFile(filepath.Join(dir, caKeyFile))
	if !rotated.NeedsRotation(strings.TrimSpace(string(before))) {
		t.Fatal("expected a retired key")
	}
	if _, err := rst.Ensure(Names{IPs: addrs("192.168.1.20")}, false, false); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{caKeyFile, leafKeyFile} {
		after, _ := os.ReadFile(filepath.Join(dir, f))
		if rotated.NeedsRotation(strings.TrimSpace(string(after))) {
			t.Errorf("%s not re-encrypted", f)
		}
	}
}
