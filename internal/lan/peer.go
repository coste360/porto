package lan

import (
	"net"
	"net/netip"
)

// IsPrivatePeer reports whether a TCP peer may talk to the local-network listener: loopback, RFC 1918, IPv6 ULA
// (fc00::/7) or link-local. IPv4-mapped IPv6 addresses are unmapped first; CGNAT (100.64/10) and every public
// address are refused (INV-79).
func IsPrivatePeer(a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast()
}

// PrivateListener closes, before reading a byte, every accepted connection whose TCP peer is not private. Wrap
// it with tls.NewListener so the handshake never starts for refused peers.
type PrivateListener struct {
	net.Listener
	// OnReject, when set, is told about each refused peer (counted in the status; never logged at info level).
	OnReject func(netip.Addr)
}

// Accept returns the next connection from a private peer.
func (l *PrivateListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		peer := peerAddr(c.RemoteAddr())
		if IsPrivatePeer(peer) {
			return c, nil
		}
		_ = c.Close()
		if l.OnReject != nil {
			l.OnReject(peer)
		}
	}
}

func peerAddr(a net.Addr) netip.Addr {
	if ta, ok := a.(*net.TCPAddr); ok {
		return ta.AddrPort().Addr()
	}
	if a == nil {
		return netip.Addr{}
	}
	ap, err := netip.ParseAddrPort(a.String())
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr()
}
