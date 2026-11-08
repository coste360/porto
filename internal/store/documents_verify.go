package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// SetDocumentVerified records (verified) or clears the office verification of a document of the marina and bumps
// its version (m8-plan §5.2). ErrNotFound for an unknown or foreign document.
func SetDocumentVerified(ctx context.Context, db bun.IDB, s MarinaScope, id string, verified bool, userID string, at time.Time) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	q := db.NewUpdate().Table("documents").Set("version = version + 1").Set("updated_at = ?", UTC(at)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if verified {
		var uid any
		if ValidID(userID) {
			uid = userID
		}
		q = q.Set("verified_at = ?", UTC(at)).Set("verified_by_user_id = ?", uid)
	} else {
		q = q.Set("verified_at = NULL").Set("verified_by_user_id = NULL")
	}
	res, err := q.Exec(ctx)
	return expectOne(res, err)
}
