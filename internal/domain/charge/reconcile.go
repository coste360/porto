package charge

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Invoice reconciliation (m5-plan §3.6): the invoicing software sends back, per charge, the invoice number and
// date; invoiced is information only and blocks nothing.

// MaxReconcileRows bounds a reconciliation file.
const MaxReconcileRows = 20000

// Reconciliation outcomes.
const (
	OutcomeOK            = "ok"
	OutcomeUnknownCharge = "unknown_charge"
	OutcomeNotExported   = "not_exported"
	OutcomeCancelled     = "cancelled"
	OutcomeInvalidDate   = "invalid_date"
	OutcomeDuplicateRow  = "duplicate_row"
)

// ReconcileRow is one input row: a charge id or number, the invoice number and its date (text as received).
type ReconcileRow struct {
	Row           int
	ChargeID      string
	ChargeNumber  string
	InvoiceNumber string
	InvoiceDate   string
}

// ReportRow is one row of a reconciliation report (charge numbers only).
type ReportRow struct {
	Row          int    `json:"row"`
	ChargeNumber string `json:"charge_number,omitempty"`
	Outcome      string `json:"outcome"`
}

// ErrCSVInvalid is returned for a reconciliation file without the expected columns (422 csv_invalid).
var ErrCSVInvalid = csvimport.ErrCSVInvalid

// ParseReconcileCSV parses a reconciliation CSV: optional BOM, separator ';', ',' or tab (detected from the
// header), a header with "id addebito" or "numero addebito", "numero fattura" and "data" (case and spacing free);
// blank lines are skipped. Rows are numbered from 1 after the header.
func ParseReconcileCSV(text string) ([]ReconcileRow, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	first, _, _ := strings.Cut(text, "\n")
	sep := ';'
	switch {
	case strings.Count(first, ";") > 0:
		sep = ';'
	case strings.Count(first, "\t") > 0:
		sep = '\t'
	case strings.Count(first, ",") > 0:
		sep = ','
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return nil, ErrCSVInvalid
	}
	col := map[string]int{}
	for i, h := range header {
		k := strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(h), "_", " "))), " ")
		col[k] = i
	}
	idCol, hasID := col["id addebito"]
	numCol, hasNum := col["numero addebito"]
	invCol, hasInv := col["numero fattura"]
	dateCol, hasDate := col["data"]
	if !hasDate {
		dateCol, hasDate = col["data fattura"]
	}
	if (!hasID && !hasNum) || !hasInv || !hasDate {
		return nil, ErrCSVInvalid
	}
	get := func(rec []string, i int, ok bool) string {
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var out []ReconcileRow
	n := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrCSVInvalid
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		n++
		if n > MaxReconcileRows {
			return nil, ErrCSVInvalid
		}
		out = append(out, ReconcileRow{
			Row: n, ChargeID: get(rec, idCol, hasID), ChargeNumber: strings.ToUpper(get(rec, numCol, hasNum)),
			InvoiceNumber: get(rec, invCol, hasInv), InvoiceDate: get(rec, dateCol, hasDate),
		})
	}
	return out, nil
}

// ParseInvoiceDate reads GG/MM/AAAA (also G/M/AAAA and with '-' or '.') or YYYY-MM-DD.
func ParseInvoiceDate(s string) (calendar.Date, bool) {
	s = strings.TrimSpace(s)
	if d, err := calendar.Parse(s); err == nil {
		return d, true
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '-' || r == '.' })
	if len(parts) != 3 || len(parts[2]) != 4 {
		return calendar.Date{}, false
	}
	pad := func(p string) string {
		if len(p) == 1 {
			return "0" + p
		}
		return p
	}
	d, err := calendar.Parse(parts[2] + "-" + pad(parts[1]) + "-" + pad(parts[0]))
	return d, err == nil
}

// ReconcileInput is a reconciliation: a CSV text or rows, dry run or apply.
type ReconcileInput struct {
	CSV    string
	Rows   []ReconcileRow
	DryRun bool
}

// Reconcile applies (or simulates) invoice references from the invoicing software (charges.export): each charge
// at most once, only exported or invoiced charges, stored with its report.
func (s *Service) Reconcile(ctx context.Context, p *auth.Principal, marinaID string, in ReconcileInput) (*store.InvoiceReconciliation, []ReportRow, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, nil, err
	}
	rows := in.Rows
	source, sha := "manual", ""
	if strings.TrimSpace(in.CSV) != "" {
		if rows, err = ParseReconcileCSV(in.CSV); err != nil {
			return nil, nil, err
		}
		sum := sha256.Sum256([]byte(in.CSV))
		source, sha = "csv", hex.EncodeToString(sum[:])
	}
	if len(rows) == 0 {
		return nil, nil, ErrCSVInvalid
	}
	if len(rows) > MaxReconcileRows {
		return nil, nil, ErrCSVInvalid
	}
	for i := range rows {
		if rows[i].Row == 0 {
			rows[i].Row = i + 1
		}
	}
	var rec *store.InvoiceReconciliation
	var report []ReportRow
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		rec, report, err = s.reconcileTx(ctx, tx, scope, p.Actor(), source, sha, "", rows, in.DryRun)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return rec, report, nil
}

// reconcileTx resolves the rows, applies the ok ones (unless dry run) and stores the report.
func (s *Service) reconcileTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, source, sha, exportID string,
	rows []ReconcileRow, dryRun bool,
) (*store.InvoiceReconciliation, []ReportRow, error) {
	var ids, numbers []string
	for _, r := range rows {
		if r.ChargeID != "" {
			ids = append(ids, r.ChargeID)
		} else if r.ChargeNumber != "" {
			numbers = append(numbers, r.ChargeNumber)
		}
	}
	byID := map[string]store.Charge{}
	byNumber := map[string]store.Charge{}
	byIDs, err := store.ChargesByIDs(ctx, tx, scope, ids)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range byIDs {
		byID[c.ID] = c
	}
	byNumbers, err := store.ChargesByNumbers(ctx, tx, scope, numbers)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range byNumbers {
		byNumber[c.Number] = c
	}
	seen := map[string]bool{}
	report := make([]ReportRow, 0, len(rows))
	matched := 0
	now := s.now()
	for _, r := range rows {
		c, ok := byID[r.ChargeID]
		if r.ChargeID == "" {
			c, ok = byNumber[r.ChargeNumber]
		}
		rr := ReportRow{Row: r.Row, ChargeNumber: c.Number}
		date, dateOK := ParseInvoiceDate(r.InvoiceDate)
		num := strings.TrimSpace(r.InvoiceNumber)
		switch {
		case !ok:
			rr.Outcome = OutcomeUnknownCharge
		case seen[c.ID]:
			rr.Outcome = OutcomeDuplicateRow
		case c.PaymentStatus == store.ChargeCancelled:
			rr.Outcome = OutcomeCancelled
		case c.InvoicingStatus == store.ChargeToInvoice:
			rr.Outcome = OutcomeNotExported
		case !dateOK || num == "" || len(num) > 64:
			rr.Outcome = OutcomeInvalidDate
		default:
			rr.Outcome = OutcomeOK
		}
		if ok {
			seen[c.ID] = true
		}
		if rr.Outcome == OutcomeOK {
			matched++
			same := c.InvoicingStatus == store.ChargeInvoiced && c.ExternalInvoiceNumber == num && c.ExternalInvoiceDate.Equal(date)
			if !dryRun && !same {
				done, err := store.SetChargeInvoice(ctx, tx, scope, c.ID, num, date, now)
				if err != nil {
					return nil, nil, mapStore(err)
				}
				if !done {
					rr.Outcome = OutcomeNotExported
					matched--
				} else if err := s.emit(ctx, tx, scope, event.ChargeInvoiced, event.ChargeInvoicedData{ChargeID: c.ID, InvoiceDate: date, Source: source}); err != nil {
					return nil, nil, err
				}
			}
		}
		report = append(report, rr)
	}
	rec := &store.InvoiceReconciliation{
		ID: store.NewID(), MarinaID: scope.ID(), Source: source, ExportID: exportID, FileSHA256: sha, DryRun: dryRun,
		RowCount: len(rows), MatchedCount: matched, Report: store.MustJSON(report), CreatedAt: now, UpdatedAt: now,
	}
	if actor.Type == audit.ActorUser {
		rec.CreatedByUserID = actor.ID
	}
	if err := store.InsertInvoiceReconciliation(ctx, tx, rec); err != nil {
		return nil, nil, err
	}
	if !dryRun {
		if err := s.write(ctx, tx, scope, actor, "invoice_reconciliation.apply", "invoice_reconciliation", rec.ID, map[string]any{
			"source": source, "rows": len(rows), "matched": matched,
		}); err != nil {
			return nil, nil, err
		}
	}
	return rec, report, nil
}

// ListReconciliations returns a page of reconciliations (charges.export).
func (s *Service) ListReconciliations(ctx context.Context, p *auth.Principal, marinaID, cursor string, limit int) (store.Page[store.InvoiceReconciliation], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return store.Page[store.InvoiceReconciliation]{}, err
	}
	return store.ListInvoiceReconciliations(ctx, s.d.DB.DB, scope, cursor, limit)
}

// ReportOf decodes the report of a reconciliation.
func ReportOf(r *store.InvoiceReconciliation) []ReportRow {
	var out []ReportRow
	_ = json.Unmarshal(r.Report, &out)
	if out == nil {
		out = []ReportRow{}
	}
	return out
}
