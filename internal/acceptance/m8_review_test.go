//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// confirmed books, fills in and pays a booking through the public API and confirms it with the signed webhook.
func (w *m8World) confirmed(t *testing.T, key, email string) (map[string]any, *store.Contract) {
	t.Helper()
	b := w.mustCreate(t, key)
	w.details(t, b, email)
	w.pay(t, b)
	w.succeed(t, "evt_"+key, "pi_"+key)
	c := w.contractOf(t, str(b, "booking_id"))
	if c.Status != "confirmed" {
		t.Fatalf("not confirmed: %s", c.Status)
	}
	return b, c
}

func (w *m8World) upload(t *testing.T, b map[string]any, docType string) {
	t.Helper()
	pdf, err := os.ReadFile(filepath.Join("testdata", "registry", "sample.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if r := w.pub.raw(t, "POST", w.P+"/bookings/"+str(b, "booking_id")+"/documents?type="+docType, "application/pdf", pdf,
		"Porto-Booking-Token", str(b, "booking_token")); r.status != 201 {
		t.Fatalf("upload %d %s", r.status, r.body)
	}
}

// activeAllocations counts the active allocations of a transit (INV-08).
func (w *m8World) activeAllocations(t *testing.T, contractID string) []store.Allocation {
	t.Helper()
	rows, err := store.AllocationsOfContracts(context.Background(), w.h.app.DB.DB, w.scope, []string{contractID})
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Allocation
	for _, r := range rows {
		if r.Status == store.AllocActive {
			out = append(out, r.Allocation)
		}
	}
	return out
}

// TestBooking_TokenSurfaceHidesCustomerDocuments (spec 23.3, INV-88): a booking whose email matches an existing
// customer is linked to that customer, but its token shows and counts only the documents uploaded through it:
// never the identity or insurance documents of the customer, which must still be uploaded to pay.
func TestBooking_TokenSurfaceHidesCustomerDocuments(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.settings(t, func(s *store.OnlineBookingSettings) {
			s.RequiredFields = store.MustJSON(onlineconf.RequiredFields{Documents: []string{"insurance_policy"}})
		})
		victim := w.mustCreate(t, "k-docs-0001")
		w.details(t, victim, "victim@example.test")
		w.upload(t, victim, "insurance_policy")
		w.upload(t, victim, "identity_card")
		own := w.pub.expect(t, 200, "GET", w.P+"/bookings/"+str(victim, "booking_id"), nil, "Porto-Booking-Token", str(victim, "booking_token"))
		if len(jsonList(own, "documents")) != 2 || len(jsonList(own, "missing_documents")) != 0 {
			t.Fatalf("own documents %v", own)
		}

		other := w.create(t, "k-docs-0002", 1000, 350, "2027-06-20", "2027-06-22").json(t)
		got := w.details(t, other, "victim@example.test")
		if w.contractOf(t, str(other, "booking_id")).CustomerID != w.contractOf(t, str(victim, "booking_id")).CustomerID {
			t.Fatal("precondition: the second booking is linked to the same customer (D10)")
		}
		view := w.pub.expect(t, 200, "GET", w.P+"/bookings/"+str(other, "booking_id"), nil, "Porto-Booking-Token", str(other, "booking_token"))
		for _, v := range []map[string]any{got, view} {
			if docs := jsonList(v, "documents"); len(docs) != 0 {
				t.Fatalf("another customer's documents exposed: %v", docs)
			}
			if miss := jsonList(v, "missing_documents"); len(miss) != 1 || miss[0] != "insurance_policy" {
				t.Fatalf("missing documents %v", miss)
			}
		}
		r := w.pub.do(t, "POST", w.P+"/bookings/"+str(other, "booking_id")+"/pay", map[string]any{}, "Porto-Booking-Token",
			str(other, "booking_token"), "Idempotency-Key", "pay-docs-0002")
		if r.status != 422 || r.problemType() != "booking_rules" {
			t.Fatalf("pay with the victim's insurance: %d %s", r.status, r.body)
		}
		w.upload(t, other, "insurance_policy")
		if paid := w.pay(t, other); str(paid, "status") != "pending_payment" {
			t.Fatalf("pay after own upload %v", paid)
		}
	})
}

// TestBooking_ConsentTextsUnpublished (spec 30, m8-plan §5.1): without published terms and privacy the booking page
// is not payable and the details are refused (no booking without its GDPR consents).
func TestBooking_ConsentTextsUnpublished(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		if _, err := db.NewDelete().TableExpr("booking_texts").Where("marina_id = ? AND kind = 'privacy'", w.marina).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if cfg := w.pub.expect(t, 200, "GET", w.P+"/booking/config", nil); cfg["payable"] != false {
			t.Fatalf("payable without privacy text: %v", cfg)
		}
		b := w.mustCreate(t, "k-cons-0001")
		r := w.pub.do(t, "PUT", w.P+"/bookings/"+str(b, "booking_id")+"/details", map[string]any{
			"customer": map[string]any{"first_name": "Anna", "last_name": "Bianchi", "email": "c@example.test", "country": "IT", "locale": "it"},
			"vessel":   map[string]any{"name": "Maestrale", "hull_type": "monohull_sail"},
			"consents": map[string]any{"terms_version": 1, "privacy_version": 0, "marketing": false},
		}, "Porto-Booking-Token", str(b, "booking_token"))
		if r.status != 422 || r.problemType() != "booking_rules" {
			t.Fatalf("details without privacy text: %d %s", r.status, r.body)
		}
		if c := w.contractOf(t, str(b, "booking_id")); c.CustomerID != "" {
			t.Fatal("details stored without consents")
		}
		n, _ := db.NewSelect().TableExpr("consents").Where("marina_id = ?", w.marina).Count(context.Background())
		if n != 0 {
			t.Fatalf("consents %d", n)
		}
	})
}

// TestPortal_CancelRefundAtomic: the customer's cancellation and its refund commit together. When the refund
// cannot be written the cancellation is refused and nothing changes; the retry cancels and refunds once, and a
// replay neither cancels nor refunds again.
func TestPortal_CancelRefundAtomic(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		_, c := w.confirmed(t, "k-cref-0001", "cref@example.test")
		ctx := context.Background()
		prev, err := w.h.app.Booking.PreviewCancellation(ctx, w.scope, c.CustomerID, c.ID)
		if err != nil || prev.Refund <= 0 {
			t.Fatalf("preview %+v %v", prev, err)
		}
		w.h.app.Booking.SetPayments(nil) // the refund cannot be written
		if _, err := w.h.app.Booking.CancelByCustomer(ctx, w.scope, c.CustomerID, c.ID, int64(prev.Refund)); err == nil {
			t.Fatal("cancelled without its refund")
		}
		if got := w.contractOf(t, w.bookingOf(t, c.ID)); got.Status != "confirmed" {
			t.Fatalf("cancellation committed without refund: %s", got.Status)
		}
		if len(w.fake.Refunds) != 0 {
			t.Fatalf("refunds %+v", w.fake.Refunds)
		}
		w.h.app.Booking.SetPayments(w.h.app.Payments)
		for range 2 {
			res, err := w.h.app.Booking.CancelByCustomer(ctx, w.scope, c.CustomerID, c.ID, int64(prev.Refund))
			if err != nil || res.Status != "cancelled" {
				t.Fatalf("cancel %+v %v", res, err)
			}
		}
		if len(w.fake.Refunds) != 1 || w.fake.Refunds[0].AmountCents != int64(prev.Refund) {
			t.Fatalf("provider refunds %+v", w.fake.Refunds)
		}
		w.invariants(t)
	})
}

// bookingOf returns the online booking id of a transit.
func (w *m8World) bookingOf(t *testing.T, contractID string) string {
	t.Helper()
	ob, err := store.OnlineBookingByContract(context.Background(), w.h.app.DB.DB, w.scope, contractID)
	if err != nil {
		t.Fatal(err)
	}
	return ob.ID
}

// TestPortal_ChangeDatesRefundsShorterStay: a shorter stay is refunded in the same transaction as the change; when
// the refund cannot be written the dates do not change.
func TestPortal_ChangeDatesRefundsShorterStay(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		_, c := w.confirmed(t, "k-dref-0001", "dref@example.test")
		ctx := context.Background()
		in := booking.DateChange{Arrival: calendar.MustParse(m8Arrival), Departure: calendar.MustParse("2027-06-11")}
		prev, err := w.h.app.Booking.PreviewDateChange(ctx, w.scope, c.CustomerID, c.ID, in)
		if err != nil || !prev.Available || prev.Refund <= 0 || prev.Delta >= 0 {
			t.Fatalf("preview %+v %v", prev, err)
		}
		total := prev.NewTotal
		in.ExpectedTotalCents = &total
		w.h.app.Booking.SetPayments(nil)
		if _, err := w.h.app.Booking.ChangeDates(ctx, w.scope, c.CustomerID, c.ID, in); err == nil {
			t.Fatal("dates changed without the refund")
		}
		if got, _ := store.GetContract(ctx, db.DB, w.scope, c.ID); !got.EndsOn.Equal(c.EndsOn) || got.TotalCents != c.TotalCents {
			t.Fatalf("change committed without refund: %s %d", got.EndsOn, got.TotalCents)
		}
		w.h.app.Booking.SetPayments(w.h.app.Payments)
		res, err := w.h.app.Booking.ChangeDates(ctx, w.scope, c.CustomerID, c.ID, in)
		if err != nil || res.Refund != prev.Refund {
			t.Fatalf("change %+v %v", res, err)
		}
		if len(w.fake.Refunds) != 1 || w.fake.Refunds[0].AmountCents != int64(prev.Refund) {
			t.Fatalf("provider refunds %+v", w.fake.Refunds)
		}
		if got, _ := store.GetContract(ctx, db.DB, w.scope, c.ID); !got.EndsOn.Equal(in.Departure) || money.Cents(got.TotalCents) != total {
			t.Fatalf("after change %s %d", got.EndsOn, got.TotalCents)
		}
		w.invariants(t)
	})
}

// TestCheckout_BrowserReturnDoesNotConfirm (INV-09): the browser return page reads the status with the status
// token alone; however often it is called, the transit stays pending_payment and no payment is recorded.
func TestCheckout_BrowserReturnDoesNotConfirm(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		b := w.mustCreate(t, "k-ret-0001")
		w.details(t, b, "ret@example.test")
		paid := w.pay(t, b)
		for range 3 {
			st := w.pub.expect(t, 200, "GET", w.P+"/bookings/"+str(b, "booking_id")+"/status", nil, "Porto-Status-Token", str(paid, "status_token"))
			if str(st, "status") != "pending_payment" {
				t.Fatalf("status %v", st)
			}
		}
		c := w.contractOf(t, str(b, "booking_id"))
		if c.Status != "pending_payment" || c.ConfirmationSource != "" {
			t.Fatalf("confirmed by the browser return: %+v", c)
		}
		n, err := db.NewSelect().TableExpr("payments").Where("marina_id = ?", w.marina).Count(context.Background())
		if err != nil || n != 0 {
			t.Fatalf("payments %d %v", n, err)
		}
		if len(w.activeAllocations(t, c.ID)) == 0 {
			t.Fatal("pending online transit without allocation")
		}
		w.invariants(t)
	})
}

// TestCheckout_HoldHasAllocation (INV-08, online): a new online booking holds a concrete resource for every night
// with an expiring hold row, like a desk allocation.
func TestCheckout_HoldHasAllocation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		b := w.mustCreate(t, "k-hold-0001")
		c := w.contractOf(t, str(b, "booking_id"))
		al := w.activeAllocations(t, c.ID)
		if len(al) != 1 || al[0].Kind != store.AllocKindHold || al[0].ExpiresAt.IsZero() || al[0].ResourceID == "" {
			t.Fatalf("hold allocations %+v", al)
		}
		n, err := db.NewSelect().TableExpr("berth_occupancies").Where("allocation_id = ?", al[0].ID).Count(context.Background())
		if err != nil || n != 2 {
			t.Fatalf("occupancy rows %d %v", n, err)
		}
		w.invariants(t)
	})
}

// TestApproval_ApprovedNotTimedOut (INV-08/INV-09): a booking approved by the desk without a provider stays
// awaiting_approval with an active hold past the approval deadline; the deadline job never rejects it.
func TestApproval_ApprovedNotTimedOut(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, false)
		w.settings(t, func(s *store.OnlineBookingSettings) {
			s.AllowWithoutProvider = true
			s.Approval = store.JSON(`{"always":false,"hold_hours":2,"group_ids":[]}`)
		})
		// allow_without_provider takes bookings without a checkout provider: the page is payable (D7).
		if cfg := w.pub.expect(t, 200, "GET", w.P+"/booking/config", nil); cfg["payable"] != true {
			t.Fatalf("not payable with allow_without_provider: %v", cfg["payable"])
		}
		b := w.mustCreate(t, "k-apdl-0001")
		w.details(t, b, "apdl@example.test")
		if paid := w.pay(t, b); str(paid, "status") != "awaiting_approval" {
			t.Fatalf("pay %v", paid)
		}
		c := w.contractOf(t, str(b, "booking_id"))
		if _, err := w.h.app.Booking.Approve(context.Background(), w.deskPrincipal(t), w.marina, c.ID, c.Version); err != nil {
			t.Fatal(err)
		}
		w.setClock(t, w.clock.Now().Add(2*time.Hour+20*time.Minute)) // past the deadline and its 15 min hold grace
		w.h.runJobs("bookings.approval_expire", "contracts.expire")
		c = w.contractOf(t, str(b, "booking_id"))
		if c.Status != "awaiting_approval" || len(w.activeAllocations(t, c.ID)) == 0 {
			t.Fatalf("approved booking timed out: %s", c.Status)
		}
		w.invariants(t)
	})
}

// TestConcurrency_ApproveVsDeadline (INV-08): 100 goroutines race desk approvals against the approval deadline job
// on 10 bookings just past their deadline. Every booking ends either approved (awaiting_approval with an active
// hold) or rejected with approval_timeout, never approved and cancelled.
func TestConcurrency_ApproveVsDeadline(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, false)
		w.settings(t, func(s *store.OnlineBookingSettings) {
			s.AllowWithoutProvider = true
			s.Approval = store.JSON(`{"always":false,"hold_hours":2,"group_ids":[]}`)
		})
		const n = 10
		ids := make([]string, n)
		start := calendar.MustParse("2027-06-13")
		for i := range n {
			arr := start.AddDays(i)
			b := w.create(t, "k-avd-"+arr.String(), 1000, 350, arr.String(), arr.AddDays(1).String()).json(t)
			w.details(t, b, "avd@example.test")
			if paid := w.pay(t, b); str(paid, "status") != "awaiting_approval" {
				t.Fatalf("pay %v", paid)
			}
			ids[i] = w.contractOf(t, str(b, "booking_id")).ID
		}
		w.setClock(t, w.clock.Now().Add(2*time.Hour+time.Minute))
		p := w.deskPrincipal(t)
		ctx := context.Background()
		var wg sync.WaitGroup
		var unexpected atomic.Value
		for g := range 100 {
			wg.Go(func() {
				var err error
				if g%2 == 0 {
					_, err = w.h.app.Booking.Approve(ctx, p, w.marina, ids[g%n], 0)
					if errors.Is(err, contract.ErrInvalidTransition) {
						err = nil // rejected or approved meanwhile
					}
				} else {
					err = w.h.app.Booking.RunApprovalExpiry(ctx)
				}
				if err != nil {
					unexpected.Store(err)
				}
			})
		}
		wg.Wait()
		if err, _ := unexpected.Load().(error); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		w.h.runJobs("bookings.approval_expire")
		for _, id := range ids {
			ob, err := store.OnlineBookingByContract(ctx, db.DB, w.scope, id)
			if err != nil {
				t.Fatal(err)
			}
			c, _ := store.GetContract(ctx, db.DB, w.scope, id)
			switch {
			case !ob.ApprovedAt.IsZero():
				if c.Status != "awaiting_approval" || !ob.RejectedAt.IsZero() || len(w.activeAllocations(t, id)) == 0 {
					t.Fatalf("approved booking %s: status %s rejected %v", id, c.Status, ob.RejectedAt)
				}
			case c.Status != "cancelled" || ob.RejectReason != "approval_timeout":
				t.Fatalf("unapproved booking %s: status %s reason %q", id, c.Status, ob.RejectReason)
			}
		}
		w.invariants(t)
	})
}

// rawWebhook posts a signed provider event without failing the test (for goroutines).
func (w *m8World) rawWebhook(ev ext.PaymentEvent) int {
	body, sig := fixture.SignedBody(ev, m5WebhookSecret, w.clock.Now())
	req, _ := http.NewRequest(http.MethodPost, w.h.srv.URL+"/webhooks/payments/"+fixture.FakeProviderCode+"/"+w.marina, bytes.NewReader(body))
	req.Header.Set(fixture.FakeSignatureHeader, sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

// rawDeskTransit creates a desk transit on a berth without failing the test (for goroutines).
func (w *m8World) rawDeskTransit(vessel, customer, berth string) int {
	body, _ := json.Marshal(map[string]any{
		"vessel_id": vessel, "customer_id": customer, "arrival": m8Arrival, "departure": m8Departure,
		"placements": []any{berthPlacement(berth, m8Arrival, "2027-06-11")},
	})
	req, _ := http.NewRequest(http.MethodPost, w.h.srv.URL+w.M+"/transits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", w.owner.csrf())
	resp, err := w.owner.http.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

// TestConcurrency_ConfirmVsExpireOnline (INV-05, INV-08, INV-09): after the hold of a pending online payment has
// lapsed, 100 goroutines race the late provider webhook (late payment recovery on), the contracts.expire job and
// desk transits on the same berth and nights. At most one side wins the berth: either the online transit is
// confirmed and no desk transit was created, or the desk won once and the late payment is refunded in full.
func TestConcurrency_ConfirmVsExpireOnline(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		b := w.mustCreate(t, "k-cve-0001")
		w.details(t, b, "cve@example.test")
		w.pay(t, b)
		c := w.contractOf(t, str(b, "booking_id"))
		berth := w.activeAllocations(t, c.ID)[0].ResourceID
		vessel := w.vessel(t, "Libeccio", 1000, 350)
		cust := w.customer(t, "Neri")
		w.setClock(t, w.clock.Now().Add(31*time.Minute))
		req := w.fake.LastCheckout()
		ev := ext.PaymentEvent{
			EventID: "evt_cve", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_cve", AmountCents: req.AmountCents, Method: "card_online",
			Metadata: map[string]string{"marina_id": w.marina, "intent_id": req.IntentID},
		}
		ctx := context.Background()
		var wg sync.WaitGroup
		var desk atomic.Int32
		for g := range 100 {
			wg.Go(func() {
				switch g % 3 {
				case 0:
					w.rawWebhook(ev)
				case 1:
					_ = w.h.app.Jobs.RunNow(ctx, "contracts.expire")
				default:
					if w.rawDeskTransit(vessel, cust, berth) == 201 {
						desk.Add(1)
					}
				}
			})
		}
		wg.Wait()
		if code := w.rawWebhook(ev); code != 204 { // the provider retries until it gets an answer
			t.Fatalf("webhook retry %d", code)
		}
		c = w.contractOf(t, str(b, "booking_id"))
		switch c.Status {
		case "confirmed":
			if desk.Load() != 0 || len(w.fake.Refunds) != 0 {
				t.Fatalf("online confirmed and desk transits %d refunds %d", desk.Load(), len(w.fake.Refunds))
			}
		case "expired":
			if desk.Load() != 1 || len(w.fake.Refunds) != 1 || w.fake.Refunds[0].AmountCents != req.AmountCents {
				t.Fatalf("desk transits %d refunds %+v", desk.Load(), w.fake.Refunds)
			}
		default:
			t.Fatalf("online transit %s", c.Status)
		}
		w.invariants(t)
	})
}

// auditProbe asserts the audit rows and outbox events written for one transit, step by step.
type auditProbe struct {
	w        *m8World
	id       string
	audits   int
	outboxes int
}

func (w *m8World) probe(id string) *auditProbe { return &auditProbe{w: w, id: id} }

type auditRow struct {
	Action  string `bun:"action"`
	ActorID string `bun:"actor_id"`
}

func (p *auditProbe) rows(t *testing.T) ([]auditRow, int) {
	t.Helper()
	ctx := context.Background()
	var rows []auditRow
	if err := p.w.h.app.DB.NewSelect().TableExpr("audit_log").Column("action", "actor_id").
		Where("entity_type = 'transit' AND entity_id = ?", p.id).OrderExpr("created_at, id").Scan(ctx, &rows); err != nil {
		t.Fatal(err)
	}
	n, err := p.w.h.app.DB.NewSelect().TableExpr("outbox_events").Where("CAST(payload AS TEXT) LIKE ?", "%"+p.id+"%").Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return rows, int(n)
}

// expect checks that the last step wrote action (by actorID, "" = any) once, and at least one event of the transit.
func (p *auditProbe) expect(t *testing.T, action, actorID string) {
	t.Helper()
	rows, n := p.rows(t)
	added := rows[p.audits:]
	hits := 0
	for _, r := range added {
		if r.Action == action && (actorID == "" || r.ActorID == actorID) {
			hits++
		}
	}
	if hits != 1 || n <= p.outboxes {
		t.Fatalf("%s by %q: new audit rows %+v, outbox %d -> %d", action, actorID, added, p.outboxes, n)
	}
	p.audits, p.outboxes = len(rows), n
}

// TestContract_M8StateChangesWriteAuditAndOutbox (INV-30): every M8 transition of an online or channel transit
// writes its audit row (with the actor) and its outbox event, and both roll back with the transaction.
func TestContract_M8StateChangesWriteAuditAndOutbox(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		ctx := context.Background()

		// Public create, details, provider confirmation, customer date change and cancellation.
		a := w.mustCreate(t, "k-aud-0001")
		ca := w.contractOf(t, str(a, "booking_id"))
		pa := w.probe(ca.ID)
		pa.expect(t, "transit.create", "public")
		w.details(t, a, "aud@example.test")
		pa.expect(t, "transit.update", "public")
		w.pay(t, a)
		pa.audits, pa.outboxes = func() (int, int) { r, n := pa.rows(t); return len(r), n }()
		w.succeed(t, "evt_aud_a", "pi_aud_a")
		pa.expect(t, "transit.confirm", "")
		ca = w.contractOf(t, str(a, "booking_id"))
		cust := "customer:" + ca.CustomerID
		in := booking.DateChange{Arrival: calendar.MustParse(m8Arrival), Departure: calendar.MustParse("2027-06-11")}
		prev, err := w.h.app.Booking.PreviewDateChange(ctx, w.scope, ca.CustomerID, ca.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		in.ExpectedTotalCents = &prev.NewTotal
		if _, err := w.h.app.Booking.ChangeDates(ctx, w.scope, ca.CustomerID, ca.ID, in); err != nil {
			t.Fatal(err)
		}
		pa.expect(t, "transit.stay", cust)
		// Cancellation of the same transit: its match was partly reversed by the date change, the rest is refunded.
		cp, err := w.h.app.Booking.PreviewCancellation(ctx, w.scope, ca.CustomerID, ca.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.h.app.Booking.CancelByCustomer(ctx, w.scope, ca.CustomerID, ca.ID, int64(cp.Refund)); err != nil {
			t.Fatal(err)
		}
		pa.expect(t, "transit.cancel", cust)

		// Approval with an authorised card: payment_authorized, approve (status unchanged), reject.
		w.approvalAlways(t)
		b := w.create(t, "k-aud-0002", 1000, 350, "2027-06-14", "2027-06-16").json(t)
		w.details(t, b, "aud2@example.test")
		w.pay(t, b)
		cb := w.contractOf(t, str(b, "booking_id"))
		pb := w.probe(cb.ID)
		pb.audits, pb.outboxes = func() (int, int) { r, n := pb.rows(t); return len(r), n }()
		w.authorize(t, "evt_aud_b", "pi_aud_b")
		pb.expect(t, "transit.payment_authorized", "")
		desk := w.deskPrincipal(t)
		if _, err := w.h.app.Booking.Approve(ctx, desk, w.marina, cb.ID, 0); err != nil {
			t.Fatal(err)
		}
		rows, _ := pb.rows(t)
		if last := rows[len(rows)-1]; last.Action != "transit.approve" || last.ActorID != desk.UserID {
			t.Fatalf("approve audit %+v", last)
		}
		pb.audits = len(rows)
		c3 := w.create(t, "k-aud-0003", 1000, 350, "2027-06-17", "2027-06-18").json(t)
		w.details(t, c3, "aud3@example.test")
		w.pay(t, c3)
		w.authorize(t, "evt_aud_c", "pi_aud_c")
		cc := w.contractOf(t, str(c3, "booking_id"))
		pc := w.probe(cc.ID)
		pc.audits, pc.outboxes = func() (int, int) { r, n := pc.rows(t); return len(r), n }()
		if _, err := w.h.app.Booking.Reject(ctx, desk, w.marina, cc.ID, 0, "no", false); err != nil {
			t.Fatal(err)
		}
		pc.expect(t, "transit.reject", desk.UserID)

		// Approval deadline job.
		d := w.create(t, "k-aud-0004", 1000, 350, "2027-06-19", "2027-06-20").json(t)
		w.details(t, d, "aud4@example.test")
		w.pay(t, d)
		w.authorize(t, "evt_aud_d", "pi_aud_d")
		cd := w.contractOf(t, str(d, "booking_id"))
		pd := w.probe(cd.ID)
		pd.audits, pd.outboxes = func() (int, int) { r, n := pd.rows(t); return len(r), n }()
		w.setClock(t, w.clock.Now().Add(49*time.Hour))
		w.h.runJobs("bookings.approval_expire")
		pd.expect(t, "transit.expire", "system:"+booking.JobApprovalExpire)

		// Channel confirmation.
		w.connectChannel(t)
		if code := w.postChannel(t, m8Reservation("EXT-AUD", "ev-aud", ext.ChannelReservationNew, true)); code != 200 {
			t.Fatalf("channel webhook %d", code)
		}
		r := w.reservation(t, "EXT-AUD")
		pr := w.probe(r.ContractID)
		rows, _ = pr.rows(t)
		var confirmBy string
		for _, x := range rows {
			if x.Action == "transit.confirm" {
				confirmBy = x.ActorID
			}
		}
		if confirmBy != "channel:fake" {
			t.Fatalf("channel confirm audit %+v", rows)
		}

		// Rollback: a failing transaction leaves neither the audit row nor the event.
		e := w.create(t, "k-aud-0005", 1000, 350, "2027-06-22", "2027-06-23").json(t)
		ce := w.contractOf(t, str(e, "booking_id"))
		pe := w.probe(ce.ID)
		beforeRows, beforeEvents := pe.rows(t)
		boom := errors.New("boom")
		err = w.h.app.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			if _, err := w.h.app.Contracts.CancelOnlineTransitTx(ctx, tx, w.scope, contract.PublicActor(), ce.ID, 0, "abandoned"); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("tx %v", err)
		}
		afterRows, afterEvents := pe.rows(t)
		if len(afterRows) != len(beforeRows) || afterEvents != beforeEvents {
			t.Fatalf("rolled back cancel left audit %d->%d events %d->%d", len(beforeRows), len(afterRows), beforeEvents, afterEvents)
		}
		if got := w.contractOf(t, str(e, "booking_id")); got.Status != "held" {
			t.Fatalf("status %s", got.Status)
		}
		w.invariants(t)
	})
}

// TestInvariants_OnlineQuotaLabelQ19: a broken online quota counter is reported by the quota check as Q19, never
// as Q17 (rights coverage, INV-43).
func TestInvariants_OnlineQuotaLabelQ19(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.quotaGroup(t, 5, w.harbour.Berths)
		w.mustCreate(t, "k-q19-0001")
		ctx := context.Background()
		if _, err := db.NewUpdate().Table("online_quota_usage").Set("used = used + 1").Where("marina_id = ?", w.marina).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		vs, err := store.CheckOccupancyInvariants(ctx, db.DB, w.marina, calendar.In(w.clock.Now(), time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		if len(vs) == 0 || !slices.ContainsFunc(vs, func(v store.Violation) bool { return v.Query == "Q19" }) ||
			slices.ContainsFunc(vs, func(v store.Violation) bool { return v.Query == "Q17" }) {
			t.Fatalf("violations %+v", vs)
		}
	})
}
