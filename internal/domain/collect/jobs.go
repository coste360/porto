package collect

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Reconciliation and expiry jobs (m5-plan §5.4).

// Reconciliation bounds: intents older than ReconcileAfter, at most ReconcileBatch per marina and run; provider
// events are kept EventRetention for dedupe.
const (
	ReconcileAfter = 10 * time.Minute
	ReconcileBatch = 50
	EventRetention = 180 * 24 * time.Hour
)

// RunReconcile is the job payments.reconcile (only marinas with an enabled provider): open or processing intents
// older than 10 minutes are checked at the provider and the outcome goes through ApplyProviderEvent with the
// synthetic event id reconcile:<ref>:<status> (applied once); pending provider refunds are sent again with their
// idempotency key; provider events older than 180 days are deleted.
func (s *Service) RunReconcile(ctx context.Context) error {
	ids, err := store.MarinasWithEnabledProvider(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if _, err := s.ReconcileMarina(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := store.DeleteOldProviderEvents(ctx, s.d.DB.DB, s.now().Add(-EventRetention)); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ReconcileMarina reconciles one marina and returns how many events it applied.
func (s *Service) ReconcileMarina(ctx context.Context, marinaID string) (int, error) {
	db := s.d.DB.DB
	scope, err := store.SystemMarinaScope(ctx, db, marinaID)
	if err != nil {
		return 0, err
	}
	now := s.now()
	intents, err := store.IntentsToReconcile(ctx, db, scope, now.Add(-ReconcileAfter), ReconcileBatch)
	if err != nil {
		return 0, err
	}
	applied := 0
	for i := range intents {
		in := &intents[i]
		if in.Purpose == PurposeMandateSetup {
			continue // activation needs the setup intent data of the webhook
		}
		e, err := s.providerByCode(ctx, db, scope, in.ProviderCode)
		if err != nil || !e.drv.Capabilities().StatusCheck {
			continue
		}
		cctx, err := s.withCreds(ctx, scope, e.row)
		if err != nil {
			continue
		}
		ref := ext.ProviderRef{SessionRef: in.ProviderSessionRef, PaymentRef: in.ProviderPaymentRef}
		if ref.PaymentRef != "" {
			ref.SessionRef = ""
		}
		if ref.SessionRef == "" && ref.PaymentRef == "" {
			continue
		}
		ev, callErr := e.drv.Status(cctx, ref)
		_ = store.TouchIntentChecked(ctx, db, scope, in.ID, s.now())
		if callErr != nil || ev == nil || ev.Kind == "" {
			continue
		}
		key := ref.PaymentRef
		if key == "" {
			key = ref.SessionRef
		}
		ev.EventID = "reconcile:" + key + ":" + ev.Kind
		if ev.Metadata == nil {
			ev.Metadata = map[string]string{}
		}
		ev.Metadata["intent_id"], ev.Metadata["marina_id"] = in.ID, scope.ID()
		if err := s.ApplyProviderEvent(ctx, in.ProviderCode, marinaID, ev); err != nil {
			s.d.Log.WarnContext(ctx, "reconcile apply failed", "marina_id", marinaID, "intent_id", in.ID, "err", err)
			continue
		}
		applied++
	}
	refunds, err := store.PendingProviderRefunds(ctx, db, scope, "", now.Add(-ReconcileAfter), ReconcileBatch)
	if err != nil {
		return applied, err
	}
	for _, r := range refunds {
		if err := s.sendRefund(ctx, scope, r.ID); err != nil {
			s.d.Log.WarnContext(ctx, "reconcile refund failed", "marina_id", marinaID, "refund_id", r.ID, "code", providerCode(err))
		}
	}
	return applied, nil
}

// RunIntentsExpire is the job payments.intents_expire: open intents past their expiry become expired (the provider
// session is closed after commit, best effort), payment requests past their expiry become expired, and a transit
// whose link expired goes back to held (or the M3 expiry path when its hold is gone).
func (s *Service) RunIntentsExpire(ctx context.Context) error {
	ids, err := store.MarinaIDsForJobs(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if err := s.ExpireMarina(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ExpireMarina runs the expiry of one marina.
func (s *Service) ExpireMarina(ctx context.Context, marinaID string) error {
	db := s.d.DB.DB
	scope, err := store.SystemMarinaScope(ctx, db, marinaID)
	if err != nil {
		return err
	}
	actor := jobActor("payments.intents_expire")
	type closeReq struct{ provider, session string }
	var toClose []closeReq
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		toClose = toClose[:0]
		now := s.now()
		intents, err := store.ExpiredOpenIntents(ctx, tx, scope, now, 200)
		if err != nil {
			return err
		}
		for i := range intents {
			in, err := store.LockPaymentIntent(ctx, tx, scope, intents[i].ID)
			if err != nil {
				return err
			}
			if err := s.expiredTx(ctx, tx, scope, actor, in); err != nil {
				return err
			}
			if in.ProviderSessionRef != "" {
				toClose = append(toClose, closeReq{in.ProviderCode, in.ProviderSessionRef})
			}
		}
		reqs, err := store.ExpiredOpenRequests(ctx, tx, scope, now, 200)
		if err != nil {
			return err
		}
		for i := range reqs {
			r := &reqs[i]
			ok, err := store.SetRequestStatus(ctx, tx, scope, r.ID, store.RequestExpired, 0, now)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if err := s.audit(ctx, tx, scope, actor, "payment_request.expire", "payment_request", r.ID, map[string]any{"status": store.RequestExpired}); err != nil {
				return err
			}
			if err := s.emit(ctx, tx, scope, event.PaymentRequestChanged, event.PaymentRequestChangedData{RequestID: r.ID, Status: "expired"}); err != nil {
				return err
			}
			live, err := store.LiveIntentsOf(ctx, tx, scope, r.ID, "", "")
			if err != nil {
				return err
			}
			for _, in := range live {
				if in.Status == store.IntentProcessing {
					continue
				}
				if _, err := store.SetIntentStatus(ctx, tx, scope, in.ID, fromCreatedOpen, store.IntentExpired, "", now); err != nil {
					return err
				}
			}
			if r.Source == RequestTransit {
				c, err := transitOf(ctx, tx, scope, r)
				if err != nil {
					return err
				}
				if c != nil {
					if err := s.revertTransitTx(ctx, tx, scope, c.ID); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, c := range toClose {
		e, err := s.providerByCode(ctx, db, scope, c.provider)
		if err != nil {
			continue
		}
		if cctx, err := s.withCreds(ctx, scope, e.row); err == nil {
			_ = e.drv.CloseCheckout(cctx, c.session) // best effort
		}
	}
	return nil
}
