//go:build integration

package pricelist

import (
	"encoding/json"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestPriceList_PublishedCatalogStable: items and revenue classes stay editable, also when a published list uses
// them. A published list prices with the item snapshot taken at activation, so its quotes do not change and
// content_sha256 = CatalogHash(LoadCatalog(list)) forever, while a new draft picks up the live values (INV-26).
func TestPriceList_PublishedCatalogStable(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		act := e.activate(t, e.createList(t, e.transitDoc("T-2027", 2027)), calendar.Date{}).Document
		quote := func() string {
			t.Helper()
			b, _, err := e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
				PriceListID: act.ID, ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"), Departure: d("2027-07-13"),
				Vessel: &SimVessel{LOA: 1100, Beam: 380},
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			return string(out)
		}
		hashNow := func() string {
			t.Helper()
			cat, err := LoadCatalog(ctx, db.DB, e.scope(t), act.ID)
			if err != nil {
				t.Fatal(err)
			}
			return catalogHash(cat)
		}
		before := quote()
		if hashNow() != *act.ContentSHA256 {
			t.Fatal("hash differs right after activation")
		}
		patch := func(state string, p catalog.ItemPatch) {
			t.Helper()
			it, err := e.cat.GetItem(ctx, e.Owner, e.MarinaID, e.trn)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.cat.UpdateItem(ctx, e.Owner, e.MarinaID, e.trn, it.Version, p); err != nil {
				t.Fatalf("%s: item edit refused: %v", state, err)
			}
		}
		stable := func(state string) {
			t.Helper()
			if got := quote(); got != before {
				t.Errorf("%s: quote changed:\n%s\n%s", state, before, got)
			}
			if h := hashNow(); h != *act.ContentSHA256 {
				t.Errorf("%s: content_sha256 %s != CatalogHash(LoadCatalog) %s", state, *act.ContentSHA256, h)
			}
		}

		// Active list: VAT, VAT inclusion, external code and the revenue class code change.
		patch("active", catalog.ItemPatch{VATRateBP: ptr(int64(1000)), PriceIncludesVAT: ptr(false), ExternalCode: ptr("EXT-9")})
		it, err := e.cat.GetItem(ctx, e.Owner, e.MarinaID, e.trn)
		if err != nil {
			t.Fatal(err)
		}
		rc := revenueClass(t, e, it.RevenueClassID)
		if _, err := e.cat.UpdateRevenueClass(ctx, e.Owner, e.MarinaID, rc.ID, rc.Version, catalog.RevenueClassPatch{Code: ptr("transit_x")}); err != nil {
			t.Fatalf("revenue class code change refused: %v", err)
		}
		stable("active")

		// Archived list: VAT nature, code and active change too.
		cur, err := e.pl.Get(ctx, e.Owner, e.MarinaID, act.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.pl.Archive(ctx, e.Owner, e.MarinaID, act.ID, cur.Version); err != nil {
			t.Fatal(err)
		}
		patch("archived", catalog.ItemPatch{VATRateBP: ptr(int64(0)), VATNature: ptr("N1"), Code: ptr("TRN-NEW"), Active: ptr(false)})
		stable("archived")

		// A new draft prices with the live item.
		draft := e.createList(t, e.transitDoc("T-2028", 2028))
		cat, err := LoadCatalog(ctx, db.DB, e.scope(t), draft.ID)
		if err != nil {
			t.Fatal(err)
		}
		var live *pricing.Item
		for i := range cat.Items {
			if cat.Items[i].ID == e.trn {
				live = &cat.Items[i]
			}
		}
		want := pricing.Item{
			ID: e.trn, Code: "TRN-NEW", Type: pricing.ItemBerthFee, Unit: "night", VATRateBP: 0, VATNature: "N1",
			PriceIncludesVAT: false, RevenueClass: "transit_x", ExternalCode: "EXT-9", Active: false,
		}
		if live == nil || *live != want {
			t.Fatalf("draft item %+v, want %+v", live, want)
		}
		old, err := LoadCatalog(ctx, db.DB, e.scope(t), act.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range old.Items {
			if x.ID == e.trn && (x.Code != "TRN-NTT" || x.VATRateBP != 2200 || !x.PriceIncludesVAT || !x.Active || x.RevenueClass != "transit") {
				t.Fatalf("snapshot item changed: %+v", x)
			}
		}
	})
}

func revenueClass(t *testing.T, e *env, id string) catalog.RevenueClass {
	t.Helper()
	classes, err := e.cat.ListRevenueClasses(fixture.Ctx(), e.Owner, e.MarinaID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range classes {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("revenue class %s not found", id)
	return catalog.RevenueClass{}
}
