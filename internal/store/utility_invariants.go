package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/qty"
)

// ConsumptionCorrectionPrefix starts the share_key of a correction delta (m9-plan §9.4, D23): the reallocation of a
// charged interval after a confirmed reading inside it. Corrections are left out of Q22.
const ConsumptionCorrectionPrefix = "corr:"

// CheckUtilityInvariants runs the M9 maintenance and utilities invariant queries on a marina and returns every
// violation (none on a coherent database). Used by tests, the acceptance scenarios and the demo seed check.
//
//	Q21 (INV-90) a charged consumption points to a live charge of its billing, a pending one to none, and a billing
//	    has exactly one charge (never a billing without a charge) and at most one live primary charge;
//	Q22 (INV-91) the shares of an interval between two readings sum to the reading difference (× multiplier, half
//	    even; a negative difference without rollover counts as zero), lie inside its nights and cover all of them;
//	Q23 (INV-92) every active maintenance allocation belongs to a scheduled, in-progress or done task (a cancelled
//	    task keeps only its past nights: cancelling releases the nights from the day of the cancellation).
func CheckUtilityInvariants(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	var out []Violation
	add := func(q, detail string, ids ...string) {
		out = append(out, Violation{Query: q, IDs: ids, Detail: detail})
	}
	if err := checkQ21(ctx, db, marinaID, add); err != nil {
		return nil, fmt.Errorf("Q21: %w", err)
	}
	if err := checkQ22(ctx, db, marinaID, add); err != nil {
		return nil, fmt.Errorf("Q22: %w", err)
	}
	if err := checkQ23(ctx, db, marinaID, add); err != nil {
		return nil, fmt.Errorf("Q23: %w", err)
	}
	return out, nil
}

func checkQ21(ctx context.Context, db bun.IDB, marinaID string, add func(string, string, ...string)) error {
	var bad []struct {
		ID     string `bun:"id"`
		Detail string `bun:"detail"`
	}
	err := db.NewSelect().TableExpr("utility_consumptions AS c").
		ColumnExpr("CAST(c.id AS TEXT) AS id").
		ColumnExpr(`CASE WHEN ch.id IS NULL THEN 'charge missing'
			WHEN ch.payment_status = 'cancelled' THEN 'charge cancelled'
			WHEN b.id IS NULL OR b.charge_id IS NULL OR b.charge_id <> c.charge_id THEN 'billing mismatch'
			ELSE 'origin mismatch' END AS detail`).
		Join("LEFT JOIN charges AS ch ON ch.id = c.charge_id AND ch.marina_id = c.marina_id").
		Join("LEFT JOIN utility_billings AS b ON b.id = c.billing_id AND b.marina_id = c.marina_id").
		Where("c.marina_id = ?", marinaID).Where("c.status = ?", ConsumptionCharged).
		Where(`(ch.id IS NULL OR ch.payment_status = 'cancelled' OR b.id IS NULL OR b.charge_id IS NULL OR b.charge_id <> c.charge_id
			OR ch.origin_type <> 'utility' OR CAST(ch.origin_id AS TEXT) <> CAST(b.id AS TEXT))`).
		Scan(ctx, &bad)
	if err != nil {
		return err
	}
	for _, r := range bad {
		add("Q21", r.Detail, r.ID)
	}
	var ids []string
	if err := db.NewSelect().TableExpr("utility_consumptions").ColumnExpr("CAST(id AS TEXT)").Where("marina_id = ?", marinaID).
		Where("status <> ?", ConsumptionCharged).Where("(charge_id IS NOT NULL OR billing_id IS NOT NULL)").Scan(ctx, &ids); err != nil {
		return err
	}
	for _, id := range ids {
		add("Q21", "not charged but linked to a charge", id)
	}
	var orphan []string
	if err := db.NewSelect().TableExpr("utility_billings").ColumnExpr("CAST(id AS TEXT)").Where("marina_id = ?", marinaID).
		Where("charge_id IS NULL").Scan(ctx, &orphan); err != nil {
		return err
	}
	for _, id := range orphan {
		add("Q21", "billing without a charge", id)
	}
	var dup []string
	if err := db.NewSelect().TableExpr("charges").ColumnExpr("CAST(origin_id AS TEXT)").Where("marina_id = ?", marinaID).
		Where("origin_type = ?", ChargeOriginUtility).Where("corrects_charge_id IS NULL").Where("payment_status <> 'cancelled'").
		GroupExpr("origin_id").Having("COUNT(*) > 1").Scan(ctx, &dup); err != nil {
		return err
	}
	for _, id := range dup {
		add("Q21", "two live primary charges for one billing", id)
	}
	return nil
}

func checkQ22(ctx context.Context, db bun.IDB, marinaID string, add func(string, string, ...string)) error {
	var rows []UtilityConsumption
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", marinaID).OrderExpr("from_reading_id, to_reading_id, first_night").Scan(ctx); err != nil {
		return err
	}
	type key struct{ from, to string }
	groups := map[key][]UtilityConsumption{}
	var order []key
	readingIDs := map[string]bool{}
	meterIDs := map[string]bool{}
	for _, c := range rows {
		if strings.HasPrefix(c.ShareKey, ConsumptionCorrectionPrefix) {
			continue
		}
		k := key{c.FromReadingID, c.ToReadingID}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], c)
		readingIDs[c.FromReadingID], readingIDs[c.ToReadingID] = true, true
		meterIDs[c.MeterID] = true
	}
	if len(order) == 0 {
		return nil
	}
	var readings []MeterReading
	if err := db.NewSelect().Model(&readings).Where("marina_id = ?", marinaID).Where("id IN (?)", bun.List(utilitySortedKeys(readingIDs))).Scan(ctx); err != nil {
		return err
	}
	var meters []UtilityMeter
	if err := db.NewSelect().Model(&meters).Where("marina_id = ?", marinaID).Where("id IN (?)", bun.List(utilitySortedKeys(meterIDs))).Scan(ctx); err != nil {
		return err
	}
	rd := map[string]MeterReading{}
	for _, r := range readings {
		rd[r.ID] = r
	}
	mt := map[string]UtilityMeter{}
	for _, m := range meters {
		mt[m.ID] = m
	}
	for _, k := range order {
		g := groups[k]
		prev, cur, m := rd[k.from], rd[k.to], mt[g[0].MeterID]
		diff := cur.ValueMilli - prev.ValueMilli
		if diff < 0 && m.RolloverMilli != nil {
			diff = *m.RolloverMilli - prev.ValueMilli + cur.ValueMilli
		}
		var want qty.Milli
		if diff > 0 {
			v, err := qty.Milli(diff).Mul(qty.Milli(m.MultiplierMilli))
			if err != nil {
				return err
			}
			want = v
		}
		var sum int64
		ids := make([]string, 0, len(g))
		first, last := prev.ReadOn, cur.ReadOn.AddDays(-1)
		if last.Before(first) {
			last = first
		}
		covered := make([]bool, first.DaysUntil(last)+1)
		inside := true
		for _, c := range g {
			sum += c.QtyMilli
			ids = append(ids, c.ID)
			if c.FirstNight.Before(first) || c.LastNight.After(last) {
				inside = false
				continue
			}
			for i := first.DaysUntil(c.FirstNight); i <= first.DaysUntil(c.LastNight); i++ {
				covered[i] = true
			}
		}
		if sum != int64(want) {
			add("Q22", fmt.Sprintf("Σ shares %d ≠ Δ %d", sum, want), ids...)
		}
		if !inside || slices.Contains(covered, false) {
			add("Q22", "shares do not cover the nights of the interval", ids...)
		}
	}
	return nil
}

func checkQ23(ctx context.Context, db bun.IDB, marinaID string, add func(string, string, ...string)) error {
	var allocs []string
	if err := db.NewSelect().TableExpr("allocations").ColumnExpr("CAST(id AS TEXT)").Where("marina_id = ?", marinaID).
		Where("kind = ?", AllocKindMaintenance).Where("status = ?", AllocActive).OrderExpr("id").Scan(ctx, &allocs); err != nil {
		return err
	}
	if len(allocs) == 0 {
		return nil
	}
	var tasks []MaintenanceTask
	if err := db.NewSelect().Model(&tasks).Column("id", "allocation_ids").Where("marina_id = ?", marinaID).
		Where("status IN (?)", bun.List([]string{TaskScheduled, TaskInProgress, TaskDone, TaskCancelled})).Scan(ctx); err != nil {
		return err
	}
	owned := map[string]bool{}
	for _, t := range tasks {
		var ids []string
		_ = json.Unmarshal(t.AllocationIDs, &ids)
		for _, id := range ids {
			owned[id] = true
		}
	}
	for _, id := range allocs {
		if !owned[id] {
			add("Q23", "maintenance allocation without its task", id)
		}
	}
	return nil
}

func utilitySortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
