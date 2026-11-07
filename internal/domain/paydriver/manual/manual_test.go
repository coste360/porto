package manual

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

func TestManual_NoOnlineCapability(t *testing.T) {
	d := New()
	c := d.Capabilities()
	if d.Code() != "manual" || c.Checkout || c.MandateSetup || c.MandateCharge || c.Refund || c.StatusCheck || len(c.Methods) != 5 {
		t.Fatalf("capabilities = %+v", c)
	}
	ctx := context.Background()
	if _, err := d.CreateCheckout(ctx, ext.CheckoutRequest{}); !errors.Is(err, ext.ErrNotSupported) {
		t.Fatalf("checkout: %v", err)
	}
	if _, err := d.ParseWebhook(httptest.NewRequest("POST", "/", nil), []byte("s"), time.Unix(0, 0)); !errors.Is(err, ext.ErrInvalidSignature) {
		t.Fatalf("webhook: %v", err)
	}
}
