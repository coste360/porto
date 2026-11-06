package store

import (
	"context"

	"github.com/uptrace/bun"
)

// Queries of consents (insert-only through the API, m1-plan §3.1).

// InsertConsent inserts a consent record.
func InsertConsent(ctx context.Context, db bun.IDB, c *Consent) error {
	_, err := db.NewInsert().Model(c).Exec(ctx)
	return err
}

// ListConsents returns the consent history of a customer, newest first.
func ListConsents(ctx context.Context, db bun.IDB, s MarinaScope, customerID string) ([]Consent, error) {
	out := []Consent{}
	if !ValidID(customerID) {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("customer_id = ?", customerID).
		OrderExpr("recorded_at DESC, id DESC").Scan(ctx)
	return out, err
}
