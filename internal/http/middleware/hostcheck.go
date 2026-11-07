package middleware

import (
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/http/problem"
)

// HostCheck answers 421 Misdirected Request when the Host header is not one of allowed (host:port, compared
// case-insensitively, a trailing dot of the name ignored). It protects the loopback API from DNS rebinding (a
// page of evil.example resolving to 127.0.0.1 sends Host: evil.example:<port>) and pins the local-network
// listener to the names of its certificate (INV-80). An empty allowed list disables the check (server edition).
func HostCheck(allowed []string) func(http.Handler) http.Handler {
	hosts := make(map[string]struct{}, len(allowed))
	for _, h := range allowed {
		if h = normalizeHost(h); h != "" {
			hosts[h] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		if len(hosts) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := hosts[normalizeHost(r.Host)]; !ok {
				problem.Write(w, r, problem.BadRequest, problem.Status(http.StatusMisdirectedRequest),
					problem.Detail("unexpected host"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// normalizeHost lowercases host[:port] and drops the trailing dot of a fully qualified name ("porto.local.:8443").
func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.HasSuffix(h, "]") {
		return strings.TrimSuffix(h[:i], ".") + h[i:]
	}
	return strings.TrimSuffix(h, ".")
}
