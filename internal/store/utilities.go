package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of utility meters, readings, consumptions, billings and settings (M9, m9-plan §9). Owned by UTILITIES.
// A replayed offline reading is deduplicated by meter_readings_client_ref_uidx (INV-83); a consumption is claimed
// by one charge only through a conditional update (INV-90).

// ConsumptionFrozenStatuses are the statuses a recomputation never touches.
var ConsumptionFrozenStatuses = []string{ConsumptionCharged, ConsumptionExcluded}

// ---- settings ----

// DefaultUtilitySettings returns the settings of a marina without a row.
func DefaultUtilitySettings(marinaID string, now time.Time) *UtilitySettings {
	return &UtilitySettings{MarinaID: marinaID, Mode: UtilityModeWithInstallment, Version: 1, CreatedAt: UTC(now), UpdatedAt: UTC(now)}
}

// GetUtilitySettings returns the settings of the marina, or the defaults (version 1) when the row is missing.
func GetUtilitySettings(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time) (*UtilitySettings, error) {
	row := new(UtilitySettings)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Scan(ctx)
	if err != nil {
		if errors.Is(notFound(err), ErrNotFound) {
			return DefaultUtilitySettings(s.ID(), now), nil
		}
		return nil, err
	}
	return row, nil
}

// UpdateUtilitySettings writes the settings when the stored version equals expected (a missing row has version 1
// and is created first).
func UpdateUtilitySettings(ctx context.Context, tx bun.IDB, s MarinaScope, u *UtilitySettings, expected int, now time.Time) error {
	seed := DefaultUtilitySettings(s.ID(), now)
	if _, err := tx.NewInsert().Model(seed).On("CONFLICT DO NOTHING").Exec(ctx); err != nil {
		return err
	}
	u.MarinaID, u.UpdatedAt = s.ID(), UTC(now)
	res, err := tx.NewUpdate().Model(u).
		Column("power_item_id", "water_item_id", "mode", "power_daily_threshold_milli", "water_daily_threshold_milli", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	u.Version = expected + 1
	return nil
}

// ---- meters ----

// InsertUtilityMeter inserts a meter of the marina.
func InsertUtilityMeter(ctx context.Context, tx bun.IDB, s MarinaScope, m *UtilityMeter) error {
	m.MarinaID = s.ID()
	if m.Version == 0 {
		m.Version = 1
	}
	m.CreatedAt, m.UpdatedAt = UTC(m.CreatedAt), UTC(m.UpdatedAt)
	_, err := tx.NewInsert().Model(m).Exec(ctx)
	return err
}

// GetUtilityMeter returns a meter of the marina.
func GetUtilityMeter(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*UtilityMeter, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(UtilityMeter)
	if err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// LockUtilityMeter reads a meter FOR UPDATE on PostgreSQL: readings and recomputations of a meter are serialised.
func LockUtilityMeter(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*UtilityMeter, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(UtilityMeter)
	q := tx.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// UpdateUtilityMeter writes the mutable columns when the version equals expected and bumps it.
func UpdateUtilityMeter(ctx context.Context, tx bun.IDB, s MarinaScope, m *UtilityMeter, expected int, now time.Time) error {
	m.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(m).
		Column("code", "pedestal_id", "socket_code", "serial", "resource_type", "resource_id", "equipment_id", "item_id",
			"multiplier_milli", "rollover_milli", "daily_threshold_milli", "active", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", m.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetUtilityMeter(ctx, tx, s, m.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	m.Version = expected + 1
	return nil
}

// UtilityMeterFilter filters the meters.
type UtilityMeterFilter struct {
	Kinds      []string
	PedestalID string
	ResourceID string
	Active     *bool
	Q          string // code, serial or socket (case-insensitive substring)
}

// ListUtilityMeters returns a page of meters ordered by id.
func ListUtilityMeters(ctx context.Context, db bun.IDB, s MarinaScope, f UtilityMeterFilter, cursor string, limit int) (Page[UtilityMeter], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[UtilityMeter]{}, err
	}
	limit = ClampLimit(limit)
	var rows []UtilityMeter
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if len(f.Kinds) > 0 {
		q = q.Where("kind IN (?)", bun.List(f.Kinds))
	}
	for col, id := range map[string]string{"pedestal_id": f.PedestalID, "resource_id": f.ResourceID} {
		if id == "" {
			continue
		}
		if !ValidID(id) {
			return Page[UtilityMeter]{Items: []UtilityMeter{}}, nil
		}
		q = q.Where(col+" = ?", id)
	}
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	if q2 := strings.ToLower(strings.TrimSpace(f.Q)); q2 != "" {
		like := "%" + strings.NewReplacer("%", "", "_", "").Replace(q2) + "%"
		q = q.WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("LOWER(code) LIKE ?", like).WhereOr("LOWER(COALESCE(serial, '')) LIKE ?", like).
				WhereOr("LOWER(COALESCE(socket_code, '')) LIKE ?", like)
		})
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[UtilityMeter]{}, err
	}
	return pageOf(rows, limit, func(m UtilityMeter) string { return m.ID }), nil
}

// ---- readings ----

// InsertMeterReadingIfAbsent inserts a reading unless a unique key holds it (same client_ref, or the same meter
// and instant): it reports whether the row was inserted.
func InsertMeterReadingIfAbsent(ctx context.Context, tx bun.IDB, s MarinaScope, r *MeterReading) (bool, error) {
	r.MarinaID = s.ID()
	if r.Version == 0 {
		r.Version = 1
	}
	r.AnomalyCodes = emptyJSONArray(r.AnomalyCodes)
	r.ReadAt, r.CreatedAt, r.UpdatedAt = UTC(r.ReadAt), UTC(r.CreatedAt), UTC(r.UpdatedAt)
	res, err := tx.NewInsert().Model(r).On("CONFLICT DO NOTHING").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// GetMeterReading returns a reading of the marina.
func GetMeterReading(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*MeterReading, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	r := new(MeterReading)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// MeterReadingByClientRef returns the reading recorded with a client_ref.
func MeterReadingByClientRef(ctx context.Context, db bun.IDB, s MarinaScope, clientRef string) (*MeterReading, error) {
	r := new(MeterReading)
	if err := db.NewSelect().Model(r).Where("marina_id = ?", s.ID()).Where("client_ref = ?", clientRef).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return r, nil
}

// UpdateMeterReading writes status, anomalies, void reason and photo when the version equals expected.
func UpdateMeterReading(ctx context.Context, tx bun.IDB, s MarinaScope, r *MeterReading, expected int, now time.Time) error {
	r.UpdatedAt = UTC(now)
	r.AnomalyCodes = emptyJSONArray(r.AnomalyCodes)
	res, err := tx.NewUpdate().Model(r).Column("status", "anomaly_codes", "void_reason", "photo_file_id", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetMeterReading(ctx, tx, s, r.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	r.Version = expected + 1
	return nil
}

// MeterReadingFilter filters the readings.
type MeterReadingFilter struct {
	MeterID  string
	Status   string
	From, To calendar.Date // read_on
}

// ListMeterReadings returns a page of readings, newest recorded first.
func ListMeterReadings(ctx context.Context, db bun.IDB, s MarinaScope, f MeterReadingFilter, cursor string, limit int) (Page[MeterReading], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[MeterReading]{}, err
	}
	limit = ClampLimit(limit)
	var rows []MeterReading
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.MeterID != "" {
		if !ValidID(f.MeterID) {
			return Page[MeterReading]{Items: []MeterReading{}}, nil
		}
		q = q.Where("meter_id = ?", f.MeterID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if !f.From.IsZero() {
		q = q.Where("read_on >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("read_on <= ?", f.To)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[MeterReading]{}, err
	}
	return pageOf(rows, limit, func(r MeterReading) string { return r.ID }), nil
}

// MeterReadingsOf returns the non-void readings of a meter ordered by read_at.
func MeterReadingsOf(ctx context.Context, db bun.IDB, s MarinaScope, meterID string) ([]MeterReading, error) {
	out := []MeterReading{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("meter_id = ?", meterID).
		Where("status <> ?", ReadingVoid).OrderExpr("read_at, id").Scan(ctx)
	return out, err
}

// LastMeterReadings returns the latest non-void reading of each meter.
func LastMeterReadings(ctx context.Context, db bun.IDB, s MarinaScope, meterIDs []string) (map[string]MeterReading, error) {
	out := map[string]MeterReading{}
	if len(meterIDs) == 0 {
		return out, nil
	}
	var rows []MeterReading
	err := db.NewSelect().Model(&rows).Where("meter_reading.marina_id = ?", s.ID()).
		Where("meter_reading.meter_id IN (?)", bun.List(meterIDs)).Where("meter_reading.status <> ?", ReadingVoid).
		Where("meter_reading.read_at = (SELECT MAX(r2.read_at) FROM meter_readings AS r2 WHERE r2.meter_id = meter_reading.meter_id AND r2.status <> ?)", ReadingVoid).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.MeterID] = r
	}
	return out, nil
}

// LastChargedReadAt returns the read_at of the latest reading closing a charged consumption of the meter (zero
// when none): earlier readings are out of order (m9-plan §9.2, D23).
func LastChargedReadAt(ctx context.Context, db bun.IDB, s MarinaScope, meterID string) (time.Time, error) {
	var rows []MeterReading
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("meter_id = ?", meterID).
		Where("id IN (SELECT c.to_reading_id FROM utility_consumptions AS c WHERE c.marina_id = ? AND c.meter_id = ? AND c.status = ?)",
			s.ID(), meterID, ConsumptionCharged).
		OrderExpr("read_at DESC").Limit(1).Scan(ctx)
	if err != nil || len(rows) == 0 {
		return time.Time{}, err
	}
	return rows[0].ReadAt, nil
}

// ReadingCharged reports whether a reading bounds a charged consumption.
func ReadingCharged(ctx context.Context, db bun.IDB, s MarinaScope, readingID string) (bool, error) {
	n, err := db.NewSelect().Model((*UtilityConsumption)(nil)).Where("marina_id = ?", s.ID()).
		Where("status = ?", ConsumptionCharged).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("from_reading_id = ?", readingID).WhereOr("to_reading_id = ?", readingID)
		}).Count(ctx)
	return n > 0, err
}

// ---- consumptions ----

// ConsumptionsOfMeter returns every consumption of a meter ordered by first night.
func ConsumptionsOfMeter(ctx context.Context, db bun.IDB, s MarinaScope, meterID string) ([]UtilityConsumption, error) {
	out := []UtilityConsumption{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("meter_id = ?", meterID).
		OrderExpr("first_night, id").Scan(ctx)
	return out, err
}

// InsertUtilityConsumptions inserts consumptions of the marina.
func InsertUtilityConsumptions(ctx context.Context, tx bun.IDB, s MarinaScope, rows []UtilityConsumption) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].MarinaID = s.ID()
		if rows[i].Version == 0 {
			rows[i].Version = 1
		}
		rows[i].CreatedAt, rows[i].UpdatedAt = UTC(rows[i].CreatedAt), UTC(rows[i].UpdatedAt)
	}
	_, err := tx.NewInsert().Model(&rows).Exec(ctx)
	return err
}

// DeleteOpenUtilityConsumptions deletes the given consumptions while none of them is charged or excluded.
func DeleteOpenUtilityConsumptions(ctx context.Context, tx bun.IDB, s MarinaScope, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	res, err := tx.NewDelete().Model((*UtilityConsumption)(nil)).Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(ids)).Where("status NOT IN (?)", bun.List(ConsumptionFrozenStatuses)).Exec(ctx)
	if err != nil {
		return err
	}
	if int(affected(res)) != len(ids) {
		return ErrVersionMismatch
	}
	return nil
}

// GetUtilityConsumption returns a consumption of the marina.
func GetUtilityConsumption(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*UtilityConsumption, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(UtilityConsumption)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// UpdateUtilityConsumption writes status, contract, customer, quantity and exclusion when the version equals
// expected and the row is not charged.
func UpdateUtilityConsumption(ctx context.Context, tx bun.IDB, s MarinaScope, c *UtilityConsumption, expected int, now time.Time) error {
	c.UpdatedAt = UTC(now)
	res, err := tx.NewUpdate().Model(c).Column("status", "contract_id", "customer_id", "qty_milli", "exclude_reason", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("version = ?", expected).
		Where("status <> ?", ConsumptionCharged).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetUtilityConsumption(ctx, tx, s, c.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	c.Version = expected + 1
	return nil
}

// UtilityConsumptionFilter filters the consumptions.
type UtilityConsumptionFilter struct {
	Status     string
	ContractID string
	MeterID    string
	Kind       string
	From, To   calendar.Date // intersecting nights
}

// ListUtilityConsumptions returns a page of consumptions ordered by id.
func ListUtilityConsumptions(ctx context.Context, db bun.IDB, s MarinaScope, f UtilityConsumptionFilter, cursor string, limit int) (Page[UtilityConsumption], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[UtilityConsumption]{}, err
	}
	limit = ClampLimit(limit)
	var rows []UtilityConsumption
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	for col, id := range map[string]string{"contract_id": f.ContractID, "meter_id": f.MeterID} {
		if id == "" {
			continue
		}
		if !ValidID(id) {
			return Page[UtilityConsumption]{Items: []UtilityConsumption{}}, nil
		}
		q = q.Where(col+" = ?", id)
	}
	if f.Kind != "" {
		q = q.Where("meter_id IN (SELECT m.id FROM utility_meters AS m WHERE m.marina_id = ? AND m.kind = ?)", s.ID(), f.Kind)
	}
	if !f.From.IsZero() {
		q = q.Where("last_night >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("first_night <= ?", f.To)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[UtilityConsumption]{}, err
	}
	return pageOf(rows, limit, func(c UtilityConsumption) string { return c.ID }), nil
}

// PendingConsumptionsOfContract returns the pending (chargeable) consumptions of a contract by first night.
func PendingConsumptionsOfContract(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) ([]UtilityConsumption, error) {
	out := []UtilityConsumption{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).
		Where("status = ?", ConsumptionPending).Where("charge_id IS NULL").OrderExpr("first_night, id").Scan(ctx)
	return out, err
}

// ContractsWithPendingConsumptions returns the contracts with pending consumptions.
func ContractsWithPendingConsumptions(ctx context.Context, db bun.IDB, s MarinaScope) ([]string, error) {
	var out []string
	err := db.NewSelect().Model((*UtilityConsumption)(nil)).ColumnExpr("DISTINCT CAST(contract_id AS TEXT)").
		Where("marina_id = ?", s.ID()).Where("status = ?", ConsumptionPending).Where("contract_id IS NOT NULL").
		OrderExpr("1").Scan(ctx, &out)
	return out, err
}

// ClaimConsumptions marks pending consumptions as charged by one charge and billing; it returns how many rows it
// claimed (fewer than len(ids) means another run took some: the caller rolls back, INV-90).
func ClaimConsumptions(ctx context.Context, tx bun.IDB, s MarinaScope, ids []string, chargeID, billingID string, now time.Time) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := tx.NewUpdate().Model((*UtilityConsumption)(nil)).
		Set("status = ?", ConsumptionCharged).Set("charge_id = ?", chargeID).Set("billing_id = ?", billingID).
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Where("status = ?", ConsumptionPending).
		Where("charge_id IS NULL").Exec(ctx)
	if err != nil {
		return 0, err
	}
	return int(affected(res)), nil
}

// ReopenConsumptionsOfCharge puts the consumptions charged by a (cancelled) charge back to pending, unlinked from
// the charge and its billing, and returns their ids.
func ReopenConsumptionsOfCharge(ctx context.Context, tx bun.IDB, s MarinaScope, chargeID string, now time.Time) ([]string, error) {
	var ids []string
	if err := tx.NewSelect().Model((*UtilityConsumption)(nil)).ColumnExpr("CAST(id AS TEXT)").Where("marina_id = ?", s.ID()).
		Where("charge_id = ?", chargeID).Where("status = ?", ConsumptionCharged).OrderExpr("id").Scan(ctx, &ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	_, err := tx.NewUpdate().Model((*UtilityConsumption)(nil)).
		Set("status = ?", ConsumptionPending).Set("charge_id = NULL").Set("billing_id = NULL").
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Exec(ctx)
	return ids, err
}

// ---- billings ----

// InsertUtilityBilling inserts a billing (the origin of a utility charge).
func InsertUtilityBilling(ctx context.Context, tx bun.IDB, s MarinaScope, b *UtilityBilling) error {
	b.MarinaID = s.ID()
	b.CreatedAt = UTC(b.CreatedAt)
	_, err := tx.NewInsert().Model(b).Exec(ctx)
	return err
}

// SetUtilityBillingCharge records the charge of a billing.
func SetUtilityBillingCharge(ctx context.Context, tx bun.IDB, s MarinaScope, billingID, chargeID string) error {
	return expectOne(tx.NewUpdate().Model((*UtilityBilling)(nil)).Set("charge_id = ?", chargeID).
		Where("marina_id = ?", s.ID()).Where("id = ?", billingID).Exec(ctx))
}

// ---- occupants and charging dates ----

// MeterOccupant is an allocation on the resource of a meter with its contract.
type MeterOccupant struct {
	AllocationID      string        `bun:"allocation_id"`
	Kind              string        `bun:"kind"`
	VesselID          string        `bun:"vessel_id"`
	FirstNight        calendar.Date `bun:"first_night"`
	LastNight         calendar.Date `bun:"last_night"`
	ContractID        string        `bun:"contract_id"`
	ContractKind      string        `bun:"contract_kind"`
	CustomerID        string        `bun:"customer_id"`
	BillingCustomerID string        `bun:"billing_customer_id"`
}

// MeterOccupants returns the active contract, transit and owner allocations on a resource intersecting [first,
// last], with the contract of their line, ordered by first night.
func MeterOccupants(ctx context.Context, db bun.IDB, s MarinaScope, resourceType, resourceID string, first, last calendar.Date) ([]MeterOccupant, error) {
	out := []MeterOccupant{}
	if !ValidID(resourceID) {
		return out, nil
	}
	err := db.NewSelect().TableExpr("allocations AS a").
		ColumnExpr("CAST(a.id AS TEXT) AS allocation_id, a.kind, COALESCE(CAST(a.vessel_id AS TEXT), '') AS vessel_id").
		ColumnExpr("a.first_night, a.last_night").
		ColumnExpr("COALESCE(CAST(c.id AS TEXT), '') AS contract_id, COALESCE(c.kind, '') AS contract_kind").
		ColumnExpr("COALESCE(CAST(c.customer_id AS TEXT), '') AS customer_id").
		ColumnExpr("COALESCE(CAST(c.billing_customer_id AS TEXT), '') AS billing_customer_id").
		Join("LEFT JOIN contract_lines AS l ON l.id = a.contract_line_id AND l.marina_id = a.marina_id").
		Join("LEFT JOIN contracts AS c ON c.id = l.contract_id AND c.marina_id = a.marina_id").
		Where("a.marina_id = ?", s.ID()).Where("a.resource_type = ?", resourceType).Where("a.resource_id = ?", resourceID).
		Where("a.status = ?", AllocActive).Where("a.kind IN (?)", bun.List([]string{AllocKindContract, AllocKindTransit, AllocKindOwner})).
		Where("a.last_night >= ?", first).Where("a.first_night <= ?", last).
		OrderExpr("a.first_night, a.id").Scan(ctx, &out)
	return out, err
}

// InstallmentChargeIssued returns the due date of a live instalment charge of the contract issued on day (ok false
// when none).
func InstallmentChargeIssued(ctx context.Context, db bun.IDB, s MarinaScope, contractID string, day calendar.Date) (calendar.Date, bool, error) {
	var rows []Charge
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).
		Where("origin_type = ?", ChargeOriginInstallment).Where("issued_on = ?", day).Where("corrects_charge_id IS NULL").
		Where("payment_status <> ?", "cancelled").OrderExpr("due_on").Limit(1).Scan(ctx)
	if err != nil || len(rows) == 0 {
		return calendar.Date{}, false, err
	}
	return rows[0].DueOn, true, nil
}

// LastNightOfContract returns the last night of the active allocations of a contract (zero when none).
func LastNightOfContract(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (calendar.Date, error) {
	var rows []Allocation
	err := db.NewSelect().Model(&rows).Where("allocation.marina_id = ?", s.ID()).Where("allocation.status = ?", AllocActive).
		Where("allocation.contract_line_id IN (SELECT l.id FROM contract_lines AS l WHERE l.marina_id = ? AND l.contract_id = ?)", s.ID(), contractID).
		OrderExpr("allocation.last_night DESC").Limit(1).Scan(ctx)
	if err != nil || len(rows) == 0 {
		return calendar.Date{}, err
	}
	return rows[0].LastNight, nil
}

// OccupiedNight is one night of an allocation on a resource.
type OccupiedNight struct {
	AllocationID string        `bun:"allocation_id"`
	Night        calendar.Date `bun:"night"`
}

// OccupiedNights returns the nights of [first, last] held on a resource, per allocation, from the occupancy rows
// (the truth per night: released and resold nights belong to the reseller), ordered by allocation and night.
func OccupiedNights(ctx context.Context, db bun.IDB, s MarinaScope, resourceType, resourceID string, first, last calendar.Date) ([]OccupiedNight, error) {
	out := []OccupiedNight{}
	if !ValidID(resourceID) {
		return out, nil
	}
	f := FamilyOf(resourceType)
	err := db.NewSelect().TableExpr(f.Table+" AS o").ColumnExpr("DISTINCT CAST(o.allocation_id AS TEXT) AS allocation_id, o.night").
		Where("o.marina_id = ?", s.ID()).Where("o."+f.Column+" = ?", resourceID).Where("o.night >= ?", first).Where("o.night <= ?", last).
		OrderExpr("allocation_id, o.night").Scan(ctx, &out)
	return out, err
}
