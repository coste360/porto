package store

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of contracts and contract_lines (m3-plan §4, §5.1), plus the read helpers the contract and presence
// services need over allocations, releases, settings and resources (they never write occupancy rows: that is
// internal/domain/occupancy). Every function is scoped by the marina.

// ContractFilter narrows ListContracts.
type ContractFilter struct {
	Kind        string
	Status      string
	Term        string
	CustomerID  string
	VesselID    string
	ActiveOn    *calendar.Date // starts_on ≤ d < ends_on
	ArrivalFrom *calendar.Date // starts_on ≥
	ArrivalTo   *calendar.Date // starts_on ≤
	Search      string         // vessel name (folded) or number
	Number      int64          // exact number when the search is numeric
}

// InsertContract inserts a contract; a number already taken is ErrConflict.
func InsertContract(ctx context.Context, db bun.IDB, c *Contract) error {
	_, err := db.NewInsert().Model(c).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetContract returns one contract of a marina.
func GetContract(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Contract, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(Contract)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// LockContract returns one contract of a marina, locked for update on PostgreSQL (SQLite write transactions are
// already serialised by the write queue). State changes start with it so two concurrent actions on the same
// contract run one after the other.
func LockContract(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Contract, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(Contract)
	q := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// ContractsByIDs returns the contracts of a marina with the given ids.
func ContractsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Contract, error) {
	out := []Contract{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).OrderExpr("id").Scan(ctx)
	return out, err
}

var contractColumns = []string{
	"status", "customer_id", "billing_customer_id", "starts_on", "ends_on", "price_list_id", "price_snapshot",
	"engine_version", "total_cents", "deposit_cents", "policy", "expected_arrival_time", "expected_departure_time",
	"notes", "hold_lost", "activated_at", "signed_on_paper", "confirmed_at", "confirmed_by_user_id",
	"confirmation_source", "confirmation_ref", "pay_at_departure", "cancelled_at", "cancel_reason", "terminated_on",
	"terminate_reason", "updated_at",
}

// UpdateContract writes every mutable column when the stored version equals expected and bumps the version.
func UpdateContract(ctx context.Context, db bun.IDB, s MarinaScope, expected int, c *Contract) error {
	res, err := db.NewUpdate().Model(c).Column(contractColumns...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetContract(ctx, db, s, c.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	c.Version = expected + 1
	return nil
}

// ListContracts returns a page of contracts, newest first.
func ListContracts(ctx context.Context, db bun.IDB, s MarinaScope, f ContractFilter, cursor string, limit int) (Page[Contract], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Contract]{}, err
	}
	rows := []Contract{}
	q := db.NewSelect().Model(&rows).Where("contract.marina_id = ?", s.ID())
	if f.Kind != "" {
		q = q.Where("contract.kind = ?", f.Kind)
	}
	if f.Status != "" {
		q = q.Where("contract.status = ?", f.Status)
	}
	if f.Term != "" {
		q = q.Where("contract.term = ?", f.Term)
	}
	if f.CustomerID != "" {
		if !ValidID(f.CustomerID) {
			return Page[Contract]{Items: []Contract{}}, nil
		}
		q = q.Where("(contract.customer_id = ? OR contract.billing_customer_id = ?)", f.CustomerID, f.CustomerID)
	}
	if f.VesselID != "" {
		if !ValidID(f.VesselID) {
			return Page[Contract]{Items: []Contract{}}, nil
		}
		q = q.Where("contract.vessel_id = ?", f.VesselID)
	}
	if f.ActiveOn != nil {
		q = q.Where("contract.starts_on <= ?", *f.ActiveOn).Where("contract.ends_on > ?", *f.ActiveOn)
	}
	if f.ArrivalFrom != nil {
		q = q.Where("contract.starts_on >= ?", *f.ArrivalFrom)
	}
	if f.ArrivalTo != nil {
		q = q.Where("contract.starts_on <= ?", *f.ArrivalTo)
	}
	if f.Search != "" {
		sub := "EXISTS (SELECT 1 FROM vessels AS v WHERE v.id = contract.vessel_id AND v.marina_id = contract.marina_id AND v.search_text LIKE ? ESCAPE '!')"
		if f.Number > 0 {
			q = q.Where("("+sub+" OR contract.number = ?)", likePattern(f.Search), f.Number)
		} else {
			q = q.Where(sub, likePattern(f.Search))
		}
	}
	if after != "" {
		q = q.Where("contract.id < ?", after)
	}
	if err := q.OrderExpr("contract.id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Contract]{}, err
	}
	return pageOf(rows, limit, func(c Contract) string { return c.ID }), nil
}

// InsertContractLines inserts lines of a contract.
func InsertContractLines(ctx context.Context, db bun.IDB, lines []ContractLine) error {
	if len(lines) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&lines).Exec(ctx)
	return err
}

// LinesOfContracts returns the lines of the given contracts, ordered by contract and seq.
func LinesOfContracts(ctx context.Context, db bun.IDB, s MarinaScope, contractIDs []string) ([]ContractLine, error) {
	out := []ContractLine{}
	contractIDs = validIDs(contractIDs)
	if len(contractIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id IN (?)", bun.List(contractIDs)).
		OrderExpr("contract_id, seq").Scan(ctx)
	return out, err
}

// NextLineSeq returns the next free seq of the lines of a contract.
func NextLineSeq(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (int, error) {
	var n int
	err := db.NewRaw("SELECT COALESCE(MAX(seq), 0) + 1 FROM contract_lines WHERE marina_id = ? AND contract_id = ?", s.ID(), contractID).
		Scan(ctx, &n)
	return n, err
}

// ContractAllocation is an allocation with the contract of its line.
type ContractAllocation struct {
	Allocation `bun:",extend"`
	ContractID string `bun:"contract_id"`
}

// AllocationsOfContracts returns every allocation (any status) of the resource lines of the given contracts,
// ordered by contract, first night and creation.
func AllocationsOfContracts(ctx context.Context, db bun.IDB, s MarinaScope, contractIDs []string) ([]ContractAllocation, error) {
	out := []ContractAllocation{}
	contractIDs = validIDs(contractIDs)
	if len(contractIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).ModelTableExpr("allocations AS allocation").ColumnExpr("allocation.*").ColumnExpr("cl.contract_id").
		Join("JOIN contract_lines AS cl ON cl.id = allocation.contract_line_id AND cl.marina_id = allocation.marina_id").
		Where("allocation.marina_id = ?", s.ID()).Where("cl.contract_id IN (?)", bun.List(contractIDs)).
		OrderExpr("cl.contract_id, allocation.first_night, allocation.created_at, allocation.id").Scan(ctx)
	return out, err
}

// AllocationsByIDs returns allocations of a marina by id, ordered by id.
func AllocationsByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Allocation, error) {
	out := []Allocation{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).OrderExpr("id").Scan(ctx)
	return out, err
}

// ContractOfAllocation returns the contract id of an allocation's line ("" for quote holds and blocks).
func ContractOfAllocation(ctx context.Context, db bun.IDB, s MarinaScope, allocationID string) (string, error) {
	if !ValidID(allocationID) {
		return "", nil
	}
	var ids []string
	err := db.NewRaw(`SELECT cl.contract_id FROM allocations AS a JOIN contract_lines AS cl ON cl.id = a.contract_line_id
		WHERE a.marina_id = ? AND a.id = ?`, s.ID(), allocationID).Scan(ctx, &ids)
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

// ResourceRef is the code and map of a berth, dynamic area or land resource.
type ResourceRef struct {
	Type  string
	ID    string
	Code  string
	MapID string
}

// ResourceRefs returns the codes and maps of resources keyed by id (resource ids are UUIDs, unique across types).
func ResourceRefs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]ResourceRef, error) {
	out := map[string]ResourceRef{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		ID    string `bun:"id"`
		Code  string `bun:"code"`
		MapID string `bun:"map_id"`
	}
	for _, t := range []struct{ table, typ string }{
		{"berths", ResourceBerth}, {"dynamic_areas", ResourceArea}, {"land_resources", ResourceLand},
	} {
		var rows []row
		if err := db.NewSelect().TableExpr(t.table).Column("id", "code", "map_id").Where("marina_id = ?", s.ID()).
			Where("id IN (?)", bun.List(ids)).Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.ID] = ResourceRef{Type: t.typ, ID: r.ID, Code: r.Code, MapID: r.MapID}
		}
	}
	return out, nil
}

// ResourceExists reports whether a resource of the given type exists in the marina.
func ResourceExists(ctx context.Context, db bun.IDB, s MarinaScope, typ, id string) (bool, error) {
	table := map[string]string{ResourceBerth: "berths", ResourceArea: "dynamic_areas", ResourceLand: "land_resources"}[typ]
	if table == "" || !ValidID(id) {
		return false, nil
	}
	n, err := db.NewSelect().TableExpr(table).Where("marina_id = ?", s.ID()).Where("id = ?", id).Count(ctx)
	return n > 0, err
}

// ContractOccupancySettings returns the occupancy settings of the marina, or the defaults when the row does not
// exist yet (it is created lazily by the occupancy service).
func ContractOccupancySettings(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time) (*OccupancySettings, error) {
	row := new(OccupancySettings)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Scan(ctx)
	if err != nil {
		if errors.Is(notFound(err), ErrNotFound) {
			return DefaultOccupancySettings(s.ID(), now), nil
		}
		return nil, err
	}
	return row, nil
}

// ReleasesOfAllocations returns the releases of the given allocations, ordered by from_night.
func ReleasesOfAllocations(ctx context.Context, db bun.IDB, s MarinaScope, allocationIDs []string) ([]Release, error) {
	out := []Release{}
	allocationIDs = validIDs(allocationIDs)
	if len(allocationIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("allocation_id IN (?)", bun.List(allocationIDs)).
		OrderExpr("from_night, id").Scan(ctx)
	return out, err
}

// CloseReleasesFrom ends the active releases of a contract at a termination date: releases starting on or after
// effective are revoked, releases spanning it are shortened to effective − 1. The rows of those nights are
// already gone (the allocations were truncated). It returns the ids it changed.
func CloseReleasesFrom(ctx context.Context, db bun.IDB, s MarinaScope, contractID string, effective calendar.Date, now time.Time) ([]string, error) {
	var rows []Release
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).
		Where("status = 'active'").Where("to_night >= ?", effective).Scan(ctx); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		q := db.NewUpdate().Model((*Release)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", r.ID).
			Set("version = version + 1").Set("updated_at = ?", UTC(now))
		if !r.FromNight.Before(effective) {
			q = q.Set("status = 'revoked'").Set("revoked_at = ?", UTC(now))
		} else {
			q = q.Set("to_night = ?", effective.AddDays(-1))
		}
		if _, err := q.Exec(ctx); err != nil {
			return nil, err
		}
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// MarinaIDsForJobs returns the ids of the marinas that are not archived.
func MarinaIDsForJobs(ctx context.Context, db bun.IDB) ([]string, error) {
	var ids []string
	err := db.NewSelect().TableExpr("marinas").Column("id").Where("archived_at IS NULL").OrderExpr("id").Scan(ctx, &ids)
	return ids, err
}

// HeldTransitsToExpire returns the held transits of a marina whose hold is gone: no active allocation left, or an
// active hold allocation past its expiry (at most limit).
func HeldTransitsToExpire(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time, limit int) ([]Contract, error) {
	out := []Contract{}
	err := db.NewSelect().Model(&out).Where("contract.marina_id = ?", s.ID()).
		Where("contract.kind = ?", ContractKindTransit).Where("contract.status = 'held'").
		Where(`(NOT EXISTS (SELECT 1 FROM allocations AS a JOIN contract_lines AS cl ON cl.id = a.contract_line_id
			WHERE cl.contract_id = contract.id AND a.status = 'active')
		OR EXISTS (SELECT 1 FROM allocations AS a JOIN contract_lines AS cl ON cl.id = a.contract_line_id
			WHERE cl.contract_id = contract.id AND a.status = 'active' AND a.kind = 'hold' AND a.expires_at < ?))`, UTC(now)).
		OrderExpr("contract.id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// DraftContractsLostHold returns the draft mooring contracts of a marina not yet flagged hold_lost whose holds
// are gone or past expiry (at most limit).
func DraftContractsLostHold(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time, limit int) ([]Contract, error) {
	out := []Contract{}
	err := db.NewSelect().Model(&out).Where("contract.marina_id = ?", s.ID()).
		Where("contract.kind <> ?", ContractKindTransit).Where("contract.status = 'draft'").Where("contract.hold_lost = ?", false).
		Where(`(NOT EXISTS (SELECT 1 FROM allocations AS a JOIN contract_lines AS cl ON cl.id = a.contract_line_id
			WHERE cl.contract_id = contract.id AND a.status = 'active')
		OR EXISTS (SELECT 1 FROM allocations AS a JOIN contract_lines AS cl ON cl.id = a.contract_line_id
			WHERE cl.contract_id = contract.id AND a.status = 'active' AND a.kind = 'hold' AND a.expires_at < ?))`, UTC(now)).
		OrderExpr("contract.id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// ActiveContractsEnded returns the active mooring contracts of a marina whose end date is reached (at most limit).
func ActiveContractsEnded(ctx context.Context, db bun.IDB, s MarinaScope, today calendar.Date, limit int) ([]Contract, error) {
	out := []Contract{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("kind <> ?", ContractKindTransit).
		Where("status = 'active'").Where("ends_on <= ?", today).OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// CountContracts counts the contracts of a marina (seed idempotency).
func CountContracts(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Model((*Contract)(nil)).Where("marina_id = ?", s.ID()).Count(ctx))
}

// SeedResources returns the active berths (by code), linear dynamic areas and land resources of a marina (the
// M3 demo seed).
func SeedResources(ctx context.Context, db bun.IDB, s MarinaScope) ([]Berth, []DynamicArea, []LandResource, error) {
	var bs []Berth
	if err := db.NewSelect().Model(&bs).Where("marina_id = ?", s.ID()).Where("active = ?", true).OrderExpr("code, id").Scan(ctx); err != nil {
		return nil, nil, nil, err
	}
	var as []DynamicArea
	if err := db.NewSelect().Model(&as).Where("marina_id = ?", s.ID()).Where("active = ?", true).Where("kind = 'linear'").
		OrderExpr("code, id").Scan(ctx); err != nil {
		return nil, nil, nil, err
	}
	var ls []LandResource
	if err := db.NewSelect().Model(&ls).Where("marina_id = ?", s.ID()).Where("active = ?", true).OrderExpr("code, id").Scan(ctx); err != nil {
		return nil, nil, nil, err
	}
	return bs, as, ls, nil
}

// AllVessels returns the active vessels of a marina ordered by id (the M3 demo seed).
func AllVessels(ctx context.Context, db bun.IDB, s MarinaScope) ([]Vessel, error) {
	out := []Vessel{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("archived_at IS NULL").OrderExpr("id").Scan(ctx)
	return out, err
}

// ContractByLine returns the contract owning a contract line of a marina.
func ContractByLine(ctx context.Context, db bun.IDB, s MarinaScope, lineID string) (*Contract, error) {
	if !ValidID(lineID) {
		return nil, ErrNotFound
	}
	c := new(Contract)
	err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).
		Where("id = (SELECT l.contract_id FROM contract_lines l WHERE l.id = ? AND l.marina_id = ?)", lineID, s.ID()).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return c, nil
}
