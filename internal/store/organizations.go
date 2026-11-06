package store

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// InsertOrganization inserts a new organization.
func InsertOrganization(ctx context.Context, db bun.IDB, o *Organization) error {
	_, err := db.NewInsert().Model(o).Exec(ctx)
	return err
}

// GetOrganization returns the organization of the scope.
func GetOrganization(ctx context.Context, db bun.IDB, s OrgScope) (*Organization, error) {
	o := new(Organization)
	if err := db.NewSelect().Model(o).Where("id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return o, nil
}

// OrganizationsByIDs returns the organizations with the given ids, ordered by name.
func OrganizationsByIDs(ctx context.Context, db bun.IDB, ids []string) ([]Organization, error) {
	out := []Organization{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("id IN (?)", bun.List(ids)).OrderExpr("name, id").Scan(ctx)
	return out, err
}

// UpdateOrganization writes the editable fields of o when the stored version equals ifMatch, and bumps the
// version. It returns ErrVersionMismatch when the version differs.
func UpdateOrganization(ctx context.Context, db bun.IDB, s OrgScope, ifMatch int, o *Organization) error {
	res, err := db.NewUpdate().Model(o).
		Column("name", "vat_number", "fiscal_code", "billing_address", "sdi_code", "pec", "updated_at").
		Set("version = version + 1").
		Where("id = ?", s.ID()).Where("version = ?", ifMatch).
		Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetOrganization(ctx, db, s); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	o.Version = ifMatch + 1
	return nil
}

// InsertMarina inserts a new marina. A taken slug is ErrConflict.
func InsertMarina(ctx context.Context, db bun.IDB, m *Marina) error {
	_, err := db.NewInsert().Model(m).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetMarina returns the marina of the scope.
func GetMarina(ctx context.Context, db bun.IDB, s MarinaScope) (*Marina, error) {
	m := new(Marina)
	if err := db.NewSelect().Model(m).Where("id = ?", s.ID()).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// ListMarinas returns the active marinas of an organization ordered by name. With only != nil the result is
// restricted to those ids (marina-level grants).
func ListMarinas(ctx context.Context, db bun.IDB, s OrgScope, only []string) ([]Marina, error) {
	out := []Marina{}
	q := db.NewSelect().Model(&out).Where("organization_id = ?", s.ID()).Where("archived_at IS NULL")
	if only != nil {
		only = validIDs(only)
		if len(only) == 0 {
			return out, nil
		}
		q = q.Where("id IN (?)", bun.List(only))
	}
	err := q.OrderExpr("name, id").Scan(ctx)
	return out, err
}

// ActiveMarinasByIDs returns the non-archived marinas among ids, ordered by name.
func ActiveMarinasByIDs(ctx context.Context, db bun.IDB, ids []string) ([]Marina, error) {
	out := []Marina{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("id IN (?)", bun.List(ids)).Where("archived_at IS NULL").
		OrderExpr("name, id").Scan(ctx)
	return out, err
}

// ActiveMarinasOfOrganizations returns the non-archived marinas of the given organizations.
func ActiveMarinasOfOrganizations(ctx context.Context, db bun.IDB, orgIDs []string) ([]Marina, error) {
	out := []Marina{}
	orgIDs = validIDs(orgIDs)
	if len(orgIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("organization_id IN (?)", bun.List(orgIDs)).
		Where("archived_at IS NULL").OrderExpr("name, id").Scan(ctx)
	return out, err
}

// MarinaBySlug returns an active marina by slug.
func MarinaBySlug(ctx context.Context, db bun.IDB, slug string) (*Marina, error) {
	m := new(Marina)
	err := db.NewSelect().Model(m).Where("slug = ?", slug).Where("archived_at IS NULL").Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// UpdateMarina writes the editable fields of m when the stored version equals ifMatch and bumps the version.
func UpdateMarina(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, m *Marina) error {
	res, err := db.NewUpdate().Model(m).
		Column("name", "kind", "timezone", "currency", "default_locale", "locales", "contacts", "address", "geo",
			"concession_ref", "concession_expires_on", "settings", "settings_version", "theme", "booking_enabled",
			"allowed_embed_origins", "updated_at").
		Set("version = version + 1").
		Where("id = ?", s.ID()).Where("version = ?", ifMatch).Where("archived_at IS NULL").
		Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetMarina(ctx, db, s); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	m.Version = ifMatch + 1
	return nil
}

// ArchiveMarina marks an active marina archived.
func ArchiveMarina(ctx context.Context, db bun.IDB, s MarinaScope, at time.Time) error {
	res, err := db.NewUpdate().Table("marinas").
		Set("archived_at = ?", at).Set("updated_at = ?", at).Set("version = version + 1").
		Where("id = ?", s.ID()).Where("archived_at IS NULL").Exec(ctx)
	if err := expectOne(res, err); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func validIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if ValidID(id) {
			out = append(out, id)
		}
	}
	return out
}
