//go:build integration

package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// availability runs the public availability search (L 1000 × B 350 unless dims are given).
func (w *m8World) availability(t *testing.T, arrival, departure, dims string) response {
	t.Helper()
	if dims == "" {
		dims = "&loa_cm=1000&beam_cm=350"
	}
	return w.pub.do(t, "GET", w.P+"/booking/availability?arrival="+arrival+"&departure="+departure+dims, nil)
}

// nightsOf returns the berth_occupancies nights of a resource held by an allocation.
func (w *m8World) nightsOf(t *testing.T, allocationID string) []string {
	t.Helper()
	var rows []struct {
		BerthID string `bun:"berth_id"`
		Night   string `bun:"night"`
		Kind    string `bun:"kind"`
	}
	if err := w.h.app.DB.NewSelect().TableExpr("berth_occupancies").ColumnExpr("CAST(berth_id AS TEXT) AS berth_id, CAST(night AS TEXT) AS night, kind").
		Where("allocation_id = ?", allocationID).OrderExpr("night").Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.Kind+":"+r.Night[:10])
	}
	return out
}

// TestBooking_CreateHoldsConcreteResource (INV-08, m8-plan §4.3): POST /bookings holds one concrete berth of the
// requested category for every night of the stay, as a hold that expires with the booking (held, PORTO_HOLD_TTL),
// in the same transaction as the held transit; the berth is not offered to the next search.
func TestBooking_CreateHoldsConcreteResource(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		b := w.mustCreate(t, "k-hold-0001")
		c := w.contractOf(t, str(b, "booking_id"))
		if c.Status != "held" || c.Kind != store.ContractKindTransit {
			t.Fatalf("transit %+v", c)
		}
		als := w.activeAllocations(t, c.ID)
		if len(als) != 1 {
			t.Fatalf("allocations %+v", als)
		}
		al := als[0]
		if al.ResourceType != store.ResourceBerth || !slices.Contains(w.harbour.Berths, al.ResourceID) || al.Kind != store.AllocKindHold ||
			al.FirstNight.String() != m8Arrival || al.LastNight.String() != "2027-06-11" || al.VesselID != c.VesselID {
			t.Fatalf("allocation %+v", al)
		}
		exp, err := time.Parse(time.RFC3339, str(b, "hold_expires_at"))
		if err != nil || !al.ExpiresAt.Equal(exp) || !exp.Equal(w.clock.Now().Add(10*time.Minute)) {
			t.Fatalf("hold expiry %v vs %v (%v)", al.ExpiresAt, str(b, "hold_expires_at"), err)
		}
		if got := w.nightsOf(t, al.ID); !slices.Equal(got, []string{"hold:2027-06-10", "hold:2027-06-11"}) {
			t.Fatalf("occupancy nights %v", got)
		}
		// Two more bookings take the other two berths; each holds a different one, then nothing is left.
		seen := map[string]bool{al.ResourceID: true}
		for _, k := range []string{"k-hold-0002", "k-hold-0003"} {
			x := w.contractOf(t, str(w.mustCreate(t, k), "booking_id"))
			a := w.activeAllocations(t, x.ID)
			if len(a) != 1 || seen[a[0].ResourceID] {
				t.Fatalf("second hold %+v", a)
			}
			seen[a[0].ResourceID] = true
		}
		if r := w.create(t, "k-hold-0004", 1000, 350, m8Arrival, m8Departure); r.status != 409 || r.problemType() != "booking_unavailable" {
			t.Fatalf("fourth booking %d %s", r.status, r.body)
		}
		w.invariants(t)
	})
}

// TestBooking_AvailabilityNoPII (INV-70, m8-plan §3.2): the public availability answers categories with yes/no and a
// "from" price only: no berth code, id, customer, vessel name, reference or count, even with confirmed bookings of
// named customers on the same nights.
func TestBooking_AvailabilityNoPII(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		b, c := w.confirmed(t, "k-pii-0001", "mario.rossi@example.test")
		w.mustCreate(t, "k-pii-0002")
		r := w.availability(t, m8Arrival, m8Departure, "")
		if r.status != 200 {
			t.Fatalf("availability %d %s", r.status, r.body)
		}
		body := string(r.body)
		var codes []string
		if err := db.NewSelect().TableExpr("berths").Column("code").Where("marina_id = ?", w.marina).Scan(context.Background(), &codes); err != nil {
			t.Fatal(err)
		}
		forbidden := append([]string{
			"mario.rossi", "example.test", "Anna", "Bianchi", "Maestrale", c.ID, c.CustomerID, c.VesselID,
			str(b, "booking_id"), str(b, "reference"),
		}, append(codes, w.harbour.Berths...)...)
		for _, f := range forbidden {
			if f != "" && strings.Contains(body, f) {
				t.Fatalf("availability leaks %q: %s", f, body)
			}
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(r.body, &top); err != nil {
			t.Fatal(err)
		}
		for k := range top {
			if !slices.Contains([]string{"arrival", "departure", "nights", "reason", "categories"}, k) {
				t.Fatalf("unexpected field %q", k)
			}
		}
		cats := jsonList(r.json(t), "categories")
		if len(cats) == 0 {
			t.Fatal("no categories")
		}
		for _, x := range cats {
			for k := range x.(map[string]any) {
				if !slices.Contains([]string{"code", "name", "available", "from_cents", "approval_required"}, k) {
					t.Fatalf("unexpected category field %q in %v", k, x)
				}
			}
		}
		// The quote answers a price, nothing about who is moored.
		q := w.pub.do(t, "POST", w.P+"/booking/quote", map[string]any{
			"arrival": m8Arrival, "departure": m8Departure, "category": "L12", "vessel": map[string]any{"loa_cm": 1000, "beam_cm": 350},
		})
		for _, f := range forbidden {
			if f != "" && strings.Contains(string(q.body), f) {
				t.Fatalf("quote leaks %q", f)
			}
		}
	})
}

// TestBooking_CrossMarina404 (INV-88, spec 26): a booking of marina A is not reachable from marina B in any way:
// public operations with A's token on B's slug, a customer-area session of A on B's slug and B's office all get
// 404, never 403, and B's lists do not show it.
func TestBooking_CrossMarina404(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		ctx := context.Background()
		other, err := w.h.app.Auth.CreateOwner(ctx, authOwner("altra"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewUpdate().Table("marinas").Set("booking_enabled = ?", true).Where("id = ?", other.MarinaID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		b, c := w.confirmed(t, "k-xm-0001", "xm@example.test")
		held := w.mustCreate(t, "k-xm-0002")
		B := "/api/public/v1/altra"
		tok := "Porto-Booking-Token"
		for _, r := range []response{
			w.pub.do(t, "GET", B+"/bookings/"+str(held, "booking_id"), nil, tok, str(held, "booking_token")),
			w.pub.do(t, "PUT", B+"/bookings/"+str(held, "booking_id")+"/details", map[string]any{
				"customer": map[string]any{"first_name": "X", "last_name": "Y", "email": "x@example.test", "country": "IT", "locale": "it"},
				"vessel":   map[string]any{"name": "Z", "hull_type": "monohull_sail"},
				"consents": map[string]any{"terms_version": 1, "privacy_version": 1, "marketing": false},
			}, tok, str(held, "booking_token")),
			w.pub.do(t, "POST", B+"/bookings/"+str(held, "booking_id")+"/pay", map[string]any{}, tok, str(held, "booking_token"), "Idempotency-Key", "xm-pay-00001"),
			w.pub.do(t, "GET", B+"/bookings/"+str(b, "booking_id")+"/status", nil, tok, str(b, "booking_token")),
		} {
			if r.status != 404 || r.problemType() != "link_invalid" {
				t.Fatalf("public cross-marina %d %s", r.status, r.body)
			}
		}
		if got := w.contractOf(t, str(held, "booking_id")); got.CustomerID != "" {
			t.Fatal("details written through another marina")
		}
		// Customer area: A's session on B's slug.
		sess := w.session(t, c)
		for _, path := range []string{"/account/bookings", "/account/bookings/" + c.ID, "/account/bookings/" + c.ID + "/cancellation"} {
			if r := w.pub.do(t, "GET", B+path, nil, "Porto-Customer-Session", sess); r.status != 404 {
				t.Fatalf("account %s on another marina: %d %s", path, r.status, r.body)
			}
		}
		if r := w.pub.do(t, "POST", B+"/account/bookings/"+c.ID+"/cancel", map[string]any{"expected_refund_cents": 0},
			"Porto-Customer-Session", sess, "Idempotency-Key", "xm-cancel-0001"); r.status != 404 {
			t.Fatalf("cancel on another marina %d", r.status)
		}
		// The office of B: A's bookings are not found and not listed; A's office cannot read B.
		ob := w.h.cookieClient()
		ob.login(t, "owner-altra@porto.test")
		MB := A + "/marinas/" + other.MarinaID
		if r := ob.do(t, "GET", MB+"/online-bookings/"+c.ID, nil); r.status != 404 {
			t.Fatalf("B reads A's booking: %d", r.status)
		}
		if r := ob.do(t, "POST", MB+"/online-bookings/"+c.ID+"/reject", map[string]any{"reason": "x", "notify": false}, "If-Match", `"1"`); r.status != 404 {
			t.Fatalf("B rejects A's booking: %d", r.status)
		}
		if l := ob.expect(t, 200, "GET", MB+"/online-bookings", nil); len(jsonList(l, "items")) != 0 {
			t.Fatalf("B lists A's bookings %v", l)
		}
		if r := w.owner.do(t, "GET", MB+"/online-bookings", nil); r.status != 404 {
			t.Fatalf("A reads B's list: %d", r.status)
		}
		if got := w.contractOf(t, str(b, "booking_id")); got.Status != "confirmed" {
			t.Fatalf("status %s", got.Status)
		}
	})
}

// TestBooking_RetryNextSolutionOnConflict (m8-plan §4.3, INV-01): when the chosen berth is taken between the search
// and the write (a lost race, forced at the end of the attempt), the attempt rolls back entirely and the next
// solution is booked; after three lost attempts the answer is 409 booking_unavailable and nothing is left behind.
func TestBooking_RetryNextSolutionOnConflict(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		ctx := context.Background()
		// The engine's first choice for this vessel on an empty harbour (same choice on any free dates).
		first := w.activeAllocations(t, w.contractOf(t, str(w.create(t, "k-retry-0000", 1000, 350, "2027-06-20", "2027-06-22").json(t), "booking_id")).ID)[0].ResourceID
		counts := func() [4]int {
			var out [4]int
			for i, tbl := range []string{"contracts", "online_bookings", "inquiries", "allocations"} {
				n, err := db.NewSelect().TableExpr(tbl).Where("marina_id = ?", w.marina).Count(ctx)
				if err != nil {
					t.Fatal(err)
				}
				out[i] = int(n)
			}
			return out
		}
		before := counts()
		var calls atomic.Int32
		w.h.app.Booking.SetBeforeCommit(func(attempt int) error {
			calls.Add(1)
			if attempt == 1 {
				return occupancy.ErrResourceTaken
			}
			return nil
		})
		t.Cleanup(func() { w.h.app.Booking.SetBeforeCommit(nil) })
		b := w.mustCreate(t, "k-retry-0001")
		c := w.contractOf(t, str(b, "booking_id"))
		als := w.activeAllocations(t, c.ID)
		if calls.Load() != 2 || len(als) != 1 || als[0].ResourceID == first {
			t.Fatalf("calls %d, allocations %+v (first choice %s)", calls.Load(), als, first)
		}
		after := counts()
		for i := range after {
			if after[i] != before[i]+1 {
				t.Fatalf("rows before %v after %v: the lost attempt left rows", before, after)
			}
		}
		// Every attempt lost: three tries, then booking_unavailable with the categories still available.
		calls.Store(0)
		w.h.app.Booking.SetBeforeCommit(func(int) error { calls.Add(1); return occupancy.ErrResourceTaken })
		before = counts()
		r := w.create(t, "k-retry-0002", 1000, 350, "2027-06-25", "2027-06-27") // three free berths, three tries
		if r.status != 409 || r.problemType() != "booking_unavailable" || calls.Load() != 3 {
			t.Fatalf("all lost: %d %s, calls %d", r.status, r.body, calls.Load())
		}
		if got := counts(); got != before {
			t.Fatalf("rows before %v after %v", before, got)
		}
		// Another error is not a lost race: no retry, the error is returned.
		calls.Store(0)
		boom := errors.New("boom")
		w.h.app.Booking.SetBeforeCommit(func(int) error { calls.Add(1); return boom })
		if r := w.create(t, "k-retry-0003", 1000, 350, m8Arrival, m8Departure); r.status != 500 || calls.Load() != 1 {
			t.Fatalf("other error %d calls %d", r.status, calls.Load())
		}
		w.h.app.Booking.SetBeforeCommit(nil)
		w.invariants(t)
	})
}

// TestLostDemand_AnonymousRowDeduped (m8-plan §9, D17): a search with nothing available becomes one anonymous
// inquiry (no customer, vessel, contract or client data); the same search again within 30 minutes is not stored
// again, a different search is, and the same search after the dedupe window is stored again.
func TestLostDemand_AnonymousRowDeduped(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		ctx := context.Background()
		type row struct {
			Outcome  string `bun:"outcome"`
			Channel  string `bun:"channel"`
			Source   string `bun:"source"`
			Purpose  string `bun:"purpose"`
			LOA      int64  `bun:"loa_cm"`
			Beam     int64  `bun:"beam_cm"`
			Arrival  string `bun:"arrival"`
			Customer string `bun:"customer_id"`
			Vessel   string `bun:"vessel_id"`
			Contract string `bun:"contract_id"`
		}
		rows := func() []row {
			var out []row
			if err := db.NewSelect().TableExpr("inquiries").Column("outcome", "channel", "source", "purpose", "loa_cm", "beam_cm").
				ColumnExpr("CAST(arrival AS TEXT) AS arrival").
				ColumnExpr("coalesce(CAST(customer_id AS TEXT), '') AS customer_id, coalesce(CAST(vessel_id AS TEXT), '') AS vessel_id").
				ColumnExpr("coalesce(CAST(contract_id AS TEXT), '') AS contract_id").
				Where("marina_id = ?", w.marina).OrderExpr("created_at, id").Scan(ctx, &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		big := "&loa_cm=3000&beam_cm=800"
		for range 3 {
			w.availability(t, m8Arrival, m8Departure, big)
		}
		got := rows()
		if len(got) != 1 {
			t.Fatalf("deduped rows %+v", got)
		}
		if r := got[0]; r.Outcome != "declined_size" || r.Channel != "online" || r.Source != "public" || r.Purpose != "transit" ||
			r.LOA != 3000 || r.Beam != 800 || r.Arrival[:10] != m8Arrival || r.Customer != "" || r.Vessel != "" || r.Contract != "" {
			t.Fatalf("anonymous row %+v", r)
		}
		// A search with something available stores nothing; other dates are another row.
		w.availability(t, m8Arrival, m8Departure, "")
		w.availability(t, "2027-06-20", "2027-06-22", big)
		if got = rows(); len(got) != 2 {
			t.Fatalf("rows %+v", got)
		}
		// After the window the same search counts again.
		w.setClock(t, w.clock.Now().Add(31*time.Minute))
		w.availability(t, m8Arrival, m8Departure, big)
		if got = rows(); len(got) != 3 {
			t.Fatalf("rows after the window %+v", got)
		}
		var cols []string
		if db.IsPostgres() {
			err := db.NewRaw("SELECT column_name FROM information_schema.columns WHERE table_name = 'inquiries' AND table_schema = current_schema()").Scan(ctx, &cols)
			if err != nil {
				t.Fatal(err)
			}
		} else if err := db.NewRaw("SELECT name FROM pragma_table_info('inquiries')").Scan(ctx, &cols); err != nil {
			t.Fatal(err)
		}
		for _, c := range cols {
			if strings.Contains(c, "ip") && c != "ship" || strings.Contains(c, "agent") {
				t.Fatalf("inquiries stores client data: %s", c)
			}
		}
	})
}

// TestChannel_CancelReleases (INV-89, m8-plan §8): a channel cancellation cancels the transit and releases its
// berth nights and its online quota in the same transaction; the same cancel event applied again changes nothing;
// the freed place is bookable online.
func TestChannel_CancelReleases(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.connectChannel(t)
		g := w.quotaGroup(t, 1, w.harbour.Berths)
		if code := w.postChannel(t, m8Reservation("CX-1", "cx-new", ext.ChannelReservationNew, true)); code != 200 {
			t.Fatalf("webhook %d", code)
		}
		r := w.reservation(t, "CX-1")
		als := w.activeAllocations(t, r.ContractID)
		if r.Status != store.ChannelResBooked || len(als) != 1 || len(w.claimsPerNight(t, g)) != 2 {
			t.Fatalf("booked %+v %+v claims %v", r, als, w.claimsPerNight(t, g))
		}
		// The quota is used: the online page has nothing for these nights.
		if x := w.create(t, "k-cx-0001", 1000, 350, m8Arrival, m8Departure); x.status != 409 {
			t.Fatalf("over quota %d", x.status)
		}
		cancel := m8Reservation("CX-1", "cx-cancel", ext.ChannelReservationCancel, true)
		for range 2 {
			if code := w.postChannel(t, cancel); code != 200 {
				t.Fatalf("cancel %d", code)
			}
		}
		r = w.reservation(t, "CX-1")
		c, err := store.GetContract(context.Background(), db.DB, w.scope, r.ContractID)
		if err != nil || r.Status != store.ChannelResCancelled || c.Status != "cancelled" {
			t.Fatalf("cancelled %+v %+v %v", r, c, err)
		}
		if a := w.activeAllocations(t, c.ID); len(a) != 0 {
			t.Fatalf("allocations left %+v", a)
		}
		if got := w.nightsOf(t, als[0].ID); len(got) != 0 {
			t.Fatalf("nights left %v", got)
		}
		if n := w.claimsPerNight(t, g); len(n) != 0 {
			t.Fatalf("claims left %v", n)
		}
		n, _ := db.NewSelect().TableExpr("audit_log").Where("entity_id = ? AND action = 'transit.cancel'", c.ID).Count(context.Background())
		if n != 1 {
			t.Fatalf("cancel applied %d times", n)
		}
		acks := w.ch.Acks()
		if last := acks[len(acks)-1]; last.Outcome != ext.ChannelAckAccepted {
			t.Fatalf("ack %+v", last)
		}
		w.mustCreate(t, "k-cx-0002")
		w.invariants(t)
	})
}

// TestAcceptance_M8_ApprovalFlow (m8-plan §6, §14.4): over the API only. A booking needing approval authorises the
// card, waits in the office list; a stale If-Match is refused; the office approves (capture) and the capture
// webhook confirms with the confirmation email. A second booking is rejected from the office: the authorisation is
// voided, the place released and the customer told.
func TestAcceptance_M8_ApprovalFlow(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.approvalAlways(t)
		av := w.availability(t, m8Arrival, m8Departure, "").json(t)
		if cats := jsonList(av, "categories"); len(cats) != 1 || cats[0].(map[string]any)["approval_required"] != true {
			t.Fatalf("availability announces approval %v", av)
		}
		b := w.mustCreate(t, "k-aflow-0001")
		if b["approval_required"] != true {
			t.Fatalf("create %v", b)
		}
		w.details(t, b, "aflow@example.test")
		w.upload(t, b, "insurance_policy")
		if paid := w.pay(t, b); str(paid, "status") != "pending_payment" || !w.fake.LastCheckout().CaptureManual {
			t.Fatalf("pay %v", paid)
		}
		w.authorize(t, "evt_aflow_auth", "pi_aflow")
		st := w.pub.expect(t, 200, "GET", w.P+"/bookings/"+str(b, "booking_id"), nil, "Porto-Booking-Token", str(b, "booking_token"))
		if str(st, "status") != "awaiting_approval" {
			t.Fatalf("public status %v", st)
		}
		list := w.owner.expect(t, 200, "GET", w.M+"/online-bookings?status=awaiting_approval", nil)
		items := jsonList(list, "items")
		if len(items) != 1 {
			t.Fatalf("office list %v", list)
		}
		it := items[0].(map[string]any)
		cid := str(it, "contract_id")
		if str(it, "reference") != str(b, "reference") || str(it, "approval_deadline") == "" {
			t.Fatalf("list item %v", it)
		}
		det := w.owner.expect(t, 200, "GET", w.M+"/online-bookings/"+cid, nil)
		ver := etagOf(det["version"])
		if r := w.owner.do(t, "POST", w.M+"/online-bookings/"+cid+"/approve", nil, "If-Match", `"999"`); r.status != 412 {
			t.Fatalf("stale If-Match %d %s", r.status, r.body)
		}
		ap := w.owner.expect(t, 200, "POST", w.M+"/online-bookings/"+cid+"/approve", nil, "If-Match", ver)
		if str(ap, "status") != "awaiting_approval" || len(w.fake.Captures) != 1 {
			t.Fatalf("approve %v captures %+v", ap, w.fake.Captures)
		}
		req := w.fake.LastCheckout()
		if code := w.postWebhook(t, ext.PaymentEvent{
			EventID: "evt_aflow_cap", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_aflow", AmountCents: req.AmountCents,
			Metadata: map[string]string{"marina_id": w.marina, "intent_id": req.IntentID},
		}, w.clock.Now()); code != 204 {
			t.Fatalf("capture webhook %d", code)
		}
		if d := w.owner.expect(t, 200, "GET", w.M+"/online-bookings/"+cid, nil); str(d, "status") != "confirmed" {
			t.Fatalf("after capture %v", d["status"])
		}
		w.h.drainMail()
		if len(w.h.mailTo("aflow@example.test", "booking_received")) != 1 || len(w.h.mailTo("aflow@example.test", "booking_confirmed")) != 1 {
			t.Fatal("received/confirmed emails")
		}

		// Rejection from the office.
		b2 := w.mustCreate(t, "k-aflow-0002")
		w.details(t, b2, "aflow2@example.test")
		w.upload(t, b2, "insurance_policy")
		w.pay(t, b2)
		w.authorize(t, "evt_aflow_auth2", "pi_aflow2")
		c2 := w.contractOf(t, str(b2, "booking_id"))
		if len(w.activeAllocations(t, c2.ID)) != 1 {
			t.Fatal("awaiting approval without its hold (INV-08)")
		}
		d2 := w.owner.expect(t, 200, "GET", w.M+"/online-bookings/"+c2.ID, nil)
		rj := w.owner.expect(t, 200, "POST", w.M+"/online-bookings/"+c2.ID+"/reject", map[string]any{"reason": "pescaggio", "notify": true},
			"If-Match", etagOf(d2["version"]))
		if str(rj, "status") != "cancelled" || len(w.fake.Voids) != 1 || len(w.activeAllocations(t, c2.ID)) != 0 {
			t.Fatalf("reject %v voids %+v", rj["status"], w.fake.Voids)
		}
		w.h.drainMail()
		if len(w.h.mailTo("aflow2@example.test", "booking_rejected")) != 1 {
			t.Fatal("rejection email")
		}
		if r := w.owner.do(t, "POST", w.M+"/online-bookings/"+c2.ID+"/approve", nil, "If-Match", etagOf(rj["version"])); r.status < 400 {
			t.Fatalf("approve after reject %d", r.status)
		}
		w.invariants(t)
	})
}

// TestAcceptance_M8_ChannelFlow (ADR 0013, m8-plan §8, §14.4): over the API only. The office connects the fake
// channel, tests it and syncs: a pulled paid reservation is booked and confirmed by the channel, listed in the
// office lists, availability is pushed; a signed cancel releases the place and is listed as cancelled.
func TestAcceptance_M8_ChannelFlow(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		chs := w.owner.expect(t, 200, "GET", w.M+"/booking-channels", nil)
		if items := jsonList(chs, "items"); len(items) != 1 || str(items[0].(map[string]any), "code") != "fake" {
			t.Fatalf("channels %v", chs)
		}
		put := w.owner.do(t, "PUT", w.M+"/booking-channels/fake", map[string]any{
			"enabled": true, "push_enabled": true, "category_map": map[string]string{"EXT-A": "L12"},
			"credentials": map[string]string{"marina_ref": "M-1"}, "webhook_secret": m8ChannelSecret,
		}, "If-Match", `"0"`)
		if put.status != 200 || strings.Contains(string(put.body), m8ChannelSecret) || strings.Contains(string(put.body), "M-1") {
			t.Fatalf("connect %d %s", put.status, put.body)
		}
		etag := put.header.Get("ETag")
		if tr := w.owner.expect(t, 200, "POST", w.M+"/booking-channels/fake/test", nil); tr["ok"] != true {
			t.Fatalf("test %v", tr)
		}
		w.h.app.Channels.SetPushDays(12)
		w.ch.Inject(m8Reservation("FLOW-1", "flow-new", ext.ChannelReservationNew, true))
		sy := w.owner.expect(t, 200, "POST", w.M+"/booking-channels/fake/sync", nil)
		if sy["pulled"] != float64(1) || sy["booked"] != float64(1) || sy["pushed"] != true {
			t.Fatalf("sync %v", sy)
		}
		if avail, _ := w.ch.Pushed(); len(avail) == 0 || avail[len(avail)-1].MarinaRef != "M-1" {
			t.Fatalf("pushed %+v", avail)
		}
		res := w.owner.expect(t, 200, "GET", w.M+"/channel-reservations", nil)
		items := jsonList(res, "items")
		if len(items) != 1 || str(items[0].(map[string]any), "status") != "booked" {
			t.Fatalf("reservations %v", res)
		}
		ob := w.owner.expect(t, 200, "GET", w.M+"/online-bookings?channel=external", nil)
		obs := jsonList(ob, "items")
		if len(obs) != 1 || str(obs[0].(map[string]any), "status") != "confirmed" || str(obs[0].(map[string]any), "channel_code") != "fake" {
			t.Fatalf("office list %v", ob)
		}
		cid := str(obs[0].(map[string]any), "contract_id")
		if acks := w.ch.Acks(); len(acks) != 1 || acks[0].Outcome != ext.ChannelAckAccepted {
			t.Fatalf("acks %+v", acks)
		}
		// The channel cancels with a signed callback.
		if code := w.postChannel(t, m8Reservation("FLOW-1", "flow-cancel", ext.ChannelReservationCancel, true)); code != 200 {
			t.Fatalf("cancel %d", code)
		}
		if d := w.owner.expect(t, 200, "GET", w.M+"/online-bookings/"+cid, nil); str(d, "status") != "cancelled" {
			t.Fatalf("after cancel %v", d["status"])
		}
		if len(w.activeAllocations(t, cid)) != 0 {
			t.Fatal("place not released")
		}
		res = w.owner.expect(t, 200, "GET", w.M+"/channel-reservations", nil)
		if items := jsonList(res, "items"); str(items[0].(map[string]any), "status") != "cancelled" {
			t.Fatalf("reservations after cancel %v", res)
		}
		// Disabled connection: the callback is refused and nothing is booked.
		if r := w.owner.do(t, "PUT", w.M+"/booking-channels/fake", map[string]any{
			"enabled": false, "push_enabled": false, "category_map": map[string]string{"EXT-A": "L12"},
		}, "If-Match", `"0"`); r.status != 412 {
			t.Fatalf("stale If-Match %d", r.status)
		}
		w.owner.expect(t, 200, "PUT", w.M+"/booking-channels/fake", map[string]any{
			"enabled": false, "push_enabled": false, "category_map": map[string]string{"EXT-A": "L12"},
		}, "If-Match", etag)
		if code := w.postChannel(t, m8Reservation("FLOW-2", "flow-new-2", ext.ChannelReservationNew, true)); code == 200 {
			n, _ := db.NewSelect().TableExpr("channel_reservations").Where("marina_id = ? AND external_ref = 'FLOW-2'", w.marina).Count(context.Background())
			if n != 0 {
				t.Fatal("a disabled connection booked")
			}
		}
		w.invariants(t)
	})
}
