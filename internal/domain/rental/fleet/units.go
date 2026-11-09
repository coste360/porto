package fleet

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/rental/price"
	"github.com/coste360/porto/internal/store"
)

// ---- units (rental.manage) ----

// unitViews decorates units with the vessel name and engine power, the home berth code and the licence rule
// (without a skipper) for the marina threshold.
func (s *Service) unitViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, units []store.RentalUnit) ([]UnitView, error) {
	out := make([]UnitView, 0, len(units))
	if len(units) == 0 {
		return out, nil
	}
	refs, err := store.LoadRentalUnitRefs(ctx, db, scope, units)
	if err != nil {
		return nil, err
	}
	set, err := store.LoadRentalSettings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		kw := store.EngineKWMilli(refs.Engines[u.VesselID])
		out = append(out, UnitView{
			RentalUnit: u, VesselName: refs.VesselName[u.VesselID], EngineKWMilli: kw, HomeBerthCode: refs.BerthCode[u.HomeBerthID],
			LicenceRequired: price.LicenceRequired(u.Licence, kw, set.LicenceThresholdKWMilli, false),
		})
	}
	return out, nil
}

func (s *Service) unitView(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*UnitView, error) {
	u, err := store.GetRentalUnit(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	views, err := s.unitViews(ctx, db, scope, []store.RentalUnit{*u})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// ListUnits lists the units of the marina.
func (s *Service) ListUnits(ctx context.Context, p *auth.Principal, marinaID string, f UnitFilter) ([]UnitView, string, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, "", err
	}
	v := &auth.ValidationError{}
	if f.Status != "" && !slices.Contains(unitStatuses, f.Status) {
		v.Add("status", "invalid")
	}
	if f.Category != "" && !slices.Contains(categories, f.Category) {
		v.Add("category", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, "", err
	}
	page, err := store.ListRentalUnits(ctx, s.d.DB.DB, scope, store.RentalUnitFilter{Status: f.Status, Category: f.Category}, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.unitViews(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

// validateUnit checks a unit before a write (enums, ranges, references in the marina, rental item).
func (s *Service) validateUnit(ctx context.Context, db bun.IDB, scope store.MarinaScope, u *store.RentalUnit, checkVessel bool) error {
	v := &auth.ValidationError{}
	if !unitCodeRe.MatchString(u.Code) {
		v.Add("code", "invalid")
	}
	if !slices.Contains(categories, u.Category) {
		v.Add("category", "invalid")
	}
	if u.Seats < 1 || u.Seats > 99 {
		v.Add("seats", "out_of_range")
	}
	if !slices.Contains(licenceModes, u.Licence) {
		v.Add("licence", "invalid")
	}
	if !slices.Contains(fuelTypes, u.FuelType) {
		v.Add("fuel_type", "invalid")
	}
	switch {
	case u.TankMilli < 0 || u.TankMilli > 100_000_000:
		v.Add("tank_milli", "out_of_range")
	case u.TankMilli == 0 && u.FuelType != store.RentalFuelNone && u.FuelType != store.RentalFuelElectric:
		v.Add("tank_milli", "required")
	}
	if u.DepositCents < 0 || u.DepositCents > 100_000_000 {
		v.Add("deposit_cents", "out_of_range")
	}
	if !slices.Contains(unitStatuses, u.Status) {
		v.Add("status", "invalid")
	}
	if !slices.Contains(ownerships, u.Ownership) {
		v.Add("ownership", "invalid")
	}
	switch {
	case u.Ownership == store.RentalOwnershipThirdParty && u.OwnerCustomerID == "":
		v.Add("owner_customer_id", "required")
	case u.Ownership != store.RentalOwnershipThirdParty && u.OwnerCustomerID != "":
		v.Add("owner_customer_id", "not_allowed")
	}
	if len([]rune(u.Note)) > 500 {
		v.Add("note", "too_long")
	}
	if u.Sort < -100000 || u.Sort > 100000 {
		v.Add("sort", "out_of_range")
	}
	if checkVessel {
		if _, err := store.GetVessel(ctx, db, scope, u.VesselID); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v.Add("vessel_id", "not_found")
		}
	}
	if u.HomeBerthID != "" {
		if _, err := store.BerthByID(ctx, db, scope, u.HomeBerthID); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v.Add("home_berth_id", "not_found")
		}
	}
	if u.OwnerCustomerID != "" {
		if _, err := store.GetCustomer(ctx, db, scope, u.OwnerCustomerID); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
			v.Add("owner_customer_id", "not_found")
		}
	}
	if u.ItemID == "" {
		v.Add("item_id", "required")
	} else if err := itemCheck(ctx, db, scope, v, "item_id", u.ItemID, true); err != nil {
		return err
	}
	return v.Err()
}

func unitDiff(u *store.RentalUnit) map[string]any {
	return map[string]any{"code": u.Code, "category": u.Category, "status": u.Status, "licence": u.Licence, "deposit_cents": u.DepositCents}
}

// CreateUnit adds a registry vessel to the fleet.
func (s *Service) CreateUnit(ctx context.Context, p *auth.Principal, marinaID string, in UnitInput) (*UnitView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage)
	if err != nil {
		return nil, err
	}
	now := s.now()
	u := &store.RentalUnit{
		ID: store.NewID(), VesselID: in.VesselID, Code: strings.TrimSpace(in.Code), Category: in.Category, Seats: in.Seats,
		Licence: or(in.Licence, store.RentalLicenceThreshold), FuelType: in.FuelType, TankMilli: int64(in.Tank),
		DepositCents: int64(in.Deposit), Status: or(in.Status, store.RentalUnitAvailable), HomeBerthID: in.HomeBerthID,
		ItemID: in.ItemID, Ownership: or(in.Ownership, store.RentalOwnershipMarina), OwnerCustomerID: in.OwnerCustomerID,
		Note: in.Note, Sort: in.Sort, CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.validateUnit(ctx, tx, scope, u, true); err != nil {
			return err
		}
		if err := store.InsertRentalUnit(ctx, tx, scope, u); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return err
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_unit.create", "rental_unit", u.ID, unitDiff(u))
	})
	if err != nil {
		return nil, err
	}
	return s.unitView(ctx, s.d.DB.DB, scope, u.ID)
}

// GetUnit returns a unit.
func (s *Service) GetUnit(ctx context.Context, p *auth.Principal, marinaID, unitID string) (*UnitView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage, store.RentalTableUnits, unitID)
	if err != nil {
		return nil, err
	}
	return s.unitView(ctx, s.d.DB.DB, scope, unitID)
}

// UpdateUnit changes a unit (If-Match).
func (s *Service) UpdateUnit(ctx context.Context, p *auth.Principal, marinaID, unitID string, ifMatch int, in UnitPatch) (*UnitView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage, store.RentalTableUnits, unitID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		u, err := store.GetRentalUnit(ctx, tx, scope, unitID)
		if err != nil {
			return notFound(err)
		}
		if u.Version != ifMatch {
			return ErrVersionMismatch
		}
		setStr(&u.Code, in.Code)
		setStr(&u.Category, in.Category)
		setInt(&u.Seats, in.Seats)
		setStr(&u.Licence, in.Licence)
		setStr(&u.FuelType, in.FuelType)
		if in.Tank != nil {
			u.TankMilli = int64(*in.Tank)
		}
		if in.Deposit != nil {
			u.DepositCents = int64(*in.Deposit)
		}
		setStr(&u.Status, in.Status)
		setStr(&u.HomeBerthID, in.HomeBerthID)
		setStr(&u.ItemID, in.ItemID)
		setStr(&u.Ownership, in.Ownership)
		setStr(&u.OwnerCustomerID, in.OwnerCustomerID)
		if in.Ownership != nil && *in.Ownership == store.RentalOwnershipMarina && in.OwnerCustomerID == nil {
			u.OwnerCustomerID = ""
		}
		setStr(&u.Note, in.Note)
		setInt(&u.Sort, in.Sort)
		u.Code = strings.TrimSpace(u.Code)
		if err := s.validateUnit(ctx, tx, scope, u, false); err != nil {
			return err
		}
		if err := store.UpdateRentalUnit(ctx, tx, scope, u, ifMatch, s.now()); err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return versionErr(err)
		}
		return s.audit(ctx, tx, scope, p.Actor(), "rental_unit.update", "rental_unit", u.ID, unitDiff(u))
	})
	if err != nil {
		return nil, err
	}
	return s.unitView(ctx, s.d.DB.DB, scope, unitID)
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func setStr(dst *string, v *string) {
	if v != nil {
		*dst = strings.TrimSpace(*v)
	}
}

func setInt(dst *int, v *int) {
	if v != nil {
		*dst = *v
	}
}
