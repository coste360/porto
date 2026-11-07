package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of contract_addenda (spec 13.4; m4-plan §3.5).

// InsertAddendum inserts an addendum; a seq already taken is ErrConflict (the caller retries with the next).
func InsertAddendum(ctx context.Context, db bun.IDB, a *ContractAddendum) error {
	_, err := db.NewInsert().Model(a).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// NextAddendumSeq returns the next seq of the addenda of a contract.
func NextAddendumSeq(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (int, error) {
	var n int
	err := db.NewSelect().Model((*ContractAddendum)(nil)).ColumnExpr("COALESCE(MAX(seq), 0)").
		Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).Scan(ctx, &n)
	return n + 1, err
}

// LockAddendum returns an addendum of a contract, locked for update on PostgreSQL.
func LockAddendum(ctx context.Context, db bun.IDB, s MarinaScope, contractID, id string) (*ContractAddendum, error) {
	if !ValidID(id) || !ValidID(contractID) {
		return nil, ErrNotFound
	}
	a := new(ContractAddendum)
	q := db.NewSelect().Model(a).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return a, nil
}

// GetAddendumByID returns an addendum of a marina by id (signature callbacks).
func GetAddendumByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ContractAddendum, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	a := new(ContractAddendum)
	if err := db.NewSelect().Model(a).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return a, nil
}

// UpdateAddendum writes a when its stored version equals expected and bumps the version.
func UpdateAddendum(ctx context.Context, db bun.IDB, s MarinaScope, expected int, a *ContractAddendum, now time.Time) error {
	a.UpdatedAt = UTC(now)
	res, err := db.NewUpdate().Model(a).ExcludeColumn("id", "marina_id", "contract_id", "seq", "created_at", "version").
		Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", a.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	a.Version = expected + 1
	return nil
}

// AddendaOfContract returns the addenda of a contract by seq.
func AddendaOfContract(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) ([]ContractAddendum, error) {
	out := []ContractAddendum{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).
		OrderExpr("seq").Scan(ctx)
	return out, err
}

// DeleteServiceLine deletes a service line of a contract (addendum service_remove); false when not a service
// line of the contract.
func DeleteServiceLine(ctx context.Context, db bun.IDB, s MarinaScope, contractID, lineID string) (bool, error) {
	if !ValidID(lineID) {
		return false, nil
	}
	res, err := db.NewDelete().Model((*ContractLine)(nil)).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).
		Where("id = ?", lineID).Where("line_kind = 'service'").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}
