//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// enableFake turns the fake checkout provider on for the world (payment links and pre-authorisations).
func (w *hoWorld) enableFake(t *testing.T) {
	t.Helper()
	M := w.M
	set := w.owner.expect(t, 200, "GET", M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", nil)
	w.owner.expect(t, 200, "PUT", M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", map[string]any{
		"enabled": true, "mode": "test", "methods": []string{"card"}, "secret_key": "sk_test_x", "webhook_secret": m5WebhookSecret,
		"sepa_enabled": false,
	}, "If-Match", etagOf(set["version"]))
}

// TestAcceptance_M11_InvoiceSwitch (m11-plan §9.1, spec 19.5/16.5): a confirmed booking with an unpaid main charge
// (quick-cash payment link) switched to invoice_requested gets its main charge re-created in invoice mode, which
// the charge export picks up; once the main charge is paid, the switch is refused (validation invoice_requested charge_paid), so a
// booking never mixes receipt and invoice charges.
func TestAcceptance_M11_InvoiceSwitch(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		fake := fixture.NewFakeProvider()
		w := newHoWorld(t, db, fake)
		M := w.M
		w.enableFake(t)
		w.at(t, 8, 0)
		cust := w.customer(t, "Bianchi")
		book := func(day string) (string, string) {
			b := w.owner.expect(t, 201, "POST", M+"/rental/bookings", map[string]any{
				"unit_id": w.unit, "formula_id": w.morning, "start": bkPoint(day, 16), "end": bkPoint(day, 26),
				"customer_id": cust,
			})
			return str(b, "id"), M + "/rental/bookings/" + str(b, "id")
		}
		id, B := book("2027-06-12")
		link := w.owner.expect(t, 200, "POST", B+"/quick-cash", map[string]any{"target": "main", "method": "link"})
		if str(link, "link_url") == "" || !strings.HasPrefix(str(link, "link_qr_svg"), "<svg") {
			t.Fatalf("link %v", link)
		}
		cur := w.owner.expect(t, 200, "GET", B, nil)
		w.owner.expect(t, 200, "PATCH", B, map[string]any{"invoice_requested": true}, "If-Match", etagOf(cur["version"]))
		var modes []string
		if err := db.NewSelect().TableExpr("charges").Column("fiscal_mode").Where("origin_type = 'rental'").Where("origin_id = ?", id).
			Where("payment_status <> 'cancelled'").Scan(context.Background(), &modes); err != nil {
			t.Fatal(err)
		}
		if len(modes) != 1 || modes[0] != "invoice" {
			t.Fatalf("live main charge modes %v, want [invoice]", modes)
		}
		exp := w.owner.expect(t, 201, "POST", M+"/charge-exports", map[string]any{"period_from": "2027-06-01", "period_to": "2027-06-30"})
		if n, _ := num(exp, "charge_count"); n != 1 {
			t.Fatalf("export %v, want the invoice-mode main charge", exp)
		}
		if got := w.sum(t, "SELECT COUNT(*) FROM charges WHERE origin_type = 'rental' AND origin_id = ? AND invoicing_status = 'exported'", id); got != 1 {
			t.Fatalf("exported rental charges %d", got)
		}

		// Paid main charge (receipt): the switch is refused.
		_, B2 := book("2027-06-13")
		w.owner.expect(t, 200, "POST", B2+"/quick-cash", map[string]any{"target": "main", "method": "cash"})
		cur = w.owner.expect(t, 200, "GET", B2, nil)
		r := w.owner.do(t, "PATCH", B2, map[string]any{"invoice_requested": true}, "If-Match", etagOf(cur["version"]))
		if r.status != http.StatusBadRequest || !strings.Contains(string(r.body), `"code":"charge_paid"`) {
			t.Fatalf("switch after payment: %d %s", r.status, r.body)
		}
		if vs, err := store.CheckRentalInvariants(context.Background(), db.DB, w.marina); err != nil || len(vs) != 0 {
			t.Fatalf("rental invariants %v %v", vs, err)
		}
	})
}

// TestAcceptance_M11_OfflineStaleReplay (INV-83): a queued checklist save replayed after a newer save never
// overwrites it; the answer is the current handover.
func TestAcceptance_M11_OfflineStaleReplay(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newHoWorld(t, db, nil)
		M := w.M
		w.at(t, 8, 0)
		older := w.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
		b := w.owner.expect(t, 201, "POST", M+"/rental/bookings", map[string]any{
			"unit_id": w.unit, "formula_id": w.morning, "start": bkPoint("2027-06-12", 16), "end": bkPoint("2027-06-12", 26),
			"renter": map[string]string{"name": "Ada Rossi"},
		})
		B := M + "/rental/bookings/" + str(b, "id")
		w.at(t, 8, 10)
		// The newer save (online, another device) lands first.
		w.owner.expect(t, 200, "PUT", B+"/handovers/checkout/checklist", map[string]any{
			"client_ref": "online-new-0001", "items": w.checklistEntries, "fuel_eighths": 8, "engine_hours_milli": 120500,
		})
		raw, _ := json.Marshal(map[string]any{
			"client_ref": "off-stale-0001", "items": w.checklistEntries, "fuel_eighths": 3, "engine_hours_milli": 100, "device_at": older,
		})
		r, err := w.owner.replayUntilDone(http.MethodPut, B+"/handovers/checkout/checklist", raw, "application/json",
			"Idempotency-Key", "off-key-stale-0001", offlineHeader, older)
		if err != nil || r.status != http.StatusOK {
			t.Fatalf("stale replay %d %v", r.status, err)
		}
		if got := w.sum(t, "SELECT fuel_eighths FROM rental_handovers WHERE booking_id = ? AND phase = 'checkout'", str(b, "id")); got != 8 {
			t.Fatalf("fuel %d, the stale replay overwrote the newer save", got)
		}
		if got := w.sum(t, "SELECT engine_hours_milli FROM rental_handovers WHERE booking_id = ? AND phase = 'checkout'", str(b, "id")); got != 120500 {
			t.Fatalf("engine hours %d", got)
		}
	})
}
