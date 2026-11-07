package lan

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/http/middleware"
	"github.com/coste360/porto/internal/reqctx"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// listenerEcho answers with the listener kind of the connection.
var listenerEcho = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = io.WriteString(w, string(reqctx.ListenerFrom(r.Context())))
})

func newTestServer(t *testing.T, o Options) *Server {
	t.Helper()
	if o.Dir == "" {
		o.Dir = t.TempDir()
	}
	if o.Keys == nil {
		o.Keys = testKeys(t)
	}
	if o.Clock == nil {
		o.Clock = clock.NewFake(t0)
	}
	if o.Port == 0 {
		o.Port = freePort(t)
	}
	if o.Handler == nil && o.NewServer == nil {
		o.Handler = listenerEcho
	}
	if o.Hostname == nil {
		o.Hostname = func() (string, error) { return "ufficio", nil }
	}
	o.Loopback = true
	s := New(o)
	t.Cleanup(func() { _ = s.Disable(context.Background()) })
	return s
}

func dialable(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func pinnedClient(fp string) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: PinnedClientConfig(fp)}}
}

func TestLANServer_OnlyWhenEnabled(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var changes []desktopport.LANStatus
	s := newTestServer(t, Options{OnChange: func(st desktopport.LANStatus) {
		mu.Lock()
		defer mu.Unlock()
		changes = append(changes, st)
	}})
	port := s.Status().Port
	if dialable(port) || s.Status().Listening || s.PairTarget().Reachable {
		t.Fatal("listening before enable")
	}
	st, err := s.Enable(ctx, desktopport.LANOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || !st.Listening || st.URL != "https://127.0.0.1:"+strconv.Itoa(port) || st.Address != "127.0.0.1" ||
		len(st.Fingerprint) != 64 || len(st.CAFingerprint) != 64 || !st.CertExpiresAt.Equal(t0.Add(LeafValidity)) {
		t.Fatalf("status %+v", st)
	}
	if !dialable(port) {
		t.Fatal("not listening after enable")
	}
	tgt := s.PairTarget()
	if !tgt.Reachable || tgt.Origin != st.URL || len(tgt.Fingerprint) != 43 {
		t.Fatalf("pair target %+v", tgt)
	}
	// Enabling again with the same settings keeps the listener and the certificate.
	again, err := s.Enable(ctx, desktopport.LANOptions{})
	if err != nil || again.Fingerprint != st.Fingerprint {
		t.Fatalf("re-enable: %v", err)
	}

	// The port is busy for a second server: error, setting stays off.
	other := newTestServer(t, Options{Port: port})
	ost, err := other.Enable(ctx, desktopport.LANOptions{})
	if !errors.Is(err, desktopport.ErrLANPortUnavailable) || ost.Listening || ost.Error != "lan_port_unavailable" {
		t.Fatalf("busy port: %+v %v", ost, err)
	}

	if err := s.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if dialable(port) || s.Status().Listening || s.Status().URL != "" || s.PairTarget().Reachable {
		t.Fatal("still listening after disable")
	}
	if err := s.Disable(ctx); err != nil {
		t.Fatal("second disable", err)
	}
	mu.Lock()
	n := len(changes)
	mu.Unlock()
	if n < 3 {
		t.Fatalf("OnChange called %d times", n)
	}

	// Invalid port and address.
	if _, err := s.Enable(ctx, desktopport.LANOptions{Port: 80}); !errors.Is(err, desktopport.ErrInvalidSettings) {
		t.Fatalf("port 80: %v", err)
	}
	if _, err := s.Enable(ctx, desktopport.LANOptions{Address: "192.168.99.99"}); !errors.Is(err, desktopport.ErrInvalidSettings) {
		t.Fatalf("absent address: %v", err)
	}
	if dialable(port) {
		t.Fatal("invalid settings started the listener")
	}
}

func TestLANServer_NoAddress(t *testing.T) {
	s := New(Options{
		Dir: t.TempDir(), Keys: testKeys(t), Clock: clock.NewFake(t0), Port: freePort(t), Handler: listenerEcho,
		Interfaces: func() ([]Interface, error) {
			return []Interface{{Name: "en0", Up: true, Addrs: []netip.Addr{netip.MustParseAddr("203.0.113.4")}}}, nil
		},
	})
	st, err := s.Enable(context.Background(), desktopport.LANOptions{})
	if !errors.Is(err, desktopport.ErrLANNoAddress) || st.Listening || st.Error != "lan_no_address" {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestLANServer_PinnedClient(t *testing.T) {
	ctx := context.Background()
	s := newTestServer(t, Options{})
	st, err := s.Enable(ctx, desktopport.LANOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, fp := range []string{st.Fingerprint, strings.ToUpper(st.Fingerprint), s.PairTarget().Fingerprint} {
		resp, err := pinnedClient(fp).Get(st.URL + "/")
		if err != nil {
			t.Fatalf("pinned %s: %v", fp, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != string(reqctx.ListenerLAN) {
			t.Fatalf("connection tagged %q", b)
		}
	}
	wrong := strings.Repeat("ab", 32)
	if _, err := pinnedClient(wrong).Get(st.URL + "/"); err == nil {
		t.Fatal("wrong pin accepted")
	}
	if _, err := pinnedClient("").Get(st.URL + "/"); err == nil {
		t.Fatal("empty pin accepted")
	}
	// Without the pin the self-issued certificate is not trusted.
	if _, err := (&http.Client{Timeout: 5 * time.Second}).Get(st.URL + "/"); err == nil {
		t.Fatal("system roots trusted the local CA")
	}

	// Rotation: new fingerprint, the old pin is refused, the listener keeps running.
	rot, err := s.RotateCertificate(ctx, false)
	if err != nil || rot.Fingerprint == st.Fingerprint || rot.CAFingerprint != st.CAFingerprint || !rot.Listening {
		t.Fatalf("rotate: %+v %v", rot, err)
	}
	if _, err := pinnedClient(st.Fingerprint).Get(rot.URL + "/"); err == nil {
		t.Fatal("old pin accepted after rotation")
	}
	resp, err := pinnedClient(rot.Fingerprint).Get(rot.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	all, err := s.RotateCertificate(ctx, true)
	if err != nil || all.CAFingerprint == st.CAFingerprint {
		t.Fatalf("rotate all: %v", err)
	}
}

// The real app router answers /api/desktop/v1 only on the loopback listener (middleware.LoopbackOnly); here the
// same middleware and Host check run over the LAN server, from a loopback TCP peer.
func TestLANServer_DesktopAPINotReachable(t *testing.T) {
	ctx := context.Background()
	inner := middleware.LoopbackOnly("/api/desktop/v1/")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	s := newTestServer(t, Options{NewServer: func(hosts []string) *http.Server {
		return &http.Server{Handler: middleware.HostCheck(hosts)(inner), ReadHeaderTimeout: 5 * time.Second}
	}})
	st, err := s.Enable(ctx, desktopport.LANOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := pinnedClient(st.Fingerprint)
	get := func(url, host string) int {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if host != "" {
			req.Host = host
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(st.URL+"/api/desktop/v1/status", ""); got != http.StatusNotFound {
		t.Fatalf("desktop API over the LAN: %d", got)
	}
	if got := get(st.URL+"/api/admin/v1/meta", ""); got != http.StatusNoContent {
		t.Fatalf("admin API over the LAN: %d", got)
	}
	port := strconv.Itoa(st.Port)
	if got := get(st.URL+"/api/admin/v1/meta", "localhost:"+port); got != http.StatusNoContent {
		t.Fatalf("SAN localhost: %d", got)
	}
	if got := get(st.URL+"/api/admin/v1/meta", "ufficio.local:"+port); got != http.StatusNoContent {
		t.Fatalf("SAN mdns: %d", got)
	}
	if got := get(st.URL+"/api/admin/v1/meta", "evil.example:"+port); got != http.StatusMisdirectedRequest {
		t.Fatalf("rebinding host: %d", got)
	}
}

func TestAllowedHosts(t *testing.T) {
	st := &CertStore{Dir: t.TempDir(), Keys: testKeys(t), Clock: clock.NewFake(t0)}
	c, err := st.Ensure(Names{IPs: addrs("192.168.1.20", "fd12::20"), DNS: []string{"localhost"}}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(AllowedHosts(c.Leaf, 8443), " ")
	if got != "192.168.1.20:8443 [fd12::20]:8443 localhost:8443" {
		t.Fatalf("hosts %s", got)
	}
	got = strings.Join(AllowedHosts(c.Leaf, 443), " ")
	if got != "192.168.1.20:443 192.168.1.20 [fd12::20]:443 [fd12::20] localhost:443 localhost" {
		t.Fatalf("hosts on 443: %s", got)
	}
}
