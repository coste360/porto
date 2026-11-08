package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of the customer area (m8-plan §7): login and booking links, sessions, and the customer lookups of the
// login request. Tokens are stored only as SHA-256 hex; lookups by hash join the marina so the caller can check
// the slug (INV-88).

// InsertPortalLink inserts a customer-area link.
func InsertPortalLink(ctx context.Context, db bun.IDB, l *CustomerPortalLink) error {
	_, err := db.NewInsert().Model(l).Exec(ctx)
	return err
}

// PortalLinkByHash returns the link with the token hash and the slug of its marina (ErrNotFound when unknown).
func PortalLinkByHash(ctx context.Context, db bun.IDB, hash string) (*CustomerPortalLink, string, error) {
	var row struct {
		CustomerPortalLink `bun:",extend"`
		Slug               string `bun:"slug"`
	}
	err := db.NewSelect().Model(&row).ColumnExpr("customer_portal_link.*").ColumnExpr("m.slug AS slug").
		Join("JOIN marinas AS m ON m.id = customer_portal_link.marina_id").
		Where("customer_portal_link.token_hash = ?", hash).Where("m.archived_at IS NULL").Limit(1).Scan(ctx)
	if err != nil {
		return nil, "", notFound(err)
	}
	return &row.CustomerPortalLink, row.Slug, nil
}

// UsePortalLoginLink marks an unused login link as used; false when it was already used (single use under
// concurrency: the conditional update has one winner).
func UsePortalLoginLink(ctx context.Context, db bun.IDB, id string, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("customer_portal_links").Set("used_at = ?", UTC(at)).
		Where("id = ?", id).Where("kind = ?", PortalLinkLogin).Where("used_at IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// TouchPortalBookingLink records the last use of a reusable booking link.
func TouchPortalBookingLink(ctx context.Context, db bun.IDB, id string, at time.Time) error {
	_, err := db.NewUpdate().Table("customer_portal_links").Set("used_at = ?", UTC(at)).
		Where("id = ?", id).Where("kind = ?", PortalLinkBooking).Exec(ctx)
	return err
}

// InsertPortalSession inserts a customer-area session.
func InsertPortalSession(ctx context.Context, db bun.IDB, s *CustomerPortalSession) error {
	_, err := db.NewInsert().Model(s).Exec(ctx)
	return err
}

// PortalSessionByHash returns the session with the token hash and the slug of its marina.
func PortalSessionByHash(ctx context.Context, db bun.IDB, hash string) (*CustomerPortalSession, string, error) {
	var row struct {
		CustomerPortalSession `bun:",extend"`
		Slug                  string `bun:"slug"`
	}
	err := db.NewSelect().Model(&row).ColumnExpr("customer_portal_session.*").ColumnExpr("m.slug AS slug").
		Join("JOIN marinas AS m ON m.id = customer_portal_session.marina_id").
		Where("customer_portal_session.token_hash = ?", hash).Where("m.archived_at IS NULL").Limit(1).Scan(ctx)
	if err != nil {
		return nil, "", notFound(err)
	}
	return &row.CustomerPortalSession, row.Slug, nil
}

// TouchPortalSession slides the idle expiry of a live session.
func TouchPortalSession(ctx context.Context, db bun.IDB, id string, at time.Time) error {
	_, err := db.NewUpdate().Table("customer_portal_sessions").Set("last_seen_at = ?", UTC(at)).
		Where("id = ?", id).Where("revoked_at IS NULL").Exec(ctx)
	return err
}

// RevokePortalSession revokes a session of the marina.
func RevokePortalSession(ctx context.Context, db bun.IDB, s MarinaScope, id string, at time.Time) error {
	_, err := db.NewUpdate().Table("customer_portal_sessions").Set("revoked_at = ?", UTC(at)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("revoked_at IS NULL").Exec(ctx)
	return err
}

// DeleteExpiredPortalRows deletes links and sessions expired before cutoff (retention cleanup).
func DeleteExpiredPortalRows(ctx context.Context, db bun.IDB, cutoff time.Time) (int, error) {
	r1, err := db.NewDelete().Table("customer_portal_links").Where("expires_at < ?", UTC(cutoff)).Exec(ctx)
	if err != nil {
		return 0, err
	}
	r2, err := db.NewDelete().Table("customer_portal_sessions").Where("expires_at < ?", UTC(cutoff)).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return int(affected(r1) + affected(r2)), nil
}

// CustomersWithTransits returns the ids among ids of live customers of the marina that have at least one transit.
func CustomersWithTransits(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]string, error) {
	out := []string{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("customers AS c").ColumnExpr("c.id").
		Where("c.marina_id = ?", s.ID()).Where("c.id IN (?)", bun.List(ids)).
		Where("c.anonymized_at IS NULL").Where("c.merged_into_id IS NULL").
		Where("EXISTS (SELECT 1 FROM contracts AS k WHERE k.marina_id = c.marina_id AND k.customer_id = c.id AND k.kind = 'transit')").
		OrderExpr("c.id").Scan(ctx, &out)
	return out, err
}

// LiveCustomer reports whether the customer of the marina exists and is neither anonymised nor merged.
func LiveCustomer(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) (bool, error) {
	if !ValidID(customerID) {
		return false, nil
	}
	n, err := db.NewSelect().Table("customers").Where("marina_id = ?", s.ID()).Where("id = ?", customerID).
		Where("anonymized_at IS NULL").Where("merged_into_id IS NULL").Count(ctx)
	return n == 1, err
}

// CustomerVesselIDs returns the vessels linked to a customer of the marina: through a vessel party or as the vessel
// of one of the customer's transits.
func CustomerVesselIDs(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) ([]string, error) {
	out := []string{}
	if !ValidID(customerID) {
		return out, nil
	}
	err := db.NewRaw(`SELECT vessel_id FROM vessel_parties WHERE marina_id = ? AND customer_id = ?
		UNION SELECT vessel_id FROM contracts WHERE marina_id = ? AND customer_id = ? AND kind = 'transit'`,
		s.ID(), customerID, s.ID(), customerID).Scan(ctx, &out)
	return out, err
}

// DocumentOfCustomer returns a document of the marina owned by the customer or by one of the customer's vessels
// (ErrNotFound otherwise: the customer area sees only its own documents).
func DocumentOfCustomer(ctx context.Context, db bun.IDB, s MarinaScope, customerID, documentID string) (*Document, error) {
	d, err := GetDocument(ctx, db, s, documentID)
	if err != nil {
		return nil, err
	}
	if d.CustomerID == customerID && customerID != "" {
		return d, nil
	}
	if d.VesselID == "" {
		return nil, ErrNotFound
	}
	vs, err := CustomerVesselIDs(ctx, db, s, customerID)
	if err != nil {
		return nil, err
	}
	for _, v := range vs {
		if v == d.VesselID {
			return d, nil
		}
	}
	return nil, ErrNotFound
}
