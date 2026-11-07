//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// M2 pricing acceptance (spec 32.3 M2, m2-plan §8.2): a transit + annual list configured through the API, the
// simulator, the matrix (= simulator, INV-23), coverage, immutability (INV-22), renewal and activation at a date
// (INV-24), roles and isolation, events without personal data. Y = 2027, today 1 March in Europe/Rome.

type pricingWorld struct {
	h      *harness
	owner  *client
	marina string
	M      string
}

func etagOf(v any) string { return fmt.Sprintf(`"%d"`, int(v.(float64))) }

// pricingMap builds a water map with a quay and two piers: berths 350 cm wide (class L10) and 400 cm (L12).
func (w *pricingWorld) pricingMap(t *testing.T) {
	t.Helper()
	d := w.owner.expect(t, 201, "POST", w.M+"/maps", map[string]any{"kind": "water", "name": map[string]string{"it": "Porto"}, "width_cm": 30000, "height_cm": 20000})
	P := w.M + "/maps/" + str(d, "id")
	doc := harborRequest(d)
	doc["structures"] = []any{map[string]any{
		"key": "quay-1", "kind": "quay", "code": "BANCHINA", "points": []map[string]int{{"x": 0, "y": 2000}, {"x": 30000, "y": 2000}}, "sides": []string{"right"},
	}}
	doc["water_polygons"] = []any{[]map[string]int{{"x": 0, "y": 2000}, {"x": 30000, "y": 2000}, {"x": 30000, "y": 20000}, {"x": 0, "y": 20000}}}
	res := w.owner.expect(t, 200, "PUT", P, doc, "If-Match", `"1"`)
	saved := res["document"].(map[string]any)
	doc = harborRequest(saved)
	quayID := jsonList(doc, "structures")[0].(map[string]any)["id"].(string)
	for i, width := range []int{350, 400} {
		code := string(rune('A' + i))
		g := map[string]any{
			"type": "pier", "key_prefix": fmt.Sprintf("g%d-", i+1), "origin": map[string]int{"x": 6000 + i*9000, "y": 2000}, "root_id": quayID,
			"rotation_dd": 900, "length_cm": 6000, "width_cm": 300, "code": code, "sides": []string{"left", "right"},
			"berths_per_side": 4, "berth_width_cm": width, "berth_length_cm": 1200, "depth_cm": 350,
			"numbering": map[string]any{"scheme": "continuous", "prefix": code + "-"},
		}
		gen := w.owner.expect(t, 200, "POST", P+"/generate", map[string]any{"document": doc, "generator": g})
		el := gen["elements"].(map[string]any)
		doc["structures"] = append(jsonList(doc, "structures"), jsonList(el, "structures")...)
		doc["berths"] = append(jsonList(doc, "berths"), jsonList(el, "berths")...)
	}
	w.owner.expect(t, 200, "PUT", P, doc, "If-Match", etagOf(saved["version"]))
}

func TestAcceptance_M2_Pricing(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		c := clock.NewFake(time.Date(2027, 3, 1, 9, 0, 0, 0, time.UTC))
		h := newRegistryHarness(t, db, c)
		ctx := context.Background()
		own, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Listini SRL", MarinaName: "Marina Listini", MarinaSlug: "listini",
		})
		if err != nil {
			t.Fatal(err)
		}
		w := &pricingWorld{h: h, owner: h.cookieClient(), marina: own.MarinaID, M: A + "/marinas/" + own.MarinaID}
		w.owner.login(t, "owner@porto.test")
		o, M := w.owner, w.M

		// 1. Categories from the map, revenue classes (implicit defaults), items.
		w.pricingMap(t)
		sync := o.expect(t, 200, "POST", M+"/berth-categories/sync", nil, "Idempotency-Key", "acc-sync-0001")
		if n := len(jsonList(sync, "created")); n != 8 {
			t.Fatalf("sync created %d", n)
		}
		cats := o.expect(t, 200, "GET", M+"/berth-categories", nil)
		usage := map[string]float64{}
		for _, ci := range jsonList(cats, "items") {
			cm := ci.(map[string]any)
			usage[str(cm, "code")] = cm["usage"].(map[string]any)["berths"].(float64)
		}
		if usage["L10"] != 8 || usage["L12"] != 8 || len(jsonList(cats, "unknown_in_use")) != 0 {
			t.Fatalf("category usage %v", usage)
		}
		classes := o.expect(t, 200, "GET", M+"/revenue-classes", nil)
		classID := map[string]string{}
		for _, rc := range jsonList(classes, "items") {
			classID[str(rc.(map[string]any), "code")] = str(rc.(map[string]any), "id")
		}
		trn := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": "TRN-NTT", "name": map[string]string{"it": "Passaggio per notte"}, "item_type": "berth_fee", "unit": "night",
			"revenue_class_id": classID["transit"], "price_includes_vat": true,
		}, "Idempotency-Key", "acc-item-trn-0001")
		orm := o.expect(t, 201, "POST", M+"/items", map[string]any{
			"code": "ORM-ANN", "name": map[string]string{"it": "Ormeggio annuale"}, "item_type": "berth_fee", "unit": "year",
			"revenue_class_id": classID["mooring_annual"],
		})
		trnID, ormID := str(trn, "id"), str(orm, "id")

		// 2. List T-2027 (transit, annual), periods, bands, rules; valid; activate.
		doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
			"code": "T-2027", "name": map[string]string{"it": "Listino 2027"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
			"contract_types": []string{"transit", "annual"},
		}, "Idempotency-Key", "acc-list-0001")
		listID := str(doc, "id")
		L := M + "/price-lists/" + listID
		body := map[string]any{
			"code": "T-2027", "name": map[string]string{"it": "Listino 2027"}, "valid_from": "2027-01-01", "valid_to": "2027-12-31",
			"contract_types": []string{"transit", "annual"},
			"settings": map[string]any{
				"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"},
				"min_beam_ratio": map[string]int{"num": 1, "den": 3}, "rounding": map[string]string{"*": "none", "annual": "euro"},
				"primary_items": map[string]string{"transit": trnID, "annual": ormID},
			},
			"periods": []map[string]any{
				{
					"key": "p-bassa", "code": "BASSA", "name": map[string]string{"it": "Bassa"}, "kind": "season",
					"ranges": []map[string]string{{"from": "2027-01-01", "to": "2027-05-31"}, {"from": "2027-10-01", "to": "2027-12-31"}},
				},
				{
					"key": "p-alta", "code": "ALTA", "name": map[string]string{"it": "Alta"}, "kind": "season",
					"ranges": []map[string]string{{"from": "2027-06-01", "to": "2027-09-30"}},
				},
			},
			"bands": []map[string]any{{"key": "b10", "code": "L10", "max_cm": 1000, "category": "L10"}, {"key": "b12", "code": "L12", "max_cm": 1200, "category": "L12"}},
			"rules": []map[string]any{
				{
					"key": "r-bassa", "stage": "base", "name": "Passaggio bassa", "item_id": trnID, "basis": "per_meter", "unit": "night", "unit_cents": 300,
					"scope": map[string]any{"periods": []string{"BASSA"}},
				},
				{
					"key": "r-alta", "stage": "base", "name": "Passaggio alta", "item_id": trnID, "basis": "per_meter", "unit": "night", "unit_cents": 750,
					"scope": map[string]any{"periods": []string{"ALTA"}},
				},
				{
					"key": "r-ann", "stage": "base", "name": "Annuale", "item_id": ormID, "basis": "length_band", "unit": "year",
					"amounts": map[string]int{"L10": 380000, "L12": 490000},
				},
			},
		}
		saved := o.expect(t, 200, "PUT", L, body, "If-Match", `"1"`)
		if len(saved["id_map"].(map[string]any)) != 7 {
			t.Fatalf("id_map %v", saved["id_map"])
		}
		val := o.expect(t, 200, "POST", L+"/validate", nil)
		for _, is := range jsonList(val, "issues") {
			if str(is.(map[string]any), "severity") == "error" {
				t.Fatalf("validation error %v", is)
			}
		}
		act := o.expect(t, 200, "POST", L+"/activate", map[string]any{}, "If-Match", etagOf(saved["document"].(map[string]any)["version"]))
		active := act["document"].(map[string]any)
		if str(active, "status") != "active" || str(active, "content_sha256") == "" {
			t.Fatalf("activated %v", active)
		}

		// 3. 3 nights in ALTA, 11,34 m: 11,34 → 11,40 m (10 cm up); 750 ¢/m × 1140 / 100 = 8550 ¢ per night;
		// × 3 = 25650 ¢ = 256,50 € VAT included; net = half_even(25650 × 10000 / 12200) = 21025, VAT 4625.
		sim := o.expect(t, 200, "POST", M+"/pricing/simulate", map[string]any{
			"contract_type": "transit", "arrival": "2027-07-10", "departure": "2027-07-13", "category": "L12",
			"vessel": map[string]any{"loa_cm": 1134, "beam_cm": 380, "hull_type": "monohull_sail"},
		})
		if sim["total_cents"].(float64) != 25650 || sim["net_cents"].(float64) != 21025 || sim["vat_cents"].(float64) != 4625 {
			t.Fatalf("simulate total %v net %v vat %v", sim["total_cents"], sim["net_cents"], sim["vat_cents"])
		}
		if res := sim["resolved"].(map[string]any); str(res, "price_list_id") != listID || str(res, "quoted_on") != "2027-03-01" {
			t.Fatalf("resolved %v", res)
		}

		// 4. Matrix: L12 × ALTA = 12,00 m × 7,50 € = 90,00 €; every cell of every contract type = its probe (INV-23).
		checked := 0
		for _, ct := range []string{"transit", "annual"} {
			m := o.expect(t, 200, "GET", L+"/matrix?contract_type="+ct, nil)
			rows, cols := jsonList(m, "rows"), jsonList(m, "columns")
			for _, cell := range jsonList(m, "cells") {
				cm := cell.(map[string]any)
				if cm["total_cents"] == nil {
					t.Fatalf("%s cell without price: %v", ct, cm)
				}
				row := str(rows[int(cm["row"].(float64))].(map[string]any), "key")
				col := str(cols[int(cm["col"].(float64))].(map[string]any), "key")
				if ct == "transit" && row == "L12" && col == "ALTA" && cm["total_cents"].(float64) != 9000 {
					t.Fatalf("L12 × ALTA = %v", cm["total_cents"])
				}
				// Annual L10: 3 800,00 € VAT excluded + 22 % = 4 636,00 € (already a whole euro).
				if ct == "annual" && row == "L10" && (cm["total_cents"].(float64) != 463600 || cm["net_cents"].(float64) != 380000) {
					t.Fatalf("annual L10 = %v", cm["total_cents"])
				}
				s := o.expect(t, 200, "POST", M+"/pricing/simulate", cm["probe"])
				if s["total_cents"] != cm["total_cents"] || s["net_cents"] != cm["net_cents"] {
					t.Fatalf("%s %s×%s: matrix %v, simulate %v", ct, row, col, cm["total_cents"], s["total_cents"])
				}
				checked++
			}
		}
		if checked != 6 {
			t.Fatalf("checked %d cells", checked)
		}

		// 5. Coverage: no gaps; a copy without the BASSA rule has the gap.
		cov := o.expect(t, 200, "GET", L+"/coverage", nil)
		if len(jsonList(cov, "gaps")) != 0 || cov["checked"].(float64) == 0 {
			t.Fatalf("coverage %v", cov)
		}
		cp := o.expect(t, 201, "POST", L+"/renewal", map[string]any{
			"dry_run": false, "code": "T-2027-B", "name": map[string]string{"it": "Copia"}, "shift_years": 0, "amount_rounding": "none",
		})
		cpBody := harborRequest(cp)
		for _, k := range []string{"status", "issues", "source_price_list_id", "content_sha256", "activated_at", "archived_at"} {
			delete(cpBody, k)
		}
		var kept []any
		for _, r := range jsonList(cpBody, "rules") {
			if str(r.(map[string]any), "name") != "Passaggio bassa" {
				kept = append(kept, r)
			}
		}
		cpBody["rules"] = kept
		cpSaved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(cp, "id"), cpBody, "If-Match", etagOf(cp["version"]))
		cpDoc := cpSaved["document"].(map[string]any)
		cov = o.expect(t, 200, "GET", M+"/price-lists/"+str(cp, "id")+"/coverage", nil)
		gaps := jsonList(cov, "gaps")
		if len(gaps) == 0 {
			t.Fatal("no gap after removing the BASSA rule")
		}
		for _, g := range gaps {
			if str(g.(map[string]any), "contract_type") != "transit" || str(g.(map[string]any), "period") != "BASSA" {
				t.Fatalf("unexpected gap %v", g)
			}
		}

		// 6. The active list is immutable: 409, and the database refuses a raw update (INV-22).
		r := o.do(t, "PUT", L, body, "If-Match", etagOf(active["version"]))
		if r.status != 409 || r.problemType() != "price_list_immutable" {
			t.Fatalf("save active: %d %s", r.status, r.body)
		}
		_, err = db.NewRaw("UPDATE price_rules SET body = ? WHERE price_list_id = ?", `{"basis":"flat","amount_cents":1}`, listID).Exec(ctx)
		if !store.IsImmutableViolation(err) {
			t.Fatalf("raw update: %v", err)
		}

		// 7. Renewal dry run +5 %: every cell +500 bp ± rounding; apply; activation at a date truncates; overlap.
		renew := map[string]any{
			"dry_run": true, "code": "T-2028", "name": map[string]string{"it": "Listino 2028"}, "shift_years": 1, "amount_rounding": "none",
			"adjustments": []map[string]any{{"scope": "all", "percent_bp": 500}},
		}
		prev := o.expect(t, 200, "POST", L+"/renewal", renew)
		cells := 0
		for _, df := range jsonList(prev, "diffs") {
			for _, cell := range jsonList(df.(map[string]any), "cells") {
				bp := cell.(map[string]any)["delta_bp"].(float64)
				// ± rounding: unit prices are whole cents (750 ¢/m × 1,05 = 787,5 → 788, i.e. +507 bp).
				if bp < 480 || bp > 520 {
					t.Fatalf("delta_bp %v", bp)
				}
				cells++
			}
		}
		if cells == 0 {
			t.Fatal("empty diff")
		}
		renew["dry_run"] = false
		next := o.expect(t, 201, "POST", L+"/renewal", renew, "Idempotency-Key", "acc-renew-0001")
		if str(next, "status") != "draft" || str(next, "valid_from") != "2028-01-01" {
			t.Fatalf("renewed %v", next)
		}
		// The copy T-2027-B (transit + annual) from 1 July: T-2027 is truncated to 30 June.
		res := o.expect(t, 200, "POST", M+"/price-lists/"+str(cp, "id")+"/activate", map[string]any{"effective_from": "2027-07-01"}, "If-Match", etagOf(cpDoc["version"]))
		// It lacks the BASSA rule, which is a coverage gap but not a validation error.
		tr := jsonList(res, "truncated")
		if len(tr) != 1 || str(tr[0].(map[string]any), "id") != listID || str(tr[0].(map[string]any), "valid_to") != "2027-06-30" {
			t.Fatalf("truncated %v", tr)
		}
		// A list starting on T-2027's valid_from overlaps it.
		third := o.expect(t, 201, "POST", L+"/renewal", map[string]any{"dry_run": false, "code": "T-2027-C", "name": map[string]string{"it": "C"}, "shift_years": 0, "amount_rounding": "none"})
		r = o.do(t, "POST", M+"/price-lists/"+str(third, "id")+"/activate", map[string]any{}, "If-Match", etagOf(third["version"]))
		if r.status != 409 || r.problemType() != "price_list_overlap" {
			t.Fatalf("overlap: %d %s", r.status, r.body)
		}

		// 8. Roles and isolation.
		dock := h.member(t, o, own.MarinaID, "dock@porto.test", "dock")
		yard := h.member(t, o, own.MarinaID, "yard@porto.test", "yard")
		dock.expect(t, 200, "POST", M+"/pricing/simulate", map[string]any{
			"contract_type": "transit", "arrival": "2027-07-10", "departure": "2027-07-11",
			"vessel": map[string]any{"loa_cm": 1000, "beam_cm": 350},
		})
		if r := dock.do(t, "POST", M+"/price-lists", map[string]any{
			"code": "X", "name": map[string]string{"it": "x"}, "valid_from": "2027-01-01",
			"valid_to": "2027-12-31", "contract_types": []string{"transit"},
		}); r.status != 403 {
			t.Fatalf("dock create: %d", r.status)
		}
		if r := yard.do(t, "GET", L+"/matrix", nil); r.status != 403 {
			t.Fatalf("yard matrix: %d", r.status)
		}
		other, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "other@porto.test", Name: "Other", Password: password, Organization: "Altro", MarinaName: "Altra", MarinaSlug: "altra",
		})
		if err != nil {
			t.Fatal(err)
		}
		oc := h.cookieClient()
		oc.login(t, "other@porto.test")
		OM := A + "/marinas/" + other.MarinaID
		for _, p := range []string{OM + "/price-lists/" + listID, OM + "/items/" + trnID, L, M + "/items/" + trnID} {
			if r := oc.do(t, "GET", p, nil); r.status != 404 {
				t.Fatalf("other marina %s: %d", p, r.status)
			}
		}

		// 9. Outbox: config.pricing_changed for create, save, activated, renewed; no personal data; audit rows.
		var payloads []string
		if err := db.NewSelect().Table("outbox_events").Column("payload").Where("type = ?", event.ConfigPricingChanged).
			Where("marina_id = ?", own.MarinaID).Scan(ctx, &payloads); err != nil {
			t.Fatal(err)
		}
		changes := map[string]int{}
		for _, p := range payloads {
			var d event.PricingChangedData
			if err := json.Unmarshal([]byte(p), &d); err != nil {
				t.Fatal(err)
			}
			changes[d.Change]++
			for _, s := range []string{"Listino", "owner@", "Passaggio", "T-2027"} {
				if strings.Contains(p, s) {
					t.Fatalf("event payload carries %q: %s", s, p)
				}
			}
		}
		for _, ch := range []string{"created", "saved", "activated", "renewed"} {
			if changes[ch] == 0 {
				t.Fatalf("no %s event: %v", ch, changes)
			}
		}
		for _, a := range []string{"price_list.create", "price_list.save", "price_list.activate", "price_list.truncate", "price_list.renew", "item.create", "berth_category.sync"} {
			if n, _ := store.CountAudit(ctx, db.DB, a); n == 0 {
				t.Fatalf("no audit %s", a)
			}
		}
		assertNoPII(t, h.logs.String(), "owner@porto.test", "Porto Listini SRL")
	})
}
