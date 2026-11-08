//go:build integration

package acceptance

import (
	"context"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// Customer-area tests over the public account API (m8-plan §7.2): a session opened from a booking link, then the
// operations with Porto-Customer-Session.

// session opens a customer-area session for the customer of a transit (booking link → session token).
func (w *m8World) session(t *testing.T, c *store.Contract) string {
	t.Helper()
	var link string
	if err := w.h.app.DB.RunInTx(context.Background(), func(ctx context.Context, tx bun.Tx) error {
		var err error
		link, err = w.h.app.Portal.IssueBookingLinkTx(ctx, tx, w.scope, c.CustomerID, c.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s := w.pub.expect(t, 201, "POST", w.P+"/account/session", map[string]any{"token": link})
	if str(s, "session_token") == "" {
		t.Fatalf("session %v", s)
	}
	return str(s, "session_token")
}

// account calls an account operation of a transit with the session.
func (w *m8World) account(t *testing.T, sess, method, path string, body any, headers ...string) response {
	t.Helper()
	return w.pub.do(t, method, w.P+"/account/bookings"+path, body, append([]string{"Porto-Customer-Session", sess}, headers...)...)
}

func cents(t *testing.T, m map[string]any, k string) int64 {
	t.Helper()
	v, ok := m[k].(float64)
	if !ok {
		t.Fatalf("%s missing in %v", k, m)
	}
	return int64(v)
}

// TestPortal_CancelUsesPolicySnapshot (m8-plan §7.2, INV-20): the cancellation preview and the refund come from the
// tiers and refund mode frozen on the contract at booking time; changing the marina settings afterwards changes
// nothing for that booking (while a new booking gets the new tiers).
func TestPortal_CancelUsesPolicySnapshot(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		_, c := w.confirmed(t, "k-snap-0001", "snap@example.test") // 9 days before arrival: 100% by the default tiers
		w.settings(t, func(s *store.OnlineBookingSettings) {
			s.CancellationTiers = store.JSON(`[{"min_days_before":30,"refund_bp":10000},{"min_days_before":0,"refund_bp":0}]`)
			s.RefundAs = "credit"
		})
		sess := w.session(t, c)
		r := w.account(t, sess, "GET", "/"+c.ID+"/cancellation", nil)
		if r.status != 200 {
			t.Fatalf("preview %d %s", r.status, r.body)
		}
		prev := r.json(t)
		paid := cents(t, prev, "paid_cents")
		if prev["allowed"] != true || paid <= 0 || cents(t, prev, "refund_cents") != paid || str(prev, "refund_as") != "refund" {
			t.Fatalf("preview from the snapshot %v", prev)
		}
		// A booking made after the change carries the new tiers: nothing back at 9 days.
		_, c2 := w.confirmed(t, "k-snap-0002", "snap2@example.test")
		p2, err := w.h.app.Booking.PreviewCancellation(context.Background(), w.scope, c2.CustomerID, c2.ID)
		if err != nil || p2.Refund != 0 || p2.RefundAs != "credit" {
			t.Fatalf("new booking preview %+v %v", p2, err)
		}
		res := w.account(t, sess, "POST", "/"+c.ID+"/cancel", map[string]any{"expected_refund_cents": paid}, "Idempotency-Key", "cancel-snap-0001")
		if res.status != 200 {
			t.Fatalf("cancel %d %s", res.status, res.body)
		}
		if out := res.json(t); str(out, "status") != "cancelled" || cents(t, out, "refund_cents") != paid || str(out, "refund_as") != "refund" {
			t.Fatalf("cancel %v", out)
		}
		if len(w.fake.Refunds) != 1 || w.fake.Refunds[0].AmountCents != paid {
			t.Fatalf("provider refunds %+v", w.fake.Refunds)
		}
		w.invariants(t)
	})
}

// TestPortal_CancelRefundChanged409 (m8-plan §7.2): when the refund moved to a lower tier after the preview, the
// cancellation with the old expected refund is 409 refund_changed and changes nothing; with the new amount it
// cancels and refunds exactly that amount.
func TestPortal_CancelRefundChanged409(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		_, c := w.confirmed(t, "k-r409-0001", "r409@example.test")
		sess := w.session(t, c)
		prev := w.account(t, sess, "GET", "/"+c.ID+"/cancellation", nil).json(t)
		paid, old := cents(t, prev, "paid_cents"), cents(t, prev, "refund_cents")
		if old != paid {
			t.Fatalf("100%% tier expected: %v", prev)
		}
		// Five days before arrival (2027-06-05): the 2-day tier, 50%.
		w.setClock(t, time.Date(2027, 6, 5, 9, 0, 0, 0, time.UTC))
		sess = w.session(t, c)
		r := w.account(t, sess, "POST", "/"+c.ID+"/cancel", map[string]any{"expected_refund_cents": old}, "Idempotency-Key", "cancel-r409-1")
		if r.status != 409 || r.problemType() != "refund_changed" {
			t.Fatalf("stale refund %d %s", r.status, r.body)
		}
		if got := w.contractOf(t, w.bookingOf(t, c.ID)); got.Status != "confirmed" || len(w.fake.Refunds) != 0 {
			t.Fatalf("refused cancellation changed something: %s %+v", got.Status, w.fake.Refunds)
		}
		prev = w.account(t, sess, "GET", "/"+c.ID+"/cancellation", nil).json(t)
		half := cents(t, prev, "refund_cents")
		if half <= 0 || half >= paid || half != (paid+1)/2 && half != paid/2 {
			t.Fatalf("50%% tier %v (paid %d)", prev, paid)
		}
		r = w.account(t, sess, "POST", "/"+c.ID+"/cancel", map[string]any{"expected_refund_cents": half}, "Idempotency-Key", "cancel-r409-2")
		if r.status != 200 {
			t.Fatalf("cancel %d %s", r.status, r.body)
		}
		if len(w.fake.Refunds) != 1 || w.fake.Refunds[0].AmountCents != half {
			t.Fatalf("provider refunds %+v", w.fake.Refunds)
		}
		w.invariants(t)
	})
}

// TestPortal_ChangeDatesQuotaAndPrice (m8-plan §7.2, INV-12): a date change moves the quota claims with the stay
// (the old nights are released, the new ones claimed, never above the cap), is refused when the quota of a new
// night is used up, asks for the price the customer saw (409 price_changed) and turns a higher price into a
// supplement to pay with pay-balance.
func TestPortal_ChangeDatesQuotaAndPrice(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		g := w.quotaGroup(t, 1, w.harbour.Berths)
		_, c := w.confirmed(t, "k-chq-0001", "chq@example.test")
		if r := w.create(t, "k-chq-0002", 1000, 350, "2027-06-13", "2027-06-14"); r.status != 201 {
			t.Fatalf("other booking %d %s", r.status, r.body)
		}
		want := func(nights ...string) {
			t.Helper()
			got := w.claimsPerNight(t, g)
			if len(got) != len(nights) {
				t.Fatalf("claims %v, want nights %v", got, nights)
			}
			for _, n := range nights {
				if got[n] != 1 {
					t.Fatalf("claims %v, want nights %v", got, nights)
				}
			}
		}
		want("2027-06-10", "2027-06-11", "2027-06-13")
		sess := w.session(t, c)
		change := func(arrival, departure string, expected int64, key string) response {
			return w.account(t, sess, "POST", "/"+c.ID+"/change-dates", map[string]any{
				"arrival": arrival, "departure": departure, "expected_total_cents": expected,
			}, "Idempotency-Key", key)
		}
		preview := func(arrival, departure string) map[string]any {
			r := w.account(t, sess, "POST", "/"+c.ID+"/change-dates/preview", map[string]any{"arrival": arrival, "departure": departure})
			if r.status != 200 {
				t.Fatalf("preview %d %s", r.status, r.body)
			}
			return r.json(t)
		}
		// Into a night whose quota is used up by the other booking: refused, claims unchanged.
		p := preview("2027-06-11", "2027-06-14")
		if r := change("2027-06-11", "2027-06-14", cents(t, p, "new_total_cents"), "idem-key-chg-q-1"); r.status != 409 || r.problemType() != "booking_unavailable" {
			t.Fatalf("over quota %d %s", r.status, r.body)
		}
		want("2027-06-10", "2027-06-11", "2027-06-13")
		// One night longer: a higher price, asked exactly.
		p = preview("2027-06-10", "2027-06-13")
		total, sup := cents(t, p, "new_total_cents"), cents(t, p, "supplement_cents")
		if p["available"] != true || sup <= 0 || total != c.TotalCents+sup || cents(t, p, "refund_cents") != 0 {
			t.Fatalf("longer stay preview %v (old total %d)", p, c.TotalCents)
		}
		if r := change("2027-06-10", "2027-06-13", total-1, "idem-key-chg-q-2"); r.status != 409 || r.problemType() != "price_changed" {
			t.Fatalf("stale price %d %s", r.status, r.body)
		}
		want("2027-06-10", "2027-06-11", "2027-06-13")
		if r := change("2027-06-10", "2027-06-13", total, "idem-key-chg-q-3"); r.status != 200 {
			t.Fatalf("change %d %s", r.status, r.body)
		}
		want("2027-06-10", "2027-06-11", "2027-06-12", "2027-06-13")
		got, err := store.GetContract(context.Background(), db.DB, w.scope, c.ID)
		if err != nil || got.TotalCents != total || got.EndsOn.String() != "2027-06-13" {
			t.Fatalf("after change %+v %v", got, err)
		}
		bk := w.account(t, sess, "GET", "/"+c.ID, nil).json(t)
		if cents(t, bk, "residual_cents") != sup || bk["can_pay_balance"] != true {
			t.Fatalf("supplement to pay %v", bk)
		}
		// Shorter again: the released nights go back to the quota.
		p = preview("2027-06-11", "2027-06-12")
		if r := change("2027-06-11", "2027-06-12", cents(t, p, "new_total_cents"), "idem-key-chg-q-4"); r.status != 200 {
			t.Fatalf("shorter %d %s", r.status, r.body)
		}
		want("2027-06-11", "2027-06-13")
		w.invariants(t) // Q19: used = claims ≤ cap
	})
}

// TestPortal_PayBalanceReusesRequest (m8-plan §7.2): pay-balance creates one payment request for the open residual
// and reuses it on later calls (same link, amount refreshed after a partial payment); nothing due is 422.
func TestPortal_PayBalanceReusesRequest(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.settings(t, func(s *store.OnlineBookingSettings) { s.PaymentMode, s.DepositBP = "deposit", 3000 })
		_, c := w.confirmed(t, "k-bal-0001", "bal@example.test")
		sess := w.session(t, c)
		bk := w.account(t, sess, "GET", "/"+c.ID, nil).json(t)
		residual := cents(t, bk, "residual_cents")
		if residual <= 0 || residual >= c.TotalCents {
			t.Fatalf("deposit residual %v", bk)
		}
		pay := func(key string) map[string]any {
			t.Helper()
			r := w.account(t, sess, "POST", "/"+c.ID+"/pay-balance", nil, "Idempotency-Key", key)
			if r.status != 200 {
				t.Fatalf("pay balance %d %s", r.status, r.body)
			}
			return r.json(t)
		}
		openRequests := func() int {
			n, err := db.NewSelect().TableExpr("payment_requests").Where("marina_id = ? AND customer_id = ? AND status = 'open'", w.marina, c.CustomerID).Count(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		a := pay("idem-key-bal-1")
		b := pay("idem-key-bal-2")
		if str(a, "checkout_url") == "" || str(a, "checkout_url") != str(b, "checkout_url") || cents(t, a, "amount_cents") != residual || openRequests() != 1 {
			t.Fatalf("not reused: %v %v open %d", a, b, openRequests())
		}
		// The office records part of the balance: the same request, the new residual.
		w.owner.expect(t, 201, "POST", w.M+"/payments", map[string]any{
			"customer_id": c.CustomerID, "kind": "payment", "method": "bank_transfer", "amount_cents": 10_00, "auto_match": "oldest_due_first",
		})
		d := pay("idem-key-bal-3")
		if str(d, "checkout_url") != str(a, "checkout_url") || cents(t, d, "amount_cents") != residual-10_00 || openRequests() != 1 {
			t.Fatalf("after a partial payment %v open %d", d, openRequests())
		}
		w.owner.expect(t, 201, "POST", w.M+"/payments", map[string]any{
			"customer_id": c.CustomerID, "kind": "payment", "method": "bank_transfer", "amount_cents": residual - 10_00, "auto_match": "oldest_due_first",
		})
		if r := w.account(t, sess, "POST", "/"+c.ID+"/pay-balance", nil, "Idempotency-Key", "idem-key-bal-4"); r.status != 422 || r.problemType() != "nothing_due" {
			t.Fatalf("nothing due %d %s", r.status, r.body)
		}
	})
}

// TestPortal_ChangeDatesThenCancelRefundsTheRest (INV-52, 00017): a shorter stay refunds part of the payment by a
// partial reversal of its match; cancelling the same booking afterwards reverses the same match again and refunds
// exactly the rest: the refunds add up to the paid amount, never more, and the ledger stays consistent.
func TestPortal_ChangeDatesThenCancelRefundsTheRest(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		_, c := w.confirmed(t, "k-rest-0001", "rest@example.test")
		ctx := context.Background()
		sess := w.session(t, c)
		paid := cents(t, w.account(t, sess, "GET", "/"+c.ID+"/cancellation", nil).json(t), "paid_cents")
		p := w.account(t, sess, "POST", "/"+c.ID+"/change-dates/preview", map[string]any{"arrival": m8Arrival, "departure": "2027-06-11"}).json(t)
		first := cents(t, p, "refund_cents")
		if first <= 0 || first >= paid {
			t.Fatalf("date change refund %v (paid %d)", p, paid)
		}
		if r := w.account(t, sess, "POST", "/"+c.ID+"/change-dates", map[string]any{
			"arrival": m8Arrival, "departure": "2027-06-11", "expected_total_cents": cents(t, p, "new_total_cents"),
		}, "Idempotency-Key", "rest-chg"); r.status != 200 {
			t.Fatalf("change %d %s", r.status, r.body)
		}
		prev := w.account(t, sess, "GET", "/"+c.ID+"/cancellation", nil).json(t)
		rest := cents(t, prev, "refund_cents")
		if cents(t, prev, "paid_cents") != paid-first || rest != paid-first {
			t.Fatalf("after the change %v (paid %d, refunded %d)", prev, paid, first)
		}
		if r := w.account(t, sess, "POST", "/"+c.ID+"/cancel", map[string]any{"expected_refund_cents": rest}, "Idempotency-Key", "rest-cancel"); r.status != 200 {
			t.Fatalf("cancel after a partial refund %d %s", r.status, r.body)
		}
		var sum int64
		for _, r := range w.fake.Refunds {
			sum += r.AmountCents
		}
		if len(w.fake.Refunds) != 2 || sum != paid {
			t.Fatalf("refunds %+v, Σ %d, paid %d", w.fake.Refunds, sum, paid)
		}
		var matches []store.PaymentMatch
		if err := db.NewSelect().Model(&matches).Where("marina_id = ? AND reverses_match_id IS NULL", w.marina).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if len(matches) != 1 || matches[0].ReversedCents != matches[0].AmountCents || matches[0].AmountCents != paid {
			t.Fatalf("match %+v", matches)
		}
		vs, err := store.CheckPaymentsInvariants(ctx, db.DB, w.scope)
		if err != nil || len(vs) != 0 {
			t.Fatalf("ledger %+v %v", vs, err)
		}
		if got := w.contractOf(t, w.bookingOf(t, c.ID)); got.Status != "cancelled" {
			t.Fatalf("status %s", got.Status)
		}
		w.invariants(t)
	})
}

// TestPortal_ChangeDatesBackAndForthRefundsEachTime: a stay shortened, lengthened and shortened again to the same
// dates is refunded at each shortening (the refund key is unique per change, never a replay of the first refund),
// and the ledger stays consistent (B50, B52, B52r).
func TestPortal_ChangeDatesBackAndForthRefundsEachTime(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		_, c := w.confirmed(t, "k-bnf-0001", "bnf@example.test")
		sess := w.session(t, c)
		var refunds []int64
		change := func(departure, key string) {
			t.Helper()
			p := w.account(t, sess, "POST", "/"+c.ID+"/change-dates/preview", map[string]any{"arrival": m8Arrival, "departure": departure})
			if p.status != 200 {
				t.Fatalf("preview %d %s", p.status, p.body)
			}
			pv := p.json(t)
			r := w.account(t, sess, "POST", "/"+c.ID+"/change-dates", map[string]any{
				"arrival": m8Arrival, "departure": departure, "expected_total_cents": cents(t, pv, "new_total_cents"),
			}, "Idempotency-Key", key)
			if r.status != 200 {
				t.Fatalf("change to %s: %d %s", departure, r.status, r.body)
			}
			if n := cents(t, r.json(t), "refund_cents"); n > 0 {
				refunds = append(refunds, n)
			}
		}
		change("2027-06-11", "idem-key-bnf-1") // shorter: refund
		change("2027-06-12", "idem-key-bnf-2") // longer again: supplement
		change("2027-06-11", "idem-key-bnf-3") // the same shorter dates: refunded again
		if len(refunds) != 2 || refunds[0] <= 0 || refunds[1] <= 0 {
			t.Fatalf("announced refunds %v", refunds)
		}
		// Two refund rows and two reversals, one per shortening (not one row sent twice to the provider).
		var rows []store.Refund
		if err := db.NewSelect().Model(&rows).Where("marina_id = ?", w.marina).OrderExpr("created_at, id").Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		var reversed int64
		if err := db.NewSelect().TableExpr("payment_matches").ColumnExpr("COALESCE(SUM(reversed_cents), 0)").
			Where("marina_id = ?", w.marina).Scan(context.Background(), &reversed); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].AmountCents != refunds[0] || rows[1].AmountCents != refunds[1] || reversed != refunds[0]+refunds[1] {
			t.Fatalf("refund rows %+v, reversed %d, announced %v", rows, reversed, refunds)
		}
		if len(w.fake.Refunds) != 2 || w.fake.Refunds[0].IdempotencyKey == w.fake.Refunds[1].IdempotencyKey {
			t.Fatalf("provider refunds %+v", w.fake.Refunds)
		}
		vs, err := store.CheckPaymentsInvariants(context.Background(), db.DB, w.scope)
		if err != nil || len(vs) != 0 {
			t.Fatalf("ledger %+v %v", vs, err)
		}
		w.invariants(t)
	})
}
