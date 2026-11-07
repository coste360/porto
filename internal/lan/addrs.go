package lan

import (
	"net"
	"net/netip"
	"slices"
	"strings"
)

// Interface is a network interface as the address choice sees it (pure input of PrivateAddrs and PickAddress).
type Interface struct {
	Name     string
	Up       bool
	Loopback bool
	Addrs    []netip.Addr
}

// SystemInterfaces lists the interfaces of this machine with their unicast addresses.
func SystemInterfaces() ([]Interface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]Interface, 0, len(ifs))
	for _, ifc := range ifs {
		it := Interface{Name: ifc.Name, Up: ifc.Flags&net.FlagUp != 0, Loopback: ifc.Flags&net.FlagLoopback != 0}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
				it.Addrs = append(it.Addrs, ip.Unmap())
			}
		}
		out = append(out, it)
	}
	return out, nil
}

// virtualPrefixes are interface names of containers, VMs, VPNs and Apple peer-to-peer links: phones on the
// marina Wi-Fi never reach those addresses.
var virtualPrefixes = []string{"docker", "br-", "veth", "vboxnet", "vmnet", "utun", "awdl", "llw", "tailscale", "zt"}

// IsVirtualInterface reports whether an interface name belongs to a virtual network.
func IsVirtualInterface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// addrRank orders usable addresses: 192.168/16 (home and small-office routers), 10/8, 172.16/12, then IPv6 ULA.
// -1 means unusable for phones (public, loopback, link-local, CGNAT).
func addrRank(a netip.Addr) int {
	a = a.Unmap()
	switch {
	case a.Is4() && netip.MustParsePrefix("192.168.0.0/16").Contains(a):
		return 0
	case a.Is4() && netip.MustParsePrefix("10.0.0.0/8").Contains(a):
		return 1
	case a.Is4() && netip.MustParsePrefix("172.16.0.0/12").Contains(a):
		return 2
	case a.Is6() && a.IsPrivate():
		return 3
	}
	return -1
}

// PrivateAddrs returns the private addresses of the up, non-loopback, non-virtual interfaces, best first
// (stable within a rank), without duplicates. These are the certificate SANs and the addresses offered in the UI.
func PrivateAddrs(ifaces []Interface) []netip.Addr {
	var out []netip.Addr
	for _, it := range ifaces {
		if !it.Up || it.Loopback || IsVirtualInterface(it.Name) {
			continue
		}
		for _, a := range it.Addrs {
			a = a.Unmap().WithZone("")
			if addrRank(a) >= 0 && !slices.Contains(out, a) {
				out = append(out, a)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b netip.Addr) int { return addrRank(a) - addrRank(b) })
	return out
}

// PickAddress returns the address phones should use: the best of PrivateAddrs; ok is false when there is none.
func PickAddress(ifaces []Interface) (netip.Addr, bool) {
	addrs := PrivateAddrs(ifaces)
	if len(addrs) == 0 {
		return netip.Addr{}, false
	}
	return addrs[0], true
}

// MDNSName returns "<hostname>.local" for a machine name ("Ufficio-Porto.fritz.box" → "ufficio-porto.local"),
// or "" when nothing usable remains.
func MDNSName(hostname string) string {
	h, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(hostname)), ".")
	var b strings.Builder
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '_':
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" || len(s) > 63 {
		return ""
	}
	return s + ".local"
}
