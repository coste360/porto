package charge

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// The charges.backfill job (m5-plan §3.7): issued, partially paid and paid M4 instalments and holder instalments
// without a charge get it once (issued on their issue date), and paid ones a manual payment of the settled method
// matched in full (source backfill, through BackfillPayer). One transaction per instalment, conditional link
// (charge_id IS NULL), idempotent: a second run finds nothing. It also retries instalments the issue hook could not
// charge (origin item missing, target mismatch) once their cause is fixed.

var backfillActor = audit.Actor{Type: audit.ActorSystem, ID: "charges.backfill"}

// BackfillResult counts what a run created.
type BackfillResult struct {
	Charges  int
	Payments int
}

// RunBackfill is the job charges.backfill over every marina.
func (s *Service) RunBackfill(ctx context.Context) error {
	_, err := s.RunBackfillCount(ctx)
	return err
}

// RunBackfillCount is RunBackfill returning the counts.
func (s *Service) RunBackfillCount(ctx context.Context) (BackfillResult, error) {
	var total BackfillResult
	ids, err := store.MarinasWithBillingWork(ctx, s.d.DB.DB)
	if err != nil {
		return total, err
	}
	var errs []error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		r, err := s.BackfillMarina(ctx, scope)
		total.Charges += r.Charges
		total.Payments += r.Payments
		if err != nil {
			errs = append(errs, err)
		}
	}
	return total, errors.Join(errs...)
}

// BackfillMarina backfills one marina in batches of 200.
func (s *Service) BackfillMarina(ctx context.Context, scope store.MarinaScope) (BackfillResult, error) {
	var res BackfillResult
	after := ""
	for {
		rows, err := store.InstallmentsToBackfill(ctx, s.d.DB.DB, scope, after, 200)
		if err != nil {
			return res, err
		}
		for _, r := range rows {
			after = r.ID
			if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				c, err := store.LockContract(ctx, tx, scope, r.ContractID)
				if err != nil {
					return err
				}
				inst, err := store.GetInstallment(ctx, tx, scope, r.ID)
				if err != nil || inst.ChargeID != "" || inst.AmountCents <= 0 {
					return err
				}
				issued := dateOf(ctx, tx, scope, inst.IssuedAt)
				if inst.IssuedAt.IsZero() {
					issued = inst.IssueOn
				}
				ch, created, err := s.installmentChargeTx(ctx, tx, scope, backfillActor, c, inst, issued)
				if err != nil || ch == nil {
					return err
				}
				if created {
					res.Charges++
				}
				if inst.Status == store.InstallmentPaid && created {
					paid, err := s.backfillPaymentTx(ctx, tx, scope, ch, BackfillPayment{
						Method: inst.SettlementMethod, ReceivedOn: inst.SettledOn, RefEnc: inst.SettlementRefEnc, Amount: money.Cents(inst.AmountCents),
					})
					if err != nil {
						return err
					}
					if paid {
						res.Payments++
					}
				}
				if err := s.write(ctx, tx, scope, backfillActor, "charge.backfill", "charge", ch.ID, map[string]any{
					"installment_id": inst.ID, "status": inst.Status,
				}); err != nil {
					return err
				}
				return s.trueUpIfLastTx(ctx, tx, scope, backfillActor, c)
			}); err != nil {
				return res, err
			}
		}
		if len(rows) < 200 {
			break
		}
	}
	after = ""
	for {
		rows, err := store.HolderInstallmentsToBackfill(ctx, s.d.DB.DB, scope, after, 200)
		if err != nil {
			return res, err
		}
		for _, r := range rows {
			after = r.ID
			if err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				if r.RightID != "" {
					if _, err := store.LockBerthRight(ctx, tx, scope, r.RightID); err != nil && !errors.Is(err, store.ErrNotFound) {
						return err
					}
				}
				h, err := store.GetHolderInstallment(ctx, tx, scope, r.ID)
				if err != nil || h.ChargeID != "" {
					return err
				}
				issued := dateOf(ctx, tx, scope, h.IssuedAt)
				if h.IssuedAt.IsZero() {
					issued = h.IssueOn
				}
				ch, err := s.holderChargeTx(ctx, tx, scope, backfillActor, h, issued)
				if err != nil || ch == nil {
					return err
				}
				res.Charges++
				if h.Status == store.InstallmentPaid && ch.TotalCents > 0 {
					paid, err := s.backfillPaymentTx(ctx, tx, scope, ch, BackfillPayment{
						Method: h.SettlementMethod, ReceivedOn: h.SettledOn, RefEnc: h.SettlementRefEnc, Amount: money.Cents(h.AmountCents),
					})
					if err != nil {
						return err
					}
					if paid {
						res.Payments++
					}
				}
				return s.write(ctx, tx, scope, backfillActor, "charge.backfill", "charge", ch.ID, map[string]any{
					"holder_installment_id": h.ID, "status": h.Status,
				})
			}); err != nil {
				return res, err
			}
		}
		if len(rows) < 200 {
			break
		}
	}
	return res, nil
}

// backfillPaymentTx records the historical payment through the payment service (false without one).
func (s *Service) backfillPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, ch *store.Charge, p BackfillPayment) (bool, error) {
	bp, ok := s.credits.(BackfillPayer)
	if !ok || bp == nil {
		return false, nil
	}
	if p.ReceivedOn.IsZero() {
		p.ReceivedOn = ch.IssuedOn
	}
	if p.Method == "" {
		p.Method = "other"
	}
	return true, bp.RecordBackfillPaymentTx(ctx, tx, scope, backfillActor, ch, p)
}
