package yardquote

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

var hullTypes = []string{"monohull_motor", "monohull_sail", "catamaran", "trimaran", "rib", "gozzo", "other"}

// defaultSettings is the settings row of a marina that never saved one (Version 0).
func defaultSettings(scope store.MarinaScope) store.YardSettings {
	return store.YardSettings{
		MarinaID: scope.ID(), TestVessel: store.MustJSON(DefaultTestVessel()), QuoteValidityDays: DefaultQuoteValidityDays,
		DepositBP: DefaultDepositBP, AdvanceOnAccept: true, ExternalMarkupBP: DefaultExternalMarkupBP,
		LiftFromSlot: DefaultLiftFromSlot, LiftToSlot: DefaultLiftToSlot,
	}
}

// SettingsTx returns the yard settings of the marina with defaults (Version 0 when never saved). Read by
// workorder (deposit, markup, advance) and lifting (window).
func (s *Service) SettingsTx(ctx context.Context, db bun.IDB, scope store.MarinaScope) (Settings, error) {
	row, err := store.GetYardSettings(ctx, db, scope)
	if errors.Is(err, store.ErrNotFound) {
		return defaultSettings(scope), nil
	}
	if err != nil {
		return Settings{}, err
	}
	return *row, nil
}

func testVesselOf(j store.JSON) TestVessel {
	tv := DefaultTestVessel()
	if len(j) > 2 {
		var v TestVessel
		if json.Unmarshal(j, &v) == nil && v.LOACm > 0 {
			tv = v
		}
	}
	return tv
}

// GetSettings returns the yard settings (yard.read).
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*SettingsView, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	row, err := s.SettingsTx(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	return &SettingsView{Row: row, TestVessel: testVesselOf(row.TestVessel)}, nil
}

func validTestVessel(v *auth.ValidationError, tv TestVessel) {
	if tv.LOACm <= 0 || tv.LOACm > 20000 {
		v.Add("test_vessel.loa_cm", "invalid")
	}
	if tv.BeamCm <= 0 || tv.BeamCm > 5000 {
		v.Add("test_vessel.beam_cm", "invalid")
	}
	for f, x := range map[string]int64{"draft_cm": tv.DraftCm, "air_draft_cm": tv.AirDraftCm, "lwl_cm": tv.LWLCm, "displacement_kg": tv.DisplacementKg} {
		if x < 0 {
			v.Add("test_vessel."+f, "invalid")
		}
	}
	if !slices.Contains(hullTypes, tv.HullType) {
		v.Add("test_vessel.hull_type", "invalid")
	}
	if len(tv.EnginesKW) > 8 {
		v.Add("test_vessel.engines_kw", "invalid")
	}
	for _, kw := range tv.EnginesKW {
		if kw < 0 {
			v.Add("test_vessel.engines_kw", "invalid")
		}
	}
	if tv.YearBuilt < 0 || tv.YearBuilt > 9999 {
		v.Add("test_vessel.year_built", "invalid")
	}
}

// PutSettings changes the yard settings (yard.manage). ifMatch is required once the settings were saved (428 is
// the handler's job when it is missing): 0 means "first save".
func (s *Service) PutSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in SettingsInput) (*SettingsView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := s.SettingsTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		if in.TestVessel != nil {
			validTestVessel(v, *in.TestVessel)
			row.TestVessel = store.MustJSON(in.TestVessel)
		}
		setInt := func(dst *int, src *int, field string, lo, hi int) {
			if src == nil {
				return
			}
			if *src < lo || *src > hi {
				v.Add(field, "invalid")
			}
			*dst = *src
		}
		setInt(&row.QuoteValidityDays, in.QuoteValidityDays, "quote_validity_days", 1, 365)
		setInt(&row.DepositBP, in.DepositBP, "deposit_bp", 0, 10000)
		setInt(&row.ExternalMarkupBP, in.ExternalMarkupBP, "external_markup_bp", 0, 100000)
		setInt(&row.LiftFromSlot, in.LiftFromSlot, "lift_from_slot", 0, 95)
		setInt(&row.LiftToSlot, in.LiftToSlot, "lift_to_slot", 1, 96)
		if row.LiftToSlot <= row.LiftFromSlot {
			v.Add("lift_to_slot", "invalid")
		}
		for dst, src := range map[*bool]*bool{
			&row.AdvanceOnAccept: in.AdvanceOnAccept, &row.FixedIncludesExternal: in.FixedIncludesExternal,
			&row.WorkersIssueMaterials: in.WorkersIssueMaterials, &row.LiftRequestOnMove: in.LiftRequestOnMove,
		} {
			if src != nil {
				*dst = *src
			}
		}
		if in.ClearExternalItem {
			row.ExternalItemID = ""
		} else if in.ExternalItemID != nil {
			if _, err := store.GetItem(ctx, tx, scope, *in.ExternalItemID); err != nil {
				v.Add("external_item_id", "not_found")
			}
			row.ExternalItemID = *in.ExternalItemID
		}
		if err := v.Err(); err != nil {
			return err
		}
		if err := store.SaveYardSettings(ctx, tx, scope, &row, ifMatch, s.now()); err != nil {
			return versionErr(err)
		}
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "yard_settings.update", "settings", scope.ID(), map[string]any{
			"quote_validity_days": row.QuoteValidityDays, "deposit_bp": row.DepositBP, "advance_on_accept": row.AdvanceOnAccept,
			"external_markup_bp": row.ExternalMarkupBP, "lift_from_slot": row.LiftFromSlot, "lift_to_slot": row.LiftToSlot,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.GetSettings(ctx, p, marinaID)
}
