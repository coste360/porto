//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M3 desk flow (m3-plan §9.4): search, held transit, manual confirmation, day view, arrival, annual contract
// with a parking, release and resale, revoke on resold nights, move and undo, layout exchange on a linear area,
// quotes with holds (accept, expiry), roles and isolation, events without personal data, invariants. The
// harbour (20 berths, a 60 m linear area, parkings) is inserted through the store; everything else goes
// through the API.

type deskWorld struct {
	h       *harness
	clock   *clock.Fake
	marina  string
	M       string
	owner   *client
	clients map[string]*client
	harbour contracttest.Harbour
}

func (w *deskWorld) member(t *testing.T, role string) *client {
	t.Helper()
	email := role + "@porto.test"
	mem := w.owner.expect(t, 201, "POST", w.M+"/members", map[string]any{"email": email, "name": role, "role": role})
	if err := w.h.app.Auth.SetPassword(context.Background(), str(mem, "user_id"), password); err != nil {
		t.Fatal(err)
	}
	c := w.h.cookieClient()
	c.login(t, email)
	return c
}

// setClock moves the clock and logs every client in again (sessions expire when idle).
func (w *deskWorld) setClock(t *testing.T, at time.Time) {
	t.Helper()
	w.clock.Set(at)
	w.owner.login(t, "owner@porto.test")
	for role, c := range w.clients {
		c.login(t, role+"@porto.test")
	}
}

func newDeskWorld(t *testing.T, db *store.DB, berths int) *deskWorld {
	t.Helper()
	c := clock.NewFake(time.Date(2027, 6, 1, 7, 0, 0, 0, time.UTC))
	h := newRegistryHarness(t, db, c)
	own, err := h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
		Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Banchina SRL", MarinaName: "Marina Banchina", MarinaSlug: "banchina",
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &deskWorld{h: h, clock: c, marina: own.MarinaID, M: A + "/marinas/" + own.MarinaID, owner: h.cookieClient(), clients: map[string]*client{}}
	w.owner.login(t, "owner@porto.test")
	w.harbour = contracttest.InsertHarbour(t, db, own.MarinaID, store.UTC(c.Now()), berths)
	for _, role := range []string{"office", "dock", "yard", "viewer"} {
		w.clients[role] = w.member(t, role)
	}
	w.prices(t, false)
	return w
}

// prices creates the 2027 list through the API: transits 3 € per metre and night, annual 4 800 € flat and, with
// allTerms, seasonal 3 000 € (window May–September) and monthly 600 €.
func (w *deskWorld) prices(t *testing.T, allTerms bool) {
	t.Helper()
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
	orm := o.expect(t, 201, "POST", M+"/items", map[string]any{
		"code": "ORM-ANN", "name": map[string]string{"it": "Annuale"}, "item_type": "berth_fee", "unit": "year",
		"revenue_class_id": classID["mooring_annual"], "price_includes_vat": true,
	})
	types := []string{"transit", "annual"}
	primary := map[string]string{"transit": str(trn, "id"), "annual": str(orm, "id")}
	rules := []map[string]any{
		{"key": "r1", "stage": "base", "name": "Transito", "item_id": str(trn, "id"), "basis": "per_meter", "unit": "night", "unit_cents": 300, "scope": map[string]any{"contract_types": []string{"transit"}}},
		{"key": "r2", "stage": "base", "name": "Annuale", "item_id": str(orm, "id"), "basis": "flat", "unit": "year", "amount_cents": 480000, "scope": map[string]any{"contract_types": []string{"annual"}}},
	}
	settings := map[string]any{
		"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"}, "min_beam_ratio": map[string]int{"num": 1, "den": 3},
		"primary_items": primary,
	}
	if allTerms {
		types = append(types, "seasonal", "monthly")
		primary["seasonal"], primary["monthly"] = str(orm, "id"), str(orm, "id")
		settings["season_window"] = map[string]string{"from": "2027-05-01", "to": "2027-09-30"}
		rules = append(rules,
			map[string]any{"key": "r3", "stage": "base", "name": "Stagionale", "item_id": str(orm, "id"), "basis": "flat", "unit": "season", "amount_cents": 300000, "scope": map[string]any{"contract_types": []string{"seasonal"}}},
			map[string]any{"key": "r4", "stage": "base", "name": "Mensile", "item_id": str(orm, "id"), "basis": "flat", "unit": "month", "amount_cents": 60000, "scope": map[string]any{"contract_types": []string{"monthly"}}},
		)
	}
	doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
		"code": "L2027", "name": map[string]string{"it": "L2027"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": types,
	})
	body := map[string]any{
		"code": "L2027", "name": map[string]string{"it": "L2027"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": types, "settings": settings, "periods": []any{}, "bands": []any{}, "rules": rules,
	}
	saved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(doc, "id"), body, "If-Match", etagOf(doc["version"]))
	sd := saved["document"].(map[string]any)
	o.expect(t, 200, "POST", M+"/price-lists/"+str(doc, "id")+"/activate", map[string]any{}, "If-Match", etagOf(sd["version"]))
}

func (w *deskWorld) vessel(t *testing.T, name string, loa, beam int) string {
	t.Helper()
	v := w.owner.expect(t, 201, "POST", w.M+"/vessels", map[string]any{"name": name, "loa_cm": loa, "beam_cm": beam, "hull_type": "monohull_sail"})
	return str(v, "id")
}

func (w *deskWorld) customer(t *testing.T, last string) string {
	t.Helper()
	r := w.owner.expect(t, 201, "POST", w.M+"/customers", map[string]any{"kind": "person", "first_name": "Mario", "last_name": last})
	return str(r["customer"].(map[string]any), "id")
}

func berthPlacement(id, first, last string) map[string]any {
	return map[string]any{"resource_type": "berth", "resource_id": id, "first_night": first, "last_night": last}
}

func firstAllocation(t *testing.T, c map[string]any) map[string]any {
	t.Helper()
	for _, a := range jsonList(c, "allocations") {
		am := a.(map[string]any)
		if str(am, "status") == "active" {
			return am
		}
	}
	t.Fatalf("no active allocation in %v", c)
	return nil
}

func TestAcceptance_M3_DeskFlow(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 20)
		o, M := w.owner, w.M
		dock, office, yard, viewer := w.clients["dock"], w.clients["office"], w.clients["yard"], w.clients["viewer"]
		b := w.harbour.Berths
		libeccio := w.vessel(t, "Libeccio", 1134, 380)
		rossi := w.customer(t, "Rossi")

		// 1. Dock: search a transit of 3 nights for the 11.34 m vessel → solutions with prices.
		sr := dock.expect(t, 200, "POST", M+"/occupancies/search", map[string]any{
			"purpose": "transit", "vessel_id": libeccio, "arrival": "2027-06-05", "departure": "2027-06-08", "customer_id": rossi,
			"resource_types": []string{"berth", "dynamic_area"},
		})
		sols := jsonList(sr, "solutions")
		if len(sols) == 0 {
			t.Fatalf("no solutions: %v", sr)
		}
		first := sols[0].(map[string]any)
		if first["pricing"] == nil {
			t.Fatalf("solution without price: %v", first)
		}
		var placements []any
		for _, sg := range jsonList(first, "segments") {
			s := sg.(map[string]any)
			p := map[string]any{"resource_type": s["resource_type"], "resource_id": s["resource_id"], "first_night": s["first_night"], "last_night": s["last_night"]}
			if s["x_cm"] != nil {
				p["x_cm"] = s["x_cm"]
			}
			placements = append(placements, p)
		}
		// 2. Held transit on the first solution, confirmed with the manual driver.
		tr := dock.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": libeccio, "customer_id": rossi, "arrival": "2027-06-05", "departure": "2027-06-08", "placements": placements,
		}, "Idempotency-Key", "acc-m3-transit-0001")
		if str(tr, "status") != "held" {
			t.Fatalf("transit %v", tr)
		}
		T := M + "/transits/" + str(tr, "id")
		tr = dock.expect(t, 200, "POST", T+"/confirm", map[string]any{"reference": "POS 12"}, "If-Match", etagOf(tr["version"]))
		if str(tr, "status") != "confirmed" {
			t.Fatalf("confirm %v", tr)
		}
		trAlloc := firstAllocation(t, tr)
		day := dock.expect(t, 200, "GET", M+"/maps/"+w.harbour.MapID+"/day?date=2027-06-05", nil)
		found := false
		for _, el := range jsonList(day, "elements") {
			em := el.(map[string]any)
			if str(em, "resource_id") == str(trAlloc, "resource_id") && str(em, "kind") == "transit" {
				found = true
			}
		}
		if !found {
			t.Fatalf("day view without the transit: %v", day)
		}
		// 3. Arrival on the 5th.
		w.setClock(t, time.Date(2027, 6, 5, 13, 0, 0, 0, time.UTC))
		arr := dock.expect(t, 201, "POST", M+"/presences", map[string]any{"vessel_id": libeccio, "event": "arrived"}, "Idempotency-Key", "acc-m3-arrival-0001")
		if c, _ := arr["contract"].(map[string]any); c == nil || str(c, "status") != "arrived" {
			t.Fatalf("arrival %v", arr)
		}

		// 4. Office: annual contract with a parking, activated.
		grecale := w.vessel(t, "Grecale", 1050, 360)
		bianchi := w.customer(t, "Bianchi")
		annualBerth := b[10]
		ct := office.expect(t, 201, "POST", M+"/contracts", map[string]any{
			"term": "annual", "customer_id": bianchi, "vessel_id": grecale, "starts_on": "2027-01-01",
			"placements":      []any{berthPlacement(annualBerth, "2027-01-01", "2027-12-31")},
			"extra_resources": []any{map[string]any{"land_resource_id": w.harbour.Lands[0]}},
		})
		ct = office.expect(t, 200, "POST", M+"/contracts/"+str(ct, "id")+"/activate", nil, "If-Match", etagOf(ct["version"]))
		var annualAlloc map[string]any
		for _, a := range jsonList(ct, "allocations") {
			if str(a.(map[string]any), "resource_id") == annualBerth {
				annualAlloc = a.(map[string]any)
			}
		}
		if str(ct, "status") != "active" || annualAlloc == nil || str(annualAlloc, "kind") != "contract" {
			t.Fatalf("annual %v", ct)
		}
		// 5. Release 10 nights; a transit search finds the released berth; book it → release.resold.
		rel := office.expect(t, 201, "POST", M+"/releases", map[string]any{
			"allocation_id": str(annualAlloc, "id"), "from_night": "2027-06-10", "to_night": "2027-06-19", "source": "customer_phone",
		})
		maestrale := w.vessel(t, "Maestrale", 1000, 350)
		sr = dock.expect(t, 200, "POST", M+"/occupancies/search", map[string]any{
			"purpose": "transit", "vessel_id": maestrale, "arrival": "2027-06-12", "departure": "2027-06-15", "consider_out": false, "limit": 50,
			"resource_types": []string{"berth"},
		})
		seen := false
		for _, s := range jsonList(sr, "solutions") {
			for _, sg := range jsonList(s.(map[string]any), "segments") {
				if str(sg.(map[string]any), "resource_id") == annualBerth {
					seen = true
				}
			}
		}
		if !seen {
			t.Fatalf("released berth not offered: %v", sr)
		}
		dock.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": maestrale, "customer_id": rossi, "arrival": "2027-06-12", "departure": "2027-06-15",
			"placements": []any{berthPlacement(annualBerth, "2027-06-12", "2027-06-14")}, "confirm": map[string]any{},
		})
		if n := len(eventsOf(t, db, event.ReleaseResold)); n != 1 {
			t.Errorf("release.resold events %d (a confirmed transit resells the released nights)", n)
		}
		// 6. Revoke on resold nights → 409 resource_taken. The resale bumped the release's version: read it again.
		rels := office.expect(t, 200, "GET", M+"/releases?resource_id="+annualBerth, nil)
		for _, it := range jsonList(rels, "items") {
			if str(it.(map[string]any), "id") == str(rel, "id") {
				rel = it.(map[string]any)
			}
		}
		if rel["resold_allocation_id"] == nil || str(rel, "resold_allocation_id") == "" {
			t.Errorf("release not marked resold: %v", rel)
		}
		r := office.do(t, "POST", M+"/releases/"+str(rel, "id")+"/revoke", map[string]any{"from_night": "2027-06-12", "to_night": "2027-06-14"}, "If-Match", etagOf(rel["version"]))
		if r.status != 409 || r.problemType() != "resource_taken" {
			t.Fatalf("revoke on resold nights: %d %s", r.status, r.body)
		}

		// 7. Drag-equivalent move of the arrived transit to another berth from today, and undo.
		mv := dock.expect(t, 200, "POST", M+"/occupancies/move", map[string]any{
			"allocation_id": str(trAlloc, "id"), "version": trAlloc["version"],
			"target": map[string]any{"resource_type": "berth", "resource_id": b[15]},
		})
		dock.expect(t, 200, "POST", M+"/move-undo/"+str(mv, "undo_id"), nil)

		// 8. Layout session on the linear area: two vessels exchange places.
		v1, v2 := w.vessel(t, "Uno", 800, 300), w.vessel(t, "Due", 900, 320)
		areaTransit := func(v string, x int) map[string]any {
			return dock.expect(t, 201, "POST", M+"/transits", map[string]any{
				"vessel_id": v, "customer_id": rossi, "arrival": "2027-06-05", "departure": "2027-06-09",
				"placements": []any{map[string]any{"resource_type": "dynamic_area", "resource_id": w.harbour.Area, "x_cm": x, "first_night": "2027-06-05", "last_night": "2027-06-08"}},
				"confirm":    map[string]any{},
			})
		}
		a1, a2 := firstAllocation(t, areaTransit(v1, 100)), firstAllocation(t, areaTransit(v2, 3000))
		dock.expect(t, 200, "POST", M+"/dynamic-areas/"+w.harbour.Area+"/layout", map[string]any{
			"date": "2027-06-05",
			"base": []any{
				map[string]any{"allocation_id": a1["id"], "version": a1["version"]}, map[string]any{"allocation_id": a2["id"], "version": a2["version"]},
			},
			"placements": []any{map[string]any{"allocation_id": a1["id"], "x_cm": 3000}, map[string]any{"allocation_id": a2["id"], "x_cm": 100}},
		})

		// 9. Mooring quote with two options and holds; accept option 2 → draft contract; option 1's hold gone.
		scirocco := w.vessel(t, "Scirocco", 1100, 370)
		q := o.expect(t, 201, "POST", M+"/quotes", map[string]any{
			"kind": "mooring", "term": "annual", "customer_id": rossi, "vessel_id": scirocco, "starts_on": "2027-07-01", "ends_on": "2028-07-01",
			"options": []any{
				map[string]any{"hold": true, "placements": []any{berthPlacement(b[16], "2027-07-01", "2028-06-30")}},
				map[string]any{"hold": true, "placements": []any{berthPlacement(b[17], "2027-07-01", "2028-06-30")}},
			},
		})
		opts := jsonList(q, "options")
		hold1 := str(jsonList(opts[0].(map[string]any), "placements")[0].(map[string]any), "allocation_id")
		acc := o.expect(t, 200, "POST", M+"/quotes/"+str(q, "id")+"/accept", map[string]any{"option_id": opts[1].(map[string]any)["id"]}, "If-Match", etagOf(q["version"]))
		if c := acc["contract"].(map[string]any); str(c, "status") != "draft" || str(acc["quote"].(map[string]any), "status") != "accepted" {
			t.Fatalf("accept %v", acc)
		}
		if n, _ := db.NewSelect().TableExpr("berth_occupancies").Where("allocation_id = ?", hold1).Count(context.Background()); n != 0 {
			t.Fatal("option 1 hold kept its rows")
		}
		// 10. Another quote expires after the clock passes its validity and the job runs.
		q2 := o.expect(t, 201, "POST", M+"/quotes", map[string]any{
			"kind": "transit", "customer_id": rossi, "vessel_id": scirocco, "starts_on": "2027-08-01", "ends_on": "2027-08-04", "valid_until": "2027-06-06",
			"options": []any{map[string]any{"hold": true, "placements": []any{berthPlacement(b[18], "2027-08-01", "2027-08-03")}}},
		})
		hold2 := str(jsonList(jsonList(q2, "options")[0].(map[string]any), "placements")[0].(map[string]any), "allocation_id")
		w.setClock(t, time.Date(2027, 6, 7, 8, 0, 0, 0, time.UTC))
		w.h.runJobs("contracts.expire")
		q2 = o.expect(t, 200, "GET", M+"/quotes/"+str(q2, "id"), nil)
		if str(q2, "status") != "expired" {
			t.Fatalf("quote not expired: %v", q2)
		}
		if n, _ := db.NewSelect().TableExpr("berth_occupancies").Where("allocation_id = ?", hold2).Count(context.Background()); n != 0 {
			t.Fatal("expired hold kept its rows")
		}

		// 11. Roles and isolation.
		yd := yard.expect(t, 200, "GET", M+"/maps/"+w.harbour.MapID+"/day?date=2027-06-07", nil)
		for _, el := range jsonList(yd, "elements") {
			if c, ok := el.(map[string]any)["customer"].(map[string]any); ok && c["display_name"] != nil {
				t.Fatalf("yard sees a customer name: %v", c)
			}
		}
		if r := yard.do(t, "POST", M+"/transits", map[string]any{"vessel_id": libeccio, "arrival": "2027-07-01", "departure": "2027-07-02"}); r.status != 403 {
			t.Fatalf("yard createTransit: %d", r.status)
		}
		if r := viewer.do(t, "GET", M+"/contracts", nil); r.status != 403 {
			t.Fatalf("viewer listContracts: %d", r.status)
		}
		other, err := w.h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
			Email: "altro@porto.test", Name: "Altro", Password: password, Organization: "Altro SRL", MarinaName: "Altra", MarinaSlug: "altra",
		})
		if err != nil {
			t.Fatal(err)
		}
		oc := w.h.cookieClient()
		oc.login(t, "altro@porto.test")
		OM := A + "/marinas/" + other.MarinaID
		for _, p := range []string{OM + "/contracts/" + str(ct, "id"), OM + "/quotes/" + str(q, "id"), M + "/transits"} {
			if r := oc.do(t, "GET", p, nil); r.status != 404 {
				t.Fatalf("%s from another marina: %d", p, r.status)
			}
		}

		// 12. Public events of §7.5 were written, without personal data.
		for _, typ := range []string{
			event.TransitHeld, event.TransitConfirmed, event.TransitArrived, event.ContractCreated, event.ContractActivated,
			event.QuoteAccepted, event.QuoteExpired, event.OccupancyAllocated, event.OccupancyMoved, event.OccupancyHoldExpired,
			event.ReleaseCreated, event.ReleaseResold, event.PresenceArrived,
		} {
			evs := eventsOf(t, db, typ)
			if len(evs) == 0 {
				t.Errorf("no %s event", typ)
			}
			for _, ev := range evs {
				var data map[string]any
				_ = json.Unmarshal(ev.Payload, &data)
				if err := event.CheckNoPII(data); err != nil {
					t.Errorf("%s: %v", typ, err)
				}
				for _, secret := range []string{"Rossi", "Bianchi", "Libeccio", "POS 12"} {
					if strings.Contains(string(ev.Payload), secret) {
						t.Errorf("%s carries %q", typ, secret)
					}
				}
			}
		}
		today := calendar.In(w.clock.Now(), time.UTC)
		vs, err := store.CheckOccupancyInvariants(context.Background(), db.DB, w.marina, today)
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) != 0 {
			t.Fatalf("invariants: %+v", vs)
		}
	})
}

func eventsOf(t *testing.T, db *store.DB, typ string) []store.OutboxEvent {
	t.Helper()
	var out []store.OutboxEvent
	if err := db.NewSelect().Model(&out).Where("type = ?", typ).Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return out
}
