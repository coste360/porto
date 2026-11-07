package catalog

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Default pricing settings (spec 12.1: VAT 22 %, prices VAT excluded by default for B2B contracts).
const (
	DefaultVATRateBP        = 2200
	DefaultPriceIncludesVAT = false
)

func settingsOf(r *store.PricingSettings) Settings {
	return Settings{DefaultVATRateBP: r.DefaultVATRateBP, DefaultPriceIncludesVAT: r.DefaultPriceIncludesVAT, Version: r.Version}
}

// EnsureSettings returns the pricing settings row of a marina, inserting the defaults built by def when it is
// missing. It must run in a write transaction.
func EnsureSettings(ctx context.Context, tx bun.IDB, scope store.MarinaScope, def func() store.PricingSettings) (*store.PricingSettings, error) {
	r, err := store.GetPricingSettings(ctx, tx, scope)
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return r, err
	}
	row := def()
	return store.EnsurePricingSettings(ctx, tx, scope, &row)
}

func (s *Service) defaultSettingsRow() store.PricingSettings {
	now := s.now()
	return store.PricingSettings{
		ID: store.NewID(), DefaultVATRateBP: DefaultVATRateBP, DefaultPriceIncludesVAT: DefaultPriceIncludesVAT,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}

// EnsureSettingsRow is EnsureSettings with the service clock (used by the pricelist service inside its
// transactions).
func (s *Service) EnsureSettingsRow(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (*store.PricingSettings, error) {
	return EnsureSettings(ctx, tx, scope, s.defaultSettingsRow)
}

// readSettings returns the stored settings or the defaults (version 1) without writing.
func (s *Service) readSettings(ctx context.Context, db bun.IDB, scope store.MarinaScope) (Settings, error) {
	r, err := store.GetPricingSettings(ctx, db, scope)
	if errors.Is(err, store.ErrNotFound) {
		d := s.defaultSettingsRow()
		return settingsOf(&d), nil
	}
	if err != nil {
		return Settings{}, err
	}
	return settingsOf(r), nil
}

// GetSettings returns the pricing settings of a marina (pricing.read); the defaults when none were stored.
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	out, err := s.readSettings(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SettingsInput are the new pricing defaults.
type SettingsInput struct {
	DefaultVATRateBP        int64
	DefaultPriceIncludesVAT bool
}

// UpdateSettings replaces the pricing defaults when the version equals ifMatch (pricing.configure).
func (s *Service) UpdateSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in SettingsInput) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	c := newChecker()
	c.rangeI("default_vat_rate_bp", in.DefaultVATRateBP, 0, 10000)
	if err := c.err(); err != nil {
		return nil, err
	}
	var out Settings
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := s.EnsureSettingsRow(ctx, tx, scope)
		if err != nil {
			return err
		}
		r.DefaultVATRateBP, r.DefaultPriceIncludesVAT, r.UpdatedAt = in.DefaultVATRateBP, in.DefaultPriceIncludesVAT, s.now()
		if err := store.UpdatePricingSettings(ctx, tx, scope, r, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		out = settingsOf(r)
		return s.write(ctx, tx, p, scope, "pricing_settings.update", "pricing_settings", r.ID,
			map[string]any{"version": r.Version}, event.PricingChangedData{Version: r.Version, Change: event.ChangeSettings})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
