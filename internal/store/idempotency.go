package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// ClaimIdempotencyKey records the first use of (scope, key). When the row is new it returns (row, true): the
// caller executes the request and then calls CompleteIdempotencyKey (or ReleaseIdempotencyKey on failure). When
// the key is already known it returns the existing row and false: the caller replays the stored response, or
// rejects the request if the hash differs or the first request is still in progress. An expired row is replaced.
// The unique (scope, key) constraint makes exactly one concurrent caller win.
func ClaimIdempotencyKey(ctx context.Context, db bun.IDB, scope, key, requestHash string, now, expiresAt time.Time) (*IdempotencyKey, bool, error) {
	for range 2 {
		row := &IdempotencyKey{
			ID: NewID(), Scope: scope, Key: key, RequestHash: requestHash, State: IdempotencyInProgress,
			ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now,
		}
		res, err := db.NewInsert().Model(row).On("CONFLICT (scope, key) DO NOTHING").Exec(ctx)
		if err != nil {
			return nil, false, err
		}
		if affected(res) == 1 {
			return row, true, nil
		}
		existing := new(IdempotencyKey)
		err = db.NewSelect().Model(existing).Where("scope = ?", scope).Where("key = ?", key).Scan(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // deleted in between: try again
			}
			return nil, false, err
		}
		if existing.ExpiresAt.After(now) {
			return existing, false, nil
		}
		if _, err := db.NewDelete().Table("idempotency_keys").Where("id = ?", existing.ID).Exec(ctx); err != nil {
			return nil, false, err
		}
	}
	return nil, false, ErrConflict
}

// CompleteIdempotencyKey stores the response of the request that claimed the key.
func CompleteIdempotencyKey(ctx context.Context, db bun.IDB, id string, status int, contentType string, body []byte, at time.Time) error {
	if body == nil {
		body = []byte{}
	}
	_, err := db.NewUpdate().Table("idempotency_keys").
		Set("state = ?", IdempotencyCompleted).Set("response_status = ?", status).
		Set("response_content_type = ?", contentType).Set("response_body = ?", body).Set("updated_at = ?", at).
		Where("id = ?", id).Exec(ctx)
	return err
}

// ReleaseIdempotencyKey forgets an in-progress claim (the request failed before producing a response worth
// replaying), so that the client can retry with the same key.
func ReleaseIdempotencyKey(ctx context.Context, db bun.IDB, id string) error {
	_, err := db.NewDelete().Table("idempotency_keys").Where("id = ?", id).
		Where("state = ?", IdempotencyInProgress).Exec(ctx)
	return err
}

// DeleteExpiredIdempotencyKeys removes expired keys.
func DeleteExpiredIdempotencyKeys(ctx context.Context, db bun.IDB, now time.Time) (int64, error) {
	res, err := db.NewDelete().Table("idempotency_keys").Where("expires_at <= ?", now).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// InsertAudit inserts an audit row.
func InsertAudit(ctx context.Context, db bun.IDB, e *AuditEntry) error {
	_, err := db.NewInsert().Model(e).Exec(ctx)
	return err
}

// AuditFilter narrows an audit listing.
type AuditFilter struct {
	EntityType, EntityID, ActorID string
	From, To                      time.Time // inclusive From, exclusive To; zero = unbounded
}

// ListAudit returns the audit rows of a marina, newest first, after the cursor.
func ListAudit(ctx context.Context, db bun.IDB, s MarinaScope, f AuditFilter, cursor string, limit int) (Page[AuditEntry], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[AuditEntry]{}, err
	}
	rows := []AuditEntry{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.EntityType != "" {
		q = q.Where("entity_type = ?", f.EntityType)
	}
	if f.EntityID != "" {
		q = q.Where("entity_id = ?", f.EntityID)
	}
	if f.ActorID != "" {
		q = q.Where("actor_id = ?", f.ActorID)
	}
	if !f.From.IsZero() {
		q = q.Where("created_at >= ?", UTC(f.From))
	}
	if !f.To.IsZero() {
		q = q.Where("created_at < ?", UTC(f.To))
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[AuditEntry]{}, err
	}
	return pageOf(rows, limit, func(e AuditEntry) string { return e.ID }), nil
}

// CountAudit counts audit rows matching an action (tests and acceptance checks).
func CountAudit(ctx context.Context, db bun.IDB, action string) (int, error) {
	return toInt(db.NewSelect().Table("audit_log").Where("action = ?", action).Count(ctx))
}
