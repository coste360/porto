//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M10 acceptance criterion 1 (spec 32, m10-plan §4.4, §16.4): the quote of an antifouling job for a 12 m boat is
// computed without typing prices by hand. The catalogue and the price list are built through the API; the quote is
// created with only a package id, a vessel and a customer (no price or quantity field in any request); lines and
// totals are the §4.4 numbers, no line is manual; a package version 2 saved later leaves the quote unchanged; the
// quote is sent and accepted from its magic link. The work order side is a fake here (QUOTE's scope, §16.3): the
// real chain is ORDERS' acceptance test.

// quoteOrders is the fake work order side of the acceptance: it counts the committed creations.
type quoteOrders struct{ committed atomic.Int32 }

func (f *quoteOrders) CreateFromQuoteTx(ctx context.Context, _ bun.Tx, _ store.MarinaScope, _ audit.Actor, q yardquote.AcceptedQuote) (yardquote.OrderRef, error) {
	store.AfterCommit(ctx, func() { f.committed.Add(1) })
	return yardquote.OrderRef{WorkOrderID: store.NewID(), Number: "C2027-000001"}, nil
}

func (f *quoteOrders) ApplyVariationTx(ctx context.Context, tx bun.Tx, s store.MarinaScope, a audit.Actor, q yardquote.AcceptedQuote) (yardquote.OrderRef, error) {
	return f.CreateFromQuoteTx(ctx, tx, s, a, q)
}

// yardCatalogue builds the §4.4 catalogue through the API and returns the package id.
func (w *deskWorld) yardCatalogue(t *testing.T) (pkg map[string]any, items map[string]string) {
	t.Helper()
	o, M := w.owner, w.M
	classes := map[string]string{}
	for _, rc := range jsonList(o.expect(t, 200, "GET", M+"/revenue-classes", nil), "items") {
		classes[str(rc.(map[string]any), "code")] = str(rc.(map[string]any), "id")
	}
	items = map[string]string{}
	var rules []map[string]any
	for _, it := range []struct {
		code, typ, unit, class string
		cents                  int
	}{
		{"SRV-ALAG", "service", "m", "services", 1800},
		{"SRV-LAV", "service", "m2", "services", 400},
		{"LAB-HAND", "labour", "hour", "yard_labour", 3800},
		{"LAB-PAINT", "labour", "hour", "yard_labour", 4500},
		{"MAT-AVEG", "material", "piece", "yard_materials", 9500},
		{"SRV-VARO", "service", "m", "services", 1800},
	} {
		r := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": it.code, "name": map[string]string{"it": it.code}, "item_type": it.typ, "unit": it.unit, "vat_rate_bp": 2200,
			"revenue_class_id": classes[it.class], "price_includes_vat": false,
		})
		items[it.code] = str(r, "id")
		rules = append(rules, map[string]any{"key": it.code, "stage": "base", "name": it.code, "item_id": items[it.code], "basis": "flat", "unit": "stay", "amount_cents": it.cents})
	}
	doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
		"code": "CANT27", "name": map[string]string{"it": "Cantiere"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31", "contract_types": []string{"storage"},
	})
	saved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(doc, "id"), map[string]any{
		"code": "CANT27", "name": map[string]string{"it": "Cantiere"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
		"contract_types": []string{"storage"}, "periods": []any{}, "bands": []any{}, "rules": rules,
		"settings": map[string]any{
			"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"},
			"primary_items": map[string]string{"storage": items["SRV-ALAG"]},
		},
	}, "If-Match", etagOf(doc["version"]))
	o.expect(t, 200, "POST", M+"/price-lists/"+str(doc, "id")+"/activate", map[string]any{}, "If-Match", etagOf(saved["document"].(map[string]any)["version"]))
	wt := o.expect(t, 201, "POST", M+"/yard/work-types", map[string]any{"code": "ANTIFOULING", "name": map[string]string{"it": "Antivegetativa"}})
	o.expect(t, 201, "POST", M+"/yard/qualifications", map[string]any{"code": "hand", "name": map[string]string{"it": "Operaio"}, "labour_item_id": items["LAB-HAND"], "default_cost_cents": 2200})
	o.expect(t, 201, "POST", M+"/yard/qualifications", map[string]any{"code": "painter", "name": map[string]string{"it": "Verniciatore"}, "labour_item_id": items["LAB-PAINT"], "default_cost_cents": 2600})
	o.expect(t, 200, "PUT", M+"/yard/items/"+items["SRV-LAV"], map[string]any{"cost_cents": 100})
	o.expect(t, 200, "PUT", M+"/yard/items/"+items["MAT-AVEG"], map[string]any{"cost_cents": 5800})
	o.expect(t, 201, "POST", M+"/yard/formula-variables", map[string]any{"name": "hull_area", "source": "derived", "kind": "number", "unit": "m2", "expression": "loa * (beam + draft) * 0.85"})
	o.expect(t, 201, "POST", M+"/yard/formula-variables", map[string]any{"name": "layers", "source": "job", "kind": "number", "unit": "pcs", "min_milli": 1000, "max_milli": 4000, "default_milli": 2000})
	pkg = o.expect(t, 201, "POST", M+"/yard/work-packages", map[string]any{
		"code": "ANTIVEG", "name": map[string]string{"it": "Antivegetativa"}, "work_type_id": str(wt, "id"), "job_vars": []string{"layers"},
		"lines": antivegAPILines(items, "round(hull_area, 1)"),
	})
	return pkg, items
}

// canonical is the JSON of v with sorted keys (maps compare by content).
func canonical(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func antivegAPILines(items map[string]string, washFormula string) []map[string]any {
	return []map[string]any{
		{"seq": 1, "item_id": items["SRV-ALAG"], "qty_formula": "loa", "price_formula": `if(hull_type == "catamaran", list_price * 1.5, list_price)`},
		{"seq": 2, "item_id": items["SRV-LAV"], "qty_formula": washFormula},
		{"seq": 3, "item_id": items["LAB-HAND"], "qty_formula": "round(hull_area * 0.15, 1)", "task": map[string]any{"title": map[string]string{"it": "Carteggiatura"}}},
		{"seq": 4, "item_id": items["LAB-PAINT"], "qty_formula": "round(hull_area * layers * 0.05, 1)", "task": map[string]any{"title": map[string]string{"it": "Antivegetativa"}}},
		{"seq": 5, "item_id": items["MAT-AVEG"], "qty_formula": "ceil(hull_area * layers / 25)"},
		{"seq": 6, "item_id": items["SRV-VARO"], "qty_formula": "loa"},
	}
}

func TestAcceptance_M10_AntifoulingQuote(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 2)
		orders := &quoteOrders{}
		w.h.app.YardQuotes.SetOrderCreator(orders)
		o, M := w.owner, w.M
		pkg, items := w.yardCatalogue(t)
		vessel := str(o.expect(t, 201, "POST", M+"/vessels", map[string]any{
			"name": "Maestrale", "loa_cm": 1200, "beam_cm": 400, "draft_cm": 150, "hull_type": "monohull_sail",
		}), "id")
		cust := w.mailCustomer(t, "Bianchi", "bianchi@example.test", "it")

		// Only a package id, the vessel and the customer.
		q := o.expect(t, 201, "POST", M+"/yard/quotes", map[string]any{
			"customer_id": cust, "vessel_id": vessel, "selections": []map[string]any{{"package_id": str(pkg, "id")}},
		})
		want := []struct {
			qty, unit, net, vat int64
		}{{12000, 1800, 21600, 4752}, {56100, 400, 22440, 4937}, {8400, 3800, 31920, 7022}, {5600, 4500, 25200, 5544}, {5000, 9500, 47500, 10450}, {12000, 1800, 21600, 4752}}
		lines := jsonList(q, "lines")
		if len(lines) != len(want) {
			t.Fatalf("%d lines", len(lines))
		}
		for i, wl := range want {
			l := lines[i].(map[string]any)
			qv, _ := num(l, "qty_milli")
			uv, _ := num(l, "unit_cents")
			nv, _ := num(l, "net_cents")
			vv, _ := num(l, "vat_cents")
			if qv != wl.qty || uv != wl.unit || nv != wl.net || vv != wl.vat || l["manual"] != false {
				t.Fatalf("line %d: %v", i+1, l)
			}
		}
		tot := q["totals"].(map[string]any)
		for k, v := range map[string]int64{"net_cents": 170260, "vat_cents": 37457, "gross_cents": 207717, "deposit_amount_cents": 62315} {
			if got, _ := num(tot, k); got != v {
				t.Fatalf("%s = %d, want %d", k, got, v)
			}
		}
		hours := map[int64]bool{}
		for _, h := range jsonList(q, "estimated_hours") {
			v, _ := num(h.(map[string]any), "hours_milli")
			hours[v] = true
		}
		if !hours[8400] || !hours[5600] || q["blocking"] != false || str(q, "number") != "PC2027-000001" {
			t.Fatalf("quote %v", q)
		}

		// Package version 2 later: the quote is unchanged.
		o.expect(t, 201, "POST", M+"/yard/work-packages/"+str(pkg, "id")+"/versions",
			map[string]any{"lines": antivegAPILines(items, "round(hull_area * 2, 1)")}, "If-Match", etagOf(pkg["version"]))
		again := o.expect(t, 200, "GET", M+"/yard/quotes/"+str(q, "id"), nil)
		if g, _ := num(again["totals"].(map[string]any), "gross_cents"); g != 207717 || canonical(t, jsonList(again, "lines")) != canonical(t, lines) {
			t.Fatalf("quote changed after package v2: %v", again["totals"])
		}

		// Send: email with the PDF and the link; the customer accepts from the link.
		sent := o.expect(t, 200, "POST", M+"/yard/quotes/"+str(q, "id")+"/send", map[string]any{}, "If-Match", etagOf(again["version"]))
		link := str(sent, "link_url")
		if str(sent["quote"].(map[string]any), "status") != "sent" || link == "" || str(sent, "message_id") == "" {
			t.Fatalf("send %v", sent)
		}
		token := link[strings.LastIndex(link, "/")+1:]
		pub := w.h.cookieClient()
		page := pub.expect(t, 200, "GET", "/api/public/v1/banchina/links/"+token, nil)
		yq, ok := page["yard_quote"].(map[string]any)
		if !ok || yq["can_accept"] != true || str(yq, "vessel_name") != "Maestrale" {
			t.Fatalf("link page %v", page)
		}
		if g, _ := num(yq, "gross_cents"); g != 207717 || len(jsonList(yq, "lines")) != 6 {
			t.Fatalf("link quote %v", yq)
		}
		acceptor := pub
		acc := acceptor.expect(t, 200, "POST", "/api/public/v1/banchina/links/"+token+"/yard-quote/accept", map[string]any{})
		if str(acc, "quote_number") != "PC2027-000001" || orders.committed.Load() != 1 {
			t.Fatalf("accept %v (%d orders)", acc, orders.committed.Load())
		}
		final := o.expect(t, 200, "GET", M+"/yard/quotes/"+str(q, "id"), nil)
		if str(final, "status") != "accepted" || str(final, "accepted_via") != "link" {
			t.Fatalf("final %v", final)
		}
		if r := acceptor.do(t, "POST", "/api/public/v1/banchina/links/"+token+"/yard-quote/accept", map[string]any{}); r.status != 404 || r.problemType() != "link_invalid" {
			t.Fatalf("second accept %d %s", r.status, r.body)
		}
	})
}
