package event

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Egress errors.
var (
	ErrEgressBlocked = errors.New("event: destination address not allowed")
	ErrEgressURL     = errors.New("event: destination URL not allowed")
)

// EgressPolicy decides which destinations outgoing requests (webhooks, later providers) may reach.
type EgressPolicy struct {
	AllowPrivate bool // dev/tests only: allow loopback, private and link-local addresses
	AllowHTTP    bool // dev/tests only: allow plain http
}

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",       // "this network"
		"10.0.0.0/8",      // private
		"100.64.0.0/10",   // carrier-grade NAT
		"127.0.0.0/8",     // loopback
		"169.254.0.0/16",  // link-local, cloud metadata (169.254.169.254)
		"172.16.0.0/12",   // private
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // documentation
		"192.168.0.0/16",  // private
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"224.0.0.0/4",     // multicast
		"240.0.0.0/4",     // reserved, broadcast
		"::/96",           // IPv4-compatible (deprecated), includes :: and ::1
		"::/128",          // unspecified
		"::1/128",         // loopback
		"64:ff9b::/96",    // NAT64 (embeds IPv4)
		"64:ff9b:1::/48",  // local NAT64
		"100::/64",        // discard
		"2001:db8::/32",   // documentation
		"2002::/16",       // 6to4 (embeds IPv4)
		"fc00::/7",        // unique local
		"fe80::/10",       // link-local
		"ff00::/8",        // multicast
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// CheckAddr rejects addresses that are not public unicast destinations: loopback, private, link-local (and the
// cloud metadata address), CGNAT, unique local, multicast, unspecified, reserved, also in IPv4-mapped form.
func (p EgressPolicy) CheckAddr(a netip.Addr) error {
	if !a.IsValid() {
		return ErrEgressBlocked
	}
	if p.AllowPrivate {
		return nil
	}
	a = a.Unmap().WithZone("")
	for _, pfx := range blockedPrefixes {
		if pfx.Contains(a) {
			return fmt.Errorf("%w: %s", ErrEgressBlocked, a)
		}
	}
	if !a.IsGlobalUnicast() {
		return fmt.Errorf("%w: %s", ErrEgressBlocked, a)
	}
	return nil
}

// CheckURL validates a destination URL: https only (unless AllowHTTP), a host, no userinfo, no fragment. A
// literal IP host is checked with CheckAddr; names are checked when dialling.
func (p EgressPolicy) CheckURL(u *url.URL) error {
	if u == nil || !u.IsAbs() {
		return fmt.Errorf("%w: not absolute", ErrEgressURL)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !p.AllowHTTP {
			return fmt.Errorf("%w: https required", ErrEgressURL)
		}
	default:
		return fmt.Errorf("%w: scheme %q", ErrEgressURL, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: credentials in URL", ErrEgressURL)
	}
	if u.Fragment != "" {
		return fmt.Errorf("%w: fragment", ErrEgressURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: no host", ErrEgressURL)
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("%w: port", ErrEgressURL)
		}
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return p.CheckAddr(a)
	}
	if !p.AllowPrivate && (host == "localhost" || strings.HasSuffix(host, ".localhost")) {
		return fmt.Errorf("%w: localhost", ErrEgressBlocked)
	}
	return nil
}

// lookupFunc resolves a host name to addresses.
type lookupFunc func(ctx context.Context, host string) ([]netip.Addr, error)

func defaultLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// NewEgressClient returns an HTTP client that only reaches destinations allowed by p. The address is checked
// after DNS resolution and again on the socket actually connected (so a DNS rebinding between check and connect
// is caught); redirects are not followed; proxies from the environment are ignored.
func NewEgressClient(p EgressPolicy, timeout time.Duration) *http.Client {
	return newEgressClient(p, timeout, defaultLookup)
}

func newEgressClient(p EgressPolicy, timeout time.Duration, lookup lookupFunc) *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: %s", ErrEgressBlocked, address)
			}
			return p.CheckAddr(ap.Addr())
		},
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			var addrs []netip.Addr
			if a, err := netip.ParseAddr(host); err == nil {
				addrs = []netip.Addr{a}
			} else {
				addrs, err = lookup(ctx, host)
				if err != nil {
					return nil, err
				}
			}
			lastErr := error(ErrEgressBlocked)
			for _, a := range addrs {
				if err := p.CheckAddr(a); err != nil {
					lastErr = err
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Sign returns the Porto-Signature header value for body sent at the given time:
// "t=<unix>,v1=<hex HMAC-SHA256(secret, "<unix>.<body>")>".
func Sign(secret string, body []byte, at time.Time) string {
	return SignAll([]string{secret}, body, at)
}

// SignAll signs with every secret (one v1= per secret), so that receivers can rotate secrets.
func SignAll(secrets []string, body []byte, at time.Time) string {
	ts := strconv.FormatInt(at.Unix(), 10)
	var b strings.Builder
	b.WriteString("t=")
	b.WriteString(ts)
	for _, s := range secrets {
		b.WriteString(",v1=")
		b.WriteString(mac(s, ts, body))
	}
	return b.String()
}

func mac(secret, ts string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(ts))
	h.Write([]byte("."))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// Signature verification errors.
var (
	ErrSignatureMalformed = errors.New("event: malformed signature header")
	ErrSignatureExpired   = errors.New("event: signature timestamp outside tolerance")
	ErrSignatureMismatch  = errors.New("event: signature mismatch")
)

// VerifySignature checks a Porto-Signature header (receiver side; used by tests and integrators' examples).
func VerifySignature(secret, header string, body []byte, now time.Time, tolerance time.Duration) error {
	var ts string
	var sigs []string
	for part := range strings.SplitSeq(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return ErrSignatureMalformed
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return ErrSignatureMalformed
	}
	at := time.Unix(unix, 0)
	if d := now.Sub(at); d > tolerance || d < -tolerance {
		return ErrSignatureExpired
	}
	want := mac(secret, ts, body)
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return ErrSignatureMismatch
}
