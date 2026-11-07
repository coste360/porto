package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the M4 concession card and its obligations (m4-plan §4.7). One concession per marina.

// GetConcession returns the concession of the marina, or ErrNotFound when none was stored yet.
func GetConcession(ctx context.Context, db bun.IDB, s MarinaScope) (*Concession, error) {
	c := new(Concession)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Limit(1).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// InsertConcession inserts the concession of a marina; a second one is ErrConflict (concessions_marina_uidx).
func InsertConcession(ctx context.Context, db bun.IDB, s MarinaScope, c *Concession) error {
	c.MarinaID = s.ID()
	_, err := db.NewInsert().Model(c).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateConcession writes the card when the version equals expected.
func UpdateConcession(ctx context.Context, db bun.IDB, s MarinaScope, c *Concession, expected int) error {
	if err := versionedUpdate(ctx, db, s, c, "concessions", c.ID, expected, "authority", "deed_kind", "deed_number", "granted_on",
		"expires_on", "annual_fee_cents", "surfaces", "prescriptions", "status", "updated_at"); err != nil {
		return err
	}
	c.Version = expected + 1
	return nil
}

// SetMarinaConcessionExpiry writes marinas.concession_expires_on (the default end of new rights) and bumps the
// marina version.
func SetMarinaConcessionExpiry(ctx context.Context, db bun.IDB, s MarinaScope, on calendar.Date, at time.Time) error {
	q := db.NewUpdate().Table("marinas").Set("version = version + 1").Set("updated_at = ?", at).Where("id = ?", s.ID())
	if on.IsZero() {
		q = q.Set("concession_expires_on = NULL")
	} else {
		q = q.Set("concession_expires_on = ?", on)
	}
	return expectOne(q.Exec(ctx))
}

// InsertObligation inserts a concession obligation.
func InsertObligation(ctx context.Context, db bun.IDB, s MarinaScope, o *ConcessionObligation) error {
	o.MarinaID = s.ID()
	_, err := db.NewInsert().Model(o).Exec(ctx)
	return err
}

// GetObligation returns an obligation of the marina.
func GetObligation(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ConcessionObligation, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	o := new(ConcessionObligation)
	if err := db.NewSelect().Model(o).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return o, nil
}

// UpdateObligation writes an obligation when the version equals expected.
func UpdateObligation(ctx context.Context, db bun.IDB, s MarinaScope, o *ConcessionObligation, expected int) error {
	if err := versionedUpdate(ctx, db, s, o, "concession_obligations", o.ID, expected, "kind", "description", "due_on", "recurrence",
		"amount_cents", "done_on", "updated_at"); err != nil {
		return err
	}
	o.Version = expected + 1
	return nil
}

// DeleteObligation deletes an obligation when the version equals expected.
func DeleteObligation(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	res, err := db.NewDelete().Model((*ConcessionObligation)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetObligation(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ListObligations returns the obligations of the marina by due date.
func ListObligations(ctx context.Context, db bun.IDB, s MarinaScope) ([]ConcessionObligation, error) {
	out := []ConcessionObligation{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("due_on, id").Scan(ctx)
	return out, err
}
