//go:build integration

package acceptance

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M9 reports and tiles (m9-plan §11, §14.4, INV-62): on the seeded demo, through the API only, every M9 tile
// equals the records of its drill report, the blocked exits tile keeps its own 30-day period, the money columns
// follow reports.all and another marina is not found.

func init() {
	tileScale["storage_occupancy"] = 10000
}

// m9RecordSum adds the value of one record of an M9 drill report to the tile sums (called by recordSum).
func m9RecordSum(key string, r map[string]any, value, numer *int64) {
	switch key {
	case "storage_occupancy":
		*numer += m7num(r, "footprint_m2_nights_milli")
	case "storage_exits_blocked":
		if r["exit_blocked"] == true {
			*value++
		}
	case "maintenance_overdue":
		if m7num(r, "overdue_days") > 0 && r["status"] != "done" && r["status"] != "cancelled" {
			*value++
		}
	case "utility_unbilled":
		// The pending value is not a record column: the tile is checked against the totals of the drill.
	}
}

// m9TotalsDenom reads the denominator that is not a record value (the usable surface of the storage areas).
func m9TotalsDenom(key string, body map[string]any, denom *int64) {
	if key != "storage_occupancy" {
		return
	}
	tot, _ := body["totals"].(map[string]any)
	*denom = m7num(tot, "available_m2_nights_milli")
}

func TestAcceptance_M9_ReportsTiles(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		h := newM7Harness(t, db)
		owner := h.login(t, auth.RoleOwner)
		from, to := calendar.New(h.today.Year(), time.January, 1), h.today
		v := url.Values{"from": {from.String()}, "to": {to.String()}, "compare": {"none"}}
		dash := owner.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/dashboard?"+v.Encode(), nil)
		seen := map[string]bool{}
		for _, x := range dash["tiles"].([]any) {
			tile := x.(map[string]any)
			key := str(tile, "key")
			switch key {
			case "storage_occupancy", "storage_exits_blocked", "maintenance_overdue", "utility_unbilled":
			default:
				continue
			}
			seen[key] = true
			if tile["available"] != true {
				t.Errorf("%s: unavailable", key)
				continue
			}
			drill, _ := tile["drill"].(map[string]any)
			if key == "storage_exits_blocked" {
				params, _ := drill["params"].(map[string]any)
				if params["from"] != h.today.String() || params["to"] != h.today.AddDays(29).String() {
					t.Errorf("exits tile period %v", params)
				}
			}
			if key == "utility_unbilled" {
				body := owner.expect(t, http.StatusOK, "GET", h.drillURL(drill, from, to, nil), nil)
				tot, _ := body["totals"].(map[string]any)
				if m7num(tot, "pending_value_cents") != m7num(tile, "value") {
					t.Errorf("utility_unbilled: totals %v != tile %v", tot["pending_value_cents"], tile["value"])
				}
				continue
			}
			h.checkTile(t, owner, "m9", key, tile, drill, from, to)
		}
		if len(seen) != 4 {
			t.Errorf("M9 tiles = %v", seen)
		}

		// Operational roles see the M9 reports without money; the unbilled tile needs reports.all.
		office := h.login(t, auth.RoleOffice)
		for _, k := range []string{"storage", "maintenance", "utilities"} {
			body := office.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/reports/"+k, nil)
			tot, _ := body["totals"].(map[string]any)
			for _, money := range []string{"production_cents", "external_cost_cents", "charged_cents", "pending_value_cents"} {
				if _, ok := tot[money]; ok {
					t.Errorf("office %s report has %s", k, money)
				}
			}
		}
		od := office.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/dashboard", nil)
		for _, x := range od["tiles"].([]any) {
			if tile := x.(map[string]any); str(tile, "key") == "utility_unbilled" && tile["available"] == true {
				t.Error("office sees the unbilled utilities tile")
			}
		}
		// Invalid filters are 422, another marina 404.
		owner.expect(t, http.StatusUnprocessableEntity, "GET", A+"/marinas/"+h.marina+"/reports/utilities?kind=mooring", nil)
		owner.expect(t, http.StatusNotFound, "GET", A+"/marinas/"+store.NewID()+"/reports/storage", nil)
	})
}
