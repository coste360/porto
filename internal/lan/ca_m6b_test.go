package lan

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/desktopport"
)

func TestLANCert_NameConstraints(t *testing.T) {
	c := clock.NewFake(t0)
	st := &CertStore{Dir: t.TempDir(), Keys: testKeys(t), Clock: c}
	names := Names{IPs: addrs("192.168.1.20", "127.0.0.1", "fd12::20"), DNS: []string{"localhost", "ufficio.local"}}
	certs, err := st.Ensure(names, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !certs.CAConstrained() || !certs.CA.PermittedDNSDomainsCritical {
		t.Fatal("new CA is not name constrained")
	}
	roots := x509.NewCertPool()
	roots.AddCert(certs.CA)
	for _, host := range []string{"192.168.1.20", "127.0.0.1", "ufficio.local", "localhost"} {
		if _, err := certs.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: host}); err != nil {
			t.Errorf("leaf for %s refused: %v", host, err)
		}
	}

	// A leaf the CA signs for a public name or address must not verify (the CA cannot vouch for the internet).
	_, caPriv, err := st.read(caCertFile, caKeyFile, aadCA)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		dns string
		ip  net.IP
	}{{dns: "www.example.com"}, {ip: net.ParseIP("8.8.8.8")}} {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(42), Subject: pkix.Name{CommonName: "evil"},
			NotBefore: t0.Add(-1), NotAfter: t0.Add(LeafValidity), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		host := bad.dns
		if bad.dns != "" {
			tmpl.DNSNames = []string{bad.dns}
		} else {
			tmpl.IPAddresses = []net.IP{bad.ip}
			host = bad.ip.String()
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, certs.CA, &k.PublicKey, caPriv)
		if err != nil {
			t.Fatal(err)
		}
		evil, _ := x509.ParseCertificate(der)
		if _, err := evil.Verify(x509.VerifyOptions{Roots: roots, DNSName: host, CurrentTime: t0}); err == nil {
			t.Errorf("a leaf for %s verified under the constrained CA", host)
		}
	}
}

func TestLANCert_OldCAUnconstrainedUntilRotateAll(t *testing.T) {
	dir := t.TempDir()
	c := clock.NewFake(t0)
	keys := testKeys(t)
	st := &CertStore{Dir: dir, Keys: keys, Clock: c}
	names := Names{IPs: addrs("127.0.0.1"), DNS: []string{"localhost"}}
	// An M6a CA: same files, no constraints.
	writeUnconstrainedCA(t, st)
	certs, err := st.Ensure(names, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if certs.CAConstrained() {
		t.Fatal("an M6a CA reported constrained")
	}
	certs, err = st.Ensure(names, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !certs.CAConstrained() {
		t.Fatal("rotate all did not constrain the CA")
	}
}

func writeUnconstrainedCA(t *testing.T, st *CertStore) {
	t.Helper()
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "Porto local CA 00000000"},
		NotBefore: t0.Add(-1), NotAfter: t0.Add(CAValidity), KeyUsage: x509.KeyUsageCertSign,
		BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	if err := st.write(caCertFile, caKeyFile, aadCA, cert, k); err != nil {
		t.Fatal(err)
	}
}

func TestLAN_CADownload(t *testing.T) {
	ctx := context.Background()
	s := newTestServer(t, Options{})
	st, err := s.Enable(ctx, desktopport.LANOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !st.CAConstrained || st.CADownloadURL != st.URL+CACertPath {
		t.Fatalf("status %+v", st)
	}
	cl := pinnedClient(st.Fingerprint)
	get := func(path string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1:"+strconv.Itoa(st.Port)+path, nil)
		res, err := cl.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, b
	}
	res, body := get(CACertPath)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/x-x509-ca-cert" {
		t.Fatalf("crt %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	ca, err := x509.ParseCertificate(body)
	if err != nil || FingerprintHex(ca.Raw) != st.CAFingerprint || !Constrained(ca) {
		t.Fatalf("downloaded CA %v", err)
	}
	res, body = get(CAMobileConfigPth)
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("com.apple.security.root")) ||
		strings.Contains(string(body), "PRIVATE KEY") {
		t.Fatalf("mobileconfig %d", res.StatusCode)
	}
	// Everything else still reaches the app.
	if res, body := get("/admin/"); res.StatusCode != http.StatusOK || string(body) != "lan" {
		t.Fatalf("app %d %q", res.StatusCode, body)
	}

	// An unconstrained (M6a) CA is not offered.
	_ = s.Disable(ctx)
	dir := t.TempDir()
	s2 := newTestServer(t, Options{Dir: dir})
	writeUnconstrainedCA(t, &CertStore{Dir: dir, Keys: s2.opts.Keys, Clock: s2.opts.Clock})
	st2, err := s2.Enable(ctx, desktopport.LANOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st2.CAConstrained || st2.CADownloadURL != "" {
		t.Fatalf("M6a CA offered: %+v", st2)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1:"+strconv.Itoa(st2.Port)+CACertPath, nil)
	res2, err := pinnedClient(st2.Fingerprint).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusNotFound {
		t.Fatalf("unconstrained CA download %d", res2.StatusCode)
	}
}
