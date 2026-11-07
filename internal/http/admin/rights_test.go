//go:build integration

package admin_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

// rightsSetup is the occupancy setup plus two customers.
type rightsSetup struct {
	*occSetup
	customers []string
}

func newRightsSetup(t *testing.T) *rightsSetup {
	t.Helper()
	o := newOccSetup(t)
	rs := &rightsSetup{occSetup: o}
	for _, n := range []string{"Rossi", "Bianchi"} {
		var cu struct {
			Customer struct {
				ID string `json:"id"`
			} `json:"customer"`
		}
		o.owner.expect(t, 201, "", "POST", o.M+"/customers", map[string]any{"kind": "person", "first_name": "Mario", "last_name": n}, &cu)
		rs.customers = append(rs.customers, cu.Customer.ID)
	}
	return rs
}

func (o *rightsSetup) year() int {
	y, _ := strconv.Atoi(o.day(0)[:4])
	return y
}

func TestRightsHandlers_Scenario(t *testing.T) {
	o := newRightsSetup(t)
	c, M, X := o.owner, o.M, o.otherM
	end := o.day(400)
	body := map[string]any{
		"berth_id": o.berths[0], "kind": "ownership_share", "from_on": o.day(0), "to_on": end, "title": map[string]any{"deed": "atto"},
		"holders": []map[string]any{{"customer_id": o.customers[0], "share_millis": 500}, {"customer_id": o.customers[1], "share_millis": 500}},
		"vessels": []map[string]any{{"vessel_id": o.vessels[0], "primary": true}},
	}
	// validation and permissions
	bad := map[string]any{
		"berth_id": o.berths[0], "kind": "ownership_share", "from_on": o.day(0), "to_on": end, "title": map[string]any{},
		"holders": []map[string]any{{"customer_id": o.customers[0], "share_millis": 999}},
	}
	c.expect(t, 422, "shares_invalid", "POST", M+"/rights", bad, nil)
	o.dock.expect(t, 403, "forbidden", "POST", M+"/rights", body, nil)
	o.other.expect(t, 404, "not_found", "POST", M+"/rights", body, nil)
	var r api.Right
	resp := c.expect(t, 201, "", "POST", M+"/rights", body, &r)
	if resp.header.Get("ETag") != etag(1) || len(r.Holders) != 2 || r.Holders[0].Customer.DisplayName == nil || len(*r.Allocations) != 1 {
		t.Fatalf("right %+v", r)
	}
	c.expect(t, 409, "resource_taken", "POST", M+"/rights", body, nil)
	c.expect(t, 200, "", "GET", M+"/rights/"+r.Id, nil, nil)
	o.other.expect(t, 404, "not_found", "GET", X+"/rights/"+r.Id, nil, nil)
	o.other.expect(t, 404, "not_found", "GET", M+"/rights/"+r.Id, nil, nil)
	var page api.RightPage
	c.expect(t, 200, "", "GET", M+"/rights?customer_id="+o.customers[1], nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("by customer %d", len(page.Items))
	}
	c.expect(t, 428, "precondition_required", "PATCH", M+"/rights/"+r.Id, map[string]any{"title": map[string]any{"deed": "x"}}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", M+"/rights/"+r.Id, map[string]any{"title": map[string]any{"deed": "x"}}, nil, "If-Match", etag(9))
	c.expect(t, 200, "", "PATCH", M+"/rights/"+r.Id, map[string]any{"title": map[string]any{"deed": "x"}}, &r, "If-Match", etag(1))

	// rights lens: holder on A-1, marina elsewhere, no names
	var lens api.MapRights
	c.expect(t, 200, "", "GET", M+"/maps/"+o.mapID+"/rights", nil, &lens)
	holder := 0
	for _, it := range lens.Items {
		if it.Status == api.MapRightStatusHolder {
			holder++
		}
	}
	if holder != 1 || len(lens.Items) != 8 {
		t.Fatalf("lens %+v", lens)
	}
	o.other.expect(t, 404, "not_found", "GET", M+"/maps/"+o.mapID+"/rights", nil, nil)

	// mandate, statement, accrual
	var m api.Mandate
	c.expect(t, 201, "", "POST", M+"/mandates", map[string]any{
		"right_id": r.Id, "from_on": o.day(10), "to_on": o.day(40), "owner_share_bp": 6000,
		"channels": []string{"transit"}, "settlement": "credit",
	}, &m)
	c.expect(t, 409, "mandate_overlap", "POST", M+"/mandates", map[string]any{
		"right_id": r.Id, "from_on": o.day(20), "to_on": o.day(50), "owner_share_bp": 6000,
		"channels": []string{"transit"}, "settlement": "credit",
	}, nil)
	var st api.MandateStatement
	c.expect(t, 200, "", "GET", M+"/mandates/"+m.Id+"/statement", nil, &st)
	csvResp := c.expect(t, 200, "", "GET", M+"/mandates/"+m.Id+"/statement?format=csv", nil, nil)
	if !strings.HasPrefix(csvResp.header.Get("Content-Type"), "text/csv") {
		t.Fatalf("csv content type %s", csvResp.header.Get("Content-Type"))
	}
	var acc api.AccrueResult
	c.expect(t, 200, "", "POST", M+"/mandates/accrue", nil, &acc)
	c.expect(t, 200, "", "POST", M+"/mandates/"+m.Id+"/end", map[string]any{"on": o.day(20)}, &m, "If-Match", etag(m.Version))
	if m.Status != "ended" {
		t.Fatalf("mandate %+v", m)
	}

	// transfer on paper
	var tr api.RightTransfer
	c.expect(t, 201, "", "POST", M+"/right-transfers", map[string]any{
		"right_id": r.Id, "effective_on": o.day(60),
		"to_holders": []map[string]any{{"customer_id": o.customers[1], "share_millis": 1000}}, "fees_policy": "reassign",
	}, &tr)
	c.expect(t, 200, "", "POST", M+"/right-transfers/"+tr.Id+"/complete", nil, &tr, "If-Match", etag(tr.Version))
	if tr.Status != "completed" || tr.NewRightId == nil {
		t.Fatalf("transfer %+v", tr)
	}
	var hist api.RightHistory
	c.expect(t, 200, "", "GET", M+"/berths/"+o.berths[0]+"/right-history", nil, &hist)
	if len(hist.Items) != 2 || hist.Items[1].TransferId == nil {
		t.Fatalf("history %+v", hist)
	}
	o.other.expect(t, 404, "not_found", "GET", M+"/berths/"+o.berths[0]+"/right-history", nil, nil)

	// end and cancel
	var nr api.Right
	c.expect(t, 200, "", "GET", M+"/rights/"+*tr.NewRightId, nil, &nr)
	c.expect(t, 200, "", "POST", M+"/rights/"+nr.Id+"/cancel", map[string]any{"on": o.day(0), "reason": "errore"}, &nr, "If-Match", etag(nr.Version))
	c.expect(t, 409, "invalid_transition", "POST", M+"/rights/"+nr.Id+"/cancel", map[string]any{"on": o.day(0), "reason": "errore"}, nil, "If-Match", etag(nr.Version))
}

func TestFeeHandlers_Scenario(t *testing.T) {
	o := newRightsSetup(t)
	c, M := o.owner, o.M
	year := o.year()
	jan1 := strconv.Itoa(year) + "-01-01"
	c.expect(t, 201, "", "POST", M+"/rights", map[string]any{
		"berth_id": o.berths[0], "kind": "long_term_use", "from_on": jan1, "to_on": strconv.Itoa(year+2) + "-12-31", "title": map[string]any{},
		"holders": []map[string]any{{"customer_id": o.customers[0], "share_millis": 1000}},
	}, nil)
	var ft api.FeeTable
	c.expect(t, 400, "validation_failed", "POST", M+"/fee-tables", map[string]any{"year": year, "name": "x", "basis": "per_category", "rows": []map[string]any{{}}}, nil)
	c.expect(t, 201, "", "POST", M+"/fee-tables", map[string]any{
		"year": year, "name": "Oneri", "basis": "per_m2_berth", "istat_bp": 100,
		"rows": []map[string]any{{"rate_cents_per_m2": 1000}},
	}, &ft)
	c.expect(t, 409, "invalid_transition", "POST", M+"/fee-runs", map[string]any{"year": year, "table_id": ft.Id, "plan": map[string]any{}}, nil)
	c.expect(t, 200, "", "POST", M+"/fee-tables/"+ft.Id+"/activate", nil, &ft, "If-Match", etag(ft.Version))
	var run api.FeeRun
	c.expect(t, 201, "", "POST", M+"/fee-runs", map[string]any{"year": year, "table_id": ft.Id, "plan": map[string]any{"schedule": map[string]any{"mode": "single", "due_offset_days": 30}}}, &run)
	var sim api.FeeRunSimulation
	resp := c.expect(t, 200, "", "POST", M+"/fee-runs/"+run.Id+"/simulate", nil, &sim, "If-Match", etag(run.Version))
	if sim.TotalCents <= 0 || len(sim.Excluded) != 0 {
		t.Fatalf("simulation %+v", sim)
	}
	c.expect(t, 200, "", "POST", M+"/fee-runs/"+run.Id+"/issue", nil, &run, "If-Match", resp.header.Get("ETag"))
	c.expect(t, 409, "invalid_transition", "POST", M+"/fee-runs/"+run.Id+"/issue", nil, nil, "If-Match", etag(run.Version))
	var lines api.FeeLinePage
	c.expect(t, 200, "", "GET", M+"/fee-runs/"+run.Id+"/lines", nil, &lines)
	if len(lines.Items) != 1 || lines.Items[0].AmountCents != sim.TotalCents {
		t.Fatalf("lines %+v", lines)
	}
	c.expect(t, 200, "", "GET", M+"/fee-runs/"+run.Id+"/lines?format=csv", nil, nil)
	var hi api.HolderInstallmentPage
	c.expect(t, 200, "", "GET", M+"/holder-installments?source_type=management_fee", nil, &hi)
	if len(hi.Items) != 1 || hi.Items[0].AmountCents != sim.TotalCents {
		t.Fatalf("holder instalments %+v", hi)
	}
	h := hi.Items[0]
	o.dock.expect(t, 403, "forbidden", "POST", M+"/holder-installments/"+h.Id+"/settle", map[string]any{"method": "cash"}, nil, "If-Match", etag(h.Version))
	o.other.expect(t, 404, "not_found", "POST", M+"/holder-installments/"+h.Id+"/settle", map[string]any{"method": "cash"}, nil, "If-Match", etag(h.Version))
	c.expect(t, 200, "", "POST", M+"/holder-installments/"+h.Id+"/settle", map[string]any{"method": "bank_transfer", "reference": "CRO"}, &h, "If-Match", etag(h.Version))
	if h.Status != "paid" {
		t.Fatalf("settled %+v", h)
	}
	var bulk api.BulkSettleResult
	c.expect(t, 409, "conflict", "POST", M+"/holder-installments/settle", map[string]any{"items": []map[string]any{{"id": h.Id, "version": h.Version}}, "method": "cash"}, &bulk)
	c.expect(t, 200, "", "GET", M+"/holder-installments?format=csv", nil, nil)
	c.expect(t, 409, "invalid_transition", "DELETE", M+"/fee-tables/"+ft.Id, nil, nil, "If-Match", etag(ft.Version))
}

func TestCommonChargeHandlers_Scenario(t *testing.T) {
	o := newRightsSetup(t)
	c, M := o.owner, o.M
	year := o.year()
	c.expect(t, 201, "", "POST", M+"/rights", map[string]any{
		"berth_id": o.berths[0], "kind": "membership", "from_on": strconv.Itoa(year) + "-01-01", "to_on": strconv.Itoa(year+1) + "-12-31", "title": map[string]any{},
		"holders": []map[string]any{{"customer_id": o.customers[0], "share_millis": 333}, {"customer_id": o.customers[1], "share_millis": 667}},
	}, nil)
	var mt api.MillesimalTable
	c.expect(t, 201, "", "POST", M+"/millesimal-tables", map[string]any{"name": "Generale", "code": "GEN"}, &mt)
	c.expect(t, 409, "conflict", "POST", M+"/millesimal-tables", map[string]any{"name": "Generale", "code": "GEN"}, nil)
	var codes []string
	for i := range o.berths {
		codes = append(codes, "A-"+strconv.Itoa(i+1))
	}
	csvText := "berth;millis\n"
	for i, code := range codes {
		v := 125
		if i == 0 {
			v = 124
		}
		csvText += code + ";" + strconv.Itoa(v) + "\n"
	}
	var es api.MillesimalEntries
	resp := c.expect(t, 200, "", "PUT", M+"/millesimal-tables/"+mt.Id+"/entries", map[string]any{"csv": csvText}, &es, "If-Match", etag(mt.Version))
	if es.SumMillis != 999 || es.Rejected != nil {
		t.Fatalf("entries %+v", es)
	}
	c.expect(t, 422, "table_incomplete", "POST", M+"/millesimal-tables/"+mt.Id+"/activate", nil, nil, "If-Match", resp.header.Get("ETag"))
	resp = c.expect(t, 200, "", "PUT", M+"/millesimal-tables/"+mt.Id+"/entries", map[string]any{"csv": strings.Replace(csvText, ";124", ";125", 1)}, &es, "If-Match", resp.header.Get("ETag"))
	c.expect(t, 200, "", "POST", M+"/millesimal-tables/"+mt.Id+"/activate", nil, &mt, "If-Match", resp.header.Get("ETag"))
	c.expect(t, 409, "table_active", "PUT", M+"/millesimal-tables/"+mt.Id+"/entries", map[string]any{"csv": csvText}, nil, "If-Match", etag(mt.Version))
	c.expect(t, 200, "", "GET", M+"/millesimal-tables/"+mt.Id+"/entries?format=csv", nil, nil)

	var b api.Budget
	c.expect(t, 201, "", "POST", M+"/common-charge-budgets", map[string]any{"year": year, "name": "Bilancio"}, &b)
	c.expect(t, 409, "conflict", "POST", M+"/common-charge-budgets", map[string]any{"year": year}, nil)
	var it api.BudgetItem
	c.expect(t, 201, "", "POST", M+"/common-charge-budgets/"+b.Id+"/items", map[string]any{"name": "Pulizia", "table_id": mt.Id, "budget_cents": 1_234_567}, &it)
	c.expect(t, 200, "", "POST", M+"/common-charge-budgets/"+b.Id+"/approve", nil, &b, "If-Match", etag(b.Version))
	var run api.CommonChargeRun
	c.expect(t, 400, "validation_failed", "POST", M+"/common-charge-budgets/"+b.Id+"/runs", map[string]any{"kind": "advance"}, nil)
	c.expect(t, 201, "", "POST", M+"/common-charge-budgets/"+b.Id+"/runs", map[string]any{"kind": "advance", "share_bp": 5000}, &run)
	if run.TotalCents != 617_284 || len(run.TotalsByItem) != 1 || run.TotalsByItem[0].AllocatedCents != 617_284 {
		t.Fatalf("run %+v", run)
	}
	var allocs api.CommonAllocationPage
	c.expect(t, 200, "", "GET", M+"/common-charge-runs/"+run.Id+"/allocations", nil, &allocs)
	var sum int64
	for _, a := range allocs.Items {
		sum += a.AmountCents
	}
	if sum != 617_284 {
		t.Fatalf("allocations Σ %d", sum)
	}
	c.expect(t, 200, "", "POST", M+"/common-charge-runs/"+run.Id+"/issue", nil, &run, "If-Match", etag(run.Version))
	c.expect(t, 200, "", "GET", M+"/common-charge-runs/"+run.Id+"/statements?format=csv&by=item", nil, nil)
	c.expect(t, 200, "", "GET", M+"/common-charge-runs/"+run.Id+"/statements?format=csv", nil, nil)
	c.expect(t, 200, "", "GET", M+"/common-charge-runs/"+run.Id+"/allocations?format=csv", nil, nil)
	o.other.expect(t, 404, "not_found", "GET", M+"/common-charge-runs/"+run.Id, nil, nil)
	var bt api.Budget
	c.expect(t, 200, "", "GET", M+"/common-charge-budgets/"+b.Id, nil, &bt)
	var items api.BudgetItemList
	c.expect(t, 200, "", "PUT", M+"/common-charge-budgets/"+b.Id+"/actuals", map[string]any{"csv": "Pulizia;10000,00\n"}, &items, "If-Match", etag(bt.Version))
	if items.Items[0].ActualCents == nil || *items.Items[0].ActualCents != 1_000_000 || *items.Items[0].AdvancesCents != 617_284 {
		t.Fatalf("actuals %+v", items)
	}
	var bal api.CommonChargeRun
	c.expect(t, 201, "", "POST", M+"/common-charge-budgets/"+b.Id+"/runs", map[string]any{"kind": "balance"}, &bal)
	if bal.TotalCents != 1_000_000-617_284 {
		t.Fatalf("balance %+v", bal)
	}
}

func TestConcessionHandlers_Scenario(t *testing.T) {
	o := newRightsSetup(t)
	c, M := o.owner, o.M
	var con api.Concession
	c.expect(t, 200, "", "GET", M+"/concession", nil, &con)
	if con.Version != 0 {
		t.Fatalf("empty concession %+v", con)
	}
	o.dock.expect(t, 403, "forbidden", "PUT", M+"/concession", map[string]any{"authority": "Comune"}, nil, "If-Match", etag(0))
	c.expect(t, 200, "", "PUT", M+"/concession", map[string]any{
		"authority": "Comune", "expires_on": o.day(3650), "annual_fee_cents": 1_000_000,
		"surfaces": []map[string]any{{"kind": "water", "area_cm2": 500_000_000}},
	}, &con, "If-Match", etag(0))
	if con.Version != 1 || con.ExpiresOn == nil {
		t.Fatalf("concession %+v", con)
	}
	// the expiry is the default end of new rights
	var r api.Right
	c.expect(t, 201, "", "POST", M+"/rights", map[string]any{
		"berth_id": o.berths[1], "kind": "ownership_share", "from_on": o.day(0), "title": map[string]any{},
		"holders": []map[string]any{{"customer_id": o.customers[0], "share_millis": 1000}},
	}, &r)
	if r.ToOn != o.day(3649) {
		t.Fatalf("default end %s", r.ToOn)
	}
	var ob api.ConcessionObligation
	c.expect(t, 201, "", "POST", M+"/concession/obligations", map[string]any{"kind": "fee_instalment", "due_on": o.day(30), "recurrence": "yearly", "amount_cents": 500_000}, &ob)
	c.expect(t, 200, "", "POST", M+"/concession/obligations/"+ob.Id+"/done", nil, &ob, "If-Match", etag(ob.Version))
	if ob.DoneOn != nil || ob.DueOn == o.day(30) {
		t.Fatalf("yearly obligation rolled %+v", ob)
	}
	var list api.ConcessionObligationList
	c.expect(t, 200, "", "GET", M+"/concession/obligations", nil, &list)
	if len(list.Items) != 1 {
		t.Fatalf("obligations %d", len(list.Items))
	}
	o.other.expect(t, 404, "not_found", "GET", M+"/concession/obligations", nil, nil)
	c.expect(t, 204, "", "DELETE", M+"/concession/obligations/"+ob.Id, nil, nil, "If-Match", etag(ob.Version))
}
