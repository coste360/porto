package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
)

// Writes and lists of the M11 rental fleet (m11-plan §3; owned by FLEET). The frozen read helpers live in
// rental_fleet_models.go. Duplicate codes or vessels are unique violations (the service maps them to code_taken);
// every query is scoped by marina_id.

// InsertRentalUnit inserts a unit of the marina.
func InsertRentalUnit(ctx context.Context, tx bun.IDB, s MarinaScope, u *RentalUnit) error {
	u.MarinaID, u.Version = s.ID(), 1
	u.CreatedAt, u.UpdatedAt = UTC(u.CreatedAt), UTC(u.UpdatedAt)
	_, err := tx.NewInsert().Model(u).Exec(ctx)
	return err
}

// UpdateRentalUnit writes the mutable columns of a unit when the version equals expected and bumps it. The engine
// hours are written only by check-in (handover), never here.
func UpdateRentalUnit(ctx context.Context, tx bun.IDB, s MarinaScope, u *RentalUnit, expected int, now time.Time) error {
	u.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(u).
		Column("code", "category", "seats", "licence", "fuel_type", "tank_milli", "deposit_cents", "status", "home_berth_id",
			"item_id", "ownership", "owner_customer_id", "note", "sort", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", u.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRentalUnit(ctx, tx, s, u.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	u.Version = expected + 1
	return nil
}

// RentalUnitFilter filters ListRentalUnits.
type RentalUnitFilter struct {
	Status   string
	Category string
}

// ListRentalUnits returns a page of units ordered by id (creation order).
func ListRentalUnits(ctx context.Context, db bun.IDB, s MarinaScope, f RentalUnitFilter, cursor string, limit int) (Page[RentalUnit], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[RentalUnit]{}, err
	}
	limit = ClampLimit(limit)
	var rows []RentalUnit
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Category != "" {
		q = q.Where("category = ?", f.Category)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[RentalUnit]{}, err
	}
	return pageOf(rows, limit, func(u RentalUnit) string { return u.ID }), nil
}

// AllRentalUnits returns every unit of the marina by sort and code (reports, quotes).
func AllRentalUnits(ctx context.Context, db bun.IDB, s MarinaScope) ([]RentalUnit, error) {
	rows := []RentalUnit{}
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).OrderExpr("sort, code").Scan(ctx)
	return rows, err
}

// RentalUnitRefs are the registry data shown with units: vessel name and engines JSON, home berth code.
type RentalUnitRefs struct {
	VesselName map[string]string
	Engines    map[string]JSON
	BerthCode  map[string]string
}

// LoadRentalUnitRefs returns the vessel names and engines (by vessel id) and the berth codes (by berth id) of
// the units.
func LoadRentalUnitRefs(ctx context.Context, db bun.IDB, s MarinaScope, units []RentalUnit) (RentalUnitRefs, error) {
	out := RentalUnitRefs{VesselName: map[string]string{}, Engines: map[string]JSON{}, BerthCode: map[string]string{}}
	var vessels, berths []string
	for _, u := range units {
		vessels = append(vessels, u.VesselID)
		if u.HomeBerthID != "" {
			berths = append(berths, u.HomeBerthID)
		}
	}
	if len(vessels) > 0 {
		var rows []struct {
			ID      string `bun:"id"`
			Name    string `bun:"name"`
			Engines JSON   `bun:"engines"`
		}
		if err := db.NewSelect().Table("vessels").Column("id", "name", "engines").Where("marina_id = ?", s.ID()).
			Where("id IN (?)", bun.List(vessels)).Scan(ctx, &rows); err != nil {
			return out, err
		}
		for _, r := range rows {
			out.VesselName[r.ID], out.Engines[r.ID] = r.Name, r.Engines
		}
	}
	if len(berths) > 0 {
		var rows []struct {
			ID   string `bun:"id"`
			Code string `bun:"code"`
		}
		if err := db.NewSelect().Table("berths").Column("id", "code").Where("marina_id = ?", s.ID()).
			Where("id IN (?)", bun.List(berths)).Scan(ctx, &rows); err != nil {
			return out, err
		}
		for _, r := range rows {
			out.BerthCode[r.ID] = r.Code
		}
	}
	return out, nil
}

// EngineKWMilli sums vessels.engines[].power_kw (whole kW) as milli-kW; malformed JSON counts as 0.
func EngineKWMilli(engines JSON) int64 {
	var list []struct {
		PowerKW *int64 `json:"power_kw"`
	}
	if len(engines) == 0 || json.Unmarshal(engines, &list) != nil {
		return 0
	}
	var total int64
	for _, e := range list {
		if e.PowerKW != nil && *e.PowerKW > 0 {
			total += *e.PowerKW * 1000
		}
	}
	return total
}

// ItemRevenueClassCode returns the revenue class code of an item of the marina (ErrNotFound when missing).
func ItemRevenueClassCode(ctx context.Context, db bun.IDB, s MarinaScope, itemID string) (string, error) {
	if !ValidID(itemID) {
		return "", ErrNotFound
	}
	var code string
	err := db.NewSelect().TableExpr("items AS i").ColumnExpr("rc.code").
		Join("JOIN revenue_classes AS rc ON rc.id = i.revenue_class_id AND rc.marina_id = i.marina_id").
		Where("i.marina_id = ?", s.ID()).Where("i.id = ?", itemID).Scan(ctx, &code)
	if err != nil {
		return "", notFound(err)
	}
	return code, nil
}

// ---- skippers ----

// InsertRentalSkipper inserts a skipper.
func InsertRentalSkipper(ctx context.Context, tx bun.IDB, s MarinaScope, k *RentalSkipper) error {
	k.MarinaID, k.Version = s.ID(), 1
	k.CreatedAt, k.UpdatedAt = UTC(k.CreatedAt), UTC(k.UpdatedAt)
	_, err := tx.NewInsert().Model(k).Exec(ctx)
	return err
}

// UpdateRentalSkipper writes a skipper when the version equals expected and bumps it.
func UpdateRentalSkipper(ctx context.Context, tx bun.IDB, s MarinaScope, k *RentalSkipper, expected int, now time.Time) error {
	k.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(k).
		Column("name", "contact_enc", "licence_expires_on", "user_id", "active", "sort", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", k.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRentalSkipper(ctx, tx, s, k.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	k.Version = expected + 1
	return nil
}

// ListRentalSkippers returns a page of skippers ordered by id.
func ListRentalSkippers(ctx context.Context, db bun.IDB, s MarinaScope, active *bool, cursor string, limit int) (Page[RentalSkipper], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[RentalSkipper]{}, err
	}
	limit = ClampLimit(limit)
	var rows []RentalSkipper
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if active != nil {
		q = q.Where("active = ?", *active)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[RentalSkipper]{}, err
	}
	return pageOf(rows, limit, func(k RentalSkipper) string { return k.ID }), nil
}

// RentalSkipperHoursOf returns the weekly hours of the skippers, by skipper id.
func RentalSkipperHoursOf(ctx context.Context, db bun.IDB, s MarinaScope, skipperIDs []string) (map[string][]RentalSkipperHours, error) {
	out := map[string][]RentalSkipperHours{}
	if len(skipperIDs) == 0 {
		return out, nil
	}
	var rows []RentalSkipperHours
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("skipper_id IN (?)", bun.List(skipperIDs)).
		Order("skipper_id", "weekday", "from_slot").Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.SkipperID] = append(out[r.SkipperID], r)
	}
	return out, nil
}

// ReplaceRentalSkipperHours deletes the weekly hours of a skipper and inserts rows.
func ReplaceRentalSkipperHours(ctx context.Context, tx bun.IDB, s MarinaScope, skipperID string, rows []RentalSkipperHours) error {
	if _, err := tx.NewDelete().Model((*RentalSkipperHours)(nil)).Where("marina_id = ?", s.ID()).Where("skipper_id = ?", skipperID).Exec(ctx); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].MarinaID, rows[i].SkipperID = s.ID(), skipperID
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// TouchRentalSkipper bumps the version of a skipper when it equals expected (hours replaced).
func TouchRentalSkipper(ctx context.Context, tx bun.IDB, s MarinaScope, id string, expected int, now time.Time) error {
	res, err := tx.NewUpdate().Table("rental_skippers").Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRentalSkipper(ctx, tx, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// ---- formulas ----

// InsertRentalFormula inserts a formula.
func InsertRentalFormula(ctx context.Context, tx bun.IDB, s MarinaScope, f *RentalFormula) error {
	f.MarinaID, f.Version = s.ID(), 1
	f.CreatedAt, f.UpdatedAt = UTC(f.CreatedAt), UTC(f.UpdatedAt)
	_, err := tx.NewInsert().Model(f).Exec(ctx)
	return err
}

// UpdateRentalFormula writes a formula when the version equals expected and bumps it (the kind never changes).
func UpdateRentalFormula(ctx context.Context, tx bun.IDB, s MarinaScope, f *RentalFormula, expected int, now time.Time) error {
	f.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(f).
		Column("code", "name", "band_code", "min_slots", "max_slots", "min_days", "max_days", "skipper", "fuel", "fuel_item_id",
			"late_item_id", "grace_minutes", "late_step_minutes", "categories", "active", "sort", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", f.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRentalFormula(ctx, tx, s, f.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	f.Version = expected + 1
	return nil
}

// ListRentalFormulas returns the formulas by sort and code.
func ListRentalFormulas(ctx context.Context, db bun.IDB, s MarinaScope, active *bool) ([]RentalFormula, error) {
	rows := []RentalFormula{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if active != nil {
		q = q.Where("active = ?", *active)
	}
	err := q.OrderExpr("sort, code").Scan(ctx)
	return rows, err
}

// ---- extras ----

// InsertRentalExtra inserts an extra.
func InsertRentalExtra(ctx context.Context, tx bun.IDB, s MarinaScope, e *RentalExtra) error {
	e.MarinaID, e.Version = s.ID(), 1
	e.CreatedAt, e.UpdatedAt = UTC(e.CreatedAt), UTC(e.UpdatedAt)
	_, err := tx.NewInsert().Model(e).Exec(ctx)
	return err
}

// UpdateRentalExtra writes an extra when the version equals expected and bumps it (the item never changes).
func UpdateRentalExtra(ctx context.Context, tx bun.IDB, s MarinaScope, e *RentalExtra, expected int, now time.Time) error {
	e.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(e).
		Column("charge_mode", "default_qty_milli", "max_qty_milli", "categories", "active", "sort", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", e.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetRentalExtra(ctx, tx, s, e.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	e.Version = expected + 1
	return nil
}

// RentalExtraRow is an extra with its item code and name.
type RentalExtraRow struct {
	RentalExtra
	ItemCode string
	ItemName JSON
}

// ListRentalExtras returns the extras by sort with their item code and name.
func ListRentalExtras(ctx context.Context, db bun.IDB, s MarinaScope, active *bool) ([]RentalExtraRow, error) {
	rows := []RentalExtra{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if active != nil {
		q = q.Where("active = ?", *active)
	}
	if err := q.OrderExpr("sort, id").Scan(ctx); err != nil {
		return nil, err
	}
	return withExtraItems(ctx, db, s, rows)
}

func withExtraItems(ctx context.Context, db bun.IDB, s MarinaScope, rows []RentalExtra) ([]RentalExtraRow, error) {
	out := make([]RentalExtraRow, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ItemID)
	}
	items, err := ItemsByIDs(ctx, db, s, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, r := range rows {
		it := byID[r.ItemID]
		out = append(out, RentalExtraRow{RentalExtra: r, ItemCode: it.Code, ItemName: it.Name})
	}
	return out, nil
}

// RentalExtraByID returns one extra with its item code and name.
func RentalExtraByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalExtraRow, error) {
	e, err := GetRentalExtra(ctx, db, s, id)
	if err != nil {
		return nil, err
	}
	rows, err := withExtraItems(ctx, db, s, []RentalExtra{*e})
	if err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// ---- settings ----

// SaveRentalSettings inserts the settings of a marina (expected 0: never saved; a concurrent first save is a
// version mismatch) or updates them when the version equals expected, bumping it.
func SaveRentalSettings(ctx context.Context, tx bun.IDB, s MarinaScope, r *RentalSettings, expected int, now time.Time) error {
	r.MarinaID, r.UpdatedAt = s.ID(), UTC(now)
	if expected == 0 {
		r.Version, r.CreatedAt = 1, UTC(now)
		if _, err := tx.NewInsert().Model(r).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrVersionMismatch
			}
			return err
		}
		return nil
	}
	res, err := tx.NewUpdate().Model(r).
		Column("open_from_slot", "open_to_slot", "bands", "buffer_slots", "desk_hold_minutes", "no_show_after_minutes",
			"licence_threshold_kw_milli", "skipper_item_id", "damage_item_id", "checklist", "checkout_min_photos",
			"checkin_min_photos", "deposit_modes", "identity_retention_days", "default_register_id", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	r.Version = expected + 1
	return nil
}
