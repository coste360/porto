package deadline

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
)

// InsuranceValid reports whether the vessel has an insurance document valid on the given night (spec 11.5;
// m4-plan §6.8, §15 D29): an M1 document of type insurance_policy of the vessel whose expiry is on or after the
// night (a policy without expiry is valid). contract calls it for the insurance policy (warn or block) of contract
// creation, quote acceptance, renewals and tacit renewals. It reads only; the caller has authorised scope.
func InsuranceValid(ctx context.Context, db bun.IDB, scope store.MarinaScope, vesselID string, on calendar.Date) (bool, error) {
	if vesselID == "" {
		return false, nil
	}
	until, err := store.InsuranceUntil(ctx, db, scope, []string{vesselID})
	if err != nil {
		return false, err
	}
	exp, ok := until[vesselID]
	return ok && !exp.Before(on), nil
}
