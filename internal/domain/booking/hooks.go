package booking

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Contract hooks and customer emails of online bookings (m8-plan §5.4, §6). The emails go to the address
// submitted with the booking (contact_enc), queued in the caller's transaction with a dedupe key (INV-77). Values
// are display strings; nothing here is logged.

func euro(c int64) string { return strings.Replace(money.Cents(c).String(), ".", ",", 1) + " €" }

func displayDate(d calendar.Date, locale string) string {
	s := d.String()
	if len(s) != 10 || locale == "en" {
		return s
	}
	return s[8:10] + "/" + s[5:7] + "/" + s[0:4]
}

// contactOf decrypts the contact of a booking (nil when none or unreadable).
func (s *Service) contactOf(ob *store.OnlineBooking) *Contact {
	if ob.ContactEnc == "" || s.d.Keys == nil {
		return nil
	}
	raw, err := s.d.Keys.DecryptString(ob.ContactEnc, contactAAD(ob.ID))
	if err != nil {
		return nil
	}
	var c Contact
	if json.Unmarshal([]byte(raw), &c) != nil {
		return nil
	}
	return &c
}

// mailBase are the values shared by the booking emails.
func (s *Service) mailBase(mc *marinaCtx, ob *store.OnlineBooking, c *store.Contract, ct *Contact) map[string]string {
	loc := ob.Locale
	v := map[string]string{
		notify.ValueMarinaName: mc.m.Name, "reference": ob.Reference,
		notify.ValueStartsOn: displayDate(c.StartsOn, loc), notify.ValueEndsOn: displayDate(c.EndsOn, loc),
	}
	if ct != nil {
		v[notify.ValueCustomerName] = strings.TrimSpace(ct.FirstName + " " + ct.LastName)
	}
	return v
}

// queueBookingMail queues one booking email to the submitted contact (nothing when there is none).
func (s *Service) queueBookingMail(ctx context.Context, tx bun.IDB, mc *marinaCtx, ob *store.OnlineBooking, c *store.Contract,
	template, dedupe string, extra map[string]string,
) error {
	if s.d.Notify == nil {
		return nil
	}
	ct := s.contactOf(ob)
	if ct == nil || ct.Email == "" {
		return nil
	}
	vals := s.mailBase(mc, ob, c, ct)
	for k, v := range extra {
		if v != "" {
			vals[k] = v
		}
	}
	_, _, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
		Template: template, MarinaID: mc.scope.ID(), CustomerID: c.CustomerID, To: ct.Email, Locale: ob.Locale,
		DedupeKey: dedupe, SourceType: "contract", SourceID: c.ID, Values: vals,
	})
	return err
}

// bookingFor returns the marina context and the online booking of a contract (nil booking when it is not one).
func (s *Service) bookingFor(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string) (*marinaCtx, *store.OnlineBooking, error) {
	ob, err := store.OnlineBookingByContract(ctx, tx, scope, contractID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	mc, err := s.scoped(ctx, tx, scope)
	if err != nil {
		return nil, nil, err
	}
	return mc, ob, nil
}

// arrivalValues are the arrival information of the confirmation email in the booking's locale.
func arrivalValues(info onlineconf.ArrivalInfo, locale string) map[string]string {
	text := info.Text.It
	if locale == "en" && info.Text.En != "" {
		text = info.Text.En
	}
	return map[string]string{"vhf_channel": info.VHFChannel, "marina_phone": info.Phone, "rules_url": info.RulesURL, "arrival_text": text}
}

// OnOnlineConfirmedTx implements contract.OnlineHooks: the confirmation email with arrival information, the
// berth when the policy reveals it, the transit QR payload and the customer-area link (m8-plan §5.4).
func (s *Service) OnOnlineConfirmedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract) error {
	mc, ob, err := s.bookingFor(ctx, tx, scope, c.ID)
	if err != nil || ob == nil {
		return err
	}
	extra := arrivalValues(mc.set.ArrivalInfo, ob.Locale)
	extra["qr"] = "PT1:" + c.ID
	if op := snapshotPolicy(c); op == nil || op.RevealBerth != onlineconf.RevealNever {
		if code, err := berthCode(ctx, tx, scope, c.ID); err == nil {
			extra["berth"] = code
		}
	}
	if s.links != nil && c.CustomerID != "" {
		tok, err := s.links.IssueBookingLinkTx(ctx, tx, scope, c.CustomerID, c.ID)
		if err != nil {
			return err
		}
		extra[notify.ValueLink] = s.links.LinkURL(mc.m.Slug, tok)
	}
	return s.queueBookingMail(ctx, tx, mc, ob, c, notify.TemplateBookingConfirmed, "booking-confirmed:"+c.ID, extra)
}

// OnOnlineApprovalRequestedTx implements contract.OnlineHooks: the "booking received" email (the office sees the
// request live through transit.approval_requested).
func (s *Service) OnOnlineApprovalRequestedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, _ string) error {
	mc, ob, err := s.bookingFor(ctx, tx, scope, c.ID)
	if err != nil || ob == nil {
		return err
	}
	extra := map[string]string{}
	if !ob.ApprovalDeadline.IsZero() {
		extra[notify.ValueDeadlineOn] = displayDate(calendar.In(ob.ApprovalDeadline, mc.loc), ob.Locale)
	}
	if ob.Channel == store.OnlineChannelOnline {
		extra[notify.ValueAmount] = euro(int64(dueNow(c)))
	}
	return s.queueBookingMail(ctx, tx, mc, ob, c, notify.TemplateBookingReceived, "booking-received:"+c.ID, extra)
}

// berthCode returns the code of the resource of the first active allocation of a contract.
func berthCode(ctx context.Context, db bun.IDB, scope store.MarinaScope, contractID string) (string, error) {
	rows, err := store.AllocationsOfContracts(ctx, db, scope, []string{contractID})
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Status == store.AllocActive {
			return resourceCode(ctx, db, scope, r.ResourceType, r.ResourceID)
		}
	}
	return "", store.ErrNotFound
}

func resourceCode(ctx context.Context, db bun.IDB, scope store.MarinaScope, rtype, id string) (string, error) {
	return store.ResourceCode(ctx, db, scope, rtype, id)
}
