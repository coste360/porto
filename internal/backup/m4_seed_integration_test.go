//go:build integration

package backup

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
)

// seedM4Rows adds at least one row of every M4 table (m4-plan §5.4): contract settings, an instalment plan with a
// paid row, a campaign with a proposal pointing to a successor, an addendum, a charter fleet and contract, a waiting
// list entry, a concession, a right with holders, vessels, an owner allocation, a transfer, a mandate with revenue,
// an issued fee run with its frozen lines, an active millesimal table with its frozen entries, an issued common
// charge run with its frozen allocations, holder instalments, a published template version, a generated and a signed
// document, a signature request with a signer, an access link, a communication with a recipient and its message,
// signature settings and an import run. The frozen children exercise the late status pass of internal/backup.
func seedM4Rows(t *testing.T, db *store.DB, marinaID, userID string) {
	t.Helper()
	ctx := context.Background()
	now := store.UTC(time.Now())
	d := calendar.MustParse
	var berth store.Berth
	if err := db.NewSelect().Model(&berth).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("berth: %v", err)
	}
	var vessel store.Vessel
	if err := db.NewSelect().Model(&vessel).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("vessel: %v", err)
	}
	var customer store.Customer
	if err := db.NewSelect().Model(&customer).Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).Scan(ctx); err != nil {
		t.Fatalf("customer: %v", err)
	}
	var mp store.Map
	if err := db.NewSelect().Model(&mp).Where("id = ?", berth.MapID).Scan(ctx); err != nil {
		t.Fatalf("map: %v", err)
	}

	// Contracts: a source and a successor (the successor gets the lower id, the proposal points forward).
	successorID := store.NewID()
	mkContract := func(id string, year int, status string) *store.Contract {
		return &store.Contract{
			ID: id, MarinaID: marinaID, Kind: store.ContractKindMooring, Term: "annual", Number: int64(9001 + year - 2026),
			Year: year, Status: status, CustomerID: customer.ID, VesselID: vessel.ID,
			StartsOn: calendar.New(year, 1, 1), EndsOn: calendar.New(year+1, 1, 1), Channel: "desk",
			PriceSnapshot: store.JSON(`{"total_cents":300000}`), EngineVersion: 1, TotalCents: 300000,
			Policy: store.JSON(`{"version":2}`), InstallmentPlanVersion: 1, CreatedByUserID: userID, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
	}
	source := mkContract(store.NewID(), 2026, "active")
	amount := int64(-1500)
	source.TerminationAmountCents = &amount
	source.SignedAt = now
	successor := mkContract(successorID, 2027, "draft")
	charter := mkContract(store.NewID(), 2028, "active")
	charter.Kind = store.ContractKindCharter
	charter.Term = ""
	charter.Charter = true

	fleet := &store.CharterFleet{ID: store.NewID(), MarinaID: marinaID, CustomerID: customer.ID, Name: "Fleet", Version: 1, CreatedAt: now, UpdatedAt: now}
	campaign := &store.RenewalCampaign{
		ID: store.NewID(), MarinaID: marinaID, Name: "Rinnovi 2027", Filter: store.JSON(`{"terms":["annual"]}`),
		TargetStartsRule: "next_term", DeadlineOn: d("2026-11-15"), Status: "open", Counts: store.JSON(`{"sent":1}`),
		LaunchedAt: now, CreatedByUserID: userID, Version: 2, CreatedAt: now, UpdatedAt: now,
	}
	newTotal := int64(315000)
	proposal := &store.RenewalProposal{
		ID: store.NewID(), MarinaID: marinaID, CampaignID: campaign.ID, SourceContractID: source.ID,
		NewContractID: successorID, Status: "sent", OldTotalCents: 300000, NewTotalCents: &newTotal,
		DeadlineOn: d("2026-11-15"), SentAt: now, SentCount: 1, Version: 2, CreatedAt: now, UpdatedAt: now,
	}

	// Rights.
	concession := &store.Concession{
		ID: store.NewID(), MarinaID: marinaID, Authority: "Autorità", DeedKind: "atto", DeedNumber: "1/2020",
		GrantedOn: d("2020-01-01"), ExpiresOn: d("2060-12-31"), AnnualFeeCents: 1000000, Surfaces: store.JSON(`[]`),
		Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	right := &store.BerthRight{
		ID: store.NewID(), MarinaID: marinaID, BerthID: berth.ID, Kind: "ownership_share", FromOn: d("2029-01-01"),
		ToOn: d("2029-12-31"), Title: store.JSON(`{"deed":"rogito"}`), Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	ownerAlloc := &store.Allocation{
		ID: store.NewID(), MarinaID: marinaID, VesselID: vessel.ID, ResourceType: store.ResourceBerth, ResourceID: berth.ID,
		MapID: mp.ID, FirstNight: d("2029-01-01"), LastNight: d("2029-01-02"), Kind: "owner", Status: store.AllocActive,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	mandate := &store.ManagementMandate{
		ID: store.NewID(), MarinaID: marinaID, RightID: right.ID, FromOn: d("2029-07-01"), ToOn: d("2029-08-31"),
		OwnerShareBP: 6000, Channels: store.Strings{"transit"}, Settlement: "credit", Status: "active", Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	feeTable := &store.ManagementFeeTable{
		ID: store.NewID(), MarinaID: marinaID, Year: 2029, Name: "Oneri", Basis: "per_m2_berth", IstatBP: 120,
		Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	rate := int64(1500)
	feeRun := &store.ManagementFeeRun{
		ID: store.NewID(), MarinaID: marinaID, Year: 2029, TableID: feeTable.ID, Status: "draft", Plan: store.JSON(`{}`),
		Totals: store.JSON(`{"total_cents":7200}`), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	feeLine := &store.ManagementFeeLine{
		ID: store.NewID(), MarinaID: marinaID, RunID: feeRun.ID, RightID: right.ID, BerthID: berth.ID, BaseCents: 7200,
		Days: 365, AmountCents: 7200, Holders: store.JSON(`[]`), CreatedAt: now, UpdatedAt: now,
	}
	table := &store.MillesimalTable{ID: store.NewID(), MarinaID: marinaID, Name: "Generale", Code: "BKGEN", Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now}
	budget := &store.CommonChargeBudget{ID: store.NewID(), MarinaID: marinaID, Year: 2029, Status: "approved", ApprovedAt: now, Version: 2, CreatedAt: now, UpdatedAt: now}
	item := &store.CommonChargeItem{ID: store.NewID(), MarinaID: marinaID, BudgetID: budget.ID, Name: "Pulizie", TableID: table.ID, BudgetCents: 1001, Version: 1, CreatedAt: now, UpdatedAt: now}
	share := int64(5000)
	run := &store.CommonChargeRun{
		ID: store.NewID(), MarinaID: marinaID, BudgetID: budget.ID, Kind: "advance", Seq: 1, ShareBP: &share,
		ReferenceOn: d("2029-01-01"), Status: "draft", Totals: store.JSON(`{}`), Plan: store.JSON(`{}`), Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}

	// Documents.
	file := &store.StoredFile{
		ID: store.NewID(), MarinaID: marinaID, Purpose: "document", ContentType: "application/pdf", SizeBytes: 10,
		SHA256: "abc", StorageKey: marinaID + "/m4-doc.bin", CreatedAt: now, UpdatedAt: now,
	}
	tmpl := &store.Template{ID: store.NewID(), MarinaID: marinaID, Kind: "contract_annual", Code: "backup-m4", Name: "Annuale", Version: 1, CreatedAt: now, UpdatedAt: now}
	tv := &store.TemplateVersion{
		ID: store.NewID(), MarinaID: marinaID, TemplateID: tmpl.ID, Version: 1, Locale: "it", BodyMD: "# Contratto",
		Page: store.JSON(`{}`), Status: "published", SHA256: "sha", PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	// The signed copy gets the lower id: restore defers signed_from_id.
	signedID := store.NewID()
	doc := &store.GeneratedDocument{
		ID: store.NewID(), MarinaID: marinaID, Kind: "contract_annual", TemplateVersionID: tv.ID, Locale: "it",
		SubjectType: "contract", SubjectID: source.ID, Status: "ready", DataEnc: "ciphertext", FileID: file.ID,
		SHA256: "abc", Pages: 2, PDFA: true, CreatedAt: now, UpdatedAt: now,
	}
	signed := &store.GeneratedDocument{
		ID: signedID, MarinaID: marinaID, Kind: "contract_annual", BuiltinRef: "it/signature_evidence", Locale: "it",
		SubjectType: "contract", SubjectID: source.ID, Status: "ready", FileID: file.ID, SHA256: "def", Pages: 3,
		PDFA: true, Signed: true, SignedFromID: doc.ID, CreatedAt: now, UpdatedAt: now,
	}
	request := &store.SignatureRequest{
		ID: store.NewID(), MarinaID: marinaID, SubjectType: "contract", SubjectID: source.ID, DocumentID: doc.ID,
		Driver: "otp", OrderMode: "parallel", Status: "signed", ExpiresAt: now.Add(24 * time.Hour),
		SignedDocumentID: signedID, CompletedAt: now, Version: 3, CreatedAt: now, UpdatedAt: now,
	}
	link := &store.AccessLink{
		ID: store.NewID(), MarinaID: marinaID, Purpose: "signature", SubjectType: "signature_signer", SubjectID: request.ID,
		CustomerID: customer.ID, TokenHash: "hash-m4", ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	msg := &store.Message{
		ID: store.NewID(), MarinaID: marinaID, Channel: "email", Template: "communication", Locale: "it",
		RecipientHash: "h", RecipientEnc: "e", PayloadEnc: "p", DedupeKey: "comm:m4", Status: "sent", CreatedAt: now, UpdatedAt: now,
	}
	comm := &store.CommCampaign{
		ID: store.NewID(), MarinaID: marinaID, Name: "Avviso", Category: "service", Audience: store.JSON(`{"customer_ids":[]}`),
		Channels: store.Strings{"email"}, TemplateID: tmpl.ID, Subject: "Avviso", BodyMD: "Testo", Attachments: store.JSON(`[]`),
		Status: "sent", Counts: store.JSON(`{"queued":1}`), SentAt: now, Version: 2, CreatedAt: now, UpdatedAt: now,
	}
	importRun := &store.ImportRun{
		ID: store.NewID(), MarinaID: marinaID, Entity: "contracts", Status: "applied", Mapping: store.JSON(`{"code":0}`),
		Report: store.JSON(`{"valid":1}`), FileSHA256: "f", RowCount: 1, AppliedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now,
	}

	rows := []any{}
	// The demo seed may already have created the per-marina singletons.
	if n, _ := db.NewSelect().Table("contract_settings").Where("marina_id = ?", marinaID).Count(ctx); n == 0 {
		rows = append(rows, store.DefaultContractSettings(marinaID, now))
	}
	if n, _ := db.NewSelect().Table("concessions").Where("marina_id = ?", marinaID).Count(ctx); n == 0 {
		rows = append(rows, concession)
	} else if err := db.NewSelect().Model(concession).Where("marina_id = ?", marinaID).Scan(ctx); err != nil {
		t.Fatalf("concession: %v", err)
	}
	if n, _ := db.NewSelect().Table("signature_settings").Where("marina_id = ?", marinaID).Count(ctx); n == 0 {
		rows = append(rows, &store.SignatureSettings{MarinaID: marinaID, Version: 1, CreatedAt: now, UpdatedAt: now})
	}
	rows = append(rows,
		successor, source, charter, fleet,
		&store.CharterFleetVessel{ID: store.NewID(), MarinaID: marinaID, FleetID: fleet.ID, VesselID: vessel.ID, Active: true, CreatedAt: now, UpdatedAt: now},
		&store.CharterContract{ContractID: charter.ID, MarinaID: marinaID, FleetID: fleet.ID, ChangeoverWeekday: 6, PricingMode: "per_berth", OutNights: store.JSON(`[]`), Version: 1, CreatedAt: now, UpdatedAt: now},
		&store.Installment{
			ID: store.NewID(), MarinaID: marinaID, ContractID: source.ID, PlanVersion: 1, Seq: 1, Kind: "deposit",
			IssueOn: d("2026-01-01"), DueOn: d("2026-01-08"), AmountCents: 90000, Status: store.InstallmentPaid, IssuedAt: now,
			SettlementSource: store.SettlementManual, SettlementMethod: "bank_transfer", SettlementRefEnc: "ref", SettledOn: d("2026-01-05"),
			SettledAt: now, SettledByUserID: userID, Version: 2, CreatedAt: now, UpdatedAt: now,
		},
		&store.Installment{
			ID: store.NewID(), MarinaID: marinaID, ContractID: source.ID, PlanVersion: 1, Seq: 2, Kind: "installment",
			IssueOn: d("2026-03-15"), DueOn: d("2026-04-01"), AmountCents: 210000, Status: store.InstallmentScheduled,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		campaign, proposal,
		&store.ContractAddendum{
			ID: store.NewID(), MarinaID: marinaID, ContractID: source.ID, Seq: 1, Kind: "parking", EffectiveOn: d("2026-06-01"),
			Payload: store.JSON(`{"item_id":null}`), AmountDeltaCents: 5000, Status: "draft", Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		&store.WaitlistEntry{
			ID: store.NewID(), MarinaID: marinaID, CustomerID: customer.ID, LOACm: 1200, BeamCm: 400, Term: "annual",
			GroupIDs: store.Strings{}, RequestedFrom: d("2027-01-01"), Status: "waiting", Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		&store.ConcessionObligation{ID: store.NewID(), MarinaID: marinaID, ConcessionID: concession.ID, Kind: "insurance", DueOn: d("2027-03-31"), Recurrence: "yearly", Version: 1, CreatedAt: now, UpdatedAt: now},
		right,
		&store.BerthRightHolder{ID: store.NewID(), MarinaID: marinaID, RightID: right.ID, CustomerID: customer.ID, ShareMillis: 1000, CreatedAt: now, UpdatedAt: now},
		&store.BerthRightVessel{ID: store.NewID(), MarinaID: marinaID, RightID: right.ID, VesselID: vessel.ID, IsPrimary: true, CreatedAt: now, UpdatedAt: now},
		ownerAlloc,
		&store.BerthOccupancy{MarinaID: marinaID, BerthID: berth.ID, Night: d("2029-01-01"), AllocationID: ownerAlloc.ID, Kind: "owner"},
		&store.BerthOccupancy{MarinaID: marinaID, BerthID: berth.ID, Night: d("2029-01-02"), AllocationID: ownerAlloc.ID, Kind: "owner"},
		&store.BerthRightAllocation{AllocationID: ownerAlloc.ID, MarinaID: marinaID, RightID: right.ID, CreatedAt: now},
		&store.RightTransfer{
			ID: store.NewID(), MarinaID: marinaID, RightID: right.ID, EffectiveOn: d("2029-06-01"), ToHolders: store.JSON(`[]`),
			RequirementPolicy: "warn", RequirementIssues: store.JSON(`[]`), FeesPolicy: "reassign", Status: "draft", Version: 1,
			CreatedAt: now, UpdatedAt: now,
		},
		mandate,
		&store.MandateRevenue{
			ID: store.NewID(), MarinaID: marinaID, MandateID: mandate.ID, AllocationID: ownerAlloc.ID, Nights: 2,
			RevenueCents: 20000, OwnerShareCents: 12000, ThroughOn: d("2029-07-02"), CreatedAt: now, UpdatedAt: now,
		},
		feeTable,
		&store.ManagementFeeTableRow{ID: store.NewID(), MarinaID: marinaID, TableID: feeTable.ID, RateCentsPerM2: &rate, Sort: 1, CreatedAt: now, UpdatedAt: now},
		feeRun, feeLine, table,
		&store.MillesimalEntry{ID: store.NewID(), MarinaID: marinaID, TableID: table.ID, BerthID: berth.ID, Millis: 1000, CreatedAt: now, UpdatedAt: now},
		budget, item, run,
		&store.CommonChargeAllocation{
			ID: store.NewID(), MarinaID: marinaID, RunID: run.ID, ItemID: item.ID, BerthID: berth.ID, RightID: right.ID,
			CustomerID: customer.ID, BerthShareCents: 500, AmountCents: 500, CreatedAt: now,
		},
		&store.HolderInstallment{
			ID: store.NewID(), MarinaID: marinaID, SourceType: "common_advance", SourceID: run.ID, RunID: run.ID, RightID: right.ID,
			CustomerID: customer.ID, Seq: 1, Kind: "installment", IssueOn: d("2029-01-15"), DueOn: d("2029-01-31"),
			AmountCents: 500, Status: store.InstallmentScheduled, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		&store.HolderInstallment{
			ID: store.NewID(), MarinaID: marinaID, SourceType: "common_balance", SourceID: run.ID, CustomerID: customer.ID,
			Seq: 1, Kind: "credit", IssueOn: d("2029-12-31"), DueOn: d("2030-01-31"), AmountCents: -120,
			Status: store.InstallmentScheduled, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		file, tmpl, tv, doc, signed, request,
		&store.SignatureSigner{
			ID: store.NewID(), MarinaID: marinaID, RequestID: request.ID, Seq: 1, Role: "customer", CustomerID: customer.ID,
			DisplayNameEnc: "n", EmailEnc: "e", EmailHash: "h", Locale: "it", Status: "signed", LinkID: link.ID,
			SignedAt: now, EvidenceEnc: "ev", Version: 2, CreatedAt: now, UpdatedAt: now,
		},
		link, msg, comm,
		&store.CommRecipient{ID: store.NewID(), MarinaID: marinaID, CampaignID: comm.ID, CustomerID: customer.ID, Status: "queued", MessageID: msg.ID, CreatedAt: now, UpdatedAt: now},
		&store.CustomerMessage{ID: store.NewID(), MarinaID: marinaID, CustomerID: customer.ID, MessageID: msg.ID, Category: "service", SourceType: "communication", SourceID: comm.ID, Template: "communication", CreatedAt: now},
		importRun,
		&store.ImportRunItem{ID: store.NewID(), MarinaID: marinaID, RunID: importRun.ID, EntityType: "contract", EntityID: source.ID, CreatedAt: now},
	)
	for _, row := range rows {
		if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
			t.Fatalf("insert %T: %v", row, err)
		}
	}
	// Freeze the parents after their children are in, as the services do.
	for _, q := range []struct{ table, status, id string }{
		{"millesimal_tables", "active", table.ID},
		{"management_fee_runs", "issued", feeRun.ID},
		{"common_charge_runs", "issued", run.ID},
	} {
		if _, err := db.NewUpdate().Table(q.table).Set("status = ?", q.status).Where("id = ?", q.id).Exec(ctx); err != nil {
			t.Fatalf("freeze %s: %v", q.table, err)
		}
	}
	if _, err := db.NewUpdate().Table("contracts").Set("signed_document_id = ?", signedID).
		Set("signature_request_id = ?", request.ID).Where("id = ?", source.ID).Exec(ctx); err != nil {
		t.Fatalf("link contract: %v", err)
	}
}
