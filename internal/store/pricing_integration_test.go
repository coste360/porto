//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// Pricing queries (m2-plan §5.1). Rows are inserted directly; the services own validation.

func pricingRows(t *testing.T, db *store.DB, s store.MarinaScope) (*store.RevenueClass, *store.Item, *store.PriceList) {
	t.Helper()
	ctx := context.Background()
	rc := &store.RevenueClass{ID: store.NewID(), Code: "transit", Name: store.JSON(`{"it":"Transiti"}`), Version: 1, CreatedAt: t0, UpdatedAt: t0}
	if err := store.InsertRevenueClass(ctx, db.DB, s, rc); err != nil {
		t.Fatal(err)
	}
	it := &store.Item{
		ID: store.NewID(), Code: "TRN-NTT", Name: store.JSON(`{"it":"Passaggio"}`), Description: store.JSON(`{}`), ItemType: "berth_fee",
		Unit: "night", VATRateBP: 2200, RevenueClassID: rc.ID, Active: true, Version: 1, CreatedAt: t0, UpdatedAt: t0,
	}
	if err := store.InsertItem(ctx, db.DB, s, it); err != nil {
		t.Fatal(err)
	}
	l := &store.PriceList{
		ID: store.NewID(), Code: "T", Name: store.JSON(`{"it":"T"}`), Status: store.PriceListDraft, ValidFrom: calendar.MustParse("2027-01-01"),
		ValidTo: calendar.MustParse("2027-12-31"), ContractTypes: store.Strings{"transit"}, Settings: store.JSON(`{}`), NextRuleSeq: 1,
		Version: 1, CreatedAt: t0, UpdatedAt: t0,
	}
	if err := store.InsertPriceList(ctx, db.DB, s, l); err != nil {
		t.Fatal(err)
	}
	return rc, it, l
}

func TestStore_PricingCatalogScoped(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, a := seedOrgMarina(t, db, "alpha")
		_, b := seedOrgMarina(t, db, "beta")
		sa, _ := store.ResolveMarinaScope(ctx, db.DB, a)
		sb, _ := store.ResolveMarinaScope(ctx, db.DB, b)
		rc, it, _ := pricingRows(t, db, sa)
		if _, err := store.GetItem(ctx, db.DB, sb, it.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("item of another marina: %v", err)
		}
		if _, err := store.GetRevenueClass(ctx, db.DB, sb, rc.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("class of another marina: %v", err)
		}
		if got, _ := store.ItemsByIDs(ctx, db.DB, sb, []string{it.ID, "bad"}); len(got) != 0 {
			t.Fatalf("items by ids across marinas: %d", len(got))
		}
		// Settings: ensure is idempotent, version check, activation seq.
		p := &store.PricingSettings{ID: store.NewID(), DefaultVATRateBP: 2200, Version: 1, CreatedAt: t0, UpdatedAt: t0}
		first, err := store.EnsurePricingSettings(ctx, db.DB, sa, p)
		if err != nil {
			t.Fatal(err)
		}
		again, err := store.EnsurePricingSettings(ctx, db.DB, sa, &store.PricingSettings{ID: store.NewID(), Version: 1, CreatedAt: t0, UpdatedAt: t0})
		if err != nil || again.ID != first.ID || again.DefaultVATRateBP != 2200 {
			t.Fatalf("ensure again: %+v %v", again, err)
		}
		if err := store.BumpActivationSeq(ctx, db.DB, sa); err != nil {
			t.Fatal(err)
		}
		if err := store.BumpActivationSeq(ctx, db.DB, sb); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("bump without row: %v", err)
		}
		got, _ := store.GetPricingSettings(ctx, db.DB, sa)
		if got.ActivationSeq != 1 {
			t.Fatalf("activation seq %d", got.ActivationSeq)
		}
		got.DefaultVATRateBP = 1000
		if err := store.UpdatePricingSettings(ctx, db.DB, sa, got, 9); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("stale settings: %v", err)
		}
		// Counts.
		counts, err := store.RevenueClassItemCounts(ctx, db.DB, sa)
		if err != nil || counts[rc.ID] != 1 {
			t.Fatalf("item counts %v %v", counts, err)
		}
		if err := store.DeleteRevenueClass(ctx, db.DB, sa, rc.ID, 1); err == nil {
			t.Fatal("class with items deleted (FK)")
		}
	})
}

func TestStore_PriceListChildrenReplace(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, a := seedOrgMarina(t, db, "alpha")
		sa, _ := store.ResolveMarinaScope(ctx, db.DB, a)
		_, it, l := pricingRows(t, db, sa)
		band := func(code string, order int) store.PriceListBand {
			return store.PriceListBand{ID: store.NewID(), Code: code, MaxCm: int64(1000 + order), SortOrder: order, CreatedAt: t0, UpdatedAt: t0}
		}
		rule := func(seq int64) store.PriceRule {
			return store.PriceRule{ID: store.NewID(), Seq: seq, Stage: "base", Name: "r", ItemID: it.ID, Active: true, Body: store.JSON(`{"basis":"flat"}`), CreatedAt: t0, UpdatedAt: t0}
		}
		c := &store.PriceListChildren{
			Periods: []store.RatePeriod{{
				ID: store.NewID(), Code: "ALTA", Name: store.JSON(`{"it":"A"}`), Kind: "season", Color: "chart-1",
				Ranges: store.JSON(`[{"from":"2027-06-01","to":"2027-09-30"}]`), CreatedAt: t0, UpdatedAt: t0,
			}},
			Bands: []store.PriceListBand{band("L10", 0), band("L12", 1)},
			Rules: []store.PriceRule{rule(1), rule(2)},
		}
		if err := store.InsertPriceListChildren(ctx, db.DB, sa, l.ID, c); err != nil {
			t.Fatal(err)
		}
		before, err := store.LoadPriceListChildren(ctx, db.DB, sa, l.ID)
		if err != nil || len(before.Bands) != 2 || len(before.Rules) != 2 {
			t.Fatalf("load %+v %v", before, err)
		}
		// Reorder bands, swap the rule seqs, drop the period: the unique indexes never collide.
		next := &store.PriceListChildren{
			Periods: []store.RatePeriod{},
			Bands:   []store.PriceListBand{before.Bands[1], before.Bands[0]},
			Rules:   []store.PriceRule{before.Rules[0], before.Rules[1]},
		}
		next.Bands[0].SortOrder, next.Bands[1].SortOrder = 0, 1
		next.Rules[0].Seq, next.Rules[1].Seq = 2, 1
		if err := store.ReplacePriceListChildren(ctx, db.DB, sa, l.ID, next, before); err != nil {
			t.Fatal(err)
		}
		after, _ := store.LoadPriceListChildren(ctx, db.DB, sa, l.ID)
		if len(after.Periods) != 0 || after.Bands[0].Code != "L12" || after.Bands[0].ID != before.Bands[1].ID || after.Rules[0].ID != before.Rules[1].ID {
			t.Fatalf("after %+v", after)
		}
		// References and the in-use rule.
		refs, err := store.ItemPriceListRefs(ctx, db.DB, sa, it.ID)
		if err != nil || len(refs) != 1 || refs[0] != l.ID {
			t.Fatalf("refs %v %v", refs, err)
		}
		if err := store.DeleteItem(ctx, db.DB, sa, it.ID, 1); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("delete referenced item: %v", err)
		}
		rows, err := store.ListPriceLists(ctx, db.DB, sa, store.PriceListFilter{ContractType: "transit", On: calendar.MustParse("2027-02-01")})
		if err != nil || len(rows) != 1 || rows[0].RuleCount != 2 {
			t.Fatalf("list %+v %v", rows, err)
		}
		if err := store.DeletePriceList(ctx, db.DB, sa, l.ID, 1); err != nil {
			t.Fatal(err)
		}
		if gone, _ := store.LoadPriceListChildren(ctx, db.DB, sa, l.ID); len(gone.Rules) != 0 || len(gone.Bands) != 0 {
			t.Fatal("children survived the cascade")
		}
	})
}

func TestStore_PricingInputs(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, a := seedOrgMarina(t, db, "alpha")
		_, b := seedOrgMarina(t, db, "beta")
		sa, _ := store.ResolveMarinaScope(ctx, db.DB, a)
		sb, _ := store.ResolveMarinaScope(ctx, db.DB, b)
		m := newMapRow(t, db, sa, 0)
		for i, spec := range []struct {
			class          string
			length, maxLOA int64
		}{{"L12", 1200, 1180}, {"L12", 1250, 1220}, {"", 1000, 990}} {
			br := berthRow(m.ID, "B"+string(rune('1'+i)), int64(i)*400)
			br.MarinaID, br.PriceClass, br.LengthCm, br.MaxLOACm, br.Attributes = a, spec.class, spec.length, spec.maxLOA, store.Strings{}
			if _, err := db.NewInsert().Model(br).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				r, err := store.PricingResourceByID(ctx, db.DB, sa, "berth", br.ID)
				if err != nil || r.PriceClass != "L12" || r.LengthCm != 1200 || r.WidthCm != 300 || len(r.GroupIDs) != 0 {
					t.Fatalf("resource %+v %v", r, err)
				}
				if _, err := store.PricingResourceByID(ctx, db.DB, sb, "berth", br.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("resource of another marina: %v", err)
				}
				if _, err := store.PricingResourceByID(ctx, db.DB, sa, "boat", br.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("bad type: %v", err)
				}
			}
		}
		dims, err := store.PriceClassMaxDims(ctx, db.DB, sa)
		if err != nil || len(dims) != 1 || dims[0].Code != "L12" || dims[0].MaxLOACm != 1220 || dims[0].MaxLengthCm != 1250 {
			t.Fatalf("dims %+v %v", dims, err)
		}
		without, err := store.ElementsWithoutPriceClass(ctx, db.DB, sa)
		if err != nil || without["berth"] != 1 || without["land_resource"] != 0 {
			t.Fatalf("without %v %v", without, err)
		}
		counts, err := store.PriceClassCounts(ctx, db.DB, sa)
		if err != nil || counts["berth"]["L12"] != 2 {
			t.Fatalf("counts %v %v", counts, err)
		}
		if _, err := store.PricingVesselByID(ctx, db.DB, sa, store.NewID()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("vessel: %v", err)
		}
		if _, err := store.CustomerCategoryByID(ctx, db.DB, sa, "x"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("customer: %v", err)
		}
	})
}
