package store

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Billing settings and origin items (m5-plan §6.1). The settings row is created lazily with the column defaults.

// Origin item keys (charge_origin_items.origin_key).
const (
	OriginItemManagementFee   = "management_fee"
	OriginItemCommonAdvance   = "common_advance"
	OriginItemCommonBalance   = "common_balance"
	OriginItemTransferFee     = "transfer_fee"
	OriginItemTerminationPen  = "termination_penalty"
	OriginItemTerminationRef  = "termination_refund"
	OriginItemDepositRetain   = "deposit_retention"
	OriginItemOpeningBalance  = "opening_balance"
	OriginItemTrueUpFallback  = "contract_trueup_fallback"
	defaultReminderPolicyJSON = `{"levels":[{"level":"pre_due","days":-7,"enabled":true},{"level":"due","days":0,"enabled":true},` +
		`{"level":"courteous","days":7,"enabled":true},{"level":"formal","days":21,"enabled":true},` +
		`{"level":"final_notice","days":45,"enabled":true}]}`
)

// OriginItemKeys lists every origin key in display order.
var OriginItemKeys = []string{
	OriginItemManagementFee, OriginItemCommonAdvance, OriginItemCommonBalance, OriginItemTransferFee,
	OriginItemTerminationPen, OriginItemTerminationRef, OriginItemDepositRetain, OriginItemOpeningBalance,
	OriginItemTrueUpFallback,
}

// GetBillingSettings returns the billing settings of the marina, creating the row with the defaults when missing.
func GetBillingSettings(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time) (*BillingSettings, error) {
	b := new(BillingSettings)
	err := db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Scan(ctx)
	if err == nil {
		return b, nil
	}
	if err = notFound(err); !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return DefaultBillingSettings(s.ID(), now), nil
}

// DefaultBillingSettings is the settings row with the column defaults (not stored).
func DefaultBillingSettings(marinaID string, now time.Time) *BillingSettings {
	return &BillingSettings{
		MarinaID: marinaID, DefaultDueDays: 30, AutoPaymentRequest: true, ReminderPolicy: JSON(defaultReminderPolicyJSON),
		ReminderHour: 9, ArrearsPolicy: JSON(`{"enabled":false,"days_overdue":60,"min_amount_cents":0}`),
		ExportReminderDays: 30, FiscalDriver: "none", Version: 0, CreatedAt: UTC(now), UpdatedAt: UTC(now),
	}
}

// SaveBillingSettings writes the settings at version expected (0 = the row does not exist yet: insert);
// ErrVersionMismatch when someone else saved first.
func SaveBillingSettings(ctx context.Context, db bun.IDB, s MarinaScope, b *BillingSettings, expected int, now time.Time) error {
	b.MarinaID, b.UpdatedAt = s.ID(), UTC(now)
	if expected == 0 {
		b.CreatedAt, b.Version = UTC(now), 1
		res, err := db.NewInsert().Model(b).On("CONFLICT DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		if affected(res) == 0 {
			return ErrVersionMismatch
		}
		return nil
	}
	res, err := db.NewUpdate().Model(b).
		Column("default_due_days", "auto_payment_request", "reminder_policy", "reminder_hour", "arrears_policy",
			"export_reminder_days", "fiscal_driver", "receipt_footer", "sepa_lead_days", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	b.Version = expected + 1
	return nil
}

// OriginItems returns the configured origin items of the marina.
func OriginItems(ctx context.Context, db bun.IDB, s MarinaScope) ([]ChargeOriginItem, error) {
	out := []ChargeOriginItem{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("origin_key").Scan(ctx)
	return out, err
}

// OriginItem returns the item id configured for an origin key ("" when none).
func OriginItem(ctx context.Context, db bun.IDB, s MarinaScope, key string) (string, error) {
	var id string
	err := db.NewSelect().Model((*ChargeOriginItem)(nil)).Column("item_id").Where("marina_id = ?", s.ID()).
		Where("origin_key = ?", key).Scan(ctx, &id)
	if errors.Is(notFound(err), ErrNotFound) {
		return "", nil
	}
	return id, err
}

// ReplaceOriginItems replaces the origin items of the marina with items (key → item id).
func ReplaceOriginItems(ctx context.Context, db bun.IDB, s MarinaScope, items map[string]string, now time.Time) error {
	if _, err := db.NewDelete().Model((*ChargeOriginItem)(nil)).Where("marina_id = ?", s.ID()).Exec(ctx); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	rows := make([]ChargeOriginItem, 0, len(items))
	for _, k := range OriginItemKeys {
		if id, ok := items[k]; ok && id != "" {
			rows = append(rows, ChargeOriginItem{
				ID: NewID(), MarinaID: s.ID(), OriginKey: k, ItemID: id, CreatedAt: UTC(now), UpdatedAt: UTC(now),
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&rows).Exec(ctx)
	return err
}
