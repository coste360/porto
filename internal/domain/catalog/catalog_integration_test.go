//go:build integration

package catalog

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func newEnv(t testing.TB, db *store.DB) (*fixture.Env, *Service) {
	t.Helper()
	e := fixture.New(t, db)
	return e, NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log})
}

func scopeOf(t testing.TB, e *fixture.Env, marinaID string) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), e.DB.DB, marinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// addBerths inserts a map with one berth per price class (empty = no class) directly through the store.
func addBerths(t testing.TB, e *fixture.Env, classes ...string) {
	t.Helper()
	ctx := context.Background()
	scope := scopeOf(t, e, e.MarinaID)
	now := store.UTC(e.Clock.Now())
	m := &store.Map{
		ID: store.NewID(), Kind: "water", Name: store.MustJSON(I18nText{It: "Acqua"}), Icon: "map", WidthCm: 100000, HeightCm: 100000,
		GridCm: 50, WaterPolygons: store.MustJSON([]any{}), LandPolygons: store.MustJSON([]any{}), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertMap(ctx, e.DB.DB, scope, m); err != nil {
		t.Fatal(err)
	}
	for i, c := range classes {
		b := &store.Berth{
			ID: store.NewID(), MarinaID: e.MarinaID, MapID: m.ID, Code: "B" + string(rune('A'+i)), Side: "none",
			AnchorXCm: int64(i) * 500, AnchorYCm: 0, WidthCm: 400, LengthCm: 1200, MaxLOACm: 1200, MaxBeamCm: 350,
			MooringType: "mooring_line", PriceClass: c, Attributes: store.Strings{}, Active: true, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := e.DB.NewInsert().Model(b).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func classID(t testing.TB, s *Service, e *fixture.Env, code string) string {
	t.Helper()
	list, err := s.ListRevenueClasses(fixture.Ctx(), e.Owner, e.MarinaID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Code == code {
			return c.ID
		}
	}
	t.Fatalf("revenue class %s not found", code)
	return ""
}

func ptr[T any](v T) *T { return &v }

func TestCatalog_RevenueClassDefaults(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		list, err := s.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
		if err != nil {
			t.Fatal(err)
		}
		var codes []string
		for _, c := range list {
			codes = append(codes, c.Code)
		}
		if !slices.Equal(codes, DefaultRevenueClassCodes()) {
			t.Fatalf("defaults = %v", codes)
		}
		// Second read: no duplicates.
		again, err := s.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
		if err != nil || len(again) != 9 {
			t.Fatalf("second list = %d, %v", len(again), err)
		}
		parent := list[0]
		sub, err := s.CreateRevenueClass(ctx, e.Owner, e.MarinaID, RevenueClassInput{Code: "mooring_seasonal", Name: I18nText{It: "Stagionali"}, ParentID: parent.ID})
		if err != nil {
			t.Fatal(err)
		}
		if sub.ParentID == nil || *sub.ParentID != parent.ID {
			t.Fatalf("parent = %v", sub.ParentID)
		}
		// One level only.
		if _, err := s.CreateRevenueClass(ctx, e.Owner, e.MarinaID, RevenueClassInput{Code: "deep", Name: I18nText{It: "x"}, ParentID: sub.ID}); !isValidation(err, "parent_id") {
			t.Fatalf("too deep: %v", err)
		}
		if _, err := s.CreateRevenueClass(ctx, e.Owner, e.MarinaID, RevenueClassInput{Code: "transit", Name: I18nText{It: "dup"}}); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate code: %v", err)
		}
		if _, err := s.CreateRevenueClass(ctx, e.Owner, e.MarinaID, RevenueClassInput{Code: "Bad Code", Name: I18nText{It: "x"}}); !isValidation(err, "code") {
			t.Fatalf("bad code: %v", err)
		}
		// Parent with a child cannot be deleted; archive works.
		if err := s.DeleteRevenueClass(ctx, e.Owner, e.MarinaID, parent.ID, parent.Version); !errors.Is(err, ErrInUse) {
			t.Fatalf("delete parent: %v", err)
		}
		up, err := s.UpdateRevenueClass(ctx, e.Owner, e.MarinaID, sub.ID, sub.Version, RevenueClassPatch{Archived: ptr(true)})
		if err != nil || !up.Archived {
			t.Fatalf("archive: %+v %v", up, err)
		}
		visible, _ := s.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
		withArchived, _ := s.ListRevenueClasses(ctx, e.Owner, e.MarinaID, true)
		if len(visible) != 9 || len(withArchived) != 10 {
			t.Fatalf("visible %d, with archived %d", len(visible), len(withArchived))
		}
		if _, err := s.UpdateRevenueClass(ctx, e.Owner, e.MarinaID, sub.ID, sub.Version, RevenueClassPatch{SortOrder: ptr(3)}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale version: %v", err)
		}
		if err := s.DeleteRevenueClass(ctx, e.Owner, e.MarinaID, sub.ID, up.Version); err != nil {
			t.Fatal(err)
		}
		if n := e.AuditCount(t, "revenue_class.create"); n != 1 {
			t.Fatalf("audit create = %d", n)
		}
	})
}

func isValidation(err error, field string) bool {
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

func TestCatalog_ItemScenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		rc := classID(t, s, e, "transit")
		// Defaults from the settings: 22 %, VAT excluded.
		it, err := s.CreateItem(ctx, e.Owner, e.MarinaID, ItemInput{
			Code: "trn-ntt", Name: I18nText{It: "Passaggio notte"}, ItemType: "berth_fee", Unit: "night", RevenueClassID: rc,
		})
		if err != nil {
			t.Fatal(err)
		}
		if it.Code != "TRN-NTT" || it.VATRateBP != 2200 || it.PriceIncludesVAT || !it.Active || it.VATNature != nil {
			t.Fatalf("defaults: %+v", it)
		}
		// Settings change the defaults of new items.
		set, err := s.GetSettings(ctx, e.Owner, e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpdateSettings(ctx, e.Owner, e.MarinaID, set.Version, SettingsInput{DefaultVATRateBP: 1000, DefaultPriceIncludesVAT: true}); err != nil {
			t.Fatal(err)
		}
		it2, err := s.CreateItem(ctx, e.Owner, e.MarinaID, ItemInput{
			Code: "UTL-H2O", Name: I18nText{It: "Acqua"}, ItemType: "utility", Unit: "m3", RevenueClassID: rc,
		})
		if err != nil || it2.VATRateBP != 1000 || !it2.PriceIncludesVAT {
			t.Fatalf("settings defaults: %+v %v", it2, err)
		}
		// VAT rate 0 needs a nature; a nature with a rate is refused.
		if _, err := s.CreateItem(ctx, e.Owner, e.MarinaID, ItemInput{
			Code: "DEP-CAU", Name: I18nText{It: "Cauzione"}, ItemType: "deposit", Unit: "lump", VATRateBP: ptr(int64(0)), RevenueClassID: rc,
		}); !isValidation(err, "vat_nature") {
			t.Fatalf("rate 0 without nature: %v", err)
		}
		if _, err := s.CreateItem(ctx, e.Owner, e.MarinaID, ItemInput{
			Code: "X1", Name: I18nText{It: "x"}, ItemType: "fee", Unit: "lump", VATNature: "N1", RevenueClassID: rc,
		}); !isValidation(err, "vat_nature") {
			t.Fatalf("nature with rate: %v", err)
		}
		dep, err := s.CreateItem(ctx, e.Owner, e.MarinaID, ItemInput{
			Code: "DEP-CAU", Name: I18nText{It: "Cauzione"}, ItemType: "deposit", Unit: "lump", VATRateBP: ptr(int64(0)), VATNature: "N1", RevenueClassID: rc,
		})
		if err != nil || dep.VATNature == nil || *dep.VATNature != "N1" {
			t.Fatalf("deposit: %+v %v", dep, err)
		}
		// The DB CHECK is the backstop.
		_, err = db.NewUpdate().Table("items").Set("vat_nature = NULL").Where("id = ?", dep.ID).Exec(context.Background())
		if err == nil {
			t.Fatal("CHECK (vat_rate_bp = 0) = (vat_nature IS NOT NULL) did not hold")
		}
		if _, err := s.CreateItem(ctx, e.Owner, e.MarinaID, ItemInput{
			Code: "TRN-NTT", Name: I18nText{It: "dup"}, ItemType: "berth_fee", Unit: "night", RevenueClassID: rc,
		}); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate code: %v", err)
		}
		if _, err := s.CreateItem(ctx, e.Owner, e.MarinaID, ItemInput{
			Code: "BAD CODE", Name: I18nText{It: "x"}, ItemType: "nope", Unit: "night", RevenueClassID: rc,
		}); !isValidation(err, "code") || !isValidation(err, "item_type") {
			t.Fatalf("bad fields: %v", err)
		}
		// Patch: rate non-zero clears the nature.
		up, err := s.UpdateItem(ctx, e.Owner, e.MarinaID, dep.ID, dep.Version, ItemPatch{VATRateBP: ptr(int64(2200)), Name: &I18nText{It: "Deposito"}})
		if err != nil || up.VATNature != nil || up.VATRateBP != 2200 || up.Version != 2 {
			t.Fatalf("patch: %+v %v", up, err)
		}
		if _, err := s.UpdateItem(ctx, e.Owner, e.MarinaID, dep.ID, 1, ItemPatch{Active: ptr(false)}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		// Listing: filters and paging by sort order and code.
		page, err := s.ListItems(ctx, e.Owner, e.MarinaID, ItemFilter{}, "", 2)
		if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
			t.Fatalf("page 1: %+v %v", page, err)
		}
		rest, err := s.ListItems(ctx, e.Owner, e.MarinaID, ItemFilter{}, page.NextCursor, 2)
		if err != nil || len(rest.Items) != 1 || rest.NextCursor != "" {
			t.Fatalf("page 2: %+v %v", rest, err)
		}
		if got := []string{page.Items[0].Code, page.Items[1].Code, rest.Items[0].Code}; !slices.Equal(got, []string{"DEP-CAU", "TRN-NTT", "UTL-H2O"}) {
			t.Fatalf("order = %v", got)
		}
		q, _ := s.ListItems(ctx, e.Owner, e.MarinaID, ItemFilter{Q: "acqua"}, "", 0)
		typ, _ := s.ListItems(ctx, e.Owner, e.MarinaID, ItemFilter{Type: "berth_fee"}, "", 0)
		if len(q.Items) != 1 || len(typ.Items) != 1 {
			t.Fatalf("filters: q=%d type=%d", len(q.Items), len(typ.Items))
		}
		// in_use on delete: an item referenced by a price list rule.
		listID := insertListUsing(t, e, it.ID)
		got, err := s.GetItem(ctx, e.Owner, e.MarinaID, it.ID)
		if err != nil || got.Usage == nil || got.Usage.PriceLists != 1 {
			t.Fatalf("usage: %+v %v", got, err)
		}
		if err := s.DeleteItem(ctx, e.Owner, e.MarinaID, it.ID, it.Version); !errors.Is(err, ErrInUse) {
			t.Fatalf("delete used: %v", err)
		}
		var iu *InUseError
		if errors.As(s.DeleteItem(ctx, e.Owner, e.MarinaID, it.ID, it.Version), &iu); iu == nil || iu.References != 1 {
			t.Fatalf("references = %+v", iu)
		}
		// Deactivation works, deletion of an unused item works.
		if _, err := s.UpdateItem(ctx, e.Owner, e.MarinaID, it.ID, it.Version, ItemPatch{Active: ptr(false)}); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteItem(ctx, e.Owner, e.MarinaID, it2.ID, it2.Version); err != nil {
			t.Fatal(err)
		}
		_ = listID
		if n := len(e.Events(t, event.ConfigPricingChanged)); n < 5 {
			t.Fatalf("events = %d", n)
		}
		// Office reads, cannot write; yard cannot read.
		office := e.AddUser(t, "office@porto.test", auth.RoleOffice, e.OrgID, e.MarinaID)
		yard := e.AddUser(t, "yard@porto.test", auth.RoleYard, e.OrgID, e.MarinaID)
		if _, err := s.GetItem(ctx, office, e.MarinaID, it.ID); err != nil {
			t.Fatalf("office read: %v", err)
		}
		if _, err := s.CreateItem(ctx, office, e.MarinaID, ItemInput{Code: "Z"}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("office write: %v", err)
		}
		if _, err := s.GetItem(ctx, yard, e.MarinaID, it.ID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard read: %v", err)
		}
	})
}

// insertListUsing inserts a draft list with one base rule on item through the store.
func insertListUsing(t testing.TB, e *fixture.Env, itemID string) string {
	t.Helper()
	ctx := context.Background()
	scope := scopeOf(t, e, e.MarinaID)
	now := store.UTC(e.Clock.Now())
	l := &store.PriceList{
		ID: store.NewID(), Code: "L-" + itemID[len(itemID)-6:], Name: store.MustJSON(I18nText{It: "L"}), Status: store.PriceListDraft,
		ContractTypes: store.Strings{"transit"}, Settings: store.MustJSON(map[string]any{}), NextRuleSeq: 2, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	l.ValidFrom, l.ValidTo = mustDate("2027-01-01"), mustDate("2027-12-31")
	if err := store.InsertPriceList(ctx, e.DB.DB, scope, l); err != nil {
		t.Fatal(err)
	}
	r := &store.PriceRule{
		ID: store.NewID(), MarinaID: e.MarinaID, PriceListID: l.ID, Seq: 1, Stage: "base", Name: "r", ItemID: itemID, Active: true,
		Body: store.MustJSON(map[string]any{"basis": "flat", "unit": "night", "amount_cents": 100}), CreatedAt: now, UpdatedAt: now,
	}
	if _, err := e.DB.NewInsert().Model(r).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return l.ID
}

func TestCatalog_CategorySync(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		addBerths(t, e, "L10", "L12", "L12", "BOA", "", "custom-x")
		before, err := s.ListCategories(ctx, e.Owner, e.MarinaID, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(before.Items) != 0 || len(before.UnknownInUse) != 4 {
			t.Fatalf("before: %+v", before)
		}
		created, err := s.SyncCategories(ctx, e.Owner, e.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		var codes []string
		for _, c := range created {
			codes = append(codes, c.Code)
		}
		// The default rule classes L8..L25 first, then the codes in use not in the rule.
		want := []string{"L8", "L10", "L12", "L14", "L16", "L18", "L20", "L25", "BOA", "custom-x"}
		if !slices.Equal(codes, want) {
			t.Fatalf("created = %v", codes)
		}
		after, err := s.ListCategories(ctx, e.Owner, e.MarinaID, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(after.UnknownInUse) != 0 {
			t.Fatalf("unknown after sync: %+v", after.UnknownInUse)
		}
		var l12 BerthCategory
		for _, c := range after.Items {
			if c.Code == "L12" {
				l12 = c
			}
		}
		if l12.Usage.Berths != 2 || !slices.Equal(l12.AppliesTo, []string{"berth", "dynamic_area"}) {
			t.Fatalf("L12: %+v", l12)
		}
		// Second sync creates nothing.
		again, err := s.SyncCategories(ctx, e.Owner, e.MarinaID)
		if err != nil || len(again) != 0 {
			t.Fatalf("second sync: %d %v", len(again), err)
		}
		// A category in use cannot be deleted; an unused one can.
		if err := s.DeleteCategory(ctx, e.Owner, e.MarinaID, l12.ID, l12.Version); !errors.Is(err, ErrInUse) {
			t.Fatalf("delete used: %v", err)
		}
		var l8 BerthCategory
		for _, c := range after.Items {
			if c.Code == "L8" {
				l8 = c
			}
		}
		up, err := s.UpdateCategory(ctx, e.Owner, e.MarinaID, l8.ID, l8.Version, CategoryPatch{Name: &I18nText{It: "Fino a 8 m"}, AppliesTo: &[]string{"dynamic_area", "berth"}})
		if err != nil || up.Name.It != "Fino a 8 m" || !slices.Equal(up.AppliesTo, []string{"berth", "dynamic_area"}) {
			t.Fatalf("update: %+v %v", up, err)
		}
		if _, err := s.UpdateCategory(ctx, e.Owner, e.MarinaID, l8.ID, up.Version, CategoryPatch{AppliesTo: &[]string{"boat"}}); !isValidation(err, "applies_to") {
			t.Fatalf("bad applies_to: %v", err)
		}
		if err := s.DeleteCategory(ctx, e.Owner, e.MarinaID, l8.ID, up.Version); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateCategory(ctx, e.Owner, e.MarinaID, CategoryInput{Code: "L10", Name: I18nText{It: "dup"}, AppliesTo: []string{"berth"}}); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate: %v", err)
		}
		if e.AuditCount(t, "berth_category.sync") != 2 {
			t.Fatal("sync audit missing")
		}
	})
}

func TestCatalog_CrossMarinaRefs(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e, s := newEnv(t, db)
		ctx := fixture.Ctx()
		_, otherMarina, otherOwner := e.OtherOrganization(t)
		otherClass := ""
		list, err := s.ListRevenueClasses(ctx, otherOwner, otherMarina, false)
		if err != nil {
			t.Fatal(err)
		}
		otherClass = list[0].ID
		otherItem, err := s.CreateItem(ctx, otherOwner, otherMarina, ItemInput{
			Code: "A", Name: I18nText{It: "a"}, ItemType: "fee", Unit: "lump", RevenueClassID: otherClass,
		})
		if err != nil {
			t.Fatal(err)
		}
		// A revenue class of another marina is 404 when creating an item here.
		if _, err := s.CreateItem(ctx, e.Owner, e.MarinaID, ItemInput{
			Code: "A", Name: I18nText{It: "a"}, ItemType: "fee", Unit: "lump", RevenueClassID: otherClass,
		}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("foreign revenue class: %v", err)
		}
		if _, err := s.GetItem(ctx, e.Owner, e.MarinaID, otherItem.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("foreign item: %v", err)
		}
		if _, err := s.UpdateItem(ctx, e.Owner, e.MarinaID, otherItem.ID, 1, ItemPatch{Active: ptr(false)}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("foreign item update: %v", err)
		}
		if err := s.DeleteRevenueClass(ctx, e.Owner, e.MarinaID, otherClass, 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("foreign class delete: %v", err)
		}
		if _, err := s.CreateRevenueClass(ctx, e.Owner, e.MarinaID, RevenueClassInput{Code: "sub", Name: I18nText{It: "s"}, ParentID: otherClass}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("foreign parent: %v", err)
		}
		// Another marina's id in the path is 404 for this owner.
		if _, err := s.ListItems(ctx, e.Owner, otherMarina, ItemFilter{}, "", 0); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign marina: %v", err)
		}
	})
}

func mustDate(s string) calendar.Date { return calendar.MustParse(s) }
