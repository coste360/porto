//go:build integration

package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestMigrations_M8Constraints: the M8 guarantees that are database constraints hold on both dialects: the online
// quota counter never exceeds its cap and a conditional increment is the only claim (INV-12), one claim per
// (contract, group, night), the loosened CHECKs (confirmation by channel, terms consents), published booking texts
// are immutable and unique per version, online bookings are unique by contract, reference and idempotency hash,
// channel reservations and events are applied once (INV-89), and portal token hashes are unique.
func TestMigrations_M8Constraints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := m4Rows(t, db)
		now := store.UTC(time.Now())
		night := calendar.New(2027, 7, 1)

		// INV-12: online quota counter.
		g := &store.BerthGroup{
			ID: store.NewID(), MarinaID: f.marinaID, Code: "Q", Name: store.JSON(`{"it":"Q"}`), Kind: "custom",
			OnlineQuotaKind: store.QuotaCount, OnlineQuotaValue: 1, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, g)
		def := &store.BerthGroup{
			ID: store.NewID(), MarinaID: f.marinaID, Code: "D", Name: store.JSON(`{"it":"D"}`), Kind: "custom",
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, def)
		var kind string
		if err := db.NewRaw("SELECT online_quota_kind FROM berth_groups WHERE id = ?", def.ID).Scan(ctx, &kind); err != nil || kind != "none" {
			t.Fatalf("default quota kind = %q, %v", kind, err)
		}
		mustInsert(t, db, &store.OnlineQuotaUsage{MarinaID: f.marinaID, GroupID: g.ID, Night: night, Cap: 1, UpdatedAt: now})
		claim := func() int64 {
			res, err := db.NewRaw("UPDATE online_quota_usage SET used = used + 1 WHERE group_id = ? AND night = ? AND used < cap",
				g.ID, night).Exec(ctx)
			if err != nil {
				t.Fatal(err)
			}
			n, _ := res.RowsAffected()
			return n
		}
		if claim() != 1 || claim() != 0 {
			t.Fatal("conditional claim must succeed once with cap 1")
		}
		if _, err := db.NewRaw("UPDATE online_quota_usage SET used = 2 WHERE group_id = ?", g.ID).Exec(ctx); err == nil {
			t.Fatal("used above cap accepted")
		}
		if _, err := db.NewRaw("UPDATE online_quota_usage SET used = -1 WHERE group_id = ?", g.ID).Exec(ctx); err == nil {
			t.Fatal("negative used accepted")
		}
		c := &store.OnlineQuotaClaim{MarinaID: f.marinaID, ContractID: f.contractID, GroupID: g.ID, Night: night, CreatedAt: now}
		mustInsert(t, db, c)
		wantUnique(t, "second claim", insertRow(t, db, c))

		// Loosened CHECKs.
		if _, err := db.NewRaw("UPDATE contracts SET confirmation_source = 'channel' WHERE id = ?", f.contractID).Exec(ctx); err != nil {
			t.Fatalf("confirmation_source channel: %v", err)
		}
		if _, err := db.NewRaw("UPDATE contracts SET confirmation_source = 'browser' WHERE id = ?", f.contractID).Exec(ctx); err == nil {
			t.Fatal("unknown confirmation_source accepted")
		}
		consent := func(kind string) *store.Consent {
			return &store.Consent{
				ID: store.NewID(), MarinaID: f.marinaID, CustomerID: f.customerID, Kind: kind, PolicyVersion: "1",
				Granted: true, Source: "online", TextSHA256: strings.Repeat("a", 64), RecordedAt: now, CreatedAt: now,
			}
		}
		mustInsert(t, db, consent(store.BookingTextTerms))
		if err := insertRow(t, db, consent("cookies")); err == nil {
			t.Fatal("unknown consent kind accepted")
		}

		// Booking texts: unique version, immutable content.
		txt := &store.BookingText{
			ID: store.NewID(), MarinaID: f.marinaID, Kind: store.BookingTextTerms, Version: 1, Texts: store.JSON(`{"it":"T"}`),
			SHA256: strings.Repeat("b", 64), PublishedAt: now, CreatedAt: now,
		}
		mustInsert(t, db, txt)
		dup := *txt
		dup.ID = store.NewID()
		wantUnique(t, "same text version", insertRow(t, db, &dup))
		_, err := db.NewRaw("UPDATE booking_texts SET texts = ? WHERE id = ?", `{"it":"X"}`, txt.ID).Exec(ctx)
		wantTrigger(t, "edit published text", store.MsgBookingTextImmutable, err)

		// Online bookings.
		ob := func(contractID, ref, idem string) *store.OnlineBooking {
			return &store.OnlineBooking{
				ID: store.NewID(), MarinaID: f.marinaID, ContractID: contractID, Reference: ref, TokenNonce: "n",
				IdempotencyHash: idem, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		first := ob(f.contractID, "ABCDEFGH", "h1")
		mustInsert(t, db, first)
		var ch string
		if err := db.NewRaw("SELECT channel FROM online_bookings WHERE id = ?", first.ID).Scan(ctx, &ch); err != nil || ch != "online" {
			t.Fatalf("default channel = %q, %v", ch, err)
		}
		wantUnique(t, "second booking of a contract", insertRow(t, db, ob(f.contractID, "ZZZZZZZZ", "")))
		other := &store.Contract{
			ID: store.NewID(), MarinaID: f.marinaID, Kind: store.ContractKindMooring, Term: "annual", Number: 2, Year: 2027,
			Status: "active", CustomerID: f.customerID, VesselID: f.vesselID, StartsOn: calendar.New(2027, 1, 1),
			EndsOn: calendar.New(2028, 1, 1), Channel: "online", Policy: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, other)
		wantUnique(t, "same reference", insertRow(t, db, ob(other.ID, "ABCDEFGH", "h2")))
		wantUnique(t, "same idempotency hash", insertRow(t, db, ob(other.ID, "ABCDEFGJ", "h1")))
		if err := insertRow(t, db, ob(other.ID, "SHORT", "")); err == nil {
			t.Fatal("reference of the wrong length accepted")
		}

		// INV-89: channel reservations and events once.
		res := &store.ChannelReservation{
			ID: store.NewID(), MarinaID: f.marinaID, ChannelCode: "fake", ExternalRef: "R1", Status: store.ChannelResReceived,
			PayloadSHA256: "p", CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, res)
		res2 := *res
		res2.ID = store.NewID()
		wantUnique(t, "same external reservation", insertRow(t, db, &res2))
		ev := &store.ChannelEvent{
			ID: store.NewID(), MarinaID: f.marinaID, ChannelCode: "fake", EventID: "E1", Status: store.ChannelEventApplied,
			ReceivedAt: now, CreatedAt: now,
		}
		mustInsert(t, db, ev)
		ev2 := *ev
		ev2.ID = store.NewID()
		wantUnique(t, "same channel event", insertRow(t, db, &ev2))

		// Portal tokens.
		link := &store.CustomerPortalLink{
			ID: store.NewID(), MarinaID: f.marinaID, CustomerID: f.customerID, Kind: store.PortalLinkLogin,
			TokenHash: strings.Repeat("c", 64), ExpiresAt: now.Add(30 * time.Minute), CreatedAt: now,
		}
		mustInsert(t, db, link)
		link2 := *link
		link2.ID = store.NewID()
		wantUnique(t, "same portal token", insertRow(t, db, &link2))
		bookingLink := *link
		bookingLink.ID, bookingLink.Kind, bookingLink.TokenHash = store.NewID(), store.PortalLinkBooking, strings.Repeat("d", 64)
		if err := insertRow(t, db, &bookingLink); err == nil {
			t.Fatal("booking link without contract accepted")
		}
	})
}
