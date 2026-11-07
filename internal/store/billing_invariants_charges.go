package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/uptrace/bun"
)

// Charge invariant queries (m5-plan §12), owned by CHARGES-EXPORT:
//
//	B51 (INV-51) a charge's totals equal the sums of its lines (total = Σ gross, net = Σ net, vat = Σ vat), every
//	    line has net + vat = gross, and a charge has at least one line; an exported or invoiced charge has a
//	    first export (or is an opening balance);
//	B53 (INV-53) each charge has at most one first export item, a to_invoice charge has none, an exported charge
//	    has exactly one and its first_export_id is that export; opening balances are never exported first;
//	    cancelled charges are never in a first export;
//	B54 (INV-54) at most one live primary charge per origin of the unique kinds;
//	B57 (INV-57) the charges of a completed contract or a charged transit sum to its target per line key and per
//	    (rate, nature): the check needs the pure planner and runs through ChargeTargetCheck, registered by
//	    internal/domain/charge (the store never imports domain packages).
func checkChargesInvariants(ctx context.Context, db bun.IDB, s MarinaScope) ([]Violation, error) {
	var out []Violation
	add := func(q, detail string, ids ...string) {
		out = append(out, Violation{Query: q, IDs: ids, Detail: detail})
	}

	var sums []struct {
		ID    string `bun:"id"`
		Total int64  `bun:"total_cents"`
		Net   int64  `bun:"net_cents"`
		VAT   int64  `bun:"vat_cents"`
		G     int64  `bun:"g"`
		N     int64  `bun:"n"`
		V     int64  `bun:"v"`
		Lines int    `bun:"lines"`
	}
	if err := db.NewSelect().TableExpr("charges AS c").
		ColumnExpr("c.id, c.total_cents, c.net_cents, c.vat_cents").
		ColumnExpr("COALESCE(SUM(l.gross_cents), 0) AS g, COALESCE(SUM(l.net_cents), 0) AS n, COALESCE(SUM(l.vat_cents), 0) AS v, COUNT(l.id) AS lines").
		Join("LEFT JOIN charge_lines AS l ON l.charge_id = c.id").
		Where("c.marina_id = ?", s.ID()).GroupExpr("c.id, c.total_cents, c.net_cents, c.vat_cents").
		Scan(ctx, &sums); err != nil {
		return nil, fmt.Errorf("B51: %w", err)
	}
	for _, r := range sums {
		switch {
		case r.Lines == 0:
			add("B51", "charge without lines", r.ID)
		case r.G != r.Total || r.N != r.Net || r.V != r.VAT:
			add("B51", fmt.Sprintf("totals %d/%d/%d ≠ lines %d/%d/%d", r.Total, r.Net, r.VAT, r.G, r.N, r.V), r.ID)
		case r.Net+r.VAT != r.Total:
			add("B51", "net + vat ≠ total", r.ID)
		}
	}

	var firsts []struct {
		ID      string `bun:"id"`
		Status  string `bun:"invoicing_status"`
		Payment string `bun:"payment_status"`
		Origin  string `bun:"origin_type"`
		FirstID string `bun:"first_export_id"`
		Items   int    `bun:"items"`
		ItemExp string `bun:"item_export"`
	}
	if err := db.NewSelect().TableExpr("charges AS c").
		ColumnExpr("c.id, c.invoicing_status, c.payment_status, c.origin_type, COALESCE(CAST(c.first_export_id AS TEXT), '') AS first_export_id").
		ColumnExpr("COUNT(i.id) AS items, COALESCE(MAX(CAST(i.export_id AS TEXT)), '') AS item_export").
		Join("LEFT JOIN charge_export_items AS i ON i.charge_id = c.id AND i.is_first = ?", true).
		Where("c.marina_id = ?", s.ID()).
		GroupExpr("c.id, c.invoicing_status, c.payment_status, c.origin_type, c.first_export_id").
		Scan(ctx, &firsts); err != nil {
		return nil, fmt.Errorf("B53: %w", err)
	}
	for _, r := range firsts {
		switch {
		case r.Items > 1:
			add("B53", fmt.Sprintf("%d first exports", r.Items), r.ID)
		case r.Status == ChargeToInvoice && r.Items != 0:
			add("B53", "to_invoice charge in a first export", r.ID)
		case r.Origin == ChargeOriginOpeningBalance && r.Items != 0:
			add("B53", "opening balance exported", r.ID)
		case r.Status != ChargeToInvoice && r.Origin != ChargeOriginOpeningBalance && r.Items != 1:
			add("B53", r.Status+" charge without first export", r.ID)
		case r.Items == 1 && r.FirstID != r.ItemExp:
			add("B53", "first_export_id differs from the first item", r.ID)
		}
	}

	var dups []struct {
		Origin string `bun:"origin_type"`
		ID     string `bun:"origin_id"`
		N      int    `bun:"n"`
	}
	if err := db.NewSelect().Table("charges").ColumnExpr("origin_type, origin_id, COUNT(*) AS n").
		Where("marina_id = ?", s.ID()).Where("corrects_charge_id IS NULL").Where("payment_status <> ?", ChargeCancelled).
		Where("origin_type IN (?)", bun.List([]string{
			ChargeOriginInstallment, ChargeOriginHolderInstallment, ChargeOriginTransit,
			ChargeOriginTermination, ChargeOriginDepositRetention,
		})).
		GroupExpr("origin_type, origin_id").Having("COUNT(*) > 1").Scan(ctx, &dups); err != nil {
		return nil, fmt.Errorf("B54: %w", err)
	}
	for _, d := range dups {
		add("B54", fmt.Sprintf("%d live primary charges of %s", d.N, d.Origin), d.ID)
	}

	if check := chargeTargetCheck(); check != nil {
		v, err := check(ctx, db, s)
		if err != nil {
			return nil, fmt.Errorf("B57: %w", err)
		}
		out = append(out, v...)
	}
	return out, nil
}

// ChargeTargetCheck runs B57 (registered by internal/domain/charge).
type ChargeTargetCheck func(ctx context.Context, db bun.IDB, s MarinaScope) ([]Violation, error)

var (
	targetCheckMu sync.RWMutex
	targetCheck   ChargeTargetCheck
)

// RegisterChargeTargetCheck registers the B57 implementation.
func RegisterChargeTargetCheck(f ChargeTargetCheck) {
	targetCheckMu.Lock()
	defer targetCheckMu.Unlock()
	targetCheck = f
}

func chargeTargetCheck() ChargeTargetCheck {
	targetCheckMu.RLock()
	defer targetCheckMu.RUnlock()
	return targetCheck
}

// ContractsForTargetCheck returns the contracts whose charges must sum to their target: mooring contracts with a
// plan in a live or completed status and no scheduled instalment left, and transits confirmed, arrived or departed;
// both with at least one live planned charge.
func ContractsForTargetCheck(ctx context.Context, db bun.IDB, s MarinaScope) ([]Contract, error) {
	out := []Contract{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.WhereGroup(" OR ", func(q *bun.SelectQuery) *bun.SelectQuery {
				return q.Where("kind <> ?", ContractKindTransit).Where("installment_plan_version > 0").
					Where("status IN ('active','suspended','renewed','expired')").
					Where("NOT EXISTS (SELECT 1 FROM installments AS i WHERE i.contract_id = contract.id AND i.status = 'scheduled')")
			}).WhereGroup(" OR ", func(q *bun.SelectQuery) *bun.SelectQuery {
				return q.Where("kind = ?", ContractKindTransit).Where("status IN ('confirmed','arrived','departed')")
			})
		}).
		Where("EXISTS (SELECT 1 FROM charges AS ch WHERE ch.contract_id = contract.id AND ch.payment_status <> 'cancelled' AND ch.origin_type IN ('installment','transit'))").
		OrderExpr("id").Scan(ctx)
	return out, err
}
