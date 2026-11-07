//go:build integration

package admin_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// saveBody turns a document into a save request body (the PriceListSave members only).
func saveBody(doc api.PriceListDocument) map[string]any {
	return map[string]any{
		"code": doc.Code, "name": doc.Name, "valid_from": doc.ValidFrom, "valid_to": doc.ValidTo, "contract_types": doc.ContractTypes,
		"settings": doc.Settings, "periods": doc.Periods, "bands": doc.Bands, "rules": doc.Rules,
	}
}

func etag(v int) string { return `"` + strconv.Itoa(v) + `"` }

// transitList creates and saves a valid transit list for 2027 through the API; it returns the document.
func transitList(t *testing.T, ps *pricingSetup, code string) api.PriceListDocument {
	t.Helper()
	c, M := ps.owner, ps.M
	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	var item api.Item
	r := c.do(t, "GET", M+"/items?q=trn-ntt", nil)
	var page api.ItemPage
	_ = json.Unmarshal(r.body, &page)
	if len(page.Items) == 1 {
		item = page.Items[0]
	} else {
		c.expect(t, 201, "", "POST", M+"/items", map[string]any{
			"code": "TRN-NTT", "name": map[string]string{"it": "Passaggio"}, "item_type": "berth_fee", "unit": "night",
			"revenue_class_id": classes.Items[1].Id, "price_includes_vat": true,
		}, &item)
	}
	var doc api.PriceListDocument
	c.expect(t, 201, "", "POST", M+"/price-lists", map[string]any{
		"code": code, "name": map[string]string{"it": code}, "valid_from": "2027-01-01", "valid_to": "2027-12-31", "contract_types": []string{"transit"},
	}, &doc)
	body := saveBody(doc)
	settings := map[string]any{
		"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"}, "min_beam_ratio": map[string]int{"num": 1, "den": 3},
		"primary_items": map[string]string{"transit": item.Id},
	}
	body["settings"] = settings
	body["periods"] = []map[string]any{
		{"key": "p1", "code": "BASSA", "name": map[string]string{"it": "Bassa"}, "kind": "season", "ranges": []map[string]string{{"from": "2027-01-01", "to": "2027-05-31"}, {"from": "2027-10-01", "to": "2027-12-31"}}},
		{"key": "p2", "code": "ALTA", "name": map[string]string{"it": "Alta"}, "kind": "season", "ranges": []map[string]string{{"from": "2027-06-01", "to": "2027-09-30"}}},
	}
	body["bands"] = []map[string]any{{"key": "b1", "code": "L10", "max_cm": 1000}, {"key": "b2", "code": "L12", "max_cm": 1200}}
	body["rules"] = []map[string]any{
		{"key": "r1", "stage": "base", "name": "Bassa", "item_id": item.Id, "basis": "per_meter", "unit": "night", "unit_cents": 300, "scope": map[string]any{"periods": []string{"BASSA"}}},
		{"key": "r2", "stage": "base", "name": "Alta", "item_id": item.Id, "basis": "per_meter", "unit": "night", "unit_cents": 750, "scope": map[string]any{"periods": []string{"ALTA"}}},
	}
	var res api.PriceListSaveResult
	c.expect(t, 200, "", "PUT", M+"/price-lists/"+doc.Id, body, &res, "If-Match", etag(doc.Version))
	if len(res.IdMap) != 6 {
		t.Fatalf("id_map %v", res.IdMap)
	}
	for _, is := range res.Issues {
		if is.Severity == "error" {
			t.Fatalf("issue %+v", is)
		}
	}
	return res.Document
}

func TestPriceListHandlers_Scenario(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	c.expect(t, 400, "validation_failed", "POST", M+"/price-lists", map[string]any{
		"code": "X", "name": map[string]string{"it": "x"}, "valid_from": "2027-12-31", "valid_to": "2027-01-01", "contract_types": []string{"transit"},
	}, nil)
	c.expect(t, 400, "bad_request", "POST", M+"/price-lists", map[string]any{
		"code": "X", "name": map[string]string{"it": "x"}, "valid_from": "2027-13-01", "valid_to": "2027-01-01", "contract_types": []string{"transit"},
	}, nil)
	ps.dock.expect(t, 403, "forbidden", "POST", M+"/price-lists", map[string]any{
		"code": "X", "name": map[string]string{"it": "x"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31", "contract_types": []string{"transit"},
	}, nil)
	doc := transitList(t, ps, "T-2027")
	P := M + "/price-lists/" + doc.Id
	c.expect(t, 409, "conflict", "POST", M+"/price-lists", map[string]any{
		"code": "T-2027", "name": map[string]string{"it": "x"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31", "contract_types": []string{"transit"},
	}, nil)

	var got api.PriceListDocument
	r := ps.dock.expect(t, 200, "", "GET", P, nil, &got)
	if r.header.Get("ETag") != etag(got.Version) || len(got.Rules) != 2 || got.Rules[0].Seq == nil || *got.Rules[0].Seq != 1 {
		t.Fatalf("get %+v", got)
	}
	var lists api.PriceListSummaryList
	c.expect(t, 200, "", "GET", M+"/price-lists?status=draft&contract_type=transit&on=2027-05-01", nil, &lists)
	if len(lists.Items) != 1 || lists.Items[0].RuleCount != 2 {
		t.Fatalf("lists %+v", lists)
	}
	c.expect(t, 400, "bad_request", "GET", M+"/price-lists?on=yesterday", nil, nil)

	// Save: missing If-Match, stale version, shape error.
	c.expect(t, 428, "precondition_required", "PUT", P, saveBody(got), nil)
	c.expect(t, 412, "version_conflict", "PUT", P, saveBody(got), nil, "If-Match", `"1"`)
	bad := saveBody(got)
	bad["rules"] = []map[string]any{{"stage": "base", "name": "no key"}}
	c.expect(t, 400, "validation_failed", "PUT", P, bad, nil, "If-Match", etag(got.Version))

	// Validate: saved document, or a dry run of a body.
	var val api.PriceListValidation
	ps.dock.expect(t, 200, "", "POST", P+"/validate", nil, &val)
	dry := saveBody(got)
	dry["periods"] = append(got.Periods, api.RatePeriod{Code: "X", Kind: "season", Name: api.I18nText{It: "x"}, Key: ptr("pk"), Ranges: []api.DateRange{{From: "2027-05-01", To: "2027-06-15"}}})
	ps.dock.expect(t, 200, "", "POST", P+"/validate", dry, &val)
	if !hasIssue(val.Issues, "period_overlap") {
		t.Fatalf("dry run issues %+v", val.Issues)
	}

	// Activate: invalid → 422 with issues; valid → 200; then immutable.
	invalid := saveBody(got)
	invalid["periods"] = dry["periods"]
	var saved api.PriceListSaveResult
	c.expect(t, 200, "", "PUT", P, invalid, &saved, "If-Match", etag(got.Version))
	r = c.expect(t, 422, "price_list_invalid", "POST", P+"/activate", map[string]any{}, nil, "If-Match", etag(saved.Document.Version))
	var prob struct {
		Issues []api.PriceListIssue `json:"price_list_issues"`
	}
	_ = json.Unmarshal(r.body, &prob)
	if !hasIssue(prob.Issues, "period_overlap") {
		t.Fatalf("problem issues %s", r.body)
	}
	c.expect(t, 200, "", "PUT", P, saveBody(got), &saved, "If-Match", etag(saved.Document.Version))
	c.expect(t, 428, "precondition_required", "POST", P+"/activate", map[string]any{}, nil)
	c.expect(t, 400, "validation_failed", "POST", P+"/activate", map[string]any{"effective_from": "2030-01-01"}, nil, "If-Match", etag(saved.Document.Version))
	var act api.PriceListActivateResult
	c.expect(t, 200, "", "POST", P+"/activate", map[string]any{}, &act, "If-Match", etag(saved.Document.Version))
	if act.Document.Status != "active" || act.Document.ContentSha256 == nil || len(act.Truncated) != 0 {
		t.Fatalf("activated %+v", act)
	}
	c.expect(t, 409, "price_list_immutable", "PUT", P, saveBody(act.Document), nil, "If-Match", etag(act.Document.Version))
	c.expect(t, 409, "price_list_immutable", "DELETE", P, nil, nil, "If-Match", etag(act.Document.Version))
	c.expect(t, 409, "price_list_immutable", "POST", P+"/activate", map[string]any{}, nil, "If-Match", etag(act.Document.Version))

	// Overlap and truncation.
	second := transitList(t, ps, "T-2027-B")
	r = c.expect(t, 409, "price_list_overlap", "POST", M+"/price-lists/"+second.Id+"/activate", map[string]any{}, nil, "If-Match", etag(second.Version))
	var ov struct {
		Details struct {
			Conflicts []api.PriceListRef `json:"conflicts"`
		} `json:"details"`
	}
	_ = json.Unmarshal(r.body, &ov)
	if len(ov.Details.Conflicts) != 1 || ov.Details.Conflicts[0].Id != doc.Id {
		t.Fatalf("conflicts %s", r.body)
	}
	c.expect(t, 200, "", "POST", M+"/price-lists/"+second.Id+"/activate", map[string]any{"effective_from": "2027-07-01"}, &act, "If-Match", etag(second.Version))
	if len(act.Truncated) != 1 || act.Truncated[0].ValidTo != "2027-06-30" {
		t.Fatalf("truncated %+v", act.Truncated)
	}

	// Renewal dry run and apply.
	renew := map[string]any{
		"dry_run": true, "code": "T-2028", "name": map[string]string{"it": "2028"}, "shift_years": 1, "amount_rounding": "none",
		"adjustments": []map[string]any{{"scope": "all", "percent_bp": 500}},
	}
	var preview api.RenewalPreview
	c.expect(t, 200, "", "POST", P+"/renewal", renew, &preview)
	if len(preview.Diffs) != 1 || preview.Document.ValidFrom != "2028-01-01" {
		t.Fatalf("preview %+v", preview.Document)
	}
	c.expect(t, 400, "validation_failed", "POST", P+"/renewal", map[string]any{"dry_run": true, "code": "T", "name": map[string]string{"it": "x"}, "shift_years": 1, "amount_rounding": "cents"}, nil)
	renew["dry_run"] = false
	var renewed api.PriceListDocument
	r = c.expect(t, 201, "", "POST", P+"/renewal", renew, &renewed, "Idempotency-Key", "renew-key-0001")
	if renewed.Status != "draft" || renewed.SourcePriceListId == nil || *renewed.SourcePriceListId != doc.Id || r.header.Get("ETag") != `"1"` {
		t.Fatalf("renewed %+v", renewed)
	}
	c.expect(t, 409, "conflict", "POST", P+"/renewal", renew, nil)

	// Archive and delete.
	c.expect(t, 409, "conflict", "POST", M+"/price-lists/"+renewed.Id+"/archive", nil, nil, "If-Match", `"1"`)
	var cur, arch api.PriceListDocument
	c.expect(t, 200, "", "GET", P, nil, &cur)
	c.expect(t, 200, "", "POST", P+"/archive", nil, &arch, "If-Match", etag(cur.Version))
	if arch.Status != "archived" || arch.ValidTo != "2027-06-30" {
		t.Fatalf("archived %+v", arch)
	}
	c.expect(t, 412, "version_conflict", "DELETE", M+"/price-lists/"+renewed.Id, nil, nil, "If-Match", `"7"`)
	c.expect(t, 204, "", "DELETE", M+"/price-lists/"+renewed.Id, nil, nil, "If-Match", `"1"`)
	c.expect(t, 404, "not_found", "GET", M+"/price-lists/"+renewed.Id, nil, nil)

	// Another marina: 404 on every path.
	for _, p := range []string{P, P + "/matrix", P + "/coverage"} {
		ps.other.expect(t, 404, "not_found", "GET", p, nil, nil)
	}
	ps.other.expect(t, 404, "not_found", "POST", P+"/validate", nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/price-lists/"+doc.Id, nil, nil)
	_ = context.Background()
}

func hasIssue(list []api.PriceListIssue, code string) bool {
	for _, is := range list {
		if is.Code == code {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }

func TestPricingToolHandlers_Scenario(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	doc := transitList(t, ps, "T-2027")
	P := M + "/price-lists/" + doc.Id
	sim := map[string]any{
		"contract_type": "transit", "arrival": "2027-07-10", "departure": "2027-07-13",
		"vessel": map[string]any{"loa_cm": 1134, "beam_cm": 380, "hull_type": "monohull_sail"},
	}
	// No active list yet: 422 no_price with reason no_active_list; with the draft's id it prices.
	r := ps.dock.expect(t, 422, "no_price", "POST", M+"/pricing/simulate", sim, nil)
	var np struct {
		Details map[string]string `json:"details"`
	}
	_ = json.Unmarshal(r.body, &np)
	if np.Details["reason"] != "no_active_list" {
		t.Fatalf("details %s", r.body)
	}
	sim["price_list_id"] = doc.Id
	var b api.PriceBreakdown
	ps.dock.expect(t, 200, "", "POST", M+"/pricing/simulate", sim, &b)
	// 11,34 m → 11,40 m × 7,50 €/m = 85,50 € × 3 nights = 256,50 € (VAT included); net 210,25, VAT 46,25.
	if b.TotalCents != 25650 || b.NetCents != 21025 || b.VatCents != 4625 || b.Resolved == nil || b.Resolved.DimensionOrigin != "input" {
		t.Fatalf("breakdown total %d net %d vat %d resolved %+v", b.TotalCents, b.NetCents, b.VatCents, b.Resolved)
	}
	if len(b.Lines) != 1 || len(b.Lines[0].Blocks) != 3 || b.Lines[0].Blocks[0].Modifiers == nil || len(b.Trace) == 0 {
		t.Fatalf("lines %+v", b.Lines)
	}
	ps.yard.expect(t, 403, "forbidden", "POST", M+"/pricing/simulate", sim, nil)
	ps.other.expect(t, 404, "not_found", "POST", M+"/pricing/simulate", sim, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/pricing/simulate", map[string]any{"contract_type": "boat", "arrival": "2027-07-10", "departure": "2027-07-13"}, nil)
	c.expect(t, 400, "bad_request", "POST", M+"/pricing/simulate", map[string]any{"contract_type": "transit", "arrival": "10/07/2027", "departure": "2027-07-13"}, nil)
	noDims := map[string]any{"price_list_id": doc.Id, "contract_type": "transit", "arrival": "2027-07-10", "departure": "2027-07-13"}
	if r := c.do(t, "POST", M+"/pricing/simulate", noDims); r.status != 422 && r.status != 400 {
		t.Fatalf("no dimensions: %d %s", r.status, r.body)
	}
	sim["promo_code"] = "NOPE"
	c.expect(t, 422, "promo_not_applicable", "POST", M+"/pricing/simulate", sim, nil)
	delete(sim, "promo_code")

	// Matrix: every cell equals the simulation of its probe (INV-23).
	var m api.PriceMatrix
	ps.dock.expect(t, 200, "", "GET", P+"/matrix?contract_type=transit&rows=bands&channel=desk&quoted_on=2027-03-01", nil, &m)
	if len(m.Rows) != 2 || len(m.Columns) != 2 || len(m.Cells) != 4 || m.QuotedOn != "2027-03-01" {
		t.Fatalf("matrix %+v", m)
	}
	for _, cell := range m.Cells {
		if cell.TotalCents == nil {
			t.Fatalf("cell without price: %+v", cell)
		}
		var sb api.PriceBreakdown
		c.expect(t, 200, "", "POST", M+"/pricing/simulate", cell.Probe, &sb)
		if sb.TotalCents != *cell.TotalCents || sb.NetCents != *cell.NetCents {
			t.Fatalf("cell %d/%d: matrix %d, simulate %d", cell.Row, cell.Col, *cell.TotalCents, sb.TotalCents)
		}
	}
	// L12 × ALTA: 12,00 m × 7,50 € = 90,00 €.
	for _, cell := range m.Cells {
		if m.Rows[cell.Row].Key == "L12" && m.Columns[cell.Col].Key == "ALTA" && *cell.TotalCents != 9000 {
			t.Fatalf("L12×ALTA = %d", *cell.TotalCents)
		}
	}
	c.expect(t, 400, "validation_failed", "GET", P+"/matrix?contract_type=transit&rows=categories&item_id=x&channel=online&customer_category=member", nil, nil)
	c.expect(t, 200, "", "GET", P+"/matrix?rows=categories", nil, nil)
	c.expect(t, 400, "bad_request", "GET", P+"/matrix?quoted_on=x", nil, nil)
	ps.yard.expect(t, 403, "forbidden", "GET", P+"/matrix", nil, nil)

	// Coverage.
	var cov api.PriceCoverage
	ps.dock.expect(t, 200, "", "GET", P+"/coverage?quoted_on=2027-03-01", nil, &cov)
	if len(cov.Gaps) != 0 || cov.Checked == 0 || cov.UnknownCategories == nil {
		t.Fatalf("coverage %+v", cov)
	}
	ps.yard.expect(t, 403, "forbidden", "GET", P+"/coverage", nil, nil)
}
