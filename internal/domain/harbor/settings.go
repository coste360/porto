package harbor

import (
	"context"
	"errors"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Settings are the harbour settings of a marina (harbor_settings).
type Settings struct {
	PriceClassRule layout.PriceClassRule
	LOAToleranceCm int64
	DefaultGridCm  int64
	Version        int
}

// SettingsPatch lists the fields to change (nil = unchanged).
type SettingsPatch struct {
	PriceClassRule *layout.PriceClassRule
	LOAToleranceCm *int64
	DefaultGridCm  *int64
}

func defaultSettings() Settings {
	return Settings{PriceClassRule: layout.DefaultPriceClassRule(), LOAToleranceCm: layout.DefaultLOATol, DefaultGridCm: 50, Version: 1}
}

func settingsOf(h *store.HarborSettings) Settings {
	s := Settings{LOAToleranceCm: h.LOAToleranceCm, DefaultGridCm: h.DefaultGridCm, Version: h.Version}
	s.PriceClassRule = unmarshal[layout.PriceClassRule](h.PriceClassRule)
	if s.PriceClassRule.Basis == "" {
		s.PriceClassRule = layout.DefaultPriceClassRule()
	}
	return s
}

// loadSettings returns the stored settings of a marina, or the defaults (version 1) when none were stored.
func loadSettings(ctx context.Context, db bun.IDB, scope store.MarinaScope) (Settings, error) {
	h, err := store.GetHarborSettings(ctx, db, scope)
	if errors.Is(err, store.ErrNotFound) {
		return defaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	return settingsOf(h), nil
}

func (s Settings) validation() layout.Settings {
	v := layout.DefaultSettings()
	v.LOATolerance = geo.Cm(s.LOAToleranceCm)
	return v
}

// GetSettings returns the harbour settings (map.view).
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	out, err := loadSettings(ctx, s.d.DB.DB, scope)
	return &out, err
}

func checkRule(c checker, r *layout.PriceClassRule) {
	if r.Basis != "width" {
		c.add("price_class_rule.basis", "invalid")
	}
	if len(r.Bands) > 32 {
		c.add("price_class_rule.bands", "too_many")
	}
	var last geo.Cm
	for i, b := range r.Bands {
		f := fmt.Sprintf("price_class_rule.bands[%d]", i)
		if b.MaxCm <= last || b.MaxCm > layout.MaxMapSide {
			c.add(f+".max_cm", "not_increasing")
		}
		last = b.MaxCm
		c.code(f+".class", b.Class)
	}
	c.code("price_class_rule.fallback", r.Fallback)
}

// UpdateSettings changes the harbour settings when their version equals ifMatch (harbor.configure). Settings
// never stored have version 1 (the defaults).
func (s *Service) UpdateSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in SettingsPatch) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	c := checker{&auth.ValidationError{}}
	if in.PriceClassRule != nil {
		checkRule(c, in.PriceClassRule)
	}
	if in.LOAToleranceCm != nil {
		c.rangeI("loa_tolerance_cm", *in.LOAToleranceCm, 0, 500)
	}
	if in.DefaultGridCm != nil {
		c.rangeI("default_grid_cm", *in.DefaultGridCm, 1, 10000)
	}
	if err := c.v.Err(); err != nil {
		return nil, err
	}
	var out Settings
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		h, err := store.GetHarborSettings(ctx, tx, scope)
		if errors.Is(err, store.ErrNotFound) {
			d := defaultSettings()
			h = &store.HarborSettings{
				ID: store.NewID(), PriceClassRule: store.MustJSON(d.PriceClassRule), LOAToleranceCm: d.LOAToleranceCm,
				DefaultGridCm: d.DefaultGridCm, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			if err := store.InsertHarborSettings(ctx, tx, scope, h); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		changed := []string{}
		if in.PriceClassRule != nil {
			h.PriceClassRule = store.MustJSON(in.PriceClassRule)
			changed = append(changed, "price_class_rule")
		}
		if in.LOAToleranceCm != nil {
			h.LOAToleranceCm = *in.LOAToleranceCm
			changed = append(changed, "loa_tolerance_cm")
		}
		if in.DefaultGridCm != nil {
			h.DefaultGridCm = *in.DefaultGridCm
			changed = append(changed, "default_grid_cm")
		}
		h.UpdatedAt = now
		if err := store.UpdateHarborSettings(ctx, tx, scope, h, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "harbor_settings.update",
			EntityType: "harbor_settings", EntityID: h.ID, Diff: map[string]any{"fields": changed, "version": h.Version},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigMapChanged, event.MapChangedData{Change: event.ChangeSettings}); err != nil {
			return err
		}
		out = settingsOf(h)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
