package payment

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Statement of account and balances (spec 16.4; m5-plan §4.5). The balance of a customer is Σ residual of their
// open positive charges − their credit. The statement lists the same ledger as dated rows: a charge adds its total
// (a negative charge subtracts it, its offsets are not rows), a payment or credit subtracts its amount, a refund
// adds it back, a deposit only counts for what was retained (its matches). Adjustment credits are not rows: their
// negative charge already is. So the closing balance at today equals the balance
// (TestStatement_ClosingEqualsBalance).

// Statement row kinds.
const (
	RowCharge  = "charge"
	RowPayment = "payment"
	RowRefund  = "refund"
	RowCredit  = "credit"
)

// Entry is a dated movement of a customer's balance (positive raises it).
type Entry struct {
	Date        calendar.Date
	Kind        string
	ID          string
	Number      string
	Description string
	DueOn       calendar.Date
	Amount      money.Cents
}

// StatementRow is a row of a statement with the running balance.
type StatementRow struct {
	Entry
	Debit, Credit, Balance money.Cents
}

// Statement is the statement of account of a customer for [From, To].
type Statement struct {
	CustomerID   string
	CustomerName string
	From, To     calendar.Date
	Opening      money.Cents
	Closing      money.Cents
	CreditCents  money.Cents
	Rows         []StatementRow
}

var kindOrder = map[string]int{RowCharge: 0, RowPayment: 1, RowCredit: 2, RowRefund: 3}

// BuildStatement builds a statement from entries (pure): the opening balance sums the entries before from, the
// rows are the entries in [from, to] by date (charges first on a day, then payments, credits and refunds), each
// with the running balance; the closing balance is the opening plus the rows. A zero from or to is open.
func BuildStatement(entries []Entry, from, to calendar.Date) Statement {
	sorted := slices.Clone(entries)
	slices.SortStableFunc(sorted, func(a, b Entry) int {
		if c := a.Date.Compare(b.Date); c != 0 {
			return c
		}
		if c := kindOrder[a.Kind] - kindOrder[b.Kind]; c != 0 {
			return c
		}
		return strings.Compare(a.Number, b.Number)
	})
	st := Statement{From: from, To: to, Rows: []StatementRow{}}
	for _, e := range sorted {
		if !from.IsZero() && e.Date.Before(from) {
			st.Opening += e.Amount
		}
	}
	bal := st.Opening
	for _, e := range sorted {
		if (!from.IsZero() && e.Date.Before(from)) || (!to.IsZero() && e.Date.After(to)) {
			continue
		}
		bal += e.Amount
		r := StatementRow{Entry: e, Balance: bal}
		if e.Amount >= 0 {
			r.Debit = e.Amount
		} else {
			r.Credit = -e.Amount
		}
		st.Rows = append(st.Rows, r)
	}
	st.Closing = bal
	return st
}

// ledgerEntries reads the movements of a customer's balance.
func (s *Service) ledgerEntries(ctx context.Context, db bun.IDB, scope store.MarinaScope, customerID string) ([]Entry, error) {
	var out []Entry
	charges, err := store.CustomerCharges(ctx, db, scope, customerID)
	if err != nil {
		return nil, err
	}
	for _, c := range charges {
		out = append(out, Entry{Date: c.IssuedOn, Kind: RowCharge, ID: c.ID, Number: c.Number, Description: c.Description, DueOn: c.DueOn, Amount: money.Cents(c.TotalCents)})
	}
	pays, err := store.AllPayments(ctx, db, scope, store.PaymentFilter{CustomerID: customerID})
	if err != nil {
		return nil, err
	}
	counted := map[string]bool{}
	for _, p := range pays {
		switch {
		case p.Kind == store.PaymentKindPayment:
			out = append(out, Entry{Date: p.ReceivedOn, Kind: RowPayment, ID: p.ID, Number: p.Number, Description: p.Method, Amount: -money.Cents(p.AmountCents)})
			counted[p.ID] = true
		case p.Kind == store.PaymentKindCredit && p.AdjustmentChargeID == "":
			out = append(out, Entry{Date: p.ReceivedOn, Kind: RowCredit, ID: p.ID, Number: p.Number, Description: p.Source, Amount: -money.Cents(p.AmountCents)})
			counted[p.ID] = true
		case p.Kind == store.PaymentKindCredit:
			// Credit of a negative charge (adjustment or negative opening balance): the charge is the row, its
			// refunds still count (INV-50: the statement closes at the customer balance).
			counted[p.ID] = true
		}
	}
	refunds, err := store.AllRefunds(ctx, db, scope, store.RefundFilter{CustomerID: customerID, Kind: store.RefundKindRefund})
	if err != nil {
		return nil, err
	}
	for _, r := range refunds {
		if r.Status == store.RefundFailed || !counted[r.PaymentID] {
			continue
		}
		out = append(out, Entry{Date: r.BusinessDate, Kind: RowRefund, ID: r.ID, Number: r.Number, Description: r.Reason, Amount: money.Cents(r.AmountCents)})
	}
	deps, err := store.DepositMatchesOfCustomer(ctx, db, scope, customerID)
	if err != nil {
		return nil, err
	}
	loc := location(ctx, db, scope)
	for _, m := range deps {
		out = append(out, Entry{Date: calendar.In(m.CreatedAt, loc), Kind: RowPayment, ID: m.MatchID, Number: m.PaymentNumber, Description: "deposit", Amount: -money.Cents(m.AmountCents)})
	}
	return out, nil
}

// BalanceTx returns the balance of a customer (Σ residual of open positive charges − credit) and the credit.
func BalanceTx(ctx context.Context, db bun.IDB, scope store.MarinaScope, customerID string) (balance, credit money.Cents, err error) {
	open, err := store.OpenPositiveCharges(ctx, db, scope, store.OpenChargesFilter{CustomerID: customerID})
	if err != nil {
		return 0, 0, err
	}
	for _, c := range open {
		balance += money.Cents(c.ResidualCents())
	}
	t, err := store.CreditOf(ctx, db, scope, customerID)
	if err != nil {
		return 0, 0, err
	}
	return balance - money.Cents(t.CreditCents), money.Cents(t.CreditCents), nil
}

// Balance returns the balance and credit of a customer (charges.manage).
func (s *Service) Balance(ctx context.Context, p *auth.Principal, marinaID, customerID string) (balance, credit money.Cents, err error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return 0, 0, err
	}
	return BalanceTx(ctx, s.d.DB.DB, scope, customerID)
}

// CustomerStatement returns the statement of account of a customer for [from, to] (charges.manage; to defaults to
// today, from to the first day of the year of to).
func (s *Service) CustomerStatement(ctx context.Context, p *auth.Principal, marinaID, customerID string, from, to calendar.Date) (*Statement, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	return s.statement(ctx, s.d.DB.DB, scope, p, customerID, from, to)
}

func (s *Service) statement(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, customerID string, from, to calendar.Date) (*Statement, error) {
	names, err := s.customerNames(ctx, db, scope, p, []string{customerID})
	if err != nil {
		return nil, err
	}
	if _, ok := names[customerID]; !ok {
		return nil, ErrNotFound
	}
	if to.IsZero() {
		to = s.today(ctx, db, scope)
	}
	if from.IsZero() {
		from = calendar.MustParse(to.String()[:4] + "-01-01")
	}
	if to.Before(from) {
		return nil, invalid("to", "before_from")
	}
	entries, err := s.ledgerEntries(ctx, db, scope, customerID)
	if err != nil {
		return nil, err
	}
	st := BuildStatement(entries, from, to)
	st.CustomerID, st.CustomerName = customerID, names[customerID]
	t, err := store.CreditOf(ctx, db, scope, customerID)
	if err != nil {
		return nil, err
	}
	st.CreditCents = money.Cents(t.CreditCents)
	return &st, nil
}

// StatementCSV renders a statement as an Italian CSV (semicolon separated, amounts with a decimal comma).
func StatementCSV(st *Statement) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = ';'
	_ = w.Write([]string{"Data", "Tipo", "Numero", "Descrizione", "Scadenza", "Dare", "Avere", "Saldo"})
	_ = w.Write([]string{st.From.String(), "saldo_iniziale", "", "", "", "", "", euros(st.Opening)})
	for _, r := range st.Rows {
		due := ""
		if !r.DueOn.IsZero() {
			due = r.DueOn.String()
		}
		_ = w.Write([]string{r.Date.String(), r.Kind, r.Number, r.Description, due, euros(r.Debit), euros(r.Credit), euros(r.Balance)})
	}
	_ = w.Write([]string{st.To.String(), "saldo_finale", "", "", "", "", "", euros(st.Closing)})
	w.Flush()
	return b.Bytes()
}

// euros formats cents as an Italian decimal (1234,56).
func euros(c money.Cents) string { return strings.Replace(c.String(), ".", ",", 1) }

// StatementPDF renders the statement with the docs kind statement_of_account (ephemeral, m5-plan §8.3) in the
// customer's locale.
func (s *Service) StatementPDF(ctx context.Context, p *auth.Principal, marinaID, customerID string, from, to calendar.Date) ([]byte, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if s.d.Docs == nil {
		return nil, errors.New("payment: docs service not wired")
	}
	db := s.d.DB.DB
	st, err := s.statement(ctx, db, scope, p, customerID, from, to)
	if err != nil {
		return nil, err
	}
	mv, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys)
	if err != nil {
		return nil, err
	}
	c, err := store.GetCustomer(ctx, db, scope, customerID)
	if err != nil {
		return nil, err
	}
	return s.d.Docs.RenderEphemeral(ctx, scope, docs.KindStatementOfAccount, c.Locale, StatementViewOf(st, mv, docs.PartyOf(c)))
}

// StatementViewOf converts a statement to the frozen document view.
func StatementViewOf(st *Statement, mv views.Marina, party views.Party) views.StatementView {
	v := views.StatementView{
		Marina: mv, Customer: party, From: st.From, To: st.To, Opening: st.Opening, Closing: st.Closing, Credit: st.CreditCents,
		Rows: make([]views.StatementRow, 0, len(st.Rows)),
	}
	for _, r := range st.Rows {
		v.Rows = append(v.Rows, views.StatementRow{
			Date: r.Date, Kind: r.Kind, Number: r.Number, Description: r.Description, DueOn: r.DueOn,
			Debit: r.Debit, Credit: r.Credit, Balance: r.Balance,
		})
	}
	return v
}
