package middleware

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/reqctx"
)

// Listener middleware of the desktop edition (m6a-plan §2.10, §3.3); HostCheck is in hostcheck.go.

// ListenerTag returns an http.Server ConnContext that tags every connection with its listener kind.
func ListenerTag(kind reqctx.ListenerKind) func(ctx context.Context, c net.Conn) context.Context {
	return func(ctx context.Context, _ net.Conn) context.Context { return reqctx.WithListener(ctx, kind) }
}

// LoopbackOnly answers 404 not_found for requests under prefix unless they arrived on the desktop loopback
// listener from a loopback TCP peer (INV-80). The peer is r.RemoteAddr, never a forwarded header.
func LoopbackOnly(prefix string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, prefix) &&
				(!reqctx.IsLoopbackListener(r.Context()) || !loopbackPeer(r.RemoteAddr)) {
				problem.Write(w, r, problem.NotFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func loopbackPeer(remote string) bool {
	ap, err := netip.ParseAddrPort(remote)
	if err != nil {
		return false
	}
	return ap.Addr().Unmap().IsLoopback()
}
