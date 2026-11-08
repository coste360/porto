package booking

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Desk operations on online and channel bookings (m8-plan §6, §12.3; transits.desk). Another marina's booking is
// 404 (auth.ErrNotFound).

// PaymentLinkHours is the life of the payment link sent after an approval without authorisation (m8-plan §4.2).
const PaymentLinkHours = 24

func (s *Service) deskScope(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	return p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
}

// summaryOf builds a desk list row.
func (s *Service) summaryOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, ob *store.OnlineBooking, c *store.Contract, now time.Time) Summary {
	sm := Summary{
		ContractID: c.ID, Reference: ob.Reference, Code: contract.Code(c), Status: c.Status, Channel: ob.Channel,
		Arrival: c.StartsOn, Departure: c.EndsOn, ApprovalReason: ob.ApprovalReason, ApprovalDeadline: ob.ApprovalDeadline,
		Total: money.Cents(c.TotalCents), DueNow: dueNow(c), CreatedAt: ob.CreatedAt, Version: c.Version, Category: CategoryStandard,
	}
	if ob.Channel == store.OnlineChannelExternal {
		if cr, err := channelOfContract(ctx, db, scope, c.ID); err == nil {
			sm.ChannelCode = cr
		}
	}
	if exp, al, err := holdExpiry(ctx, db, scope, c.ID, now); err == nil {
		sm.HoldExpiresAt = exp
		if al != nil {
			if pc, err := store.ResourcePriceClass(ctx, db, scope, al.ResourceType, al.ResourceID); err == nil && pc != "" {
				sm.Category = pc
			}
		}
	}
	if v, err := store.GetVessel(ctx, db, scope, c.VesselID); err == nil {
		sm.LOACm, sm.BeamCm = geo.Cm(v.LOACm), geo.Cm(v.BeamCm)
	}
	return sm
}

func channelOfContract(ctx context.Context, db bun.IDB, scope store.MarinaScope, contractID string) (string, error) {
	var codes []string
	err := db.NewSelect().Model((*store.ChannelReservation)(nil)).Column("channel_code").Where("marina_id = ?", scope.ID()).
		Where("contract_id = ?", contractID).Limit(1).Scan(ctx, &codes)
	if err != nil || len(codes) == 0 {
		return "", store.ErrNotFound
	}
	return codes[0], nil
}

// List returns the online and channel bookings of the marina, newest first.
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter) (*Page, error) {
	scope, err := s.deskScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	page, err := store.ListOnlineBookings(ctx, db, scope, store.OnlineBookingFilter{Status: f.Status, Channel: f.Channel, From: f.From, To: f.To}, f.Cursor, f.Limit)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			return nil, &RulesError{Field: "cursor", Code: "invalid"}
		}
		return nil, err
	}
	out := &Page{Items: []Summary{}, NextCursor: page.NextCursor}
	now := s.now()
	for i := range page.Items {
		row := &page.Items[i]
		c, err := store.GetContract(ctx, db, scope, row.ContractID)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, s.summaryOf(ctx, db, scope, &row.OnlineBooking, c, now))
	}
	return out, nil
}

// Detail returns one online booking; the contact is decrypted only with customers.read.
func (s *Service) Detail(ctx context.Context, p *auth.Principal, marinaID, contractID string) (*Detail, error) {
	scope, err := s.deskScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, p, scope, contractID)
}

func (s *Service) detail(ctx context.Context, db bun.IDB, p *auth.Principal, scope store.MarinaScope, contractID string) (*Detail, error) {
	ob, err := store.OnlineBookingByContract(ctx, db, scope, contractID)
	if err != nil {
		return nil, auth.ErrNotFound
	}
	c, err := store.GetContract(ctx, db, scope, contractID)
	if err != nil {
		return nil, auth.ErrNotFound
	}
	mc, err := s.scoped(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	d := &Detail{
		Summary: s.summaryOf(ctx, db, scope, ob, c, s.now()), TransitCode: contract.Code(c), CustomerID: c.CustomerID,
		VesselID: c.VesselID, PeopleOnBoard: ob.PeopleOnBoard, ExpectedArrivalTime: c.ExpectedArrivalTime, Notes: c.Notes,
		Locale: ob.Locale, ApprovedAt: ob.ApprovedAt, ApprovedByUserID: ob.ApprovedByUserID, RejectedAt: ob.RejectedAt,
		RejectReason: ob.RejectReason, Documents: []DeskDocument{}, Consents: []DeskConsent{},
	}
	if p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead) {
		d.Contact = s.contactOf(ob)
	}
	docs, _, err := s.bookingDocuments(ctx, db, mc, c, "")
	if err != nil {
		return nil, err
	}
	for _, x := range docs {
		row, err := store.GetDocument(ctx, db, scope, x.ID)
		if err != nil {
			continue
		}
		d.Documents = append(d.Documents, DeskDocument{
			ID: row.ID, Type: row.Type, FileName: row.FileName, ExpiresOn: row.ExpiresOn, UploadedAt: row.CreatedAt,
			VerifiedAt: row.VerifiedAt, VerifiedByUserID: row.VerifiedByUserID,
		})
	}
	if c.CustomerID != "" {
		cs, err := store.ListConsents(ctx, db, scope, c.CustomerID)
		if err != nil {
			return nil, err
		}
		for _, x := range cs {
			if x.Source == "online" {
				d.Consents = append(d.Consents, DeskConsent{Kind: x.Kind, Version: x.PolicyVersion, Granted: x.Granted, RecordedAt: x.RecordedAt})
			}
		}
	}
	d.Payment = PaymentState{State: "none"}
	if in, err := store.LatestTransitIntent(ctx, db, scope, c.ID); err == nil {
		st := in.Status
		switch {
		case !in.VoidedAt.IsZero():
			st = "voided"
		case in.Status == store.IntentProcessing && !in.AuthorizedAt.IsZero():
			st = "authorized"
		}
		d.Payment = PaymentState{
			State: st, IntentID: in.ID, ProviderCode: in.ProviderCode, Amount: money.Cents(in.AmountCents),
			CaptureMethod: in.CaptureMethod, AuthorizedAt: in.AuthorizedAt, CapturedAt: in.CapturedAt,
		}
		if w, err := store.GetWorkItemState(ctx, db, scope, "capture_failed", in.ID); err == nil && w.Status == store.WorkItemOpen {
			d.Payment.CaptureFailed = true
		}
	}
	return d, nil
}

// authorizedIntent returns the live authorised intent of a transit (nil when none).
func authorizedIntent(ctx context.Context, db bun.IDB, scope store.MarinaScope, contractID string) *store.PaymentIntent {
	in, err := store.LiveTransitIntent(ctx, db, scope, contractID)
	if err != nil || in.AuthorizedAt.IsZero() || !in.VoidedAt.IsZero() {
		return nil
	}
	return in
}

// Approve approves a booking awaiting approval (m8-plan §6): capture after commit when authorised, otherwise
// pending_payment with a 24 h payment link; without an online provider the transit stays awaiting_approval until
// the desk confirms it with the manual driver.
func (s *Service) Approve(ctx context.Context, p *auth.Principal, marinaID, contractID string, ifMatch int) (*Detail, error) {
	scope, err := s.deskScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	if _, err := store.OnlineBookingByContract(ctx, db, scope, contractID); err != nil {
		return nil, auth.ErrNotFound
	}
	who := contract.Desk(p)
	var capture string
	// An approved booking that stays awaiting_approval (capture or manual confirmation pending) keeps its hold for
	// the payment link window.
	holdUntil := s.now().Add(PaymentLinkHours * time.Hour)
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.LockContract(ctx, tx, scope, contractID)
		if err != nil {
			return auth.ErrNotFound
		}
		if c.Status != string(contract.StatusAwaitingApproval) {
			return &contract.TransitionError{From: c.Status, Action: contract.ActionApprove}
		}
		if in := authorizedIntent(ctx, tx, scope, c.ID); in != nil {
			if _, err := s.d.Contracts.MarkApprovedTx(ctx, tx, scope, who, c.ID, ifMatch, holdUntil); err != nil {
				return err
			}
			capture = in.ID
			return nil
		}
		if _, perr := s.d.Collect.OnlineCheckoutProvider(ctx, tx, scope); perr != nil {
			_, err := s.d.Contracts.MarkApprovedTx(ctx, tx, scope, who, c.ID, ifMatch, holdUntil)
			return err
		}
		return s.approveWithLinkTx(ctx, tx, scope, who, c, ifMatch)
	})
	if err != nil {
		return nil, err
	}
	if capture != "" {
		if err := s.d.Collect.CaptureIntent(context.WithoutCancel(ctx), scope, capture); err != nil {
			s.d.Log.WarnContext(ctx, "booking capture failed", "marina_id", scope.ID(), "contract_id", contractID, "err", err)
		}
	}
	return s.detail(ctx, db, p, scope, contractID)
}

// approveWithLinkTx moves an awaiting_approval transit to pending_payment with a 24 h payment link and emails it.
func (s *Service) approveWithLinkTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, who contract.Who, c *store.Contract, ifMatch int) error {
	until := s.now().Add(PaymentLinkHours * time.Hour)
	c, err := s.d.Contracts.ApproveTx(ctx, tx, scope, who, c.ID, ifMatch, until)
	if err != nil {
		return err
	}
	link, err := s.d.Collect.OnlineTransitLinkTx(ctx, tx, scope, who.Actor, c.ID, c.CustomerID, int64(dueNow(c)), until)
	if err != nil {
		return err
	}
	mc, ob, err := s.bookingFor(ctx, tx, scope, c.ID)
	if err != nil || ob == nil {
		return err
	}
	return s.queueBookingMail(ctx, tx, mc, ob, c, notify.TemplateBookingApprovedPay, "booking-approved-pay:"+c.ID+":"+strconv.Itoa(c.Version), map[string]string{
		notify.ValueLink: link, notify.ValueAmount: euro(int64(dueNow(c))),
		notify.ValueExpiresAt: displayDate(calendar.In(until, mc.loc), ob.Locale),
	})
}

// Reject rejects a booking awaiting approval, releases the hold and the quotas, voids the authorisation after
// commit, and emails the customer when notify is set.
func (s *Service) Reject(ctx context.Context, p *auth.Principal, marinaID, contractID string, ifMatch int, reason string, notifyCustomer bool) (*Detail, error) {
	scope, err := s.deskScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	if _, err := store.OnlineBookingByContract(ctx, db, scope, contractID); err != nil {
		return nil, auth.ErrNotFound
	}
	void, err := s.rejectTx(ctx, scope, contract.Desk(p), contractID, ifMatch, contract.ActionReject, reason, notifyCustomer)
	if err != nil {
		return nil, err
	}
	if void != "" {
		if err := s.d.Collect.VoidIntent(context.WithoutCancel(ctx), scope, void); err != nil {
			s.d.Log.WarnContext(ctx, "booking void failed", "marina_id", scope.ID(), "contract_id", contractID, "err", err)
		}
	}
	return s.detail(ctx, db, p, scope, contractID)
}

// rejectTx rejects (or expires) an awaiting_approval transit and returns the authorised intent to void.
func (s *Service) rejectTx(ctx context.Context, scope store.MarinaScope, who contract.Who, contractID string, ifMatch int,
	action contract.Action, reason string, notifyCustomer bool,
) (string, error) {
	var void string
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if in := authorizedIntent(ctx, tx, scope, contractID); in != nil {
			void = in.ID
		}
		c, err := s.d.Contracts.RejectTx(ctx, tx, scope, who, contractID, ifMatch, action, reason)
		if err != nil {
			return err
		}
		if !notifyCustomer {
			return nil
		}
		mc, ob, err := s.bookingFor(ctx, tx, scope, c.ID)
		if err != nil || ob == nil {
			return err
		}
		return s.queueBookingMail(ctx, tx, mc, ob, c, notify.TemplateBookingRejected, "booking-rejected:"+c.ID, nil)
	})
	return void, err
}

// SendPaymentLink approves without the authorisation and emails a payment link (after a capture failure).
func (s *Service) SendPaymentLink(ctx context.Context, p *auth.Principal, marinaID, contractID string, ifMatch int) (*Detail, error) {
	scope, err := s.deskScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	if _, err := store.OnlineBookingByContract(ctx, db, scope, contractID); err != nil {
		return nil, auth.ErrNotFound
	}
	who := contract.Desk(p)
	var void string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.LockContract(ctx, tx, scope, contractID)
		if err != nil {
			return auth.ErrNotFound
		}
		if c.Status != string(contract.StatusAwaitingApproval) {
			return &contract.TransitionError{From: c.Status, Action: contract.ActionApprove}
		}
		if in := authorizedIntent(ctx, tx, scope, c.ID); in != nil {
			void = in.ID
		}
		return s.approveWithLinkTx(ctx, tx, scope, who, c, ifMatch)
	})
	if err != nil {
		return nil, err
	}
	if void != "" {
		if err := s.d.Collect.VoidIntent(context.WithoutCancel(ctx), scope, void); err != nil {
			s.d.Log.WarnContext(ctx, "booking void failed", "marina_id", scope.ID(), "contract_id", contractID, "err", err)
		}
	}
	return s.detail(ctx, db, p, scope, contractID)
}
