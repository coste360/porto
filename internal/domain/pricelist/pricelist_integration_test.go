//go:build integration

package pricelist

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func seqsByName(doc *Document) map[string]int64 {
	out := map[string]int64{}
	for _, r := range doc.Rules {
		out[r.Name] = r.Seq
	}
	return out
}

func TestPriceList_SaveDocumentDiff(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		l, err := e.pl.Create(ctx, e.Owner, e.MarinaID, CreateInput{
			Code: "T-2027", Name: e.transitDoc("x", 2027).Name, ValidFrom: d("2027-01-01"), ValidTo: d("2027-12-31"),
			ContractTypes: []pricing.ContractType{pricing.ContractTransit, pricing.ContractAnnual},
		})
		if err != nil {
			t.Fatal(err)
		}
		if l.Status != store.PriceListDraft || l.Settings.DimensionSource != pricing.DimensionVessel || len(l.Rules) != 0 {
			t.Fatalf("new draft: %+v", l)
		}
		res, err := e.pl.Save(ctx, e.Owner, e.MarinaID, l.ID, l.Version, e.transitDoc("T-2027", 2027))
		if err != nil {
			t.Fatal(err)
		}
		doc := res.Document
		if len(res.IDMap) != 9 || doc.Version != 2 {
			t.Fatalf("id_map %d, version %d", len(res.IDMap), doc.Version)
		}
		for _, k := range []string{"p-bassa", "b12", "r-alta"} {
			if !store.ValidID(res.IDMap[k]) {
				t.Fatalf("id_map[%s] = %q", k, res.IDMap[k])
			}
		}
		if doc.Rules[0].ID != res.IDMap["r-bassa"] || doc.Periods[1].ID != res.IDMap["p-alta"] || doc.Bands[2].ID != res.IDMap["b14"] {
			t.Fatal("ids do not follow the document order")
		}
		want := map[string]int64{"Passaggio bassa": 1, "Passaggio alta": 2, "Annuale": 3, "Auto": 4}
		if got := seqsByName(doc); !mapsEqual(got, want) {
			t.Fatalf("seqs = %v", got)
		}
		if hasErrors(res.Issues) {
			t.Fatalf("issues: %+v", res.Issues)
		}
		// Second save: change one rule body, drop one, add one, swap two bands, rename a period.
		req := docToSave(doc)
		req.Rules[1].UnitCents = cents(800)
		req.Rules = slices.Delete(req.Rules, 3, 4) // Auto
		req.Rules = append(req.Rules, Rule{Key: "r-new", Rule: pricing.Rule{
			Stage: pricing.StageModifier, Name: "Multiscafo",
			TargetItemTypes: []pricing.ItemType{pricing.ItemBerthFee}, Conditions: pricing.Conditions{HullTypes: []string{"catamaran"}},
			Effect: pricing.Effect{PercentBP: ptr(int64(5000))},
		}})
		req.Bands[0], req.Bands[1] = req.Bands[1], req.Bands[0]
		req.Periods[0].Name.It = "Bassa stagione"
		res2, err := e.pl.Save(ctx, e.Owner, e.MarinaID, l.ID, doc.Version, req)
		if err != nil {
			t.Fatal(err)
		}
		got := seqsByName(res2.Document)
		if got["Passaggio bassa"] != 1 || got["Annuale"] != 3 || got["Passaggio alta"] != 5 || got["Multiscafo"] != 6 || len(got) != 4 {
			t.Fatalf("seqs after edit = %v", got)
		}
		if len(res2.IDMap) != 1 || res2.IDMap["r-new"] == "" {
			t.Fatalf("id_map = %v", res2.IDMap)
		}
		if res2.Document.Bands[0].Code != "L12" || res2.Document.Bands[0].ID != doc.Bands[1].ID {
			t.Fatalf("band order/id: %+v", res2.Document.Bands)
		}
		if res2.Document.Periods[0].Name.It != "Bassa stagione" || res2.Document.Periods[0].ID != doc.Periods[0].ID {
			t.Fatal("period update lost")
		}
		// A modifier is stored without item_id; active defaults to true.
		for _, r := range res2.Document.Rules {
			if r.Name == "Multiscafo" && (r.ItemID != "" || r.Active == nil || !*r.Active) {
				t.Fatalf("modifier = %+v", r)
			}
		}
		// Shape errors: unknown id, duplicate key, bad stage, unknown item.
		bad := docToSave(res2.Document)
		bad.Periods[0].ID = store.NewID()
		bad.Bands = append(bad.Bands, Band{Key: "r-new2", Code: "X"}, Band{Key: "r-new2", Code: "Y"})
		bad.Rules[0].Stage = "nope"
		bad.Rules[1].ItemID = store.NewID()
		_, err = e.pl.Save(ctx, e.Owner, e.MarinaID, l.ID, res2.Document.Version, bad)
		var v *auth.ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("bad save: %v", err)
		}
		fields := map[string]bool{}
		for _, f := range v.Fields {
			fields[f.Field] = true
		}
		for _, f := range []string{"periods[0].id", "bands[4].key", "rules[0].stage"} {
			if !fields[f] {
				t.Fatalf("missing field error %s in %+v", f, v.Fields)
			}
		}
		bad = docToSave(res2.Document)
		bad.Rules[1].ItemID = store.NewID()
		if _, err := e.pl.Save(ctx, e.Owner, e.MarinaID, l.ID, res2.Document.Version, bad); !errors.As(err, &v) || v.Fields[0].Field != "rules[1].item_id" {
			t.Fatalf("unknown item: %v", err)
		}
	})
}

func mapsEqual(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestPriceList_SaveVersionConflict(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		doc := e.createList(t, e.transitDoc("T-2027", 2027))
		ctx := fixture.Ctx()
		if _, err := e.pl.Save(ctx, e.Owner, e.MarinaID, doc.ID, doc.Version-1, docToSave(doc)); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale save: %v", err)
		}
		if err := e.pl.Delete(ctx, e.Owner, e.MarinaID, doc.ID, 99); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale delete: %v", err)
		}
		if _, err := e.pl.Activate(ctx, e.Owner, e.MarinaID, doc.ID, 99, calendar.Date{}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale activate: %v", err)
		}
	})
}

func TestPriceList_PublishedIsImmutable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		doc := e.createList(t, e.transitDoc("T-2027", 2027))
		act := e.activate(t, doc, calendar.Date{}).Document
		ctx := fixture.Ctx()
		if act.Status != store.PriceListActive || act.ContentSHA256 == nil || *act.ContentSHA256 == "" || act.ActivatedAt == nil {
			t.Fatalf("activated: %+v", act)
		}
		// Service: 409.
		if _, err := e.pl.Save(ctx, e.Owner, e.MarinaID, act.ID, act.Version, docToSave(act)); !errors.Is(err, ErrImmutable) {
			t.Fatalf("save active: %v", err)
		}
		if err := e.pl.Delete(ctx, e.Owner, e.MarinaID, act.ID, act.Version); !errors.Is(err, ErrImmutable) {
			t.Fatalf("delete active: %v", err)
		}
		if _, err := e.pl.Activate(ctx, e.Owner, e.MarinaID, act.ID, act.Version, calendar.Date{}); !errors.Is(err, ErrImmutable) {
			t.Fatalf("re-activate: %v", err)
		}
		// Database: every write on the children and on the pricing columns fails in the triggers (INV-22).
		raw := func(name, q string, args ...any) {
			t.Helper()
			_, err := db.NewRaw(q, args...).Exec(context.Background())
			if err == nil || !store.IsImmutableViolation(err) {
				t.Errorf("%s: want trigger error, got %v", name, err)
			}
		}
		raw("rule amount", "UPDATE price_rules SET body = ? WHERE price_list_id = ?", `{"basis":"flat"}`, act.ID)
		raw("rule priority", "UPDATE price_rules SET priority = 5 WHERE price_list_id = ?", act.ID)
		raw("rule delete", "DELETE FROM price_rules WHERE price_list_id = ?", act.ID)
		raw("rule insert", "INSERT INTO price_rules (id, marina_id, price_list_id, seq, stage, name, priority, stackable, active, body, created_at, updated_at) VALUES (?, ?, ?, 99, 'modifier', 'x', 0, ?, ?, '{}', ?, ?)",
			store.NewID(), e.MarinaID, act.ID, false, true, e.Clock.Now(), e.Clock.Now())
		raw("period update", "UPDATE rate_periods SET ranges = '[]' WHERE price_list_id = ?", act.ID)
		raw("period delete", "DELETE FROM rate_periods WHERE price_list_id = ?", act.ID)
		raw("band update", "UPDATE price_list_bands SET max_cm = 1 WHERE price_list_id = ?", act.ID)
		raw("band delete", "DELETE FROM price_list_bands WHERE price_list_id = ?", act.ID)
		raw("settings", "UPDATE price_lists SET settings = '{}' WHERE id = ?", act.ID)
		raw("code", "UPDATE price_lists SET code = 'X' WHERE id = ?", act.ID)
		raw("valid_to extension", "UPDATE price_lists SET valid_to = ? WHERE id = ?", d("2028-06-30"), act.ID)
		raw("back to draft", "UPDATE price_lists SET status = 'draft' WHERE id = ?", act.ID)
		raw("delete list", "DELETE FROM price_lists WHERE id = ?", act.ID)
		// Allowed: truncation of valid_to, then archive; an archived list never becomes active again.
		if _, err := db.NewRaw("UPDATE price_lists SET valid_to = ? WHERE id = ?", d("2027-11-30"), act.ID).Exec(context.Background()); err != nil {
			t.Fatalf("truncation refused: %v", err)
		}
		arch, err := e.pl.Archive(ctx, e.Owner, e.MarinaID, act.ID, act.Version)
		if err != nil {
			t.Fatal(err)
		}
		raw("archived back to active", "UPDATE price_lists SET status = 'active' WHERE id = ?", arch.ID)
		raw("archived rule", "UPDATE price_rules SET name = 'y' WHERE price_list_id = ?", arch.ID)
		if _, err := e.pl.Archive(ctx, e.Owner, e.MarinaID, arch.ID, arch.Version); !errors.Is(err, ErrImmutable) {
			t.Fatalf("archive archived: %v", err)
		}
		// The store maps trigger errors to ErrImmutable.
		l, err := store.GetPriceList(context.Background(), db.DB, e.scope(t), arch.ID)
		if err != nil {
			t.Fatal(err)
		}
		l.Code = "CHANGED"
		if err := store.UpdatePriceListHeader(context.Background(), db.DB, e.scope(t), l, l.Version); !errors.Is(err, store.ErrImmutable) {
			t.Fatalf("store mapping: %v", err)
		}
	})
}

func TestPriceList_ActivateRequiresValid(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		req := e.transitDoc("T-2027", 2027)
		// Two overlapping seasons and a primary item missing: blocking errors, but the draft is saved.
		req.Periods[1].Ranges = append(req.Periods[1].Ranges, pricing.DateRange{From: d("2027-05-01"), To: d("2027-05-10")})
		delete(req.Settings.PrimaryItems, pricing.ContractAnnual)
		doc := e.createList(t, req)
		if !hasErrors(doc.Issues) {
			t.Fatalf("expected blocking issues, got %+v", doc.Issues)
		}
		_, err := e.pl.Activate(ctx, e.Owner, e.MarinaID, doc.ID, doc.Version, calendar.Date{})
		var inv *InvalidError
		if !errors.As(err, &inv) || len(inv.Issues) == 0 {
			t.Fatalf("activate invalid: %v", err)
		}
		codes := map[string]bool{}
		for _, is := range inv.Issues {
			codes[is.Code] = true
			if is.Severity != pricing.SeverityError {
				t.Fatalf("warning in blocking issues: %+v", is)
			}
		}
		if !codes[pricing.IssuePeriodOverlap] || !codes[pricing.IssuePrimaryItemMissing] {
			t.Fatalf("codes = %v", codes)
		}
		// Dry-run validation of a fixed document finds no errors; nothing is written.
		fixed := e.transitDoc("T-2027", 2027)
		issues, err := e.pl.Validate(ctx, e.Owner, e.MarinaID, doc.ID, &fixed)
		if err != nil || hasErrors(issues) {
			t.Fatalf("dry run: %+v %v", issues, err)
		}
		saved, err := e.pl.Validate(ctx, e.Owner, e.MarinaID, doc.ID, nil)
		if err != nil || !hasErrors(saved) {
			t.Fatalf("saved doc validation: %+v %v", saved, err)
		}
		// effective_from outside the validity is a field error.
		if _, err := e.pl.Activate(ctx, e.Owner, e.MarinaID, doc.ID, doc.Version, d("2028-01-01")); !isField(err, "effective_from") {
			t.Fatalf("effective_from: %v", err)
		}
		// Unknown categories are warnings, not errors.
		fixed.Bands[0].Category = "NOPE"
		issues, _ = e.pl.Validate(ctx, e.Owner, e.MarinaID, doc.ID, &fixed)
		found := false
		for _, is := range issues {
			if is.Code == pricing.IssueCategoryUnknown && is.Severity == pricing.SeverityWarning {
				found = true
			}
		}
		if !found {
			t.Fatalf("category_unknown missing: %+v", issues)
		}
	})
}

func isField(err error, field string) bool {
	var v *auth.ValidationError
	if !errors.As(err, &v) {
		return false
	}
	for _, f := range v.Fields {
		if f.Field == field {
			return true
		}
	}
	return false
}

func TestPriceList_ActivateAtDateTruncatesPredecessor(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		first := e.activate(t, e.createList(t, e.transitDoc("T-2027", 2027)), calendar.Date{}).Document
		second := e.createList(t, e.transitDoc("T-2027-B", 2027))
		res := e.activate(t, second, d("2027-07-01"))
		if len(res.Truncated) != 1 || res.Truncated[0].ID != first.ID || res.Truncated[0].ValidTo != d("2027-06-30") {
			t.Fatalf("truncated = %+v", res.Truncated)
		}
		if res.Document.ValidFrom != d("2027-07-01") {
			t.Fatalf("valid_from = %s", res.Document.ValidFrom)
		}
		got, err := e.pl.Get(fixture.Ctx(), e.Owner, e.MarinaID, first.ID)
		if err != nil || got.ValidTo != d("2027-06-30") || got.Version != first.Version+1 || got.Status != store.PriceListActive {
			t.Fatalf("predecessor: %+v %v", got, err)
		}
		// The arrival night picks the list.
		q := func(arr string) string {
			_, r, err := e.pl.Quote(fixture.Ctx(), e.Owner, e.MarinaID, QuoteRequest{
				ContractType: pricing.ContractTransit, Arrival: d(arr), Departure: d(arr).AddDays(1), Vessel: &SimVessel{LOA: 1000, Beam: 350},
			})
			if err != nil {
				t.Fatal(err)
			}
			return r.PriceListCode
		}
		if q("2027-06-30") != "T-2027" || q("2027-07-01") != "T-2027-B" {
			t.Fatal("wrong list per night")
		}
		if e.AuditCount(t, "price_list.truncate") != 1 {
			t.Fatal("truncate audit missing")
		}
		evs := e.Events(t, event.ConfigPricingChanged)
		var data event.PricingChangedData
		if err := json.Unmarshal(evs[len(evs)-1].Payload, &data); err != nil {
			t.Fatal(err)
		}
		if data.Change != event.ChangeActivated || !slices.Equal(data.TruncatedIDs, []string{first.ID}) {
			t.Fatalf("event = %+v", data)
		}
	})
}

func TestPriceList_ActivateOverlapRejected(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		first := e.activate(t, e.createList(t, e.transitDoc("T-2027", 2027)), calendar.Date{}).Document
		// Starting on the predecessor's valid_from: nothing to truncate, so it overlaps.
		second := e.createList(t, e.transitDoc("T-2027-B", 2027))
		_, err := e.pl.Activate(fixture.Ctx(), e.Owner, e.MarinaID, second.ID, second.Version, calendar.Date{})
		var ov *OverlapError
		if !errors.As(err, &ov) || len(ov.Conflicts) != 1 || ov.Conflicts[0].ID != first.ID {
			t.Fatalf("overlap: %v", err)
		}
		// A list for other contract types does not conflict.
		other := e.transitDoc("S-2027", 2027)
		other.ContractTypes = []pricing.ContractType{pricing.ContractMonthly}
		other.Settings.PrimaryItems = map[pricing.ContractType]string{pricing.ContractMonthly: e.trn}
		other.Rules = []Rule{{Key: "m", Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: "Mese", ItemID: e.trn, Basis: pricing.BasisFlat,
			Unit: pricing.UnitMonth, AmountCents: cents(50000),
		}}}
		e.activate(t, e.createList(t, other), calendar.Date{})
		// The next year is free.
		e.activate(t, e.createList(t, e.transitDoc("T-2028", 2028)), calendar.Date{})
		assertOneActivePerNight(t, e)
	})
}

// assertOneActivePerNight checks INV-24 over the active lists of the fixture marina.
func assertOneActivePerNight(t *testing.T, e *env) {
	t.Helper()
	actives, err := store.ActivePriceLists(context.Background(), e.DB.DB, e.scope(t))
	if err != nil {
		t.Fatal(err)
	}
	for i := range actives {
		for j := i + 1; j < len(actives); j++ {
			a, b := actives[i], actives[j]
			if !sharesContractType(a.ContractTypes, b.ContractTypes) {
				continue
			}
			if !a.ValidTo.Before(b.ValidFrom) && !b.ValidTo.Before(a.ValidFrom) {
				t.Fatalf("INV-24: %s [%s,%s] overlaps %s [%s,%s]", a.Code, a.ValidFrom, a.ValidTo, b.Code, b.ValidFrom, b.ValidTo)
			}
		}
	}
}

func TestPriceList_ArchiveAndDelete(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		draft := e.createList(t, e.transitDoc("T-2027", 2027))
		if _, err := e.pl.Archive(ctx, e.Owner, e.MarinaID, draft.ID, draft.Version); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("archive draft: %v", err)
		}
		act := e.activate(t, draft, calendar.Date{}).Document
		arch, err := e.pl.Archive(ctx, e.Owner, e.MarinaID, act.ID, act.Version)
		if err != nil || arch.Status != store.PriceListArchived || arch.ArchivedAt == nil {
			t.Fatalf("archive: %+v %v", arch, err)
		}
		// Archived lists still quote with an explicit id (D27), not by default.
		_, _, err = e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{ContractType: pricing.ContractTransit, Arrival: d("2027-07-01"), Departure: d("2027-07-02"), Vessel: &SimVessel{LOA: 1000, Beam: 300}})
		if !errors.Is(err, ErrNoActiveList) {
			t.Fatalf("no active list: %v", err)
		}
		if _, _, err := e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{PriceListID: arch.ID, ContractType: pricing.ContractTransit, Arrival: d("2027-07-01"), Departure: d("2027-07-02"), Vessel: &SimVessel{LOA: 1000, Beam: 300}}); err != nil {
			t.Fatalf("quote archived: %v", err)
		}
		// Delete a draft with its children; then it is gone.
		d2 := e.createList(t, e.transitDoc("T-2028", 2028))
		if err := e.pl.Delete(ctx, e.Owner, e.MarinaID, d2.ID, d2.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := e.pl.Get(ctx, e.Owner, e.MarinaID, d2.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted: %v", err)
		}
		n, _ := db.NewSelect().Table("price_rules").Where("price_list_id = ?", d2.ID).Count(context.Background())
		if n != 0 {
			t.Fatalf("rules left: %d", n)
		}
		list, err := e.pl.List(ctx, e.Owner, e.MarinaID, ListFilter{})
		if err != nil || len(list) != 1 || list[0].RuleCount != 4 {
			t.Fatalf("list: %+v %v", list, err)
		}
		if f, _ := e.pl.List(ctx, e.Owner, e.MarinaID, ListFilter{Status: store.PriceListDraft}); len(f) != 0 {
			t.Fatalf("status filter: %d", len(f))
		}
		if f, _ := e.pl.List(ctx, e.Owner, e.MarinaID, ListFilter{ContractType: "monthly"}); len(f) != 0 {
			t.Fatalf("contract type filter: %d", len(f))
		}
		if f, _ := e.pl.List(ctx, e.Owner, e.MarinaID, ListFilter{On: d("2027-03-01")}); len(f) != 1 {
			t.Fatalf("on filter: %d", len(f))
		}
	})
}

func TestPriceList_RenewalDryRunAndApply(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		src := e.activate(t, e.createList(t, e.transitDoc("T-2027", 2027)), calendar.Date{}).Document
		req := RenewalRequest{
			DryRun: true, Code: "T-2028", Name: src.Name, ShiftYears: 1, AmountRounding: pricing.AmountRoundNone,
			Adjustments: []pricing.Adjustment{{Scope: pricing.AdjustAll, PercentBP: ptr(int64(500))}},
		}
		preview, doc, err := e.pl.Renew(ctx, e.Owner, e.MarinaID, src.ID, req)
		if err != nil || doc != nil {
			t.Fatalf("dry run: %v", err)
		}
		if preview.Document.ValidFrom != d("2028-01-01") || preview.Document.Periods[1].Ranges[0].From != d("2028-06-01") {
			t.Fatalf("shift: %+v", preview.Document.Periods)
		}
		if preview.Summary.Rules != 4 || preview.Summary.AmountsChanged == 0 {
			t.Fatalf("summary: %+v", preview.Summary)
		}
		if len(preview.Diffs) != 2 {
			t.Fatalf("diffs = %d", len(preview.Diffs))
		}
		for _, df := range preview.Diffs {
			for _, c := range df.Cells {
				if c.DeltaBP == nil || *c.DeltaBP < 490 || *c.DeltaBP > 510 {
					t.Fatalf("%s cell %d/%d delta_bp = %v", df.ContractType, c.Row, c.Col, c.DeltaBP)
				}
			}
		}
		if lists, _ := e.pl.List(ctx, e.Owner, e.MarinaID, ListFilter{}); len(lists) != 1 {
			t.Fatal("dry run wrote a list")
		}
		req.DryRun = false
		_, doc, err = e.pl.Renew(ctx, e.Owner, e.MarinaID, src.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Status != store.PriceListDraft || doc.SourcePriceListID == nil || *doc.SourcePriceListID != src.ID || doc.Code != "T-2028" {
			t.Fatalf("renewed: %+v", doc)
		}
		// Seq preserved; per-metre price +5 %; ids are new.
		if doc.Rules[1].Seq != src.Rules[1].Seq || *doc.Rules[1].UnitCents != money.Cents(788) || doc.Rules[1].ID == src.Rules[1].ID {
			t.Fatalf("rule: %+v", doc.Rules[1])
		}
		if doc.Periods[0].Name.It != src.Periods[0].Name.It {
			t.Fatal("period names not copied")
		}
		// The new draft can be saved (next_rule_seq is past the copied seqs) and activated.
		res, err := e.pl.Save(ctx, e.Owner, e.MarinaID, doc.ID, doc.Version, func() SaveRequest {
			s := docToSave(doc)
			s.Rules = append(s.Rules, Rule{Key: "k", Rule: pricing.Rule{
				Stage: pricing.StageDiscount, Name: "Promo", DiscountKind: pricing.DiscountPromo,
				PromoCode: "estate", Effect: pricing.Effect{PercentBP: ptr(int64(1000))},
			}})
			return s
		}())
		if err != nil {
			t.Fatal(err)
		}
		if seqsByName(res.Document)["Promo"] <= src.Rules[3].Seq {
			t.Fatal("new seq collides with copied ones")
		}
		for _, r := range res.Document.Rules {
			if r.Name == "Promo" && r.PromoCode != "ESTATE" {
				t.Fatalf("promo code not upper case: %q", r.PromoCode)
			}
		}
		e.activate(t, res.Document, calendar.Date{})
		// A code already taken is a conflict.
		if _, _, err := e.pl.Renew(ctx, e.Owner, e.MarinaID, src.ID, req); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate code: %v", err)
		}
		if _, _, err := e.pl.Renew(ctx, e.Owner, e.MarinaID, src.ID, RenewalRequest{Code: "Z", Name: src.Name, ShiftYears: 9, AmountRounding: "x"}); !isField(err, "shift_years") {
			t.Fatalf("bad request: %v", err)
		}
	})
}

func TestPriceList_WritesAuditAndOutbox(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		doc := e.createList(t, e.transitDoc("T-2027", 2027))
		act := e.activate(t, doc, calendar.Date{}).Document
		if _, _, err := e.pl.Renew(ctx, e.Owner, e.MarinaID, act.ID, RenewalRequest{Code: "T-2028", Name: act.Name, ShiftYears: 1, AmountRounding: pricing.AmountRoundEuro}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.pl.Archive(ctx, e.Owner, e.MarinaID, act.ID, act.Version); err != nil {
			t.Fatal(err)
		}
		for _, a := range []string{"price_list.create", "price_list.save", "price_list.activate", "price_list.renew", "price_list.archive"} {
			if e.AuditCount(t, a) != 1 {
				t.Errorf("audit %s = %d", a, e.AuditCount(t, a))
			}
		}
		changes := map[string]int{}
		for _, ev := range e.Events(t, event.ConfigPricingChanged) {
			var data event.PricingChangedData
			if err := json.Unmarshal(ev.Payload, &data); err != nil {
				t.Fatal(err)
			}
			changes[data.Change]++
			if strings.Contains(string(ev.Payload), "Listino") || strings.Contains(string(ev.Payload), "T-2027") {
				t.Fatalf("event carries names or codes: %s", ev.Payload)
			}
			if ev.MarinaID != e.MarinaID {
				t.Fatalf("event marina = %s", ev.MarinaID)
			}
		}
		for _, c := range []string{event.ChangeCreated, event.ChangeSaved, event.ChangeActivated, event.ChangeRenewed, event.ChangeArchived} {
			if changes[c] < 1 {
				t.Errorf("no %s event (%v)", c, changes)
			}
		}
	})
}

func TestPriceList_Permissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		doc := e.createList(t, e.transitDoc("T-2027", 2027))
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		yard := e.AddUser(t, "yard@porto.test", auth.RoleYard, e.OrgID, e.MarinaID)
		if _, err := e.pl.Create(ctx, dock, e.MarinaID, CreateInput{}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock create: %v", err)
		}
		if _, err := e.pl.Get(ctx, dock, e.MarinaID, doc.ID); err != nil {
			t.Fatalf("dock read: %v", err)
		}
		if _, err := e.pl.Matrix(ctx, yard, e.MarinaID, doc.ID, MatrixParams{}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard matrix: %v", err)
		}
		_, otherMarina, otherOwner := e.OtherOrganization(t)
		if _, err := e.pl.Get(ctx, otherOwner, otherMarina, doc.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("other marina list: %v", err)
		}
		if _, err := e.pl.Get(ctx, otherOwner, e.MarinaID, doc.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other marina path: %v", err)
		}
	})
}
