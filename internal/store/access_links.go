package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of customer magic links (m4-plan §6.6, INV-75). Tokens are never stored: only their SHA-256.

// InsertAccessLink inserts a link.
func InsertAccessLink(ctx context.Context, db bun.IDB, l *AccessLink) error {
	_, err := db.NewInsert().Model(l).Exec(ctx)
	return err
}

// RevokeAccessLinks revokes the active links of a purpose, subject and customer ("" customer = any).
func RevokeAccessLinks(ctx context.Context, db bun.IDB, s MarinaScope, purpose, subjectType, subjectID, customerID string, at time.Time) error {
	q := db.NewUpdate().Table("access_links").Set("revoked_at = ?", UTC(at)).Set("updated_at = ?", UTC(at)).
		Where("marina_id = ?", s.ID()).Where("purpose = ?", purpose).Where("subject_type = ?", subjectType).
		Where("subject_id = ?", subjectID).Where("revoked_at IS NULL")
	if customerID != "" {
		q = q.Where("customer_id = ?", customerID)
	}
	_, err := q.Exec(ctx)
	return err
}

// AccessLinkByHash returns a link by token hash with the slug of its marina (any marina: the caller checks the
// slug and the state).
func AccessLinkByHash(ctx context.Context, db bun.IDB, hash string) (*AccessLink, string, error) {
	var row struct {
		AccessLink `bun:",extend"`
		Slug       string `bun:"slug"`
	}
	err := db.NewSelect().Model(&row).ColumnExpr("access_link.*").ColumnExpr("m.slug AS slug").
		Join("JOIN marinas AS m ON m.id = access_link.marina_id").Where("access_link.token_hash = ?", hash).
		Limit(1).Scan(ctx)
	if err != nil {
		return nil, "", notFound(err)
	}
	return &row.AccessLink, row.Slug, nil
}

// TouchAccessLink records a use of a link.
func TouchAccessLink(ctx context.Context, db bun.IDB, id string, at time.Time) error {
	_, err := db.NewUpdate().Table("access_links").Set("last_used_at = ?", UTC(at)).Set("use_count = use_count + 1").
		Where("id = ?", id).Exec(ctx)
	return err
}

// GetAccessLink returns a link of a marina by id.
func GetAccessLink(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*AccessLink, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	l := new(AccessLink)
	if err := db.NewSelect().Model(l).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return l, nil
}

// DeleteExpiredAccessLinks deletes the links expired before (any marina) and returns how many.
func DeleteExpiredAccessLinks(ctx context.Context, db bun.IDB, before time.Time) (int64, error) {
	res, err := db.NewDelete().Table("access_links").Where("expires_at < ?", UTC(before)).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}

// DeleteOldOTPs deletes the signature codes created before (any marina) and returns how many.
func DeleteOldOTPs(ctx context.Context, db bun.IDB, before time.Time) (int64, error) {
	res, err := db.NewDelete().Table("signature_otps").Where("created_at < ?", UTC(before)).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}
