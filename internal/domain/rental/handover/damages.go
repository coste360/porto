package handover

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Damages (m11-plan §8 step 3): recorded on a checked-out booking with an estimate and photos (kind damage of the
// check-in handover); decided at check-in (charged: covered by the retained deposit or the return charge; waived
// with a reason). Only an open damage changes.

func (s *Service) damageView(ctx context.Context, db bun.IDB, scope store.MarinaScope, d *store.RentalDamage) (*DamageView, error) {
	files, err := store.RentalDamageFilesOf(ctx, db, scope, []string{d.ID})
	if err != nil {
		return nil, err
	}
	v := &DamageView{RentalDamage: *d, Files: []FileView{}}
	for _, f := range files {
		v.Files = append(v.Files, FileView{RentalHandoverFile: f.RentalHandoverFile, ContentType: f.ContentType})
	}
	return v, nil
}

func checkDescription(desc string) (string, error) {
	desc = strings.TrimSpace(desc)
	if n := utf8.RuneCountInString(desc); n == 0 || n > 500 {
		return "", invalid("description", "invalid")
	}
	return desc, nil
}

// CreateDamage records a damage of a checked-out booking.
func (s *Service) CreateDamage(ctx context.Context, p *auth.Principal, marinaID, bookingID string, in DamageInput) (*DamageView, error) {
	scope, err := s.bookingScope(ctx, p, marinaID, bookingID)
	if err != nil {
		return nil, err
	}
	desc, err := checkDescription(in.Description)
	if err != nil {
		return nil, err
	}
	if in.Estimate < 0 {
		return nil, invalid("estimate_cents", "positive")
	}
	var out *store.RentalDamage
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := s.lock(ctx, tx, scope, bookingID, 0, store.RentalStatusCheckedOut)
		if err != nil {
			return err
		}
		now := s.now()
		out = &store.RentalDamage{
			ID: store.NewID(), MarinaID: scope.ID(), BookingID: b.ID, Description: desc, EstimateCents: int64(in.Estimate),
			Status: store.RentalDamageOpen, CreatedByUserID: p.UserID, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertRentalDamage(ctx, tx, out); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "rental_damage.create", "rental_damage", out.ID, map[string]any{
			"booking_id": b.ID, "estimate_cents": out.EstimateCents,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b, "damage")
	})
	if err != nil {
		return nil, err
	}
	return s.damageView(ctx, s.d.DB.DB, scope, out)
}

// lockDamageTx locks the booking of a damage (checked out) and the damage (open, expected version).
func (s *Service) lockDamageTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, damageID string, ifMatch int) (*store.RentalBooking, *store.RentalDamage, error) {
	pre, err := store.GetRentalDamage(ctx, tx, scope, damageID)
	if err != nil {
		return nil, nil, notFound(err)
	}
	b, err := s.lock(ctx, tx, scope, pre.BookingID, 0, store.RentalStatusCheckedOut)
	if err != nil {
		return nil, nil, err
	}
	d, err := store.LockRentalDamage(ctx, tx, scope, damageID)
	if err != nil {
		return nil, nil, notFound(err)
	}
	if d.Version != ifMatch {
		return nil, nil, ErrVersionMismatch
	}
	if d.Status != store.RentalDamageOpen {
		return nil, nil, ErrInvalidTransition
	}
	return b, d, nil
}

// UpdateDamage changes or waives a damage (If-Match).
func (s *Service) UpdateDamage(ctx context.Context, p *auth.Principal, marinaID, damageID string, ifMatch int, in DamagePatch) (*DamageView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage, store.RentalTableDamages, damageID)
	if err != nil {
		return nil, err
	}
	if in.Description != nil {
		d, err := checkDescription(*in.Description)
		if err != nil {
			return nil, err
		}
		in.Description = &d
	}
	if in.Estimate != nil && *in.Estimate < 0 {
		return nil, invalid("estimate_cents", "positive")
	}
	in.WaiveReason = strings.TrimSpace(in.WaiveReason)
	if utf8.RuneCountInString(in.WaiveReason) > 200 {
		return nil, invalid("waive_reason", "too_long")
	}
	if in.Waived != nil && *in.Waived && in.WaiveReason == "" {
		return nil, invalid("waive_reason", "required")
	}
	var out *store.RentalDamage
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, d, err := s.lockDamageTx(ctx, tx, scope, damageID, ifMatch)
		if err != nil {
			return err
		}
		if in.Description != nil {
			d.Description = *in.Description
		}
		if in.Estimate != nil {
			d.EstimateCents = int64(*in.Estimate)
		}
		if in.Waived != nil && *in.Waived {
			d.Status, d.WaiveReason = store.RentalDamageWaived, in.WaiveReason
		}
		if err := store.UpdateRentalDamage(ctx, tx, scope, d, s.now()); err != nil {
			return err
		}
		out = d
		if err := s.audit(ctx, tx, scope, p.Actor(), "rental_damage.update", "rental_damage", d.ID, map[string]any{
			"booking_id": b.ID, "estimate_cents": d.EstimateCents, "status": d.Status,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b, "damage")
	})
	if err != nil {
		return nil, err
	}
	return s.damageView(ctx, s.d.DB.DB, scope, out)
}

// DeleteDamage deletes an open damage before check-in (If-Match).
func (s *Service) DeleteDamage(ctx context.Context, p *auth.Principal, marinaID, damageID string, ifMatch int) error {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermRentalManage, store.RentalTableDamages, damageID)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, d, err := s.lockDamageTx(ctx, tx, scope, damageID, ifMatch)
		if err != nil {
			return err
		}
		if err := store.DeleteRentalDamage(ctx, tx, scope, d.ID); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "rental_damage.delete", "rental_damage", d.ID, map[string]any{
			"booking_id": b.ID,
		}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, b, "damage")
	})
}
