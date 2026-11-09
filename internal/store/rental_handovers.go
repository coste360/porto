package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// Queries of the rental handovers (m11-plan §7, §8, §12; owned by HANDOVER): the check-out and check-in drafts, their
// files, and the deposit columns of rental_bookings that the handover service owns (deposit mode, status, payment,
// intent, decision, retained amount). rental_bookings.status, the slot rows and hold_expires_at are written only by
// reservation (INV-97): nothing here touches them.

// RentalHandoverFileRow is a handover file with its stored file metadata.
type RentalHandoverFileRow struct {
	RentalHandoverFile `bun:",extend"`
	ContentType        string `bun:"content_type"`
	SHA256             string `bun:"sha256"`
	StorageKey         string `bun:"storage_key"`
}

// RentalBookingByDepositIntent returns the booking whose deposit pre-authorisation is intentID.
func RentalBookingByDepositIntent(ctx context.Context, db bun.IDB, s MarinaScope, intentID string) (*RentalBooking, error) {
	if !ValidID(intentID) {
		return nil, ErrNotFound
	}
	row := new(RentalBooking)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("deposit_intent_id = ?", intentID).Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// RentalDepositColumns are the deposit columns of rental_bookings the handover service may set.
var RentalDepositColumns = map[string]bool{
	"deposit_mode": true, "deposit_cents": true, "deposit_status": true, "deposit_payment_id": true,
	"deposit_intent_id": true, "deposit_decision_cents": true, "deposit_retained_cents": true,
	"deposit_waive_reason": true,
}

// SetRentalBookingDeposit sets deposit columns of a booking (only the RentalDepositColumns; nil clears a nullable
// column). The booking version is not bumped: deposit progress is not an If-Match edit, and the provider events
// change it asynchronously.
func SetRentalBookingDeposit(ctx context.Context, db bun.IDB, s MarinaScope, id string, set map[string]any, now time.Time) error {
	q := db.NewUpdate().Table("rental_bookings").Where("marina_id = ?", s.ID()).Where("id = ?", id)
	for k, v := range set {
		if !RentalDepositColumns[k] {
			return errors.New("store: not a rental deposit column: " + k)
		}
		q = q.Set("? = ?", bun.Ident(k), v)
	}
	res, err := q.Set("updated_at = ?", UTC(now)).Exec(ctx)
	return expectOne(res, err)
}

// RentalDepositsToSettle returns the pre-authorised deposits whose decision waits for a capture or a void (job
// rental.deposits).
func RentalDepositsToSettle(ctx context.Context, db bun.IDB, s MarinaScope, limit int) ([]RentalBooking, error) {
	out := []RentalBooking{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("deposit_mode = ?", RentalDepositPreauth).
		Where("deposit_status = ?", RentalDepositStatusCapturePending).Where("deposit_intent_id IS NOT NULL").
		OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// GetRentalHandover returns the handover of a booking phase.
func GetRentalHandover(ctx context.Context, db bun.IDB, s MarinaScope, bookingID, phase string) (*RentalHandover, error) {
	return rentalHandover(ctx, db, s, bookingID, phase, false)
}

// LockRentalHandover locks the handover of a booking phase.
func LockRentalHandover(ctx context.Context, db bun.IDB, s MarinaScope, bookingID, phase string) (*RentalHandover, error) {
	return rentalHandover(ctx, db, s, bookingID, phase, true)
}

func rentalHandover(ctx context.Context, db bun.IDB, s MarinaScope, bookingID, phase string, lock bool) (*RentalHandover, error) {
	if !ValidID(bookingID) {
		return nil, ErrNotFound
	}
	row := new(RentalHandover)
	q := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("booking_id = ?", bookingID).Where("phase = ?", phase)
	if lock && IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// RentalHandoverByClientRef returns the handover last saved with an offline client_ref.
func RentalHandoverByClientRef(ctx context.Context, db bun.IDB, s MarinaScope, ref string) (*RentalHandover, error) {
	row := new(RentalHandover)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("client_ref = ?", ref).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// InsertRentalHandover inserts a handover (a concurrent insert of the same booking phase is ErrConflict).
func InsertRentalHandover(ctx context.Context, db bun.IDB, h *RentalHandover) error {
	_, err := db.NewInsert().Model(h).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateRentalHandover writes the mutable columns of a handover, conditional on its version (bumped).
func UpdateRentalHandover(ctx context.Context, db bun.IDB, s MarinaScope, h *RentalHandover, now time.Time) error {
	res, err := db.NewUpdate().Model(h).
		Column("checklist", "fuel_eighths", "engine_hours_milli", "refuel_milli", "identity_enc", "identity_purged_at",
			"notes", "actual_at", "client_ref", "device_at", "completed_at", "completed_by_user_id").
		Set("version = version + 1").Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", h.ID).Where("version = ?", h.Version).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	if err := expectOne(res, err); err != nil {
		return err
	}
	h.Version++
	h.UpdatedAt = UTC(now)
	return nil
}

// InsertRentalHandoverFile inserts a handover file row (a replayed client_ref is ErrConflict).
func InsertRentalHandoverFile(ctx context.Context, db bun.IDB, f *RentalHandoverFile) error {
	_, err := db.NewInsert().Model(f).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

func handoverFileQuery(db bun.IDB, s MarinaScope, out *[]RentalHandoverFileRow) *bun.SelectQuery {
	return db.NewSelect().Model(out).ModelTableExpr("rental_handover_files AS rhf").
		ColumnExpr("rhf.*").ColumnExpr("sf.content_type, sf.sha256, sf.storage_key").
		Join("JOIN stored_files AS sf ON sf.id = rhf.file_id AND sf.marina_id = rhf.marina_id").
		Where("rhf.marina_id = ?", s.ID()).Where("sf.deleted_at IS NULL")
}

// RentalHandoverFilesOf returns the live files of handovers (and of damages, by damage id), oldest first.
func RentalHandoverFilesOf(ctx context.Context, db bun.IDB, s MarinaScope, handoverIDs []string) ([]RentalHandoverFileRow, error) {
	out := []RentalHandoverFileRow{}
	if len(handoverIDs) == 0 {
		return out, nil
	}
	err := handoverFileQuery(db, s, &out).Where("rhf.handover_id IN (?)", bun.List(handoverIDs)).
		OrderExpr("rhf.created_at, rhf.id").Scan(ctx)
	return out, err
}

// RentalDamageFilesOf returns the live photos of damages, oldest first.
func RentalDamageFilesOf(ctx context.Context, db bun.IDB, s MarinaScope, damageIDs []string) ([]RentalHandoverFileRow, error) {
	out := []RentalHandoverFileRow{}
	if len(damageIDs) == 0 {
		return out, nil
	}
	err := handoverFileQuery(db, s, &out).Where("rhf.damage_id IN (?)", bun.List(damageIDs)).
		OrderExpr("rhf.created_at, rhf.id").Scan(ctx)
	return out, err
}

// RentalHandoverFileByClientRef returns the file uploaded with an offline client_ref.
func RentalHandoverFileByClientRef(ctx context.Context, db bun.IDB, s MarinaScope, ref string) (*RentalHandoverFileRow, error) {
	out := []RentalHandoverFileRow{}
	if err := handoverFileQuery(db, s, &out).Where("rhf.client_ref = ?", ref).Limit(1).Scan(ctx); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

// DeleteRentalHandoverFiles deletes handover file rows by id.
func DeleteRentalHandoverFiles(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := db.NewDelete().TableExpr("rental_handover_files").Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(ids)).Exec(ctx)
	return err
}

// RentalIdentityToPurge returns the check-out handovers holding identity data (or identity photos) of bookings
// closed, cancelled or marked no-show before the instant (job rental.identity_retention).
func RentalIdentityToPurge(ctx context.Context, db bun.IDB, s MarinaScope, before time.Time, limit int) ([]RentalHandover, error) {
	out := []RentalHandover{}
	err := db.NewSelect().Model(&out).ModelTableExpr("rental_handovers AS rh").ColumnExpr("rh.*").
		Join("JOIN rental_bookings AS rb ON rb.id = rh.booking_id AND rb.marina_id = rh.marina_id").
		Where("rh.marina_id = ?", s.ID()).Where("rh.phase = ?", RentalPhaseCheckout).
		Where("rh.identity_purged_at IS NULL").
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("rb.closed_at < ?", UTC(before)).WhereOr("rb.cancelled_at < ?", UTC(before)).
				WhereOr("rb.no_show_at < ?", UTC(before))
		}).
		OrderExpr("rh.id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// RentalHandoverExists reports whether a handover row exists (used by tests and the seed).
func RentalHandoverExists(ctx context.Context, db bun.IDB, s MarinaScope, bookingID, phase string) (bool, error) {
	_, err := GetRentalHandover(ctx, db, s, bookingID, phase)
	if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// RentalContractSignature returns the contract signature request of a booking (subject rental): the signed one when
// any, else the most recent.
func RentalContractSignature(ctx context.Context, db bun.IDB, s MarinaScope, bookingID string) (*SignatureRequest, error) {
	row := new(SignatureRequest)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("subject_type = ?", SignatureSubjectRental).
		Where("subject_id = ?", bookingID).OrderExpr("CASE WHEN status = 'signed' THEN 0 ELSE 1 END, created_at DESC, id DESC").
		Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// RentalDocument returns the latest live unsigned generated document of a kind for a subject id (the booking for
// rental_contract, the handover for rental_handover).
func RentalDocument(ctx context.Context, db bun.IDB, s MarinaScope, kind, subjectID string) (*GeneratedDocument, error) {
	row := new(GeneratedDocument)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("subject_type = ?", SubjectRentalBooking).
		Where("subject_id = ?", subjectID).Where("kind = ?", kind).Where("signed = ?", false).
		Where("status <> ?", DocSuperseded).OrderExpr("created_at DESC, id DESC").Limit(1).Scan(ctx)
	if err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// RaiseRentalUnitEngineHours sets the engine hours of a unit when higher than the stored ones (check-in, §8.1).
func RaiseRentalUnitEngineHours(ctx context.Context, db bun.IDB, s MarinaScope, unitID string, milli int64, now time.Time) error {
	_, err := db.NewUpdate().Table("rental_units").Set("engine_hours_milli = ?", milli).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", unitID).Where("engine_hours_milli < ?", milli).Exec(ctx)
	return err
}

// RentalHandoverByID returns a handover of the marina by id.
func RentalHandoverByID(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RentalHandover, error) {
	return getRentalRow[RentalHandover](ctx, db, s, id, false)
}
