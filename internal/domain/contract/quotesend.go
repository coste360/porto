package contract

import (
	"context"
	"strconv"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Quote sending (spec 13.2; m4-plan §3.2): the quote PDF (kind quote), a magic link (purpose quote, expiring with
// the quote, rotated on every send) and the email with the PDF attached to the customer's first email. Without
// an email the call fails with 422 customer_email_missing unless manual (the M3 "mark as sent").

// SendResult is the outcome of a quote send.
type SendResult struct {
	Quote      *QuoteDetail
	DocumentID string
	MessageID  string
}

// SendQuote sends (or resends) a draft or sent quote (contracts.write, If-Match). manual keeps the M3 mark.
func (s *Service) SendQuote(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, manual bool) (*SendResult, error) {
	if manual {
		q, err := s.MarkQuoteSent(ctx, p, marinaID, id, ifMatch)
		if err != nil {
			return nil, err
		}
		return &SendResult{Quote: q}, nil
	}
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	a := actor{p: p}
	res := &SendResult{}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		q, err := lockQuote(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		now := s.now()
		if (q.Status == string(QuoteDraft) || q.Status == string(QuoteSent)) && !now.Before(q.ExpiresAt) {
			return ErrQuoteExpired
		}
		var t *Transition
		switch q.Status {
		case string(QuoteDraft):
			tr, err := Next(QuoteTransitions, QuoteDraft, ActionSend)
			if err != nil {
				return err
			}
			t = &tr
		case string(QuoteSent): // resend: the link rotates, no state change
		default:
			return &TransitionError{From: q.Status, Action: ActionSend}
		}
		if q.CustomerID == "" {
			return ErrCustomerEmailMissing
		}
		to, _, err := customerEmail(ctx, tx, scope, q.CustomerID)
		if err != nil {
			return err
		}
		if to == "" {
			return ErrCustomerEmailMissing
		}
		docID, err := s.composeTx(ctx, tx, scope, a, docs.KindQuote, docs.SubjectQuote, q.ID, false)
		if err != nil {
			return err
		}
		link := ""
		if s.links != nil {
			token, _, err := s.links.CreateTx(ctx, tx, scope, LinkQuote, string(docs.SubjectQuote), q.ID, q.CustomerID, q.ExpiresAt)
			if err != nil {
				return err
			}
			m, err := store.GetMarina(ctx, tx, scope)
			if err != nil {
				return err
			}
			link = s.links.URL(m.Slug, LinkQuote, token)
		}
		n := q.SentCount + 1
		var attach []string
		if docID != "" {
			attach = []string{docID}
		}
		msgID, err := s.queueMailTx(ctx, tx, scope, Mail{
			Template: MailQuote, CustomerID: q.CustomerID, DedupeKey: "quote:" + q.ID + ":sent:" + strconv.Itoa(n),
			Values: map[string]string{
				"quote_code": ContractCode(SeriesQuote, q.Year, q.Number), "valid_until": q.ValidUntil.String(), "link": link,
			},
			DocumentIDs: attach, SourceType: "quote", SourceID: q.ID,
		})
		if err != nil {
			return err
		}
		from := q.Status
		if t != nil {
			q.Status, q.SentAt = string(t.To), now
		}
		q.SentCount, q.LinkRotatedAt = n, now
		if err := updateQuote(ctx, tx, scope, q, now); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, a, "quote.send", entityQuote, q.ID, map[string]any{
			"from_status": from, "status": q.Status, "sent_count": n, "document_id": docID, "message_id": msgID,
		}); err != nil {
			return err
		}
		res.DocumentID, res.MessageID = docID, msgID
		if t != nil {
			return s.emitTransition(ctx, tx, scope, *t, subject{quote: q})
		}
		return s.emit(ctx, tx, scope, event.QuoteChanged, event.QuoteChangedData{QuoteID: q.ID, Change: event.ChangeSent})
	})
	if err != nil {
		return nil, err
	}
	res.Quote, err = s.quoteDetail(ctx, s.d.DB.DB, scope, p, id)
	return res, err
}
