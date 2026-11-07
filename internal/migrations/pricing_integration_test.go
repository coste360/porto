//go:build integration

package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// pricingRows inserts a marina with a revenue class, an item and a draft price list with one period, one band
// and one rule. It returns the marina and list ids.
func pricingRows(t *testing.T, db *store.DB) (marinaID, listID string) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(time.Now())
	org := &store.Organization{ID: store.NewID(), Name: "O", BillingAddress: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now}
	m := &store.Marina{
		ID: store.NewID(), OrganizationID: org.ID, Slug: "porto-p", Name: "Porto P", Kind: "marina",
		Timezone: "Europe/Rome", Currency: "EUR", DefaultLocale: "it", Locales: store.Strings{"it"},
		Contacts: store.JSON(`{}`), Address: store.JSON(`{}`), Settings: store.JSON(`{}`), SettingsVersion: 1,
		Theme: store.JSON(`{}`), AllowedEmbedOrigins: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	rc := &store.RevenueClass{ID: store.NewID(), MarinaID: m.ID, Code: "transit", Name: store.JSON(`{"it":"Transiti"}`), Version: 1, CreatedAt: now, UpdatedAt: now}
	item := &store.Item{
		ID: store.NewID(), MarinaID: m.ID, Code: "TRN-NTT", Name: store.JSON(`{"it":"Transiti notte"}`), Description: store.JSON(`{}`),
		ItemType: "berth_fee", Unit: "night", VATRateBP: 2200, PriceIncludesVAT: true, RevenueClassID: rc.ID, Active: true,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	pl := &store.PriceList{
		ID: store.NewID(), MarinaID: m.ID, Code: "T-2027", Name: store.JSON(`{"it":"Listino"}`), Status: store.PriceListDraft,
		ValidFrom: calendar.MustParse("2027-01-01"), ValidTo: calendar.MustParse("2027-12-31"),
		ContractTypes: store.Strings{"transit"}, Settings: store.JSON(`{}`), NextRuleSeq: 2, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	period := &store.RatePeriod{
		ID: store.NewID(), MarinaID: m.ID, PriceListID: pl.ID, Code: "ALTA", Name: store.JSON(`{"it":"Alta"}`), Kind: "season",
		Color: "chart-1", Ranges: store.JSON(`[{"from":"2027-06-16","to":"2027-08-31"}]`), CreatedAt: now, UpdatedAt: now,
	}
	band := &store.PriceListBand{ID: store.NewID(), MarinaID: m.ID, PriceListID: pl.ID, Code: "L12", MaxCm: 1200, SortOrder: 1, CreatedAt: now, UpdatedAt: now}
	rule := &store.PriceRule{
		ID: store.NewID(), MarinaID: m.ID, PriceListID: pl.ID, Seq: 1, Stage: "base", Name: "Night", ItemID: item.ID, Active: true,
		Body: store.JSON(`{"basis":"per_meter","unit":"night","unit_cents":750}`), CreatedAt: now, UpdatedAt: now,
	}
	snap := &store.PriceListItem{
		ID: store.NewID(), MarinaID: m.ID, PriceListID: pl.ID, ItemID: item.ID, Code: item.Code, ItemType: item.ItemType,
		Unit: item.Unit, VATRateBP: 2200, PriceIncludesVAT: true, RevenueClass: rc.Code, Active: true, CreatedAt: now,
	}
	for _, row := range []any{org, m, rc, item, pl, period, band, rule, snap} {
		if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", row, err)
		}
	}
	return m.ID, pl.ID
}

func activate(t *testing.T, db *store.DB, listID string) {
	t.Helper()
	_, err := db.NewUpdate().Table("price_lists").Set("status = 'active'").Set("content_sha256 = ?", "abc").
		Set("activated_at = ?", store.UTC(time.Now())).Set("version = version + 1").Where("id = ?", listID).Exec(context.Background())
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
}

func wantImmutable(t *testing.T, what string, err error) {
	t.Helper()
	if !store.IsImmutableViolation(err) {
		t.Fatalf("%s: want price_list_immutable, got %v", what, err)
	}
}

// TestMigrations_PriceListImmutable: once a list is not a draft, its periods, bands, rules, item snapshot and pricing columns
// cannot change on either dialect (INV-22); only status → archived, valid_to truncation and bookkeeping columns.
func TestMigrations_PriceListImmutable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		marinaID, listID := pricingRows(t, db)
		// Drafts are freely editable.
		if _, err := db.NewUpdate().Table("price_rules").Set("priority = 5").Where("price_list_id = ?", listID).Exec(ctx); err != nil {
			t.Fatalf("draft rule update: %v", err)
		}
		activate(t, db, listID)
		now := store.UTC(time.Now())
		for _, tbl := range []string{"rate_periods", "price_list_bands", "price_rules", "price_list_items"} {
			_, err := db.NewUpdate().Table(tbl).Set("created_at = ?", now).Where("price_list_id = ?", listID).Exec(ctx)
			wantImmutable(t, tbl+" update", err)
			_, err = db.NewDelete().Table(tbl).Where("price_list_id = ?", listID).Exec(ctx)
			wantImmutable(t, tbl+" delete", err)
		}
		_, err := db.NewInsert().Model(&store.PriceListBand{
			ID: store.NewID(), MarinaID: marinaID, PriceListID: listID, Code: "L14", MaxCm: 1400, SortOrder: 2, CreatedAt: now, UpdatedAt: now,
		}).Exec(ctx)
		wantImmutable(t, "band insert", err)
		_, err = db.NewInsert().Model(&store.PriceListItem{
			ID: store.NewID(), MarinaID: marinaID, PriceListID: listID, ItemID: store.NewID(), Code: "X", ItemType: "service",
			Unit: "lump", VATRateBP: 2200, CreatedAt: now,
		}).Exec(ctx)
		wantImmutable(t, "item snapshot insert", err)
		for _, set := range []string{"settings = '{\"x\":1}'", "code = 'OTHER'", "valid_from = '2027-02-01'", "status = 'draft'", "valid_to = '2028-01-31'", "next_rule_seq = 9"} {
			_, err := db.NewUpdate().Table("price_lists").Set(set).Where("id = ?", listID).Exec(ctx)
			wantImmutable(t, "list "+set, err)
		}
		_, err = db.NewDelete().Table("price_lists").Where("id = ?", listID).Exec(ctx)
		wantImmutable(t, "list delete", err)
		// Allowed: truncation, then archiving; archived never goes back to active.
		if _, err := db.NewUpdate().Table("price_lists").Set("valid_to = '2027-06-30'").Set("version = version + 1").
			Set("updated_at = ?", now).Where("id = ?", listID).Exec(ctx); err != nil {
			t.Fatalf("truncate: %v", err)
		}
		if _, err := db.NewUpdate().Table("price_lists").Set("status = 'archived'").Set("archived_at = ?", now).
			Where("id = ?", listID).Exec(ctx); err != nil {
			t.Fatalf("archive: %v", err)
		}
		_, err = db.NewUpdate().Table("price_lists").Set("status = 'active'").Where("id = ?", listID).Exec(ctx)
		wantImmutable(t, "archived → active", err)
		if err := store.MapImmutable(err); err == nil {
			t.Fatal("MapImmutable lost the error")
		}
	})
}

// TestMigrations_PriceListCascade: deleting a draft list cascades to its periods, bands, rules and item snapshot (the child
// triggers find no parent and let the cascade through); the CHECKs of items and rules hold.
func TestMigrations_PriceListCascade(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		marinaID, listID := pricingRows(t, db)
		if _, err := db.NewDelete().Table("price_lists").Where("id = ?", listID).Exec(ctx); err != nil {
			t.Fatalf("delete draft: %v", err)
		}
		for _, tbl := range []string{"rate_periods", "price_list_bands", "price_rules", "price_list_items"} {
			n, err := store.CountRows(ctx, db.DB, tbl)
			if err != nil || n != 0 {
				t.Fatalf("%s rows after cascade = %d, %v", tbl, n, err)
			}
		}
		now := store.UTC(time.Now())
		var rcID string
		if err := db.NewRaw("SELECT id FROM revenue_classes WHERE marina_id = ?", marinaID).Scan(ctx, &rcID); err != nil {
			t.Fatal(err)
		}
		// VAT rate 0 needs a nature, a rate needs none.
		for i, it := range []store.Item{
			{VATRateBP: 0},
			{VATRateBP: 2200, VATNature: "N1"},
			{VATRateBP: 2200, Code: "lower"},
		} {
			it.ID, it.MarinaID, it.RevenueClassID = store.NewID(), marinaID, rcID
			if it.Code == "" {
				it.Code = "BAD-" + string(rune('A'+i))
			}
			it.Name, it.Description, it.ItemType, it.Unit, it.Version, it.CreatedAt, it.UpdatedAt =
				store.JSON(`{}`), store.JSON(`{}`), "service", "lump", 1, now, now
			if _, err := db.NewInsert().Model(&it).Exec(ctx); err == nil {
				t.Fatalf("item %d accepted: %+v", i, it)
			}
		}
	})
}
