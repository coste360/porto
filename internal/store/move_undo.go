package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of move_undo_log (m3-plan §3.5): the last move of an allocation is undoable for 24 h.

// InsertMoveUndo inserts an undo entry of the marina.
func InsertMoveUndo(ctx context.Context, tx bun.IDB, s MarinaScope, u *MoveUndo) error {
	u.MarinaID = s.ID()
	u.ExpiresAt = UTC(u.ExpiresAt)
	_, err := tx.NewInsert().Model(u).Exec(ctx)
	return err
}

// GetMoveUndo returns an undo entry of the marina.
func GetMoveUndo(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*MoveUndo, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	u := new(MoveUndo)
	if err := db.NewSelect().Model(u).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return u, nil
}

// MarkMoveUndone records the undo of an entry; an entry already undone is ErrConflict.
func MarkMoveUndone(ctx context.Context, tx bun.IDB, s MarinaScope, id string, now time.Time) error {
	res, err := tx.NewUpdate().Model((*MoveUndo)(nil)).Set("undone_at = ?", UTC(now)).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("undone_at IS NULL").Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrConflict
	}
	return nil
}

// DeleteExpiredMoveUndo removes entries expired before cutoff (transient data, excluded from backups).
func DeleteExpiredMoveUndo(ctx context.Context, db bun.IDB, cutoff time.Time) (int64, error) {
	res, err := db.NewDelete().Model((*MoveUndo)(nil)).Where("expires_at < ?", UTC(cutoff)).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}
