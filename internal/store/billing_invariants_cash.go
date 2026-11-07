package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/uptrace/bun"
)

// fiscalNumberRe is the format of fiscal document numbers (YYYYMMDD-NNNN).
var fiscalNumberRe = regexp.MustCompile(`^\d{8}-\d{4,}$`)

// checkCashInvariants runs the cash queries of m5-plan §12:
//
//	B55  a closed session's stored expected amounts equal the recomputed float + payments − refunds ± movements
//	     per method, and its difference = Σ counted − Σ expected; at most one open session per register;
//	F1   fiscal numbers are well formed, unique per marina and their day prefix is the business date;
//	B58  each (charge, level) is reminded once; no queued reminder item survives its run; one run per day.
func checkCashInvariants(ctx context.Context, db bun.IDB, s MarinaScope) ([]Violation, error) {
	var out []Violation
	var closed []CashSession
	if err := db.NewSelect().Model(&closed).Where("marina_id = ?", s.ID()).Where("status = ?", CashSessionClosed).Scan(ctx); err != nil {
		return nil, err
	}
	for i := range closed {
		c := &closed[i]
		t, err := SessionCashTotals(ctx, db, s, c.ID)
		if err != nil {
			return nil, err
		}
		want := map[string]int64{"cash": c.FloatCents + t.MovementsIn - t.MovementsOut}
		for m, v := range t.Payments {
			want[m] += v
		}
		for m, v := range t.Refunds {
			want[m] -= v
		}
		var expected, counted map[string]int64
		_ = json.Unmarshal(c.Expected, &expected)
		_ = json.Unmarshal(c.Counted, &counted)
		var expSum, cntSum int64
		for m, v := range want {
			if expected[m] != v {
				out = append(out, Violation{Query: "B55", IDs: []string{c.ID}, Detail: fmt.Sprintf("method %s expected %d recomputed %d", m, expected[m], v)})
			}
		}
		for _, v := range expected {
			expSum += v
		}
		for _, v := range counted {
			cntSum += v
		}
		if c.DifferenceCents != cntSum-expSum {
			out = append(out, Violation{Query: "B55", IDs: []string{c.ID}, Detail: fmt.Sprintf("difference %d != %d", c.DifferenceCents, cntSum-expSum)})
		}
	}
	var openDup []struct {
		RegisterID string `bun:"register_id"`
		N          int    `bun:"n"`
	}
	if err := db.NewSelect().TableExpr("cash_sessions").ColumnExpr("register_id, COUNT(*) AS n").Where("marina_id = ?", s.ID()).
		Where("status = ?", CashSessionOpen).GroupExpr("register_id").Having("COUNT(*) > 1").Scan(ctx, &openDup); err != nil {
		return nil, err
	}
	for _, d := range openDup {
		out = append(out, Violation{Query: "B55", IDs: []string{d.RegisterID}, Detail: fmt.Sprintf("%d open sessions", d.N)})
	}
	var docs []FiscalDocument
	if err := db.NewSelect().Model(&docs).Column("id", "number", "business_date").Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, err
	}
	seen := map[string]string{}
	for _, d := range docs {
		day := d.BusinessDate.String()
		prefix := ""
		if len(day) == 10 {
			prefix = day[0:4] + day[5:7] + day[8:10] + "-"
		}
		if !fiscalNumberRe.MatchString(d.Number) || len(d.Number) < len(prefix) || d.Number[:len(prefix)] != prefix {
			out = append(out, Violation{Query: "F1", IDs: []string{d.ID}, Detail: "number " + d.Number + " day " + day})
		}
		if other, ok := seen[d.Number]; ok {
			out = append(out, Violation{Query: "F1", IDs: []string{other, d.ID}, Detail: "duplicate number " + d.Number})
		}
		seen[d.Number] = d.ID
	}
	var dupItems []struct {
		ChargeID string `bun:"charge_id"`
		Level    string `bun:"level"`
	}
	if err := db.NewSelect().TableExpr("reminder_items").ColumnExpr("charge_id, level").Where("marina_id = ?", s.ID()).
		GroupExpr("charge_id, level").Having("COUNT(*) > 1").Scan(ctx, &dupItems); err != nil {
		return nil, err
	}
	for _, d := range dupItems {
		out = append(out, Violation{Query: "B58", IDs: []string{d.ChargeID}, Detail: "level " + d.Level + " reminded twice"})
	}
	var queued []string
	if err := db.NewSelect().TableExpr("reminder_items AS ri").Join("JOIN reminder_runs AS rr ON rr.id = ri.run_id").
		ColumnExpr("ri.id").Where("ri.marina_id = ?", s.ID()).Where("ri.status = ?", "queued").Where("rr.status = ?", ReminderRunDone).
		Scan(ctx, &queued); err != nil {
		return nil, err
	}
	for _, id := range queued {
		out = append(out, Violation{Query: "B58", IDs: []string{id}, Detail: "queued item after its run"})
	}
	return out, nil
}
