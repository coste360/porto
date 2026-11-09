package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/store"
)

// ---- formulas (read rental.manage, write marina.configure) ----

// ListFormulas lists the formulas.
func (s *Service) ListFormulas(ctx context.Context, p *auth.Principal, marinaID string, active *bool) ([]store.RentalFormula, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, err
	}
	return store.ListRentalFormulas(ctx, s.d.DB.DB, scope, active)
}

// FormulaCategories decodes the categories of a formula (empty = every category).
func FormulaCategories(f *store.RentalFormula) []string {
	return decodeStrings(f.Categories)
}

func decodeStrings(j store.JSON) []string {
	out := []string{}
	if len(j) > 0 {
		_ = json.Unmarshal(j, &out)
	}
	return out
}

func encodeStrings(list []string) store.JSON {
	if list == nil {
		list = []string{}
	}
	raw, _ := json.Marshal(list)
	return store.JSON(raw)
}

// FormulaName decodes the I18n name of a formula.
func FormulaName(f *store.RentalFormula) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal(f.Name, &out)
	return out
}

func (s *Service) validateFormula(ctx context.Context, db bun.IDB, scope store.MarinaScope, f *store.RentalFormula) error {
	v := &auth.ValidationError{}
	if !formulaCodeRe.MatchString(f.Code) {
		v.Add("code", "invalid")
	}
	validI18n(v, "name", FormulaName(f), 80)
	if !slices.Contains(formulaKinds, f.Kind) {
		v.Add("kind", "invalid")
		return v
	}
	if f.Kind == store.RentalKindHalfDay {
		set, err := s.LoadSettingsTx(ctx, db, scope)
		if err != nil {
			return err
		}
		if f.BandCode == "" {
			v.Add("band_code", "required")
		} else if !slices.ContainsFunc(set.Bands, func(b store.RentalBand) bool { return b.Code == f.BandCode }) {
			v.Add("band_code", "not_found")
		}
	} else {
		f.BandCode = ""
	}
	if f.MinSlots < 1 || f.MinSlots > 48 {
		v.Add("min_slots", "out_of_range")
	}
	if f.MaxSlots != nil && (*f.MaxSlots < f.MinSlots || *f.MaxSlots > 48) {
		v.Add("max_slots", "out_of_range")
	}
	minDays := 1
	switch f.Kind {
	case store.RentalKindMultiDay:
		minDays = 2
	case store.RentalKindWeek:
		minDays = 7
	}
	if f.MinDays < minDays || f.MinDays > 366 {
		v.Add("min_days", "out_of_range")
	}
	if f.MaxDays < f.MinDays || f.MaxDays > 366 {
		v.Add("max_days", "out_of_range")
	}
	if !slices.Contains(skipperModes, f.Skipper) {
		v.Add("skipper", "invalid")
	}
	if !slices.Contains(fuelModes, f.Fuel) {
		v.Add("fuel", "invalid")
	}
	switch {
	case f.Fuel == store.RentalFuelMetered && f.FuelItemID == "":
		v.Add("fuel_item_id", "required")
	case f.Fuel != store.RentalFuelMetered:
		f.FuelItemID = ""
	}
	if err := itemCheck(ctx, db, scope, v, "fuel_item_id", f.FuelItemID, false); err != nil {
		return err
	}
	if err := itemCheck(ctx, db, scope, v, "late_item_id", f.LateItemID, false); err != nil {
		return err
	}
	if f.GraceMinutes < 0 || f.GraceMinutes > 120 {
		v.Add("grace_minutes", "out_of_range")
	}
	if !slices.Contains(lateSteps, f.LateStepMinutes) {
		v.Add("late_step_minutes", "invalid")
	}
	validCategories(v, "categories", FormulaCategories(f))
	return v.Err()
}

func formulaDiff(f *store.RentalFormula) map[string]any {
	return map[string]any{"code": f.Code, "kind": f.Kind, "active": f.Active, "skipper": f.Skipper, "fuel": f.Fuel}
}

// CreateFormula adds a formula.
func (s *Service) CreateFormula(ctx context.Context, p *auth.Principal, marinaID string, in FormulaInput) (*store.RentalFormula, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	now := s.now()
	name, _ := json.Marshal(in.Name)
	f := &store.RentalFormula{
		ID: store.NewID(), Code: strings.TrimSpace(in.Code), Name: store.JSON(name), Kind: in.Kind, BandCode: in.BandCode,
		MinSlots: in.MinSlots, MinDays: in.MinDays, MaxDays: in.MaxDays, Skipper: or(in.Skipper, store.RentalSkipperNone),
		Fuel: or(in.Fuel, store.RentalFuelIncluded), FuelItemID: in.FuelItemID, LateItemID: in.LateItemID, GraceMinutes: 15,
		LateStepMinutes: in.LateStepMinutes, Categories: encodeStrings(in.Categories), Active: in.Active == nil || *in.Active,
		Sort: in.Sort, CreatedAt: now, UpdatedAt: now,
	}
	if in.MaxSlots > 0 {
		m := in.MaxSlots
		f.MaxSlots = &m
	}
	if f.MinSlots == 0 {
		f.MinSlots = slots.DefaultMinSlots
	}
	if f.MinDays == 0 {
		switch in.Kind {
		case store.RentalKindMultiDay:
			f.MinDays = 2
		case store.RentalKindWeek:
			f.MinDays = 7
		default:
			f.MinDays = 1
		}
	}
	if f.MaxDays == 0 {
		f.MaxDays = max(slots.DefaultMaxDays, f.MinDays)
	}
	if in.GraceMinutes != nil {
		f.GraceMinutes = *in.GraceMinutes
	}
	if f.LateStepMinutes == 0 {
		f.LateStepMinutes = 30
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.validateFormula(ctx, tx, scope, f); err != nil {
			return err
		}
		if err := store.InsertRentalFormula(ctx, tx, scope, f); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return err
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_formula.create", "rental_formula", f.ID, formulaDiff(f))
	})
	if err != nil {
		return nil, err
	}
	return store.GetRentalFormula(ctx, s.d.DB.DB, scope, f.ID)
}

// UpdateFormula changes a formula (If-Match).
func (s *Service) UpdateFormula(ctx context.Context, p *auth.Principal, marinaID, formulaID string, ifMatch int, in FormulaPatch) (*store.RentalFormula, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMarinaConfigure, store.RentalTableFormulas, formulaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		f, err := store.GetRentalFormula(ctx, tx, scope, formulaID)
		if err != nil {
			return notFound(err)
		}
		if f.Version != ifMatch {
			return ErrVersionMismatch
		}
		setStr(&f.Code, in.Code)
		if in.Name != nil {
			raw, _ := json.Marshal(in.Name)
			f.Name = store.JSON(raw)
		}
		setStr(&f.BandCode, in.BandCode)
		setInt(&f.MinSlots, in.MinSlots)
		if in.MaxSlots != nil {
			if *in.MaxSlots <= 0 {
				f.MaxSlots = nil
			} else {
				m := *in.MaxSlots
				f.MaxSlots = &m
			}
		}
		setInt(&f.MinDays, in.MinDays)
		setInt(&f.MaxDays, in.MaxDays)
		setStr(&f.Skipper, in.Skipper)
		setStr(&f.Fuel, in.Fuel)
		setStr(&f.FuelItemID, in.FuelItemID)
		setStr(&f.LateItemID, in.LateItemID)
		setInt(&f.GraceMinutes, in.GraceMinutes)
		setInt(&f.LateStepMinutes, in.LateStepMinutes)
		if in.Categories != nil {
			f.Categories = encodeStrings(in.Categories)
		}
		if in.Active != nil {
			f.Active = *in.Active
		}
		setInt(&f.Sort, in.Sort)
		if err := s.validateFormula(ctx, tx, scope, f); err != nil {
			return err
		}
		if err := store.UpdateRentalFormula(ctx, tx, scope, f, ifMatch, s.now()); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return versionErr(err)
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_formula.update", "rental_formula", f.ID, formulaDiff(f))
	})
	if err != nil {
		return nil, err
	}
	return store.GetRentalFormula(ctx, s.d.DB.DB, scope, formulaID)
}

// ---- extras ----

func extraView(r *store.RentalExtraRow) ExtraView {
	return ExtraView{RentalExtra: r.RentalExtra, ItemCode: r.ItemCode, Name: r.ItemName}
}

// ListExtras lists the extras.
func (s *Service) ListExtras(ctx context.Context, p *auth.Principal, marinaID string, active *bool) ([]ExtraView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListRentalExtras(ctx, s.d.DB.DB, scope, active)
	if err != nil {
		return nil, err
	}
	out := make([]ExtraView, 0, len(rows))
	for i := range rows {
		out = append(out, extraView(&rows[i]))
	}
	return out, nil
}

func validateExtra(e *store.RentalExtra, v *auth.ValidationError) {
	if !slices.Contains(chargeModes, e.ChargeMode) {
		v.Add("charge_mode", "invalid")
	}
	if e.MaxQtyMilli <= 0 || e.MaxQtyMilli > 1_000_000 {
		v.Add("max_qty_milli", "out_of_range")
	}
	if e.DefaultQtyMilli < 0 || e.DefaultQtyMilli > e.MaxQtyMilli {
		v.Add("default_qty_milli", "out_of_range")
	}
	validCategories(v, "categories", decodeStrings(e.Categories))
}

func (s *Service) extraByID(ctx context.Context, scope store.MarinaScope, id string) (*ExtraView, error) {
	r, err := store.RentalExtraByID(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	v := extraView(r)
	return &v, nil
}

// CreateExtra adds an extra.
func (s *Service) CreateExtra(ctx context.Context, p *auth.Principal, marinaID string, in ExtraInput) (*ExtraView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	now := s.now()
	e := &store.RentalExtra{
		ID: store.NewID(), ItemID: in.ItemID, ChargeMode: or(in.ChargeMode, store.RentalExtraPerRental),
		DefaultQtyMilli: int64(in.DefaultQtyMilli), MaxQtyMilli: int64(in.MaxQtyMilli), Categories: encodeStrings(in.Categories),
		Active: in.Active == nil || *in.Active, Sort: in.Sort, CreatedAt: now, UpdatedAt: now,
	}
	if e.MaxQtyMilli == 0 {
		e.MaxQtyMilli = 10_000
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		v := &auth.ValidationError{}
		if e.ItemID == "" {
			v.Add("item_id", "required")
		} else if err := itemCheck(ctx, tx, scope, v, "item_id", e.ItemID, false); err != nil {
			return err
		}
		validateExtra(e, v)
		if err := v.Err(); err != nil {
			return err
		}
		if err := store.InsertRentalExtra(ctx, tx, scope, e); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return err
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_extra.create", "rental_extra", e.ID, map[string]any{"item_id": e.ItemID, "charge_mode": e.ChargeMode})
	})
	if err != nil {
		return nil, err
	}
	return s.extraByID(ctx, scope, e.ID)
}

// UpdateExtra changes an extra (If-Match).
func (s *Service) UpdateExtra(ctx context.Context, p *auth.Principal, marinaID, extraID string, ifMatch int, in ExtraPatch) (*ExtraView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMarinaConfigure, store.RentalTableExtras, extraID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		e, err := store.GetRentalExtra(ctx, tx, scope, extraID)
		if err != nil {
			return notFound(err)
		}
		if e.Version != ifMatch {
			return ErrVersionMismatch
		}
		setStr(&e.ChargeMode, in.ChargeMode)
		if in.DefaultQtyMilli != nil {
			e.DefaultQtyMilli = int64(*in.DefaultQtyMilli)
		}
		if in.MaxQtyMilli != nil {
			e.MaxQtyMilli = int64(*in.MaxQtyMilli)
		}
		if in.Categories != nil {
			e.Categories = encodeStrings(in.Categories)
		}
		if in.Active != nil {
			e.Active = *in.Active
		}
		setInt(&e.Sort, in.Sort)
		v := &auth.ValidationError{}
		validateExtra(e, v)
		if err := v.Err(); err != nil {
			return err
		}
		if err := store.UpdateRentalExtra(ctx, tx, scope, e, ifMatch, s.now()); err != nil {
			return versionErr(err)
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_extra.update", "rental_extra", e.ID, map[string]any{"charge_mode": e.ChargeMode, "active": e.Active})
	})
	if err != nil {
		return nil, err
	}
	return s.extraByID(ctx, scope, extraID)
}

// ---- settings ----

// decodeSettings decodes the JSON columns of a settings row.
func decodeSettings(row *store.RentalSettings) *Settings {
	out := &Settings{Row: row, Bands: []store.RentalBand{}, Checklist: []store.RentalChecklistItem{}, DepositModes: []string{}}
	_ = json.Unmarshal(row.Bands, &out.Bands)
	_ = json.Unmarshal(row.Checklist, &out.Checklist)
	_ = json.Unmarshal(row.DepositModes, &out.DepositModes)
	for i := range out.Checklist {
		if out.Checklist[i].Categories == nil {
			out.Checklist[i].Categories = []string{}
		}
	}
	return out
}

// LoadSettingsTx returns the decoded settings of the marina in db (seam for reservation and handover). Preauth
// availability is read from the collect service when wired.
func (s *Service) LoadSettingsTx(ctx context.Context, db bun.IDB, scope store.MarinaScope) (*Settings, error) {
	row, err := store.LoadRentalSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	out := decodeSettings(row)
	if s.d.Preauth != nil {
		ok, err := s.d.Preauth.PreauthAvailable(ctx, db, scope)
		if err != nil {
			return nil, err
		}
		out.PreauthAvailable = ok
	}
	return out, nil
}

// GetSettings returns the settings (defaults with version 0 when never saved).
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*Settings, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, err
	}
	return s.LoadSettingsTx(ctx, s.d.DB.DB, scope)
}

func (s *Service) validateSettings(ctx context.Context, db bun.IDB, scope store.MarinaScope, in *SettingsInput) error {
	v := &auth.ValidationError{}
	for i, b := range in.Bands {
		if !bandCodeRe.MatchString(b.Code) {
			v.Add("bands.code", "invalid")
		}
		validI18n(v, "bands.label", b.Label, 40)
		if in.Bands[i].Label == nil {
			in.Bands[i].Label = map[string]string{}
		}
	}
	if in.DeskHoldMinutes < 5 || in.DeskHoldMinutes > 1440 {
		v.Add("desk_hold_minutes", "out_of_range")
	}
	if in.NoShowAfterMinutes < 0 || in.NoShowAfterMinutes > 1440 {
		v.Add("no_show_after_minutes", "out_of_range")
	}
	if in.LicenceThresholdKWMilli <= 0 || in.LicenceThresholdKWMilli > 10_000_000 {
		v.Add("licence_threshold_kw_milli", "out_of_range")
	}
	if n := len(in.Checklist); n < 1 || n > 30 {
		v.Add("checklist", "out_of_range")
	}
	keys := map[string]bool{}
	for i, c := range in.Checklist {
		if !keyRe.MatchString(c.Key) || keys[c.Key] {
			v.Add("checklist.key", "invalid")
		}
		keys[c.Key] = true
		validI18n(v, "checklist.label", c.Label, 80)
		validCategories(v, "checklist.categories", c.Categories)
		if c.Categories == nil {
			in.Checklist[i].Categories = []string{}
		}
	}
	if in.CheckoutMinPhotos < 0 || in.CheckoutMinPhotos > 10 {
		v.Add("checkout_min_photos", "out_of_range")
	}
	if in.CheckinMinPhotos < 0 || in.CheckinMinPhotos > 10 {
		v.Add("checkin_min_photos", "out_of_range")
	}
	if len(in.DepositModes) == 0 {
		v.Add("deposit_modes", "required")
	}
	seen := map[string]bool{}
	for _, m := range in.DepositModes {
		if !slices.Contains(depositModes, m) || seen[m] {
			v.Add("deposit_modes", "invalid")
			break
		}
		seen[m] = true
	}
	if in.IdentityRetentionDays < 30 || in.IdentityRetentionDays > 3650 {
		v.Add("identity_retention_days", "out_of_range")
	}
	if err := itemCheck(ctx, db, scope, v, "skipper_item_id", in.SkipperItemID, true); err != nil {
		return err
	}
	if err := itemCheck(ctx, db, scope, v, "damage_item_id", in.DamageItemID, false); err != nil {
		return err
	}
	if in.DefaultRegisterID != "" {
		if _, err := store.GetCashRegister(ctx, db, scope, in.DefaultRegisterID); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v.Add("default_register_id", "not_found")
		}
	}
	if err := v.Err(); err != nil {
		return err
	}
	ts := slots.Settings{OpenFrom: in.OpenFromSlot, OpenTo: in.OpenToSlot, Buffer: in.BufferSlots}
	for _, b := range in.Bands {
		ts.Bands = append(ts.Bands, slots.Band{Code: b.Code, From: b.FromSlot, To: b.ToSlot})
	}
	if err := slots.ValidateSettings(ts); err != nil {
		var re *slots.RangeError
		if errors.As(err, &re) && re.Code == slots.CodeBandGapLtBuffer {
			return ErrBandGapLtBuffer
		}
		field := "open_window"
		if re != nil && re.Expected != "" {
			field = re.Expected
			if re.Kind == slots.KindHalfDay {
				field = "bands"
			}
		}
		v.Add(field, "invalid")
		return v
	}
	// A half-day formula must keep its band.
	formulas, err := store.ListRentalFormulas(ctx, db, scope, nil)
	if err != nil {
		return err
	}
	for _, f := range formulas {
		if f.Kind == store.RentalKindHalfDay && !slices.ContainsFunc(in.Bands, func(b store.RentalBand) bool { return b.Code == f.BandCode }) {
			v.Add("bands", "in_use")
			return v
		}
	}
	return nil
}

// PutSettings saves the settings (If-Match "0" for the first save).
func (s *Service) PutSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in SettingsInput) (*Settings, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.LoadRentalSettings(ctx, tx, scope)
		if err != nil {
			return err
		}
		if cur.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := s.validateSettings(ctx, tx, scope, &in); err != nil {
			return err
		}
		bands, _ := json.Marshal(in.Bands)
		checklist, _ := json.Marshal(in.Checklist)
		row := &store.RentalSettings{
			OpenFromSlot: in.OpenFromSlot, OpenToSlot: in.OpenToSlot, Bands: store.JSON(bands), BufferSlots: in.BufferSlots,
			DeskHoldMinutes: in.DeskHoldMinutes, NoShowAfterMinutes: in.NoShowAfterMinutes,
			LicenceThresholdKWMilli: in.LicenceThresholdKWMilli, SkipperItemID: in.SkipperItemID, DamageItemID: in.DamageItemID,
			Checklist: store.JSON(checklist), CheckoutMinPhotos: in.CheckoutMinPhotos, CheckinMinPhotos: in.CheckinMinPhotos,
			DepositModes: encodeStrings(in.DepositModes), IdentityRetentionDays: in.IdentityRetentionDays,
			DefaultRegisterID: in.DefaultRegisterID, CreatedAt: cur.CreatedAt,
		}
		if err := store.SaveRentalSettings(ctx, tx, scope, row, ifMatch, s.now()); err != nil {
			return versionErr(err)
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_settings.update", "rental_settings", scope.ID(), map[string]any{
			"open_from_slot": in.OpenFromSlot, "open_to_slot": in.OpenToSlot, "buffer_slots": in.BufferSlots, "bands": len(in.Bands),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.LoadSettingsTx(ctx, s.d.DB.DB, scope)
}
