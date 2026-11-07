package desktop

import (
	"context"
	"log/slog"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/desktopport"
)

// LAN is the local-network server as the launcher needs it (internal/lan implements it): the controller plus the
// pairing target used by device.Service.
type LAN interface {
	desktopport.LANController
	PairTarget() desktopport.PairTarget
}

// LANDeps is what the launcher hands to the local-network server of one App (a restore builds a new App and a new
// server).
type LANDeps struct {
	// App builds the TLS listener's http.Server with App.NewHTTPServer(reqctx.ListenerLAN, hosts).
	App   *app.App
	Dir   string // <data>/lan: certificates (private keys KeyRing-encrypted)
	Keys  *crypto.KeyRing
	Clock clock.Clock
	Log   *slog.Logger
	// Port and Address are the stored choices (desktop.json, PORTO_DESKTOP_LAN_PORT); Address "" = automatic.
	Port    int
	Address string
	// Origin is the loopback base URL (pairing QRs point there, unreachable, while the local network is off).
	Origin string
	// Loopback (dev only, PORTO_DESKTOP_LAN_LOOPBACK) binds the listener on 127.0.0.1 for tests.
	Loopback bool
}

// LANFactory builds the local-network server. The listener must not exist before Enable (INV-79).
type LANFactory func(LANDeps) (LAN, error)

// unavailableLAN is the controller used when no LANFactory is wired: the local network cannot be enabled.
type unavailableLAN struct {
	port   int
	origin string
}

func (u *unavailableLAN) Status() desktopport.LANStatus { return desktopport.LANStatus{Port: u.port} }

func (u *unavailableLAN) Enable(context.Context, desktopport.LANOptions) (desktopport.LANStatus, error) {
	st := u.Status()
	st.Error = "lan_no_address"
	return st, desktopport.ErrLANNoAddress
}

func (*unavailableLAN) Disable(context.Context) error { return nil }

func (u *unavailableLAN) RotateCertificate(context.Context, bool) (desktopport.LANStatus, error) {
	return u.Status(), desktopport.ErrLANNoAddress
}

func (u *unavailableLAN) PairTarget() desktopport.PairTarget {
	return desktopport.PairTarget{Origin: u.origin}
}
