package store

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// InsertOutboxEvent inserts an event in the caller's transaction.
func InsertOutboxEvent(ctx context.Context, db bun.IDB, e *OutboxEvent) error {
	_, err := db.NewInsert().Model(e).Exec(ctx)
	return err
}

// GetOutboxEvent returns an event by id.
func GetOutboxEvent(ctx context.Context, db bun.IDB, id string) (*OutboxEvent, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(OutboxEvent)
	if err := db.NewSelect().Model(e).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// OutboxEventsByIDs returns events by id.
func OutboxEventsByIDs(ctx context.Context, db bun.IDB, ids []string) ([]OutboxEvent, error) {
	out := []OutboxEvent{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("id IN (?)", bun.List(ids)).Scan(ctx)
	return out, err
}

// skipLocked returns the row-locking clause of a claim subquery: on PostgreSQL concurrent claimers skip rows
// another transaction is claiming; SQLite serialises writers, so it needs none.
func skipLocked(db bun.IDB) string {
	if IsPostgres(db) {
		return " FOR UPDATE SKIP LOCKED"
	}
	return ""
}

// ClaimOutboxEvents leases up to limit undispatched, due events until now+lease and returns them in id order.
// A claimed event is invisible to other claimers until the lease expires, so concurrent dispatchers never
// dispatch the same event at the same time.
func ClaimOutboxEvents(ctx context.Context, db bun.IDB, now time.Time, lease time.Duration, limit int) ([]OutboxEvent, error) {
	var rows []OutboxEvent
	query := `UPDATE outbox_events SET locked_until = ?, updated_at = ?
		WHERE id IN (SELECT id FROM outbox_events
			WHERE dispatched_at IS NULL
			AND (next_attempt_at IS NULL OR next_attempt_at <= ?)
			AND (locked_until IS NULL OR locked_until <= ?)
			ORDER BY id LIMIT ?` + skipLocked(db) + `)
		RETURNING *`
	if err := db.NewRaw(query, UTC(now.Add(lease)), now, now, now, limit).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b OutboxEvent) int { return strings.Compare(a.ID, b.ID) })
	return rows, nil
}

// MarkOutboxDispatched records the successful dispatch of an event.
func MarkOutboxDispatched(ctx context.Context, db bun.IDB, id string, at time.Time) error {
	_, err := db.NewUpdate().Table("outbox_events").
		Set("dispatched_at = ?", at).Set("locked_until = NULL").Set("last_error = NULL").Set("updated_at = ?", at).
		Where("id = ?", id).Exec(ctx)
	return err
}

// MarkOutboxRetry records a failed dispatch: one more attempt, next try at next.
func MarkOutboxRetry(ctx context.Context, db bun.IDB, id string, next time.Time, lastErr string, at time.Time) error {
	_, err := db.NewUpdate().Table("outbox_events").
		Set("attempts = attempts + 1").Set("next_attempt_at = ?", next).Set("locked_until = NULL").
		Set("last_error = ?", truncate(lastErr, 1000)).Set("updated_at = ?", at).
		Where("id = ?", id).Exec(ctx)
	return err
}

// CountPendingOutbox returns the number of undispatched events.
func CountPendingOutbox(ctx context.Context, db bun.IDB) (int, error) {
	return toInt(db.NewSelect().Table("outbox_events").Where("dispatched_at IS NULL").Count(ctx))
}

// DeleteDispatchedOutbox removes events dispatched before the given instant that no delivery references.
func DeleteDispatchedOutbox(ctx context.Context, db bun.IDB, before time.Time) (int64, error) {
	res, err := db.NewDelete().Table("outbox_events").
		Where("dispatched_at IS NOT NULL").Where("dispatched_at < ?", before).
		Where("NOT EXISTS (SELECT 1 FROM webhook_deliveries d WHERE d.event_id = outbox_events.id)").Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// truncate cuts s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut]
}
