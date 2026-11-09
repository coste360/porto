package reservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/store"
)

var blockReasons = map[string]bool{
	store.RentalBlockMaintenance: true, store.RentalBlockClosure: true, store.RentalBlockDayOff: true, store.RentalBlockOther: true,
}

// maxBlockDays bounds a block (rows are written per slot).
const maxBlockDays = 366

func blockRange(b *store.RentalBlock) slots.Range {
	return slots.Range{Start: slots.Point{Date: b.FromOn, Slot: b.FromSlot}, End: slots.Point{Date: b.ToOn, Slot: b.ToSlot}}
}

func validBlockRange(v *auth.ValidationError, r slots.Range) {
	if !slots.WellFormed(r) {
		v.Add("to", "invalid")
		return
	}
	if slots.Days(r) > maxBlockDays {
		v.Add("to", "out_of_range")
	}
}

// ListBlocks lists the blocks of a window.
func (s *Service) ListBlocks(ctx context.Context, p *auth.Principal, marinaID string, f BlockFilter) ([]store.RentalBlock, error) {
	scope, err := s.authorize(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return store.ListRentalBlocks(ctx, s.d.DB.DB, scope, store.RentalBlockFilter{From: f.From, To: f.To, UnitID: f.UnitID, SkipperID: f.SkipperID})
}

func (s *Service) blockAudit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action string, b *store.RentalBlock) error {
	diff := map[string]any{
		"from_on": b.FromOn, "from_slot": b.FromSlot, "to_on": b.ToOn, "to_slot": b.ToSlot, "reason": b.Reason,
	}
	if b.UnitID != "" {
		diff["unit_id"] = b.UnitID
	}
	if b.SkipperID != "" {
		diff["skipper_id"] = b.SkipperID
	}
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: "rental_block",
		EntityID: b.ID, Diff: diff,
	})
}

// lockTarget locks the unit or skipper of a block (the lock of every allocation on it).
func lockTarget(ctx context.Context, tx bun.IDB, scope store.MarinaScope, b *store.RentalBlock) error {
	if b.UnitID != "" {
		if _, err := store.LockRentalUnit(ctx, tx, scope, b.UnitID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return invalid("unit_id", "not_found")
			}
			return err
		}
		return nil
	}
	if _, err := store.LockRentalSkipper(ctx, tx, scope, b.SkipperID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return invalid("skipper_id", "not_found")
		}
		return err
	}
	return nil
}

// CreateBlock blocks a unit or a skipper (block rows through the same insert-and-count path).
func (s *Service) CreateBlock(ctx context.Context, p *auth.Principal, marinaID string, in BlockInput) (*store.RentalBlock, error) {
	scope, err := s.authorize(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if (in.UnitID == "") == (in.SkipperID == "") {
		v.Add("unit_id", "exactly_one_target")
	}
	if !blockReasons[in.Reason] {
		v.Add("reason", "invalid")
	}
	note := cleanText(v, "note", in.Note, 200)
	validBlockRange(v, in.Range)
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	b := &store.RentalBlock{
		ID: store.NewID(), UnitID: in.UnitID, SkipperID: in.SkipperID, FromOn: in.Range.Start.Date, FromSlot: in.Range.Start.Slot,
		ToOn: in.Range.End.Date, ToSlot: in.Range.End.Slot, Reason: in.Reason, Note: note, CreatedByUserID: p.UserID,
		CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := lockTarget(ctx, tx, scope, b); err != nil {
			return err
		}
		if err := store.InsertRentalBlock(ctx, tx, scope, b); err != nil {
			return err
		}
		if err := s.insertBlockTx(ctx, tx, scope, b, now); err != nil {
			return err
		}
		if err := s.blockAudit(ctx, tx, scope, p.Actor(), "rental_block.create", b); err != nil {
			return err
		}
		return s.emitEntity(ctx, tx, scope, "block", "block", b.ID, b.UnitID, b.SkipperID)
	})
	if err != nil {
		return nil, err
	}
	return store.GetRentalBlock(ctx, s.d.DB.DB, scope, b.ID)
}

// UpdateBlock changes a block (If-Match).
func (s *Service) UpdateBlock(ctx context.Context, p *auth.Principal, marinaID, blockID string, ifMatch int, in BlockPatch) (*store.RentalBlock, error) {
	scope, err := s.authorize(ctx, p, marinaID, store.RentalTableBlocks, blockID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if in.Reason != nil && !blockReasons[*in.Reason] {
		v.Add("reason", "invalid")
	}
	var note *string
	if in.Note != nil {
		n := cleanText(v, "note", *in.Note, 200)
		note = &n
	}
	if in.Range != nil {
		validBlockRange(v, *in.Range)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockRentalBlock(ctx, tx, scope, blockID)
		if err != nil {
			return notFound(err)
		}
		if b.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := lockTarget(ctx, tx, scope, b); err != nil {
			return err
		}
		if in.Reason != nil {
			b.Reason = *in.Reason
		}
		if note != nil {
			b.Note = *note
		}
		if in.Range != nil && *in.Range != blockRange(b) {
			b.FromOn, b.FromSlot, b.ToOn, b.ToSlot = in.Range.Start.Date, in.Range.Start.Slot, in.Range.End.Date, in.Range.End.Slot
			if err := s.insertBlockTx(ctx, tx, scope, b, now); err != nil {
				return err
			}
		}
		if err := store.UpdateRentalBlock(ctx, tx, scope, b, ifMatch, now); err != nil {
			return versionErr(err)
		}
		if err := s.blockAudit(ctx, tx, scope, p.Actor(), "rental_block.update", b); err != nil {
			return err
		}
		return s.emitEntity(ctx, tx, scope, "block", "block", b.ID, b.UnitID, b.SkipperID)
	})
	if err != nil {
		return nil, err
	}
	return store.GetRentalBlock(ctx, s.d.DB.DB, scope, blockID)
}

// DeleteBlock deletes a block and its rows (If-Match).
func (s *Service) DeleteBlock(ctx context.Context, p *auth.Principal, marinaID, blockID string, ifMatch int) error {
	scope, err := s.authorize(ctx, p, marinaID, store.RentalTableBlocks, blockID)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockRentalBlock(ctx, tx, scope, blockID)
		if err != nil {
			return notFound(err)
		}
		if b.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := lockTarget(ctx, tx, scope, b); err != nil {
			return err
		}
		if err := store.DeleteRentalBlock(ctx, tx, scope, b.ID, ifMatch); err != nil {
			return versionErr(err)
		}
		if err := s.blockAudit(ctx, tx, scope, p.Actor(), "rental_block.delete", b); err != nil {
			return err
		}
		return s.emitEntity(ctx, tx, scope, "block", "block", b.ID, b.UnitID, b.SkipperID)
	})
}

// TemplateRentalConfirmed is the optional "noleggio confermato" email (spec 28); it is sent only when the notify
// package knows the template.
const TemplateRentalConfirmed = notify.TemplateRentalConfirmed

// confirmEmailTx queues the rental_confirmed email to the renter (walk-in email) or the customer (first email),
// once per booking (dedupe key). Values are display strings; the address is encrypted by notify.
func (s *Service) confirmEmailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, b *store.RentalBooking, renter *Renter, loc *time.Location) error {
	if s.d.Notify == nil {
		return nil
	}
	if _, err := notify.Render(TemplateRentalConfirmed, "it", notify.RenderData{}); err != nil {
		return nil // template not registered
	}
	to, locale, name := "", "it", ""
	if renter != nil {
		to, name = renter.Email, renter.Name
	}
	if to == "" && b.CustomerID != "" {
		c, err := store.GetCustomer(ctx, tx, scope, b.CustomerID)
		if err == nil {
			var emails []struct {
				Value string `json:"value"`
			}
			_ = decodeJSON(c.Emails, &emails)
			if len(emails) > 0 {
				to = emails[0].Value
			}
			if c.Locale != "" {
				locale = c.Locale
			}
			name = c.CompanyName
			if name == "" {
				name = c.FirstName + " " + c.LastName
			}
		}
	}
	if to == "" {
		return nil
	}
	unitCode := ""
	if u, err := store.GetRentalUnit(ctx, tx, scope, b.UnitID); err == nil {
		unitCode = u.Code
	}
	at := func(d slots.Point) string {
		return slots.Instant(d.Date, d.Slot, loc).In(loc).Format("2006-01-02 15:04")
	}
	r := RangeOf(b)
	_, _, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
		Template: TemplateRentalConfirmed, MarinaID: scope.ID(), CustomerID: b.CustomerID, To: to, Locale: locale,
		DedupeKey: "rental_confirmed:" + b.ID, SourceType: "rental_booking", SourceID: b.ID,
		Values: map[string]string{
			notify.ValueMarinaName: store.RentalMarinaName(ctx, tx, scope), notify.ValueCustomerName: name, "reference": Number(b.Year, b.Number), notify.ValueStartsOn: at(r.Start),
			notify.ValueEndsOn: at(r.End), notify.ValueAmount: "€ " + moneyOf(b.TotalCents).String(), "unit": unitCode,
			"qr": fmt.Sprintf("PR1:%s", b.ID),
		},
	})
	return err
}
