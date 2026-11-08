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

// M10 reports and tiles (m10-plan §12, INV-62): on the seeded demo, through the API only, the three yard tiles equal
// the records of their drill views (open orders = count, hours = Σ hours, margin = Σ margins of the orders invoiced in
// the period); the yard report needs reports.yard or reports.all (yard_mgr yes, office no); every view answers;
// invalid filters are 422 and another marina 404.

func TestAcceptance_M10_Reports(t *testing.T) {
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
			field := map[string]string{"yard_open_orders": "", "yard_hours": "hours_milli", "yard_margin": "margin_cents"}
			f, ok := field[key]
			if !ok {
				continue
			}
			seen[key] = true
			if tile["available"] != true {
				t.Errorf("%s: unavailable", key)
				continue
			}
			drill, _ := tile["drill"].(map[string]any)
			body := owner.expect(t, http.StatusOK, "GET", h.drillURL(drill, from, to, url.Values{"page_size": {"1000"}}), nil)
			recs, _ := body["records"].([]any)
			var sum int64
			for _, r := range recs {
				if f == "" {
					sum++
					continue
				}
				sum += m7num(r.(map[string]any), f)
			}
			if sum != m7num(tile, "value") {
				t.Errorf("%s: Σ records %d != tile %v", key, sum, tile["value"])
			}
			if r := owner.do(t, "GET", h.drillURL(drill, from, to, url.Values{"format": {"csv"}}), nil); r.status != http.StatusOK {
				t.Errorf("%s csv: %d", key, r.status)
			}
		}
		if len(seen) != 3 {
			t.Errorf("yard tiles %v", seen)
		}
		mgr := h.login(t, auth.RoleYardMgr)
		for _, view := range []string{"hours", "load", "margins", "margins_by_work_type", "lifting"} {
			body := mgr.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/reports/yard?view="+view, nil)
			if str(body, "view") != view {
				t.Errorf("view %s: %v", view, body["view"])
			}
		}
		mgr.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/reports/yard?view=load&group_by=bay", nil)
		office := h.login(t, auth.RoleOffice)
		office.expect(t, http.StatusForbidden, "GET", A+"/marinas/"+h.marina+"/reports/yard", nil)
		od := office.expect(t, http.StatusOK, "GET", A+"/marinas/"+h.marina+"/dashboard", nil)
		for _, x := range od["tiles"].([]any) {
			if k := str(x.(map[string]any), "key"); k == "yard_hours" || k == "yard_margin" || k == "yard_open_orders" {
				t.Errorf("office sees %s", k)
			}
		}
		owner.expect(t, http.StatusUnprocessableEntity, "GET", A+"/marinas/"+h.marina+"/reports/yard?view=hours&group_by=equipment", nil)
		owner.expect(t, http.StatusUnprocessableEntity, "GET", A+"/marinas/"+h.marina+"/reports/yard?view=unknown", nil)
		owner.expect(t, http.StatusNotFound, "GET", A+"/marinas/"+store.NewID()+"/reports/yard", nil)
	})
}

// m10RecordSum adds the value of one record of a yard drill view to the tile sums (called by recordSum).
func m10RecordSum(key string, r map[string]any, value *int64) {
	switch key {
	case "yard_open_orders":
		*value++
	case "yard_hours":
		*value += m7num(r, "hours_milli")
	case "yard_margin":
		*value += m7num(r, "margin_cents")
	}
}
