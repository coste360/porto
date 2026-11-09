//go:build integration

package admin_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

// fleetTSetup is a desk setup with a rental member, rental items, the §5.3 price list (2027) and a 40 kW vessel.
type fleetTSetup struct {
	*deskSetup
	rental              *harborClient
	gom, skp, snk, frg  string
	fuel, late, nonRent string
	vessel40            string
}

func newFleetTSetup(t *testing.T) *fleetTSetup {
	t.Helper()
	ds := newDeskSetup(t)
	fs := &fleetTSetup{deskSetup: ds}
	c, M := ds.owner, ds.M
	var mem struct {
		UserID string `json:"user_id"`
	}
	c.expect(t, 201, "", "POST", M+"/members", map[string]any{"email": "rental@porto.test", "name": "rental", "role": "rental"}, &mem)
	if err := ds.s.app.Auth.SetPassword(context.Background(), mem.UserID, harborPW); err != nil {
		t.Fatal(err)
	}
	fs.rental = ds.s.login(t, "rental@porto.test")

	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	class := map[string]string{}
	for _, rc := range classes.Items {
		class[rc.Code] = rc.Id
	}
	item := func(code, unit, rc string) string {
		var it api.Item
		c.expect(t, 201, "", "POST", M+"/items", map[string]any{
			"code": code, "name": map[string]string{"it": code}, "item_type": "service", "unit": unit,
			"revenue_class_id": class[rc], "price_includes_vat": true,
		}, &it)
		return it.Id
	}
	fs.gom, fs.skp = item("NOL-GOM58", "hour", "rental"), item("SKIPPER", "hour", "rental")
	fs.snk, fs.frg = item("SNORKEL", "lump", "rental"), item("FRIGO", "lump", "rental")
	fs.fuel, fs.late = item("FUEL-P", "piece", "rental"), item("LATE-GOM", "hour", "rental")
	fs.nonRent = item("SRV-X", "lump", "services")

	var doc api.PriceListDocument
	c.expect(t, 201, "", "POST", M+"/price-lists", map[string]any{
		"code": "NOL-2027", "name": map[string]string{"it": "Noleggio"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{"charter"},
	}, &doc)
	body := saveBody(doc)
	body["contract_types"] = []string{}
	body["periods"] = []any{map[string]any{"key": "alta", "code": "alta", "name": map[string]string{"it": "Alta"}, "kind": "season", "ranges": []any{map[string]string{"from": "2027-07-01", "to": "2027-08-31"}}}}
	body["bands"] = []any{}
	n := 0
	rule := func(item, unit string, cents int64, scope map[string]any) map[string]any {
		n++
		r := map[string]any{"key": "r" + strconv.Itoa(n), "stage": "base", "name": unit, "item_id": item, "basis": "flat", "unit": unit, "amount_cents": cents}
		if scope != nil {
			r["scope"] = scope
		}
		return r
	}
	alta := map[string]any{"periods": []string{"alta"}}
	body["rules"] = []map[string]any{
		rule(fs.gom, "hour", 4500, nil), rule(fs.gom, "hour", 5500, alta),
		rule(fs.gom, "half_day", 15000, map[string]any{"time_bands": []string{"morning"}}),
		rule(fs.gom, "half_day", 13000, map[string]any{"time_bands": []string{"afternoon"}}),
		rule(fs.gom, "day", 24000, nil), rule(fs.gom, "week", 140000, nil),
		rule(fs.skp, "hour", 2500, nil), rule(fs.skp, "half_day", 10000, nil), rule(fs.skp, "day", 18000, nil),
		rule(fs.snk, "stay", 1000, nil), rule(fs.frg, "stay", 500, nil),
	}
	var res api.PriceListSaveResult
	c.expect(t, 200, "", "PUT", M+"/price-lists/"+doc.Id, body, &res, "If-Match", etag(doc.Version))
	c.expect(t, 200, "", "POST", M+"/price-lists/"+doc.Id+"/activate", map[string]any{}, nil, "If-Match", etag(res.Document.Version))

	var v api.Vessel
	c.expect(t, 201, "", "POST", M+"/vessels", map[string]any{
		"name": "Gabbiano", "loa_cm": 580, "beam_cm": 240, "hull_type": "rib", "engines": []map[string]any{{"power_kw": 40}},
	}, &v)
	fs.vessel40 = v.Id
	return fs
}

func TestRentalFleetHandlers_Scenario(t *testing.T) {
	fs := newFleetTSetup(t)
	c, M, X := fs.owner, fs.M, fs.otherM
	rc := fs.rental

	// Settings: defaults with version 0, first save with If-Match "0", configuration needs marina.configure.
	var set api.RentalSettings
	c.expect(t, 200, "", "GET", M+"/rental/settings", nil, &set)
	if set.Version != 0 || set.OpenFromSlot != 16 || len(set.Bands) != 2 || len(set.Checklist) != 8 {
		t.Fatalf("default settings %+v", set)
	}
	in := map[string]any{
		"open_from_slot": 16, "open_to_slot": 38, "bands": set.Bands, "buffer_slots": 1, "desk_hold_minutes": 60,
		"no_show_after_minutes": 60, "licence_threshold_kw_milli": 30000, "skipper_item_id": fs.skp, "checklist": set.Checklist,
		"checkout_min_photos": 2, "checkin_min_photos": 1, "deposit_modes": []string{"cash", "card_pos", "preauth"},
		"identity_retention_days": 365,
	}
	rc.expect(t, 403, "forbidden", "PUT", M+"/rental/settings", in, nil, "If-Match", etag(0))
	c.expect(t, 428, "precondition_required", "PUT", M+"/rental/settings", in, nil)
	c.expect(t, 200, "", "PUT", M+"/rental/settings", in, &set, "If-Match", etag(0))
	if set.Version != 1 || set.SkipperItemId == nil || *set.SkipperItemId != fs.skp {
		t.Fatalf("saved settings %+v", set)
	}
	c.expect(t, 412, "version_conflict", "PUT", M+"/rental/settings", in, nil, "If-Match", etag(0))
	gap := map[string]any{}
	for k, v := range in {
		gap[k] = v
	}
	gap["buffer_slots"] = 3
	c.expect(t, 422, "band_gap_lt_buffer", "PUT", M+"/rental/settings", gap, nil, "If-Match", etag(1))
	gap["buffer_slots"], gap["open_from_slot"] = 1, 4
	c.expect(t, 400, "validation_failed", "PUT", M+"/rental/settings", gap, nil, "If-Match", etag(1))
	gap["open_from_slot"], gap["skipper_item_id"] = 16, fs.nonRent
	c.expect(t, 400, "validation_failed", "PUT", M+"/rental/settings", gap, nil, "If-Match", etag(1))
	fs.other.expect(t, 404, "not_found", "GET", M+"/rental/settings", nil, nil)

	// Units.
	unit := map[string]any{
		"vessel_id": fs.vessel40, "code": "GOM58", "category": "rib", "seats": 8, "fuel_type": "petrol", "tank_milli": 60000,
		"deposit_cents": 50000, "item_id": fs.gom, "home_berth_id": fs.berths[0],
	}
	var u api.RentalUnit
	rc.expect(t, 201, "", "POST", M+"/rental/units", unit, &u)
	if !u.LicenceRequired || u.EngineKwMilli != 40000 || u.VesselName != "Gabbiano" || u.HomeBerthCode == nil || u.Licence != "threshold" {
		t.Fatalf("unit %+v", u)
	}
	rc.expect(t, 409, "code_taken", "POST", M+"/rental/units", unit, nil)
	bad := map[string]any{"vessel_id": fs.vessel, "code": "x", "category": "rib", "seats": 0, "fuel_type": "petrol", "item_id": fs.nonRent}
	rc.expect(t, 400, "validation_failed", "POST", M+"/rental/units", bad, nil)
	fs.yard.expect(t, 403, "forbidden", "GET", M+"/rental/units", nil, nil)
	var units api.RentalUnitList
	rc.expect(t, 200, "", "GET", M+"/rental/units?category=rib", nil, &units)
	if len(units.Items) != 1 {
		t.Fatalf("units %+v", units)
	}
	rc.expect(t, 400, "validation_failed", "GET", M+"/rental/units?status=broken", nil, nil)
	rc.expect(t, 428, "precondition_required", "PATCH", M+"/rental/units/"+u.Id, map[string]any{"seats": 6}, nil)
	unitID := u.Id
	u = api.RentalUnit{} // omitted optional fields must not keep the previous values
	rc.expect(t, 200, "", "PATCH", M+"/rental/units/"+unitID, map[string]any{"seats": 6, "licence": "never", "home_berth_id": nil}, &u, "If-Match", etag(1))
	if u.Seats != 6 || u.LicenceRequired || u.HomeBerthId != nil || u.Version != 2 {
		t.Fatalf("patched unit %+v", u)
	}
	rc.expect(t, 200, "", "PATCH", M+"/rental/units/"+u.Id, map[string]any{"licence": "threshold"}, &u, "If-Match", etag(2))
	rc.expect(t, 412, "version_conflict", "PATCH", M+"/rental/units/"+u.Id, map[string]any{"seats": 5}, nil, "If-Match", etag(1))
	fs.other.expect(t, 404, "not_found", "GET", M+"/rental/units/"+u.Id, nil, nil)
	fs.other.expect(t, 404, "not_found", "GET", X+"/rental/units/"+u.Id, nil, nil)

	// Skippers: contact encrypted, hours replaced.
	var k api.RentalSkipper
	rc.expect(t, 201, "", "POST", M+"/rental/skippers", map[string]any{"name": "Marco", "phone": "+39 333 1234567", "licence_expires_on": "2030-12-31"}, &k)
	if k.Phone == nil || *k.Phone != "+39 333 1234567" || k.LicenceExpiresOn == nil || len(k.Hours) != 0 {
		t.Fatalf("skipper %+v", k)
	}
	rc.expect(t, 400, "validation_failed", "POST", M+"/rental/skippers", map[string]any{"name": "", "email": "nope"}, nil)
	hours := map[string]any{"hours": []map[string]int{{"weekday": 1, "from_slot": 16, "to_slot": 38}, {"weekday": 2, "from_slot": 16, "to_slot": 26}}}
	rc.expect(t, 200, "", "PUT", M+"/rental/skippers/"+k.Id+"/hours", hours, &k, "If-Match", etag(1))
	if len(k.Hours) != 2 || k.Version != 2 {
		t.Fatalf("hours %+v", k)
	}
	overlap := map[string]any{"hours": []map[string]int{{"weekday": 1, "from_slot": 16, "to_slot": 30}, {"weekday": 1, "from_slot": 20, "to_slot": 38}}}
	rc.expect(t, 400, "validation_failed", "PUT", M+"/rental/skippers/"+k.Id+"/hours", overlap, nil, "If-Match", etag(2))
	skipperID := k.Id
	k = api.RentalSkipper{}
	rc.expect(t, 200, "", "PATCH", M+"/rental/skippers/"+skipperID, map[string]any{"phone": nil, "email": "marco@example.test"}, &k, "If-Match", etag(2))
	if k.Phone != nil || k.Email == nil {
		t.Fatalf("patched skipper %+v", k)
	}
	var sk api.RentalSkipperList
	rc.expect(t, 200, "", "GET", M+"/rental/skippers?active=true", nil, &sk)
	if len(sk.Items) != 1 || len(sk.Items[0].Hours) != 2 {
		t.Fatalf("skippers %+v", sk)
	}
	fs.other.expect(t, 404, "not_found", "PATCH", M+"/rental/skippers/"+k.Id, map[string]any{"name": "x"}, nil, "If-Match", etag(3))

	// Formulas (marina.configure).
	formula := func(code, kind, band string, extra map[string]any) map[string]any {
		f := map[string]any{"code": code, "name": map[string]string{"it": code}, "kind": kind}
		if band != "" {
			f["band_code"] = band
		}
		for k, v := range extra {
			f[k] = v
		}
		return f
	}
	rc.expect(t, 403, "forbidden", "POST", M+"/rental/formulas", formula("ORARIA", "hourly", "", nil), nil)
	var hourly, morning, day, week api.RentalFormula
	c.expect(t, 201, "", "POST", M+"/rental/formulas", formula("ORARIA", "hourly", "", map[string]any{"skipper": "optional", "fuel": "metered", "fuel_item_id": fs.fuel, "late_item_id": fs.late}), &hourly)
	if hourly.MinSlots != 2 || hourly.GraceMinutes != 15 || hourly.LateStepMinutes != 30 || hourly.MaxDays != 30 {
		t.Fatalf("hourly %+v", hourly)
	}
	c.expect(t, 201, "", "POST", M+"/rental/formulas", formula("MEZZA-MATTINA", "half_day", "morning", map[string]any{"skipper": "optional"}), &morning)
	c.expect(t, 201, "", "POST", M+"/rental/formulas", formula("GIORNATA", "day", "", nil), &day)
	c.expect(t, 201, "", "POST", M+"/rental/formulas", formula("SETTIMANA", "week", "", nil), &week)
	if week.MinDays != 7 {
		t.Fatalf("week %+v", week)
	}
	c.expect(t, 409, "code_taken", "POST", M+"/rental/formulas", formula("GIORNATA", "day", "", nil), nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/rental/formulas", formula("SERA", "half_day", "evening", nil), nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/rental/formulas", formula("CARB", "day", "", map[string]any{"fuel": "metered"}), nil)
	c.expect(t, 200, "", "PATCH", M+"/rental/formulas/"+day.Id, map[string]any{"grace_minutes": 30, "categories": []string{"rib"}}, &day, "If-Match", etag(1))
	if day.GraceMinutes != 30 || len(day.Categories) != 1 {
		t.Fatalf("day %+v", day)
	}
	var fl api.RentalFormulaList
	rc.expect(t, 200, "", "GET", M+"/rental/formulas", nil, &fl)
	if len(fl.Items) != 4 {
		t.Fatalf("formulas %d", len(fl.Items))
	}
	// A band in use cannot be removed.
	noMorning := map[string]any{}
	for k, v := range in {
		noMorning[k] = v
	}
	noMorning["bands"] = []api.RentalBand{set.Bands[1]}
	c.expect(t, 400, "validation_failed", "PUT", M+"/rental/settings", noMorning, nil, "If-Match", etag(1))

	// Extras.
	var snk, frg api.RentalExtra
	c.expect(t, 201, "", "POST", M+"/rental/extras", map[string]any{"item_id": fs.snk}, &snk)
	c.expect(t, 201, "", "POST", M+"/rental/extras", map[string]any{"item_id": fs.frg, "charge_mode": "per_day"}, &frg)
	if snk.ItemCode != "SNORKEL" || snk.DefaultQtyMilli != 1000 || frg.ChargeMode != "per_day" {
		t.Fatalf("extras %+v %+v", snk, frg)
	}
	c.expect(t, 409, "code_taken", "POST", M+"/rental/extras", map[string]any{"item_id": fs.snk}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/rental/extras", map[string]any{"item_id": fs.late, "default_qty_milli": 5000, "max_qty_milli": 1000}, nil)
	c.expect(t, 200, "", "PATCH", M+"/rental/extras/"+snk.Id, map[string]any{"max_qty_milli": 4000}, &snk, "If-Match", etag(1))
	var el api.RentalExtraList
	rc.expect(t, 200, "", "GET", M+"/rental/extras?active=true", nil, &el)
	if len(el.Items) != 2 {
		t.Fatalf("extras %d", len(el.Items))
	}

	// Quotes: E1 hourly, E3 half day + skipper + snorkel, every formula sorted by total.
	pt := func(d string, s int) map[string]any { return map[string]any{"date": d, "slot": s} }
	var q api.RentalQuoteResult
	rc.expect(t, 200, "", "POST", M+"/rental/quote", map[string]any{"unit_id": u.Id, "formula_id": hourly.Id, "start": pt("2027-06-12", 20), "end": pt("2027-06-12", 26)}, &q)
	if len(q.Items) != 1 || !q.Items[0].Ok || q.Items[0].TotalCents != 13500 || !q.Items[0].LicenceRequired {
		t.Fatalf("E1 %+v", q)
	}
	rc.expect(t, 200, "", "POST", M+"/rental/quote", map[string]any{
		"unit_id": u.Id, "formula_id": morning.Id, "skipper_id": k.Id, "start": pt("2027-06-12", 16), "end": pt("2027-06-12", 26),
		"extras": []map[string]any{{"extra_id": snk.Id, "qty_milli": 1000}},
	}, &q)
	if !q.Items[0].Ok || q.Items[0].TotalCents != 26000 || len(q.Items[0].Lines) != 3 || q.Items[0].LicenceRequired {
		t.Fatalf("E3 %+v", q)
	}
	rc.expect(t, 200, "", "POST", M+"/rental/quote", map[string]any{"unit_id": u.Id, "start": pt("2027-06-12", 16), "end": pt("2027-06-12", 38)}, &q)
	if len(q.Items) != 4 || !q.Items[0].Ok || q.Items[0].FormulaCode != "GIORNATA" || q.Items[0].TotalCents != 24000 {
		t.Fatalf("all formulas %+v", q)
	}
	// hourly 08:00-19:00 = 22 slots x 45.00 / 2 = 495.00; half day and week refuse the range.
	if !q.Items[1].Ok || q.Items[1].FormulaCode != "ORARIA" || q.Items[1].TotalCents != 49500 || q.Items[2].Ok || q.Items[3].Ok {
		t.Fatalf("alternatives %+v", q.Items)
	}
	rc.expect(t, 200, "", "POST", M+"/rental/quote", map[string]any{"unit_id": u.Id, "formula_id": morning.Id, "start": pt("2027-06-12", 17), "end": pt("2027-06-12", 26)}, &q)
	if q.Items[0].Ok || q.Items[0].Problem == nil || *q.Items[0].Problem != "formula_range_mismatch" {
		t.Fatalf("mismatch %+v", q)
	}
	rc.expect(t, 200, "", "POST", M+"/rental/quote", map[string]any{"unit_id": u.Id, "formula_id": week.Id, "start": pt("2028-06-12", 16), "end": pt("2028-06-18", 38)}, &q)
	if q.Items[0].Ok || *q.Items[0].Problem != "no_price" {
		t.Fatalf("no price %+v", q)
	}
	rc.expect(t, 400, "bad_request", "POST", M+"/rental/quote", map[string]any{"unit_id": u.Id, "start": pt("bad", 1), "end": pt("2027-06-12", 2)}, nil)
	fs.other.expect(t, 404, "not_found", "POST", M+"/rental/quote", map[string]any{"unit_id": u.Id, "start": pt("2027-06-12", 16), "end": pt("2027-06-12", 26)}, nil)
	fs.other.expect(t, 404, "not_found", "POST", X+"/rental/quote", map[string]any{"unit_id": u.Id, "start": pt("2027-06-12", 16), "end": pt("2027-06-12", 26)}, nil)
}
