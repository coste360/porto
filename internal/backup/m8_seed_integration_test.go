//go:build integration

package backup

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
)

// seedM8Rows adds at least one row of every backed-up M8 table (m8-plan §13): online booking settings, a published
// booking text, an online booking of a transit with its inquiry, an online quota counter with its claim, a channel
// connection with encrypted columns, a channel reservation and a document uploaded through the booking. Portal links
// and sessions and channel events are not backed up (store.BackupExcludedTables).
func seedM8Rows(t *testing.T, db *store.DB, marinaID, userID string) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(time.Now())
	var contract store.Contract
	if err := db.NewSelect().Model(&contract).Where("marina_id = ?", marinaID).Where("kind = ?", "transit").
		OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("transit: %v", err)
	}
	var inquiry store.Inquiry
	if err := db.NewSelect().Model(&inquiry).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("inquiry: %v", err)
	}
	var group store.BerthGroup
	if err := db.NewSelect().Model(&group).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("berth group: %v", err)
	}
	night := calendar.New(2027, 7, 1)
	rows := []any{
		&store.OnlineBookingSettings{
			MarinaID: marinaID, MinNights: 1, MaxNights: 30, MinAdvanceHours: 2, MaxAdvanceDays: 365, ArrivalFrom: "12:00",
			ArrivalTo: "20:00", ClosedWeekdays: store.JSON(`[1]`), PaymentMode: "deposit", DepositBP: 3000,
			CancellationTiers: store.JSON(`[{"min_days_before":7,"refund_bp":10000}]`), RefundAs: "refund",
			Approval:       store.JSON(`{"always":false,"group_ids":[],"hold_hours":48}`),
			RequiredFields: store.JSON(`{"documents":["insurance_policy"],"people_on_board":true,"phone":true}`),
			RevealBerth:    "after_confirmation", LatePaymentRecovery: true, MaxLiveHolds: 50,
			ArrivalInfo: store.JSON(`{"vhf_channel":"9"}`), ThemePreset: "default", ShowFeet: true, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		},
		&store.BookingText{
			ID: store.NewID(), MarinaID: marinaID, Kind: store.BookingTextTerms, Version: 1, Texts: store.JSON(`{"it":"Condizioni"}`),
			SHA256: strings.Repeat("a", 64), PublishedAt: now, PublishedByUserID: userID, CreatedAt: now,
		},
		&store.OnlineBooking{
			ID: store.NewID(), MarinaID: marinaID, ContractID: contract.ID, Reference: "M8BKP0RT", TokenNonce: "nonce",
			IdempotencyHash: "idem", BodySHA256: "body", ContactEnc: "enc", PeopleOnBoard: 2, Locale: "it",
			InquiryID: inquiry.ID, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		&store.OnlineQuotaUsage{MarinaID: marinaID, GroupID: group.ID, Night: night, Cap: 2, Used: 1, UpdatedAt: now},
		&store.OnlineQuotaClaim{MarinaID: marinaID, ContractID: contract.ID, GroupID: group.ID, Night: night, CreatedAt: now},
		&store.ChannelConnection{
			ID: store.NewID(), MarinaID: marinaID, ChannelCode: "fake", Enabled: true, CredentialsEnc: "c", WebhookSecretEnc: "w",
			CategoryMap: store.JSON(`{"S":"standard"}`), PushEnabled: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		&store.ChannelReservation{
			ID: store.NewID(), MarinaID: marinaID, ChannelCode: "fake", ExternalRef: "R-1", ContractID: contract.ID,
			Status: store.ChannelResBooked, PayloadSHA256: "p", CreatedAt: now, UpdatedAt: now,
		},
	}
	var doc store.Document
	if err := db.NewSelect().Model(&doc).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("document: %v", err)
	}
	ob := rows[2].(*store.OnlineBooking)
	rows = append(rows, &store.OnlineBookingDocument{
		ID: store.NewID(), MarinaID: marinaID, OnlineBookingID: ob.ID, DocumentID: doc.ID, CreatedAt: now,
	})
	// The demo seed enables online booking (settings and published texts): those rows are already there.
	if n, err := db.NewSelect().Model((*store.OnlineBookingSettings)(nil)).Where("marina_id = ?", marinaID).Count(ctx); err != nil {
		t.Fatal(err)
	} else if n > 0 {
		rows = rows[2:]
	}
	for _, row := range rows {
		if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", row, err)
		}
	}
}
