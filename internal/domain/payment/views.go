package payment

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/store"
)

// Read side of payments and refunds (payments.collect or charges.manage). Customer names only with
// customers.read (PII gating as M3); the staff reference is never returned.

// MatchView is a match with its charge number and whether it was wholly reversed (partial reversals leave it
// reversible for the rest, see ReversedCents).
type MatchView struct {
	store.PaymentMatch
	ChargeNumber string
	Reversed     bool
}

// Detail is a payment with its matches, refunds and fiscal documents.
type Detail struct {
	store.Payment
	CustomerName    string
	HasReference    bool
	Split           []Bucket
	Matches         []MatchView
	Refunds         []store.Refund
	FiscalDocuments []store.FiscalDocument
}

// customerNames returns the display names of existing customers (keys present for every existing customer; the
// value is empty without customers.read).
func (s *Service) customerNames(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, ids []string) (map[string]string, error) {
	out := map[string]string{}
	var want []string
	for _, id := range ids {
		if id != "" && store.ValidID(id) {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return out, nil
	}
	cs, err := store.CustomersByIDs(ctx, db, scope, want)
	if err != nil {
		return nil, err
	}
	read := can(p, scope, auth.PermCustomersRead)
	for i := range cs {
		out[cs[i].ID] = ""
		if read {
			out[cs[i].ID] = customer.DisplayName(&cs[i])
		}
	}
	return out, nil
}

// details builds the views of payments; full adds matches, refunds and fiscal documents.
func (s *Service) details(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, pays []store.Payment, full bool) ([]Detail, error) {
	out := make([]Detail, 0, len(pays))
	if len(pays) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(pays))
	custs := make([]string, 0, len(pays))
	for _, pay := range pays {
		ids = append(ids, pay.ID)
		custs = append(custs, pay.CustomerID)
	}
	names, err := s.customerNames(ctx, db, scope, p, custs)
	if err != nil {
		return nil, err
	}
	matches := map[string][]MatchView{}
	refunds := map[string][]store.Refund{}
	docs := map[string][]store.FiscalDocument{}
	if full {
		ms, err := store.MatchesOfPayments(ctx, db, scope, ids)
		if err != nil {
			return nil, err
		}
		chargeIDs := make([]string, 0, len(ms))
		for _, m := range ms {
			chargeIDs = append(chargeIDs, m.ChargeID)
		}
		numbers, err := store.LedgerChargeNumbers(ctx, db, scope, chargeIDs)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			matches[m.PaymentID] = append(matches[m.PaymentID], MatchView{PaymentMatch: m, ChargeNumber: numbers[m.ChargeID], Reversed: m.AmountCents > 0 && m.ReversedCents >= m.AmountCents})
		}
		rs, err := store.RefundsOfPayments(ctx, db, scope, ids)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			refunds[r.PaymentID] = append(refunds[r.PaymentID], r)
		}
		fd, err := store.LedgerFiscalDocuments(ctx, db, scope, ids)
		if err != nil {
			return nil, err
		}
		for _, d := range fd {
			docs[d.PaymentID] = append(docs[d.PaymentID], d)
		}
	}
	for _, pay := range pays {
		d := Detail{
			Payment: pay, CustomerName: names[pay.CustomerID], HasReference: pay.ReferenceEnc != "", Split: decodeSplit(pay.VATSplit),
			Matches: matches[pay.ID], Refunds: refunds[pay.ID], FiscalDocuments: docs[pay.ID],
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *Service) detail(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, id string) (*Detail, error) {
	pay, err := store.GetPayment(ctx, db, scope, id)
	if err != nil {
		return nil, err
	}
	ds, err := s.details(ctx, db, scope, p, []store.Payment{*pay}, true)
	if err != nil {
		return nil, err
	}
	return &ds[0], nil
}

// Get returns a payment with its matches, refunds and fiscal documents (payments.collect or charges.manage).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, id string) (*Detail, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermPaymentsCollect, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// List returns one page of payments, newest first (payments.collect or charges.manage).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f store.PaymentFilter, cursor string, limit int) (store.Page[Detail], error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermPaymentsCollect, auth.PermChargesManage)
	if err != nil {
		return store.Page[Detail]{}, err
	}
	page, err := store.ListPayments(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[Detail]{}, err
	}
	ds, err := s.details(ctx, s.d.DB.DB, scope, p, page.Items, false)
	if err != nil {
		return store.Page[Detail]{}, err
	}
	return store.Page[Detail]{Items: ds, NextCursor: page.NextCursor}, nil
}

// ListRefunds returns one page of refunds and payouts, newest first (payments.collect or charges.manage).
func (s *Service) ListRefunds(ctx context.Context, p *auth.Principal, marinaID string, f store.RefundFilter, cursor string, limit int) (store.Page[store.Refund], error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermPaymentsCollect, auth.PermChargesManage)
	if err != nil {
		return store.Page[store.Refund]{}, err
	}
	return store.ListRefunds(ctx, s.d.DB.DB, scope, f, cursor, limit)
}

// Store row types the handlers and views use through this package (the HTTP layer never imports the store).
type (
	// Filter selects payments.
	Filter = store.PaymentFilter
	// RefundFilter selects refunds.
	RefundFilter = store.RefundFilter
	// Refund is a refund or payout row.
	Refund = store.Refund
	// FiscalDocument is a fiscal document row of a payment.
	FiscalDocument = store.FiscalDocument
	// Charge is a charge row.
	Charge = store.Charge
)
