package collect

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// SEPA Direct Debit (m5-plan §5.5): a mandate is collected on the provider's hosted page (setup intent) and
// activated by the webhook; the job payments.sepa_collect then debits issued open instalment charges of contracts
// whose policy asks for SEPA. The provider references are KeyRing-encrypted (AAD sepa-customer:<id>,
// sepa-pm:<id>). One live intent per charge (§16 D16) keeps a charge from being debited twice.

// MandateSetup is a new mandate with its setup URL.
type MandateSetup struct {
	Mandate  store.SEPAMandate
	SetupURL string
}

// ListMandates returns the mandates of a customer (charges.manage).
func (s *Service) ListMandates(ctx context.Context, p *auth.Principal, marinaID, customerID string) ([]store.SEPAMandate, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if err := s.customerExists(ctx, s.d.DB.DB, scope, customerID); err != nil {
		return nil, err
	}
	return store.ListSEPAMandates(ctx, s.d.DB.DB, scope, customerID)
}

func (s *Service) customerExists(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) error {
	if !store.ValidID(id) {
		return ErrNotFound
	}
	cs, err := store.CustomersByIDs(ctx, db, scope, []string{id})
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return ErrNotFound
	}
	return nil
}

// RequestMandate creates a pending mandate with its setup link (charges.manage) and optionally emails it
// (sepa_mandate_request). The provider must be enabled with SEPA.
func (s *Service) RequestMandate(ctx context.Context, p *auth.Principal, marinaID, customerID, provider string, send bool) (*MandateSetup, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if err := s.customerExists(ctx, s.d.DB.DB, scope, customerID); err != nil {
		return nil, err
	}
	var e *enabled
	if provider != "" {
		e, err = s.providerByCode(ctx, s.d.DB.DB, scope, provider)
		if err == nil && (!e.row.Enabled || !needSEPA(e.drv.Capabilities(), e.row)) {
			err = ErrProviderDisabled
		}
		if errors.Is(err, store.ErrNotFound) {
			err = ErrProviderDisabled
		}
	} else {
		e, err = s.enabledProvider(ctx, s.d.DB.DB, scope, needSEPA)
	}
	if err != nil {
		return nil, err
	}
	var in *store.PaymentIntent
	var m *store.SEPAMandate
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		in, err = s.newIntentTx(ctx, tx, scope, p.Actor(), &store.PaymentIntent{
			Purpose: PurposeMandateSetup, ProviderCode: e.row.ProviderCode, CustomerID: customerID, Method: "sepa_debit",
		}, customerID, nil)
		if err != nil {
			return err
		}
		now := s.now()
		m = &store.SEPAMandate{
			ID: store.NewID(), MarinaID: scope.ID(), CustomerID: customerID, ProviderCode: e.row.ProviderCode,
			Status: store.MandatePending, SetupIntentID: in.ID, CreatedByUserID: userOf(p.Actor()), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertSEPAMandate(ctx, tx, m); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "sepa_mandate.request", "sepa_mandate", m.ID, map[string]any{
			"customer_id": customerID, "provider_code": m.ProviderCode, "intent_id": in.ID,
		}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "sepa_mandate", ID: m.ID, Change: event.ChangeCreated})
	})
	if err != nil {
		return nil, err
	}
	cctx, err := s.withCreds(ctx, scope, e.row)
	if err != nil {
		return nil, ErrProviderDisabled
	}
	mk, err := store.GetMarina(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	back := s.d.BaseURL + "/b/" + mk.Slug
	sess, callErr := e.drv.CreateMandateSetup(cctx, ext.MandateSetupRequest{
		MarinaID: scope.ID(), IntentID: in.ID, IdempotencyKey: in.IdempotencyKey, Currency: "EUR",
		SuccessURL: back, CancelURL: back, Locale: mk.DefaultLocale,
	})
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if callErr != nil {
			if _, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromCreatedOpen, store.IntentFailed, providerCode(callErr), s.now()); err != nil {
				return err
			}
			_, err := store.SetSEPAMandateStatus(ctx, tx, scope, m.ID, []string{store.MandatePending}, store.MandateFailed, s.now())
			return err
		}
		if err := store.SetIntentRefs(ctx, tx, scope, in.ID, sess.SessionRef, "", sess.URL, sess.ExpiresAt, s.now()); err != nil {
			return err
		}
		if !send || s.d.Notify == nil {
			return nil
		}
		to, locale, name, err := customerContact(ctx, tx, scope, customerID)
		if err != nil || to == "" {
			return err
		}
		_, _, err = s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
			Template: notify.TemplateSEPAMandateRequest, MarinaID: scope.ID(), CustomerID: customerID, To: to, Locale: locale,
			DedupeKey: "sepa_mandate:" + m.ID, SourceType: "sepa_mandate", SourceID: m.ID,
			Values: map[string]string{notify.ValueMarinaName: mk.Name, notify.ValueCustomerName: name, notify.ValueLink: sess.URL},
		})
		return err
	})
	if callErr != nil {
		return nil, providerError(callErr)
	}
	if err != nil {
		return nil, err
	}
	cur, err := store.GetSEPAMandate(ctx, s.d.DB.DB, scope, m.ID)
	if err != nil {
		return nil, err
	}
	return &MandateSetup{Mandate: *cur, SetupURL: sess.URL}, nil
}

// RevokeMandate revokes a pending or active mandate (charges.manage); later collections stop.
func (s *Service) RevokeMandate(ctx context.Context, p *auth.Principal, marinaID, customerID, id string) (*store.SEPAMandate, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	var out *store.SEPAMandate
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.GetSEPAMandate(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if m.CustomerID != customerID {
			return ErrNotFound
		}
		ok, err := store.SetSEPAMandateStatus(ctx, tx, scope, id, []string{store.MandatePending, store.MandateActive}, store.MandateRevoked, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrMandateClosed
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "sepa_mandate.revoke", "sepa_mandate", id, map[string]any{"status": store.MandateRevoked}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "sepa_mandate", ID: id, Change: event.ChangeCancelled}); err != nil {
			return err
		}
		out, err = store.GetSEPAMandate(ctx, tx, scope, id)
		return err
	})
	return out, err
}

// mandateActiveTx activates the mandate of a setup intent with the encrypted provider references.
func (s *Service) mandateActiveTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in *store.PaymentIntent, ev *ext.PaymentEvent) error {
	if in.Purpose != PurposeMandateSetup {
		return nil
	}
	m, err := store.SEPAMandateBySetupIntent(ctx, tx, scope, in.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.d.Keys == nil {
		return errors.New("collect: key ring not wired")
	}
	if ev.CustomerRef != "" {
		if m.ProviderCustomerRefEnc, err = s.d.Keys.EncryptString(ev.CustomerRef, "sepa-customer:"+m.ID); err != nil {
			return err
		}
	}
	if ev.PaymentMethodRef != "" {
		if m.PaymentMethodRefEnc, err = s.d.Keys.EncryptString(ev.PaymentMethodRef, "sepa-pm:"+m.ID); err != nil {
			return err
		}
	}
	m.MandateRef, m.IBANLast4 = limit(ev.MandateRef, 200), limit(ev.IBANLast4, 4)
	ok, err := store.ActivateSEPAMandate(ctx, tx, scope, m, s.now())
	if err != nil || !ok {
		return err
	}
	if _, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromAnyButSucceeded, store.IntentSucceeded, "", s.now()); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, scope, actor, "sepa_mandate.activate", "sepa_mandate", m.ID, map[string]any{"status": store.MandateActive}); err != nil {
		return err
	}
	return s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "sepa_mandate", ID: m.ID, Change: event.ChangeActivated})
}

func (s *Service) mandateRevokedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, provider string, ev *ext.PaymentEvent) (string, string, error) {
	if ev.MandateRef == "" {
		return EventIgnored, "", nil
	}
	m, err := store.SEPAMandateByRef(ctx, tx, scope, provider, ev.MandateRef)
	if errors.Is(err, store.ErrNotFound) {
		return EventIgnored, "", nil
	}
	if err != nil {
		return "", "", err
	}
	ok, err := store.SetSEPAMandateStatus(ctx, tx, scope, m.ID, []string{store.MandateActive, store.MandatePending}, store.MandateRevoked, s.now())
	if err != nil || !ok {
		return EventApplied, "", err
	}
	if err := s.audit(ctx, tx, scope, actor, "sepa_mandate.revoke", "sepa_mandate", m.ID, map[string]any{"status": store.MandateRevoked}); err != nil {
		return "", "", err
	}
	return EventApplied, "", s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "sepa_mandate", ID: m.ID, Change: event.ChangeCancelled})
}

// sepaPolicy reports whether a contract's policy (or the marina default) asks for SEPA collection.
func sepaPolicy(raw store.JSON, def bool) bool {
	var p struct {
		Payment *struct {
			SEPA bool `json:"sepa"`
		} `json:"payment"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Payment == nil {
		return def
	}
	return p.Payment.SEPA
}

// RunSEPACollect is the job payments.sepa_collect: for each marina with an enabled SEPA provider, one sepa_collect
// intent per due open instalment charge of a SEPA contract whose customer has an active mandate; the provider is
// called after commit and the outcome arrives by webhook (days later).
func (s *Service) RunSEPACollect(ctx context.Context) error {
	ids, err := store.MarinasWithEnabledProvider(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if _, err := s.SEPACollectMarina(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// SEPACollectMarina runs the SEPA collection of one marina and returns the intents sent.
func (s *Service) SEPACollectMarina(ctx context.Context, marinaID string) (int, error) {
	db := s.d.DB.DB
	scope, err := store.SystemMarinaScope(ctx, db, marinaID)
	if err != nil {
		return 0, err
	}
	e, err := s.enabledProvider(ctx, db, scope, needSEPA)
	if errors.Is(err, ErrProviderDisabled) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	bs, err := store.GetBillingSettings(ctx, db, scope, s.now())
	if err != nil {
		return 0, err
	}
	def := false
	if cs, err := store.GetContractSettings(ctx, db, scope, s.now()); err == nil {
		def = sepaPolicy(cs.DefaultPolicy, false)
	}
	today := s.today(ctx, db, scope)
	cands, err := store.SEPACandidates(ctx, db, scope, e.row.ProviderCode, today.AddDays(bs.SEPALeadDays), 100)
	if err != nil {
		return 0, err
	}
	actor := jobActor("payments.sepa_collect")
	sent := 0
	for _, c := range cands {
		if !sepaPolicy(c.Policy, def) {
			continue
		}
		if err := s.collectOne(ctx, scope, e, actor, c); err != nil {
			s.d.Log.WarnContext(ctx, "sepa collection failed", "marina_id", marinaID, "charge_id", c.ChargeID, "code", providerCode(err))
			continue
		}
		sent++
	}
	return sent, nil
}

func (s *Service) collectOne(ctx context.Context, scope store.MarinaScope, e *enabled, actor audit.Actor, c store.SEPACandidate) error {
	var in *store.PaymentIntent
	var mandate *store.SEPAMandate
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		chs, err := store.LockChargesSorted(ctx, tx, scope, []string{c.ChargeID})
		if err != nil {
			return err
		}
		res := chs[0].ResidualCents()
		if res <= 0 {
			return nil
		}
		if mandate, err = store.GetSEPAMandate(ctx, tx, scope, c.MandateID); err != nil {
			return err
		}
		in, err = s.newIntentTx(ctx, tx, scope, actor, &store.PaymentIntent{
			Purpose: PurposeSEPACollect, ProviderCode: e.row.ProviderCode, CustomerID: c.CustomerID, AmountCents: res,
			Method: "sepa_debit", ContractID: c.ContractID, SEPAMandateID: mandate.ID,
		}, c.ChargeID, []store.PaymentIntentCharge{{ChargeID: c.ChargeID, AmountCents: res}})
		return err
	})
	if err != nil || in == nil {
		return err
	}
	cust, err1 := s.d.Keys.DecryptString(mandate.ProviderCustomerRefEnc, "sepa-customer:"+mandate.ID)
	pm, err2 := s.d.Keys.DecryptString(mandate.PaymentMethodRefEnc, "sepa-pm:"+mandate.ID)
	cctx, err3 := s.withCreds(ctx, scope, e.row)
	var pay *ext.ProviderPayment
	callErr := errors.Join(err1, err2, err3)
	if callErr == nil {
		ch, _ := store.GetCharge(ctx, s.d.DB.DB, scope, c.ChargeID)
		desc := ""
		if ch != nil {
			desc = ch.Number
		}
		pay, callErr = e.drv.ChargeMandate(cctx, ext.MandateChargeRequest{
			MarinaID: scope.ID(), IntentID: in.ID, IdempotencyKey: in.IdempotencyKey, AmountCents: in.AmountCents,
			Currency: in.Currency, Description: desc, CustomerRef: cust, PaymentMethodRef: pm,
		})
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if callErr != nil {
			return s.failedTx(ctx, tx, scope, actor, in, providerCode(callErr))
		}
		if err := store.SetIntentRefs(ctx, tx, scope, in.ID, "", pay.PaymentRef, "", in.ExpiresAt, s.now()); err != nil {
			return err
		}
		if pay.Status == ext.PaymentEventFailed {
			return s.failedTx(ctx, tx, scope, actor, in, "declined")
		}
		_, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromCreatedOpen, store.IntentProcessing, "", s.now())
		return err
	})
}
