package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of stored_files, shared by the harbour (map backgrounds) and document services. The blobs themselves
// live in internal/filestore; callers write the row and the blob in the same request and remove the blob after
// commit (AfterCommit) when a row is soft-deleted.

// InsertStoredFile inserts a stored file row.
func InsertStoredFile(ctx context.Context, db bun.IDB, f *StoredFile) error {
	_, err := db.NewInsert().Model(f).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetStoredFile returns a live (not deleted) stored file of a marina.
func GetStoredFile(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*StoredFile, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	f := new(StoredFile)
	err := db.NewSelect().Model(f).Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Where("deleted_at IS NULL").Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return f, nil
}

// SoftDeleteStoredFile marks a stored file as deleted and returns its storage key, so that the caller removes
// the blob after commit. Deleting an already deleted or unknown file is ErrNotFound.
func SoftDeleteStoredFile(ctx context.Context, db bun.IDB, s MarinaScope, id string, at time.Time) (string, error) {
	f, err := GetStoredFile(ctx, db, s, id)
	if err != nil {
		return "", err
	}
	res, err := db.NewUpdate().Table("stored_files").Set("deleted_at = ?", at).Set("updated_at = ?", at).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("deleted_at IS NULL").Exec(ctx)
	if err := expectOne(res, err); err != nil {
		return "", err
	}
	return f.StorageKey, nil
}
