//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"
	"time"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/perfbudget"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// hoWorld is a desk world with the rental fleet of m11-plan §8.2 created through the API: price list NOL-2027
// (half day morning 150.00, FRIGO 5.00 per day, SNORKEL 10.00 per rental, FUEL-P 2.10 per litre, LATE-GOM 60.00 per
// hour), the unit GOM58 (rib, 40 kW so a licence is required, tank 60 l, deposit 500.00), the formula MEZZA-MATTINA
// (half day morning, fuel metered, grace 15, step 30), the extras, the damage item, and an open cash session.
type hoWorld struct {
	*deskWorld
	items            map[string]string
	unit, morning    string
	frigo, snorkel   string
	checklistEntries []map[string]any
}

func newHoWorld(t *testing.T, db *store.DB, fake *fixture.FakeProvider) *hoWorld {
	t.Helper()
	var dw *deskWorld
	if fake != nil {
		dw = newM5World(t, db, ext.Extensions{PaymentProviders: []ext.PaymentProvider{fake}})
	} else {
		dw = newDeskWorld(t, db, 1)
	}
	w := &hoWorld{deskWorld: dw, items: map[string]string{}}
	o, M := w.owner, w.M
	w.setClock(t, time.Date(2027, 6, 12, 6, 0, 0, 0, time.UTC))
	rental := ""
	for _, rc := range jsonList(o.expect(t, 200, "GET", M+"/revenue-classes", nil), "items") {
		if str(rc.(map[string]any), "code") == "rental" {
			rental = str(rc.(map[string]any), "id")
		}
	}
	for code, unit := range map[string]string{"NOL-GOM58": "hour", "FRIGO": "lump", "SNORKEL": "lump", "FUEL-P": "piece", "LATE-GOM": "hour", "DANNI": "lump"} {
		w.items[code] = str(o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": code, "name": map[string]string{"it": code}, "item_type": "service", "unit": unit,
			"revenue_class_id": rental, "price_includes_vat": true,
		}), "id")
	}
	doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
		"code": "NOL-2027", "name": map[string]string{"it": "Noleggio"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{"charter"},
	})
	rule := func(key, code, unit string, cents int, scope map[string]any) map[string]any {
		r := map[string]any{"key": key, "stage": "base", "name": key, "item_id": w.items[code], "basis": "flat", "unit": unit, "amount_cents": cents}
		if scope != nil {
			r["scope"] = scope
		}
		return r
	}
	body := map[string]any{
		"code": "NOL-2027", "name": map[string]string{"it": "Noleggio"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{}, "settings": doc["settings"], "periods": []any{}, "bands": []any{},
		"rules": []map[string]any{
			rule("h", "NOL-GOM58", "hour", 4500, nil),
			rule("m", "NOL-GOM58", "half_day", 15000, map[string]any{"time_bands": []string{"morning"}}),
			rule("f", "FRIGO", "stay", 500, nil), rule("s", "SNORKEL", "stay", 1000, nil),
			rule("p", "FUEL-P", "stay", 210, nil), rule("l", "LATE-GOM", "hour", 6000, nil), rule("d", "DANNI", "stay", 100, nil),
		},
	}
	saved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(doc, "id"), body, "If-Match", etagOf(doc["version"]))
	o.expect(t, 200, "POST", M+"/price-lists/"+str(doc, "id")+"/activate", map[string]any{}, "If-Match",
		etagOf(saved["document"].(map[string]any)["version"]))
	set := o.expect(t, 200, "GET", M+"/rental/settings", nil)
	o.expect(t, 200, "PUT", M+"/rental/settings", map[string]any{
		"open_from_slot": 16, "open_to_slot": 38, "bands": set["bands"], "buffer_slots": 1, "desk_hold_minutes": 60,
		"no_show_after_minutes": 60, "licence_threshold_kw_milli": 30000, "checklist": set["checklist"],
		"checkout_min_photos": 2, "checkin_min_photos": 1, "deposit_modes": []string{"cash", "card_pos", "preauth"},
		"identity_retention_days": 365, "damage_item_id": w.items["DANNI"],
	}, "If-Match", `"0"`)
	for _, it := range set["checklist"].([]any) {
		w.checklistEntries = append(w.checklistEntries, map[string]any{"key": str(it.(map[string]any), "key"), "ok": true})
	}
	v := o.expect(t, 201, "POST", M+"/vessels", map[string]any{
		"name": "Gabbiano", "loa_cm": 580, "beam_cm": 240, "hull_type": "rib", "engines": []map[string]any{{"power_kw": 40}},
	})
	w.unit = str(o.expect(t, 201, "POST", M+"/rental/units", map[string]any{
		"vessel_id": str(v, "id"), "code": "GOM58", "category": "rib", "seats": 8, "fuel_type": "petrol", "tank_milli": 60000,
		"deposit_cents": 50000, "item_id": w.items["NOL-GOM58"],
	}), "id")
	w.morning = str(o.expect(t, 201, "POST", M+"/rental/formulas", map[string]any{
		"code": "MEZZA-MATTINA", "name": map[string]string{"it": "Mezza giornata mattina"}, "kind": "half_day", "band_code": "morning",
		"fuel": "metered", "fuel_item_id": w.items["FUEL-P"], "late_item_id": w.items["LATE-GOM"], "grace_minutes": 15, "late_step_minutes": 30,
	}), "id")
	w.frigo = str(o.expect(t, 201, "POST", M+"/rental/extras", map[string]any{"item_id": w.items["FRIGO"], "charge_mode": "per_day"}), "id")
	w.snorkel = str(o.expect(t, 201, "POST", M+"/rental/extras", map[string]any{"item_id": w.items["SNORKEL"], "charge_mode": "per_rental"}), "id")
	o.expect(t, 201, "POST", M+"/cash-sessions", map[string]any{"float_cents": 0})
	return w
}

// at sets the clock to a Rome wall-clock time on 2027-06-12 (UTC+2).
func (w *hoWorld) at(t *testing.T, h, m int) {
	t.Helper()
	w.setClock(t, time.Date(2027, 6, 12, h-2, m, 0, 0, time.UTC))
	w.owner.login(t, "owner@porto.test")
}

func hoPNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := range 16 {
		for y := range 16 {
			img.Set(x, y, color.RGBA{R: shade, G: 80, B: 40, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// timed sends a request, checks the status and measures that it answers within a second (criterion 2, server
// side; the budget fails the test only with PORTO_PERF_BUDGET=1, perfbudget).
func (w *hoWorld) timed(t *testing.T, want int, method, path string, body any, headers ...string) map[string]any {
	t.Helper()
	start := time.Now() //nolint:forbidigo // wall time of the request, not domain time
	out := w.owner.expect(t, want, method, path, body, headers...)
	perfbudget.Check(t, method+" "+path, time.Since(start), time.Second) //nolint:forbidigo // wall time
	return out
}

func (w *hoWorld) photo(t *testing.T, path, kind string, shade uint8) {
	t.Helper()
	r, err := w.owner.send("POST", path+"?kind="+kind, hoPNG(t, shade), "image/png")
	if err != nil || r.status != http.StatusCreated {
		t.Fatalf("photo %s: %v %d %s", kind, err, r.status, r.body)
	}
}

func (w *hoWorld) sum(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var v int64
	if err := w.h.app.DB.NewRaw(q, args...).Scan(context.Background(), &v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

// TestAcceptance_M11_CheckOutCheckIn (spec 32 M11 criterion 2, server side): the worked example of m11-plan §8.2
// through the API with its exact numbers, at most 6 write requests for the check-out and 5 for the check-in
// (photo uploads aside), each answered within a second on SQLite.
func TestAcceptance_M11_CheckOutCheckIn(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newHoWorld(t, db, nil)
		M := w.M
		b := w.owner.expect(t, 201, "POST", M+"/rental/bookings", map[string]any{
			"unit_id": w.unit, "formula_id": w.morning, "start": bkPoint("2027-06-12", 16), "end": bkPoint("2027-06-12", 26),
			"renter": map[string]string{"name": "Ada Rossi"},
			"extras": []map[string]any{{"extra_id": w.frigo, "qty_milli": 1000}, {"extra_id": w.snorkel, "qty_milli": 1000}},
		})
		if n, _ := num(b, "total_cents"); n != 16500 || !b["licence_required"].(bool) {
			t.Fatalf("booking %v", b)
		}
		B := M + "/rental/bookings/" + str(b, "id")

		// Check-out at 08:35: 6 writes (identity, contract, deposit, checklist, check-out, quick cash) + 4 photos.
		w.at(t, 8, 35)
		writes := 0
		write := func(want int, method, path string, body any, headers ...string) map[string]any {
			writes++
			return w.timed(t, want, method, path, body, headers...)
		}
		write(200, "PUT", B+"/identity", map[string]any{
			"document_type": "identity_card", "document_number": "CA12345XY", "document_expires_on": "2030-01-01",
			"licence_number": "LT-998877", "licence_expires_on": "2031-05-01",
		})
		for i, kind := range []string{"id_document", "licence", "boat", "boat"} {
			w.photo(t, B+"/handovers/checkout/photos", kind, uint8(10+i))
		}
		sig := write(200, "POST", B+"/contract/sign", map[string]any{"driver": "screen", "signature_png": base64.StdEncoding.EncodeToString(hoPNG(t, 200))})
		if str(sig, "status") != "signed" {
			t.Fatalf("signature %v", sig)
		}
		write(200, "POST", B+"/deposit", map[string]any{"mode": "cash"})
		h := write(200, "PUT", B+"/handovers/checkout/checklist", map[string]any{
			"client_ref": "co-0000001", "items": w.checklistEntries, "fuel_eighths": 8, "engine_hours_milli": 120500,
		})
		if len(h["missing"].([]any)) != 0 {
			t.Fatalf("missing %v", h["missing"])
		}
		cur := w.owner.expect(t, 200, "GET", B, nil)
		co := write(200, "POST", B+"/check-out", map[string]any{}, "If-Match", etagOf(cur["version"]))
		if str(co, "status") != "checked_out" {
			t.Fatalf("check-out %v", co)
		}
		qc := write(200, "POST", B+"/quick-cash", map[string]any{"target": "main", "method": "cash"})
		if len(qc["fiscal_document_ids"].([]any)) != 1 {
			t.Fatalf("quick cash %v", qc)
		}
		if writes > 6 {
			t.Fatalf("check-out took %d writes", writes)
		}

		// Check-in at 13:50: checklist, damage, check-in, quick cash (4 writes) + 1 photo.
		w.at(t, 13, 50)
		writes = 0
		write(200, "PUT", B+"/handovers/checkin/checklist", map[string]any{
			"client_ref": "ci-0000001", "items": w.checklistEntries, "fuel_eighths": 5, "engine_hours_milli": 125000,
		})
		w.photo(t, B+"/handovers/checkin/photos", "boat", 99)
		write(201, "POST", B+"/damages", map[string]any{"description": "Graffio sul tubolare", "estimate_cents": 18000})
		returned := time.Date(2027, 6, 12, 11, 50, 0, 0, time.UTC).Format(time.RFC3339)
		pv := w.owner.expect(t, 200, "POST", B+"/check-in/preview", map[string]any{"returned_at": returned, "deposit_decision": "retain", "retain_cents": 18000})
		for k, want := range map[string]int64{"late_fee_cents": 6000, "fuel_cents": 4725, "return_total_cents": 10725, "deposit_proposal_cents": 18000, "late_minutes": 35} {
			if got, _ := num(pv, k); got != want {
				t.Fatalf("preview %s = %d, want %d (%v)", k, got, want, pv)
			}
		}
		cur = w.owner.expect(t, 200, "GET", B, nil)
		ci := write(200, "POST", B+"/check-in", map[string]any{"returned_at": returned, "deposit_decision": "retain", "retain_cents": 18000},
			"If-Match", etagOf(cur["version"]))
		if str(ci, "status") != "closed" {
			t.Fatalf("check-in %v", ci)
		}
		ret := write(200, "POST", B+"/quick-cash", map[string]any{"target": "return", "method": "card_pos"})
		if len(ret["fiscal_document_ids"].([]any)) != 1 {
			t.Fatalf("return quick cash %v", ret)
		}
		if writes > 5 {
			t.Fatalf("check-in took %d writes", writes)
		}

		id := str(b, "id")
		if got := w.sum(t, "SELECT COALESCE(SUM(total_cents),0) FROM charges WHERE origin_type = 'rental' AND origin_id = ?", id); got != 27225 {
			t.Fatalf("rental charges %d, want 272.25", got)
		}
		if got := w.sum(t, "SELECT allocated_cents FROM payments WHERE kind = 'deposit' AND marina_id = ?", w.marina); got != 18000 {
			t.Fatalf("deposit allocated %d", got)
		}
		if got := w.sum(t, "SELECT refunded_cents FROM payments WHERE kind = 'deposit' AND marina_id = ?", w.marina); got != 32000 {
			t.Fatalf("deposit refunded %d", got)
		}
		cash := w.sum(t, "SELECT COALESCE(SUM(amount_cents),0) FROM payments WHERE method = 'cash' AND marina_id = ?", w.marina) -
			w.sum(t, "SELECT COALESCE(SUM(amount_cents),0) FROM refunds WHERE method = 'cash' AND marina_id = ?", w.marina)
		card := w.sum(t, "SELECT COALESCE(SUM(amount_cents),0) FROM payments WHERE method = 'card_pos' AND marina_id = ?", w.marina)
		if cash != 34500 || card != 10725 {
			t.Fatalf("cash %d card %d, want 345.00 and 107.25", cash, card)
		}
		if got := w.sum(t, "SELECT COALESCE(SUM(total_cents),0) FROM fiscal_documents WHERE marina_id = ?", w.marina); got != 45225 {
			t.Fatalf("corrispettivi %d, want 452.25", got)
		}
		if got := w.sum(t, "SELECT engine_hours_milli FROM rental_units WHERE id = ?", w.unit); got != 125000 {
			t.Fatalf("engine hours %d", got)
		}
		// The return (13:50) is after the planned end: the rows stay as history (16-25 booked, 26 buffer).
		if got := w.sum(t, "SELECT COUNT(*) FROM rental_slots WHERE booking_id = ?", id); got != 11 {
			t.Fatalf("rows %d, want 11", got)
		}
		vs, err := store.CheckBillingInvariants(context.Background(), db.DB, mustScope(t, db, w.marina))
		if err != nil || len(vs) != 0 {
			t.Fatalf("billing invariants %v %v", vs, err)
		}
		// Q32, Q33 and B60 (INV-19, INV-98, INV-99).
		if rv, err := store.CheckRentalInvariants(context.Background(), db.DB, w.marina); err != nil || len(rv) != 0 {
			t.Fatalf("rental invariants %v %v", rv, err)
		}
		// Both handover PDFs are served (signed links behind a redirect).
		for _, ph := range []string{"checkout", "checkin"} {
			r := w.owner.do(t, "GET", B+"/handovers/"+ph+"/pdf", nil)
			if r.status != 200 || !bytes.HasPrefix(r.body, []byte("%PDF-")) {
				t.Fatalf("%s pdf: %d %s", ph, r.status, fmt.Sprint(len(r.body)))
			}
		}
	})
}

func mustScope(t *testing.T, db *store.DB, marinaID string) store.MarinaScope {
	t.Helper()
	s, err := store.SystemMarinaScope(context.Background(), db.DB, marinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// checkOutReady fills every check-out step but the deposit (identity with licence, 4 photos, screen signature,
// checklist) and returns the booking path.
func (w *hoWorld) checkOutReady(t *testing.T, path string) {
	t.Helper()
	w.owner.expect(t, 200, "PUT", path+"/identity", map[string]any{
		"document_type": "passport", "document_number": "YA0000001", "document_expires_on": "2030-01-01",
		"licence_number": "LT-1", "licence_expires_on": "2031-05-01",
	})
	for i, kind := range []string{"id_document", "licence", "boat", "boat"} {
		w.photo(t, path+"/handovers/checkout/photos", kind, uint8(40+i))
	}
	w.owner.expect(t, 200, "POST", path+"/contract/sign", map[string]any{"driver": "screen", "signature_png": base64.StdEncoding.EncodeToString(hoPNG(t, 201))})
	w.owner.expect(t, 200, "PUT", path+"/handovers/checkout/checklist", map[string]any{
		"client_ref": "co-ready-001", "items": w.checklistEntries, "fuel_eighths": 8, "engine_hours_milli": 120500,
	})
}

// TestAcceptance_M11_Preauth (fake provider): the deposit pre-authorised through the provider page, the authorized
// webhook, check-out, a check-in retaining part of it (captured after commit), the succeeded webhook recording and
// retaining the captured amount, and the booking closed. A succeeded event without an amount retains the decided
// 120.00, never the authorised 500.00 (INV-98).
func TestAcceptance_M11_Preauth(t *testing.T) {
	for _, captured := range []int64{12000, 0} {
		t.Run(fmt.Sprintf("event_amount_%d", captured), func(t *testing.T) { m11Preauth(t, captured) })
	}
}

func m11Preauth(t *testing.T, captured int64) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		fake := fixture.NewFakeProvider()
		w := newHoWorld(t, db, fake)
		M := w.M
		set := w.owner.expect(t, 200, "GET", M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", nil)
		w.owner.expect(t, 200, "PUT", M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", map[string]any{
			"enabled": true, "mode": "test", "methods": []string{"card"}, "secret_key": "sk_test_x", "webhook_secret": m5WebhookSecret,
			"sepa_enabled": false,
		}, "If-Match", etagOf(set["version"]))
		w.at(t, 8, 0)
		b := w.owner.expect(t, 201, "POST", M+"/rental/bookings", map[string]any{
			"unit_id": w.unit, "formula_id": w.morning, "start": bkPoint("2027-06-12", 16), "end": bkPoint("2027-06-12", 26),
			"customer_id": w.customer(t, "Verdi"),
		})
		B := M + "/rental/bookings/" + str(b, "id")
		dep := w.owner.expect(t, 200, "POST", B+"/deposit", map[string]any{"mode": "preauth"})
		if str(dep, "status") != "pending" || str(dep, "checkout_url") == "" || !bytes.HasPrefix([]byte(str(dep, "checkout_qr_svg")), []byte("<svg")) {
			t.Fatalf("deposit %v", dep)
		}
		intent := str(dep, "intent_id")
		meta := map[string]string{"marina_id": w.marina, "intent_id": intent}
		if code := w.postWebhook(t, ext.PaymentEvent{EventID: "ev-a", Kind: ext.PaymentEventAuthorized, PaymentRef: "pi_r1", AmountCents: 50000, Metadata: meta}, w.clock.Now()); code != 204 {
			t.Fatalf("authorized webhook %d", code)
		}
		if d := w.owner.expect(t, 200, "GET", B+"/deposit", nil); str(d, "status") != "authorized" {
			t.Fatalf("after authorization %v", d)
		}
		w.checkOutReady(t, B)
		cur := w.owner.expect(t, 200, "GET", B, nil)
		w.owner.expect(t, 200, "POST", B+"/check-out", map[string]any{}, "If-Match", etagOf(cur["version"]))
		w.at(t, 12, 55)
		w.owner.expect(t, 200, "PUT", B+"/handovers/checkin/checklist", map[string]any{
			"client_ref": "ci-pre-0001", "items": w.checklistEntries, "fuel_eighths": 8, "engine_hours_milli": 121000,
		})
		w.photo(t, B+"/handovers/checkin/photos", "boat", 77)
		w.owner.expect(t, 201, "POST", B+"/damages", map[string]any{"description": "Elica", "estimate_cents": 12000})
		cur = w.owner.expect(t, 200, "GET", B, nil)
		ci := w.owner.expect(t, 200, "POST", B+"/check-in", map[string]any{
			"returned_at": w.clock.Now().UTC().Format(time.RFC3339), "deposit_decision": "retain", "retain_cents": 12000,
		}, "If-Match", etagOf(cur["version"]))
		if str(ci, "status") != "returned" {
			t.Fatalf("check-in %v", ci)
		}
		if len(fake.Captures) != 1 || fake.Captures[0].AmountCents != 12000 {
			t.Fatalf("captures %+v", fake.Captures)
		}
		for _, id := range []string{"ev-c", "ev-c2"} {
			if code := w.postWebhook(t, ext.PaymentEvent{EventID: id, Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_r1", AmountCents: captured, Method: "card_online", Metadata: meta}, w.clock.Now()); code != 204 {
				t.Fatalf("succeeded webhook %d", code)
			}
		}
		done := w.owner.expect(t, 200, "GET", B, nil)
		if str(done, "status") != "closed" {
			t.Fatalf("not closed %v", done)
		}
		if got := w.sum(t, "SELECT COALESCE(SUM(total_cents),0) FROM charges WHERE origin_type = 'deposit_retention' AND origin_id = ?", str(b, "id")); got != 12000 {
			t.Fatalf("retention %d", got)
		}
		if got := w.sum(t, "SELECT COUNT(*) FROM payments WHERE kind = 'deposit' AND marina_id = ?", w.marina); got != 1 {
			t.Fatalf("deposit payments %d", got)
		}
	})
}
