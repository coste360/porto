package webhooks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/domain/signature"
)

// TestSignature_WebhookUnknownProvider404: with no registered provider every callback is 404 not_found, the same
// answer as an unknown marina (Porto ships no signature provider, spec 33 D5).
func TestSignature_WebhookUnknownProvider404(t *testing.T) {
	for _, h := range []*Handler{{}, {Signatures: signature.NewService(signature.Deps{})}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/webhooks/signature/infocert/01900000-0000-7000-8000-000000000000", strings.NewReader("{}"))
		h.SignatureWebhook(w, r, "infocert", "01900000-0000-7000-8000-000000000000")
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"not_found"`) {
			t.Fatalf("status %d %s", w.Code, w.Body.String())
		}
	}
}
