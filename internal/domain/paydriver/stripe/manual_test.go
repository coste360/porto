package stripe

import (
	"errors"
	"testing"

	"github.com/coste360/porto/pkg/ext"
)

func TestStripe_ManualCaptureMapping(t *testing.T) {
	form := CheckoutForm(ext.CheckoutRequest{MarinaID: "m", IntentID: "i", AmountCents: 100, CaptureManual: true})
	if form.Get("payment_intent_data[capture_method]") != "manual" {
		t.Fatalf("manual capture not requested: %v", form)
	}
	if CheckoutForm(ext.CheckoutRequest{MarinaID: "m", IntentID: "i", AmountCents: 100}).Has("payment_intent_data[capture_method]") {
		t.Fatal("automatic checkout asks for manual capture")
	}
	meta := `"metadata": {"marina_id": "01900000-0000-7000-8000-00000000000a", "intent_id": "01900000-0000-7000-8000-0000000000b1"}`
	cases := []struct {
		name, body, kind string
		amount           int64
		ignored          bool
	}{
		{"authorised", `{"id": "evt_auth", "type": "payment_intent.amount_capturable_updated", "created": 1814000100,
			"data": {"object": {"id": "pi_m", "status": "requires_capture", "amount": 3000, "amount_capturable": 3000,
			"currency": "eur", ` + meta + `}}}`, ext.PaymentEventAuthorized, 3000, false},
		{"capturable but not requires_capture", `{"id": "evt_auth2", "type": "payment_intent.amount_capturable_updated",
			"data": {"object": {"id": "pi_m", "status": "succeeded", "amount": 3000, "currency": "eur"}}}`, "", 0, true},
		{
			"voided", `{"id": "evt_void", "type": "payment_intent.canceled", "created": 1814000200,
			"data": {"object": {"id": "pi_m", "status": "canceled", "amount": 3000, "currency": "eur", ` + meta + `}}}`,
			ext.PaymentEventAuthorizationVoided, 3000, false,
		},
		{"captured", `{"id": "evt_cap", "type": "payment_intent.succeeded", "created": 1814000300,
			"data": {"object": {"id": "pi_m", "status": "succeeded", "amount": 3000, "amount_received": 3000,
			"currency": "eur", ` + meta + `}}}`, ext.PaymentEventSucceeded, 3000, false},
		{"session of a manual capture", `{"id": "evt_cs", "type": "checkout.session.completed",
			"data": {"object": {"id": "cs_m", "mode": "payment", "status": "complete", "payment_status": "unpaid",
			"payment_intent": "pi_m", "amount_total": 3000, "currency": "eur", ` + meta + `}}}`, ext.PaymentEventProcessing, 3000, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, err := MapEvent([]byte(c.body))
			if c.ignored {
				if !errors.Is(err, ext.ErrIgnoredEvent) {
					t.Fatalf("want ignored, got %+v %v", ev, err)
				}
				return
			}
			if err != nil || ev.Kind != c.kind || ev.AmountCents != c.amount || ev.PaymentRef != "pi_m" {
				t.Fatalf("got %+v %v", ev, err)
			}
			if ev.Metadata["intent_id"] == "" {
				t.Fatalf("metadata lost: %+v", ev.Metadata)
			}
		})
	}
}

func TestStripe_CaptureAndVoidCalls(t *testing.T) {
	rc := &recorder{resp: func(path string) (int, string) {
		return 200, `{"id":"pi_m","object":"payment_intent","status":"succeeded"}`
	}}
	srv := rc.server(t)
	d := New(srv.Client(), srv.URL)
	p, err := d.Capture(creds(), ext.CaptureRequest{IntentID: "i", IdempotencyKey: "capture:i", PaymentRef: "pi_m", AmountCents: 3000})
	if err != nil || p.Status != ext.PaymentEventSucceeded {
		t.Fatalf("capture %+v %v", p, err)
	}
	if err := d.VoidAuthorization(creds(), ext.VoidRequest{IntentID: "i", IdempotencyKey: "void:i", PaymentRef: "pi_m"}); err != nil {
		t.Fatal(err)
	}
	if len(rc.reqs) != 2 || rc.reqs[0].path != "/v1/payment_intents/pi_m/capture" || rc.reqs[0].idem != "capture:i" ||
		rc.reqs[0].form.Get("amount_to_capture") != "3000" || rc.reqs[1].path != "/v1/payment_intents/pi_m/cancel" || rc.reqs[1].idem != "void:i" {
		t.Fatalf("calls %+v", rc.reqs)
	}
	if _, err := d.Capture(creds(), ext.CaptureRequest{}); err == nil {
		t.Fatal("capture without reference")
	}
}
