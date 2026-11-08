package charge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Billing settings and origin items (m5-plan §6.1, §7.3.1): read with charges.manage, written with
// marina.configure and If-Match.

// ReminderLevel is one level of the reminder policy (days after the due date; negative = before).
type ReminderLevel struct {
	Level   string `json:"level"`
	Days    int    `json:"days"`
	Enabled bool   `json:"enabled"`
}

// ReminderPolicy is the JSON of billing_settings.reminder_policy.
type ReminderPolicy struct {
	Levels []ReminderLevel `json:"levels"`
}

// ArrearsPolicy is the JSON of billing_settings.arrears_policy.
type ArrearsPolicy struct {
	Enabled        bool        `json:"enabled"`
	DaysOverdue    int         `json:"days_overdue"`
	MinAmountCents money.Cents `json:"min_amount_cents"`
}

// Settings are the billing settings with their origin items (key → item id).
type Settings struct {
	Row         store.BillingSettings
	Reminder    ReminderPolicy
	Arrears     ArrearsPolicy
	OriginItems map[string]string
}

// reminderLevels are the levels in their order.
var reminderLevels = []string{"pre_due", "due", "courteous", "formal", "final_notice"}

func settingsOf(row *store.BillingSettings, items []store.ChargeOriginItem) *Settings {
	out := &Settings{Row: *row, OriginItems: map[string]string{}}
	_ = json.Unmarshal(row.ReminderPolicy, &out.Reminder)
	if len(out.Reminder.Levels) == 0 {
		_ = json.Unmarshal(store.DefaultBillingSettings(row.MarinaID, row.CreatedAt).ReminderPolicy, &out.Reminder)
	}
	_ = json.Unmarshal(row.ArrearsPolicy, &out.Arrears)
	for _, it := range items {
		out.OriginItems[it.OriginKey] = it.ItemID
	}
	return out
}

// GetSettings returns the billing settings of the marina (charges.manage; the defaults when never saved).
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if errors.Is(err, auth.ErrForbidden) {
		scope, err = p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	}
	if err != nil {
		return nil, err
	}
	return s.settingsTx(ctx, s.d.DB.DB, scope)
}

func (s *Service) settingsTx(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*Settings, error) {
	row, err := store.GetBillingSettings(ctx, db, scope, s.now())
	if err != nil {
		return nil, err
	}
	items, err := store.OriginItems(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	return settingsOf(row, items), nil
}

// SettingsInput is a new version of the settings.
type SettingsInput struct {
	DefaultDueDays     int
	AutoPaymentRequest bool
	Reminder           ReminderPolicy
	ReminderHour       int
	Arrears            ArrearsPolicy
	ExportReminderDays int
	FiscalDriver       string
	ReceiptFooter      string
	SEPALeadDays       int
	OriginItems        map[string]string
	// FiscalDrivers are the accepted fiscal driver codes (none, export and the registered extensions).
	FiscalDrivers []string
	// FiscalBridgeDeviceID is the desktop device (kind desktop, paired with a token, of this marina) that issues
	// the documents of the driver "bridge" (M6b); required with that driver, "" = none.
	FiscalBridgeDeviceID string
}

// UpdateSettings saves the billing settings and origin items (marina.configure, If-Match; 0 when never saved).
func (s *Service) UpdateSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in SettingsInput) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	if err := in.validate(); err != nil {
		return nil, err
	}
	var out *Settings
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetBillingSettings(ctx, tx, scope, s.now())
		if err != nil {
			return err
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		var ids []string
		for _, id := range in.OriginItems {
			ids = append(ids, id)
		}
		if in.FiscalBridgeDeviceID != "" {
			d, err := store.DeviceByID(ctx, tx, in.FiscalBridgeDeviceID)
			if err != nil || d.MarinaID != scope.ID() || d.Kind != "desktop" || d.Access != store.DeviceAccessToken ||
				!d.RevokedAt.IsZero() {
				return invalid("fiscal_bridge_device_id", "invalid")
			}
		}
		items, err := store.ItemsByIDs(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		if len(items) != len(uniqueStrings(ids)) {
			return invalid("origin_items", "unknown_item")
		}
		upd := *row
		upd.DefaultDueDays, upd.AutoPaymentRequest, upd.ReminderHour = in.DefaultDueDays, in.AutoPaymentRequest, in.ReminderHour
		upd.ReminderPolicy, upd.ArrearsPolicy = store.MustJSON(in.Reminder), store.MustJSON(in.Arrears)
		upd.ExportReminderDays, upd.FiscalDriver, upd.ReceiptFooter, upd.SEPALeadDays = in.ExportReminderDays, in.FiscalDriver, in.ReceiptFooter, in.SEPALeadDays
		upd.FiscalBridgeDeviceID = in.FiscalBridgeDeviceID
		if err := store.SaveBillingSettings(ctx, tx, scope, &upd, ifMatch, s.now()); err != nil {
			return mapStore(err)
		}
		if err := store.SetFiscalBridgeDevice(ctx, tx, scope, in.FiscalBridgeDeviceID); err != nil {
			return err
		}
		if err := store.ReplaceOriginItems(ctx, tx, scope, in.OriginItems, s.now()); err != nil {
			return err
		}
		if err := s.write(ctx, tx, scope, p.Actor(), "billing_settings.update", "billing_settings", scope.ID(), map[string]any{
			"default_due_days": in.DefaultDueDays, "auto_payment_request": in.AutoPaymentRequest, "reminder_hour": in.ReminderHour,
			"export_reminder_days": in.ExportReminderDays, "fiscal_driver": in.FiscalDriver, "sepa_lead_days": in.SEPALeadDays,
			"arrears_enabled": in.Arrears.Enabled, "origin_items": len(in.OriginItems),
			"fiscal_bridge_device_id": in.FiscalBridgeDeviceID,
		}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "settings", ID: scope.ID(), Change: "updated"}); err != nil {
			return err
		}
		out, err = s.settingsTx(ctx, tx, scope)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (in *SettingsInput) validate() error {
	switch {
	case in.DefaultDueDays < 0 || in.DefaultDueDays > 365:
		return invalid("default_due_days", "range")
	case in.ReminderHour < 0 || in.ReminderHour > 23:
		return invalid("reminder_hour", "range")
	case in.ExportReminderDays < 1 || in.ExportReminderDays > 365:
		return invalid("export_reminder_days", "range")
	case in.SEPALeadDays < 0 || in.SEPALeadDays > 30:
		return invalid("sepa_lead_days", "range")
	case len(in.ReceiptFooter) > 1000:
		return invalid("receipt_footer", "too_long")
	case in.Arrears.DaysOverdue < 1 || in.Arrears.DaysOverdue > 365 || in.Arrears.MinAmountCents < 0:
		return invalid("arrears_policy", "invalid")
	}
	in.FiscalDriver = strings.TrimSpace(in.FiscalDriver)
	if !contains(in.FiscalDrivers, in.FiscalDriver) {
		return invalid("fiscal_driver", "unknown")
	}
	in.FiscalBridgeDeviceID = strings.TrimSpace(in.FiscalBridgeDeviceID)
	switch {
	case in.FiscalBridgeDeviceID != "" && !store.ValidID(in.FiscalBridgeDeviceID):
		return invalid("fiscal_bridge_device_id", "invalid")
	case in.FiscalDriver == "bridge" && in.FiscalBridgeDeviceID == "":
		return invalid("fiscal_bridge_device_id", "required")
	}
	seen := map[string]bool{}
	for _, l := range in.Reminder.Levels {
		if !contains(reminderLevels, l.Level) || seen[l.Level] || l.Days < -60 || l.Days > 365 {
			return invalid("reminder_policy", "invalid")
		}
		seen[l.Level] = true
	}
	for k, v := range in.OriginItems {
		if !contains(store.OriginItemKeys, k) || !store.ValidID(v) {
			return invalid("origin_items", "invalid")
		}
	}
	return nil
}
