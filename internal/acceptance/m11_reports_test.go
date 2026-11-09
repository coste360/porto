//go:build integration

package acceptance

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M11 reports and tiles (m11-plan §11, INV-62, INV-98): on the seeded demo, through the API only, the two rental
// tiles equal the records of their drill views (utilisation = Σ rented / Σ available slots, revenue = Σ net of the
// rental-origin charge lines); the revenue view never carries a deposit retention; the rental report needs
// reports.rental or reports.all (rental and viewer yes, office no) and the rental tiles follow it; every view answers
// and exports; invalid filters are 422 and another marina 404.

func init() {
	// The generic M7 tile check recomputes rental_utilisation as a ratio in basis points.
	tileScale["rental_utilisation"] = 10000
}

// m11RecordSum adds one record of a rental drill view to the tile sums (for the generic recordSum).
func m11RecordSum(key string, r map[string]any, value, numer, denom *int64) {
	switch key {
	case "rental_utilisation":
		*numer += m7num(r, "rented_slots")
		*denom += m7num(r, "available_slots")
	case "rental_revenue":
		*value += m7num(r, "net_cents")
	}
}

func TestAcceptance_M11_Reports(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newM7Harness(t, db)
		owner := h.login(t, auth.RoleOwner)
		M := A + "/marinas/" + h.marina
		for name, pr := range map[string][2]calendar.Date{
			"ytd":  {calendar.New(h.today.Year(), time.January, 1), h.today},
			"next": {h.today, h.today.AddDays(30)},
		} {
			from, to := pr[0], pr[1]
			v := url.Values{"from": {from.String()}, "to": {to.String()}, "compare": {"none"}}
			dash := owner.expect(t, http.StatusOK, "GET", M+"/dashboard?"+v.Encode(), nil)
			seen := map[string]bool{}
			for _, x := range dash["tiles"].([]any) {
				tile := x.(map[string]any)
				key := str(tile, "key")
				if key != "rental_utilisation" && key != "rental_revenue" {
					continue
				}
				seen[key] = true
				if tile["available"] != true {
					t.Errorf("%s %s: unavailable (%v)", name, key, tile["reason"])
					continue
				}
				drill, _ := tile["drill"].(map[string]any)
				body := owner.expect(t, http.StatusOK, "GET", h.drillURL(drill, from, to, url.Values{"page_size": {"1000"}}), nil)
				var value, numer, denom int64
				for _, r := range body["records"].([]any) {
					m11RecordSum(key, r.(map[string]any), &value, &numer, &denom)
				}
				if key == "rental_utilisation" {
					if m7num(tile, "numerator") != numer || m7num(tile, "denominator") != denom {
						t.Errorf("%s utilisation: records %d/%d != tile %v/%v", name, numer, denom, tile["numerator"], tile["denominator"])
					}
					if want, ok := m7ratio(numer, denom, 10000); ok && m7num(tile, "value") != want {
						t.Errorf("%s utilisation: value %v != %d", name, tile["value"], want)
					}
				} else if m7num(tile, "value") != value {
					t.Errorf("%s revenue: Σ records %d != tile %v", name, value, tile["value"])
				}
				if r := owner.do(t, "GET", h.drillURL(drill, from, to, url.Values{"format": {"csv"}}), nil); r.status != http.StatusOK {
					t.Errorf("%s %s csv: %d", name, key, r.status)
				}
				if r := owner.do(t, "GET", h.drillURL(drill, from, to, url.Values{"format": {"xlsx"}}), nil); r.status != http.StatusOK {
					t.Errorf("%s %s xlsx: %d", name, key, r.status)
				}
			}
			if len(seen) != 2 {
				t.Errorf("%s rental tiles %v", name, seen)
			}
		}

		// INV-98: the revenue view only carries rental-origin charges (never a deposit retention).
		from := calendar.New(h.today.Year(), time.January, 1)
		rev := owner.expect(t, http.StatusOK, "GET", M+"/reports/rental?view=revenue&page_size=1000&from="+from.String()+"&to="+h.today.AddDays(30).String(), nil)
		for _, x := range rev["records"].([]any) {
			id := str(x.(map[string]any), "charge_id")
			var origin string
			if err := db.NewSelect().Table("charges").Column("origin_type").Where("id = ?", id).Scan(context.Background(), &origin); err != nil {
				t.Fatal(err)
			}
			if origin != store.ChargeOriginRental {
				t.Errorf("revenue record of a %s charge", origin)
			}
		}

		// Permissions: rental and viewer read the report; office (reports.operational only) does not, and its dashboard
		// has no rental tiles.
		rental := h.login(t, auth.RoleRental)
		viewer := h.login(t, auth.RoleViewer)
		for _, view := range []string{"utilisation", "revenue", "damages", "deposits"} {
			body := rental.expect(t, http.StatusOK, "GET", M+"/reports/rental?view="+view, nil)
			if str(body, "view") != view {
				t.Errorf("view %s: %v", view, body["view"])
			}
			viewer.expect(t, http.StatusOK, "GET", M+"/reports/rental?view="+view+"&group_by=unit", nil)
		}
		rental.expect(t, http.StatusOK, "GET", M+"/reports/rental?view=revenue&group_by=line_kind", nil)
		office := h.login(t, auth.RoleOffice)
		office.expect(t, http.StatusForbidden, "GET", M+"/reports/rental", nil)
		od := office.expect(t, http.StatusOK, "GET", M+"/dashboard", nil)
		for _, x := range od["tiles"].([]any) {
			if k := str(x.(map[string]any), "key"); k == "rental_utilisation" || k == "rental_revenue" {
				t.Errorf("office sees %s", k)
			}
		}
		owner.expect(t, http.StatusUnprocessableEntity, "GET", M+"/reports/rental?view=utilisation&group_by=formula", nil)
		owner.expect(t, http.StatusUnprocessableEntity, "GET", M+"/reports/rental?view=unknown", nil)
		owner.expect(t, http.StatusNotFound, "GET", A+"/marinas/"+store.NewID()+"/reports/rental", nil)
	})
}
