package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of documents (m1-plan §3.1, §4.4). Every function is scoped by the marina. The document files are
// rows of stored_files (files.go) plus encrypted blobs in internal/filestore.

// DocumentFilter narrows ListDocuments.
type DocumentFilter struct {
	CustomerID    string
	VesselID      string
	Type          string
	ExpiresBefore calendar.Date // documents expiring on or before this date
}

// InsertDocument inserts a document.
func InsertDocument(ctx context.Context, db bun.IDB, d *Document) error {
	_, err := db.NewInsert().Model(d).Exec(ctx)
	return err
}

// GetDocument returns one document of a marina.
func GetDocument(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Document, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	d := new(Document)
	if err := db.NewSelect().Model(d).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return d, nil
}

var documentColumns = []string{
	"type", "number_enc", "issuer", "issued_on", "expires_on", "file_id", "file_name", "notes", "updated_at",
}

// UpdateDocument writes the editable columns when the stored version equals ifMatch (0 = no check) and bumps
// the version.
func UpdateDocument(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, d *Document) error {
	q := db.NewUpdate().Model(d).Column(documentColumns...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", d.ID)
	if ifMatch > 0 {
		q = q.Where("version = ?", ifMatch)
	} else {
		q = q.Where("version = ?", d.Version)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetDocument(ctx, db, s, d.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	if ifMatch > 0 {
		d.Version = ifMatch + 1
	} else {
		d.Version++
	}
	return nil
}

// DeleteDocument removes a document when its version equals ifMatch (0 = no check).
func DeleteDocument(ctx context.Context, db bun.IDB, s MarinaScope, id string, ifMatch int) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	q := db.NewDelete().Table("documents").Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if ifMatch > 0 {
		q = q.Where("version = ?", ifMatch)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetDocument(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ListDocuments returns a page of documents, newest first.
func ListDocuments(ctx context.Context, db bun.IDB, s MarinaScope, f DocumentFilter, cursor string, limit int) (Page[Document], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Document]{}, err
	}
	rows := []Document{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.CustomerID != "" {
		if !ValidID(f.CustomerID) {
			return Page[Document]{Items: rows}, nil
		}
		q = q.Where("customer_id = ?", f.CustomerID)
	}
	if f.VesselID != "" {
		if !ValidID(f.VesselID) {
			return Page[Document]{Items: rows}, nil
		}
		q = q.Where("vessel_id = ?", f.VesselID)
	}
	if f.Type != "" {
		q = q.Where("type = ?", f.Type)
	}
	if !f.ExpiresBefore.IsZero() {
		q = q.Where("expires_on IS NOT NULL").Where("expires_on <= ?", f.ExpiresBefore)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Document]{}, err
	}
	return pageOf(rows, limit, func(d Document) string { return d.ID }), nil
}

// DocumentsOfCustomer returns every document of a customer, newest first.
func DocumentsOfCustomer(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) ([]Document, error) {
	return documentsWhere(ctx, db, s, "customer_id", customerID)
}

// DocumentsOfVessel returns every document of a vessel, newest first.
func DocumentsOfVessel(ctx context.Context, db bun.IDB, s MarinaScope, vesselID string) ([]Document, error) {
	return documentsWhere(ctx, db, s, "vessel_id", vesselID)
}

func documentsWhere(ctx context.Context, db bun.IDB, s MarinaScope, column, id string) ([]Document, error) {
	out := []Document{}
	if !ValidID(id) {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("? = ?", bun.Ident(column), id).
		OrderExpr("id DESC").Scan(ctx)
	return out, err
}

// StoredFilesByIDs returns the live stored files of a marina with the given ids.
func StoredFilesByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]StoredFile, error) {
	out := []StoredFile{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).
		Where("deleted_at IS NULL").Scan(ctx)
	return out, err
}

// CountDocuments returns the number of documents of a marina.
func CountDocuments(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Table("documents").Where("marina_id = ?", s.ID()).Count(ctx))
}

// DocumentTypesByIDs returns the type of each document of a marina with the given ids.
func DocumentTypesByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]string, error) {
	out := map[string]string{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   string `bun:"id"`
		Type string `bun:"type"`
	}
	err := db.NewSelect().Table("documents").Column("id", "type").Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(ids)).Scan(ctx, &rows)
	for _, r := range rows {
		out[r.ID] = r.Type
	}
	return out, err
}
