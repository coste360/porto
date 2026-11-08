//go:build integration

package acceptance

import (
	"context"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// deskPrincipal is the owner of the world as a domain principal (desk operations of the booking service).
func (w *m8World) deskPrincipal(t *testing.T) *auth.Principal {
	t.Helper()
	var id string
	if err := w.h.app.DB.NewSelect().TableExpr("users").Column("id").Where("email = ?", "owner@porto.test").Scan(context.Background(), &id); err != nil {
		t.Fatal(err)
	}
	return &auth.Principal{Kind: auth.KindUser, UserID: id, Grants: []auth.Grant{{OrganizationID: w.scope.OrgID(), Role: auth.RoleOwner}}}
}

func (w *m8World) approvalAlways(t *testing.T) {
	t.Helper()
	w.settings(t, func(s *store.OnlineBookingSettings) {
		s.Approval = store.JSON(`{"always":true,"hold_hours":48,"group_ids":[]}`)
	})
}

// authorize delivers the provider's authorized event of the last (manual capture) checkout.
func (w *m8World) authorize(t *testing.T, eventID, paymentRef string) {
	t.Helper()
	req := w.fake.LastCheckout()
	ev := ext.PaymentEvent{
		EventID: eventID, Kind: ext.PaymentEventAuthorized, PaymentRef: paymentRef, AmountCents: req.AmountCents,
		Metadata: map[string]string{"marina_id": w.marina, "intent_id": req.IntentID},
	}
	if code := w.postWebhook(t, ev, w.clock.Now()); code != 204 {
		t.Fatalf("authorized webhook %d", code)
	}
}

// TestApproval_AuthorizeCaptureConfirm (m8-plan §6): the card is authorised (manual capture), the booking waits for
// the office, the approval captures after commit and the capture webhook confirms.
func TestApproval_AuthorizeCaptureConfirm(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.approvalAlways(t)
		b := w.mustCreate(t, "k-appr-0001")
		if b["approval_required"] != true {
			t.Fatalf("approval not announced %v", b)
		}
		w.details(t, b, "appr@example.test")
		paid := w.pay(t, b)
		if str(paid, "status") != "pending_payment" || !w.fake.LastCheckout().CaptureManual {
			t.Fatalf("pay %v %+v", paid, w.fake.LastCheckout())
		}
		w.authorize(t, "evt_auth", "pi_auth")
		c := w.contractOf(t, str(b, "booking_id"))
		if c.Status != "awaiting_approval" {
			t.Fatalf("after authorisation %s", c.Status)
		}
		w.h.drainMail()
		if len(w.h.mailTo("appr@example.test", "booking_received")) != 1 {
			t.Fatal("booking_received missing")
		}
		w.invariants(t) // INV-08 for awaiting_approval
		p := w.deskPrincipal(t)
		d, err := w.h.app.Booking.Approve(context.Background(), p, w.marina, c.ID, c.Version)
		if err != nil {
			t.Fatal(err)
		}
		if d.Status != "awaiting_approval" || len(w.fake.Captures) != 1 || w.fake.Captures[0].IdempotencyKey == "" {
			t.Fatalf("approve %s captures %+v", d.Status, w.fake.Captures)
		}
		req := w.fake.LastCheckout()
		ev := ext.PaymentEvent{
			EventID: "evt_cap", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_auth", AmountCents: req.AmountCents,
			Metadata: map[string]string{"marina_id": w.marina, "intent_id": req.IntentID},
		}
		if code := w.postWebhook(t, ev, w.clock.Now()); code != 204 {
			t.Fatalf("capture webhook %d", code)
		}
		if c := w.contractOf(t, str(b, "booking_id")); c.Status != "confirmed" {
			t.Fatalf("not confirmed after capture: %s", c.Status)
		}
		w.invariants(t)
	})
}

// TestApproval_RejectVoidsAndReleases: a rejection cancels the hold, releases the quota, voids the authorisation and
// emails the customer.
func TestApproval_RejectVoidsAndReleases(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.approvalAlways(t)
		b := w.mustCreate(t, "k-rej-0001")
		w.details(t, b, "rej@example.test")
		w.pay(t, b)
		w.authorize(t, "evt_auth_r", "pi_auth_r")
		c := w.contractOf(t, str(b, "booking_id"))
		d, err := w.h.app.Booking.Reject(context.Background(), w.deskPrincipal(t), w.marina, c.ID, c.Version, "posto non adatto", true)
		if err != nil {
			t.Fatal(err)
		}
		if d.Status != "cancelled" || len(w.fake.Voids) != 1 {
			t.Fatalf("reject %s voids %+v", d.Status, w.fake.Voids)
		}
		w.h.drainMail()
		if len(w.h.mailTo("rej@example.test", "booking_rejected")) != 1 {
			t.Fatal("rejection email missing")
		}
		var outcome string
		if err := db.NewSelect().TableExpr("inquiries").Column("outcome").Where("contract_id = ?", c.ID).Scan(context.Background(), &outcome); err != nil || outcome != "declined_other" {
			t.Fatalf("inquiry %q %v", outcome, err)
		}
		w.invariants(t)
	})
}

// TestApproval_DeadlineAutoRejects: the bookings.approval_expire job rejects at the deadline.
func TestApproval_DeadlineAutoRejects(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, false)
		w.settings(t, func(s *store.OnlineBookingSettings) {
			s.AllowWithoutProvider = true
			s.Approval = store.JSON(`{"always":false,"hold_hours":2,"group_ids":[]}`)
		})
		b := w.mustCreate(t, "k-dl-0001")
		w.details(t, b, "dl@example.test")
		paid := w.pay(t, b)
		if str(paid, "status") != "awaiting_approval" || str(paid, "redirect_url") != "" {
			t.Fatalf("no provider pay %v", paid)
		}
		w.setClock(t, w.clock.Now().Add(time.Hour))
		w.h.runJobs("bookings.approval_expire", "contracts.expire")
		if c := w.contractOf(t, str(b, "booking_id")); c.Status != "awaiting_approval" {
			t.Fatalf("rejected before the deadline: %s", c.Status)
		}
		w.invariants(t)
		w.setClock(t, w.clock.Now().Add(61*time.Minute))
		w.h.runJobs("bookings.approval_expire")
		c := w.contractOf(t, str(b, "booking_id"))
		if c.Status != "cancelled" {
			t.Fatalf("not rejected at the deadline: %s", c.Status)
		}
		ob, _ := store.OnlineBookingByContract(context.Background(), db.DB, w.scope, c.ID)
		if ob.RejectReason != "approval_timeout" {
			t.Fatalf("reason %q", ob.RejectReason)
		}
		w.invariants(t)
	})
}

// TestApproval_NoProviderPaymentLink: approval without authorisation moves the booking to pending_payment with a
// 24 h payment link emailed to the customer; the provider webhook then confirms.
func TestApproval_NoProviderPaymentLink(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, false)
		w.settings(t, func(s *store.OnlineBookingSettings) {
			s.AllowWithoutProvider = true
			s.Approval = store.JSON(`{"always":true,"hold_hours":48,"group_ids":[]}`)
		})
		b := w.mustCreate(t, "k-link-0001")
		w.details(t, b, "link@example.test")
		if paid := w.pay(t, b); str(paid, "status") != "awaiting_approval" {
			t.Fatalf("pay %v", paid)
		}
		c := w.contractOf(t, str(b, "booking_id"))
		w.enableProvider(t) // the provider is turned on before the office approves
		d, err := w.h.app.Booking.Approve(context.Background(), w.deskPrincipal(t), w.marina, c.ID, c.Version)
		if err != nil {
			t.Fatal(err)
		}
		if d.Status != "pending_payment" {
			t.Fatalf("approve without authorisation %s", d.Status)
		}
		w.h.drainMail()
		if len(w.h.mailTo("link@example.test", "booking_approved_pay")) != 1 {
			t.Fatal("payment link email missing")
		}
		w.invariants(t)
	})
}
