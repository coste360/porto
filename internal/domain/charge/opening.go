package charge

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Opening balances (spec 11.6; m5-plan §3.8): the residual balances of the previous software, imported once from
// CSV or Excel as opening_balance charges born invoiced (they were invoiced there, so they never enter the "da
// fatturare" export, INV-53). A negative balance is customer credit.

// OpeningMapping is the column mapping of an opening balance file (0-based; -1 = absent).
type OpeningMapping struct {
	CustomerMatch     string `json:"customer_match"` // fiscal_code, vat_number, customer_code (the customer id)
	CustomerColumn    int    `json:"customer_column"`
	AmountColumn      int    `json:"amount_column"`
	DueOnColumn       int    `json:"due_on_column"`
	DescriptionColumn int    `json:"description_column"`
	ReferenceColumn   int    `json:"reference_column"`
}

// OpeningInput is an opening balance import.
type OpeningInput struct {
	Format string // csv, xlsx
	File   []byte
	// FileBase64 is the file as received (decoded after authorisation when File is nil).
	FileBase64 string
	Delimiter  string // semicolon, comma, tab (CSV)
	HasHeader  bool
	Mapping    OpeningMapping
	DryRun     bool
}

// OpeningIssue is a rejected row (row numbers and codes only).
type OpeningIssue struct {
	Row  int    `json:"row"`
	Code string `json:"code"` // unknown_customer, ambiguous_customer, invalid_amount, invalid_date, zero_amount
}

// OpeningReport is the stored report of an import.
type OpeningReport struct {
	ValidRows  int            `json:"valid_rows"`
	TotalCents money.Cents    `json:"total_cents"`
	Issues     []OpeningIssue `json:"issues"`
}

// ReportOfImport decodes the report of an import run.
func ReportOfImport(r *store.ChargeImport) OpeningReport {
	var out OpeningReport
	_ = json.Unmarshal(r.Report, &out)
	if out.Issues == nil {
		out.Issues = []OpeningIssue{}
	}
	return out
}

// DefaultOpeningReference is the external invoice reference of a balance without one.
const DefaultOpeningReference = "SALDO-APERTURA"

// ParseEuros reads an amount in euros with a decimal comma or point and optional thousands separators
// ("1.234,56", "1234.56", "-12,5", "€ 10"); spreadsheet numbers in exponent form are accepted. The result is
// rounded half-even to cents with exact rational arithmetic (no float).
func ParseEuros(s string) (money.Cents, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "€"))
	s = strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "\u00a0", "")
	if s == "" {
		return 0, false
	}
	comma, dot := strings.LastIndex(s, ","), strings.LastIndex(s, ".")
	switch {
	case comma >= 0 && dot >= 0 && comma > dot: // 1.234,56
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	case comma >= 0 && dot >= 0: // 1,234.56
		s = strings.ReplaceAll(s, ",", "")
	case comma >= 0:
		if strings.Count(s, ",") > 1 {
			return 0, false
		}
		s = strings.Replace(s, ",", ".", 1)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, false
	}
	r.Mul(r, big.NewRat(100, 1))
	num, den := new(big.Int).Set(r.Num()), new(big.Int).Set(r.Denom())
	q, m := new(big.Int).QuoRem(num, den, new(big.Int))
	// half-even on the remainder
	twice := new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2))
	if c := twice.Cmp(den); c > 0 || (c == 0 && q.Bit(0) == 1) {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() {
		return 0, false
	}
	return money.Cents(q.Int64()), true
}

// opening row after validation.
type openingRow struct {
	line        int
	customerID  string
	amount      money.Cents
	dueOn       calendar.Date
	description string
	reference   string
}

// ImportOpeningBalances runs an opening balance import (charges.manage): dry run (report only, stored as a
// dry_run row) or apply (one transaction: a charge per valid row; invalid rows are reported and skipped).
func (s *Service) ImportOpeningBalances(ctx context.Context, p *auth.Principal, marinaID string, in OpeningInput) (*store.ChargeImport, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if in.File == nil {
		if in.File, err = DecodeBase64File(in.FileBase64); err != nil {
			return nil, err
		}
	}
	table, err := s.openingTable(in)
	if err != nil {
		return nil, err
	}
	m := in.Mapping
	if m.CustomerMatch != "fiscal_code" && m.CustomerMatch != "vat_number" && m.CustomerMatch != "customer_code" {
		return nil, invalid("mapping.customer_match", "invalid")
	}
	sum := sha256.Sum256(in.File)
	var out *store.ChargeImport
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		rows, report, err := s.openingRows(ctx, tx, scope, table, m)
		if err != nil {
			return err
		}
		now := s.now()
		rec := &store.ChargeImport{
			ID: store.NewID(), MarinaID: scope.ID(), Status: "dry_run", FileSHA256: hex.EncodeToString(sum[:]), Format: in.Format,
			Mapping: store.MustJSON(m), Report: store.MustJSON(report), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if a := p.Actor(); a.Type == "user" {
			rec.CreatedByUserID = a.ID
		}
		if in.DryRun {
			out = rec
			if err := store.InsertChargeImport(ctx, tx, rec); err != nil {
				return err
			}
			return s.write(ctx, tx, scope, p.Actor(), "opening_balance.dry_run", "charge_import", rec.ID, map[string]any{
				"valid_rows": report.ValidRows, "issues": len(report.Issues),
			})
		}
		rec.Status = "applied"
		if err := store.InsertChargeImport(ctx, tx, rec); err != nil {
			return err
		}
		itemID, err := store.OriginItem(ctx, tx, scope, store.OriginItemOpeningBalance)
		if err != nil {
			return err
		}
		if itemID == "" && len(rows) > 0 {
			return ErrBillingItemMissing
		}
		today := s.today(ctx, tx, scope)
		n := 0
		for _, r := range rows {
			line, err := s.itemLine(ctx, tx, scope, itemID, r.amount, calendar.Date{}, calendar.Date{})
			if err != nil {
				return err
			}
			if r.description != "" {
				line.Description = r.description
			}
			ref := r.reference
			if ref == "" {
				ref = DefaultOpeningReference
			}
			desc := s.describe(ctx, tx, scope, "opening_balance", ref, "")
			parts := chargeplan.All([]chargeplan.Remainder{{Line: line, Gross: line.Gross, VAT: line.VAT}})
			ch, _, err := s.CreateTx(ctx, tx, scope, p.Actor(), CreateInput{
				OriginType: store.ChargeOriginOpeningBalance, OriginID: rec.ID, CustomerID: r.customerID, IssuedOn: today,
				DueOn: r.dueOn, Description: desc, Lines: linesFromParts(parts), Invoiced: true, InvoiceNumber: ref, InvoiceDate: today,
			})
			if err != nil {
				return err
			}
			if ch != nil {
				n++
			}
		}
		rec.ChargeCount = n
		if _, err := tx.NewUpdate().Model(rec).Column("charge_count").Where("id = ?", rec.ID).Exec(ctx); err != nil {
			return err
		}
		out = rec
		if err := s.write(ctx, tx, scope, p.Actor(), "opening_balance.apply", "charge_import", rec.ID, map[string]any{
			"charges": n, "total_cents": int64(report.TotalCents), "issues": len(report.Issues),
		}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "import", ID: rec.ID, Change: "applied"})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) openingTable(in OpeningInput) (*csvimport.Table, error) {
	switch in.Format {
	case "xlsx":
		return csvimport.ParseXLSX(in.File, in.HasHeader)
	case "csv":
		var delim rune
		if in.Delimiter != "" {
			d, ok := csvimport.Delimiters[in.Delimiter]
			if !ok {
				return nil, invalid("delimiter", "invalid")
			}
			delim = d
		}
		return csvimport.Parse(string(in.File), delim, in.HasHeader)
	}
	return nil, invalid("format", "invalid")
}

// openingRows validates the rows of a file: unique customer match, non-zero amount, valid due date.
func (s *Service) openingRows(ctx context.Context, tx bun.IDB, scope store.MarinaScope, t *csvimport.Table, m OpeningMapping) ([]openingRow, OpeningReport, error) {
	rep := OpeningReport{Issues: []OpeningIssue{}}
	cell := func(rec []string, i int) string {
		if i < 0 || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var keys []string
	for _, rec := range t.Rows {
		keys = append(keys, cell(rec, m.CustomerColumn))
	}
	byKey, err := store.CustomerKeys(ctx, tx, scope, m.CustomerMatch, keys)
	if err != nil {
		return nil, rep, err
	}
	first := 1
	if t.HasHeader {
		first = 2
	}
	var out []openingRow
	for i, rec := range t.Rows {
		line := first + i
		key := cell(rec, m.CustomerColumn)
		if m.CustomerMatch != "customer_code" {
			key = strings.ToUpper(key)
		}
		ids := byKey[key]
		amount, okAmount := ParseEuros(cell(rec, m.AmountColumn))
		due, okDue := ParseInvoiceDate(cell(rec, m.DueOnColumn))
		switch {
		case len(ids) == 0:
			rep.Issues = append(rep.Issues, OpeningIssue{Row: line, Code: "unknown_customer"})
		case len(ids) > 1:
			rep.Issues = append(rep.Issues, OpeningIssue{Row: line, Code: "ambiguous_customer"})
		case !okAmount:
			rep.Issues = append(rep.Issues, OpeningIssue{Row: line, Code: "invalid_amount"})
		case amount == 0:
			rep.Issues = append(rep.Issues, OpeningIssue{Row: line, Code: "zero_amount"})
		case !okDue:
			rep.Issues = append(rep.Issues, OpeningIssue{Row: line, Code: "invalid_date"})
		default:
			r := openingRow{line: line, customerID: ids[0], amount: amount, dueOn: due}
			if m.DescriptionColumn >= 0 {
				r.description = truncate(cell(rec, m.DescriptionColumn), 300)
			}
			if m.ReferenceColumn >= 0 {
				r.reference = truncate(cell(rec, m.ReferenceColumn), 64)
			}
			out = append(out, r)
			rep.ValidRows++
			rep.TotalCents += amount
		}
	}
	return out, rep, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// UndoOpeningBalances cancels the charges of an applied import while none of them has matches or offsets and
// none is a credit (409 import_not_undoable otherwise; charges.manage, If-Match).
func (s *Service) UndoOpeningBalances(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*store.ChargeImport, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	var out *store.ChargeImport
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		rec, err := store.LockChargeImport(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if rec.Version != ifMatch {
			return ErrVersionMismatch
		}
		if rec.Status != "applied" {
			return ErrImportNotUndoable
		}
		charges, err := store.ChargesOfImport(ctx, tx, scope, rec.ID)
		if err != nil {
			return err
		}
		ids := chargeIDs(charges)
		if _, err := store.LockChargesSorted(ctx, tx, scope, ids); err != nil {
			return err
		}
		for _, c := range charges {
			if c.PaymentStatus == store.ChargeCancelled {
				continue
			}
			if c.TotalCents < 0 || c.PaidAmountCents != 0 || c.CreditedCents != 0 {
				return ErrImportNotUndoable
			}
			if matched, err := store.HasMatches(ctx, tx, scope, c.ID); err != nil {
				return err
			} else if matched {
				return ErrImportNotUndoable
			}
			ok, err := store.CancelImportedCharge(ctx, tx, scope, c.ID, "opening_balance_undo", s.now())
			if err != nil {
				return mapStore(err)
			}
			if !ok {
				return ErrImportNotUndoable
			}
			if err := s.emit(ctx, tx, scope, event.ChargeCancelled, event.ChargeCancelledData{ChargeID: c.ID, OriginType: c.OriginType}); err != nil {
				return err
			}
		}
		ok, err := store.MarkChargeImportUndone(ctx, tx, scope, rec.ID, ifMatch, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrVersionMismatch
		}
		rec.Status, rec.Version = "undone", rec.Version+1
		out = rec
		if err := s.write(ctx, tx, scope, p.Actor(), "opening_balance.undo", "charge_import", rec.ID, map[string]any{"charges": len(charges)}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "import", ID: rec.ID, Change: "undone"})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListOpeningImports returns a page of opening balance imports (charges.manage).
func (s *Service) ListOpeningImports(ctx context.Context, p *auth.Principal, marinaID, cursor string, limit int) (store.Page[store.ChargeImport], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return store.Page[store.ChargeImport]{}, err
	}
	return store.ListChargeImports(ctx, s.d.DB.DB, scope, cursor, limit)
}

// DecodeBase64File decodes the file of an import (422 invalid_file when not base64).
func DecodeBase64File(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, invalid("file_base64", "invalid")
	}
	return b, nil
}
