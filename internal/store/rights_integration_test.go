//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestRightsStore_Scenario covers the rights queries that the service relies on: versioned updates, the
// conditional settlement and issue of holder instalments, the idempotent revenue upsert, tax id lookups and the
// immutability triggers mapped to ErrImmutable.
func TestRightsStore_Scenario(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		s, _, br, vid := occupancySetup(t, db)
		now := t0
		cust := &store.Customer{
			ID: store.NewID(), MarinaID: s.ID(), Kind: "person", LastName: "Rossi", FirstName: "Mario", FiscalCode: "RSSMRA80A01H501Z",
			BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{},
			SearchText: "rossi", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertCustomer(ctx, db.DB, cust); err != nil {
			t.Fatal(err)
		}
		r := &store.BerthRight{
			ID: store.NewID(), BerthID: br.ID, Kind: "membership", FromOn: calendar.MustParse("2027-01-01"),
			ToOn: calendar.MustParse("2027-12-31"), Title: store.JSON(`{}`), Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertBerthRight(ctx, db.DB, s, r); err != nil {
			t.Fatal(err)
		}
		r.EndReason = "x"
		if err := store.UpdateBerthRight(ctx, db.DB, s, r, 9); !errors.Is(err, store.ErrVersionMismatch) {
			t.Fatalf("stale: %v", err)
		}
		if err := store.UpdateBerthRight(ctx, db.DB, s, r, 1); err != nil || r.Version != 2 {
			t.Fatalf("update: %v %d", err, r.Version)
		}
		missing := &store.BerthRight{ID: store.NewID()}
		if err := store.UpdateBerthRight(ctx, db.DB, s, missing, 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
		// holder instalments: issue once, settle once
		h := store.HolderInstallment{
			ID: store.NewID(), SourceType: "transfer_fee", SourceID: store.NewID(), RightID: r.ID, CustomerID: cust.ID,
			Seq: 1, Kind: "installment", IssueOn: calendar.MustParse("2027-06-01"), DueOn: calendar.MustParse("2027-06-30"), AmountCents: 1000,
			Status: "scheduled", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertHolderInstallments(ctx, db.DB, s, []store.HolderInstallment{h}); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertHolderInstallments(ctx, db.DB, s, []store.HolderInstallment{h}); err == nil {
			t.Fatal("duplicate accepted")
		}
		due, err := store.DueHolderInstallments(ctx, db.DB, s, calendar.MustParse("2027-06-02"), 10)
		if err != nil || len(due) != 1 {
			t.Fatalf("due %d %v", len(due), err)
		}
		for i, want := range []bool{true, false} {
			ok, err := store.IssueHolderInstallment(ctx, db.DB, s, h.ID, now)
			if err != nil || ok != want {
				t.Fatalf("issue %d: %v %v", i, ok, err)
			}
		}
		cur, _ := store.GetHolderInstallment(ctx, db.DB, s, h.ID)
		cur.Status, cur.SettlementSource, cur.SettledAt, cur.SettledOn = "paid", "manual", now, calendar.MustParse("2027-06-02")
		if err := store.SettleHolderInstallmentRow(ctx, db.DB, s, cur, cur.Version); err != nil {
			t.Fatal(err)
		}
		if err := store.SettleHolderInstallmentRow(ctx, db.DB, s, cur, cur.Version); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("second settle: %v", err)
		}
		// the paid ⇒ settlement CHECK
		_, err = db.NewUpdate().Table("holder_installments").Set("settlement_source = NULL").Where("id = ?", h.ID).Exec(ctx)
		if err == nil {
			t.Fatal("paid without settlement accepted")
		}
		// tax id lookup
		cs, err := store.CustomersByTaxIDs(ctx, db.DB, s, []string{"RSSMRA80A01H501Z"})
		if err != nil || cs["RSSMRA80A01H501Z"].ID != cust.ID {
			t.Fatalf("tax ids %v %v", cs, err)
		}
		// revenue upsert is idempotent
		al := newAllocationRow(br, vid, store.AllocKindTransit, calendar.MustParse("2027-07-01"), calendar.MustParse("2027-07-02"), time.Time{})
		if err := store.InsertAllocation(ctx, db.DB, s, al); err != nil {
			t.Fatal(err)
		}
		m := &store.ManagementMandate{
			ID: store.NewID(), RightID: r.ID, FromOn: calendar.MustParse("2027-07-01"), ToOn: calendar.MustParse("2027-07-31"),
			OwnerShareBP: 5000, Channels: store.Strings{"transit"}, Settlement: "credit", Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertMandate(ctx, db.DB, s, m); err != nil {
			t.Fatal(err)
		}
		rev := func() *store.MandateRevenue {
			return &store.MandateRevenue{
				ID: store.NewID(), MandateID: m.ID, AllocationID: al.ID, Nights: 2,
				RevenueCents: 500, OwnerShareCents: 250, ThroughOn: calendar.MustParse("2027-07-02"), CreatedAt: now, UpdatedAt: now,
			}
		}
		for i, want := range []bool{true, false} {
			ch, err := store.UpsertMandateRevenue(ctx, db.DB, s, rev())
			if err != nil || ch != want {
				t.Fatalf("upsert %d: %v %v", i, ch, err)
			}
		}
		x := rev()
		x.RevenueCents = 600
		if ch, err := store.UpsertMandateRevenue(ctx, db.DB, s, x); err != nil || !ch {
			t.Fatalf("changed upsert: %v %v", ch, err)
		}
		if rows, _ := store.RevenuesOfMandates(ctx, db.DB, s, []string{m.ID}); len(rows) != 1 || rows[0].RevenueCents != 600 {
			t.Fatalf("revenues %+v", rows)
		}
		// frozen entries of an active table map to ErrImmutable
		mt := &store.MillesimalTable{ID: store.NewID(), Name: "G", Code: "G", Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := store.InsertMillesimalTable(ctx, db.DB, s, mt); err != nil {
			t.Fatal(err)
		}
		if err := store.InsertMillesimalTable(ctx, db.DB, s, &store.MillesimalTable{ID: store.NewID(), Name: "G", Code: "G", Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now}); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("code taken: %v", err)
		}
		if err := store.ReplaceMillesimalEntries(ctx, db.DB, s, mt.ID, []store.MillesimalEntry{{ID: store.NewID(), BerthID: br.ID, Millis: 1000, CreatedAt: now, UpdatedAt: now}}); err != nil {
			t.Fatal(err)
		}
		mt.Status, mt.ActivatedAt = "active", now.Add(time.Second)
		if err := store.UpdateMillesimalTable(ctx, db.DB, s, mt, 1); err != nil {
			t.Fatal(err)
		}
		if err := store.ReplaceMillesimalEntries(ctx, db.DB, s, mt.ID, nil); !errors.Is(err, store.ErrImmutable) {
			t.Fatalf("active entries: %v", err)
		}
		st, err := store.MillesimalStats(ctx, db.DB, s, []string{mt.ID})
		if err != nil || st[mt.ID].Sum != 1000 || st[mt.ID].Count != 1 {
			t.Fatalf("stats %+v %v", st, err)
		}
	})
}
