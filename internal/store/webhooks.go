package store

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// InsertWebhookEndpoint inserts an endpoint.
func InsertWebhookEndpoint(ctx context.Context, db bun.IDB, e *WebhookEndpoint) error {
	_, err := db.NewInsert().Model(e).Exec(ctx)
	return err
}

// ListWebhookEndpoints returns the endpoints of a marina, oldest first.
func ListWebhookEndpoints(ctx context.Context, db bun.IDB, s MarinaScope) ([]WebhookEndpoint, error) {
	out := []WebhookEndpoint{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("id").Scan(ctx)
	return out, err
}

// GetWebhookEndpoint returns one endpoint of a marina.
func GetWebhookEndpoint(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*WebhookEndpoint, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(WebhookEndpoint)
	if err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// UpdateWebhookEndpoint writes the editable fields when the stored version equals ifMatch, and bumps the version.
func UpdateWebhookEndpoint(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, e *WebhookEndpoint) error {
	res, err := db.NewUpdate().Model(e).
		Column("url", "description", "events", "active", "consecutive_failures", "disabled_at", "disabled_reason", "updated_at").
		Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", e.ID).Where("version = ?", ifMatch).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetWebhookEndpoint(ctx, db, s, e.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	e.Version = ifMatch + 1
	return nil
}

// RotateWebhookSecret stores a new secret and keeps the old one valid until previousUntil.
func RotateWebhookSecret(ctx context.Context, db bun.IDB, s MarinaScope, id, secretEnc, previousEnc string, previousUntil, at time.Time) error {
	res, err := db.NewUpdate().Table("webhook_endpoints").
		Set("secret_enc = ?", secretEnc).Set("previous_secret_enc = ?", previousEnc).
		Set("previous_secret_expires_at = ?", previousUntil).Set("version = version + 1").Set("updated_at = ?", at).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// DeleteWebhookEndpoint removes an endpoint and its deliveries.
func DeleteWebhookEndpoint(ctx context.Context, db bun.IDB, s MarinaScope, id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	res, err := db.NewDelete().Table("webhook_endpoints").Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// ActiveWebhookEndpoints returns the active endpoints of a marina (event fan-out).
func ActiveWebhookEndpoints(ctx context.Context, db bun.IDB, s MarinaScope) ([]WebhookEndpoint, error) {
	out := []WebhookEndpoint{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("active = ?", true).OrderExpr("id").Scan(ctx)
	return out, err
}

// WebhookEndpointsByIDs returns endpoints by id (deliverer).
func WebhookEndpointsByIDs(ctx context.Context, db bun.IDB, ids []string) ([]WebhookEndpoint, error) {
	out := []WebhookEndpoint{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("id IN (?)", bun.List(ids)).Scan(ctx)
	return out, err
}

// RecordWebhookSuccess resets the failure counter of an endpoint.
func RecordWebhookSuccess(ctx context.Context, db bun.IDB, endpointID string, at time.Time) error {
	_, err := db.NewUpdate().Table("webhook_endpoints").Set("consecutive_failures = 0").Set("updated_at = ?", at).
		Where("id = ?", endpointID).Where("consecutive_failures <> 0").Exec(ctx)
	return err
}

// RecordWebhookFailure increments the failure counter and returns its new value.
func RecordWebhookFailure(ctx context.Context, db bun.IDB, endpointID string, at time.Time) (int, error) {
	var n []int
	err := db.NewRaw(`UPDATE webhook_endpoints SET consecutive_failures = consecutive_failures + 1, updated_at = ?
		WHERE id = ? RETURNING consecutive_failures`, at, endpointID).Scan(ctx, &n)
	if err != nil {
		return 0, err
	}
	if len(n) == 0 {
		return 0, ErrNotFound
	}
	return n[0], nil
}

// DisableWebhookEndpoint deactivates an active endpoint; false when it was already inactive.
func DisableWebhookEndpoint(ctx context.Context, db bun.IDB, endpointID, reason string, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("webhook_endpoints").
		Set("active = ?", false).Set("disabled_at = ?", at).Set("disabled_reason = ?", reason).
		Set("version = version + 1").Set("updated_at = ?", at).
		Where("id = ?", endpointID).Where("active = ?", true).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// InsertWebhookDelivery inserts a pending delivery; an existing (endpoint, event) pair is left alone and the
// call returns false (fan-out is idempotent).
func InsertWebhookDelivery(ctx context.Context, db bun.IDB, d *WebhookDelivery) (bool, error) {
	res, err := db.NewInsert().Model(d).On("CONFLICT (endpoint_id, event_id) DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// ClaimWebhookDeliveries leases up to limit due pending deliveries until now+lease, oldest due first.
func ClaimWebhookDeliveries(ctx context.Context, db bun.IDB, now time.Time, lease time.Duration, limit int) ([]WebhookDelivery, error) {
	var rows []WebhookDelivery
	query := `UPDATE webhook_deliveries SET locked_until = ?, updated_at = ?
		WHERE id IN (SELECT id FROM webhook_deliveries
			WHERE status = 'pending' AND next_attempt_at <= ?
			AND (locked_until IS NULL OR locked_until <= ?)
			ORDER BY next_attempt_at, id LIMIT ?` + skipLocked(db) + `)
		RETURNING *`
	if err := db.NewRaw(query, UTC(now.Add(lease)), now, now, now, limit).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b WebhookDelivery) int { return strings.Compare(a.ID, b.ID) })
	return rows, nil
}

// SaveWebhookAttempt writes the outcome of a delivery attempt (status, counters, timestamps, lease released).
func SaveWebhookAttempt(ctx context.Context, db bun.IDB, d *WebhookDelivery) error {
	d.LockedUntil = time.Time{}
	_, err := db.NewUpdate().Model(d).
		Column("status", "attempts", "last_status_code", "last_error", "response_excerpt", "first_attempt_at",
			"next_attempt_at", "locked_until", "delivered_at", "failed_at", "updated_at").
		Where("id = ?", d.ID).Exec(ctx)
	return err
}

// ListWebhookDeliveries returns the deliveries of an endpoint, newest first.
func ListWebhookDeliveries(ctx context.Context, db bun.IDB, s MarinaScope, endpointID, status, cursor string, limit int) (Page[WebhookDelivery], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[WebhookDelivery]{}, err
	}
	rows := []WebhookDelivery{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("endpoint_id = ?", endpointID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[WebhookDelivery]{}, err
	}
	return pageOf(rows, limit, func(d WebhookDelivery) string { return d.ID }), nil
}

// GetWebhookDelivery returns one delivery of an endpoint of a marina.
func GetWebhookDelivery(ctx context.Context, db bun.IDB, s MarinaScope, endpointID, id string) (*WebhookDelivery, error) {
	if !ValidID(id) || !ValidID(endpointID) {
		return nil, ErrNotFound
	}
	d := new(WebhookDelivery)
	err := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("endpoint_id = ?", endpointID).
		Where("id = ?", id).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

// ResetWebhookDelivery makes a delivery pending again, due now, with a fresh attempt budget.
func ResetWebhookDelivery(ctx context.Context, db bun.IDB, s MarinaScope, id string, now time.Time) error {
	res, err := db.NewUpdate().Table("webhook_deliveries").
		Set("status = ?", DeliveryPending).Set("attempts = 0").Set("next_attempt_at = ?", now).
		Set("first_attempt_at = NULL").Set("locked_until = NULL").Set("delivered_at = NULL").Set("failed_at = NULL").
		Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// PurgeWebhookDeliveries removes finished deliveries created before the given instant.
func PurgeWebhookDeliveries(ctx context.Context, db bun.IDB, before time.Time) (int64, error) {
	res, err := db.NewDelete().Table("webhook_deliveries").
		Where("status <> ?", DeliveryPending).Where("created_at < ?", before).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// InsertMessage queues a message. A message with the same (marina, dedupe key) is left alone and the call
// returns false.
func InsertMessage(ctx context.Context, db bun.IDB, m *Message) (bool, error) {
	res, err := db.NewInsert().Model(m).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// ClaimMessages leases up to limit due messages (queued and due, or "sending" with an expired lease after a
// crash), marks them sending and returns them in id order.
func ClaimMessages(ctx context.Context, db bun.IDB, now time.Time, lease time.Duration, limit int) ([]Message, error) {
	var rows []Message
	query := `UPDATE message_log SET status = 'sending', locked_until = ?, updated_at = ?
		WHERE id IN (SELECT id FROM message_log
			WHERE (status = 'queued' AND (next_attempt_at IS NULL OR next_attempt_at <= ?))
			OR (status = 'sending' AND (locked_until IS NULL OR locked_until <= ?))
			ORDER BY id LIMIT ?` + skipLocked(db) + `)
		RETURNING *`
	if err := db.NewRaw(query, UTC(now.Add(lease)), now, now, now, limit).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b Message) int { return strings.Compare(a.ID, b.ID) })
	return rows, nil
}

// MarkMessageSent records a successful send.
func MarkMessageSent(ctx context.Context, db bun.IDB, id, providerID string, at time.Time) error {
	_, err := db.NewUpdate().Table("message_log").
		Set("status = ?", MessageSent).Set("attempts = attempts + 1").Set("sent_at = ?", at).
		Set("provider_message_id = ?", nullable(providerID)).Set("locked_until = NULL").Set("last_error = NULL").
		Set("updated_at = ?", at).Where("id = ?", id).Exec(ctx)
	return err
}

// MarkMessageRetry records a failed send that will be retried at next (zero next = give up: failed).
func MarkMessageRetry(ctx context.Context, db bun.IDB, id, lastErr string, next, at time.Time) error {
	q := db.NewUpdate().Table("message_log").
		Set("attempts = attempts + 1").Set("locked_until = NULL").Set("last_error = ?", truncate(lastErr, 1000)).
		Set("updated_at = ?", at).Where("id = ?", id)
	if next.IsZero() {
		q = q.Set("status = ?", MessageFailed)
	} else {
		q = q.Set("status = ?", MessageQueued).Set("next_attempt_at = ?", next)
	}
	_, err := q.Exec(ctx)
	return err
}

// MessageWaitingAttachment is the last_error of a queued message waiting for an attachment still being generated
// (MarkMessageWaiting): not a failed attempt.
const MessageWaitingAttachment = "waiting_attachment"

// MarkMessageWaiting puts a claimed message back in the queue without counting an attempt: an attachment is still
// being generated. next is the safety-net recheck; WakeWaitingMessages makes it due as soon as a document is ready.
func MarkMessageWaiting(ctx context.Context, db bun.IDB, id string, next, at time.Time) error {
	_, err := db.NewUpdate().Table("message_log").
		Set("status = ?", MessageQueued).Set("locked_until = NULL").Set("last_error = ?", MessageWaitingAttachment).
		Set("next_attempt_at = ?", next).Set("updated_at = ?", at).Where("id = ?", id).Exec(ctx)
	return err
}

// WakeWaitingMessages makes the queued messages of the marina (marinaID "": messages without a marina) waiting for an
// attachment due at now and returns how many there were.
func WakeWaitingMessages(ctx context.Context, db bun.IDB, marinaID string, now time.Time) (int64, error) {
	q := db.NewUpdate().Table("message_log").
		Set("next_attempt_at = ?", now).Set("updated_at = ?", now).
		Where("status = ?", MessageQueued).Where("last_error = ?", MessageWaitingAttachment)
	if marinaID == "" {
		q = q.Where("marina_id IS NULL")
	} else {
		q = q.Where("marina_id = ?", marinaID)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetMessage returns one message by id.
func GetMessage(ctx context.Context, db bun.IDB, id string) (*Message, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(Message)
	if err := db.NewSelect().Model(m).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// ListMessages returns every message, oldest first (tests and operator tools).
func ListMessages(ctx context.Context, db bun.IDB) ([]Message, error) {
	out := []Message{}
	err := db.NewSelect().Model(&out).OrderExpr("id").Scan(ctx)
	return out, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
