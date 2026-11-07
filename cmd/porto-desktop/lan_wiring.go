package main

import (
	"net/http"

	"github.com/coste360/porto/internal/desktop"
	"github.com/coste360/porto/internal/lan"
	"github.com/coste360/porto/internal/reqctx"
)

// The local-network server of each App (internal/lan): its HTTP server is the App's, tagged ListenerLAN and
// Host-checked against the certificate names.
func init() {
	newLAN = func(d desktop.LANDeps) (desktop.LAN, error) {
		return lan.New(lan.Options{
			Dir: d.Dir, Keys: d.Keys, Clock: d.Clock, Log: d.Log, Port: d.Port, Address: d.Address,
			Loopback: d.Loopback, FallbackOrigin: d.Origin,
			NewServer: func(hosts []string) *http.Server { return d.App.NewHTTPServer(reqctx.ListenerLAN, hosts) },
		}), nil
	}
}
