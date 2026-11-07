package lan

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/reqctx"
)

// DefaultPort is the local-network port: fixed so phone bookmarks (and the M6b PWA) keep their origin (D2).
const DefaultPort = 8443

// Port bounds accepted by Enable (no privileged ports: the desktop app never runs as root).
const (
	MinPort = 1024
	MaxPort = 65535
)

const (
	shutdownTimeout = 5 * time.Second
	addrsCacheTTL   = 30 * time.Second
)

// Announcer publishes the local-network address on the network (mDNS, M6b). M6a uses NopAnnouncer (D12).
type Announcer interface {
	Announce(ctx context.Context, host string, port int) error
	Stop()
}

// NopAnnouncer announces nothing.
type NopAnnouncer struct{}

// Announce implements Announcer.
func (NopAnnouncer) Announce(context.Context, string, int) error { return nil }

// Stop implements Announcer.
func (NopAnnouncer) Stop() {}

// Options configure a Server.
type Options struct {
	// Dir holds the certificates (<data>/lan).
	Dir   string
	Keys  *crypto.KeyRing
	Clock clock.Clock
	Log   *slog.Logger
	// Port and Address are the stored settings (lan_port, lan_address); 0 / "" = DefaultPort / automatic.
	Port    int
	Address string
	// Loopback (PORTO_DESKTOP_LAN_LOOPBACK, dev and tests only) binds 127.0.0.1 instead of every interface and
	// adds 127.0.0.1 to the certificate.
	Loopback bool
	// NewServer builds the HTTP server for the allowed Host values (app.NewHTTPServer with reqctx.ListenerLAN).
	// When nil, a plain server over Handler is used (tests).
	NewServer func(hosts []string) *http.Server
	Handler   http.Handler
	// FallbackOrigin is where pairing QRs point while the local network is off (the loopback base URL); phones
	// cannot reach it, so PairTarget reports it unreachable.
	FallbackOrigin string
	// OnChange is told the new status after every enable, disable and rotation (the runtime persists lan_enabled,
	// lan_port and lan_address). It runs without the server lock held.
	OnChange func(desktopport.LANStatus)
	// Interfaces, Hostname and Listen replace the system ones (tests).
	Interfaces func() ([]Interface, error)
	Hostname   func() (string, error)
	Listen     func(ctx context.Context, network, addr string) (net.Listener, error)
	Announcer  Announcer
}

// Server is the local-network listener of the desktop edition; it implements desktopport.LANController. The
// listener exists only between Enable and Disable (INV-79) and accepts private peers only.
type Server struct {
	opts  Options
	certs *CertStore

	mu        sync.Mutex
	srv       *http.Server
	done      chan struct{}
	port      int
	address   string // preferred address (settings)
	inUse     netip.Addr
	current   atomic.Pointer[Certs]
	lastError string
	addrs     []netip.Addr
	addrsAt   time.Time
	rejected  atomic.Int64
}

var _ desktopport.LANController = (*Server)(nil)

// New returns a stopped server. Certificates already on disk are loaded for the status (best effort); nothing
// listens until Enable.
func New(o Options) *Server {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Interfaces == nil {
		o.Interfaces = SystemInterfaces
	}
	if o.Hostname == nil {
		o.Hostname = os.Hostname
	}
	if o.Listen == nil {
		var lc net.ListenConfig
		o.Listen = lc.Listen
	}
	if o.Announcer == nil {
		o.Announcer = NopAnnouncer{}
	}
	port := o.Port
	if port == 0 {
		port = DefaultPort
	}
	s := &Server{opts: o, certs: &CertStore{Dir: o.Dir, Keys: o.Keys, Clock: o.Clock}, port: port, address: o.Address}
	if c, err := s.certs.Load(); err == nil {
		s.current.Store(c)
	} else if !errors.Is(err, ErrNoCertificate) {
		o.Log.Warn("local network certificate unreadable", "error", err)
	}
	return s
}

// Status implements desktopport.LANController.
func (s *Server) Status() desktopport.LANStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Server) statusLocked() desktopport.LANStatus {
	st := desktopport.LANStatus{
		Enabled: s.srv != nil, Listening: s.srv != nil, Port: s.port, Error: s.lastError,
	}
	for _, a := range s.addrsLocked() {
		st.Addresses = append(st.Addresses, a.String())
	}
	if c := s.current.Load(); c != nil {
		st.Fingerprint, st.CAFingerprint, st.CertExpiresAt = c.Fingerprint(), c.CAFingerprint(), c.Leaf.NotAfter
	}
	if s.srv != nil {
		st.Address = s.inUse.String()
		st.URL = originFor(s.inUse, s.port)
	}
	return st
}

// addrsLocked returns the private addresses (or 127.0.0.1 in loopback mode), cached for a short while because
// Status is read by every pairing and meta request.
func (s *Server) addrsLocked() []netip.Addr {
	now := s.opts.Clock.Now()
	if s.addrs != nil && now.Sub(s.addrsAt) < addrsCacheTTL && !now.Before(s.addrsAt) {
		return s.addrs
	}
	var out []netip.Addr
	if s.opts.Loopback {
		out = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	} else if ifs, err := s.opts.Interfaces(); err == nil {
		out = PrivateAddrs(ifs)
	} else {
		s.opts.Log.Warn("list network interfaces", "error", err)
	}
	if out == nil {
		out = []netip.Addr{}
	}
	s.addrs, s.addrsAt = out, now
	return out
}

// PairTarget is where pairing QRs point (device.Options.PairTarget): the local-network URL and the leaf
// fingerprint while listening, else the fallback origin, unreachable.
func (s *Server) PairTarget() desktopport.PairTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		return desktopport.PairTarget{Origin: s.opts.FallbackOrigin}
	}
	t := desktopport.PairTarget{Origin: originFor(s.inUse, s.port), Reachable: true}
	if c := s.current.Load(); c != nil {
		t.Fingerprint = c.FingerprintB64()
	}
	return t
}

// Rejected returns how many connections from non-private peers were refused since the last enable.
func (s *Server) Rejected() int64 { return s.rejected.Load() }

// Enable implements desktopport.LANController: it starts the TLS listener (or keeps it when nothing changed).
// Failures leave the local network off and are reported in the status.
func (s *Server) Enable(ctx context.Context, o desktopport.LANOptions) (desktopport.LANStatus, error) {
	s.mu.Lock()
	st, err := s.enableLocked(ctx, o, false, false)
	s.mu.Unlock()
	s.notify(st)
	return st, err
}

func (s *Server) enableLocked(ctx context.Context, o desktopport.LANOptions, reissue, all bool) (desktopport.LANStatus, error) {
	port := o.Port
	if port == 0 {
		port = s.port
	}
	if port < MinPort || port > MaxPort {
		return s.statusLocked(), &desktopport.SettingsError{Fields: []string{"port"}}
	}
	s.addrs = nil // re-read the interfaces: DHCP may have moved us
	addrs := s.addrsLocked()
	if o.Address != "" {
		a, err := netip.ParseAddr(o.Address)
		if err != nil || !slices.Contains(addrs, a.Unmap()) {
			return s.statusLocked(), &desktopport.SettingsError{Fields: []string{"address"}}
		}
	}
	preferred := s.address
	if o.Address != "" {
		preferred = o.Address
	}
	addr, ok := chooseAddress(addrs, preferred)
	if !ok {
		s.stopLocked(ctx)
		s.lastError = "lan_no_address"
		return s.statusLocked(), desktopport.ErrLANNoAddress
	}
	if s.srv != nil && !reissue && !all && port == s.port && addr == s.inUse {
		if c := s.current.Load(); c != nil && c.Covers(s.namesLocked(addrs)) &&
			c.Leaf.NotAfter.Sub(s.opts.Clock.Now()) >= LeafRenewBefore {
			return s.statusLocked(), nil
		}
	}
	s.stopLocked(ctx)
	certs, err := s.certs.Ensure(s.namesLocked(addrs), reissue, all)
	if err != nil {
		s.lastError = "certificate"
		return s.statusLocked(), err
	}
	s.current.Store(certs)
	bind := ":" + strconv.Itoa(port)
	if s.opts.Loopback {
		bind = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	}
	raw, err := s.opts.Listen(ctx, "tcp", bind)
	if err != nil {
		s.lastError = "lan_port_unavailable"
		s.opts.Log.Warn("local network port unavailable", "port", port, "error", err)
		return s.statusLocked(), fmt.Errorf("%w: %w", desktopport.ErrLANPortUnavailable, err)
	}
	s.rejected.Store(0)
	filtered := &PrivateListener{Listener: raw, OnReject: func(netip.Addr) { s.rejected.Add(1) }}
	ln := tls.NewListener(filtered, &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			if c := s.current.Load(); c != nil {
				return &c.TLS, nil
			}
			return nil, errors.New("lan: no certificate")
		},
	})
	srv := s.newHTTPServer(AllowedHosts(certs.Leaf, port))
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.opts.Log.Error("local network listener stopped", "error", err)
		}
	}()
	s.srv, s.done, s.port, s.inUse, s.lastError = srv, done, port, addr, ""
	if o.Address != "" {
		s.address = o.Address
	}
	if err := s.opts.Announcer.Announce(ctx, addr.String(), port); err != nil {
		s.opts.Log.Debug("local network announce", "error", err)
	}
	s.opts.Log.Info("local network enabled", "port", port, "address", addr.String())
	return s.statusLocked(), nil
}

// newHTTPServer builds the server and tags every connection as a local-network one, whatever the factory did.
func (s *Server) newHTTPServer(hosts []string) *http.Server {
	var srv *http.Server
	if s.opts.NewServer != nil {
		srv = s.opts.NewServer(hosts)
	} else {
		h := s.opts.Handler
		if h == nil {
			h = http.NotFoundHandler()
		}
		srv = &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	}
	prev := srv.ConnContext
	srv.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		if prev != nil {
			ctx = prev(ctx, c)
		}
		return reqctx.WithListener(ctx, reqctx.ListenerLAN)
	}
	return srv
}

// Disable implements desktopport.LANController. Disabling twice is a no-op.
func (s *Server) Disable(ctx context.Context) error {
	s.mu.Lock()
	was := s.srv != nil
	s.stopLocked(ctx)
	s.lastError = ""
	st := s.statusLocked()
	s.mu.Unlock()
	if was {
		s.opts.Log.Info("local network disabled")
	}
	s.notify(st)
	return nil
}

// stopLocked shuts the listener down; connections that do not finish in time (SSE streams) are closed.
func (s *Server) stopLocked(ctx context.Context) {
	if s.srv == nil {
		return
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := s.srv.Shutdown(sctx); err != nil {
		_ = s.srv.Close()
	}
	<-s.done
	s.opts.Announcer.Stop()
	s.srv, s.done, s.inUse = nil, nil, netip.Addr{}
}

// RotateCertificate implements desktopport.LANController: it re-issues the leaf (all=false) or the CA and the
// leaf (all=true) and restarts the listener when it was running, so the Host allow-list follows the new names.
func (s *Server) RotateCertificate(ctx context.Context, all bool) (desktopport.LANStatus, error) {
	s.mu.Lock()
	var (
		st  desktopport.LANStatus
		err error
	)
	if s.srv != nil {
		st, err = s.enableLocked(ctx, desktopport.LANOptions{Port: s.port}, true, all)
	} else {
		var c *Certs
		if c, err = s.certs.Ensure(s.namesLocked(s.addrsLocked()), true, all); err == nil {
			s.current.Store(c)
		}
		st = s.statusLocked()
	}
	s.mu.Unlock()
	if err == nil {
		s.opts.Log.Info("local network certificate rotated", "all", all)
	}
	s.notify(st)
	return st, err
}

// Refresh re-checks a running listener: when the addresses changed (DHCP, another Wi-Fi) or the leaf is close to
// expiry, the leaf is re-issued and the listener restarted. The launcher calls it periodically.
func (s *Server) Refresh(ctx context.Context) (desktopport.LANStatus, error) {
	s.mu.Lock()
	if s.srv == nil {
		st := s.statusLocked()
		s.mu.Unlock()
		return st, nil
	}
	st, err := s.enableLocked(ctx, desktopport.LANOptions{Port: s.port}, false, false)
	s.mu.Unlock()
	s.notify(st)
	return st, err
}

func (s *Server) notify(st desktopport.LANStatus) {
	if s.opts.OnChange != nil {
		s.opts.OnChange(st)
	}
}

// namesLocked are the SANs the leaf must carry: every private address, <hostname>.local, localhost and, in
// loopback mode, 127.0.0.1.
func (s *Server) namesLocked(addrs []netip.Addr) Names {
	n := Names{IPs: slices.Clone(addrs), DNS: []string{"localhost"}}
	if h, err := s.opts.Hostname(); err == nil {
		if m := MDNSName(h); m != "" {
			n.DNS = append(n.DNS, m)
		}
	}
	return n
}

// chooseAddress returns the preferred address when still present, else the best one.
func chooseAddress(addrs []netip.Addr, preferred string) (netip.Addr, bool) {
	if preferred != "" {
		if p, err := netip.ParseAddr(preferred); err == nil && slices.Contains(addrs, p.Unmap()) {
			return p.Unmap(), true
		}
	}
	if len(addrs) == 0 {
		return netip.Addr{}, false
	}
	return addrs[0], true
}

func originFor(a netip.Addr, port int) string {
	return "https://" + net.JoinHostPort(a.String(), strconv.Itoa(port))
}

// AllowedHosts are the Host header values the local-network listener accepts: every SAN of the leaf with the
// port (and without it on 443).
func AllowedHosts(leaf *x509.Certificate, port int) []string {
	p := strconv.Itoa(port)
	var out []string
	add := func(h string) {
		out = append(out, net.JoinHostPort(h, p))
		if port == 443 {
			if a, err := netip.ParseAddr(h); err == nil && a.Is6() {
				h = "[" + h + "]"
			}
			out = append(out, h)
		}
	}
	for _, ip := range leaf.IPAddresses {
		if a, ok := netip.AddrFromSlice(ip); ok {
			add(a.Unmap().String())
		}
	}
	for _, d := range leaf.DNSNames {
		add(d)
	}
	return out
}
