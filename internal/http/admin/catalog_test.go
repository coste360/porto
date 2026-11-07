//go:build integration

package admin_test

import (
	"context"
	"testing"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/api"
)

// pricingSetup starts a server with an owner, a dock and a yard user of one marina, and an owner of another.
type pricingSetup struct {
	s                   *harborSrv
	owner, dock, yard   *harborClient
	other               *harborClient
	M, otherM, marinaID string
}

func newPricingSetup(t *testing.T) *pricingSetup {
	t.Helper()
	s := newHarborSrv(t)
	ctx := context.Background()
	o, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "owner@porto.test", Name: "Owner", Password: harborPW, Organization: "Org", MarinaName: "Marina", MarinaSlug: "marina-p"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{Email: "other@porto.test", Name: "Other", Password: harborPW, Organization: "Org2", MarinaName: "Altra", MarinaSlug: "marina-q"})
	if err != nil {
		t.Fatal(err)
	}
	ps := &pricingSetup{s: s, M: "/api/admin/v1/marinas/" + o.MarinaID, otherM: "/api/admin/v1/marinas/" + x.MarinaID, marinaID: o.MarinaID}
	ps.owner = s.login(t, "owner@porto.test")
	ps.other = s.login(t, "other@porto.test")
	for _, role := range []string{"dock", "yard"} {
		var mem struct {
			UserID string `json:"user_id"`
		}
		ps.owner.expect(t, 201, "", "POST", ps.M+"/members", map[string]any{"email": role + "@porto.test", "name": role, "role": role}, &mem)
		if err := s.app.Auth.SetPassword(ctx, mem.UserID, harborPW); err != nil {
			t.Fatal(err)
		}
	}
	ps.dock = s.login(t, "dock@porto.test")
	ps.yard = s.login(t, "yard@porto.test")
	return ps
}

func TestCatalogHandlers_Scenario(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M

	// Pricing settings.
	var set api.PricingSettings
	r := c.expect(t, 200, "", "GET", M+"/pricing-settings", nil, &set)
	if set.DefaultVatRateBp != 2200 || r.header.Get("ETag") != `"1"` {
		t.Fatalf("settings %+v %q", set, r.header.Get("ETag"))
	}
	c.expect(t, 428, "precondition_required", "PUT", M+"/pricing-settings", map[string]any{"default_vat_rate_bp": 1000, "default_price_includes_vat": true}, nil)
	c.expect(t, 400, "validation_failed", "PUT", M+"/pricing-settings", map[string]any{"default_vat_rate_bp": 20000, "default_price_includes_vat": true}, nil, "If-Match", `"1"`)
	c.expect(t, 200, "", "PUT", M+"/pricing-settings", map[string]any{"default_vat_rate_bp": 2200, "default_price_includes_vat": false}, &set, "If-Match", `"1"`)
	c.expect(t, 412, "version_conflict", "PUT", M+"/pricing-settings", map[string]any{"default_vat_rate_bp": 2200, "default_price_includes_vat": false}, nil, "If-Match", `"1"`)
	ps.dock.expect(t, 403, "forbidden", "PUT", M+"/pricing-settings", map[string]any{"default_vat_rate_bp": 2200, "default_price_includes_vat": false}, nil, "If-Match", `"2"`)
	ps.other.expect(t, 404, "not_found", "GET", M+"/pricing-settings", nil, nil)

	// Revenue classes.
	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	if len(classes.Items) != 9 {
		t.Fatalf("classes %d", len(classes.Items))
	}
	transit := ""
	for _, rc := range classes.Items {
		if rc.Code == "transit" {
			transit = rc.Id
		}
	}
	var rc api.RevenueClass
	c.expect(t, 201, "", "POST", M+"/revenue-classes", map[string]any{"code": "mooring_seasonal", "name": map[string]string{"it": "Stagionali"}, "parent_id": classes.Items[0].Id}, &rc)
	c.expect(t, 409, "conflict", "POST", M+"/revenue-classes", map[string]any{"code": "mooring_seasonal", "name": map[string]string{"it": "x"}}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/revenue-classes", map[string]any{"code": "BAD", "name": map[string]string{"it": "x"}}, nil)
	ps.yard.expect(t, 403, "forbidden", "GET", M+"/revenue-classes", nil, nil)
	var top api.RevenueClass
	c.expect(t, 200, "", "PATCH", M+"/revenue-classes/"+rc.Id, map[string]any{"parent_id": nil}, &top, "If-Match", `"1"`)
	if top.ParentId != nil || top.Version != 2 {
		t.Fatalf("parent not cleared: %+v", top)
	}
	c.expect(t, 412, "version_conflict", "PATCH", M+"/revenue-classes/"+rc.Id, map[string]any{"sort_order": 1}, nil, "If-Match", `"1"`)
	c.expect(t, 404, "not_found", "DELETE", M+"/revenue-classes/not-an-id", nil, nil, "If-Match", `"1"`)
	c.expect(t, 428, "precondition_required", "DELETE", M+"/revenue-classes/"+rc.Id, nil, nil)
	ps.other.expect(t, 404, "not_found", "DELETE", M+"/revenue-classes/"+rc.Id, nil, nil, "If-Match", `"2"`)
	c.expect(t, 204, "", "DELETE", M+"/revenue-classes/"+rc.Id, nil, nil, "If-Match", `"2"`)
	// A class with items is in use.
	c.expect(t, 201, "", "POST", M+"/items", map[string]any{
		"code": "TRN-NTT", "name": map[string]string{"it": "Passaggio"}, "item_type": "berth_fee", "unit": "night", "revenue_class_id": transit,
	}, nil)
	c.expect(t, 409, "in_use", "DELETE", M+"/revenue-classes/"+transit, nil, nil, "If-Match", `"1"`)
}

func TestCatalogHandlers_Items(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	rcID := classes.Items[1].Id

	var it api.Item
	r := c.expect(t, 201, "", "POST", M+"/items", map[string]any{
		"code": "TRN-NTT", "name": map[string]string{"it": "Passaggio"}, "item_type": "berth_fee", "unit": "night", "revenue_class_id": rcID,
		"price_includes_vat": true,
	}, &it)
	if r.header.Get("ETag") != `"1"` || it.VatRateBp != 2200 || !it.PriceIncludesVat {
		t.Fatalf("item %+v", it)
	}
	c.expect(t, 409, "conflict", "POST", M+"/items", map[string]any{
		"code": "TRN-NTT", "name": map[string]string{"it": "x"}, "item_type": "berth_fee", "unit": "night", "revenue_class_id": rcID,
	}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/items", map[string]any{
		"code": "DEP", "name": map[string]string{"it": "x"}, "item_type": "deposit", "unit": "lump", "vat_rate_bp": 0, "revenue_class_id": rcID,
	}, nil)
	c.expect(t, 404, "not_found", "POST", M+"/items", map[string]any{
		"code": "X", "name": map[string]string{"it": "x"}, "item_type": "fee", "unit": "lump", "revenue_class_id": "01900000-0000-7000-8000-000000000000",
	}, nil)
	var page api.ItemPage
	c.expect(t, 200, "", "GET", M+"/items?type=berth_fee&active=true&q=trn", nil, &page)
	if len(page.Items) != 1 || page.NextCursor != nil {
		t.Fatalf("page %+v", page)
	}
	ps.dock.expect(t, 200, "", "GET", M+"/items/"+it.Id, nil, &it)
	if it.Usage == nil || it.Usage.PriceLists != 0 {
		t.Fatalf("usage %+v", it.Usage)
	}
	ps.dock.expect(t, 403, "forbidden", "PATCH", M+"/items/"+it.Id, map[string]any{"active": false}, nil, "If-Match", `"1"`)
	c.expect(t, 428, "precondition_required", "PATCH", M+"/items/"+it.Id, map[string]any{"active": false}, nil)
	c.expect(t, 200, "", "PATCH", M+"/items/"+it.Id, map[string]any{"vat_rate_bp": 0, "vat_nature": "N1"}, &it, "If-Match", `"1"`)
	if it.VatNature == nil || *it.VatNature != "N1" {
		t.Fatalf("nature %+v", it)
	}
	itemID := it.Id
	it = api.Item{}
	c.expect(t, 200, "", "PATCH", M+"/items/"+itemID, map[string]any{"vat_rate_bp": 2200, "vat_nature": nil}, &it, "If-Match", `"2"`)
	if it.VatNature != nil {
		t.Fatalf("nature not cleared %+v", it)
	}
	ps.other.expect(t, 404, "not_found", "GET", M+"/items/"+it.Id, nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/items/"+it.Id, nil, nil)
	// In use by a price list: 409 in_use with the reference count.
	var doc api.PriceListDocument
	c.expect(t, 201, "", "POST", M+"/price-lists", map[string]any{
		"code": "L1", "name": map[string]string{"it": "L1"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31", "contract_types": []string{"transit"},
	}, &doc)
	save := saveBody(doc)
	save["rules"] = []map[string]any{{"key": "r1", "stage": "base", "name": "n", "item_id": it.Id, "basis": "flat", "unit": "night", "amount_cents": 100}}
	c.expect(t, 200, "", "PUT", M+"/price-lists/"+doc.Id, save, nil, "If-Match", `"1"`)
	r = c.expect(t, 409, "in_use", "DELETE", M+"/items/"+it.Id, nil, nil, "If-Match", `"3"`)
	var p struct {
		Details struct {
			References int `json:"references"`
		} `json:"details"`
	}
	_ = jsonUnmarshal(r.body, &p)
	if p.Details.References != 1 {
		t.Fatalf("details %s", r.body)
	}
	var other api.Item
	c.expect(t, 201, "", "POST", M+"/items", map[string]any{
		"code": "FEE", "name": map[string]string{"it": "x"}, "item_type": "fee", "unit": "lump", "revenue_class_id": rcID,
	}, &other)
	c.expect(t, 412, "version_conflict", "DELETE", M+"/items/"+other.Id, nil, nil, "If-Match", `"9"`)
	c.expect(t, 204, "", "DELETE", M+"/items/"+other.Id, nil, nil, "If-Match", `"1"`)
	c.expect(t, 404, "not_found", "DELETE", M+"/items/"+other.Id, nil, nil, "If-Match", `"1"`)
}

func TestCatalogHandlers_Categories(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	var sync api.BerthCategorySyncResult
	c.expect(t, 200, "", "POST", M+"/berth-categories/sync", nil, &sync)
	if len(sync.Created) != 8 {
		t.Fatalf("sync created %d", len(sync.Created))
	}
	ps.dock.expect(t, 403, "forbidden", "POST", M+"/berth-categories/sync", nil, nil)
	var list api.BerthCategoryList
	ps.dock.expect(t, 200, "", "GET", M+"/berth-categories", nil, &list)
	if len(list.Items) != 8 || list.UnknownInUse == nil {
		t.Fatalf("list %+v", list)
	}
	var cat api.BerthCategory
	c.expect(t, 201, "", "POST", M+"/berth-categories", map[string]any{"code": "BOA", "name": map[string]string{"it": "Boe"}, "applies_to": []string{"berth"}}, &cat)
	c.expect(t, 409, "conflict", "POST", M+"/berth-categories", map[string]any{"code": "BOA", "name": map[string]string{"it": "Boe"}, "applies_to": []string{"berth"}}, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/berth-categories", map[string]any{"code": "X", "name": map[string]string{"it": "x"}, "applies_to": []string{}}, nil)
	c.expect(t, 200, "", "PATCH", M+"/berth-categories/"+cat.Id, map[string]any{"name": map[string]string{"it": "Gavitelli"}, "archived": true}, &cat, "If-Match", `"1"`)
	if !cat.Archived || cat.Name.It != "Gavitelli" {
		t.Fatalf("patched %+v", cat)
	}
	c.expect(t, 412, "version_conflict", "PATCH", M+"/berth-categories/"+cat.Id, map[string]any{"sort_order": 1}, nil, "If-Match", `"1"`)
	c.expect(t, 428, "precondition_required", "DELETE", M+"/berth-categories/"+cat.Id, nil, nil)
	ps.other.expect(t, 404, "not_found", "DELETE", M+"/berth-categories/"+cat.Id, nil, nil, "If-Match", `"2"`)
	c.expect(t, 204, "", "DELETE", M+"/berth-categories/"+cat.Id, nil, nil, "If-Match", `"2"`)
}

func TestCatalogHandlers_CustomerCategoryCounts(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	for _, cat := range []string{"member", "member", "resident", ""} {
		body := map[string]any{"kind": "person", "last_name": "Rossi"}
		if cat != "" {
			body["category"] = cat
		}
		c.expect(t, 201, "", "POST", M+"/customers", body, nil)
	}
	var got api.CustomerCategoryCountList
	c.expect(t, 200, "", "GET", M+"/customer-categories", nil, &got)
	want := map[string]int{"member": 2, "resident": 1, "affiliated": 0, "charter": 0, "yard": 0}
	if len(got.Items) != len(want) || got.Items[0].Code != "member" || got.Uncategorised != 1 {
		t.Fatalf("counts %+v", got)
	}
	for _, it := range got.Items {
		if want[it.Code] != it.Count {
			t.Errorf("%s: %d, want %d", it.Code, it.Count, want[it.Code])
		}
	}
	ps.other.expect(t, 404, "not_found", "GET", M+"/customer-categories", nil, nil)
	var empty api.CustomerCategoryCountList
	ps.other.expect(t, 200, "", "GET", ps.otherM+"/customer-categories", nil, &empty)
	if empty.Uncategorised != 0 || empty.Items[0].Count != 0 {
		t.Fatalf("other marina sees counts %+v", empty)
	}
}
