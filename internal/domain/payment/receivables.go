package payment

import (
	"bytes"
	"context"
	"encoding/csv"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/xlsx"
)

// Scadenzario and receivables overview (spec 16.4; m5-plan §4.5): open charges with their residual, age bucket
// and last reminder level, plus the scheduled instalments that have no charge yet; ageing per customer.

// ReceivableFilter selects scadenzario rows.
type ReceivableFilter struct {
	DueFrom, DueTo calendar.Date
	Bucket         string
	Status         string // open, overdue, scheduled ("" = open charges and scheduled instalments)
	CustomerID     string
	ContractID     string
	GroupID        string
	OriginType     string
}

// Receivable is a row of the scadenzario.
type Receivable struct {
	store.ReceivableRow
	CustomerName      string
	ContractCode      string
	Bucket            string
	LastReminderLevel string
	Excluded          bool
}

// ReceivablePage is a page of the scadenzario with the totals of the whole selection.
type ReceivablePage struct {
	Items      []Receivable
	NextCursor string
	Totals     Ageing
}

func contractCode(kind string, year int, n int64) string {
	if kind == "" {
		return ""
	}
	return contract.Code(&store.Contract{Kind: kind, Year: year, Number: n})
}

func (s *Service) receivables(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, f ReceivableFilter) ([]Receivable, Ageing, error) {
	today := s.today(ctx, db, scope)
	sf := store.ReceivableFilter{
		DueFrom: f.DueFrom, DueTo: f.DueTo, CustomerID: f.CustomerID, ContractID: f.ContractID, GroupID: f.GroupID,
		OriginType: f.OriginType, Charges: f.Status != "scheduled", Scheduled: f.Status == "" || f.Status == "scheduled",
	}
	if f.Status == "overdue" {
		if sf.DueTo.IsZero() || !sf.DueTo.Before(today) {
			sf.DueTo = today.AddDays(-1)
		}
	}
	if f.Bucket != "" {
		from, to := DueRange(f.Bucket, today)
		if !from.IsZero() && (sf.DueFrom.IsZero() || from.After(sf.DueFrom)) {
			sf.DueFrom = from
		}
		if !to.IsZero() && (sf.DueTo.IsZero() || to.Before(sf.DueTo)) {
			sf.DueTo = to
		}
	}
	for _, id := range []string{f.CustomerID, f.ContractID, f.GroupID} {
		if id != "" && !store.ValidID(id) {
			return []Receivable{}, Ageing{}, nil
		}
	}
	rows, err := store.ReceivableRows(ctx, db, scope, sf)
	if err != nil {
		return nil, Ageing{}, err
	}
	var chargeIDs, custs []string
	for _, r := range rows {
		custs = append(custs, r.CustomerID)
		if r.Kind == "charge" {
			chargeIDs = append(chargeIDs, r.ID)
		}
	}
	names, err := s.customerNames(ctx, db, scope, p, custs)
	if err != nil {
		return nil, Ageing{}, err
	}
	levels, err := store.LastReminderLevels(ctx, db, scope, chargeIDs)
	if err != nil {
		return nil, Ageing{}, err
	}
	excl, err := store.ActiveExclusions(ctx, db, scope, today)
	if err != nil {
		return nil, Ageing{}, err
	}
	exCust, exCharge := map[string]bool{}, map[string]bool{}
	for _, e := range excl {
		if e.CustomerID != "" {
			exCust[e.CustomerID] = true
		}
		if e.ChargeID != "" {
			exCharge[e.ChargeID] = true
		}
	}
	out := make([]Receivable, 0, len(rows))
	var totals Ageing
	for _, r := range rows {
		rv := Receivable{
			ReceivableRow: r, CustomerName: names[r.CustomerID], ContractCode: contractCode(r.ContractKind, r.ContractYear, r.ContractNumber),
			Bucket: BucketOf(r.DueOn, today), LastReminderLevel: levels[r.ID], Excluded: exCust[r.CustomerID] || exCharge[r.ID],
		}
		if f.Bucket != "" && rv.Bucket != f.Bucket {
			continue
		}
		totals.Add(rv.Bucket, money.Cents(r.ResidualCents))
		out = append(out, rv)
	}
	return out, totals, nil
}

// ListReceivables returns one page of the scadenzario ordered by due date (charges.manage).
func (s *Service) ListReceivables(ctx context.Context, p *auth.Principal, marinaID string, f ReceivableFilter, cursor string, limit int) (*ReceivablePage, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	after, err := store.DecodeCursor(cursor)
	if err != nil {
		return nil, err
	}
	all, totals, err := s.receivables(ctx, s.d.DB.DB, scope, p, f)
	if err != nil {
		return nil, err
	}
	start := 0
	if after != "" {
		start = len(all)
		for i, r := range all {
			if r.ID == after {
				start = i + 1
				break
			}
		}
	}
	limit = store.ClampLimit(limit)
	end := min(start+limit, len(all))
	page := &ReceivablePage{Items: all[start:end], Totals: totals}
	if end < len(all) {
		page.NextCursor = store.EncodeCursor(all[end-1].ID)
	}
	return page, nil
}

// AllReceivables returns the whole scadenzario selection (files).
func (s *Service) AllReceivables(ctx context.Context, p *auth.Principal, marinaID string, f ReceivableFilter) ([]Receivable, Ageing, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, Ageing{}, err
	}
	return s.receivables(ctx, s.d.DB.DB, scope, p, f)
}

// AgeingRow is the ageing of one customer.
type AgeingRow struct {
	CustomerID   string
	CustomerName string
	Amounts      Ageing
	Credit       money.Cents
}

// AgeingReport is the receivables overview by customer with the totals.
type AgeingReport struct {
	Rows   []AgeingRow
	Totals Ageing
}

// ReceivablesAgeing returns the receivables overview by customer (charges.manage), customers with residuals only.
func (s *Service) ReceivablesAgeing(ctx context.Context, p *auth.Principal, marinaID string) (*AgeingReport, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	open, err := store.OpenPositiveCharges(ctx, db, scope, store.OpenChargesFilter{})
	if err != nil {
		return nil, err
	}
	by := map[string]*AgeingRow{}
	var ids []string
	rep := &AgeingReport{Rows: []AgeingRow{}}
	for _, c := range open {
		if c.CustomerID == "" {
			continue
		}
		r := by[c.CustomerID]
		if r == nil {
			r = &AgeingRow{CustomerID: c.CustomerID}
			by[c.CustomerID] = r
			ids = append(ids, c.CustomerID)
		}
		b := BucketOf(c.DueOn, today)
		r.Amounts.Add(b, money.Cents(c.ResidualCents()))
		rep.Totals.Add(b, money.Cents(c.ResidualCents()))
	}
	names, err := s.customerNames(ctx, db, scope, p, ids)
	if err != nil {
		return nil, err
	}
	credits, err := store.CreditsOfCustomers(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		r := by[id]
		r.CustomerName, r.Credit = names[id], money.Cents(credits[id].CreditCents)
		rep.Rows = append(rep.Rows, *r)
	}
	slices.SortFunc(rep.Rows, func(a, b AgeingRow) int {
		if a.Amounts.Total != b.Amounts.Total {
			if a.Amounts.Total > b.Amounts.Total {
				return -1
			}
			return 1
		}
		return strings.Compare(a.CustomerID, b.CustomerID)
	})
	return rep, nil
}

// ReceivablesCSV renders the scadenzario as an Italian CSV.
func ReceivablesCSV(rows []Receivable) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = ';'
	_ = w.Write([]string{"Tipo", "Numero", "Cliente", "Contratto", "Origine", "Descrizione", "Scadenza", "Importo", "Residuo", "Fascia", "Ultimo sollecito", "Escluso"})
	for _, r := range rows {
		_ = w.Write([]string{
			r.Kind, r.Number, r.CustomerName, r.ContractCode, r.OriginType, r.Description, r.DueOn.String(),
			euros(money.Cents(r.AmountCents)), euros(money.Cents(r.ResidualCents)), r.Bucket, r.LastReminderLevel, yesNo(r.Excluded),
		})
	}
	w.Flush()
	return b.Bytes()
}

// ReceivablesXLSX renders the scadenzario as an Excel workbook.
func ReceivablesXLSX(rows []Receivable) ([]byte, error) {
	sh := xlsx.Sheet{
		Name:   "Scadenzario",
		Header: []string{"Tipo", "Numero", "Cliente", "Contratto", "Origine", "Descrizione", "Scadenza", "Importo", "Residuo", "Fascia", "Ultimo sollecito", "Escluso"},
		Widths: []int{20, 14, 30, 16, 18, 30, 12, 12, 12, 10, 16, 8},
	}
	for _, r := range rows {
		sh.Rows = append(sh.Rows, []xlsx.Cell{
			xlsx.Str(r.Kind), xlsx.Str(r.Number), xlsx.Str(r.CustomerName), xlsx.Str(r.ContractCode), xlsx.Str(r.OriginType),
			xlsx.Str(r.Description), xlsx.Date(r.DueOn), xlsx.Cents(money.Cents(r.AmountCents)), xlsx.Cents(money.Cents(r.ResidualCents)),
			xlsx.Str(r.Bucket), xlsx.Str(r.LastReminderLevel), xlsx.Str(yesNo(r.Excluded)),
		})
	}
	var b bytes.Buffer
	if err := xlsx.Write(&b, []xlsx.Sheet{sh}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

var ageingHeader = []string{"Cliente", "Non scaduto", "0-30", "31-60", "61-90", "Oltre 90", "Totale", "Credito"}

// AgeingCSV renders the receivables overview as an Italian CSV.
func AgeingCSV(rep *AgeingReport) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = ';'
	_ = w.Write(ageingHeader)
	row := func(name string, a Ageing, credit money.Cents) []string {
		return []string{name, euros(a.NotDue), euros(a.D0To30), euros(a.D31To60), euros(a.D61To90), euros(a.D90Plus), euros(a.Total), euros(credit)}
	}
	for _, r := range rep.Rows {
		_ = w.Write(row(r.CustomerName, r.Amounts, r.Credit))
	}
	_ = w.Write(row("Totale", rep.Totals, 0))
	w.Flush()
	return b.Bytes()
}

// AgeingXLSX renders the receivables overview as an Excel workbook.
func AgeingXLSX(rep *AgeingReport) ([]byte, error) {
	sh := xlsx.Sheet{Name: "Sospesi", Header: ageingHeader, Widths: []int{30, 12, 12, 12, 12, 12, 12, 12}}
	row := func(name string, a Ageing, credit money.Cents) []xlsx.Cell {
		return []xlsx.Cell{
			xlsx.Str(name), xlsx.Cents(a.NotDue), xlsx.Cents(a.D0To30), xlsx.Cents(a.D31To60), xlsx.Cents(a.D61To90),
			xlsx.Cents(a.D90Plus), xlsx.Cents(a.Total), xlsx.Cents(credit),
		}
	}
	for _, r := range rep.Rows {
		sh.Rows = append(sh.Rows, row(r.CustomerName, r.Amounts, r.Credit))
	}
	sh.Rows = append(sh.Rows, row("Totale", rep.Totals, 0))
	var b bytes.Buffer
	if err := xlsx.Write(&b, []xlsx.Sheet{sh}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func yesNo(b bool) string {
	if b {
		return "sì"
	}
	return "no"
}
