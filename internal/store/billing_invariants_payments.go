package store

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// checkPaymentsInvariants runs the ledger queries (m5-plan §12):
//
//	B50  (INV-50) the paid amount of every charge is the sum of its matches and the allocated amount of every
//	     payment is the sum of its matches (reversals negative);
//	B52  (INV-52) the refunded amount of every payment is the sum of its refunds that did not fail, and
//	     allocated + refunded never exceed the amount (also CHECKs); deposits are matched only to retentions;
//	B52r a match is reversed by one or more reversal rows whose sum is its reversed_cents and never above its amount
//	     (00017; also payment_matches_reversed_check);
//	B52c the credit of every customer is not negative;
//	B32  (INV-32) an instalment or holder instalment with a charge follows it: paid ⇔ paid, partially_paid ⇔
//	     partially_paid, open charge ⇔ issued instalment.
func checkPaymentsInvariants(ctx context.Context, db bun.IDB, s MarinaScope) ([]Violation, error) {
	var out []Violation
	add := func(q, detail string, ids ...string) {
		out = append(out, Violation{Query: q, IDs: ids, Detail: detail})
	}

	var charges []struct {
		ID   string `bun:"id"`
		Paid int64  `bun:"paid_amount_cents"`
		Sum  int64  `bun:"sum_cents"`
	}
	if err := db.NewSelect().TableExpr("charges AS c").
		ColumnExpr("c.id, c.paid_amount_cents, COALESCE((SELECT SUM(m.amount_cents) FROM payment_matches AS m WHERE m.charge_id = c.id AND m.marina_id = c.marina_id), 0) AS sum_cents").
		Where("c.marina_id = ?", s.ID()).Scan(ctx, &charges); err != nil {
		return nil, fmt.Errorf("B50 charges: %w", err)
	}
	for _, r := range charges {
		if r.Paid != r.Sum {
			add("B50", fmt.Sprintf("charge paid %d ≠ Σ matches %d", r.Paid, r.Sum), r.ID)
		}
	}

	var pays []struct {
		ID        string `bun:"id"`
		Kind      string `bun:"kind"`
		Amount    int64  `bun:"amount_cents"`
		Allocated int64  `bun:"allocated_cents"`
		Refunded  int64  `bun:"refunded_cents"`
		Matched   int64  `bun:"matched_cents"`
		Refunds   int64  `bun:"refunds_cents"`
	}
	if err := db.NewSelect().TableExpr("payments AS p").
		ColumnExpr("p.id, p.kind, p.amount_cents, p.allocated_cents, p.refunded_cents").
		ColumnExpr("COALESCE((SELECT SUM(m.amount_cents) FROM payment_matches AS m WHERE m.payment_id = p.id AND m.marina_id = p.marina_id), 0) AS matched_cents").
		ColumnExpr("COALESCE((SELECT SUM(r.amount_cents) FROM refunds AS r WHERE r.payment_id = p.id AND r.marina_id = p.marina_id AND r.status <> 'failed'), 0) AS refunds_cents").
		Where("p.marina_id = ?", s.ID()).Scan(ctx, &pays); err != nil {
		return nil, fmt.Errorf("B50 payments: %w", err)
	}
	for _, r := range pays {
		if r.Allocated != r.Matched {
			add("B50", fmt.Sprintf("payment allocated %d ≠ Σ matches %d", r.Allocated, r.Matched), r.ID)
		}
		if r.Refunded != r.Refunds {
			add("B52", fmt.Sprintf("payment refunded %d ≠ Σ refunds %d", r.Refunded, r.Refunds), r.ID)
		}
		if r.Allocated < 0 || r.Refunded < 0 || r.Allocated+r.Refunded > r.Amount {
			add("B52", fmt.Sprintf("payment limits allocated %d refunded %d amount %d", r.Allocated, r.Refunded, r.Amount), r.ID)
		}
	}

	var revs []struct {
		ID       string `bun:"id"`
		Amount   int64  `bun:"amount_cents"`
		Reversed int64  `bun:"reversed_cents"`
		Sum      int64  `bun:"sum_cents"`
	}
	if err := db.NewSelect().TableExpr("payment_matches AS m").
		ColumnExpr("m.id, m.amount_cents, m.reversed_cents").
		ColumnExpr("COALESCE((SELECT -SUM(r.amount_cents) FROM payment_matches AS r WHERE r.reverses_match_id = m.id AND r.marina_id = m.marina_id), 0) AS sum_cents").
		Where("m.marina_id = ?", s.ID()).Where("m.reverses_match_id IS NULL").Scan(ctx, &revs); err != nil {
		return nil, fmt.Errorf("B52r matches: %w", err)
	}
	for _, r := range revs {
		if r.Reversed != r.Sum || r.Sum < 0 || r.Sum > r.Amount {
			add("B52r", fmt.Sprintf("match amount %d reversed %d Σ reversals %d", r.Amount, r.Reversed, r.Sum), r.ID)
		}
	}

	var deposits []string
	if err := db.NewSelect().TableExpr("payment_matches AS m").ColumnExpr("DISTINCT m.payment_id").
		Join("JOIN payments AS p ON p.id = m.payment_id AND p.marina_id = m.marina_id").
		Join("JOIN charges AS c ON c.id = m.charge_id AND c.marina_id = m.marina_id").
		Where("m.marina_id = ?", s.ID()).Where("p.kind = 'deposit'").Where("c.origin_type <> 'deposit_retention'").
		Scan(ctx, &deposits); err != nil {
		return nil, fmt.Errorf("B52 deposits: %w", err)
	}
	for _, id := range deposits {
		add("B52", "deposit matched to a charge that is not a retention", id)
	}

	credits, err := CreditsOfCustomers(ctx, db, s, nil)
	if err != nil {
		return nil, fmt.Errorf("B52c: %w", err)
	}
	for id, c := range credits {
		if c.CreditCents < 0 || c.DepositsCents < 0 {
			add("B52c", fmt.Sprintf("credit %d deposits %d", c.CreditCents, c.DepositsCents), id)
		}
	}

	for _, table := range []string{"installments", "holder_installments"} {
		var rows []struct {
			ID           string `bun:"id"`
			Status       string `bun:"status"`
			ChargeStatus string `bun:"charge_status"`
		}
		if err := db.NewSelect().TableExpr(table+" AS i").Join("JOIN charges AS c ON c.id = i.charge_id AND c.marina_id = i.marina_id").
			ColumnExpr("i.id, i.status, c.payment_status AS charge_status").
			Where("i.marina_id = ?", s.ID()).Where("i.status <> 'cancelled'").Where("c.payment_status <> 'cancelled'").Where("c.total_cents > 0").
			Scan(ctx, &rows); err != nil {
			return nil, fmt.Errorf("B32 %s: %w", table, err)
		}
		for _, r := range rows {
			want := map[string]string{ChargeOpen: InstallmentIssued, ChargePartiallyPaid: InstallmentPartiallyPaid, ChargePaid: InstallmentPaid}[r.ChargeStatus]
			if r.Status != want {
				add("B32", fmt.Sprintf("%s %s with charge %s", table, r.Status, r.ChargeStatus), r.ID)
			}
		}
	}
	return out, nil
}

// CheckPaymentsInvariants runs only the ledger queries (B50, B52, B52c, B32) of CheckBillingInvariants.
func CheckPaymentsInvariants(ctx context.Context, db bun.IDB, s MarinaScope) ([]Violation, error) {
	return checkPaymentsInvariants(ctx, db, s)
}
