package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Payment provider settings and the provider event dedupe log (m5-plan §5.4, INV-56). No row means the provider is
// disabled for the marina (Porto works offline by default). Owned by CASH-DRIVERS.

// GetProviderSettings returns the settings of a provider for the marina (ErrNotFound when never configured).
func GetProviderSettings(ctx context.Context, db bun.IDB, s MarinaScope, code string) (*PaymentProviderSettings, error) {
	row := new(PaymentProviderSettings)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("provider_code = ?", code).Scan(ctx)
	return row, notFound(err)
}

// ListProviderSettings returns the configured providers of the marina.
func ListProviderSettings(ctx context.Context, db bun.IDB, s MarinaScope) ([]PaymentProviderSettings, error) {
	out := []PaymentProviderSettings{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("provider_code").Scan(ctx)
	return out, err
}

// SaveProviderSettings inserts the row (expected 0, absent) or updates it at version expected.
func SaveProviderSettings(ctx context.Context, tx bun.IDB, s MarinaScope, row *PaymentProviderSettings, expected int, now time.Time) error {
	now = UTC(now)
	row.MarinaID, row.UpdatedAt = s.ID(), now
	if expected == 0 {
		row.ID, row.Version, row.CreatedAt = NewID(), 1, now
		if _, err := tx.NewInsert().Model(row).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrVersionMismatch
			}
			return err
		}
		return nil
	}
	res, err := tx.NewUpdate().Model(row).
		Column("enabled", "mode", "credentials_enc", "webhook_secret_enc", "publishable_key_last4", "sepa_enabled",
			"methods", "statement_descriptor", "updated_at").
		Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("provider_code = ?", row.ProviderCode).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	row.Version = expected + 1
	return nil
}

// MarinasWithEnabledProvider returns the active marinas with at least one enabled provider (jobs).
func MarinasWithEnabledProvider(ctx context.Context, db bun.IDB) ([]string, error) {
	var ids []string
	err := db.NewSelect().TableExpr("payment_provider_settings AS pps").ColumnExpr("DISTINCT pps.marina_id").
		Join("JOIN marinas AS m ON m.id = pps.marina_id").Where("pps.enabled = ?", true).Where("m.archived_at IS NULL").
		OrderExpr("pps.marina_id").Scan(ctx, &ids)
	return ids, err
}

// InsertProviderEvent records a provider event once (INV-56): false means the event id was already recorded and
// nothing must be applied.
func InsertProviderEvent(ctx context.Context, tx bun.IDB, e *PaymentProviderEvent) (bool, error) {
	res, err := tx.NewInsert().Model(e).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// ProviderEventExists reports whether an event id was already recorded.
func ProviderEventExists(ctx context.Context, db bun.IDB, s MarinaScope, provider, eventID string) (bool, error) {
	return db.NewSelect().Model((*PaymentProviderEvent)(nil)).Where("marina_id = ?", s.ID()).
		Where("provider_code = ?", provider).Where("event_id = ?", eventID).Exists(ctx)
}

// DeleteOldProviderEvents removes dedupe rows received before cutoff (payments.reconcile, 180 days).
func DeleteOldProviderEvents(ctx context.Context, db bun.IDB, cutoff time.Time) (int64, error) {
	res, err := db.NewDelete().Model((*PaymentProviderEvent)(nil)).Where("received_at < ?", UTC(cutoff)).Exec(ctx)
	if err != nil {
		return 0, err
	}
	return affected(res), nil
}
