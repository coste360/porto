package public

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPayPage_HeadersAndUnknown: the public pay routes are never cached, send no referrer, and answer link_invalid
// without a collection service (same answer as an unknown token).
func TestPayPage_HeadersAndUnknown(t *testing.T) {
	h := &Handler{}
	for name, call := range map[string]func(w http.ResponseWriter, r *http.Request){
		"page":     func(w http.ResponseWriter, r *http.Request) { h.GetPayPage(w, r, "marina", "TOKEN") },
		"status":   func(w http.ResponseWriter, r *http.Request) { h.GetPayStatus(w, r, "marina", "TOKEN") },
		"checkout": func(w http.ResponseWriter, r *http.Request) { h.StartPayCheckout(w, r, "marina", "TOKEN") },
	} {
		w := httptest.NewRecorder()
		call(w, httptest.NewRequest(http.MethodGet, "/api/public/v1/marina/pay/TOKEN", strings.NewReader(`{"method":"card"}`)))
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "link_invalid") ||
			w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("%s: %d %s %v", name, w.Code, w.Body.String(), w.Header())
		}
	}
}
