package yardquote

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Quote lifecycle (m10-plan §4.3): send (PDF, magic link, email), accept (panel or link: the work order in the same
// transaction, INV-96), decline, expire. The quote row is locked first, then status, expiry and version are
// re-checked under the lock (PostgreSQL READ COMMITTED).

// ErrOrdersUnavailable means no work order side is wired (accept cannot create the order).
var ErrOrdersUnavailable = errors.New("yardquote: work orders unavailable")

// lockOpen locks a quote and checks If-Match (0 = not given) and that it is draft or sent.
func lockOpen(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string, ifMatch int) (*store.YardQuote, error) {
	r, err := store.GetYardQuote(ctx, tx, scope, id, true)
	if err != nil {
		return nil, notFound(err)
	}
	if ifMatch != 0 && r.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	if r.Status != store.YardQuoteDraft && r.Status != store.YardQuoteSent {
		return nil, ErrInvalidTransition
	}
	return r, nil
}

// blocking returns the blocking issues of a stored quote (line issues; no lines at all is blocking too).
func blocking(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.YardQuote) ([]store.YardQuoteLine, error) {
	lines, err := store.YardQuoteLines(ctx, tx, scope, r.ID)
	if err != nil {
		return nil, err
	}
	var issues []price.LineIssue
	for _, l := range lines {
		var is []price.LineIssue
		_ = json.Unmarshal(l.Issues, &is)
		issues = append(issues, is...)
	}
	for _, a := range asksOf(snapshotOf(r.Snapshot)) {
		if a.Required {
			issues = append(issues, price.LineIssue{Code: price.IssueMissingVariable, Ref: a.Name})
		}
	}
	if len(lines) == 0 {
		issues = append(issues, price.LineIssue{Code: price.IssueNoPrice, Field: "lines"})
	}
	if len(issues) > 0 {
		return nil, &BlockingError{Issues: issues}
	}
	return lines, nil
}

// customerEmail returns the first email and the locale of a customer ("" when none).
func customerEmail(ctx context.Context, db bun.IDB, scope store.MarinaScope, customerID string) (string, string, string, error) {
	c, err := store.GetCustomer(ctx, db, scope, customerID)
	if err != nil {
		return "", "", "", notFound(err)
	}
	var emails []struct {
		Value string `json:"value"`
	}
	_ = json.Unmarshal(c.Emails, &emails)
	name := docs.PartyOf(c).DisplayName
	for _, e := range emails {
		if e.Value != "" {
			return e.Value, c.Locale, name, nil
		}
	}
	return "", c.Locale, name, nil
}

// Send sends (or resends) a draft or sent quote (yard.manage, If-Match): the PDF (template yard_quote), a magic link
// expiring with the quote and, unless manual, the email to the customer (422 customer_email_missing).
func (s *Service) Send(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, manual bool, locale string) (*SendResult, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	res := &SendResult{}
	var r *store.YardQuote
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err = lockOpen(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		now := s.now()
		if !now.Before(r.ExpiresAt) {
			return ErrQuoteExpired
		}
		if _, err := blocking(ctx, tx, scope, r); err != nil {
			return err
		}
		to, custLocale, custName, err := customerEmail(ctx, tx, scope, r.CustomerID)
		if err != nil {
			return err
		}
		if !manual && to == "" {
			return ErrCustomerEmailMissing
		}
		if locale == "" {
			locale = custLocale
		}
		if s.d.Docs != nil {
			docID, err := s.d.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
				Kind: docs.KindYardQuote, SubjectType: docs.SubjectYardQuote, SubjectID: r.ID, Locale: locale, CreatedBy: p.UserID, At: now,
			})
			if err != nil {
				return err
			}
			res.DocumentID = docID
			d := s.d.Docs
			bg := context.WithoutCancel(ctx)
			store.AfterCommit(ctx, func() {
				if err := d.RenderPending(bg, docID); err != nil {
					s.d.Log.Warn("document render deferred to the job", "document_id", docID, "err", err)
				}
			})
		}
		if s.d.Links != nil {
			token, _, err := s.d.Links.CreateTx(ctx, tx, scope, magiclink.PurposeQuote, string(docs.SubjectYardQuote), r.ID, r.CustomerID, r.ExpiresAt)
			if err != nil {
				return err
			}
			m, err := store.GetMarina(ctx, tx, scope)
			if err != nil {
				return err
			}
			res.LinkURL = s.d.Links.URL(m.Slug, magiclink.PurposeQuote, token)
			if !manual && s.d.Notify != nil {
				var vessel string
				if vs, err := store.GetVessel(ctx, tx, scope, r.VesselID); err == nil {
					vessel = vs.Name
				}
				var attach []notify.AttachmentRef
				if res.DocumentID != "" {
					attach = []notify.AttachmentRef{{DocumentID: res.DocumentID}}
				}
				msgID, _, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
					Template: notify.TemplateYardQuote, MarinaID: scope.ID(), CustomerID: r.CustomerID, To: to, Locale: locale,
					DedupeKey: "yard_quote:" + r.ID + ":sent:" + strconv.Itoa(r.Version), SourceType: "yard_quote", SourceID: r.ID,
					Values: map[string]string{
						notify.ValueMarinaName: m.Name, notify.ValueCustomerName: custName, notify.ValueQuoteCode: Number(r.Year, r.Number),
						notify.ValueAmount: storeCents(r.GrossCents), notify.ValueValidUntil: r.ValidUntil.String(), notify.ValueLink: res.LinkURL,
						"vessel_name": vessel,
					},
					Attachments: attach,
				})
				if err != nil {
					return err
				}
				res.MessageID = msgID
			}
		}
		from := r.Status
		r.Status = store.YardQuoteSent
		if r.SentAt.IsZero() || from == store.YardQuoteDraft {
			r.SentAt = now
		}
		if err := store.UpdateYardQuote(ctx, tx, scope, r, r.Version, now); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "yard_quote.send", "yard_quote", r.ID, map[string]any{
			"from_status": from, "status": r.Status, "manual": manual, "document_id": res.DocumentID, "message_id": res.MessageID,
		}); err != nil {
			return err
		}
		return s.emitQuote(ctx, tx, scope, event.YardQuoteSent, r)
	})
	if err != nil {
		return nil, err
	}
	res.Quote, err = s.view(ctx, s.d.DB.DB, scope, r)
	return res, err
}

func storeCents(c int64) string { return money.Cents(c).String() }

// acceptTx accepts a locked open quote: not expired, no blocking issue, then the work order (job) or the variation
// is created by the order side in the same transaction (INV-96).
func (s *Service) acceptTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, r *store.YardQuote, via string) (OrderRef, error) {
	now := s.now()
	if !now.Before(r.ExpiresAt) {
		return OrderRef{}, ErrQuoteExpired
	}
	lines, err := blocking(ctx, tx, scope, r)
	if err != nil {
		return OrderRef{}, err
	}
	if s.d.Orders == nil {
		return OrderRef{}, ErrOrdersUnavailable
	}
	btx, ok := tx.(bun.Tx)
	if !ok {
		return OrderRef{}, errors.New("yardquote: accept outside a transaction")
	}
	from := r.Status
	r.Status, r.DecidedAt, r.AcceptedVia = store.YardQuoteAccepted, now, via
	if err := store.UpdateYardQuote(ctx, tx, scope, r, r.Version, now); err != nil {
		return OrderRef{}, versionErr(err)
	}
	if s.d.Links != nil {
		if err := s.d.Links.RevokeTx(ctx, tx, scope, magiclink.PurposeQuote, string(docs.SubjectYardQuote), r.ID); err != nil {
			return OrderRef{}, err
		}
	}
	aq := AcceptedQuote{Quote: *r, Lines: lines, Snapshot: snapshotOf(r.Snapshot), AcceptedAt: now, Via: via}
	var ref OrderRef
	if r.Kind == store.YardQuoteVariation {
		ref, err = s.d.Orders.ApplyVariationTx(ctx, btx, scope, a, aq)
	} else {
		ref, err = s.d.Orders.CreateFromQuoteTx(ctx, btx, scope, a, aq)
	}
	if err != nil {
		return OrderRef{}, err
	}
	if err := s.audit(ctx, tx, scope, a, "yard_quote.accept", "yard_quote", r.ID, map[string]any{
		"from_status": from, "via": via, "work_order_id": ref.WorkOrderID, "advance_charge_id": ref.AdvanceChargeID,
	}); err != nil {
		return OrderRef{}, err
	}
	data := event.YardQuoteData{
		QuoteID: r.ID, Kind: r.Kind, Status: r.Status, CustomerID: r.CustomerID, VesselID: r.VesselID, WorkOrderID: ref.WorkOrderID,
		GrossCents: r.GrossCents,
	}
	return ref, s.emit(ctx, tx, scope, event.YardQuoteAccepted, data)
}

// Accept accepts a quote from the panel (yard.manage, If-Match): the work order (or the variation) and the advance
// are created in the same transaction. Two concurrent accepts: one wins, the other is 409 invalid_transition.
func (s *Service) Accept(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*AcceptResult, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var (
		r   *store.YardQuote
		ref OrderRef
	)
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err = lockOpen(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		ref, err = s.acceptTx(ctx, tx, scope, p.Actor(), r, store.AcceptedViaPanel)
		return err
	})
	if err != nil {
		return nil, err
	}
	v, err := s.view(ctx, s.d.DB.DB, scope, r)
	if err != nil {
		return nil, err
	}
	return &AcceptResult{Quote: v, Order: ref}, nil
}

// Decline declines a draft or sent quote (yard.manage, If-Match).
func (s *Service) Decline(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*QuoteView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	reason, ok := cleanText(reason, 500)
	if !ok {
		return nil, validationErr("reason", "invalid")
	}
	var r *store.YardQuote
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err = lockOpen(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		now := s.now()
		from := r.Status
		r.Status, r.DecidedAt, r.DeclineReason = store.YardQuoteDeclined, now, reason
		if err := store.UpdateYardQuote(ctx, tx, scope, r, r.Version, now); err != nil {
			return versionErr(err)
		}
		if s.d.Links != nil {
			if err := s.d.Links.RevokeTx(ctx, tx, scope, magiclink.PurposeQuote, string(docs.SubjectYardQuote), r.ID); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "yard_quote.decline", "yard_quote", r.ID, map[string]any{"from_status": from}); err != nil {
			return err
		}
		return s.emitQuote(ctx, tx, scope, event.YardQuoteChanged, r)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, r)
}

// RunExpire is the job yard.quotes_expire: draft and sent quotes past expires_at become expired (idempotent;
// re-checked under the row lock, audited as the system).
func (s *Service) RunExpire(ctx context.Context) error {
	if s.d.DB == nil {
		return nil
	}
	for {
		refs, err := store.ExpiringYardQuotes(ctx, s.d.DB.DB, s.now(), 200)
		if err != nil {
			return err
		}
		if len(refs) == 0 {
			return nil
		}
		done := 0
		for _, ref := range refs {
			scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, ref.MarinaID)
			if err != nil {
				continue
			}
			err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				r, err := store.GetYardQuote(ctx, tx, scope, ref.ID, true)
				if err != nil {
					return err
				}
				now := s.now()
				if (r.Status != store.YardQuoteDraft && r.Status != store.YardQuoteSent) || now.Before(r.ExpiresAt) {
					return nil
				}
				from := r.Status
				r.Status = store.YardQuoteExpired
				if err := store.UpdateYardQuote(ctx, tx, scope, r, r.Version, now); err != nil {
					return err
				}
				if s.d.Links != nil {
					if err := s.d.Links.RevokeTx(ctx, tx, scope, magiclink.PurposeQuote, string(docs.SubjectYardQuote), r.ID); err != nil {
						return err
					}
				}
				if err := s.audit(ctx, tx, scope, audit.System, "yard_quote.expire", "yard_quote", r.ID, map[string]any{"from_status": from}); err != nil {
					return err
				}
				done++
				return s.emitQuote(ctx, tx, scope, event.YardQuoteExpired, r)
			})
			if err != nil {
				s.d.Log.Warn("yard quote expiry failed", "yard_quote_id", ref.ID, "err", err)
			}
		}
		if done == 0 || len(refs) < 200 {
			return nil
		}
	}
}

// PDF returns the PDF of a quote (yard.read): the last generated document when ready, else rendered on demand.
func (s *Service) PDF(ctx context.Context, p *auth.Principal, marinaID, id string) ([]byte, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	r, err := store.GetYardQuote(ctx, db, scope, id, false)
	if err != nil {
		return nil, notFound(err)
	}
	if s.d.Docs == nil {
		return nil, docs.ErrNotReady
	}
	if row, err := store.LatestDocumentOf(ctx, db, scope, string(docs.SubjectYardQuote), r.ID, string(docs.KindYardQuote), false); err == nil && row != nil {
		if b, _, err := s.d.Docs.DocumentPDF(ctx, db, scope, row.ID); err == nil {
			return b, nil
		}
	}
	_, locale, _, err := customerEmail(ctx, db, scope, r.CustomerID)
	if err != nil {
		return nil, err
	}
	v, err := s.quoteDocView(ctx, db, scope, r, locale)
	if err != nil {
		return nil, err
	}
	return s.d.Docs.RenderView(ctx, scope, docs.KindYardQuote, locale, v)
}
