//go:build integration

package admin_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/coste360/porto/internal/http/api"
)

// yardSetup is the pricing setup plus a yard manager, a 12 m boat with a draft, a customer with an email and the
// §4.4 catalogue built through the API (prices in a storage list valid this year and next).
type yardSetup struct {
	*pricingSetup
	mgr      *harborClient
	vessel   string
	customer string
	items    map[string]string
	wt       string
	pkg      api.WorkPackage
}

func newYardSetup(t *testing.T) *yardSetup {
	t.Helper()
	ps := newPricingSetup(t)
	ys := &yardSetup{pricingSetup: ps, items: map[string]string{}}
	c, M := ps.owner, ps.M
	var mem struct {
		UserID string `json:"user_id"`
	}
	c.expect(t, 201, "", "POST", M+"/members", map[string]any{"email": "mgr@porto.test", "name": "mgr", "role": "yard_mgr"}, &mem)
	if err := ps.s.app.Auth.SetPassword(context.Background(), mem.UserID, harborPW); err != nil {
		t.Fatal(err)
	}
	ys.mgr = ps.s.login(t, "mgr@porto.test")
	var v api.Vessel
	c.expect(t, 201, "", "POST", M+"/vessels", map[string]any{"name": "Maestrale", "loa_cm": 1200, "beam_cm": 400, "draft_cm": 150, "hull_type": "monohull_sail"}, &v)
	ys.vessel = v.Id
	var cu struct {
		Customer api.Customer `json:"customer"`
	}
	c.expect(t, 201, "", "POST", M+"/customers", map[string]any{
		"kind": "person", "first_name": "Anna", "last_name": "Bianchi", "emails": []map[string]string{{"label": "home", "value": "anna@example.test"}},
	}, &cu)
	ys.customer = cu.Customer.Id

	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	prices := map[string]int64{"SRV-ALAG": 1800, "SRV-LAV": 400, "LAB-HAND": 3800, "LAB-PAINT": 4500, "MAT-AVEG": 9500, "SRV-VARO": 1800}
	for _, it := range []struct{ code, typ, unit string }{
		{"SRV-ALAG", "service", "m"},
		{"SRV-LAV", "service", "m2"},
		{"LAB-HAND", "labour", "hour"},
		{"LAB-PAINT", "labour", "hour"},
		{"MAT-AVEG", "material", "piece"},
		{"SRV-VARO", "service", "m"},
	} {
		var item api.Item
		c.expect(t, 201, "", "POST", M+"/items", map[string]any{
			"code": it.code, "name": map[string]string{"it": it.code}, "item_type": it.typ, "unit": it.unit, "vat_rate_bp": 2200,
			"revenue_class_id": classes.Items[0].Id, "price_includes_vat": false,
		}, &item)
		ys.items[it.code] = item.Id
	}
	y := time.Now().Year()
	var doc api.PriceListDocument
	c.expect(t, 201, "", "POST", M+"/price-lists", map[string]any{
		"code": "CANT", "name": map[string]string{"it": "Cantiere"}, "valid_from": strconv.Itoa(y) + "-01-01", "valid_to": strconv.Itoa(y+1) + "-12-31",
		"contract_types": []string{"storage"},
	}, &doc)
	body := saveBody(doc)
	body["settings"] = map[string]any{
		"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"},
		"primary_items": map[string]string{"storage": ys.items["SRV-ALAG"]},
	}
	body["periods"], body["bands"] = []any{}, []any{}
	var rules []map[string]any
	for code, cents := range prices {
		rules = append(rules, map[string]any{"key": code, "stage": "base", "name": code, "item_id": ys.items[code], "basis": "flat", "unit": "stay", "amount_cents": cents})
	}
	body["rules"] = rules
	var res api.PriceListSaveResult
	c.expect(t, 200, "", "PUT", M+"/price-lists/"+doc.Id, body, &res, "If-Match", etag(doc.Version))
	c.expect(t, 200, "", "POST", M+"/price-lists/"+doc.Id+"/activate", map[string]any{}, nil, "If-Match", etag(res.Document.Version))

	var wt api.WorkType
	c.expect(t, 201, "", "POST", M+"/yard/work-types", map[string]any{"code": "ANTIFOULING", "name": map[string]string{"it": "Antivegetativa"}}, &wt)
	ys.wt = wt.Id
	var q api.YardQualification
	c.expect(t, 201, "", "POST", M+"/yard/qualifications", map[string]any{"code": "hand", "name": map[string]string{"it": "Operaio"}, "labour_item_id": ys.items["LAB-HAND"], "default_cost_cents": 2200}, &q)
	c.expect(t, 201, "", "POST", M+"/yard/qualifications", map[string]any{"code": "painter", "name": map[string]string{"it": "Verniciatore"}, "labour_item_id": ys.items["LAB-PAINT"], "default_cost_cents": 2600}, &q)
	var fv api.FormulaVariable
	c.expect(t, 201, "", "POST", M+"/yard/formula-variables", map[string]any{"name": "hull_area", "source": "derived", "kind": "number", "unit": "m2", "expression": "loa * (beam + draft) * 0.85"}, &fv)
	c.expect(t, 201, "", "POST", M+"/yard/formula-variables", map[string]any{"name": "layers", "source": "job", "kind": "number", "unit": "pcs", "min_milli": 1000, "max_milli": 4000, "default_milli": 2000}, &fv)
	c.expect(t, 201, "", "POST", M+"/yard/work-packages", map[string]any{
		"code": "ANTIVEG", "name": map[string]string{"it": "Antivegetativa"}, "work_type_id": ys.wt, "job_vars": []string{"layers"},
		"lines": yardLines(ys.items),
	}, &ys.pkg)
	return ys
}

func yardLines(items map[string]string) []map[string]any {
	return []map[string]any{
		{"seq": 1, "item_id": items["SRV-ALAG"], "qty_formula": "loa", "price_formula": `if(hull_type == "catamaran", list_price * 1.5, list_price)`},
		{"seq": 2, "item_id": items["SRV-LAV"], "qty_formula": "round(hull_area, 1)"},
		{"seq": 3, "item_id": items["LAB-HAND"], "qty_formula": "round(hull_area * 0.15, 1)", "task": map[string]any{"title": map[string]string{"it": "Carteggiatura"}}},
		{"seq": 4, "item_id": items["LAB-PAINT"], "qty_formula": "round(hull_area * layers * 0.05, 1)", "task": map[string]any{"title": map[string]string{"it": "Antivegetativa"}}},
		{"seq": 5, "item_id": items["MAT-AVEG"], "qty_formula": "ceil(hull_area * layers / 25)"},
		{"seq": 6, "item_id": items["SRV-VARO"], "qty_formula": "loa"},
	}
}

func TestYardCatalogHandlers_Scenario(t *testing.T) {
	ys := newYardSetup(t)
	c, M, X := ys.owner, ys.M, ys.otherM

	// Work types: duplicate, bad code, If-Match, cross marina, roles.
	c.expect(t, 409, "code_taken", "POST", M+"/yard/work-types", map[string]any{"code": "ANTIFOULING", "name": map[string]string{"it": "x"}}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/yard/work-types", map[string]any{"code": "bad code", "name": map[string]string{"it": "x"}}, nil)
	c.expect(t, 428, "precondition_required", "PATCH", M+"/yard/work-types/"+ys.wt, map[string]any{"sort_order": 2}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", M+"/yard/work-types/"+ys.wt, map[string]any{"sort_order": 2}, nil, "If-Match", etag(9))
	var wt api.WorkType
	c.expect(t, 200, "", "PATCH", M+"/yard/work-types/"+ys.wt, map[string]any{"sort_order": 2}, &wt, "If-Match", etag(1))
	ys.other.expect(t, 404, "not_found", "PATCH", M+"/yard/work-types/"+ys.wt, map[string]any{"sort_order": 3}, nil, "If-Match", etag(2))
	ys.other.expect(t, 404, "not_found", "PATCH", X+"/yard/work-types/"+ys.wt, map[string]any{"sort_order": 3}, nil, "If-Match", etag(2))
	ys.yard.expect(t, 403, "forbidden", "GET", M+"/yard/work-types", nil, nil)
	ys.mgr.expect(t, 200, "", "GET", M+"/yard/work-types", nil, nil)
	ys.dock.expect(t, 403, "forbidden", "POST", M+"/yard/work-types", map[string]any{"code": "X", "name": map[string]string{"it": "x"}}, nil)

	// Qualifications and items.
	var quals api.YardQualificationList
	ys.mgr.expect(t, 200, "", "GET", M+"/yard/qualifications", nil, &quals)
	if len(quals.Items) != 2 {
		t.Fatalf("qualifications %d", len(quals.Items))
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/yard/qualifications", map[string]any{"code": "x", "name": map[string]string{"it": "x"}, "labour_item_id": ys.items["SRV-LAV"]}, nil)
	var items api.YardItemPage
	c.expect(t, 200, "", "GET", M+"/yard/items?item_type=labour", nil, &items)
	if len(items.Items) != 2 || !items.Items[0].Configured || items.Items[0].QualificationId == nil {
		t.Fatalf("labour items %+v", items)
	}
	var yi api.YardItem
	c.expect(t, 200, "", "PUT", M+"/yard/items/"+ys.items["MAT-AVEG"], map[string]any{"cost_cents": 5800, "category": "Vernici", "qty_formula": "1"}, &yi)
	if !yi.Configured || yi.CostCents != 5800 || yi.Version != 1 {
		t.Fatalf("item %+v", yi)
	}
	c.expect(t, 428, "precondition_required", "PUT", M+"/yard/items/"+ys.items["MAT-AVEG"], map[string]any{"cost_cents": 6000}, nil)
	c.expect(t, 200, "", "PUT", M+"/yard/items/"+ys.items["MAT-AVEG"], map[string]any{"cost_cents": 6000}, &yi, "If-Match", etag(1))
	r := c.expect(t, 422, "formula_invalid", "PUT", M+"/yard/items/"+ys.items["SRV-VARO"], map[string]any{"qty_formula": "loa *"}, nil)
	if r.problem() != "formula_invalid" {
		t.Fatal(string(r.body))
	}
	ys.other.expect(t, 404, "not_found", "GET", M+"/yard/items/"+ys.items["SRV-VARO"], nil, nil)

	// Variables and tables.
	var vars api.FormulaVariableList
	c.expect(t, 200, "", "GET", M+"/yard/formula-variables", nil, &vars)
	var layers api.FormulaVariable
	builtins := 0
	for _, v := range vars.Items {
		if v.Builtin {
			builtins++
		}
		if v.Name == "layers" {
			layers = v
		}
	}
	if builtins < 11 || layers.Id == nil {
		t.Fatalf("variables %+v", vars)
	}
	c.expect(t, 409, "conflict", "DELETE", M+"/yard/formula-variables/"+*layers.Id, nil, nil, "If-Match", etag(int(*layers.Version)))
	c.expect(t, 422, "formula_invalid", "POST", M+"/yard/formula-variables", map[string]any{"name": "bad", "source": "derived", "kind": "number", "expression": "nope * 2"}, nil)
	var tbl api.FormulaTable
	c.expect(t, 201, "", "POST", M+"/yard/formula-tables", map[string]any{"name": "lift", "rows": []map[string]any{{"up_to_milli": 10000, "value_milli": 100000}, {"up_to_milli": nil, "value_milli": 200000}}}, &tbl)
	c.expect(t, 200, "", "PATCH", M+"/yard/formula-tables/"+tbl.Id, map[string]any{"rows": []map[string]any{{"value_milli": 150000}}}, &tbl, "If-Match", etag(1))
	ys.other.expect(t, 404, "not_found", "PATCH", M+"/yard/formula-tables/"+tbl.Id, map[string]any{"rows": []map[string]any{{"value_milli": 1}}}, nil, "If-Match", etag(2))

	// Formula test.
	var vr api.FormulaValidateResult
	c.expect(t, 200, "", "POST", M+"/yard/formulas/validate", map[string]any{"formula": "round(hull_area * 0.15, 1)"}, &vr)
	if !vr.Valid || vr.QtyMilli == nil || *vr.QtyMilli != 8400 {
		t.Fatalf("validate %+v", vr)
	}
	c.expect(t, 200, "", "POST", M+"/yard/formulas/validate", map[string]any{"formula": "band(loa, lift)", "vessel_id": ys.vessel}, &vr)
	if !vr.Valid || *vr.QtyMilli != 150000 {
		t.Fatalf("validate band %+v", vr)
	}
	c.expect(t, 200, "", "POST", M+"/yard/formulas/validate", map[string]any{"formula": "1 +"}, &vr)
	if vr.Valid || len(vr.Issues) != 1 || vr.Issues[0].Code != "syntax" {
		t.Fatalf("validate syntax %+v", vr)
	}

	// Packages: versions, active flag.
	var pk api.WorkPackage
	c.expect(t, 422, "formula_invalid", "POST", M+"/yard/work-packages/"+ys.pkg.Id+"/versions", map[string]any{
		"lines": []map[string]any{{"seq": 1, "item_id": ys.items["SRV-LAV"], "qty_formula": "nope"}},
	}, nil, "If-Match", etag(int(ys.pkg.Version)))
	c.expect(t, 201, "", "POST", M+"/yard/work-packages/"+ys.pkg.Id+"/versions", map[string]any{"lines": yardLines(ys.items)}, &pk, "If-Match", etag(int(ys.pkg.Version)))
	if pk.CurrentVersion != 2 || pk.Versions == nil || len(*pk.Versions) != 2 {
		t.Fatalf("package %+v", pk)
	}
	c.expect(t, 200, "", "GET", M+"/yard/work-packages/"+ys.pkg.Id+"?version=1", nil, &pk)
	if pk.Content == nil || pk.Content.Version != 1 {
		t.Fatalf("version 1 %+v", pk.Content)
	}
	c.expect(t, 200, "", "PUT", M+"/yard/work-packages/"+ys.pkg.Id+"/active", map[string]any{"active": false}, &pk, "If-Match", etag(int(pk.Version)))
	var pkPage api.WorkPackagePage
	c.expect(t, 200, "", "GET", M+"/yard/work-packages?active=true", nil, &pkPage)
	if len(pkPage.Items) != 0 {
		t.Fatalf("active packages %d", len(pkPage.Items))
	}
	ys.other.expect(t, 404, "not_found", "GET", M+"/yard/work-packages/"+ys.pkg.Id, nil, nil)

	// Settings.
	var set api.YardSettings
	c.expect(t, 200, "", "GET", M+"/yard/settings", nil, &set)
	if set.Version != 0 || set.DepositBp != 3000 || set.QuoteValidityDays != 30 || set.TestVessel.LoaCm != 1200 {
		t.Fatalf("settings %+v", set)
	}
	c.expect(t, 200, "", "PUT", M+"/yard/settings", map[string]any{"quote_validity_days": 45}, &set)
	c.expect(t, 428, "precondition_required", "PUT", M+"/yard/settings", map[string]any{"quote_validity_days": 40}, nil)
	c.expect(t, 200, "", "PUT", M+"/yard/settings", map[string]any{"quote_validity_days": 40}, &set, "If-Match", etag(1))
	ys.mgr.expect(t, 200, "", "PUT", M+"/yard/settings", map[string]any{"deposit_bp": 2000}, &set, "If-Match", etag(2))
	ys.other.expect(t, 404, "not_found", "GET", M+"/yard/settings", nil, nil)
}

func TestYardQuoteHandlers_Scenario(t *testing.T) {
	ys := newYardSetup(t)
	c, M := ys.owner, ys.M
	in := map[string]any{"customer_id": ys.customer, "vessel_id": ys.vessel, "selections": []map[string]any{{"package_id": ys.pkg.Id}}}

	var pv api.YardQuotePreview
	ys.mgr.expect(t, 200, "", "POST", M+"/yard/quotes/preview", in, &pv)
	if pv.Totals.NetCents != 170260 || pv.Totals.VatCents != 37457 || pv.Totals.GrossCents != 207717 || pv.Totals.DepositAmountCents != 62315 || pv.Blocking {
		t.Fatalf("preview %+v", pv.Totals)
	}
	if len(pv.Variables) != 1 || pv.Variables[0].Name != "layers" || len(pv.EstimatedHours) != 2 {
		t.Fatalf("variables %+v hours %+v", pv.Variables, pv.EstimatedHours)
	}
	c.expect(t, 422, "formula_invalid", "POST", M+"/yard/quotes/preview", map[string]any{
		"customer_id": ys.customer, "vessel_id": ys.vessel, "selections": []map[string]any{{"package_id": ys.pkg.Id}}, "job_inputs": map[string]any{"layers": 9000},
	}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/yard/quotes/preview", map[string]any{"customer_id": ys.customer, "vessel_id": ys.vessel, "selections": []any{}}, nil)
	ys.yard.expect(t, 403, "forbidden", "POST", M+"/yard/quotes/preview", in, nil)

	var q api.YardQuote
	r := c.expect(t, 201, "", "POST", M+"/yard/quotes", in, &q)
	if r.header.Get("ETag") != `"1"` || q.Status != "draft" || q.Totals.GrossCents != 207717 || q.LanguageVersion != 1 {
		t.Fatalf("quote %+v", q)
	}
	for _, l := range q.Lines {
		if l.Manual {
			t.Fatalf("manual line %+v", l)
		}
	}
	var page api.YardQuotePage
	ys.mgr.expect(t, 200, "", "GET", M+"/yard/quotes?status=draft", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("quotes %d", len(page.Items))
	}
	ys.other.expect(t, 404, "not_found", "GET", M+"/yard/quotes/"+q.Id, nil, nil)
	ys.other.expect(t, 404, "not_found", "GET", ys.otherM+"/yard/quotes/"+q.Id, nil, nil)

	// Override the painter hours: marked manual.
	c.expect(t, 428, "precondition_required", "PATCH", M+"/yard/quotes/"+q.Id, map[string]any{"notes": "x"}, nil)
	c.expect(t, 200, "", "PATCH", M+"/yard/quotes/"+q.Id, map[string]any{
		"overrides": []map[string]any{{"seq": 4, "qty_milli": 6000}}, "deposit_cents": 50000, "planned_start_on": "2030-01-10",
	}, &q, "If-Match", etag(int(q.Version)))
	if !q.Lines[3].Manual || !q.Lines[3].QtyOverridden || q.Totals.DepositAmountCents != 50000 || q.PlannedStartOn == nil {
		t.Fatalf("patched %+v", q.Lines[3])
	}
	c.expect(t, 200, "", "POST", M+"/yard/quotes/"+q.Id+"/recompute", nil, &q, "If-Match", etag(int(q.Version)))
	if !q.Lines[3].QtyOverridden {
		t.Fatal("recompute dropped the override")
	}

	// PDF on demand, send without email (manual), then decline.
	if rr := c.raw(t, "GET", M+"/yard/quotes/"+q.Id+"/pdf", "", nil); rr.status != 200 || string(rr.body[:5]) != "%PDF-" {
		t.Fatalf("pdf %d", rr.status)
	}
	var sent api.YardQuoteSendResult
	c.expect(t, 200, "", "POST", M+"/yard/quotes/"+q.Id+"/send", map[string]any{"manual": true}, &sent, "If-Match", etag(int(q.Version)))
	if sent.Quote.Status != "sent" || sent.LinkUrl == nil {
		t.Fatalf("send %+v", sent)
	}
	c.expect(t, 409, "invalid_transition", "PATCH", M+"/yard/quotes/"+q.Id, map[string]any{"notes": "x"}, nil, "If-Match", etag(int(sent.Quote.Version)))
	c.expect(t, 428, "precondition_required", "POST", M+"/yard/quotes/"+q.Id+"/accept", nil, nil)
	ys.other.expect(t, 404, "not_found", "POST", M+"/yard/quotes/"+q.Id+"/accept", nil, nil, "If-Match", etag(int(sent.Quote.Version)))
	c.expect(t, 200, "", "POST", M+"/yard/quotes/"+q.Id+"/decline", map[string]any{"reason": "rinviato"}, &q, "If-Match", etag(int(sent.Quote.Version)))
	if q.Status != "declined" || q.DeclineReason == nil {
		t.Fatalf("declined %+v", q)
	}

	// The public link of a declined quote is revoked.
	token := (*sent.LinkUrl)[len(*sent.LinkUrl)-52:]
	pub := &harborClient{s: ys.s, http: ys.owner.http}
	pub.expect(t, 404, "link_invalid", "GET", "/api/public/v1/marina-p/links/"+token, nil, nil)
}
