package lan

import (
	"crypto/tls"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"
)

func TestIsPrivatePeer(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1":            true,
		"::1":                  true,
		"10.0.0.1":             true,
		"172.16.0.1":           true,
		"172.31.255.255":       true,
		"192.168.1.20":         true,
		"169.254.10.10":        true,
		"fe80::1%en0":          true,
		"fd12:3456::1":         true,
		"fc00::1":              true,
		"::ffff:192.168.1.20":  true,
		"::ffff:127.0.0.1":     true,
		"100.64.0.1":           false, // CGNAT
		"100.127.255.254":      false,
		"172.32.0.1":           false,
		"8.8.8.8":              false,
		"::ffff:8.8.8.8":       false,
		"2001:4860:4860::8888": false,
		"192.0.2.1":            false,
		"0.0.0.0":              false,
		"::":                   false,
		"64:ff9b::c0a8:114":    false, // NAT64 of 192.168.1.20 is still a public mapping
	} {
		if got := IsPrivatePeer(netip.MustParseAddr(addr)); got != want {
			t.Errorf("IsPrivatePeer(%s) = %v, want %v", addr, got, want)
		}
	}
	if IsPrivatePeer(netip.Addr{}) {
		t.Fatal("invalid address accepted")
	}
}

// fakeConn is an accepted connection with a chosen peer address.
type fakeConn struct {
	net.Conn
	remote net.Addr
	mu     sync.Mutex
	closed bool
}

func (c *fakeConn) RemoteAddr() net.Addr { return c.remote }
func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}
func (c *fakeConn) Read([]byte) (int, error) { panic("a refused connection must never be read") }

type fakeListener struct {
	conns chan net.Conn
}

func (l *fakeListener) Accept() (net.Conn, error) {
	c, ok := <-l.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return c, nil
}
func (l *fakeListener) Close() error   { return nil }
func (l *fakeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4zero, Port: 8443} }

func TestPrivateListener_RejectsPublicPeers(t *testing.T) {
	tcp := func(s string) net.Addr { return net.TCPAddrFromAddrPort(netip.MustParseAddrPort(s)) }
	public := []*fakeConn{
		{remote: tcp("8.8.8.8:5000")},
		{remote: tcp("100.64.3.3:5000")},
		{remote: tcp("[2001:db8::1]:5000")},
		{remote: tcp("[::ffff:203.0.113.7]:5000")},
	}
	private := &fakeConn{remote: tcp("192.168.1.30:5000")}
	fl := &fakeListener{conns: make(chan net.Conn, 8)}
	for _, c := range public {
		fl.conns <- c
	}
	fl.conns <- private
	close(fl.conns)
	var rejected []netip.Addr
	pl := &PrivateListener{Listener: fl, OnReject: func(a netip.Addr) { rejected = append(rejected, a) }}
	c, err := pl.Accept()
	if err != nil || c != private {
		t.Fatalf("accepted %v, %v; want the private peer", c, err)
	}
	for _, c := range public {
		if !c.closed {
			t.Errorf("connection from %s not closed", c.remote)
		}
	}
	if len(rejected) != len(public) {
		t.Fatalf("rejected %v", rejected)
	}
	if _, err := pl.Accept(); err == nil {
		t.Fatal("closed listener accepted")
	}
}

// A refused peer never gets a TLS handshake: the private filter runs under tls.NewListener.
func TestPrivateListener_RefusedBeforeHandshake(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	refuseAll := &PrivateListener{Listener: &peerOverride{Listener: raw, peer: tcpAddr("203.0.113.9:4000")}}
	ln := tls.NewListener(refuseAll, &tls.Config{MinVersion: tls.VersionTLS12})
	go func() {
		if c, err := ln.Accept(); err == nil {
			_ = c.Close()
			t.Error("public peer accepted")
		}
	}()
	c, err := net.DialTimeout("tcp", raw.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if n, err := c.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("read %d bytes, %v: the server answered a refused peer", n, err)
	}
}

func tcpAddr(s string) net.Addr { return net.TCPAddrFromAddrPort(netip.MustParseAddrPort(s)) }

// peerOverride makes every accepted connection look like it comes from peer.
type peerOverride struct {
	net.Listener
	peer net.Addr
}

func (l *peerOverride) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &remoteConn{Conn: c, remote: l.peer}, nil
}

type remoteConn struct {
	net.Conn
	remote net.Addr
}

func (c *remoteConn) RemoteAddr() net.Addr { return c.remote }
