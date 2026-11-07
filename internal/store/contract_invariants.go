package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/uptrace/bun"
)

// CheckContractInvariants runs the M4 contract invariant queries on a marina and returns every violation (none on
// a coherent database). Used by tests, the acceptance scenarios and the demo seed check.
//
//	C31 (INV-31) the live instalments of a contract with a plan sum to its total;
//	C32 (INV-32) a paid instalment has a settlement source and instant;
//	C33 (INV-33) at most one open renewal proposal per source (also a unique index);
//	Q18 (INV-34) an active contract signed through a request points to a ready, signed document;
//	C35 (INV-35) a renewed contract has an active (or later) successor of a signed or completed proposal;
//	C31h (INV-31) the live holder instalments of each source sum to its line: an issued, not excluded management
//	     fee line to its amount_cents, a (common run, customer) pair to the customer's allocations of the run
//	     (sources with an instalment of a cancelled right are skipped: cancelling a right drops its schedule);
//	C40 (INV-40) the holder shares of an active berth right sum to 1000.
func CheckContractInvariants(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	var out []Violation
	add := func(q, detail string, ids ...string) {
		out = append(out, Violation{Query: q, IDs: ids, Detail: detail})
	}

	var sums []struct {
		ID    string `bun:"id"`
		Total int64  `bun:"total_cents"`
		Sum   int64  `bun:"sum_cents"`
	}
	if err := db.NewSelect().TableExpr("contracts AS c").
		ColumnExpr("c.id, c.total_cents, COALESCE((SELECT SUM(i.amount_cents) FROM installments AS i WHERE i.contract_id = c.id AND i.marina_id = c.marina_id AND i.status <> 'cancelled'), 0) AS sum_cents").
		Where("c.marina_id = ?", marinaID).Where("c.installment_plan_version > 0").
		Where("c.status IN ('draft','awaiting_signature','active','suspended','renewed','expired')").
		Scan(ctx, &sums); err != nil {
		return nil, fmt.Errorf("C31: %w", err)
	}
	for _, r := range sums {
		if r.Sum != r.Total {
			add("C31", fmt.Sprintf("Σ instalments %d ≠ total %d", r.Sum, r.Total), r.ID)
		}
	}

	hv, err := holderLineViolations(ctx, db, marinaID)
	if err != nil {
		return nil, err
	}
	out = append(out, hv...)

	var ids []string
	if err := db.NewSelect().TableExpr("installments").Column("id").Where("marina_id = ?", marinaID).
		Where("status = 'paid'").Where("(settlement_source IS NULL OR settled_at IS NULL)").Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("C32: %w", err)
	}
	for _, id := range ids {
		add("C32", "paid without settlement", id)
	}

	ids = nil
	if err := db.NewSelect().TableExpr("renewal_proposals").Column("source_contract_id").Where("marina_id = ?", marinaID).
		Where("status IN (?)", bun.List(OpenProposalStatuses)).GroupExpr("source_contract_id").Having("COUNT(*) > 1").
		Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("C33: %w", err)
	}
	for _, id := range ids {
		add("C33", "more than one open proposal", id)
	}

	ids = nil
	if err := db.NewSelect().TableExpr("contracts AS c").Column("c.id").Where("c.marina_id = ?", marinaID).
		Where("c.status IN ('active','suspended')").Where("c.signature_request_id IS NOT NULL").Where("c.signed_on_paper = ?", false).
		Where(`NOT EXISTS (SELECT 1 FROM generated_documents AS d WHERE d.id = c.signed_document_id AND d.marina_id = c.marina_id
			AND d.status = 'ready' AND d.signed = ?)`, true).
		Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("Q18: %w", err)
	}
	for _, id := range ids {
		add("Q18", "active after signature without a ready signed document", id)
	}

	ids = nil
	if err := db.NewSelect().TableExpr("contracts AS c").Column("c.id").Where("c.marina_id = ?", marinaID).
		Where("c.status = 'renewed'").
		Where(`NOT EXISTS (SELECT 1 FROM renewal_proposals AS p JOIN contracts AS n ON n.id = p.new_contract_id AND n.marina_id = p.marina_id
			WHERE p.source_contract_id = c.id AND p.marina_id = c.marina_id AND p.status IN ('signed','completed')
			AND n.status IN ('active','suspended','renewed','expired','terminated'))`).
		Where(`NOT EXISTS (SELECT 1 FROM audit_log AS a WHERE a.entity_id = CAST(c.id AS TEXT) AND a.action = 'contract.tacit_renew')`).
		Scan(ctx, &ids); err != nil {
		return nil, fmt.Errorf("C35: %w", err)
	}
	for _, id := range ids {
		add("C35", "renewed without an active successor", id)
	}

	var shares []struct {
		ID  string `bun:"id"`
		Sum int64  `bun:"sum_millis"`
	}
	if err := db.NewSelect().TableExpr("berth_rights AS r").
		ColumnExpr("r.id, COALESCE((SELECT SUM(h.share_millis) FROM berth_right_holders AS h WHERE h.right_id = r.id AND h.marina_id = r.marina_id), 0) AS sum_millis").
		Where("r.marina_id = ?", marinaID).Where("r.status = 'active'").Scan(ctx, &shares); err != nil {
		return nil, fmt.Errorf("C40: %w", err)
	}
	for _, r := range shares {
		if r.Sum != 1000 {
			add("C40", fmt.Sprintf("holder shares sum to %d", r.Sum), r.ID)
		}
	}
	return out, nil
}

// holderLineViolations is query C31h of CheckContractInvariants.
func holderLineViolations(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	type row struct {
		Source   string `bun:"source_id"`
		Customer string `bun:"customer_id"`
		Sum      int64  `bun:"sum_cents"`
	}
	var out []Violation
	sums := func(q string, where func(*bun.SelectQuery) *bun.SelectQuery, byCustomer bool) (map[holderSource]int64, error) {
		var rows []row
		cols := "h.source_id, SUM(h.amount_cents) AS sum_cents"
		group := "h.source_id"
		if byCustomer {
			cols, group = "h.source_id, h.customer_id, SUM(h.amount_cents) AS sum_cents", "h.source_id, h.customer_id"
		}
		if err := where(db.NewSelect().TableExpr("holder_installments AS h").ColumnExpr(cols).Where("h.marina_id = ?", marinaID)).
			GroupExpr(group).Scan(ctx, &rows); err != nil {
			return nil, fmt.Errorf("C31h %s: %w", q, err)
		}
		m := make(map[holderSource]int64, len(rows))
		for _, r := range rows {
			m[holderSource{r.Source, r.Customer}] = r.Sum
		}
		return m, nil
	}
	cancelledRight := func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("EXISTS (SELECT 1 FROM berth_rights AS br WHERE br.id = h.right_id AND br.marina_id = h.marina_id AND br.status = 'cancelled')")
	}

	// Management fee lines.
	feeTypes := []string{"management_fee"}
	live, err := sums("fee", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("h.source_type IN (?)", bun.List(feeTypes)).Where("h.status <> 'cancelled'")
	}, false)
	if err != nil {
		return nil, err
	}
	skip, err := sums("fee", func(q *bun.SelectQuery) *bun.SelectQuery {
		return cancelledRight(q.Where("h.source_type IN (?)", bun.List(feeTypes)))
	}, false)
	if err != nil {
		return nil, err
	}
	var lines []row
	if err := db.NewSelect().TableExpr("management_fee_lines AS l").
		Join("JOIN management_fee_runs AS r ON r.id = l.run_id AND r.marina_id = l.marina_id").
		ColumnExpr("l.id AS source_id, l.amount_cents AS sum_cents").
		Where("l.marina_id = ?", marinaID).Where("r.status = 'issued'").Where("l.excluded = ?", false).
		Scan(ctx, &lines); err != nil {
		return nil, fmt.Errorf("C31h fee lines: %w", err)
	}
	want := map[holderSource]int64{}
	for _, l := range lines {
		want[holderSource{source: l.Source}] = l.Sum
	}
	out = append(out, compareHolderSums(want, live, skip, "fee line")...)

	// Common charge runs, per customer.
	commonTypes := []string{"common_advance", "common_balance"}
	live, err = sums("common", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("h.source_type IN (?)", bun.List(commonTypes)).Where("h.status <> 'cancelled'")
	}, true)
	if err != nil {
		return nil, err
	}
	skip, err = sums("common", func(q *bun.SelectQuery) *bun.SelectQuery {
		return cancelledRight(q.Where("h.source_type IN (?)", bun.List(commonTypes)))
	}, true)
	if err != nil {
		return nil, err
	}
	var allocs []row
	if err := db.NewSelect().TableExpr("common_charge_allocations AS a").
		Join("JOIN common_charge_runs AS r ON r.id = a.run_id AND r.marina_id = a.marina_id").
		ColumnExpr("a.run_id AS source_id, a.customer_id, SUM(a.amount_cents) AS sum_cents").
		Where("a.marina_id = ?", marinaID).Where("r.status = 'issued'").Where("a.customer_id IS NOT NULL").
		GroupExpr("a.run_id, a.customer_id").Scan(ctx, &allocs); err != nil {
		return nil, fmt.Errorf("C31h allocations: %w", err)
	}
	want = map[holderSource]int64{}
	for _, a := range allocs {
		want[holderSource{a.Source, a.Customer}] = a.Sum
	}
	out = append(out, compareHolderSums(want, live, skip, "common run")...)
	return out, nil
}

// compareHolderSums reports every source whose live holder instalments differ from the expected amount.
func compareHolderSums(want, live, skip map[holderSource]int64, what string) []Violation {
	var out []Violation
	seen := map[holderSource]bool{}
	check := func(k holderSource) {
		if seen[k] {
			return
		}
		seen[k] = true
		if _, ok := skip[k]; ok {
			return
		}
		if w, g := want[k], live[k]; w != g {
			out = append(out, Violation{Query: "C31h", IDs: k.ids(), Detail: fmt.Sprintf("%s: Σ holder instalments %d ≠ %d", what, g, w)})
		}
	}
	for k := range want {
		check(k)
	}
	for k := range live {
		check(k)
	}
	sort.Slice(out, func(i, j int) bool { return strings.Join(out[i].IDs, "|") < strings.Join(out[j].IDs, "|") })
	return out
}

// holderSource keys C31h: a fee line (customer empty) or a (common run, customer) pair.
type holderSource struct{ source, customer string }

func (k holderSource) ids() []string {
	if k.customer == "" {
		return []string{k.source}
	}
	return []string{k.source, k.customer}
}
