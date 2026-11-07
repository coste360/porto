package store

import (
	"context"

	"github.com/uptrace/bun"
)

// CheckBillingInvariants runs the M5 billing invariant queries on a marina and returns every violation (none on a
// coherent database). Used by tests, the acceptance scenarios and the demo seed check (m5-plan §12). Each builder
// owns its part:
//
//	charges  (billing_invariants_charges.go)  B51 lines/total coherence, B53 one first export, B54 one live primary
//	                                          charge per origin, B57 charges of an origin sum to its target;
//	payments (billing_invariants_payments.go) B50 paid/allocated = Σ matches, B52 limits, credit ≥ 0, instalment
//	                                          status follows its charge;
//	cash     (billing_invariants_cash.go)     B55 closed sessions reconcile, fiscal numbering unique, B58 reminders.
func CheckBillingInvariants(ctx context.Context, db bun.IDB, s MarinaScope) ([]Violation, error) {
	var out []Violation
	for _, check := range []func(context.Context, bun.IDB, MarinaScope) ([]Violation, error){
		checkChargesInvariants, checkPaymentsInvariants, checkCashInvariants,
	} {
		v, err := check(ctx, db, s)
		if err != nil {
			return nil, err
		}
		out = append(out, v...)
	}
	return out, nil
}
