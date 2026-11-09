//go:build integration

package admin_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

// TestRentalHandoverHandlers_Scenario walks the handover operations through HTTP: statuses, problem types and
// another marina's 404 (m11-plan §13.1).
func TestRentalHandoverHandlers_Scenario(t *testing.T) {
	bs := newBookingTSetup(t)
	c, rc, M, X := bs.owner, bs.rental, bs.M, bs.otherM

	var set api.RentalSettings
	c.expect(t, 200, "", "GET", M+"/rental/settings", nil, &set)
	c.expect(t, 200, "", "PUT", M+"/rental/settings", map[string]any{
		"open_from_slot": 16, "open_to_slot": 38, "bands": set.Bands, "buffer_slots": 1, "desk_hold_minutes": 60,
		"no_show_after_minutes": 60, "licence_threshold_kw_milli": 30000, "skipper_item_id": bs.skp, "checklist": set.Checklist,
		"checkout_min_photos": 2, "checkin_min_photos": 1, "deposit_modes": []string{"cash", "card_pos"}, "identity_retention_days": 365,
		"damage_item_id": bs.nonRent,
	}, nil, "If-Match", etag(int(set.Version)))
	var b api.RentalBooking
	rc.expect(t, 201, "", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.morning, "start": pt("2027-06-12", 16), "end": pt("2027-06-12", 26),
		"renter": map[string]string{"name": "Ada Rossi"},
	}, &b)
	B := M + "/rental/bookings/" + b.Id

	// Another marina's booking is 404 on the handover routes.
	c.expect(t, 404, "not_found", "GET", strings.Replace(B, M, X, 1)+"/handovers/checkout", nil, nil)
	c.expect(t, 404, "not_found", "POST", strings.Replace(B, M, X, 1)+"/quick-cash", map[string]any{"method": "cash"}, nil)

	var h api.RentalHandover
	rc.expect(t, 200, "", "GET", B+"/handovers/checkout", nil, &h)
	if h.Version != 0 || len(h.Missing) == 0 {
		t.Fatalf("empty draft %+v", h)
	}
	rc.expect(t, 404, "not_found", "GET", B+"/handovers/sideways", nil, nil)
	rc.expect(t, 428, "precondition_required", "POST", B+"/check-out", map[string]any{}, nil)
	rc.expect(t, 422, "handover_incomplete", "POST", B+"/check-out", map[string]any{}, nil, "If-Match", etag(int(b.Version)))

	// Identity and licence.
	rc.expect(t, 400, "validation_failed", "PUT", B+"/identity", map[string]any{"document_type": "card", "document_number": "X", "document_expires_on": "2030-01-01"}, nil)
	rc.expect(t, 422, "licence_expired", "PUT", B+"/identity", map[string]any{
		"document_type": "identity_card", "document_number": "CA123", "document_expires_on": "2030-01-01",
		"licence_number": "L1", "licence_expires_on": "2027-06-01",
	}, nil)
	rc.expect(t, 200, "", "PUT", B+"/identity", map[string]any{
		"document_type": "identity_card", "document_number": "CA12345XY", "document_expires_on": "2030-01-01",
		"licence_number": "LT-998877", "licence_category": "12 miglia", "licence_expires_on": "2031-05-01",
	}, &h)
	if h.Identity == nil || h.Identity.DocumentNumberMasked != "******5XY" || strings.Contains(h.Identity.DocumentNumberMasked, "CA1") {
		t.Fatalf("identity %+v", h.Identity)
	}

	// Photos (raw body).
	upload := func(kind string, shade byte, status int, problem string) {
		t.Helper()
		path := B + "/handovers/checkout/photos"
		if kind != "" {
			path += "?kind=" + kind + "&client_ref=" + kind + string('a'+rune(shade))
		}
		body := pngShade(t, shade)
		r := rc.raw(t, "POST", path, "image/png", bytes.NewReader(body))
		if r.status != status || (problem != "" && r.problem() != problem) {
			t.Fatalf("upload %s: %d %s", kind, r.status, r.body)
		}
	}
	upload("", 1, 400, "validation_failed")
	if r := rc.raw(t, "POST", B+"/handovers/checkout/photos?kind=boat", "text/plain", strings.NewReader("hello")); r.status != 415 {
		t.Fatalf("text photo: %d %s", r.status, r.body)
	}
	upload("id_document", 2, 201, "")
	upload("licence", 3, 201, "")
	upload("boat", 4, 201, "")
	upload("boat", 5, 201, "")

	// Contract on screen.
	rc.expect(t, 400, "validation_failed", "POST", B+"/contract/sign", map[string]any{"driver": "screen", "signature_png": "!!"}, nil)
	var sig api.RentalSignature
	rc.expect(t, 200, "", "POST", B+"/contract/sign", map[string]any{
		"driver": "screen", "signature_png": base64.StdEncoding.EncodeToString(pngShade(t, 9)), "device_label": "Tablet",
	}, &sig)
	if sig.Status != "signed" || sig.Driver != "screen" || sig.SignedDocumentId == nil {
		t.Fatalf("signature %+v", sig)
	}

	// Deposit.
	rc.expect(t, 422, "preauth_unavailable", "POST", B+"/deposit", map[string]any{"mode": "preauth"}, nil)
	rc.expect(t, 409, "cash_session_required", "POST", B+"/deposit", map[string]any{"mode": "cash"}, nil)
	rc.expect(t, 201, "", "POST", M+"/cash-sessions", map[string]any{"float_cents": 0}, nil)
	var dep api.RentalDeposit
	rc.expect(t, 200, "", "POST", B+"/deposit", map[string]any{"mode": "cash"}, &dep)
	if dep.Status != "held" || dep.AmountCents != 50000 {
		t.Fatalf("deposit %+v", dep)
	}

	// Checklist (offline-capable), then check-out.
	items := []map[string]any{}
	for _, it := range []string{"hull", "engine", "lifejackets", "anchor", "extinguisher", "vhf", "documents", "cleanliness"} {
		items = append(items, map[string]any{"key": it, "ok": true})
	}
	rc.expect(t, 200, "", "PUT", B+"/handovers/checkout/checklist", map[string]any{
		"client_ref": "checkout-0001", "items": items, "fuel_eighths": 8, "engine_hours_milli": 0,
	}, &h)
	if len(h.Missing) != 0 {
		t.Fatalf("still missing %v", h.Missing)
	}
	rc.expect(t, 412, "version_conflict", "POST", B+"/check-out", map[string]any{}, nil, "If-Match", etag(int(b.Version)+5))
	var out api.RentalBooking
	rc.expect(t, 200, "", "POST", B+"/check-out", map[string]any{}, &out, "If-Match", etag(int(b.Version)))
	if out.Status != "checked_out" {
		t.Fatalf("checked out %+v", out)
	}
	rc.expect(t, 409, "handover_locked", "PUT", B+"/handovers/checkout/checklist", map[string]any{"client_ref": "checkout-0002", "items": items}, nil)

	// PDFs: without a signed link the server redirects to one signed for the caller.
	if r := rc.do(t, "GET", B+"/contract/pdf", nil); r.status != 200 || r.header.Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(r.body, []byte("%PDF-")) {
		t.Fatalf("contract pdf %d %s", r.status, r.header.Get("Content-Type"))
	}
	rc.expect(t, 403, "", "GET", B+"/contract/pdf?exp=1&sig=forged", nil, nil)
	if r := rc.do(t, "GET", B+"/handovers/checkout/pdf", nil); r.status != 200 || !bytes.HasPrefix(r.body, []byte("%PDF-")) {
		t.Fatalf("handover pdf %d %s", r.status, r.body)
	}

	// Damages.
	var dm api.RentalDamage
	rc.expect(t, 400, "validation_failed", "POST", B+"/damages", map[string]any{"description": "", "estimate_cents": 100}, nil)
	rc.expect(t, 201, "", "POST", B+"/damages", map[string]any{"description": "Graffio", "estimate_cents": 18000}, &dm)
	rc.expect(t, 200, "", "PATCH", M+"/rental/damages/"+dm.Id, map[string]any{"estimate_cents": 15000}, &dm, "If-Match", etag(int(dm.Version)))
	var dm2 api.RentalDamage
	rc.expect(t, 201, "", "POST", B+"/damages", map[string]any{"description": "Doppione", "estimate_cents": 1}, &dm2)
	rc.expect(t, 412, "version_conflict", "DELETE", M+"/rental/damages/"+dm2.Id, nil, nil, "If-Match", etag(int(dm2.Version)+1))
	rc.expect(t, 204, "", "DELETE", M+"/rental/damages/"+dm2.Id, nil, nil, "If-Match", etag(int(dm2.Version)))
	c.expect(t, 404, "not_found", "PATCH", X+"/rental/damages/"+dm.Id, map[string]any{"estimate_cents": 1}, nil, "If-Match", etag(int(dm.Version)))

	// Check-in.
	rc.expect(t, 200, "", "PUT", B+"/handovers/checkin/checklist", map[string]any{
		"client_ref": "checkin-00001", "items": items, "fuel_eighths": 8, "engine_hours_milli": 1000,
	}, nil)
	if r := rc.raw(t, "POST", B+"/handovers/checkin/photos?kind=boat", "image/png", bytes.NewReader(pngShade(t, 40))); r.status != 201 {
		t.Fatalf("checkin photo %d %s", r.status, r.body)
	}
	var pv api.RentalCheckInPreview
	rc.expect(t, 200, "", "POST", B+"/check-in/preview", map[string]any{"returned_at": "2026-01-01T00:00:00Z", "deposit_decision": "retain", "retain_cents": 15000}, &pv)
	if pv.DepositProposalCents != 15000 || pv.DamageEstimateCents != 15000 || pv.ReturnTotalCents != 0 {
		t.Fatalf("preview %+v", pv)
	}
	rc.expect(t, 422, "deposit_exceeds", "POST", B+"/check-in/preview", map[string]any{"returned_at": "2026-01-01T00:00:00Z", "deposit_decision": "retain", "retain_cents": 90000}, nil)
	var in api.RentalBooking
	rc.expect(t, 200, "", "POST", B+"/check-in", map[string]any{"returned_at": out.CheckedOutAt, "deposit_decision": "retain", "retain_cents": 15000}, &in, "If-Match", etag(int(out.Version)))
	if in.Status != "closed" || in.Deposit.Status != "partially_retained" || in.Deposit.RetainedCents != 15000 {
		t.Fatalf("checked in %+v", in)
	}
	rc.expect(t, 409, "invalid_transition", "POST", B+"/close", nil, nil, "If-Match", etag(int(in.Version)))

	// Quick cash of the main charge (card POS), then nothing left; a walk-in renter has no pay page.
	rc.expect(t, 422, "customer_required", "POST", B+"/quick-cash", map[string]any{"method": "link"}, nil)
	var qc api.RentalQuickCashResult
	rc.expect(t, 200, "", "POST", B+"/quick-cash", map[string]any{"method": "card_pos"}, &qc)
	if qc.PaymentId == nil || len(qc.FiscalDocumentIds) != 1 || len(qc.Charges) == 0 {
		t.Fatalf("quick cash %+v", qc)
	}
	rc.expect(t, 409, "nothing_to_collect", "POST", B+"/quick-cash", map[string]any{"method": "cash"}, nil)
	var d api.RentalDeposit
	rc.expect(t, 200, "", "GET", B+"/deposit", nil, &d)
	if d.Status != "partially_retained" {
		t.Fatalf("deposit %+v", d)
	}
}

func pngShade(t *testing.T, shade byte) []byte {
	t.Helper()
	b := harborPNG(t)
	// A different trailing chunk keeps the image valid and the bytes distinct.
	return append(append([]byte{}, b...), []byte{0, 0, 0, 0, 't', 'E', 'X', 't', shade, shade, shade, shade}...)
}
