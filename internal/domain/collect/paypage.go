package collect

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/store"
)

// Public payment page (m5-plan §5.5): resolved by token (hash lookup, slug compared in constant time); unknown,
// expired and cancelled links are the same 404 link_invalid. The page shows the bank details always and "Paga
// online" only with an enabled provider; the return page polls the status, which the browser never changes.

// PayPage is the public view of a request.
type PayPage struct {
	MarinaName    string
	DefaultLocale string
	Locales       []string
	Amount        int64
	Currency      string
	Charges       []PayCharge
	Bank          *PayBank
	Online        bool
	Methods       []string
	Status        string
	ExpiresAt     time.Time
}

// PayCharge is a charge on the public page.
type PayCharge struct {
	Number      string
	Description string
	DueOn       calendar.Date
	Residual    int64
}

// PayBank are the transfer instructions.
type PayBank struct {
	IBAN, Holder, Bank, BIC, Reference string
}

// resolve finds the request of a token for a marina slug.
func (s *Service) resolve(ctx context.Context, slug, token string) (*store.PaymentRequest, store.MarinaScope, error) {
	if len(token) != TokenChars || slug == "" {
		return nil, store.MarinaScope{}, ErrLinkInvalid
	}
	r, mslug, err := store.PaymentRequestByTokenHash(ctx, s.d.DB.DB, HashToken(token))
	if err != nil {
		return nil, store.MarinaScope{}, ErrLinkInvalid
	}
	if subtle.ConstantTimeCompare([]byte(mslug), []byte(slug)) != 1 {
		return nil, store.MarinaScope{}, ErrLinkInvalid
	}
	switch r.Status {
	case store.RequestCancelled, store.RequestExpired:
		return nil, store.MarinaScope{}, ErrLinkInvalid
	case store.RequestOpen:
		if !r.ExpiresAt.IsZero() && !s.now().Before(r.ExpiresAt) {
			return nil, store.MarinaScope{}, ErrLinkInvalid
		}
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, r.MarinaID)
	if err != nil {
		return nil, store.MarinaScope{}, ErrLinkInvalid
	}
	return r, scope, nil
}

// transitOf returns the contract of a transit request (from its intents).
func transitOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, r *store.PaymentRequest) (*store.Contract, error) {
	if r.Source != RequestTransit {
		return nil, nil
	}
	ins, err := store.IntentsOfRequest(ctx, db, scope, r.ID)
	if err != nil {
		return nil, err
	}
	for _, in := range ins {
		if in.ContractID != "" {
			return store.GetContract(ctx, db, scope, in.ContractID)
		}
	}
	return nil, nil
}

// GetPayPage returns the public page of a token.
func (s *Service) GetPayPage(ctx context.Context, slug, token string) (*PayPage, error) {
	r, scope, err := s.resolve(ctx, slug, token)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	page := &PayPage{
		MarinaName: m.Name, DefaultLocale: m.DefaultLocale, Locales: m.Locales, Currency: r.Currency, Status: r.Status,
		ExpiresAt: r.ExpiresAt, Charges: []PayCharge{}, Methods: []string{},
	}
	views, err := s.requestViews(ctx, db, scope, []store.PaymentRequest{*r})
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, c := range views[0].Charges {
		refs = append(refs, c.Charge.Number)
		if c.Residual <= 0 {
			continue
		}
		page.Charges = append(page.Charges, PayCharge{Number: c.Charge.Number, Description: c.Charge.Description, DueOn: c.Charge.DueOn, Residual: c.Residual})
		page.Amount += c.Residual
	}
	if r.Source == RequestTransit && r.Status == store.RequestOpen {
		page.Amount = r.AmountCents
		if c, err := transitOf(ctx, db, scope, r); err == nil && c != nil {
			refs = []string{contract.Code(c)}
		}
	}
	if r.Status != store.RequestOpen {
		page.Amount = 0
	}
	if mv, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys); err == nil && mv.Bank.IBAN != "" {
		page.Bank = &PayBank{IBAN: mv.Bank.IBAN, Holder: mv.Bank.Holder, Bank: mv.Bank.Bank, BIC: mv.Bank.BIC, Reference: strings.Join(refs, " ")}
	}
	if e, err := s.enabledProvider(ctx, db, scope, needCheckout); err == nil && r.Status == store.RequestOpen && page.Amount > 0 {
		page.Online = true
		var methods []string
		_ = json.Unmarshal(e.row.Methods, &methods)
		if slices.Contains(methods, "card") || slices.Contains(methods, "wallet") || len(methods) == 0 {
			page.Methods = append(page.Methods, "card")
		}
		if slices.Contains(methods, "sepa_debit") {
			page.Methods = append(page.Methods, "sepa_debit")
		}
	}
	return page, nil
}

// PayStatus returns the status of a request for the return page: verifying while an online payment is open or
// processing at the provider and the request is not paid yet.
func (s *Service) PayStatus(ctx context.Context, slug, token string) (string, bool, error) {
	r, scope, err := s.resolve(ctx, slug, token)
	if err != nil {
		return "", false, err
	}
	if r.Status != store.RequestOpen {
		return r.Status, false, nil
	}
	live, err := store.LiveIntentsOf(ctx, s.d.DB.DB, scope, r.ID, "", "")
	if err != nil {
		return "", false, err
	}
	verifying := false
	for _, in := range live {
		if in.Status == store.IntentProcessing || (in.Status == store.IntentOpen && in.ProviderSessionRef != "") {
			verifying = true
		}
	}
	return r.Status, verifying, nil
}

// StartPayCheckout opens a hosted checkout for the request (card or sepa_debit) and returns its URL. A live session
// of the same method is reused; the provider is called after the intent committed.
func (s *Service) StartPayCheckout(ctx context.Context, slug, token, method string) (string, error) {
	if method != "card" && method != "sepa_debit" {
		return "", invalid("method", "invalid")
	}
	r, scope, err := s.resolve(ctx, slug, token)
	if err != nil {
		return "", err
	}
	if r.Status != store.RequestOpen {
		return "", ErrRequestClosed
	}
	e, err := s.enabledProvider(ctx, s.d.DB.DB, scope, needCheckout)
	if err != nil {
		return "", err
	}
	var methods []string
	_ = json.Unmarshal(e.row.Methods, &methods)
	if method == "sepa_debit" && !slices.Contains(methods, "sepa_debit") {
		return "", ErrProviderDisabled
	}
	m, err := store.GetMarina(ctx, s.d.DB.DB, scope)
	if err != nil {
		return "", err
	}
	plan := &checkoutPlan{
		e: e, locale: m.DefaultLocale, success: s.PayURL(m.Slug, token) + "/return", cancel: s.PayURL(m.Slug, token),
	}
	actor := providerActor(e.row.ProviderCode)
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.LockPaymentRequest(ctx, tx, scope, r.ID)
		if err != nil {
			return err
		}
		if cur.Status != store.RequestOpen {
			return ErrRequestClosed
		}
		live, err := store.LiveIntentsOf(ctx, tx, scope, r.ID, "", "")
		if err != nil {
			return err
		}
		now := s.now()
		var reuse *store.PaymentIntent
		for i := range live {
			in := &live[i]
			switch {
			case in.Status == store.IntentProcessing:
				return ErrPaymentInFlight
			case in.Status == store.IntentOpen && in.CheckoutURL != "" && in.Method == method && in.ExpiresAt.After(now.Add(2*time.Minute)):
				plan.reuse = in.CheckoutURL
				return nil
			case in.Status == store.IntentCreated && in.ProviderSessionRef == "" && reuse == nil:
				reuse = in
			default:
				if _, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromCreatedOpen, store.IntentCancelled, "", now); err != nil {
					return err
				}
			}
		}
		if reuse != nil && reuse.Purpose == PurposeTransit {
			c, err := store.GetContract(ctx, tx, scope, reuse.ContractID)
			if err != nil {
				return err
			}
			if contract.Status(c.Status) != contract.StatusPendingPayment {
				return ErrRequestClosed
			}
			if err := store.SetIntentMethod(ctx, tx, scope, reuse.ID, method, now); err != nil {
				return err
			}
			reuse.Method = method
			plan.intent, plan.desc = reuse, contract.Code(c)
			return nil
		}
		if reuse != nil {
			if _, err := store.SetIntentStatus(ctx, tx, scope, reuse.ID, fromCreatedOpen, store.IntentCancelled, "", now); err != nil {
				return err
			}
		}
		if cur.Source == RequestTransit {
			c, err := transitOf(ctx, tx, scope, cur)
			if err != nil || c == nil || contract.Status(c.Status) != contract.StatusPendingPayment {
				return ErrRequestClosed
			}
			in, err := s.newIntentTx(ctx, tx, scope, actor, &store.PaymentIntent{
				Purpose: PurposeTransit, ProviderCode: e.row.ProviderCode, CustomerID: cur.CustomerID, AmountCents: cur.AmountCents,
				Method: method, RequestID: cur.ID, ContractID: c.ID, ExpiresAt: now.Add(CheckoutTTL),
			}, c.ID, nil)
			plan.intent, plan.desc = in, contract.Code(c)
			return err
		}
		views, err := s.requestViews(ctx, tx, scope, []store.PaymentRequest{*cur})
		if err != nil {
			return err
		}
		var rows []store.PaymentIntentCharge
		var amount int64
		var refs []string
		for _, c := range views[0].Charges {
			if c.Residual > 0 {
				rows = append(rows, store.PaymentIntentCharge{ChargeID: c.Charge.ID, AmountCents: c.Residual})
				amount += c.Residual
				refs = append(refs, c.Charge.Number)
			}
		}
		if amount == 0 {
			return ErrChargesNotOpen
		}
		in, err := s.newIntentTx(ctx, tx, scope, actor, &store.PaymentIntent{
			Purpose: PurposePaymentRequest, ProviderCode: e.row.ProviderCode, CustomerID: cur.CustomerID, AmountCents: amount,
			Method: method, RequestID: cur.ID, ExpiresAt: now.Add(CheckoutTTL),
		}, cur.ID, rows)
		plan.intent, plan.desc = in, strings.Join(refs, " ")
		return err
	})
	if err != nil {
		return "", err
	}
	return s.startCheckout(ctx, scope, plan)
}
