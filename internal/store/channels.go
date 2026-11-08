package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Queries of channel_connections, channel_reservations and channel_events (m8-plan §8.2, INV-89). A reservation
// and an event exist once by unique keys; inserting them again is a no-op reported to the caller.

// GetChannelConnection returns the marina's connection to a channel (ErrNotFound when none).
func GetChannelConnection(ctx context.Context, db bun.IDB, s MarinaScope, code string) (*ChannelConnection, error) {
	c := new(ChannelConnection)
	err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("channel_code = ?", code).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// LockChannelConnection is GetChannelConnection with a row lock on PostgreSQL (FOR UPDATE).
func LockChannelConnection(ctx context.Context, db bun.IDB, s MarinaScope, code string) (*ChannelConnection, error) {
	c := new(ChannelConnection)
	q := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("channel_code = ?", code).Limit(1)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	err := q.Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// ChannelConnections returns the connections of the marina keyed by channel code.
func ChannelConnections(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]ChannelConnection, error) {
	var rows []ChannelConnection
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]ChannelConnection, len(rows))
	for _, r := range rows {
		out[r.ChannelCode] = r
	}
	return out, nil
}

// EnabledChannelConnections returns every enabled connection of every marina (jobs), ordered by marina and code.
func EnabledChannelConnections(ctx context.Context, db bun.IDB) ([]ChannelConnection, error) {
	var rows []ChannelConnection
	err := db.NewSelect().Model(&rows).Where("enabled = ?", true).OrderExpr("marina_id, channel_code").Scan(ctx)
	return rows, err
}

// InsertChannelConnection inserts a connection (unique per marina and channel).
func InsertChannelConnection(ctx context.Context, db bun.IDB, c *ChannelConnection) error {
	_, err := db.NewInsert().Model(c).Exec(ctx)
	return err
}

// UpdateChannelConnection writes a connection when its version equals expected (ErrVersionMismatch otherwise).
func UpdateChannelConnection(ctx context.Context, db bun.IDB, s MarinaScope, expected int, c *ChannelConnection, now time.Time) error {
	c.UpdatedAt = UTC(now)
	res, err := db.NewUpdate().Model(c).
		Column("enabled", "credentials_enc", "webhook_secret_enc", "category_map", "push_enabled", "push_dirty", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	c.Version = expected + 1
	return nil
}

// MarkChannelsDirty flags the push-enabled connections of a marina for the next push.
func MarkChannelsDirty(ctx context.Context, db bun.IDB, marinaID string) error {
	_, err := db.NewUpdate().Model((*ChannelConnection)(nil)).Set("push_dirty = ?", true).
		Where("marina_id = ?", marinaID).Where("enabled = ?", true).Where("push_enabled = ?", true).Where("push_dirty = ?", false).Exec(ctx)
	return err
}

// SetChannelPullState records the outcome of a pull (cursor, time, error code).
func SetChannelPullState(ctx context.Context, db bun.IDB, s MarinaScope, id, cursor, lastError string, now time.Time) error {
	q := db.NewUpdate().Model((*ChannelConnection)(nil)).Set("last_pull_at = ?", UTC(now)).Set("last_error = ?", lastError).
		Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if lastError == "" {
		q = q.Set("pull_cursor = ?", cursor)
	}
	_, err := q.Exec(ctx)
	return err
}

// SetChannelPushState records the outcome of a push: dirty cleared on success (and the full push time when full).
func SetChannelPushState(ctx context.Context, db bun.IDB, s MarinaScope, id string, full bool, lastError string, now time.Time) error {
	q := db.NewUpdate().Model((*ChannelConnection)(nil)).Set("last_error = ?", lastError).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if lastError == "" {
		q = q.Set("push_dirty = ?", false).Set("last_push_at = ?", UTC(now))
		if full {
			q = q.Set("last_full_push_at = ?", UTC(now))
		}
	}
	_, err := q.Exec(ctx)
	return err
}

// InsertChannelEvent records a channel event once: false when the event was already recorded (INV-89).
func InsertChannelEvent(ctx context.Context, db bun.IDB, e *ChannelEvent) (bool, error) {
	res, err := db.NewInsert().Model(e).On("CONFLICT (marina_id, channel_code, event_id) DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// SetChannelEventOutcome sets the status and reservation of an event row.
func SetChannelEventOutcome(ctx context.Context, db bun.IDB, s MarinaScope, id, status, reservationID string) error {
	q := db.NewUpdate().Model((*ChannelEvent)(nil)).Set("status = ?", status).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if reservationID != "" {
		q = q.Set("reservation_id = ?", reservationID)
	}
	_, err := q.Exec(ctx)
	return err
}

// InsertChannelReservation records a reservation once: false when (marina, channel, external ref) exists.
func InsertChannelReservation(ctx context.Context, db bun.IDB, r *ChannelReservation) (bool, error) {
	res, err := db.NewInsert().Model(r).On("CONFLICT (marina_id, channel_code, external_ref) DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// LockChannelReservation returns a reservation by external reference, locked on PostgreSQL (ErrNotFound when none).
func LockChannelReservation(ctx context.Context, db bun.IDB, s MarinaScope, code, externalRef string) (*ChannelReservation, error) {
	r := new(ChannelReservation)
	q := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("channel_code = ?", code).Where("external_ref = ?", externalRef).Limit(1)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	err := q.Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// UpdateChannelReservation writes status, reject reason and contract of a reservation.
func UpdateChannelReservation(ctx context.Context, db bun.IDB, s MarinaScope, r *ChannelReservation, now time.Time) error {
	r.UpdatedAt = UTC(now)
	_, err := db.NewUpdate().Model(r).Column("status", "reject_reason", "contract_id", "updated_at").
		Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Exec(ctx)
	return err
}

// ChannelReservationFilter filters the desk list of reservations.
type ChannelReservationFilter struct {
	Channel string
	Status  string
}

// ListChannelReservations returns a page of reservations, newest first.
func ListChannelReservations(ctx context.Context, db bun.IDB, s MarinaScope, f ChannelReservationFilter, cursor string, limit int) (Page[ChannelReservation], error) {
	limit = ClampLimit(limit)
	var rows []ChannelReservation
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Channel != "" {
		q = q.Where("channel_code = ?", f.Channel)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[ChannelReservation]{}, err
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[ChannelReservation]{}, err
	}
	return pageOf(rows, limit, func(r ChannelReservation) string { return r.ID }), nil
}
