package lan

import (
	"net/netip"
	"slices"
	"testing"
)

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, len(s))
	for i, a := range s {
		out[i] = netip.MustParseAddr(a)
	}
	return out
}

func TestPickAddress_Preferences(t *testing.T) {
	for _, c := range []struct {
		name   string
		ifaces []Interface
		want   string
		all    []string
	}{
		{
			name: "192.168 beats 10 and 172.16",
			ifaces: []Interface{
				{Name: "en1", Up: true, Addrs: addrs("172.16.0.5", "10.1.2.3")},
				{Name: "en0", Up: true, Addrs: addrs("192.168.1.20", "fe80::1")},
			},
			want: "192.168.1.20", all: []string{"192.168.1.20", "10.1.2.3", "172.16.0.5"},
		},
		{
			name:   "10 beats 172.16",
			ifaces: []Interface{{Name: "eth0", Up: true, Addrs: addrs("172.20.0.1", "10.0.0.9")}},
			want:   "10.0.0.9", all: []string{"10.0.0.9", "172.20.0.1"},
		},
		{
			name: "virtual, down and loopback interfaces are skipped",
			ifaces: []Interface{
				{Name: "lo0", Up: true, Loopback: true, Addrs: addrs("127.0.0.1", "::1")},
				{Name: "docker0", Up: true, Addrs: addrs("192.168.65.1")},
				{Name: "br-1234", Up: true, Addrs: addrs("192.168.70.1")},
				{Name: "veth9", Up: true, Addrs: addrs("192.168.71.1")},
				{Name: "vboxnet0", Up: true, Addrs: addrs("192.168.56.1")},
				{Name: "vmnet8", Up: true, Addrs: addrs("192.168.57.1")},
				{Name: "utun3", Up: true, Addrs: addrs("10.8.0.2")},
				{Name: "awdl0", Up: true, Addrs: addrs("fd00::5")},
				{Name: "llw0", Up: true, Addrs: addrs("fd00::6")},
				{Name: "tailscale0", Up: true, Addrs: addrs("100.100.1.1")},
				{Name: "zt0", Up: true, Addrs: addrs("10.147.17.1")},
				{Name: "en0", Up: false, Addrs: addrs("192.168.1.30")},
				{Name: "wlan0", Up: true, Addrs: addrs("172.16.8.8")},
			},
			want: "172.16.8.8", all: []string{"172.16.8.8"},
		},
		{
			name:   "public, CGNAT and link-local are never offered; ULA comes last",
			ifaces: []Interface{{Name: "en0", Up: true, Addrs: addrs("203.0.113.5", "100.64.0.1", "169.254.1.1", "fd12::1", "::ffff:10.0.0.1")}},
			want:   "10.0.0.1", all: []string{"10.0.0.1", "fd12::1"},
		},
		{
			name:   "nothing usable",
			ifaces: []Interface{{Name: "en0", Up: true, Addrs: addrs("203.0.113.5")}},
		},
	} {
		got, ok := PickAddress(c.ifaces)
		if c.want == "" {
			if ok {
				t.Errorf("%s: picked %s", c.name, got)
			}
			continue
		}
		if !ok || got.String() != c.want {
			t.Errorf("%s: picked %s (%v), want %s", c.name, got, ok, c.want)
		}
		var all []string
		for _, a := range PrivateAddrs(c.ifaces) {
			all = append(all, a.String())
		}
		if !slices.Equal(all, c.all) {
			t.Errorf("%s: addresses %v, want %v", c.name, all, c.all)
		}
	}
}

func TestMDNSName(t *testing.T) {
	for in, want := range map[string]string{
		"Ufficio-Porto.fritz.box": "ufficio-porto.local",
		"MacBook Pro di Anna":     "macbook-pro-di-anna.local",
		"PC_Banchina":             "pc-banchina.local",
		"   ":                     "",
		"###":                     "",
	} {
		if got := MDNSName(in); got != want {
			t.Errorf("MDNSName(%q) = %q, want %q", in, got, want)
		}
	}
}
