package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Office worklist (spec 28.3, m5-plan §4.5, §16 D25): items are computed from their sources and merged with
// work_item_states (assignee, status). Sources without a table of their own (late payments, overpayments,
// billing_item_missing, ...) are recorded as a state row whose source_type is the item kind (RaiseWorkItem).

// Work item statuses.
const (
	WorkItemOpen       = "open"
	WorkItemInProgress = "in_progress"
	WorkItemDone       = "done"
	WorkItemDismissed  = "dismissed"
)

// RaiseWorkItem records a worklist item of a source without its own table (source_type = the item kind, e.g.
// late_payment with the payment id). occurrence names this occurrence of the signal (e.g. the suspension of an
// arrears cycle, the provider event of a dispute): raising the same occurrence again leaves the row as it is (a
// dismissed item stays dismissed), while a new occurrence of a source whose item is done or dismissed reopens it,
// so that a recurring signal is never lost.
func RaiseWorkItem(ctx context.Context, db bun.IDB, s MarinaScope, sourceType, sourceID, occurrence string, now time.Time) error {
	w := &WorkItemState{
		ID: NewID(), MarinaID: s.ID(), SourceType: sourceType, SourceID: sourceID, Status: WorkItemOpen,
		Occurrence: occurrence, Version: 1, CreatedAt: UTC(now), UpdatedAt: UTC(now),
	}
	res, err := db.NewInsert().Model(w).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil || affected(res) == 1 {
		return err
	}
	_, err = db.NewUpdate().Model((*WorkItemState)(nil)).Set("occurrence = ?", occurrence).
		Set("status = CASE WHEN status IN (?, ?) THEN ? ELSE status END", WorkItemDone, WorkItemDismissed, WorkItemOpen).
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("source_type = ?", sourceType).Where("source_id = ?", sourceID).
		Where("occurrence <> ?", occurrence).Exec(ctx)
	return err
}

// GetWorkItemState returns the state row of a source (ErrNotFound when none).
func GetWorkItemState(ctx context.Context, db bun.IDB, s MarinaScope, sourceType, sourceID string) (*WorkItemState, error) {
	w := new(WorkItemState)
	if err := db.NewSelect().Model(w).Where("marina_id = ?", s.ID()).Where("source_type = ?", sourceType).
		Where("source_id = ?", sourceID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return w, nil
}

// SaveWorkItemState creates or updates the state of a source: update first, insert when there is none, and an
// insert that loses the race on the unique index updates the winner's row.
func SaveWorkItemState(ctx context.Context, db bun.IDB, s MarinaScope, w *WorkItemState, now time.Time) error {
	w.MarinaID, w.UpdatedAt = s.ID(), UTC(now)
	update := func() (bool, error) {
		res, err := db.NewUpdate().Model((*WorkItemState)(nil)).Set("assignee_user_id = ?", workItemNull(w.AssigneeUserID)).
			Set("status = ?", w.Status).Set("note = ?", w.Note).Set("updated_at = ?", w.UpdatedAt).Set("version = version + 1").
			Where("marina_id = ?", s.ID()).Where("source_type = ?", w.SourceType).Where("source_id = ?", w.SourceID).Exec(ctx)
		if err != nil {
			return false, err
		}
		return affected(res) == 1, nil
	}
	if ok, err := update(); err != nil || ok {
		return err
	}
	w.ID, w.Version, w.CreatedAt = NewID(), 1, UTC(now)
	res, err := db.NewInsert().Model(w).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 1 {
		return nil
	}
	if ok, err := update(); err != nil || ok {
		return err
	}
	return ErrConflict
}

func workItemNull(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// WorkItemStates returns every state row of the marina.
func WorkItemStates(ctx context.Context, db bun.IDB, s MarinaScope) ([]WorkItemState, error) {
	out := []WorkItemState{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("id").Scan(ctx)
	return out, err
}

// WorkSource is one computed worklist source row.
type WorkSource struct {
	SourceType  string        `bun:"source_type"`
	SourceID    string        `bun:"source_id"`
	CustomerID  string        `bun:"customer_id"`
	AmountCents int64         `bun:"amount_cents"`
	DueOn       calendar.Date `bun:"due_on"`
	CreatedAt   time.Time     `bun:"created_at"`
}

// ReminderApprovalSources returns the reminder items waiting for approval (final notices).
func ReminderApprovalSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]WorkSource, error) {
	out := []WorkSource{}
	err := db.NewSelect().TableExpr("reminder_items AS r").Join("JOIN charges AS ch ON ch.id = r.charge_id AND ch.marina_id = r.marina_id").
		ColumnExpr("'reminder_item' AS source_type, r.id AS source_id, r.customer_id").
		ColumnExpr("ch.total_cents - ch.paid_amount_cents - ch.credited_cents AS amount_cents, ch.due_on, r.created_at").
		Where("r.marina_id = ?", s.ID()).Where("r.status = 'awaiting_approval'").Scan(ctx, &out)
	return out, err
}

// ChargesToExportSources returns the to_invoice charges issued on or before issuedBefore.
func ChargesToExportSources(ctx context.Context, db bun.IDB, s MarinaScope, issuedBefore calendar.Date) ([]WorkSource, error) {
	out := []WorkSource{}
	err := db.NewSelect().TableExpr("charges").
		ColumnExpr("'charge' AS source_type, id AS source_id, COALESCE(CAST(customer_id AS TEXT), '') AS customer_id, total_cents AS amount_cents, due_on, created_at").
		Where("marina_id = ?", s.ID()).Where("invoicing_status = ?", ChargeToInvoice).Where("payment_status <> ?", ChargeCancelled).
		Where("origin_type <> ?", ChargeOriginOpeningBalance).Where("issued_on <= ?", issuedBefore).Scan(ctx, &out)
	return out, err
}

// FailedIntentSources returns the failed payment intents (provider payments to follow up).
func FailedIntentSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]WorkSource, error) {
	out := []WorkSource{}
	err := db.NewSelect().TableExpr("payment_intents").
		ColumnExpr("'payment_intent' AS source_type, id AS source_id, customer_id, amount_cents, created_at").
		Where("marina_id = ?", s.ID()).Where("status = 'failed'").Where("purpose <> 'mandate_setup'").Scan(ctx, &out)
	return out, err
}

// FailedMandateSources returns the failed SEPA mandates.
func FailedMandateSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]WorkSource, error) {
	out := []WorkSource{}
	err := db.NewSelect().TableExpr("sepa_mandates").
		ColumnExpr("'sepa_mandate' AS source_type, id AS source_id, customer_id, 0 AS amount_cents, created_at").
		Where("marina_id = ?", s.ID()).Where("status = 'failed'").Scan(ctx, &out)
	return out, err
}

// OpenDeadlineSources returns the open deadlines of the M4 engine.
func OpenDeadlineSources(ctx context.Context, db bun.IDB, s MarinaScope) ([]WorkSource, error) {
	out := []WorkSource{}
	err := db.NewSelect().TableExpr("deadlines").
		ColumnExpr("'deadline' AS source_type, id AS source_id, COALESCE(CAST(customer_id AS TEXT), '') AS customer_id, 0 AS amount_cents, due_on, created_at").
		Where("marina_id = ?", s.ID()).Where("status = 'open'").Scan(ctx, &out)
	return out, err
}

// UserInOrganization reports whether a user has a grant in the organization (worklist assignees).
func UserInOrganization(ctx context.Context, db bun.IDB, orgID, userID string) (bool, error) {
	if !ValidID(userID) {
		return false, nil
	}
	n, err := db.NewSelect().TableExpr("memberships").Where("organization_id = ?", orgID).Where("user_id = ?", userID).Count(ctx)
	return n > 0, err
}

// WorkSourceExists reports whether the source of a computed worklist item exists in the marina.
func WorkSourceExists(ctx context.Context, db bun.IDB, s MarinaScope, sourceType, sourceID string) (bool, error) {
	table := map[string]string{
		"reminder_item": "reminder_items", "charge": "charges", "payment_intent": "payment_intents",
		"sepa_mandate": "sepa_mandates", "deadline": "deadlines",
	}[sourceType]
	if table == "" || !ValidID(sourceID) {
		return false, nil
	}
	n, err := db.NewSelect().TableExpr(table).Where("marina_id = ?", s.ID()).Where("id = ?", sourceID).Count(ctx)
	return n > 0, err
}
