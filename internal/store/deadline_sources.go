package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Read-only source queries of the deadline engine (m4-plan §6.8, §15 D28). Each returns the rows that feed a kind
// with the state the deadline should have: open (with its due date), done (the source was settled or ended) or
// dismissed (cancelled). Timestamps are returned as instants; the engine converts them to marina dates.

// Deadline source states.
const (
	SourceOpen      = "open"
	SourceDone      = "done"
	SourceDismissed = "dismissed"
)

// DeadlineSource is one source row of a deadline kind.
type DeadlineSource struct {
	SourceType string        `bun:"source_type"`
	SourceID   string        `bun:"source_id"`
	Kind       string        `bun:"kind"`
	CustomerID string        `bun:"customer_id"`
	VesselID   string        `bun:"vessel_id"`
	DueOn      calendar.Date `bun:"due_on"`
	At         time.Time     `bun:"at"` // instant sources (offers, failures, expiries)
	Status     string        `bun:"status"`
	State      string        `bun:"-"`
}

func sourceRows(ctx context.Context, q *bun.SelectQuery) ([]DeadlineSource, error) {
	var out []DeadlineSource
	err := q.Scan(ctx, &out)
	return out, err
}

func idText(col string) string { return "coalesce(CAST(" + col + " AS TEXT), '')" }

// ContractEndSources returns the mooring and charter contracts with their end date.
func ContractEndSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("contracts AS c").
		ColumnExpr("'contract' AS source_type, CAST(c.id AS TEXT) AS source_id, 'contract_end' AS kind").
		ColumnExpr(idText("c.customer_id")+" AS customer_id, "+idText("c.vessel_id")+" AS vessel_id").
		ColumnExpr("c.ends_on AS due_on, c.status AS status").
		Where("c.marina_id = ?", s.ID()).Where("c.kind IN (?)", bun.List([]string{ContractKindMooring, ContractKindCharter})).
		Where("c.status IN (?)", bun.List([]string{"active", "suspended", "expired", "renewed", "terminated", "cancelled"}))
	return sourceRows(ctx, q)
}

// InstallmentSources returns the instalments of the current plan of every contract.
func InstallmentSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("installments AS i").
		ColumnExpr("'installment' AS source_type, CAST(i.id AS TEXT) AS source_id, 'installment_due' AS kind").
		ColumnExpr(idText("c.customer_id")+" AS customer_id, "+idText("c.vessel_id")+" AS vessel_id").
		ColumnExpr("i.due_on AS due_on, i.status AS status").
		Join("JOIN contracts AS c ON c.id = i.contract_id").
		Where("i.marina_id = ?", s.ID()).Where("i.plan_version = c.installment_plan_version")
	return sourceRows(ctx, q)
}

// HolderInstallmentSources returns the holder instalments (fees and common charges).
func HolderInstallmentSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("holder_installments AS h").
		ColumnExpr("'holder_installment' AS source_type, CAST(h.id AS TEXT) AS source_id, 'holder_installment_due' AS kind").
		ColumnExpr(idText("h.customer_id")+" AS customer_id, '' AS vessel_id, h.due_on AS due_on, h.status AS status").
		Where("h.marina_id = ?", s.ID())
	return sourceRows(ctx, q)
}

// RightEndSources returns the berth rights with their last night.
func RightEndSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("berth_rights AS r").
		ColumnExpr("'right' AS source_type, CAST(r.id AS TEXT) AS source_id, 'right_end' AS kind").
		ColumnExpr("'' AS customer_id, '' AS vessel_id, r.to_on AS due_on, r.status AS status").
		Where("r.marina_id = ?", s.ID()).Where("r.to_on IS NOT NULL")
	return sourceRows(ctx, q)
}

// MandateEndSources returns the management mandates with their last night.
func MandateEndSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("management_mandates AS m").
		ColumnExpr("'mandate' AS source_type, CAST(m.id AS TEXT) AS source_id, 'mandate_end' AS kind").
		ColumnExpr("'' AS customer_id, '' AS vessel_id, m.to_on AS due_on, m.status AS status").
		Where("m.marina_id = ?", s.ID())
	return sourceRows(ctx, q)
}

// ConcessionSources returns the concession expiry and the obligations.
func ConcessionSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	a, err := sourceRows(ctx, db.NewSelect().TableExpr("concessions AS c").
		ColumnExpr("'concession' AS source_type, CAST(c.id AS TEXT) AS source_id, 'concession_expiry' AS kind").
		ColumnExpr("'' AS customer_id, '' AS vessel_id, c.expires_on AS due_on, c.status AS status").
		Where("c.marina_id = ?", s.ID()).Where("c.expires_on IS NOT NULL"))
	if err != nil {
		return nil, err
	}
	b, err := sourceRows(ctx, db.NewSelect().TableExpr("concession_obligations AS o").
		ColumnExpr("'concession_obligation' AS source_type, CAST(o.id AS TEXT) AS source_id, 'concession_obligation' AS kind").
		ColumnExpr("'' AS customer_id, '' AS vessel_id, o.due_on AS due_on").
		ColumnExpr("CASE WHEN o.done_on IS NULL THEN 'open' ELSE 'done' END AS status").
		Where("o.marina_id = ?", s.ID()))
	return append(a, b...), err
}

// RenewalDeadlineSources returns the renewal proposals with their reply deadline.
func RenewalDeadlineSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("renewal_proposals AS p").
		ColumnExpr("'renewal_proposal' AS source_type, CAST(p.id AS TEXT) AS source_id, 'renewal_deadline' AS kind").
		ColumnExpr(idText("c.customer_id")+" AS customer_id, "+idText("c.vessel_id")+" AS vessel_id").
		ColumnExpr("p.deadline_on AS due_on, p.status AS status").
		Join("JOIN contracts AS c ON c.id = p.source_contract_id").
		Where("p.marina_id = ?", s.ID()).Where("p.status <> ?", "previewed")
	return sourceRows(ctx, q)
}

// WaitlistOfferSources returns the waiting-list entries with their offer time.
func WaitlistOfferSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("waitlist_entries AS w").
		ColumnExpr("'waitlist_entry' AS source_type, CAST(w.id AS TEXT) AS source_id, 'waitlist_offer' AS kind").
		ColumnExpr(idText("w.customer_id")+" AS customer_id, "+idText("w.vessel_id")+" AS vessel_id").
		ColumnExpr("coalesce(w.offered_at, w.updated_at) AS at, w.status AS status").
		Where("w.marina_id = ?", s.ID()).Where("w.status <> ?", "waiting")
	return sourceRows(ctx, q)
}

// FailedDocumentSources returns the generated documents that failed (and those retried since).
func FailedDocumentSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("generated_documents AS d").
		ColumnExpr("'generated_document' AS source_type, CAST(d.id AS TEXT) AS source_id, 'document_failed' AS kind").
		ColumnExpr("'' AS customer_id, '' AS vessel_id, d.updated_at AS at, d.status AS status").
		Where("d.marina_id = ?", s.ID()).Where("(d.status = ? OR d.error_code IS NOT NULL)", DocFailed)
	return sourceRows(ctx, q)
}

// SignatureExpirySources returns the signature requests with their expiry.
func SignatureExpirySources(ctx context.Context, db bun.IDB, s MarinaScope) ([]DeadlineSource, error) {
	q := db.NewSelect().TableExpr("signature_requests AS r").
		ColumnExpr("'signature_request' AS source_type, CAST(r.id AS TEXT) AS source_id, 'signature_expiring' AS kind").
		ColumnExpr("'' AS customer_id, '' AS vessel_id, r.expires_at AS at, r.status AS status").
		Where("r.marina_id = ?", s.ID())
	return sourceRows(ctx, q)
}

// EngineDeadlines returns the deadlines of the engine kinds of a marina.
func EngineDeadlines(ctx context.Context, db bun.IDB, s MarinaScope, kinds []string) ([]Deadline, error) {
	var out []Deadline
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("kind IN (?)", bun.List(kinds)).
		OrderExpr("id").Scan(ctx)
	return out, err
}

// InsertDeadlineIfAbsent inserts a deadline unless its (source_type, source_id, kind) exists; false when it did.
func InsertDeadlineIfAbsent(ctx context.Context, db bun.IDB, d *Deadline) (bool, error) {
	res, err := db.NewInsert().Model(d).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// SetDeadlineNotified records the lead days already notified (conditional on the version).
func SetDeadlineNotified(ctx context.Context, db bun.IDB, s MarinaScope, id string, version int, notified JSON, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("deadlines").Set("notified_at = ?", notified).Set("version = version + 1").
		Set("updated_at = ?", UTC(at)).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", version).
		Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// OpenDeadlinesDue returns the open deadlines of a marina due on or before until.
func OpenDeadlinesDue(ctx context.Context, db bun.IDB, s MarinaScope, until calendar.Date) ([]Deadline, error) {
	var out []Deadline
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", "open").
		Where("due_on <= ?", until).OrderExpr("due_on, id").Scan(ctx)
	return out, err
}

// ActiveMarinaIDs returns the ids of the marinas that are not archived.
func ActiveMarinaIDs(ctx context.Context, db bun.IDB) ([]string, error) {
	var ids []string
	err := db.NewSelect().Table("marinas").Column("id").Where("archived_at IS NULL").OrderExpr("id").Scan(ctx, &ids)
	return ids, err
}
