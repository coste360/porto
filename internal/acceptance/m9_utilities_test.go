//go:build integration

package acceptance

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M9 utilities (m9-plan §9, §14.4): meters, readings across an occupant change, an anomaly confirmed by the
// office, the charging run per contract through the M5 charges, export, and a late reading confirmed inside the
// charged interval becoming linked adjustment charges (INV-51); invariants INV-90/91 hold throughout.

// newUtilityWorld is a desk world whose 2027 list prices transits (3 € per metre and night) and the UTL-KWH item
// (0.30 € per kWh, metered), with a power meter on berth A1.
func newUtilityWorld(t *testing.T, db *store.DB) (*deskWorld, string) {
	t.Helper()
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	h := newRegistryHarness(t, db, c)
	own, err := h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
		Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Utenze SRL", MarinaName: "Marina Utenze", MarinaSlug: "utenze",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &deskWorld{h: h, clock: c, marina: own.MarinaID, M: A + "/marinas/" + own.MarinaID, owner: h.cookieClient(), clients: map[string]*client{}}
	w.owner.login(t, "owner@porto.test")
	w.harbour = contracttest.InsertHarbour(t, db, own.MarinaID, store.UTC(c.Now()), 4)
	w.clients["dock"] = w.member(t, "dock")
	o, M := w.owner, w.M
	classes := o.expect(t, 200, "GET", M+"/revenue-classes", nil)
	classID := map[string]string{}
	for _, rc := range jsonList(classes, "items") {
		classID[str(rc.(map[string]any), "code")] = str(rc.(map[string]any), "id")
	}
	trn := o.expect(t, 201, "POST", M+"/items", map[string]any{
		"code": "TRN-NTT", "name": map[string]string{"it": "Passaggio"}, "item_type": "berth_fee", "unit": "night",
		"revenue_class_id": classID["transit"], "price_includes_vat": true,
	})
	kwh := o.expect(t, 201, "POST", M+"/items", map[string]any{
		"code": "UTL-KWH", "name": map[string]string{"it": "Energia"}, "item_type": "utility", "unit": "kwh",
		"revenue_class_id": classID["utilities"], "price_includes_vat": true,
	})
	doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
		"code": "L2027", "name": map[string]string{"it": "L2027"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{"transit"},
	})
	body := map[string]any{
		"code": "L2027", "name": map[string]string{"it": "L2027"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{"transit"}, "periods": []any{}, "bands": []any{},
		"settings": map[string]any{
			"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"},
			"min_beam_ratio": map[string]int{"num": 1, "den": 3}, "primary_items": map[string]string{"transit": str(trn, "id")},
		},
		"rules": []map[string]any{
			{"key": "r1", "stage": "base", "name": "Transito", "item_id": str(trn, "id"), "basis": "per_meter", "unit": "night", "unit_cents": 300, "scope": map[string]any{"contract_types": []string{"transit"}}},
			{"key": "r2", "stage": "base", "name": "Energia", "item_id": str(kwh, "id"), "basis": "metered", "unit": "stay", "unit_cents": 30},
		},
	}
	saved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(doc, "id"), body, "If-Match", etagOf(doc["version"]))
	sd := saved["document"].(map[string]any)
	o.expect(t, 200, "POST", M+"/price-lists/"+str(doc, "id")+"/activate", map[string]any{}, "If-Match", etagOf(sd["version"]))
	set := o.expect(t, 200, "GET", M+"/utility-settings", nil)
	o.expect(t, 200, "PUT", M+"/utility-settings", map[string]any{"mode": "with_installment", "power_item_id": str(kwh, "id")}, "If-Match", etagOf(set["version"]))
	m := o.expect(t, 201, "POST", M+"/utility-meters", map[string]any{
		"code": "A1-E", "kind": "power", "unit": "kwh", "resource_type": "berth", "resource_id": w.harbour.Berths[0],
	})
	return w, str(m, "id")
}

func (w *deskWorld) reading(t *testing.T, c *client, meterID, value, at string) map[string]any {
	t.Helper()
	return c.expect(t, 201, "POST", w.M+"/meter-readings", map[string]any{"meter_id": meterID, "value": value, "read_at": at})
}

func (w *deskWorld) utilityInvariants(t *testing.T) {
	t.Helper()
	vs, err := store.CheckUtilityInvariants(context.Background(), w.h.app.DB.DB, w.marina)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("invariant %s: %v %s", v.Query, v.IDs, v.Detail)
	}
}

func TestAcceptance_M9_Utilities(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w, meter := newUtilityWorld(t, db)
		o, M, dock := w.owner, w.M, w.clients["dock"]
		a := w.vessel(t, "Libeccio", 1134, 380)
		b := w.vessel(t, "Maestrale", 1000, 350)
		cust := w.customer(t, "Rossi")
		berth := w.harbour.Berths[0]
		ta := o.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": a, "customer_id": cust, "arrival": "2027-06-02", "departure": "2027-06-05",
			"placements": []any{berthPlacement(berth, "2027-06-02", "2027-06-04")}, "confirm": map[string]any{},
		})
		tb := o.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": b, "customer_id": cust, "arrival": "2027-06-05", "departure": "2027-06-08",
			"placements": []any{berthPlacement(berth, "2027-06-05", "2027-06-07")}, "confirm": map[string]any{},
		})
		w.setClock(t, time.Date(2027, 6, 8, 10, 0, 0, 0, time.UTC))
		w.reading(t, dock, meter, "1000", "2027-06-02T07:30:00Z")
		// A typo is flagged, never refused; the office voids it.
		bad := w.reading(t, dock, meter, "100", "2027-06-04T07:30:00Z")
		if str(bad, "status") != "anomaly" {
			t.Fatalf("typo %v", bad)
		}
		o.expect(t, 200, "POST", M+"/meter-readings/"+str(bad, "id")+"/void", map[string]any{"reason": "refuso"}, "If-Match", etagOf(bad["version"]))
		w.reading(t, dock, meter, "3000", "2027-06-08T07:30:00Z")
		cons := o.expect(t, 200, "GET", M+"/utility-consumptions?status=pending", nil)
		if n := len(jsonList(cons, "items")); n != 2 {
			t.Fatalf("%d pending consumptions: %v", n, cons)
		}
		// Both transits are over on 2027-06-08: one charge each, 1000 kWh × 0.30 €.
		run := o.expect(t, 200, "POST", M+"/utility-charges/run", nil)
		if n := len(jsonList(run, "charges")); n != 2 {
			t.Fatalf("run %v", run)
		}
		for _, tr := range []map[string]any{ta, tb} {
			chs := jsonList(o.expect(t, 200, "GET", M+"/charges?contract_id="+str(tr, "id")+"&origin_type=utility", nil), "items")
			found := false
			for _, ch := range chs {
				m := ch.(map[string]any)
				if str(m, "origin_type") == "utility" {
					found = true
					if int64(m["total_cents"].(float64)) != 30000 {
						t.Fatalf("utility charge %v", m)
					}
				}
			}
			if !found {
				t.Fatalf("contract %s without utility charge: %v", str(tr, "id"), chs)
			}
		}
		// A late reading at the occupant change, confirmed, reallocates the charged interval as adjustments.
		late := w.reading(t, dock, meter, "2800", "2027-06-05T07:00:00Z")
		if str(late, "status") != "anomaly" {
			t.Fatalf("late reading %v", late)
		}
		o.expect(t, 200, "POST", M+"/meter-readings/"+str(late, "id")+"/confirm", nil, "If-Match", etagOf(late["version"]))
		run = o.expect(t, 200, "POST", M+"/utility-charges/run?contract_id="+str(ta, "id"), nil)
		chs := jsonList(run, "charges")
		if len(chs) != 1 || chs[0].(map[string]any)["adjustment"] != true {
			t.Fatalf("adjustment run %v", run)
		}
		o.expect(t, 200, "POST", M+"/utility-charges/run?contract_id="+str(tb, "id"), nil)
		w.utilityInvariants(t)
	})
}
