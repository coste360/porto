//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// bkWorld is a desk world with a rental price list (hour 45.00, half day 150.00, skipper 25.00 per hour), the
// rental settings with a skipper item, units created through the API, the ORARIA formula (skipper optional) and
// one skipper.
type bkWorld struct {
	*deskWorld
	unitItem, skipperItem string
	hourly                string
	skipper               string
	units                 []string
}

func newBkWorld(t *testing.T, db *store.DB, units int) *bkWorld {
	t.Helper()
	w := &bkWorld{deskWorld: newDeskWorld(t, db, 1)}
	o, M := w.owner, w.M
	classes := o.expect(t, 200, "GET", M+"/revenue-classes", nil)
	rental := ""
	for _, rc := range jsonList(classes, "items") {
		if str(rc.(map[string]any), "code") == "rental" {
			rental = str(rc.(map[string]any), "id")
		}
	}
	item := func(code string) string {
		return str(o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": code, "name": map[string]string{"it": code}, "item_type": "service", "unit": "hour",
			"revenue_class_id": rental, "price_includes_vat": true,
		}), "id")
	}
	w.unitItem, w.skipperItem = item("NOL-GOM58"), item("SKIPPER")
	doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
		"code": "NOL-2027", "name": map[string]string{"it": "Noleggio"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{"charter"},
	})
	rule := func(key, itemID, unit string, cents int, scope map[string]any) map[string]any {
		r := map[string]any{"key": key, "stage": "base", "name": key, "item_id": itemID, "basis": "flat", "unit": unit, "amount_cents": cents}
		if scope != nil {
			r["scope"] = scope
		}
		return r
	}
	body := map[string]any{
		"code": "NOL-2027", "name": map[string]string{"it": "Noleggio"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{}, "settings": doc["settings"], "periods": []any{}, "bands": []any{},
		"rules": []map[string]any{
			rule("h", w.unitItem, "hour", 4500, nil),
			rule("m", w.unitItem, "half_day", 15000, map[string]any{"time_bands": []string{"morning"}}),
			rule("s", w.skipperItem, "hour", 2500, nil),
		},
	}
	saved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(doc, "id"), body, "If-Match", etagOf(doc["version"]))
	o.expect(t, 200, "POST", M+"/price-lists/"+str(doc, "id")+"/activate", map[string]any{}, "If-Match",
		etagOf(saved["document"].(map[string]any)["version"]))
	set := o.expect(t, 200, "GET", M+"/rental/settings", nil)
	o.expect(t, 200, "PUT", M+"/rental/settings", map[string]any{
		"open_from_slot": 16, "open_to_slot": 38, "bands": set["bands"], "buffer_slots": 1, "desk_hold_minutes": 60,
		"no_show_after_minutes": 60, "licence_threshold_kw_milli": 30000, "skipper_item_id": w.skipperItem, "checklist": set["checklist"],
		"checkout_min_photos": 2, "checkin_min_photos": 1, "deposit_modes": []string{"cash", "card_pos"}, "identity_retention_days": 365,
	}, "If-Match", `"0"`)
	for i := range units {
		v := o.expect(t, 201, "POST", M+"/vessels", map[string]any{"name": fmt.Sprintf("Gommone %d", i), "loa_cm": 580, "beam_cm": 240, "hull_type": "rib"})
		u := o.expect(t, 201, "POST", M+"/rental/units", map[string]any{
			"vessel_id": str(v, "id"), "code": fmt.Sprintf("GOM%03d", i), "category": "rib", "seats": 6, "fuel_type": "petrol",
			"tank_milli": 60000, "item_id": w.unitItem,
		})
		w.units = append(w.units, str(u, "id"))
	}
	w.hourly = str(o.expect(t, 201, "POST", M+"/rental/formulas", map[string]any{
		"code": "ORARIA", "name": map[string]string{"it": "Oraria"}, "kind": "hourly", "skipper": "optional",
	}), "id")
	w.skipper = str(o.expect(t, 201, "POST", M+"/rental/skippers", map[string]any{"name": "Marco", "licence_expires_on": "2030-12-31"}), "id")
	return w
}

func bkPoint(date string, slot int) map[string]any { return map[string]any{"date": date, "slot": slot} }

// bkPost sends one request from a goroutine (no t.Fatal off the test goroutine) and returns status and problem type.
func (w *bkWorld) bkPost(path string, body any) (int, string, map[string]any, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, "", nil, err
	}
	req, err := http.NewRequest("POST", w.h.srv.URL+path, bytes.NewReader(b))
	if err != nil {
		return 0, "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := w.owner.csrf(); tok != "" {
		req.Header.Set("X-CSRF-Token", tok)
	}
	res, err := w.owner.http.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, str(m, "type"), m, nil
}

func (w *bkWorld) q32(t *testing.T) {
	t.Helper()
	vs, err := store.CheckRentalSlots(context.Background(), w.h.app.DB.DB, w.marina)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("invariant violated: %s", v)
	}
}

// TestAcceptance_M11_NoDoubleBooking (spec 32 M11 criterion 1, INV-18): 100 parallel HTTP createRentalBooking calls
// on one unit, 50 of them overlapping a common slot (one conflicting group) and 50 on distinct dates (control):
// exactly one 201 in the group, the rest 409 rental_slot_taken, every control 201; the planner shows no overlap and
// Q32 holds. Then 100 parallel calls with the same skipper on 100 units: one 201, 99 skipper_busy.
func TestAcceptance_M11_NoDoubleBooking(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newBkWorld(t, db, 100)
		M := w.M
		type result struct {
			group  bool
			status int
			typ    string
			err    error
		}
		results := make([]result, 100)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				body := map[string]any{"unit_id": w.units[0], "formula_id": w.hourly}
				group := i%2 == 0
				if group {
					// Every range contains slot 22 (11:00-11:30) of 2027-06-12.
					s0, s1 := 18+i%5, 23+(i/2)%5
					body["start"], body["end"] = bkPoint("2027-06-12", s0), bkPoint("2027-06-12", s1)
				} else {
					day := time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i/2).Format("2006-01-02")
					body["start"], body["end"] = bkPoint(day, 20), bkPoint(day, 24)
				}
				<-start
				status, typ, _, err := w.bkPost(M+"/rental/bookings", body)
				results[i] = result{group: group, status: status, typ: typ, err: err}
			}()
		}
		close(start)
		wg.Wait()
		won, lost, control := 0, 0, 0
		for i, r := range results {
			switch {
			case r.err != nil:
				t.Fatalf("call %d: %v", i, r.err)
			case r.group && r.status == 201:
				won++
			case r.group && r.status == 409 && r.typ == "rental_slot_taken":
				lost++
			case !r.group && r.status == 201:
				control++
			default:
				t.Fatalf("call %d (group %v): %d %s", i, r.group, r.status, r.typ)
			}
		}
		if won != 1 || lost != 49 || control != 50 {
			t.Fatalf("won %d lost %d control %d", won, lost, control)
		}
		pl := w.owner.expect(t, 200, "GET", M+"/rental/planner?from=2027-06-12&to=2027-06-12&view=hours", nil)
		rows := jsonList(pl, "rows")
		booked := 0
		for _, sg := range jsonList(rows[0].(map[string]any), "segments") {
			if str(sg.(map[string]any), "kind") == "booked" {
				booked++
			}
		}
		if str(rows[0].(map[string]any), "id") != w.units[0] || booked != 1 {
			t.Fatalf("planner overlap: %d booked segments", booked)
		}
		w.q32(t)

		// One skipper, 100 units, the same window.
		statuses := make([]int, 100)
		types := make([]string, 100)
		start = make(chan struct{})
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				body := map[string]any{
					"unit_id": w.units[i], "formula_id": w.hourly, "skipper_id": w.skipper,
					"start": bkPoint("2027-06-15", 20), "end": bkPoint("2027-06-15", 26),
				}
				<-start
				st, typ, _, err := w.bkPost(M+"/rental/bookings", body)
				if err != nil {
					st = -1
				}
				statuses[i], types[i] = st, typ
			}()
		}
		close(start)
		wg.Wait()
		won, busy := 0, 0
		for i := range statuses {
			switch {
			case statuses[i] == 201:
				won++
			case statuses[i] == 409 && types[i] == "skipper_busy":
				busy++
			default:
				t.Fatalf("skipper call %d: %d %s", i, statuses[i], types[i])
			}
		}
		if won != 1 || busy != 99 {
			t.Fatalf("skipper won %d busy %d", won, busy)
		}
		n, err := db.NewSelect().TableExpr("rental_slots").Where("marina_id = ?", w.marina).Where("date = ?", "2027-06-15").Count(context.Background())
		if err != nil || n != 7 {
			t.Fatalf("unit rows on the skipper day %d %v", n, err)
		}
		w.q32(t)
	})
}

// TestAcceptance_M11_HoldsAndIsolation (INV-05 extended, INV-73): a desk option holds the window; once expired a
// sale wins at conflict time before the job runs, the holds job cancels the option with hold_expired; another
// marina sees 404 for the bookings, the planner and the blocks.
func TestAcceptance_M11_HoldsAndIsolation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newBkWorld(t, db, 1)
		o, M := w.owner, w.M
		hold := o.expect(t, 201, "POST", M+"/rental/bookings", map[string]any{
			"unit_id": w.units[0], "formula_id": w.hourly, "start": bkPoint("2027-06-12", 20), "end": bkPoint("2027-06-12", 26), "hold": true,
			"renter": map[string]string{"name": "Ada Rossi"},
		})
		if str(hold, "status") != "held" || hold["hold_expires_at"] == nil {
			t.Fatalf("hold %v", hold)
		}
		body := map[string]any{"unit_id": w.units[0], "formula_id": w.hourly, "start": bkPoint("2027-06-12", 22), "end": bkPoint("2027-06-12", 24)}
		r := o.do(t, "POST", M+"/rental/bookings", body)
		if r.status != 409 || r.problemType() != "rental_slot_taken" {
			t.Fatalf("live hold: %d %s", r.status, r.body)
		}
		w.setClock(t, w.clock.Now().Add(61*time.Minute))
		o = w.owner
		sale := o.expect(t, 201, "POST", M+"/rental/bookings", body)
		got := o.expect(t, 200, "GET", M+"/rental/bookings/"+str(hold, "id"), nil)
		if str(got, "status") != "held" || got["slots_lost"] != true {
			t.Fatalf("reclaimed hold %v", got)
		}
		r = o.do(t, "POST", M+"/rental/bookings/"+str(hold, "id")+"/transition", map[string]any{"action": "confirm"}, "If-Match", etagOf(got["version"]))
		if r.status != 409 || r.problemType() != "rental_hold_expired" {
			t.Fatalf("confirm lost hold: %d %s", r.status, r.body)
		}
		w.h.runJobs("rental.holds_expire")
		got = o.expect(t, 200, "GET", M+"/rental/bookings/"+str(hold, "id"), nil)
		if str(got, "status") != "cancelled" || str(got, "cancel_reason") != "hold_expired" {
			t.Fatalf("expired hold %v", got)
		}
		w.q32(t)
		blk := o.expect(t, 201, "POST", M+"/rental/blocks", map[string]any{
			"unit_id": w.units[0], "from": bkPoint("2027-06-20", 0), "to": bkPoint("2027-06-20", 48), "reason": "maintenance",
		})

		// Isolation: a second marina of another organization.
		other := w.h.cookieClient()
		iso, err := w.h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
			Email: "iso@porto.test", Name: "Iso", Password: password, Organization: "Org Iso", MarinaName: "Marina Iso", MarinaSlug: "marina-iso",
		})
		if err != nil {
			t.Fatal(err)
		}
		other.login(t, "iso@porto.test")
		X := A + "/marinas/" + iso.MarinaID
		for _, path := range []string{
			M + "/rental/bookings/" + str(sale, "id"), X + "/rental/bookings/" + str(sale, "id"), M + "/rental/planner?from=2027-06-12&to=2027-06-12",
			M + "/rental/day", M + "/rental/blocks",
		} {
			if r := other.do(t, "GET", path, nil); r.status != 404 {
				t.Fatalf("GET %s: %d", path, r.status)
			}
		}
		if r := other.do(t, "PATCH", X+"/rental/blocks/"+str(blk, "id"), map[string]any{"note": "x"}, "If-Match", `"1"`); r.status != 404 {
			t.Fatalf("PATCH other block: %d", r.status)
		}
		if r := other.do(t, "POST", X+"/rental/bookings/"+str(sale, "id")+"/transition", map[string]any{"action": "cancel"}, "If-Match", `"1"`); r.status != 404 {
			t.Fatalf("transition other booking: %d", r.status)
		}
	})
}
