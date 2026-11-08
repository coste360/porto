package booking

import (
	"context"
	"errors"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/store"
)

// approvalBatch bounds the bookings rejected per marina and run.
const approvalBatch = 200

// RunApprovalExpiry rejects the awaiting_approval transits whose deadline has passed (reason approval_timeout)
// and voids their authorisations (job bookings.approval_expire, every minute). Each booking is its own
// transaction: RejectTx locks the contract and re-checks its state, so an approval committed meanwhile wins.
func (s *Service) RunApprovalExpiry(ctx context.Context) error {
	db := s.d.DB.DB
	ids, err := store.MarinaIDsForJobs(ctx, db)
	if err != nil {
		return err
	}
	who := contract.JobActor(JobApprovalExpire)
	var errs []error
	for _, mid := range ids {
		scope, err := store.SystemMarinaScope(ctx, db, mid)
		if err != nil {
			continue
		}
		due, err := store.ApprovalsPastDeadline(ctx, db, scope, s.now(), approvalBatch)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, cid := range due {
			void, err := s.rejectTx(ctx, scope, who, cid, 0, contract.ActionExpire, "approval_timeout", true)
			if errors.Is(err, contract.ErrInvalidTransition) {
				continue // approved or cancelled meanwhile
			}
			if err != nil {
				s.d.Log.ErrorContext(ctx, "approval expiry failed", "marina_id", mid, "contract_id", cid, "err", err)
				errs = append(errs, err)
				continue
			}
			if void != "" {
				if err := s.d.Collect.VoidIntent(ctx, scope, void); err != nil {
					s.d.Log.WarnContext(ctx, "booking void failed", "marina_id", mid, "contract_id", cid, "err", err)
				}
			}
		}
	}
	return errors.Join(errs...)
}

// RunCaptureRetry retries approved but not captured intents and pending voids (job bookings.capture, every
// minute).
func (s *Service) RunCaptureRetry(ctx context.Context) error {
	if s.d.Collect == nil {
		return nil
	}
	return s.d.Collect.RunAuthorizations(ctx)
}
