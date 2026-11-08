package remote

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/desktopport"
)

const testToken = "po_dev_abcdefgh_SECRETSECRETSECRET"

// --- certificates -------------------------------------------------------------------------------------------

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newCA(t *testing.T, name string) testCA {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return testCA{cert: c, key: k}
}

func (ca testCA) leaf(t *testing.T) tls.Certificate {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "porto"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &k.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.cert.Raw}, PrivateKey: k}
}

func tlsServer(t *testing.T, cert tls.Certificate, h http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(h)
	s.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func get(t *testing.T, tr *http.Transport, u string) error {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	res, err := (&http.Client{Transport: tr, Timeout: 5 * time.Second}).Do(req)
	if err == nil {
		res.Body.Close()
	}
	tr.CloseIdleConnections()
	return err
}

func TestRemoteTLS_PinLeafThenCA(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	ca := newCA(t, "Porto local CA")
	leaf1 := ca.leaf(t)
	s1 := tlsServer(t, leaf1, ok)
	u1, _ := url.Parse(s1.URL)
	fp1 := Fingerprint(leaf1.Certificate[0])

	// Pairing: the pinned leaf is accepted and its issuer recorded.
	obs := &PairingObservation{}
	tr := NewTransport(u1, Pins{})
	tr.TLSClientConfig = PairingTLSConfig(fp1, obs)
	if err := get(t, tr, s1.URL); err != nil {
		t.Fatalf("pairing with the pinned leaf: %v", err)
	}
	pins := obs.Pins()
	if pins.LeafFP != fp1 || pins.CAFP != Fingerprint(ca.cert.Raw) {
		t.Fatalf("pins %+v", pins)
	}
	// Pairing with another fingerprint is refused with both fingerprints.
	tr = NewTransport(u1, Pins{})
	tr.TLSClientConfig = PairingTLSConfig(strings.Repeat("0", 64), nil)
	var pm *desktopport.PinMismatchError
	if err := get(t, tr, s1.URL); !errors.As(err, &pm) || pm.Presented != fp1 {
		t.Fatalf("wrong pin accepted: %v", err)
	}

	// After pairing: the same leaf, and a rotated leaf under the pinned CA, are accepted.
	if err := get(t, NewTransport(u1, pins), s1.URL); err != nil {
		t.Fatalf("pinned leaf: %v", err)
	}
	s2 := tlsServer(t, ca.leaf(t), ok)
	u2, _ := url.Parse(s2.URL)
	if err := get(t, NewTransport(u2, pins), s2.URL); err != nil {
		t.Fatalf("rotated leaf under the pinned CA: %v", err)
	}
	// A certificate of another CA is refused.
	other := newCA(t, "Mallory CA")
	s3 := tlsServer(t, other.leaf(t), ok)
	u3, _ := url.Parse(s3.URL)
	if err := get(t, NewTransport(u3, pins), s3.URL); !errors.As(err, &pm) {
		t.Fatalf("foreign certificate accepted: %v", err)
	}
	// A leaf-only pin (no CA recorded) refuses a rotated leaf.
	if err := get(t, NewTransport(u2, Pins{LeafFP: fp1}), s2.URL); !errors.As(err, &pm) {
		t.Fatalf("rotated leaf accepted without a CA pin: %v", err)
	}
}

func TestRemoteTLS_NoFpUsesSystemRoots(t *testing.T) {
	ca := newCA(t, "Private CA")
	s := tlsServer(t, ca.leaf(t), http.NotFoundHandler())
	u, _ := url.Parse(s.URL)
	cfg := ClientTLSConfig(u.Hostname(), Pins{})
	if cfg.InsecureSkipVerify || cfg.VerifyConnection != nil {
		t.Fatal("no pin must use the default verification")
	}
	if err := get(t, NewTransport(u, Pins{}), s.URL); err == nil {
		t.Fatal("a certificate of an unknown CA was accepted without a pin")
	}
}

func TestNormalizeFingerprintAndLink(t *testing.T) {
	hexFP := strings.Repeat("ab", 32)
	b64 := "q6urq6urq6urq6urq6urq6urq6urq6urq6urq6urq6s"
	if NormalizeFingerprint(hexFP) != hexFP || NormalizeFingerprint(b64) != hexFP || NormalizeFingerprint("AB:"+strings.Repeat("AB", 31)) != hexFP {
		t.Fatal("fingerprint forms")
	}
	if NormalizeFingerprint("abc") != "" {
		t.Fatal("short fingerprint accepted")
	}
	l, err := ParsePairLink("porto://pair?v=1&url=https%3A%2F%2F192.168.1.5%3A8443&code=ABCD-1234&fp=" + b64)
	if err != nil || l.URL != "https://192.168.1.5:8443" || l.Code != "ABCD-1234" || l.Fingerprint != b64 {
		t.Fatalf("link %+v %v", l, err)
	}
	for _, bad := range []string{"https://x", "porto://other?url=x&code=y", "porto://pair?v=2&url=x&code=y", "porto://pair?code=y"} {
		if _, err := ParsePairLink(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, c := range []struct {
		raw string
		dev bool
		ok  bool
	}{
		{"https://porto.example.it", false, true},
		{"https://192.168.1.5:8443/", false, true},
		{"http://127.0.0.1:18100", true, true},
		{"http://127.0.0.1:18100", false, false},
		{"https://u:p@x.it", false, false},
		{"https://x.it/admin", false, false},
		{"ftp://x.it", true, false},
	} {
		if _, err := ParseOrigin(c.raw, c.dev); (err == nil) != c.ok {
			t.Errorf("ParseOrigin(%q, %v) = %v", c.raw, c.dev, err)
		}
	}
}

// --- proxy ---------------------------------------------------------------------------------------------------

type upstream struct {
	srv      *httptest.Server
	mu       sync.Mutex
	headers  []http.Header
	revoked  atomic.Bool
	indexVer atomic.Int32
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{}
	u.indexVer.Store(1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/v1/echo", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.headers = append(u.headers, r.Header.Clone())
		u.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "porto_session", Value: "upstream-session"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"host":"`+r.Host+`"}`)
	})
	mux.HandleFunc("/api/admin/v1/forbidden", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/api/device/v1/self", func(w http.ResponseWriter, _ *http.Request) {
		if u.revoked.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("/api/admin/v1/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+r.Host+"/admin/login", http.StatusFound)
	})
	mux.HandleFunc("/api/admin/v1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := range 3 {
			_, _ = io.WriteString(w, "data: "+strconv.Itoa(i)+"\n\n")
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/assets/") {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = io.WriteString(w, "console.log('"+r.URL.Path+"')")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html>index v"+strconv.Itoa(int(u.indexVer.Load()))+"</html>")
	})
	u.srv = httptest.NewServer(mux)
	t.Cleanup(u.srv.Close)
	return u
}

type proxyEnv struct {
	p      *Proxy
	base   string
	client *http.Client
	up     *upstream
	logs   *strings.Builder
	revoke chan struct{}
}

type stubLocal struct{}

func (stubLocal) ServeHTTP(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }

func newProxyEnv(t *testing.T) *proxyEnv {
	t.Helper()
	up := newUpstream(t)
	origin, _ := url.Parse(up.srv.URL)
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	env := &proxyEnv{up: up, logs: &strings.Builder{}, revoke: make(chan struct{}, 1)}
	env.p = New(Options{
		Origin: origin, Token: func() string { return testToken }, Port: port, CacheDir: t.TempDir(), Dev: true,
		Local: stubLocal{}, OnRevoked: func() { env.revoke <- struct{}{} },
		Log: testLogger(env.logs),
	})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: env.p, ReadHeaderTimeout: 5 * time.Second}}
	srv.Start()
	t.Cleanup(srv.Close)
	env.base = "http://127.0.0.1:" + strconv.Itoa(port)
	jar, _ := cookiejar.New(nil)
	env.client = &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return env
}

func (e *proxyEnv) enter(t *testing.T) {
	t.Helper()
	u, err := e.p.EnterURL("")
	if err != nil {
		t.Fatal(err)
	}
	_, k, _ := strings.Cut(u, "#k=")
	res := e.do(t, http.MethodPost, EnterPath, "k="+url.QueryEscape(k), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "same-origin",
	})
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("enter: %d", res.StatusCode)
	}
}

type result struct {
	StatusCode int
	Header     http.Header
	Body       string
}

func (e *proxyEnv) do(t *testing.T, method, path, body string, h map[string]string) result {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, e.base+path, rd)
	for k, v := range h {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return result{StatusCode: res.StatusCode, Header: res.Header, Body: string(b)}
}

func TestRemoteProxy_RequiresLaunchCookieAndHost(t *testing.T) {
	e := newProxyEnv(t)
	if r := e.do(t, http.MethodGet, "/api/admin/v1/echo", "", nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie: %d", r.StatusCode)
	}
	// The enter page carries no secret and a strict CSP.
	page := e.do(t, http.MethodGet, EnterPath, "", nil)
	if page.StatusCode != http.StatusOK || !strings.Contains(page.Header.Get("Content-Security-Policy"), "script-src 'sha256-") {
		t.Fatalf("enter page %d %v", page.StatusCode, page.Header)
	}
	// A wrong secret, or a cross-site post, gets nothing.
	u, _ := e.p.EnterURL("")
	k := u[strings.Index(u, "#k=")+3:]
	if r := e.do(t, http.MethodPost, EnterPath, "k=wrong", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "same-origin"}); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong secret: %d", r.StatusCode)
	}
	if r := e.do(t, http.MethodPost, EnterPath, "k="+k, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Sec-Fetch-Site": "cross-site"}); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site enter: %d", r.StatusCode)
	}
	if r := e.do(t, http.MethodPost, EnterPath, "k="+k, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": e.base}); r.StatusCode != http.StatusNoContent {
		t.Fatalf("enter: %d", r.StatusCode)
	}
	// Single use.
	jar, _ := cookiejar.New(nil)
	other := &proxyEnv{p: e.p, base: e.base, client: &http.Client{Jar: jar, Timeout: 5 * time.Second}}
	if r := other.do(t, http.MethodPost, EnterPath, "k="+k, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Origin": e.base}); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second exchange: %d", r.StatusCode)
	}
	if r := e.do(t, http.MethodGet, "/api/admin/v1/echo", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("with cookie: %d", r.StatusCode)
	}
	// Host check (DNS rebinding) and same-origin unsafe requests.
	if r := e.do(t, http.MethodGet, "/api/admin/v1/echo", "", map[string]string{"Host": "evil.example:80"}); r.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("foreign host: %d", r.StatusCode)
	}
	if r := e.do(t, http.MethodPost, "/api/admin/v1/echo", "{}", map[string]string{"Origin": "http://evil.example"}); r.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin post: %d", r.StatusCode)
	}
	if r := e.do(t, http.MethodPost, "/api/admin/v1/echo", "{}", map[string]string{"Sec-Fetch-Site": "same-origin"}); r.StatusCode != http.StatusOK {
		t.Fatalf("same-origin post: %d", r.StatusCode)
	}
	// Local APIs are answered by the app, not the instance.
	if r := e.do(t, http.MethodGet, "/api/bridge/v1/status", "", nil); r.StatusCode != http.StatusTeapot {
		t.Fatalf("local api: %d", r.StatusCode)
	}
}

func TestRemoteProxy_InjectsTokenStripsCookies(t *testing.T) {
	e := newProxyEnv(t)
	e.enter(t)
	r := e.do(t, http.MethodGet, "/api/admin/v1/echo", "", map[string]string{
		"Authorization": "Bearer po_live_page_supplied", "X-Forwarded-For": "1.2.3.4", AppPinHeader: "123456",
	})
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status %d", r.StatusCode)
	}
	if len(r.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("upstream cookie reached the page: %v", r.Header.Values("Set-Cookie"))
	}
	origin, _ := url.Parse(e.up.srv.URL)
	if !strings.Contains(r.Body, origin.Host) {
		t.Fatalf("Host not rewritten: %s", r.Body)
	}
	e.up.mu.Lock()
	h := e.up.headers[len(e.up.headers)-1]
	e.up.mu.Unlock()
	if h.Get("Authorization") != "Bearer "+testToken {
		t.Fatalf("authorization %q", h.Get("Authorization"))
	}
	for _, k := range []string{"Cookie", "X-Forwarded-For", AppPinHeader} {
		if v := h.Get(k); v != "" {
			t.Errorf("%s forwarded: %q", k, v)
		}
	}
	if strings.Contains(h.Get("Cookie"), CookieName) {
		t.Fatal("porto_remote forwarded")
	}
}

func TestRemoteProxy_TokenNeverReachesPage(t *testing.T) {
	e := newProxyEnv(t)
	var all strings.Builder
	collect := func(r result) {
		all.WriteString(r.Body)
		for k, v := range r.Header {
			all.WriteString(k + ": " + strings.Join(v, ",") + "\n")
		}
	}
	collect(e.do(t, http.MethodGet, EnterPath, "", nil))
	collect(e.do(t, http.MethodGet, "/api/admin/v1/echo", "", nil)) // 401 without cookie
	e.enter(t)
	collect(e.do(t, http.MethodGet, "/api/admin/v1/echo", "", nil))
	redir := e.do(t, http.MethodGet, "/api/admin/v1/redirect", "", nil)
	collect(redir)
	if loc := redir.Header.Get("Location"); loc != "/admin/login" {
		t.Fatalf("redirect not made relative: %q", loc)
	}
	collect(e.do(t, http.MethodGet, "/api/admin/v1/missing", "", nil))
	e.p.SetOffline(true)
	collect(e.do(t, http.MethodGet, "/api/admin/v1/echo", "", nil))
	e.p.SetOffline(false)
	e.up.srv.Close() // unreachable: the error page
	collect(e.do(t, http.MethodGet, "/api/admin/v1/echo", "", nil))
	if strings.Contains(all.String(), testToken) || strings.Contains(all.String(), "SECRETSECRET") {
		t.Fatal("the device token reached the page")
	}
	if strings.Contains(e.logs.String(), "SECRETSECRET") {
		t.Fatal("the device token reached the log")
	}
}

func TestRemoteProxy_SSEStreams(t *testing.T) {
	e := newProxyEnv(t)
	e.enter(t)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, e.base+"/api/admin/v1/events", nil)
	res, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	sc := bufio.NewScanner(res.Body)
	start := time.Now()
	var first time.Duration
	n := 0
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data: ") {
			if n == 0 {
				first = time.Since(start)
			}
			n++
		}
	}
	if n != 3 || first > 90*time.Millisecond {
		t.Fatalf("events %d, first after %v (not flushed per event)", n, first)
	}
}

func TestRemoteProxy_ShellCacheOffline(t *testing.T) {
	e := newProxyEnv(t)
	e.enter(t)
	if r := e.do(t, http.MethodGet, "/admin/", "", nil); r.Body != "<html>index v1</html>" {
		t.Fatalf("index %q", r.Body)
	}
	e.do(t, http.MethodGet, "/admin/assets/app-1.js", "", nil)
	e.do(t, http.MethodGet, "/admin/site.webmanifest", "", nil)
	if !e.p.Reachable() {
		t.Fatal("not reachable")
	}
	e.up.srv.Close()
	r := e.do(t, http.MethodGet, "/admin/m/abc/desk", "", nil)
	if r.StatusCode != http.StatusOK || r.Body != "<html>index v1</html>" {
		t.Fatalf("offline navigation %d %q", r.StatusCode, r.Body)
	}
	if e.p.Reachable() {
		t.Fatal("reachable after a failure")
	}
	if r := e.do(t, http.MethodGet, "/admin/assets/app-1.js", "", nil); r.StatusCode != http.StatusOK || !strings.Contains(r.Header.Get("Content-Type"), "javascript") {
		t.Fatalf("offline asset %d %s", r.StatusCode, r.Header.Get("Content-Type"))
	}
	if r := e.do(t, http.MethodGet, "/admin/site.webmanifest", "", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("offline manifest %d", r.StatusCode)
	}
	r = e.do(t, http.MethodPost, "/api/admin/v1/marinas/x/presences", "{}", map[string]string{"Sec-Fetch-Site": "same-origin"})
	if r.StatusCode != http.StatusServiceUnavailable || !strings.Contains(r.Body, `"remote_unreachable"`) {
		t.Fatalf("offline api %d %s", r.StatusCode, r.Body)
	}
	if r := e.do(t, http.MethodGet, "/admin/assets/never-seen.js", "", nil); r.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("uncached asset %d", r.StatusCode)
	}
}

func TestShellCache_PrunesOldGenerations(t *testing.T) {
	c := &ShellCache{Dir: t.TempDir()}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(c.Store(shellIndex, "", []byte("v1")))
	must(c.Store(shellAsset, "a1.js", []byte("a1")))
	must(c.Store(shellIndex, "", []byte("v2")))
	must(c.Store(shellAsset, "a2.js", []byte("a2")))
	if _, _, ok := c.Load(shellAsset, "a1.js"); !ok {
		t.Fatal("asset of the previous index pruned too early")
	}
	must(c.Store(shellIndex, "", []byte("v3")))
	if _, _, ok := c.Load(shellAsset, "a1.js"); ok {
		t.Fatal("asset older than two index versions kept")
	}
	if b, _, ok := c.Load(shellAsset, "a2.js"); !ok || string(b) != "a2" {
		t.Fatal("asset of the previous index lost")
	}
	must(c.Store(shellIndex, "", []byte("v3"))) // same index: no new generation
	if _, _, ok := c.Load(shellAsset, "a2.js"); !ok {
		t.Fatal("unchanged index pruned")
	}
	for p, want := range map[string]string{
		"/admin/": shellIndex, "/admin/m/1/desk": shellIndex, "/admin/assets/x-1.js": shellAsset,
		"/admin/icon.svg": shellStatic, "/admin/assets/../x": "", "/api/admin/v1/x": "", "/admin/sw.js": "",
	} {
		if k, _ := classify(p); k != want {
			t.Errorf("classify(%q) = %q, want %q", p, k, want)
		}
	}
}

func TestRemoteProxy_RevokedDeviceResets(t *testing.T) {
	e := newProxyEnv(t)
	e.enter(t)
	// A 401 of a panel request while the device is fine does not reset.
	e.do(t, http.MethodGet, "/api/admin/v1/forbidden", "", nil)
	select {
	case <-e.revoke:
		t.Fatal("reset on a 401 of a valid device")
	case <-time.After(200 * time.Millisecond):
	}
	e.up.revoked.Store(true)
	e.do(t, http.MethodGet, "/api/admin/v1/forbidden", "", nil)
	select {
	case <-e.revoke:
	case <-time.After(3 * time.Second):
		t.Fatal("revoked device not detected")
	}
	if !e.p.Revoked() {
		t.Fatal("proxy not marked revoked")
	}
	if r := e.do(t, http.MethodGet, "/api/admin/v1/echo", "", nil); r.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("after revocation %d", r.StatusCode)
	}
}

// TestRemoteProxy_EnterPageBilingual: the refusal of the enter page exists in Italian and English (the page is
// served before the panel's i18n catalogue), and the CSP hash covers the script actually served.
func TestRemoteProxy_EnterPageBilingual(t *testing.T) {
	e := newProxyEnv(t)
	page := e.do(t, http.MethodGet, EnterPath, "", nil)
	body := []byte(page.Body)
	for _, want := range []string{"accesso non valido", "access refused", "navigator.language"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("enter page lacks %q: %s", want, body)
		}
	}
	start := strings.Index(string(body), "<script>") + len("<script>")
	end := strings.Index(string(body), "</script>")
	sum := sha256.Sum256(body[start:end])
	if !strings.Contains(page.Header.Get("Content-Security-Policy"), base64.StdEncoding.EncodeToString(sum[:])) {
		t.Fatal("CSP hash does not match the served script")
	}
}
