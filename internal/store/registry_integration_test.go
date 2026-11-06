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

func registryCustomer(t *testing.T, db *store.DB, marinaID, last, search string, tags ...string) *store.Customer {
	t.Helper()
	c := &store.Customer{
		ID: store.NewID(), MarinaID: marinaID, Kind: "person", LastName: last, BillingAddress: store.JSON(`{}`),
		Locale: "it", Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: tags, SearchText: search,
		Version: 1, CreatedAt: t0, UpdatedAt: t0,
	}
	if err := store.InsertCustomer(context.Background(), db.DB, c); err != nil {
		t.Fatal(err)
	}
	return c
}

func scopeOf(t *testing.T, db *store.DB, marinaID string) store.MarinaScope {
	t.Helper()
	s, err := store.ResolveMarinaScope(context.Background(), db.DB, marinaID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Search, tag and archive filters behave the same on both dialects, and LIKE wildcards in the query are
// literal.
func TestRegistryStore_CustomerFilters(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, m := seedOrgMarina(t, db, "reg")
		_, other := seedOrgMarina(t, db, "reg-other")
		s := scopeOf(t, db, m)
		a := registryCustomer(t, db, m, "Rossi", "mario rossi 100% vero", "vip", "socio")
		b := registryCustomer(t, db, m, "Bianchi", "anna bianchi a_b", "socio")
		registryCustomer(t, db, other, "Rossi", "mario rossi", "vip")
		v := &store.Vessel{
			ID: store.NewID(), MarinaID: m, Name: "Stella", HullType: "other", LOACm: 1000, BeamCm: 300,
			DimensionsSource: "declared", Engines: store.JSON(`[]`), SearchText: "stella rm1", Version: 1, CreatedAt: t0, UpdatedAt: t0,
		}
		if err := store.InsertVessel(ctx, db.DB, v); err != nil {
			t.Fatal(err)
		}
		if err := store.ReplaceVesselParties(ctx, db.DB, s, v.ID, []store.VesselParty{{
			ID: store.NewID(), MarinaID: m, VesselID: v.ID, CustomerID: b.ID, Role: "owner", ShareMillis: 1000, CreatedAt: t0, UpdatedAt: t0,
		}}); err != nil {
			t.Fatal(err)
		}
		ids := func(f store.CustomerFilter) []string {
			page, err := store.ListCustomers(ctx, db.DB, s, f, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, c := range page.Items {
				out = append(out, c.ID)
			}
			return out
		}
		for name, c := range map[string]struct {
			f    store.CustomerFilter
			want []string
		}{
			"all":            {store.CustomerFilter{}, []string{b.ID, a.ID}},
			"tag vip":        {store.CustomerFilter{Tag: "vip"}, []string{a.ID}},
			"tag socio":      {store.CustomerFilter{Tag: "socio"}, []string{b.ID, a.ID}},
			"percent":        {store.CustomerFilter{Search: "100%"}, []string{a.ID}},
			"percent wild":   {store.CustomerFilter{Search: "0% v"}, []string{a.ID}},
			"underscore":     {store.CustomerFilter{Search: "a_b"}, []string{b.ID}},
			"underscore lit": {store.CustomerFilter{Search: "a_"}, []string{b.ID}},
			"by vessel":      {store.CustomerFilter{Search: "rm1"}, []string{b.ID}},
			"archived":       {store.CustomerFilter{Archived: true}, nil},
		} {
			got := ids(c.f)
			if len(got) != len(c.want) {
				t.Errorf("%s: %v, want %v", name, got, c.want)
				continue
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("%s: %v, want %v", name, got, c.want)
				}
			}
		}
		counts, err := store.VesselCountsOfCustomers(ctx, db.DB, s, []string{a.ID, b.ID})
		if err != nil || counts[b.ID] != 1 || counts[a.ID] != 0 {
			t.Fatalf("counts %v %v", counts, err)
		}
		// Owner shares are required by a CHECK for owners only.
		bad := []store.VesselParty{{ID: store.NewID(), MarinaID: m, VesselID: v.ID, CustomerID: a.ID, Role: "skipper", ShareMillis: 10, CreatedAt: t0, UpdatedAt: t0}}
		if err := store.ReplaceVesselParties(ctx, db.DB, s, v.ID, bad); err == nil {
			t.Fatal("share on a skipper accepted by the database")
		}
	})
}

// The (due_on, id) cursor of deadlines pages through equal due dates without losing rows.
func TestRegistryStore_DeadlineCursor(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, m := seedOrgMarina(t, db, "deadlines")
		s := scopeOf(t, db, m)
		c := registryCustomer(t, db, m, "Rossi", "rossi")
		days := []string{"2027-01-02", "2027-01-01", "2027-01-02", "2027-01-03", "2027-01-02"}
		for i, d := range days {
			row := &store.Deadline{
				ID: store.NewID(), MarinaID: m, SourceType: "document", SourceID: store.NewID(), Kind: "insurance",
				CustomerID: c.ID, DueOn: calendar.MustParse(d), Status: "open", NotifiedAt: store.JSON(`[]`), Version: 1,
				CreatedAt: t0, UpdatedAt: t0,
			}
			if i == 4 {
				row.Status = "done"
			}
			if err := store.InsertDeadline(ctx, db.DB, row); err != nil {
				t.Fatal(err)
			}
		}
		var seen []string
		cursor := ""
		for range 10 {
			page, err := store.ListDeadlines(ctx, db.DB, s, store.DeadlineFilter{}, cursor, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range page.Items {
				seen = append(seen, d.DueOn.String())
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		want := []string{"2027-01-01", "2027-01-02", "2027-01-02", "2027-01-02", "2027-01-03"}
		if len(seen) != len(want) {
			t.Fatalf("seen %v", seen)
		}
		for i := range want {
			if seen[i] != want[i] {
				t.Fatalf("seen %v, want %v", seen, want)
			}
		}
		open, err := store.ListDeadlines(ctx, db.DB, s, store.DeadlineFilter{
			Statuses: []string{"open"},
			DueAfter: calendar.MustParse("2027-01-02"), DueBefore: calendar.MustParse("2027-01-02"),
		}, "", 0)
		if err != nil || len(open.Items) != 2 {
			t.Fatalf("filtered %d %v", len(open.Items), err)
		}
		// One deadline per (source, kind).
		dup := &store.Deadline{
			ID: store.NewID(), MarinaID: m, SourceType: "document", SourceID: open.Items[0].SourceID, Kind: "insurance",
			DueOn: calendar.MustParse("2027-02-01"), Status: "open", NotifiedAt: store.JSON(`[]`), Version: 1, CreatedAt: t0, UpdatedAt: t0,
		}
		if err := store.InsertDeadline(ctx, db.DB, dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate source: %v", err)
		}
	})
}

// TestRegistryStore_MergeTargetStaysInMarina: the composite FK (merged_into_id, marina_id) refuses a merge into a
// customer of another marina (INV-73).
func TestRegistryStore_MergeTargetStaysInMarina(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		_, a := seedOrgMarina(t, db, "alpha")
		_, b := seedOrgMarina(t, db, "beta")
		sa := scopeOf(t, db, a)
		src, own := registryCustomer(t, db, a, "Rossi", "rossi"), registryCustomer(t, db, a, "Bianchi", "bianchi")
		other := registryCustomer(t, db, b, "Verdi", "verdi")
		if err := store.MarkCustomerMerged(ctx, db.DB, sa, src.ID, other.ID, t0); err == nil {
			t.Fatal("merge into a customer of another marina was committed")
		}
		if err := store.MarkCustomerMerged(ctx, db.DB, sa, src.ID, own.ID, t0); err != nil {
			t.Fatalf("same-marina merge: %v", err)
		}
	})
}
