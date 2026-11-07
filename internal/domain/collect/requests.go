package collect

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/store"
)

// Payment requests (m5-plan §5.5, §16 D12): a request groups open charges of a customer and gives them a public
// payment page (bank details always, "Paga online" only with an enabled provider). The token is never stored:
// it is derived from the request id and a generation with a KeyRing key, so a re-send can show the same link,
// and only its SHA-256 is kept (INV-75 rules).

// TokenChars is the length of a payment link token (base32 of an HMAC-SHA256).
const TokenChars = 52

// MaxRequestCharges bounds the charges of one request; bulk requests take at most 500 charges.
const (
	MaxRequestCharges = 200
	MaxBulkCharges    = 500
)

// DefaultRequestDays is the validity of a request created by staff without expires_on.
const DefaultRequestDays = 60

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// tokenFor derives the token of generation gen of a request.
func (s *Service) tokenFor(requestID string, gen int) (string, error) {
	if s.d.Keys == nil {
		return "", errors.New("collect: key ring not wired")
	}
	key, err := s.d.Keys.Derive("payment-link-token", 32)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(requestID + ":" + strconv.Itoa(gen)))
	return b32.EncodeToString(m.Sum(nil)), nil
}

// HashToken is the stored form of a token.
func HashToken(token string) string { return crypto.SHA256Hex(token) }

// currentToken finds the generation of the stored hash (bounded by the sends) and returns its token; "" when the
// key ring changed since (the caller rotates).
func (s *Service) currentToken(r *store.PaymentRequest) (string, int) {
	for gen := 0; gen <= r.SentCount+1; gen++ {
		tok, err := s.tokenFor(r.ID, gen)
		if err != nil {
			return "", 0
		}
		if subtle.ConstantTimeCompare([]byte(HashToken(tok)), []byte(r.TokenHash)) == 1 {
			return tok, gen
		}
	}
	return "", r.SentCount + 2
}

// PayURL is the public page of a token.
func (s *Service) PayURL(slug, token string) string {
	return s.d.BaseURL + "/b/" + slug + "/pay/" + token
}

func marinaSlug(ctx context.Context, db bun.IDB, scope store.MarinaScope) (string, string, error) {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return "", "", err
	}
	return m.Slug, m.Name, nil
}

// RequestView is a request with its charges (residual at read time).
type RequestView struct {
	Request store.PaymentRequest
	Charges []RequestCharge
	URL     string // only when just created or sent
}

// RequestCharge is a charge of a request.
type RequestCharge struct {
	Charge   store.Charge
	Amount   int64 // residual when the request was last sent
	Residual int64
}

// openCharges loads charges for a request: open positive charges of one customer, sorted by due date.
func openCharges(ctx context.Context, db bun.IDB, scope store.MarinaScope, ids []string) ([]store.Charge, error) {
	cs, err := store.CollectChargesByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	if len(cs) != len(uniq(ids)) {
		return nil, ErrNotFound
	}
	slices.SortFunc(cs, func(a, b store.Charge) int {
		if c := a.DueOn.Compare(b.DueOn); c != 0 {
			return c
		}
		return strings.Compare(a.Number, b.Number)
	})
	return cs, nil
}

func uniq(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// createRequestTx creates an open request for charges of one customer and returns it with its token. Charges must
// be open with a positive residual (ErrChargesNotOpen).
func (s *Service) createRequestTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, customerID, source string, charges []store.Charge, amount int64, expiresAt time.Time) (*store.PaymentRequest, string, error) {
	var total int64
	rows := make([]store.PaymentRequestCharge, 0, len(charges))
	for _, c := range charges {
		res := c.ResidualCents()
		if c.CustomerID != customerID || res <= 0 {
			return nil, "", ErrChargesNotOpen
		}
		total += res
		rows = append(rows, store.PaymentRequestCharge{ChargeID: c.ID, AmountCents: res})
	}
	if len(charges) > 0 {
		amount = total
	}
	now := s.now()
	r := &store.PaymentRequest{
		ID: store.NewID(), MarinaID: scope.ID(), CustomerID: customerID, AmountCents: amount, Currency: "EUR",
		Status: store.RequestOpen, Source: source, ExpiresAt: expiresAt, CreatedByUserID: userOf(actor),
		CreatedByActor: actorString(actor), Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	tok, err := s.tokenFor(r.ID, 0)
	if err != nil {
		return nil, "", err
	}
	r.TokenHash = HashToken(tok)
	if err := store.InsertPaymentRequest(ctx, tx, scope, r, rows); err != nil {
		return nil, "", err
	}
	if err := s.audit(ctx, tx, scope, actor, "payment_request.create", "payment_request", r.ID, map[string]any{
		"customer_id": customerID, "source": source, "amount_cents": amount, "charge_count": len(rows),
	}); err != nil {
		return nil, "", err
	}
	if err := s.emitRequest(ctx, tx, scope, r, "created", len(rows)); err != nil {
		return nil, "", err
	}
	return r, tok, nil
}

func (s *Service) emitRequest(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.PaymentRequest, status string, n int) error {
	return s.emit(ctx, tx, scope, event.PaymentRequestChanged, event.PaymentRequestChangedData{RequestID: r.ID, Status: status, ChargeCount: n})
}

// requestMailTx queues the payment_request email of a request with its link and the bank details. A customer
// without email is ErrCustomerEmailMissing (callers that must not fail ignore it).
func (s *Service) requestMailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.PaymentRequest, token string, charges []store.Charge, dedupe string) (string, error) {
	if s.d.Notify == nil {
		return "", nil
	}
	to, locale, name, err := customerContact(ctx, tx, scope, r.CustomerID)
	if err != nil {
		return "", err
	}
	if to == "" {
		return "", ErrCustomerEmailMissing
	}
	slug, marinaName, err := marinaSlug(ctx, tx, scope)
	if err != nil {
		return "", err
	}
	vals := s.mailValues(ctx, tx, scope, marinaName, name, charges, r.AmountCents)
	vals[notify.ValueLink] = s.PayURL(slug, token)
	if !r.ExpiresAt.IsZero() {
		vals[notify.ValueExpiresAt] = italianDate(calendar.In(r.ExpiresAt, s.location(ctx, tx, scope)))
	}
	id, _, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
		Template: notify.TemplatePaymentRequest, MarinaID: scope.ID(), CustomerID: r.CustomerID, To: to, Locale: locale,
		DedupeKey: dedupe, SourceType: "payment_request", SourceID: r.ID, Values: vals,
	})
	return id, err
}

// mailValues are the display values shared by the request and reminder emails.
func (s *Service) mailValues(ctx context.Context, db bun.IDB, scope store.MarinaScope, marinaName, customerName string, charges []store.Charge, amount int64) map[string]string {
	vals := map[string]string{
		notify.ValueMarinaName: marinaName, notify.ValueCustomerName: customerName, notify.ValueAmount: euro(amount),
	}
	var refs, lines []string
	var due calendar.Date
	for _, c := range charges {
		refs = append(refs, c.Number)
		line := c.Number + " · " + italianDate(c.DueOn) + " · " + euro(c.ResidualCents())
		if d := strings.TrimSpace(c.Description); d != "" {
			line = c.Number + " · " + d + " · " + italianDate(c.DueOn) + " · " + euro(c.ResidualCents())
		}
		lines = append(lines, line)
		if due.IsZero() || c.DueOn.Before(due) {
			due = c.DueOn
		}
	}
	if !due.IsZero() {
		vals[notify.ValueDueOn] = italianDate(due)
	}
	vals["reference"] = strings.Join(refs, " ")
	vals["charges"] = strings.Join(lines, "\n")
	if mv, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys); err == nil && mv.Bank.IBAN != "" {
		vals[notify.ValueIBAN], vals[notify.ValueBankHolder], vals[notify.ValueBank], vals["bic"] = mv.Bank.IBAN, mv.Bank.Holder, mv.Bank.Bank, mv.Bank.BIC
	}
	return vals
}

// RequestForChargeTx implements charge.PaymentRequester: when billing_settings.auto_payment_request, a payment
// request for the new charge and its email (template payment_request, dedupe key given by charge). It never fails
// the caller for a missing email address.
func (s *Service) RequestForChargeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, dedupeKey string) error {
	if ch == nil || ch.CustomerID == "" || ch.ResidualCents() <= 0 {
		return nil
	}
	bs, err := store.GetBillingSettings(ctx, tx, scope, s.now())
	if err != nil {
		return err
	}
	if !bs.AutoPaymentRequest {
		return nil
	}
	exp := ch.DueOn.AddDays(DefaultRequestDays).Time(s.location(ctx, tx, scope))
	r, tok, err := s.createRequestTx(ctx, tx, scope, actor, ch.CustomerID, RequestAuto, []store.Charge{*ch}, 0, exp)
	if err != nil {
		return err
	}
	if dedupeKey == "" {
		dedupeKey = "payment_request:" + r.ID
	}
	id, err := s.requestMailTx(ctx, tx, scope, r, tok, []store.Charge{*ch}, dedupeKey)
	if errors.Is(err, ErrCustomerEmailMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	r.LastMessageID = id
	return store.UpdateRequestSent(ctx, tx, scope, r, 0, s.now())
}

// CreateRequestInput is a staff request.
type CreateRequestInput struct {
	CustomerID string
	ChargeIDs  []string
	ExpiresOn  calendar.Date
	Send       bool
}

// CreateRequest creates a request for charges of a customer and optionally emails it (charges.manage).
func (s *Service) CreateRequest(ctx context.Context, p *auth.Principal, marinaID string, in CreateRequestInput) (*RequestView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if !store.ValidID(in.CustomerID) {
		return nil, ErrNotFound
	}
	if len(in.ChargeIDs) == 0 || len(in.ChargeIDs) > MaxRequestCharges {
		return nil, invalid("charge_ids", "count")
	}
	var out *RequestView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		v, err := s.createRequestForTx(ctx, tx, scope, p.Actor(), in.CustomerID, in.ChargeIDs, in.ExpiresOn, in.Send, RequestManual)
		out = v
		return err
	})
	return out, err
}

func (s *Service) createRequestForTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, customerID string, chargeIDs []string, expiresOn calendar.Date, send bool, source string) (*RequestView, error) {
	charges, err := openCharges(ctx, tx, scope, chargeIDs)
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, tx, scope)
	if expiresOn.IsZero() {
		expiresOn = today.AddDays(DefaultRequestDays)
	}
	if expiresOn.Before(today) {
		return nil, invalid("expires_on", "in_past")
	}
	exp := expiresOn.AddDays(1).Time(s.location(ctx, tx, scope))
	r, tok, err := s.createRequestTx(ctx, tx, scope, actor, customerID, source, charges, 0, exp)
	if err != nil {
		return nil, err
	}
	if send {
		id, err := s.requestMailTx(ctx, tx, scope, r, tok, charges, "payment_request:"+r.ID+":1")
		if err != nil {
			return nil, err
		}
		r.LastMessageID = id
		if err := store.UpdateRequestSent(ctx, tx, scope, r, 0, s.now()); err != nil {
			return nil, err
		}
		if err := s.emitRequest(ctx, tx, scope, r, "sent", len(charges)); err != nil {
			return nil, err
		}
	}
	slug, _, err := marinaSlug(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	v := &RequestView{Request: *r, URL: s.PayURL(slug, tok)}
	for _, c := range charges {
		v.Charges = append(v.Charges, RequestCharge{Charge: c, Amount: c.ResidualCents(), Residual: c.ResidualCents()})
	}
	return v, nil
}

// BulkResult counts a bulk request.
type BulkResult struct {
	Created    int
	Skipped    int
	RequestIDs []string
}

// CreateRequestsBulk creates one request per customer for up to 500 charges; charges without customer or not open
// are skipped (charges.manage).
func (s *Service) CreateRequestsBulk(ctx context.Context, p *auth.Principal, marinaID string, chargeIDs []string, send bool) (*BulkResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if len(chargeIDs) == 0 || len(chargeIDs) > MaxBulkCharges {
		return nil, invalid("charge_ids", "count")
	}
	out := &BulkResult{}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		*out = BulkResult{}
		cs, err := store.CollectChargesByIDs(ctx, tx, scope, uniq(chargeIDs))
		if err != nil {
			return err
		}
		out.Skipped = len(uniq(chargeIDs)) - len(cs)
		byCustomer := map[string][]string{}
		var order []string
		for _, c := range cs {
			if c.CustomerID == "" || c.ResidualCents() <= 0 {
				out.Skipped++
				continue
			}
			if _, ok := byCustomer[c.CustomerID]; !ok {
				order = append(order, c.CustomerID)
			}
			byCustomer[c.CustomerID] = append(byCustomer[c.CustomerID], c.ID)
		}
		for _, cust := range order {
			v, err := s.createRequestForTx(ctx, tx, scope, p.Actor(), cust, byCustomer[cust], calendar.Date{}, send, RequestManual)
			if errors.Is(err, ErrCustomerEmailMissing) {
				out.Skipped += len(byCustomer[cust])
				continue
			}
			if err != nil {
				return err
			}
			out.Created++
			out.RequestIDs = append(out.RequestIDs, v.Request.ID)
		}
		return nil
	})
	return out, err
}

// ListRequests returns the requests (charges.manage).
func (s *Service) ListRequests(ctx context.Context, p *auth.Principal, marinaID string, f store.PaymentRequestFilter, cursor string, limit int) (store.Page[RequestView], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return store.Page[RequestView]{}, err
	}
	page, err := store.ListPaymentRequests(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[RequestView]{}, err
	}
	out := store.Page[RequestView]{NextCursor: page.NextCursor, Items: make([]RequestView, 0, len(page.Items))}
	views, err := s.requestViews(ctx, s.d.DB.DB, scope, page.Items)
	if err != nil {
		return out, err
	}
	out.Items = views
	return out, nil
}

func (s *Service) requestViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.PaymentRequest) ([]RequestView, error) {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	byReq, err := store.RequestChargeIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	var all []string
	for _, cs := range byReq {
		all = append(all, cs...)
	}
	charges, err := store.CollectChargesByIDs(ctx, db, scope, uniq(all))
	if err != nil {
		return nil, err
	}
	byID := map[string]store.Charge{}
	for _, c := range charges {
		byID[c.ID] = c
	}
	out := make([]RequestView, 0, len(rows))
	for _, r := range rows {
		v := RequestView{Request: r}
		for _, id := range byReq[r.ID] {
			if c, ok := byID[id]; ok {
				v.Charges = append(v.Charges, RequestCharge{Charge: c, Amount: c.ResidualCents(), Residual: c.ResidualCents()})
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// GetRequest returns a request with its charges (charges.manage).
func (s *Service) GetRequest(ctx context.Context, p *auth.Principal, marinaID, id string) (*RequestView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	r, err := store.GetPaymentRequest(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	vs, err := s.requestViews(ctx, s.d.DB.DB, scope, []store.PaymentRequest{*r})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// SendRequest emails a request again (charges.manage, If-Match). The link stays the same unless rotate; the
// amount is refreshed to the current residuals.
func (s *Service) SendRequest(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, rotate bool) (*RequestView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	var out *RequestView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.LockPaymentRequest(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		if r.Status != store.RequestOpen {
			return ErrRequestClosed
		}
		views, err := s.requestViews(ctx, tx, scope, []store.PaymentRequest{*r})
		if err != nil {
			return err
		}
		var charges []store.Charge
		var amount int64
		for _, c := range views[0].Charges {
			if c.Residual > 0 {
				charges = append(charges, c.Charge)
				amount += c.Residual
			}
		}
		if len(views[0].Charges) > 0 {
			if amount == 0 {
				return ErrChargesNotOpen
			}
			r.AmountCents = amount
		}
		tok, gen := s.currentToken(r)
		if rotate || tok == "" {
			if tok, err = s.tokenFor(r.ID, gen+1); err != nil {
				return err
			}
			r.TokenHash = HashToken(tok)
		}
		msg, err := s.requestMailTx(ctx, tx, scope, r, tok, charges, "payment_request:"+r.ID+":"+strconv.Itoa(r.SentCount+1))
		if err != nil {
			return err
		}
		r.LastMessageID = msg
		if err := store.UpdateRequestSent(ctx, tx, scope, r, ifMatch, s.now()); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "payment_request.send", "payment_request", r.ID, map[string]any{
			"rotated": rotate, "amount_cents": r.AmountCents, "sent_count": r.SentCount,
		}); err != nil {
			return err
		}
		if err := s.emitRequest(ctx, tx, scope, r, "sent", len(views[0].Charges)); err != nil {
			return err
		}
		slug, _, err := marinaSlug(ctx, tx, scope)
		if err != nil {
			return err
		}
		views[0].Request, views[0].URL = *r, s.PayURL(slug, tok)
		out = &views[0]
		return nil
	})
	return out, err
}

// CancelRequest cancels an open request (charges.manage, If-Match); its live intents are cancelled.
func (s *Service) CancelRequest(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*RequestView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.LockPaymentRequest(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		ok, err := store.SetRequestStatus(ctx, tx, scope, id, store.RequestCancelled, ifMatch, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrRequestClosed
		}
		live, err := store.LiveIntentsOf(ctx, tx, scope, id, "", "")
		if err != nil {
			return err
		}
		for _, in := range live {
			if in.Status == store.IntentProcessing {
				continue // money in flight: the webhook decides
			}
			if _, err := store.SetIntentStatus(ctx, tx, scope, in.ID, []string{store.IntentCreated, store.IntentOpen}, store.IntentCancelled, "", s.now()); err != nil {
				return err
			}
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "payment_request.cancel", "payment_request", id, map[string]any{"status": store.RequestCancelled}); err != nil {
			return err
		}
		return s.emitRequest(ctx, tx, scope, r, "cancelled", 0)
	})
	if err != nil {
		return nil, err
	}
	return s.GetRequest(ctx, p, marinaID, id)
}

// markRequestsPaidTx moves to paid the open requests whose charges are all paid now.
func (s *Service) markRequestsPaidTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, chargeIDs []string) error {
	reqs, err := store.OpenRequestsOfCharges(ctx, tx, scope, chargeIDs)
	if err != nil {
		return err
	}
	views, err := s.requestViews(ctx, tx, scope, reqs)
	if err != nil {
		return err
	}
	for _, v := range views {
		if len(v.Charges) == 0 {
			continue
		}
		paid := true
		for _, c := range v.Charges {
			if c.Residual > 0 {
				paid = false
			}
		}
		if !paid {
			continue
		}
		if err := s.closeRequestPaidTx(ctx, tx, scope, actor, &v.Request, len(v.Charges)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) closeRequestPaidTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, r *store.PaymentRequest, n int) error {
	ok, err := store.SetRequestStatus(ctx, tx, scope, r.ID, store.RequestPaid, 0, s.now())
	if err != nil || !ok {
		return err
	}
	if err := s.audit(ctx, tx, scope, actor, "payment_request.paid", "payment_request", r.ID, map[string]any{"status": store.RequestPaid}); err != nil {
		return err
	}
	return s.emitRequest(ctx, tx, scope, r, "paid", n)
}
