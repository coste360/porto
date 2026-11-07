package meta

import (
	"slices"
	"testing"
)

func TestGet_Editions(t *testing.T) {
	srv := Get(nil, "1.0.0", EditionServer)
	if srv.Edition != EditionServer || srv.LocalMode || slices.Contains(srv.AllCapabilities(), CapDesktopLocal) {
		t.Fatalf("server: %+v", srv)
	}
	if got := Get(nil, "1.0.0", "bogus").Edition; got != EditionServer {
		t.Fatalf("unknown edition = %q", got)
	}
	lan := false
	d := Get(nil, "1.0.0", EditionDesktop)
	d.Dynamic = func() []string {
		if lan {
			return []string{CapDesktopLAN}
		}
		return nil
	}
	if d.Edition != EditionDesktop || !d.LocalMode || !slices.Contains(d.AllCapabilities(), CapDesktopLocal) ||
		slices.Contains(d.AllCapabilities(), CapDesktopLAN) {
		t.Fatalf("desktop: %+v", d)
	}
	lan = true
	if !slices.Contains(d.AllCapabilities(), CapDesktopLAN) {
		t.Fatal("desktop.lan missing while the listener runs")
	}
	if slices.Contains(d.Capabilities, CapDesktopLAN) {
		t.Fatal("dynamic capability leaked into the static list")
	}
}

func TestWith_DoNotAliasCapabilities(t *testing.T) {
	base := Get(nil, "v", EditionServer)
	n := len(base.Capabilities)
	_ = base.WithSignatureProviders(1).WithBilling(1, 1)
	if len(base.Capabilities) != n {
		t.Fatal("With* mutated the base capabilities")
	}
}
