package reqctx

import "context"

// ListenerKind tells which listener accepted a connection (m6a-plan §2.10). The server edition uses
// ListenerServer; the desktop edition tags its loopback and local-network listeners.
type ListenerKind string

// Listener kinds.
const (
	ListenerServer   ListenerKind = "server"
	ListenerLoopback ListenerKind = "loopback"
	ListenerLAN      ListenerKind = "lan"
)

// Valid reports whether k is a known listener kind.
func (k ListenerKind) Valid() bool {
	switch k {
	case ListenerServer, ListenerLoopback, ListenerLAN:
		return true
	}
	return false
}

type listenerKey struct{}

// WithListener returns a copy of ctx tagged with the listener kind (set once per connection by the HTTP server's
// ConnContext).
func WithListener(ctx context.Context, k ListenerKind) context.Context {
	return context.WithValue(ctx, listenerKey{}, k)
}

// ListenerFrom returns the listener kind of ctx; ListenerServer when the connection was not tagged.
func ListenerFrom(ctx context.Context) ListenerKind {
	if ctx != nil {
		if k, ok := ctx.Value(listenerKey{}).(ListenerKind); ok && k.Valid() {
			return k
		}
	}
	return ListenerServer
}

// IsLoopbackListener reports whether ctx belongs to a connection of the desktop loopback listener.
func IsLoopbackListener(ctx context.Context) bool { return ListenerFrom(ctx) == ListenerLoopback }
