package charge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// B57 (INV-57): the charges of an origin sum to its target. Registered on the store at init (the store does not
// import domain packages), run by store.CheckBillingInvariants.

func init() { store.RegisterChargeTargetCheck(CheckTargets) }

// CheckTargets compares, for every completed contract and charged transit, the planned lines of its live charges
// with chargeplan's target per line key and per (rate, nature).
func CheckTargets(ctx context.Context, db bun.IDB, scope store.MarinaScope) ([]store.Violation, error) {
	s := &Service{d: Deps{Log: slog.New(slog.DiscardHandler)}}
	cs, err := store.ContractsForTargetCheck(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	var out []store.Violation
	for i := range cs {
		c := &cs[i]
		target, err := s.ContractTargetOf(ctx, db, scope, c)
		if errors.Is(err, chargeplan.ErrTargetMismatch) || errors.Is(err, ErrBillingItemMissing) {
			out = append(out, store.Violation{Query: "B57", IDs: []string{c.ID}, Detail: reasonOf(err)})
			continue
		}
		if err != nil {
			return nil, err
		}
		charged, _, err := chargedOfContract(ctx, db, scope, c.ID)
		if err != nil {
			return nil, err
		}
		if v := diffTarget(target, charged); v != "" {
			out = append(out, store.Violation{Query: "B57", IDs: []string{c.ID}, Detail: v})
		}
	}
	return out, nil
}

// diffTarget describes the first difference between a target and the charged lines ("" when equal).
func diffTarget(target chargeplan.Target, charged []chargeplan.ChargedLine) string {
	for _, r := range chargeplan.Remaining(target, charged) {
		if r.Gross != 0 || r.VAT != 0 {
			return fmt.Sprintf("line %s: gross %d vat %d left to charge", r.Line.Key, r.Gross, r.VAT)
		}
	}
	lines := make([]chargeplan.Line, 0, len(charged))
	for _, c := range charged {
		l := *c.Line
		l.Gross, l.VAT, l.Net = c.GrossCents, c.VATCents, c.GrossCents-c.VATCents
		lines = append(lines, l)
	}
	got := nonZeroBuckets(chargeplan.Totals(lines))
	want := nonZeroBuckets(chargeplan.Totals(target.Lines))
	if !reflect.DeepEqual(got, want) {
		return "totals per (rate, nature) differ from the target"
	}
	return ""
}

func nonZeroBuckets(b []chargeplan.VATBucket) []chargeplan.VATBucket {
	out := []chargeplan.VATBucket{}
	for _, x := range b {
		if x.Gross != 0 || x.VAT != 0 || x.Net != money.Cents(0) {
			out = append(out, x)
		}
	}
	return out
}
