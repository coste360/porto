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

type m4Fixture struct {
	occupancyFixture
	customerID, contractID string
}

func m4Rows(t *testing.T, db *store.DB) m4Fixture {
	t.Helper()
	ctx := context.Background()
	f := m4Fixture{occupancyFixture: occupancyRows(t, db)}
	now := store.UTC(time.Now())
	c := &store.Customer{
		ID: store.NewID(), MarinaID: f.marinaID, Kind: "person", LastName: "Rossi", BillingAddress: store.JSON(`{}`), Locale: "it",
		Emails: store.JSON(`[]`), Phones: store.JSON(`[]`), Tags: store.Strings{}, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	k := &store.Contract{
		ID: store.NewID(), MarinaID: f.marinaID, Kind: store.ContractKindMooring, Term: "annual", Number: 1, Year: 2027,
		Status: "active", CustomerID: c.ID, VesselID: f.vesselID, StartsOn: calendar.New(2027, 1, 1),
		EndsOn: calendar.New(2028, 1, 1), Channel: "desk", Policy: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	for _, r := range []any{c, k} {
		if _, err := db.NewInsert().Model(r).Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", r, err)
		}
	}
	f.customerID, f.contractID = c.ID, k.ID
	return f
}

func insertRow(t *testing.T, db *store.DB, row any) error {
	t.Helper()
	_, err := db.NewInsert().Model(row).Exec(context.Background())
	return err
}

func mustInsert(t *testing.T, db *store.DB, rows ...any) {
	t.Helper()
	for _, r := range rows {
		if err := insertRow(t, db, r); err != nil {
			t.Fatalf("insert %T: %v", r, err)
		}
	}
}

func wantUnique(t *testing.T, what string, err error) {
	t.Helper()
	if !store.IsUniqueViolation(err) {
		t.Fatalf("%s: want unique violation, got %v", what, err)
	}
}

func wantTrigger(t *testing.T, what, message string, err error) {
	t.Helper()
	if !store.IsTriggerViolation(err, message) {
		t.Fatalf("%s: want %s, got %v", what, message, err)
	}
}

// TestMigrations_M4Constraints: the M4 guarantees that are database constraints hold on both dialects: one open
// renewal proposal per contract (INV-33), one open signature request per subject, one issued fee run per table and
// year, one issued balance run per budget, paid instalments carry a settlement (INV-32), and the immutability
// triggers of millesimal entries (INV-41), issued allocations and fee lines (INV-44), published template versions
// and signed documents.
func TestMigrations_M4Constraints(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		ctx := context.Background()
		f := m4Rows(t, db)
		now := store.UTC(time.Now())
		d := calendar.MustParse

		// INV-33: open proposals.
		proposal := func(status, reason string) *store.RenewalProposal {
			return &store.RenewalProposal{
				ID: store.NewID(), MarinaID: f.marinaID, SourceContractID: f.contractID, Status: status, ExclusionReason: reason,
				DeadlineOn: d("2027-11-15"), Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		mustInsert(t, db, proposal("declined", ""), proposal("expired", ""), proposal("sent", ""))
		wantUnique(t, "second open proposal", insertRow(t, db, proposal("previewed", "")))
		if err := insertRow(t, db, proposal("excluded", "")); err == nil {
			t.Fatal("excluded without reason accepted")
		}

		// INV-32: paid needs a settlement.
		inst := &store.Installment{
			ID: store.NewID(), MarinaID: f.marinaID, ContractID: f.contractID, PlanVersion: 1, Seq: 1, Kind: "deposit",
			IssueOn: d("2027-01-01"), DueOn: d("2027-01-08"), AmountCents: 100, Status: store.InstallmentPaid,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, inst); err == nil {
			t.Fatal("paid instalment without settlement accepted")
		}
		inst.SettlementSource, inst.SettledAt = store.SettlementManual, now
		mustInsert(t, db, inst)
		neg := *inst
		neg.ID, neg.Seq, neg.AmountCents, neg.Status, neg.SettlementSource, neg.SettledAt = store.NewID(), 2, -1, store.InstallmentScheduled, "", time.Time{}
		if err := insertRow(t, db, &neg); err == nil {
			t.Fatal("negative instalment accepted")
		}

		// Templates and documents.
		file := &store.StoredFile{
			ID: store.NewID(), MarinaID: f.marinaID, Purpose: "document", ContentType: "application/pdf", SizeBytes: 10,
			SHA256: "abc", StorageKey: f.marinaID + "/x.bin", CreatedAt: now, UpdatedAt: now,
		}
		tmpl := &store.Template{ID: store.NewID(), MarinaID: f.marinaID, Kind: "contract_annual", Code: "a", Name: "A", Version: 1, CreatedAt: now, UpdatedAt: now}
		tv := &store.TemplateVersion{
			ID: store.NewID(), MarinaID: f.marinaID, TemplateID: tmpl.ID, Version: 1, Locale: "it", BodyMD: "x",
			Page: store.JSON(`{}`), Status: "published", SHA256: "s", PublishedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, file, tmpl, tv)
		tv2 := *tv
		tv2.ID, tv2.Version = store.NewID(), 2
		wantUnique(t, "second published version", insertRow(t, db, &tv2))
		_, err := db.NewUpdate().Table("template_versions").Set("body_md = 'changed'").Where("id = ?", tv.ID).Exec(ctx)
		wantTrigger(t, "published body edit", store.TemplateVersionImmutableMessage, err)
		_, err = db.NewDelete().Table("template_versions").Where("id = ?", tv.ID).Exec(ctx)
		wantTrigger(t, "published delete", store.TemplateVersionImmutableMessage, err)
		if _, err := db.NewUpdate().Table("template_versions").Set("status = 'superseded'").Set("updated_at = ?", now).
			Where("id = ?", tv.ID).Exec(ctx); err != nil {
			t.Fatalf("published → superseded: %v", err)
		}
		_, err = db.NewUpdate().Table("template_versions").Set("status = 'published'").Where("id = ?", tv.ID).Exec(ctx)
		wantTrigger(t, "superseded → published", store.TemplateVersionImmutableMessage, err)

		doc := &store.GeneratedDocument{
			ID: store.NewID(), MarinaID: f.marinaID, Kind: "contract_annual", TemplateVersionID: tv.ID, Locale: "it",
			SubjectType: "contract", SubjectID: f.contractID, Status: "ready", FileID: file.ID, SHA256: "abc", Pages: 1,
			PDFA: true, CreatedAt: now, UpdatedAt: now,
		}
		signed := &store.GeneratedDocument{
			ID: store.NewID(), MarinaID: f.marinaID, Kind: "contract_annual", BuiltinRef: "it/evidence", Locale: "it",
			SubjectType: "contract", SubjectID: f.contractID, Status: "ready", FileID: file.ID, SHA256: "def", Pages: 2,
			PDFA: true, Signed: true, SignedFromID: doc.ID, CreatedAt: now, UpdatedAt: now,
		}
		mustInsert(t, db, doc, signed)
		_, err = db.NewUpdate().Table("generated_documents").Set("sha256 = 'other'").Where("id = ?", doc.ID).Exec(ctx)
		wantTrigger(t, "hash change", store.DocumentImmutableMessage, err)
		if _, err := db.NewUpdate().Table("generated_documents").Set("status = 'superseded'").Where("id = ?", doc.ID).Exec(ctx); err != nil {
			t.Fatalf("unsigned ready → superseded: %v", err)
		}
		_, err = db.NewUpdate().Table("generated_documents").Set("status = 'superseded'").Where("id = ?", signed.ID).Exec(ctx)
		wantTrigger(t, "signed update", store.DocumentImmutableMessage, err)
		_, err = db.NewDelete().Table("generated_documents").Where("id = ?", signed.ID).Exec(ctx)
		wantTrigger(t, "signed delete", store.DocumentImmutableMessage, err)

		// One open signature request per subject.
		request := func(status string) *store.SignatureRequest {
			r := &store.SignatureRequest{
				ID: store.NewID(), MarinaID: f.marinaID, SubjectType: "contract", SubjectID: f.contractID, DocumentID: doc.ID,
				Driver: "otp", OrderMode: "parallel", Status: status, ExpiresAt: now.Add(time.Hour), Version: 1,
				CreatedAt: now, UpdatedAt: now,
			}
			if status == "signed" {
				r.SignedDocumentID = signed.ID
			}
			return r
		}
		mustInsert(t, db, request("cancelled"), request("signed"), request("sent"))
		wantUnique(t, "second open request", insertRow(t, db, request("pending_document")))

		// Rights: fee runs, millesimal tables, common charge runs.
		right := &store.BerthRight{
			ID: store.NewID(), MarinaID: f.marinaID, BerthID: f.berthID, Kind: "ownership_share", FromOn: d("2027-01-01"),
			ToOn: d("2027-12-31"), Title: store.JSON(`{}`), Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		holder := &store.BerthRightHolder{ID: store.NewID(), MarinaID: f.marinaID, RightID: right.ID, CustomerID: f.customerID, ShareMillis: 1001, CreatedAt: now, UpdatedAt: now}
		mustInsert(t, db, right)
		if err := insertRow(t, db, holder); err == nil {
			t.Fatal("share 1001 accepted")
		}
		feeTable := &store.ManagementFeeTable{ID: store.NewID(), MarinaID: f.marinaID, Year: 2027, Name: "F", Basis: "per_m2_berth", Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now}
		feeRun := func(status string) *store.ManagementFeeRun {
			return &store.ManagementFeeRun{
				ID: store.NewID(), MarinaID: f.marinaID, Year: 2027, TableID: feeTable.ID, Status: status, Plan: store.JSON(`{}`),
				Totals: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
			}
		}
		issuedFee := feeRun("issued")
		mustInsert(t, db, feeTable, feeRun("discarded"), issuedFee)
		wantUnique(t, "second issued fee run", insertRow(t, db, feeRun("issued")))
		err = insertRow(t, db, &store.ManagementFeeLine{
			ID: store.NewID(), MarinaID: f.marinaID, RunID: issuedFee.ID, RightID: right.ID, BerthID: f.berthID,
			Holders: store.JSON(`[]`), CreatedAt: now, UpdatedAt: now,
		})
		wantTrigger(t, "line of issued fee run", store.AllocationsImmutableMessage, err)

		table := &store.MillesimalTable{ID: store.NewID(), MarinaID: f.marinaID, Name: "G", Code: "G", Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now}
		entry := &store.MillesimalEntry{ID: store.NewID(), MarinaID: f.marinaID, TableID: table.ID, BerthID: f.berthID, Millis: 1000, CreatedAt: now, UpdatedAt: now}
		mustInsert(t, db, table, entry)
		if _, err := db.NewUpdate().Table("millesimal_entries").Set("millis = 999").Where("id = ?", entry.ID).Exec(ctx); err != nil {
			t.Fatalf("draft entry edit: %v", err)
		}
		if _, err := db.NewUpdate().Table("millesimal_tables").Set("status = 'active'").Where("id = ?", table.ID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = db.NewUpdate().Table("millesimal_entries").Set("millis = 1000").Where("id = ?", entry.ID).Exec(ctx)
		wantTrigger(t, "active entry edit", store.MillesimalImmutableMessage, err)
		_, err = db.NewDelete().Table("millesimal_entries").Where("id = ?", entry.ID).Exec(ctx)
		wantTrigger(t, "active entry delete", store.MillesimalImmutableMessage, err)

		budget := &store.CommonChargeBudget{ID: store.NewID(), MarinaID: f.marinaID, Year: 2027, Status: "approved", Version: 1, CreatedAt: now, UpdatedAt: now}
		item := &store.CommonChargeItem{ID: store.NewID(), MarinaID: f.marinaID, BudgetID: budget.ID, Name: "I", TableID: table.ID, BudgetCents: 100, Version: 1, CreatedAt: now, UpdatedAt: now}
		run := func(kind, status string, seq int) *store.CommonChargeRun {
			r := &store.CommonChargeRun{
				ID: store.NewID(), MarinaID: f.marinaID, BudgetID: budget.ID, Kind: kind, Seq: seq, ReferenceOn: d("2027-12-31"),
				Status: status, Totals: store.JSON(`{}`), Plan: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			if kind == "advance" {
				bp := int64(5000)
				r.ShareBP = &bp
			}
			return r
		}
		advance := run("advance", "draft", 1)
		mustInsert(t, db, budget, item, advance, run("balance", "issued", 1))
		wantUnique(t, "same kind and seq", insertRow(t, db, run("advance", "draft", 1)))
		wantUnique(t, "second issued balance", insertRow(t, db, run("balance", "issued", 2)))
		alloc := &store.CommonChargeAllocation{
			ID: store.NewID(), MarinaID: f.marinaID, RunID: advance.ID, ItemID: item.ID, BerthID: f.berthID,
			BerthShareCents: 50, AmountCents: 50, CreatedAt: now,
		}
		mustInsert(t, db, alloc)
		if _, err := db.NewUpdate().Table("common_charge_runs").Set("status = 'issued'").Where("id = ?", advance.ID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		_, err = db.NewUpdate().Table("common_charge_allocations").Set("amount_cents = 51").Where("id = ?", alloc.ID).Exec(ctx)
		wantTrigger(t, "issued allocation edit", store.AllocationsImmutableMessage, err)
		_, err = db.NewDelete().Table("common_charge_allocations").Where("id = ?", alloc.ID).Exec(ctx)
		wantTrigger(t, "issued allocation delete", store.AllocationsImmutableMessage, err)

		// Holder instalments: negative only for balance credits.
		hi := &store.HolderInstallment{
			ID: store.NewID(), MarinaID: f.marinaID, SourceType: "common_advance", SourceID: advance.ID, CustomerID: f.customerID,
			Seq: 1, Kind: "installment", IssueOn: d("2027-01-01"), DueOn: d("2027-01-31"), AmountCents: -1,
			Status: store.InstallmentScheduled, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := insertRow(t, db, hi); err == nil {
			t.Fatal("negative advance instalment accepted")
		}
		hi.SourceType, hi.Kind = "common_balance", "credit"
		mustInsert(t, db, hi)
	})
}
