package yardquote

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/store"
)

// Customer magic links of yard quotes (m10-plan §4.3, §13, INV-75): access_links.purpose = quote with
// subject_type = yard_quote. Every unknown, expired, revoked or foreign token, and a link of another subject, is
// magiclink.ErrInvalid (404 link_invalid). The customer's actions are audited as the system.

// IsYardQuoteLink reports whether token is a live quote link of a yard quote (the public handler dispatches on it).
func (s *Service) IsYardQuoteLink(ctx context.Context, slug, token string) bool {
	if s.d.Links == nil {
		return false
	}
	l, err := s.d.Links.ResolvePurpose(ctx, slug, token, magiclink.PurposeQuote)
	return err == nil && l.SubjectType == string(docs.SubjectYardQuote)
}

// resolveLink resolves a yard quote link and the marina scope of its quote.
func (s *Service) resolveLink(ctx context.Context, slug, token string) (*magiclink.Link, store.MarinaScope, error) {
	if s.d.Links == nil {
		return nil, store.MarinaScope{}, magiclink.ErrInvalid
	}
	l, err := s.d.Links.ResolvePurpose(ctx, slug, token, magiclink.PurposeQuote)
	if err != nil || l.SubjectType != string(docs.SubjectYardQuote) {
		return nil, store.MarinaScope{}, magiclink.ErrInvalid
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, l.MarinaID)
	if err != nil {
		return nil, store.MarinaScope{}, magiclink.ErrInvalid
	}
	return l, scope, nil
}

// linkQuote reads (or locks) the quote of a link; it must belong to the link's customer.
func linkQuote(ctx context.Context, db bun.IDB, scope store.MarinaScope, l *magiclink.Link, lock bool) (*store.YardQuote, error) {
	r, err := store.GetYardQuote(ctx, db, scope, l.SubjectID, lock)
	if err != nil || r.CustomerID != l.CustomerID {
		return nil, magiclink.ErrInvalid
	}
	return r, nil
}

// PublicLinkQuote returns the customer-facing quote of a link (no internal costs, no formulas).
func (s *Service) PublicLinkQuote(ctx context.Context, slug, token string) (*PublicQuote, *magiclink.Link, error) {
	l, scope, err := s.resolveLink(ctx, slug, token)
	if err != nil {
		return nil, nil, err
	}
	db := s.d.DB.DB
	r, err := linkQuote(ctx, db, scope, l, false)
	if err != nil {
		return nil, nil, err
	}
	_, locale, _, err := customerEmail(ctx, db, scope, r.CustomerID)
	if err != nil {
		return nil, nil, magiclink.ErrInvalid
	}
	dv, err := s.quoteDocView(ctx, db, scope, r, locale)
	if err != nil {
		return nil, nil, err
	}
	v, err := s.view(ctx, db, scope, r)
	if err != nil {
		return nil, nil, err
	}
	out := &PublicQuote{
		Number: Number(r.Year, r.Number), Kind: r.Kind, Status: r.Status, ValidUntil: r.ValidUntil, VesselName: dv.Vessel.Name,
		WorkOrderNumber: dv.Quote.WorkOrderNumber, Net: v.Totals.Net, VAT: v.Totals.VAT, Gross: v.Totals.Gross, Deposit: v.Totals.Deposit,
		BillingMode: r.BillingMode, Conditions: r.Conditions, PlannedStartOn: r.PlannedStartOn, PlannedDeliveryOn: r.PlannedDeliveryOn,
		Lines: []PublicLine{},
	}
	for _, ln := range v.Lines {
		out.Lines = append(out.Lines, PublicLine{
			Description: ln.Spec.Description, Unit: ln.ItemUnit, Qty: ln.Qty, UnitCents: ln.Unit, Discount: ln.Discount, Net: ln.Net,
			VATRateBP: ln.VATData.RateBP,
		})
	}
	open := r.Status == store.YardQuoteDraft || r.Status == store.YardQuoteSent
	out.CanAccept = open && s.now().Before(r.ExpiresAt) && !v.Blocking
	return out, l, nil
}

// AcceptPublic accepts a yard quote through its link: the same transaction as the panel acceptance (via link).
// Quotes with conditions need acceptConditions.
func (s *Service) AcceptPublic(ctx context.Context, slug, token string, acceptConditions bool) (*PublicAcceptance, error) {
	l, scope, err := s.resolveLink(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	var (
		r   *store.YardQuote
		ref OrderRef
	)
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err = linkQuote(ctx, tx, scope, l, true)
		if err != nil {
			return err
		}
		if r.Status != store.YardQuoteDraft && r.Status != store.YardQuoteSent {
			return ErrInvalidTransition
		}
		if r.Conditions != "" && !acceptConditions {
			return validationErr("accept_conditions", "required")
		}
		ref, err = s.acceptTx(ctx, tx, scope, audit.System, r, store.AcceptedViaLink)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &PublicAcceptance{QuoteNumber: Number(r.Year, r.Number), WorkOrderNumber: ref.Number}, nil
}

// PublicDocument returns the PDF of the quote of a link.
func (s *Service) PublicDocument(ctx context.Context, slug, token string) ([]byte, error) {
	l, scope, err := s.resolveLink(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	r, err := linkQuote(ctx, db, scope, l, false)
	if err != nil {
		return nil, err
	}
	if s.d.Docs == nil {
		return nil, docs.ErrNotReady
	}
	row, err := store.LatestDocumentOf(ctx, db, scope, string(docs.SubjectYardQuote), r.ID, string(docs.KindYardQuote), false)
	if err != nil || row == nil {
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		return nil, docs.ErrNotReady
	}
	b, _, err := s.d.Docs.DocumentPDF(ctx, db, scope, row.ID)
	return b, err
}
