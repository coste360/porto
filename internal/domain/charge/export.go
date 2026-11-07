package charge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// "Da fatturare" exports (spec 16.5; m5-plan §3.6): preview, create (INV-53: each charge in exactly one first
// export), files built after commit and lazily rebuilt, re-export, downloads.

// Warning is a preview warning (customer_fiscal_incomplete) with the charges it concerns.
type Warning struct {
	Code       string
	CustomerID string
	ChargeIDs  []string
}

// Preview is the result of previewChargeExport.
type Preview struct {
	Totals   ExportTotals
	Warnings []Warning
}

func (f ExportFilter) selection() (store.ExportSelection, error) {
	if f.PeriodFrom.IsZero() || f.PeriodTo.IsZero() {
		return store.ExportSelection{}, invalid("period_from", "required")
	}
	if f.PeriodTo.Before(f.PeriodFrom) {
		return store.ExportSelection{}, invalid("period_to", "before_from")
	}
	for _, o := range f.OriginTypes {
		if originLabels[o] == "" {
			return store.ExportSelection{}, invalid("origin_types", "invalid")
		}
	}
	return store.ExportSelection{
		PeriodFrom: f.PeriodFrom, PeriodTo: f.PeriodTo, CustomerID: f.CustomerID, OriginTypes: f.OriginTypes,
		RevenueClass: strings.TrimSpace(f.RevenueClass),
	}, nil
}

// PreviewExport counts and totals the charges an export would take, with warnings (charges.export).
func (s *Service) PreviewExport(ctx context.Context, p *auth.Principal, marinaID string, f ExportFilter) (*Preview, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, err
	}
	sel, err := f.selection()
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	n, err := store.CountExportable(ctx, db, scope, sel)
	if err != nil {
		return nil, err
	}
	if n > MaxExportCharges {
		return nil, &ExportTooLargeError{Count: n}
	}
	charges, err := store.ExportableCharges(ctx, db, scope, sel, MaxExportCharges, false)
	if err != nil {
		return nil, err
	}
	lines, err := store.ChargeLinesOf(ctx, db, scope, chargeIDs(charges))
	if err != nil {
		return nil, err
	}
	out := &Preview{Totals: TotalsOf(charges, lines), Warnings: []Warning{}}
	out.Warnings, err = s.fiscalWarnings(ctx, db, scope, charges)
	return out, err
}

// fiscalWarnings lists the customers whose live fiscal data are incomplete for an invoice (walk-in receipts are
// fine).
func (s *Service) fiscalWarnings(ctx context.Context, db bun.IDB, scope store.MarinaScope, charges []store.Charge) ([]Warning, error) {
	byCustomer := map[string][]string{}
	var ids []string
	for _, c := range charges {
		if c.CustomerID == "" || c.FiscalMode == FiscalReceipt {
			continue
		}
		if _, ok := byCustomer[c.CustomerID]; !ok {
			ids = append(ids, c.CustomerID)
		}
		byCustomer[c.CustomerID] = append(byCustomer[c.CustomerID], c.ID)
	}
	out := []Warning{}
	if len(ids) == 0 {
		return out, nil
	}
	custs, err := store.CustomersByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	sort.Slice(custs, func(i, j int) bool { return custs[i].ID < custs[j].ID })
	for i := range custs {
		if snapshotOf(&custs[i]).Incomplete() {
			out = append(out, Warning{Code: "customer_fiscal_incomplete", CustomerID: custs[i].ID, ChargeIDs: byCustomer[custs[i].ID]})
		}
	}
	return out, nil
}

func chargeIDs(cs []store.Charge) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

// exportFilters is the JSON stored on an export row.
type exportFilters struct {
	PeriodFrom   string   `json:"period_from,omitempty"`
	PeriodTo     string   `json:"period_to,omitempty"`
	CustomerID   string   `json:"customer_id,omitempty"`
	OriginTypes  []string `json:"origin_types,omitempty"`
	RevenueClass string   `json:"revenue_class,omitempty"`
	ChargeIDs    int      `json:"charge_ids,omitempty"`
}

// ExportFiltersOf decodes the filters of an export row.
func ExportFiltersOf(e *store.ChargeExport) ExportFilter {
	var f exportFilters
	_ = json.Unmarshal(e.Filters, &f)
	out := ExportFilter{CustomerID: f.CustomerID, OriginTypes: f.OriginTypes, RevenueClass: f.RevenueClass}
	out.PeriodFrom, _ = calendar.Parse(f.PeriodFrom)
	out.PeriodTo, _ = calendar.Parse(f.PeriodTo)
	return out
}

// TotalsOfExport decodes the totals of an export row.
func TotalsOfExport(e *store.ChargeExport) ExportTotals {
	var t ExportTotals
	_ = json.Unmarshal(e.Totals, &t)
	return t
}

// CreateExport creates an export of the to_invoice charges of a filter (charges.export): one transaction locks the
// selection, numbers the export, moves each charge to exported with a conditional UPDATE (rows taken meanwhile by
// another export are dropped), inserts the first items, refreshes the fiscal snapshots and stores the totals;
// the files are built after commit. With exporterCode the export is delivered by that ChargeExporter
// (ErrExporterUnknown when none is registered with that code).
func (s *Service) CreateExport(ctx context.Context, p *auth.Principal, marinaID string, f ExportFilter, exporterCode string) (*store.ChargeExport, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, err
	}
	sel, err := f.selection()
	if err != nil {
		return nil, err
	}
	exporterCode = strings.TrimSpace(exporterCode)
	if exporterCode != "" && s.exporter(exporterCode) == nil {
		return nil, ErrExporterUnknown
	}
	var e *store.ChargeExport
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		n, err := store.CountExportable(ctx, tx, scope, sel)
		if err != nil {
			return err
		}
		if n > MaxExportCharges {
			return &ExportTooLargeError{Count: n}
		}
		charges, err := store.ExportableCharges(ctx, tx, scope, sel, MaxExportCharges, true)
		if err != nil {
			return err
		}
		if len(charges) == 0 {
			return ErrExportEmpty
		}
		filters := exportFilters{
			PeriodFrom: sel.PeriodFrom.String(), PeriodTo: sel.PeriodTo.String(), CustomerID: sel.CustomerID,
			OriginTypes: sel.OriginTypes, RevenueClass: sel.RevenueClass,
		}
		e, err = s.exportTx(ctx, tx, scope, p.Actor(), "export", "", sel.PeriodFrom, sel.PeriodTo, filters, exporterCode, charges, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.buildFilesAfterCommit(ctx, scope, e)
	return e, nil
}

// exportTx writes an export of charges in tx. first: the charges move to exported (first export); otherwise a
// re-export of already exported charges.
func (s *Service) exportTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, kind, reexportOf string,
	from, to calendar.Date, filters exportFilters, exporterCode string, charges []store.Charge, first bool,
) (*store.ChargeExport, error) {
	now := s.now()
	id := store.NewID()
	ids := chargeIDs(charges)
	if first {
		done, err := store.MarkExported(ctx, tx, scope, ids, id, now)
		if err != nil {
			return nil, mapStore(err)
		}
		if len(done) == 0 {
			return nil, ErrExportEmpty
		}
		keep := map[string]bool{}
		for _, d := range done {
			keep[d] = true
		}
		kept := charges[:0]
		for _, c := range charges {
			if keep[c.ID] {
				c.InvoicingStatus, c.FirstExportID = store.ChargeExported, id
				kept = append(kept, c)
			}
		}
		charges = kept
		ids = chargeIDs(charges)
		if err := s.refreshSnapshotsTx(ctx, tx, scope, charges); err != nil {
			return nil, err
		}
	}
	lines, err := store.ChargeLinesOf(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	totals := TotalsOf(charges, lines)
	year := s.today(ctx, tx, scope).Year()
	n, err := store.NextBillingNumber(ctx, tx, scope, store.SeriesExport, year)
	if err != nil {
		return nil, err
	}
	label, userID := actorLabel(actor)
	_ = label
	e := &store.ChargeExport{
		ID: id, MarinaID: scope.ID(), Number: fmt.Sprintf("E%04d-%04d", year, n), Kind: kind, ReexportOf: reexportOf,
		PeriodFrom: from, PeriodTo: to, Filters: store.MustJSON(filters), Delivery: "file", DeliveryStatus: "none",
		ChargeCount: len(charges), LineCount: len(lines), Totals: store.MustJSON(totals), CreatedByUserID: userID,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if exporterCode != "" {
		e.Delivery, e.ExporterCode, e.DeliveryStatus = "exporter", exporterCode, "pending"
	}
	live, err := s.exportCharges(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	liveByID := make(map[string]*ExportCharge, len(live))
	for i := range live {
		liveByID[live[i].Charge.ID] = &live[i]
	}
	items := make([]store.ChargeExportItem, 0, len(charges))
	for _, c := range charges {
		it := store.ChargeExportItem{ID: store.NewID(), MarinaID: scope.ID(), ExportID: id, ChargeID: c.ID, IsFirst: first, CreatedAt: now}
		if lc := liveByID[c.ID]; lc != nil {
			if it.RowSnapshotEnc, err = s.encryptRowSnapshot(rowSnapshotOf(lc), id, c.ID); err != nil {
				return nil, err
			}
		}
		items = append(items, it)
	}
	if err := store.InsertChargeExport(ctx, tx, e, items); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrExportEmpty
		}
		return nil, err
	}
	action := "charge_export.create"
	if kind == "reexport" {
		action = "charge_export.reexport"
	}
	if err := s.write(ctx, tx, scope, actor, action, "charge_export", id, map[string]any{
		"number": e.Number, "kind": kind, "charge_count": e.ChargeCount, "line_count": e.LineCount,
		"total_cents": int64(totals.Gross), "delivery": e.Delivery, "exporter_code": exporterCode,
	}); err != nil {
		return nil, err
	}
	data := event.ChargeExportedData{ExportID: id, Kind: kind, ChargeCount: e.ChargeCount}
	if !from.IsZero() {
		pf, pt := from, to
		data.PeriodFrom, data.PeriodTo = &pf, &pt
	}
	if err := s.emit(ctx, tx, scope, event.ChargeExported, data); err != nil {
		return nil, err
	}
	return e, nil
}

// refreshSnapshotsTx refreshes the fiscal snapshot of charges at their first export from the live customer, unless
// the customer is anonymised (the creation snapshot stays, spec 30).
func (s *Service) refreshSnapshotsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, charges []store.Charge) error {
	var ids []string
	for _, c := range charges {
		if c.CustomerID != "" {
			ids = append(ids, c.CustomerID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	custs, err := store.CustomersByIDs(ctx, tx, scope, ids)
	if err != nil {
		return err
	}
	byID := map[string]*store.Customer{}
	for i := range custs {
		byID[custs[i].ID] = &custs[i]
	}
	for i := range charges {
		c := &charges[i]
		cu := byID[c.CustomerID]
		if cu == nil || !cu.AnonymizedAt.IsZero() {
			continue
		}
		enc, err := s.encryptSnapshot(snapshotOf(cu), c.ID)
		if err != nil {
			return err
		}
		if err := store.RefreshFiscalSnapshot(ctx, tx, scope, c.ID, enc); err != nil {
			return err
		}
		c.FiscalSnapshotEnc = enc
	}
	return nil
}

// ReexportInput is a re-export: the charges of an export or explicit charges (already exported).
type ReexportInput struct {
	ExportID  string
	ChargeIDs []string
}

// Reexport creates a re-export (kind reexport, items not first, statuses unchanged; charges.export).
func (s *Service) Reexport(ctx context.Context, p *auth.Principal, marinaID string, in ReexportInput) (*store.ChargeExport, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, err
	}
	if (in.ExportID == "") == (len(in.ChargeIDs) == 0) {
		return nil, invalid("export_id", "export_id_or_charge_ids")
	}
	if len(in.ChargeIDs) > MaxExportCharges {
		return nil, &ExportTooLargeError{Count: len(in.ChargeIDs)}
	}
	var e *store.ChargeExport
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ids := in.ChargeIDs
		var from, to calendar.Date
		filters := exportFilters{ChargeIDs: len(in.ChargeIDs)}
		if in.ExportID != "" {
			src, err := store.GetChargeExport(ctx, tx, scope, in.ExportID)
			if err != nil {
				return notFound(err)
			}
			if ids, err = store.ExportChargeIDs(ctx, tx, scope, src.ID); err != nil {
				return err
			}
			from, to = src.PeriodFrom, src.PeriodTo
			_ = json.Unmarshal(src.Filters, &filters)
		}
		charges, err := store.ChargesByIDs(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		if len(charges) != len(uniqueStrings(ids)) {
			return auth404()
		}
		for _, c := range charges {
			// INV-53: only charges that had a first export can be re-exported; cancelled ones never are (an undone
			// opening balance is invoiced and cancelled, and opening balances are born invoiced without an export).
			if c.InvoicingStatus == store.ChargeToInvoice || c.FirstExportID == "" || c.PaymentStatus == store.ChargeCancelled {
				return invalid("charge_ids", "not_exported")
			}
		}
		if len(charges) == 0 {
			return ErrExportEmpty
		}
		e, err = s.exportTx(ctx, tx, scope, p.Actor(), "reexport", in.ExportID, from, to, filters, "", charges, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.buildFilesAfterCommit(ctx, scope, e)
	return e, nil
}

func uniqueStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// buildFilesAfterCommit builds and stores the files of a committed export; a failure is logged and recovered by
// the next download (lazy build).
func (s *Service) buildFilesAfterCommit(ctx context.Context, scope store.MarinaScope, e *store.ChargeExport) {
	if err := s.ensureFiles(ctx, scope, e); err != nil {
		s.d.Log.Warn("charge: export files not built", "marina_id", scope.ID(), "export_id", e.ID, "error_kind", fmt.Sprintf("%T", err))
	}
}

// File formats of an export download.
const (
	FormatCSV  = "csv"
	FormatXLSX = "xlsx"
	FormatPDF  = "pdf"
)

// ExportListItem is an export of the list.
type ExportListItem = store.ChargeExport

// ListExports returns a page of exports (charges.export).
func (s *Service) ListExports(ctx context.Context, p *auth.Principal, marinaID, cursor string, limit int) (store.Page[store.ChargeExport], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return store.Page[store.ChargeExport]{}, err
	}
	return store.ListChargeExports(ctx, s.d.DB.DB, scope, cursor, limit)
}

// ExportDetail is an export with a page of its items.
type ExportDetail struct {
	Export     store.ChargeExport
	Items      []store.ExportItemRow
	Names      map[string]string
	NextCursor string
}

// GetExport returns an export and a page of its items (charges.export).
func (s *Service) GetExport(ctx context.Context, p *auth.Principal, marinaID, id, cursor string, limit int) (*ExportDetail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	e, err := store.GetChargeExport(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	page, err := store.ExportItems(ctx, db, scope, id, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := &ExportDetail{Export: *e, Items: page.Items, NextCursor: page.NextCursor, Names: map[string]string{}}
	if p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead) {
		var ids []string
		for _, it := range page.Items {
			if it.CustomerID != "" {
				ids = append(ids, it.CustomerID)
			}
		}
		custs, err := store.CustomersByIDs(ctx, db, scope, uniqueStrings(ids))
		if err != nil {
			return nil, err
		}
		for i := range custs {
			out.Names[custs[i].ID] = displayName(&custs[i])
		}
	}
	return out, nil
}

// DownloadExport returns a file of an export (charges.export). The summary PDF is stored encrypted with the export
// (built after commit, lazily rebuilt when missing); the CSV and the workbook are built on each download from the
// immutable charges and the row data frozen on the items at creation (stored_files accepts PDF and images only),
// so they are the same bytes as the first build, whose sha256 is kept on the export.
func (s *Service) DownloadExport(ctx context.Context, p *auth.Principal, marinaID, id, format string) ([]byte, string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, "", err
	}
	if format != FormatCSV && format != FormatXLSX && format != FormatPDF {
		return nil, "", invalid("format", "invalid")
	}
	e, err := store.GetChargeExport(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, "", notFound(err)
	}
	if err := s.ensureFiles(ctx, scope, e); err != nil {
		return nil, "", err
	}
	if format == FormatPDF {
		b, err := s.openFile(ctx, scope, e.PDFFileID)
		return b, e.Number, err
	}
	csvB, xlsxB, err := s.spreadsheets(ctx, scope, e)
	if err != nil {
		return nil, "", err
	}
	if format == FormatCSV {
		return csvB, e.Number, nil
	}
	return xlsxB, e.Number, nil
}

// openFile reads a stored file of the marina.
func (s *Service) openFile(ctx context.Context, scope store.MarinaScope, id string) ([]byte, error) {
	f, err := store.GetStoredFile(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	return s.d.Files.Open(ctx, scope.ID(), f.ID, f.StorageKey)
}

// spreadsheets builds the CSV and the workbook of an export from its stored items.
func (s *Service) spreadsheets(ctx context.Context, scope store.MarinaScope, e *store.ChargeExport) ([]byte, []byte, error) {
	charges, err := s.LoadExportCharges(ctx, s.d.DB.DB, scope, e.ID)
	if err != nil {
		return nil, nil, err
	}
	rows := ExportRows(charges)
	csvB, err := ExportCSV(rows)
	if err != nil {
		return nil, nil, err
	}
	xlsxB, err := ExportXLSX(rows)
	return csvB, xlsxB, err
}

// ensureFiles stores the summary PDF and the sha256 of the files of an export that does not have them yet
// (idempotent).
func (s *Service) ensureFiles(ctx context.Context, scope store.MarinaScope, e *store.ChargeExport) error {
	if e.PDFFileID != "" && e.CSVSHA256 != "" && e.XLSXSHA256 != "" {
		return nil
	}
	if s.d.Files == nil {
		return errors.New("charge: no file store")
	}
	csvB, xlsxB, pdfB, err := s.BuildExportFiles(ctx, scope, e)
	if err != nil {
		return err
	}
	now := s.now()
	upd := *e
	if upd.CSVSHA256 == "" {
		upd.CSVSHA256, upd.XLSXSHA256 = sha256Hex(csvB), sha256Hex(xlsxB)
	}
	if upd.PDFFileID == "" && pdfB != nil {
		id := store.NewID()
		blob, err := s.d.Files.Put(ctx, scope.ID(), id, bytes.NewReader(pdfB), int64(len(pdfB))+1)
		if err != nil {
			return err
		}
		f := &store.StoredFile{
			ID: id, MarinaID: scope.ID(), Purpose: "document", ContentType: "application/pdf", SizeBytes: blob.Size, SHA256: blob.SHA256,
			StorageKey: blob.StorageKey, CreatedByUserID: e.CreatedByUserID, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertStoredFile(ctx, s.d.DB.DB, f); err != nil {
			return err
		}
		upd.PDFFileID, upd.PDFSHA256 = id, blob.SHA256
	}
	if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		return store.SetChargeExportFiles(ctx, tx, scope, &upd, now)
	}); err != nil {
		return err
	}
	*e = upd
	return nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// LoadExportCharges loads the charges of an export with everything its rows show (fiscal data decrypted in memory).
func (s *Service) LoadExportCharges(ctx context.Context, db bun.IDB, scope store.MarinaScope, exportID string) ([]ExportCharge, error) {
	ids, err := store.ExportChargeIDs(ctx, db, scope, exportID)
	if err != nil {
		return nil, err
	}
	out, err := s.exportCharges(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	frozen, err := store.ExportItemSnapshots(ctx, db, scope, exportID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		enc, ok := frozen[out[i].Charge.ID]
		if !ok {
			continue
		}
		r, err := s.decryptRowSnapshot(enc, exportID, out[i].Charge.ID)
		if err != nil {
			return nil, err
		}
		r.apply(&out[i])
	}
	return out, nil
}

// rowSnapshot is what the rows of an export show beyond the immutable charge and its lines, frozen when the export
// is created so that its files are the same bytes on every download (m5-plan §16 D27): later payments, vessel
// renames, receipts or a due date moved on an exported charge (allowed by INV-51) do not change a file the
// accountant already received.
type rowSnapshot struct {
	DueOn          calendar.Date `json:"due_on"`
	PaymentStatus  string        `json:"payment_status"`
	PaidCents      int64         `json:"paid_cents"`
	VesselName     string        `json:"vessel_name,omitempty"`
	VesselReg      string        `json:"vessel_reg,omitempty"`
	ContractCode   string        `json:"contract_code,omitempty"`
	InstallmentSeq int           `json:"installment_seq,omitempty"`
	CorrectsNumber string        `json:"corrects_number,omitempty"`
	FiscalDocument string        `json:"fiscal_document,omitempty"`
}

func rowSnapshotOf(c *ExportCharge) rowSnapshot {
	return rowSnapshot{
		DueOn: c.Charge.DueOn, PaymentStatus: c.Charge.PaymentStatus, PaidCents: c.Charge.PaidAmountCents + c.Charge.CreditedCents,
		VesselName: c.VesselName, VesselReg: c.VesselReg, ContractCode: c.ContractCode, InstallmentSeq: c.InstallmentSeq,
		CorrectsNumber: c.CorrectsNumber, FiscalDocument: c.FiscalDocument,
	}
}

// apply overwrites the live fields of an export charge with the frozen ones (the paid amount is carried as
// PaidAmountCents with CreditedCents zero: the files show their sum).
func (r rowSnapshot) apply(c *ExportCharge) {
	if !r.DueOn.IsZero() { // rows frozen before due_on was part of the snapshot keep the live date
		c.Charge.DueOn = r.DueOn
	}
	c.Charge.PaymentStatus, c.Charge.PaidAmountCents, c.Charge.CreditedCents = r.PaymentStatus, r.PaidCents, 0
	c.VesselName, c.VesselReg, c.ContractCode, c.InstallmentSeq = r.VesselName, r.VesselReg, r.ContractCode, r.InstallmentSeq
	c.CorrectsNumber, c.FiscalDocument = r.CorrectsNumber, r.FiscalDocument
}

func rowSnapshotAAD(exportID, chargeID string) string {
	return "charge_export_item:" + exportID + ":" + chargeID
}

func (s *Service) encryptRowSnapshot(r rowSnapshot, exportID, chargeID string) (string, error) {
	if s.d.Keys == nil {
		return "", nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return s.d.Keys.EncryptString(string(b), rowSnapshotAAD(exportID, chargeID))
}

func (s *Service) decryptRowSnapshot(enc, exportID, chargeID string) (rowSnapshot, error) {
	var r rowSnapshot
	if s.d.Keys == nil {
		return r, errors.New("charge: no key ring for the export rows")
	}
	raw, err := s.d.Keys.DecryptString(enc, rowSnapshotAAD(exportID, chargeID))
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(raw), &r)
	return r, err
}

func (s *Service) exportCharges(ctx context.Context, db bun.IDB, scope store.MarinaScope, ids []string) ([]ExportCharge, error) {
	charges, err := store.ChargesByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	lines, err := store.ChargeLinesOf(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	byCharge := map[string][]store.ChargeLine{}
	for _, l := range lines {
		byCharge[l.ChargeID] = append(byCharge[l.ChargeID], l)
	}
	var contractIDs, vesselIDs, corrects, instIDs []string
	for _, c := range charges {
		if c.ContractID != "" {
			contractIDs = append(contractIDs, c.ContractID)
		}
		if c.VesselID != "" {
			vesselIDs = append(vesselIDs, c.VesselID)
		}
		if c.CorrectsChargeID != "" {
			corrects = append(corrects, c.CorrectsChargeID)
		}
		if c.OriginType == store.ChargeOriginInstallment || c.OriginType == store.ChargeOriginHolderInstallment {
			instIDs = append(instIDs, c.OriginID)
		}
	}
	codes := map[string]string{}
	if len(contractIDs) > 0 {
		ctrs, err := store.ContractsByIDs(ctx, db, scope, uniqueStrings(contractIDs))
		if err != nil {
			return nil, err
		}
		for i := range ctrs {
			codes[ctrs[i].ID] = contract.Code(&ctrs[i])
		}
	}
	vessels := map[string]store.Vessel{}
	for _, id := range uniqueStrings(vesselIDs) {
		if v, err := store.GetVessel(ctx, db, scope, id); err == nil {
			vessels[id] = *v
		}
	}
	numbers := map[string]string{}
	if len(corrects) > 0 {
		cs, err := store.ChargesByIDs(ctx, db, scope, uniqueStrings(corrects))
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			numbers[c.ID] = c.Number
		}
	}
	seqs, err := store.InstallmentSeqs(ctx, db, scope, instIDs)
	if err != nil {
		return nil, err
	}
	docsNo, err := store.FiscalDocumentNumbers(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ExportCharge, 0, len(charges))
	for _, c := range charges {
		f, err := s.DecryptSnapshot(&c)
		if err != nil {
			return nil, err
		}
		v := vessels[c.VesselID]
		out = append(out, ExportCharge{
			Charge: c, Lines: byCharge[c.ID], Fiscal: f, ContractCode: codes[c.ContractID], InstallmentSeq: seqs[c.OriginID],
			VesselName: v.Name, VesselReg: v.Registration, CorrectsNumber: numbers[c.CorrectsChargeID], FiscalDocument: docsNo[c.ID],
		})
	}
	return out, nil
}

// BuildExportFiles builds the CSV, the workbook and the summary PDF of an export from its stored items.
func (s *Service) BuildExportFiles(ctx context.Context, scope store.MarinaScope, e *store.ChargeExport) (csvB, xlsxB, pdfB []byte, err error) {
	db := s.d.DB.DB
	charges, err := s.LoadExportCharges(ctx, db, scope, e.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	rows := ExportRows(charges)
	if csvB, err = ExportCSV(rows); err != nil {
		return nil, nil, nil, err
	}
	if xlsxB, err = ExportXLSX(rows); err != nil {
		return nil, nil, nil, err
	}
	if s.d.Docs != nil {
		view, err := s.summaryView(ctx, db, scope, e, charges)
		if err != nil {
			return nil, nil, nil, err
		}
		if pdfB, err = s.d.Docs.RenderEphemeral(ctx, scope, docs.KindChargeExportSummary, "", view); err != nil {
			return nil, nil, nil, err
		}
	}
	return csvB, xlsxB, pdfB, nil
}

// summaryView builds the data of the summary PDF (export_summary data source, m5-plan §14.3 exportdoc).
func (s *Service) summaryView(ctx context.Context, db bun.IDB, scope store.MarinaScope, e *store.ChargeExport, charges []ExportCharge) (views.ChargeExportSummaryView, error) {
	m, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys)
	if err != nil {
		return views.ChargeExportSummaryView{}, err
	}
	var cs []store.Charge
	var lines []store.ChargeLine
	for _, c := range charges {
		cs = append(cs, c.Charge)
		lines = append(lines, c.Lines...)
	}
	t := TotalsOf(cs, lines)
	v := views.ChargeExportSummaryView{
		Marina: m, Number: e.Number, Kind: e.Kind, PeriodFrom: e.PeriodFrom, PeriodTo: e.PeriodTo, Filters: filtersText(e),
		CreatedAt: e.CreatedAt, ChargeCount: len(cs), LineCount: len(lines), Net: t.Net, VATTotal: t.VAT, Total: t.Gross,
		VAT: []views.VATRow{}, ByOrigin: []views.TotalRow{}, ByRevenueClass: []views.TotalRow{}, Charges: []views.ExportChargeRow{},
	}
	for _, b := range t.ByVAT {
		v.VAT = append(v.VAT, views.VATRow{RateBP: views.BP(b.RateBP), Nature: b.Nature, Net: b.Net, VAT: b.VAT, Gross: b.Gross})
	}
	for _, o := range t.ByOrigin {
		v.ByOrigin = append(v.ByOrigin, views.TotalRow{Key: originLabels[o.Key], Count: o.Count, Gross: o.Total})
	}
	for _, o := range t.ByRevenue {
		v.ByRevenueClass = append(v.ByRevenueClass, views.TotalRow{Key: o.Key, Count: o.Count, Gross: o.Total})
	}
	sorted := make([]ExportCharge, len(charges))
	copy(sorted, charges)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Charge.Number < sorted[j].Charge.Number })
	for _, c := range sorted {
		v.Charges = append(v.Charges, views.ExportChargeRow{
			Number: c.Charge.Number, IssuedOn: c.Charge.IssuedOn, Customer: c.Fiscal.Name, OriginType: originLabels[c.Charge.OriginType],
			Net: moneyOf(c.Charge.NetCents), VAT: moneyOf(c.Charge.VATCents), Total: moneyOf(c.Charge.TotalCents), Corrects: c.CorrectsNumber,
		})
	}
	return v, nil
}

// filtersText describes the filters of an export for the summary (codes only).
func filtersText(e *store.ChargeExport) string {
	f := ExportFiltersOf(e)
	var parts []string
	if len(f.OriginTypes) > 0 {
		labels := make([]string, 0, len(f.OriginTypes))
		for _, o := range f.OriginTypes {
			labels = append(labels, originLabels[o])
		}
		parts = append(parts, strings.Join(labels, ", "))
	}
	if f.RevenueClass != "" {
		parts = append(parts, f.RevenueClass)
	}
	if f.CustomerID != "" {
		parts = append(parts, "1 cliente")
	}
	return strings.Join(parts, " · ")
}

func moneyOf(v int64) money.Cents { return money.Cents(v) }
