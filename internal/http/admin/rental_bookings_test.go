//go:build integration

package admin_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

// bookingTSetup is the fleet setup with a priced unit (GOM58, 40 kW), a second unit, a skipper and the ORARIA
// (skipper optional) and MEZZA-MATTINA formulas.
type bookingTSetup struct {
	*fleetTSetup
	unit, unit2, skipper string
	hourly, morning      string
}

func newBookingTSetup(t *testing.T) *bookingTSetup {
	t.Helper()
	fs := newFleetTSetup(t)
	bs := &bookingTSetup{fleetTSetup: fs}
	c, M := fs.owner, fs.M
	var set api.RentalSettings
	c.expect(t, 200, "", "GET", M+"/rental/settings", nil, &set)
	c.expect(t, 200, "", "PUT", M+"/rental/settings", map[string]any{
		"open_from_slot": 16, "open_to_slot": 38, "bands": set.Bands, "buffer_slots": 1, "desk_hold_minutes": 60,
		"no_show_after_minutes": 60, "licence_threshold_kw_milli": 30000, "skipper_item_id": fs.skp, "checklist": set.Checklist,
		"checkout_min_photos": 2, "checkin_min_photos": 1, "deposit_modes": []string{"cash", "card_pos"}, "identity_retention_days": 365,
	}, nil, "If-Match", etag(0))
	var u api.RentalUnit
	c.expect(t, 201, "", "POST", M+"/rental/units", map[string]any{
		"vessel_id": fs.vessel40, "code": "GOM58", "category": "rib", "seats": 8, "fuel_type": "petrol", "tank_milli": 60000,
		"deposit_cents": 50000, "item_id": fs.gom,
	}, &u)
	bs.unit = u.Id
	var v api.Vessel
	c.expect(t, 201, "", "POST", M+"/vessels", map[string]any{"name": "Gabbiano 2", "loa_cm": 580, "beam_cm": 240, "hull_type": "rib"}, &v)
	c.expect(t, 201, "", "POST", M+"/rental/units", map[string]any{
		"vessel_id": v.Id, "code": "GOM2", "category": "rib", "seats": 6, "fuel_type": "petrol", "tank_milli": 60000, "item_id": fs.gom,
	}, &u)
	bs.unit2 = u.Id
	var k api.RentalSkipper
	c.expect(t, 201, "", "POST", M+"/rental/skippers", map[string]any{"name": "Marco", "licence_expires_on": "2030-12-31"}, &k)
	bs.skipper = k.Id
	var f api.RentalFormula
	c.expect(t, 201, "", "POST", M+"/rental/formulas", map[string]any{"code": "ORARIA", "name": map[string]string{"it": "Oraria"}, "kind": "hourly", "skipper": "optional"}, &f)
	bs.hourly = f.Id
	c.expect(t, 201, "", "POST", M+"/rental/formulas", map[string]any{"code": "MEZZA-MATTINA", "name": map[string]string{"it": "Mattina"}, "kind": "half_day", "band_code": "morning", "skipper": "optional"}, &f)
	bs.morning = f.Id
	return bs
}

func pt(date string, slot int) map[string]any { return map[string]any{"date": date, "slot": slot} }

func TestRentalBookingHandlers_Scenario(t *testing.T) {
	bs := newBookingTSetup(t)
	c, rc, M, X := bs.owner, bs.rental, bs.M, bs.otherM

	// E1: hourly 2027-06-12 10:00-13:00 = 3 x 45.00, held.
	var h api.RentalBooking
	r := rc.expect(t, 201, "", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.hourly, "start": pt("2027-06-12", 20), "end": pt("2027-06-12", 26), "hold": true,
		"renter": map[string]string{"name": "Ada Rossi", "phone": "+39 333 0000000"},
	}, &h)
	if h.Status != "held" || h.TotalCents != 13500 || !strings.HasPrefix(h.Number, "NL") || !strings.HasSuffix(h.Number, "-000001") || h.HoldExpiresAt == nil ||
		h.UnitCode != "GOM58" || h.Kind != "hourly" || len(h.Lines) != 1 || h.Renter == nil || *h.Renter.Name != "Ada Rossi" {
		t.Fatalf("held %+v", h)
	}
	if r.header.Get("ETag") == "" {
		t.Fatal("no ETag")
	}
	if !h.LicenceRequired || h.Deposit.AmountCents != 50000 || h.Deposit.Status != "none" || h.FiscalMode != "receipt" {
		t.Fatalf("licence/deposit %+v", h)
	}
	// Overlap: 409 with the conflicting booking.
	r = rc.expect(t, 409, "rental_slot_taken", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.hourly, "start": pt("2027-06-12", 24), "end": pt("2027-06-12", 28),
	}, nil)
	var prob struct {
		Details struct {
			Target     string   `json:"target"`
			BookingIDs []string `json:"booking_ids"`
			Slots      []int    `json:"slots"`
		} `json:"details"`
	}
	if err := json.Unmarshal(r.body, &prob); err != nil || prob.Details.Target != "unit" || len(prob.Details.BookingIDs) != 1 || prob.Details.BookingIDs[0] != h.Id {
		t.Fatalf("conflict body %s", r.body)
	}
	rc.expect(t, 422, "formula_range_mismatch", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.morning, "start": pt("2027-06-13", 17), "end": pt("2027-06-13", 26),
	}, nil)
	rc.expect(t, 422, "outside_rental_hours", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.hourly, "start": pt("2027-06-13", 10), "end": pt("2027-06-13", 14),
	}, nil)
	rc.expect(t, 400, "validation_failed", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.hourly, "start": pt("2027-13-01", 20), "end": pt("2027-06-13", 24),
	}, nil)
	rc.expect(t, 422, "customer_required", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.hourly, "start": pt("2027-06-13", 20), "end": pt("2027-06-13", 24), "invoice_requested": true,
	}, nil)

	hid := h.Id
	// Confirm (If-Match), then the transition table refuses a second confirm.
	rc.expect(t, 428, "precondition_required", "POST", M+"/rental/bookings/"+h.Id+"/transition", map[string]any{"action": "confirm"}, nil)
	rc.expect(t, 412, "version_conflict", "POST", M+"/rental/bookings/"+h.Id+"/transition", map[string]any{"action": "confirm"}, nil, "If-Match", etag(int(h.Version)+3))
	ver := int(h.Version)
	h = api.RentalBooking{}
	rc.expect(t, 200, "", "POST", M+"/rental/bookings/"+hid+"/transition", map[string]any{"action": "confirm"}, &h, "If-Match", etag(ver))
	if h.Status != "confirmed" || h.HoldExpiresAt != nil {
		t.Fatalf("confirmed %+v", h)
	}
	rc.expect(t, 409, "invalid_transition", "POST", M+"/rental/bookings/"+h.Id+"/transition", map[string]any{"action": "confirm"}, nil, "If-Match", etag(int(h.Version)))
	rc.expect(t, 400, "validation_failed", "POST", M+"/rental/bookings/"+h.Id+"/transition", map[string]any{"action": "fly"}, nil, "If-Match", etag(int(h.Version)))

	// Half day morning with skipper (E3 without extras: 150 + 100); then the skipper is busy on another unit.
	var hd api.RentalBooking
	rc.expect(t, 201, "", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit2, "formula_id": bs.morning, "skipper_id": bs.skipper, "start": pt("2027-06-14", 16), "end": pt("2027-06-14", 26),
	}, &hd)
	if hd.TotalCents != 25000 || hd.Status != "confirmed" || len(hd.Lines) != 2 || hd.LicenceRequired {
		t.Fatalf("half day %+v", hd)
	}
	rc.expect(t, 409, "skipper_busy", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.hourly, "skipper_id": bs.skipper, "start": pt("2027-06-14", 20), "end": pt("2027-06-14", 24),
	}, nil)

	// Reschedule (PATCH, If-Match): to 14:00-16:00 = 2 x 45.00.
	rc.expect(t, 428, "precondition_required", "PATCH", M+"/rental/bookings/"+h.Id, map[string]any{"note": "x"}, nil)
	rc.expect(t, 200, "", "PATCH", M+"/rental/bookings/"+h.Id, map[string]any{
		"start": pt("2027-06-12", 28), "end": pt("2027-06-12", 32), "note": "chiavi al bar",
	}, &h, "If-Match", etag(int(h.Version)))
	if h.Start.Slot != 28 || h.TotalCents != 9000 || h.Note != "chiavi al bar" {
		t.Fatalf("rescheduled %+v", h)
	}
	rc.expect(t, 400, "validation_failed", "PATCH", M+"/rental/bookings/"+h.Id, map[string]any{"start": pt("2027-06-12", 28)}, nil, "If-Match", etag(int(h.Version)))
	rc.expect(t, 403, "forbidden", "PATCH", M+"/rental/bookings/"+h.Id, map[string]any{"start": pt("2027-06-12", 30), "end": pt("2027-06-12", 34), "keep_price": true}, nil, "If-Match", etag(int(h.Version)))

	// Reads.
	var got api.RentalBooking
	rc.expect(t, 200, "", "GET", M+"/rental/bookings/"+h.Id, nil, &got)
	if got.Version != h.Version || got.Renter == nil {
		t.Fatalf("get %+v", got)
	}
	var list api.RentalBookingList
	rc.expect(t, 200, "", "GET", M+"/rental/bookings?status=confirmed&limit=1", nil, &list)
	if len(list.Items) != 1 || list.NextCursor == nil {
		t.Fatalf("list %+v", list)
	}
	rc.expect(t, 200, "", "GET", M+"/rental/bookings?cursor="+*list.NextCursor+"&status=confirmed", nil, &list)
	if len(list.Items) != 1 {
		t.Fatalf("page 2 %+v", list)
	}
	rc.expect(t, 400, "validation_failed", "GET", M+"/rental/bookings?from=nope", nil, nil)

	var pl api.RentalPlanner
	rc.expect(t, 400, "validation_failed", "GET", M+"/rental/planner", nil, nil)
	rc.expect(t, 200, "", "GET", M+"/rental/planner?from=2027-06-12&to=2027-06-14&view=hours", nil, &pl)
	if len(pl.Rows) != 3 || pl.OpenFromSlot != 16 || len(pl.Bands) != 2 || len(pl.Rows[0].Segments) != 2 || pl.Rows[2].Kind != "skipper" {
		t.Fatalf("planner %+v", pl)
	}
	var av api.RentalAvailability
	rc.expect(t, 400, "validation_failed", "GET", M+"/rental/availability?starts_on=2027-06-12", nil, nil)
	rc.expect(t, 200, "", "GET", M+"/rental/availability?starts_on=2027-06-12&start_slot=28&ends_on=2027-06-12&end_slot=32&skipper=true", nil, &av)
	if len(av.Units) != 1 || av.Units[0].UnitId != bs.unit2 || len(av.Skippers) != 1 {
		t.Fatalf("availability %+v", av)
	}
	var day api.RentalDay
	rc.expect(t, 200, "", "GET", M+"/rental/day?date=2027-06-12", nil, &day)
	if day.Date != "2027-06-12" || len(day.Pickups) != 1 || day.Pickups[0].RenterName == nil {
		t.Fatalf("day %+v", day)
	}
	rc.expect(t, 400, "validation_failed", "GET", M+"/rental/day?date=x", nil, nil)

	// Blocks.
	var blk api.RentalBlock
	rc.expect(t, 409, "rental_slot_taken", "POST", M+"/rental/blocks", map[string]any{
		"unit_id": bs.unit, "from": pt("2027-06-12", 0), "to": pt("2027-06-12", 48), "reason": "maintenance",
	}, nil)
	rc.expect(t, 201, "", "POST", M+"/rental/blocks", map[string]any{
		"unit_id": bs.unit, "from": pt("2027-06-20", 0), "to": pt("2027-06-21", 48), "reason": "maintenance", "note": "tagliando",
	}, &blk)
	rc.expect(t, 200, "", "PATCH", M+"/rental/blocks/"+blk.Id, map[string]any{"note": "tagliando 100h"}, &blk, "If-Match", etag(int(blk.Version)))
	var bl api.RentalBlockList
	rc.expect(t, 200, "", "GET", M+"/rental/blocks?from=2027-06-01&to=2027-06-30", nil, &bl)
	if len(bl.Items) != 1 || bl.Items[0].Note != "tagliando 100h" {
		t.Fatalf("blocks %+v", bl)
	}
	rc.expect(t, 409, "rental_slot_taken", "POST", M+"/rental/bookings", map[string]any{
		"unit_id": bs.unit, "formula_id": bs.hourly, "start": pt("2027-06-20", 20), "end": pt("2027-06-20", 24),
	}, nil)
	rc.expect(t, 428, "precondition_required", "DELETE", M+"/rental/blocks/"+blk.Id, nil, nil)
	rc.expect(t, 204, "", "DELETE", M+"/rental/blocks/"+blk.Id, nil, nil, "If-Match", etag(int(blk.Version)))

	// Cancel.
	rc.expect(t, 200, "", "POST", M+"/rental/bookings/"+hd.Id+"/transition", map[string]any{"action": "cancel", "reason": "meteo"}, &hd, "If-Match", etag(int(hd.Version)))
	if hd.Status != "cancelled" || hd.CancelReason == nil || *hd.CancelReason != "meteo" {
		t.Fatalf("cancelled %+v", hd)
	}

	// Another marina: 404 for its own path with our ids, and for our path.
	bs.other.expect(t, 404, "not_found", "GET", M+"/rental/bookings/"+h.Id, nil, nil)
	bs.other.expect(t, 404, "not_found", "GET", X+"/rental/bookings/"+h.Id, nil, nil)
	bs.other.expect(t, 404, "not_found", "GET", M+"/rental/planner", nil, nil)
	bs.other.expect(t, 404, "not_found", "POST", X+"/rental/bookings/"+h.Id+"/transition", map[string]any{"action": "cancel"}, nil, "If-Match", etag(1))
	bs.other.expect(t, 404, "not_found", "PATCH", X+"/rental/blocks/"+blk.Id, map[string]any{"note": "x"}, nil, "If-Match", etag(1))
	_ = c
}
