//go:build integration

package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/channel/fakechannel"
	"github.com/coste360/porto/internal/domain/onlineconf"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// m8World is the desk world of M5 with online booking enabled: the fake payment provider, the fake booking channel,
// the berths of the harbour sold online and a public client (no cookies, tokens in headers).
type m8World struct {
	*deskWorld
	fake  *fixture.FakeProvider
	ch    *fakechannel.Channel
	pub   *client
	P     string // /api/public/v1/online
	scope store.MarinaScope
}

const m8Arrival, m8Departure = "2027-06-10", "2027-06-12"

func newM8World(t *testing.T, db *store.DB, provider bool) *m8World {
	t.Helper()
	fake := fixture.NewFakeProvider()
	ch := fakechannel.New()
	x := ext.Extensions{PaymentProviders: []ext.PaymentProvider{fake}, BookingChannels: []ext.BookingChannel{ch}}
	w := &m8World{deskWorld: newM5World(t, db, x), fake: fake, ch: ch, P: "/api/public/v1/online"}
	w.pub = w.h.cookieClient()
	ctx := context.Background()
	scope, err := store.SystemMarinaScope(ctx, db.DB, w.marina)
	if err != nil {
		t.Fatal(err)
	}
	w.scope = scope
	if _, err := db.NewUpdate().Table("marinas").Set("booking_enabled = ?", true).
		Set("allowed_embed_origins = ?", store.Strings{"https://club.example"}).Where("id = ?", w.marina).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.NewUpdate().Table("berths").Set("online_sellable = ?", true).Where("marina_id = ?", w.marina).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if provider {
		w.enableProvider(t)
	}
	w.settings(t, func(s *store.OnlineBookingSettings) {})
	w.publishTexts(t)
	return w
}

// publishTexts publishes version 1 of the terms and privacy texts (required consents, m8-plan §5.1).
func (w *m8World) publishTexts(t *testing.T) {
	t.Helper()
	for _, kind := range []string{"terms", "privacy"} {
		w.owner.expect(t, 201, "POST", w.M+"/online-booking/texts", map[string]any{
			"kind": kind, "texts": map[string]any{"it": "Testo " + kind, "en": "Text " + kind},
		})
	}
}

func (w *m8World) enableProvider(t *testing.T) {
	t.Helper()
	set := w.owner.expect(t, 200, "GET", w.M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", nil)
	w.owner.expect(t, 200, "PUT", w.M+"/payment-providers/"+fixture.FakeProviderCode+"/settings", map[string]any{
		"enabled": true, "mode": "test", "methods": []string{"card"}, "secret_key": "sk_test_x", "webhook_secret": m5WebhookSecret,
		"sepa_enabled": false,
	}, "If-Match", etagOf(set["version"]))
}

// settings writes the online booking settings row (defaults of onlineconf, no required document) and lets mutate
// change it.
func (w *m8World) settings(t *testing.T, mutate func(s *store.OnlineBookingSettings)) {
	t.Helper()
	d := onlineconf.Defaults()
	now := store.UTC(w.clock.Now())
	row := &store.OnlineBookingSettings{
		MarinaID: w.marina, MinNights: d.MinNights, MaxNights: d.MaxNights, MinAdvanceHours: d.MinAdvanceHours,
		MaxAdvanceDays: d.MaxAdvanceDays, ArrivalFrom: d.ArrivalFrom, ArrivalTo: d.ArrivalTo, ClosedWeekdays: store.JSON("[]"),
		PaymentMode: d.PaymentMode, DepositBP: d.DepositBP, CancellationTiers: store.MustJSON(d.CancellationTiers),
		RefundAs: d.RefundAs, Approval: store.MustJSON(d.Approval),
		RequiredFields: store.MustJSON(onlineconf.RequiredFields{Documents: []string{}, PeopleOnBoard: false, Phone: false}),
		RevealBerth:    d.RevealBerth, LatePaymentRecovery: true, MaxLiveHolds: 500, ArrivalInfo: store.JSON(`{"vhf_channel":"9"}`),
		ThemePreset: "default", ShowFeet: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	mutate(row)
	if _, err := w.h.app.DB.NewInsert().Model(row).On("CONFLICT (marina_id) DO UPDATE").
		Set("min_nights = EXCLUDED.min_nights, max_nights = EXCLUDED.max_nights, payment_mode = EXCLUDED.payment_mode").
		Set("deposit_bp = EXCLUDED.deposit_bp, approval = EXCLUDED.approval, required_fields = EXCLUDED.required_fields").
		Set("late_payment_recovery = EXCLUDED.late_payment_recovery, allow_without_provider = EXCLUDED.allow_without_provider").
		Set("max_live_holds = EXCLUDED.max_live_holds, cancellation_tiers = EXCLUDED.cancellation_tiers, refund_as = EXCLUDED.refund_as").
		Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// create holds a berth for a vessel through the public API and returns the response.
func (w *m8World) create(t *testing.T, key string, loa, beam int, arrival, departure string) response {
	t.Helper()
	return w.pub.do(t, "POST", w.P+"/bookings", map[string]any{
		"arrival": arrival, "departure": departure, "category": "L12", "locale": "it",
		"vessel": map[string]any{"loa_cm": loa, "beam_cm": beam},
	}, "Idempotency-Key", key)
}

func (w *m8World) mustCreate(t *testing.T, key string) map[string]any {
	t.Helper()
	r := w.create(t, key, 1000, 350, m8Arrival, m8Departure)
	if r.status != 201 {
		t.Fatalf("create %d %s", r.status, r.body)
	}
	return r.json(t)
}

func (w *m8World) details(t *testing.T, b map[string]any, email string) map[string]any {
	t.Helper()
	return w.pub.expect(t, 200, "PUT", w.P+"/bookings/"+str(b, "booking_id")+"/details", map[string]any{
		"customer": map[string]any{"first_name": "Anna", "last_name": "Bianchi", "email": email, "country": "IT", "locale": "it"},
		"vessel":   map[string]any{"name": "Maestrale", "hull_type": "monohull_sail"},
		"consents": map[string]any{"terms_version": 1, "privacy_version": 1, "marketing": false},
	}, "Porto-Booking-Token", str(b, "booking_token"))
}

func (w *m8World) contractOf(t *testing.T, bookingID string) *store.Contract {
	t.Helper()
	ob, err := store.GetOnlineBooking(context.Background(), w.h.app.DB.DB, w.scope, bookingID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := store.GetContract(context.Background(), w.h.app.DB.DB, w.scope, ob.ContractID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (w *m8World) invariants(t *testing.T) {
	t.Helper()
	today := calendar.In(w.clock.Now(), time.UTC)
	vs, err := store.CheckOccupancyInvariants(context.Background(), w.h.app.DB.DB, w.marina, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) > 0 {
		t.Fatalf("occupancy invariants: %+v", vs)
	}
}

func (w *m8World) pay(t *testing.T, b map[string]any) map[string]any {
	t.Helper()
	return w.pub.expect(t, 200, "POST", w.P+"/bookings/"+str(b, "booking_id")+"/pay", map[string]any{"return_to": "https://club.example/thanks"},
		"Porto-Booking-Token", str(b, "booking_token"), "Idempotency-Key", "pay-"+str(b, "booking_id"))
}

// succeed delivers the provider's succeeded event of the last checkout.
func (w *m8World) succeed(t *testing.T, eventID, paymentRef string) {
	t.Helper()
	req := w.fake.LastCheckout()
	ev := ext.PaymentEvent{
		EventID: eventID, Kind: ext.PaymentEventSucceeded, PaymentRef: paymentRef, AmountCents: req.AmountCents, Method: "card_online",
		Metadata: map[string]string{"marina_id": w.marina, "intent_id": req.IntentID},
	}
	if code := w.postWebhook(t, ev, w.clock.Now()); code != 204 {
		t.Fatalf("webhook %d", code)
	}
}

// TestAcceptance_M8_OnlineBookingHappyPath (m8-plan §14.4): config, availability, quote, create (held with a
// concrete hold), details, document, pay (pending_payment, browser return does not confirm), signed webhook →
// confirmed with the confirmation email, then the customer-area cancellation preview from the policy snapshot.
func TestAcceptance_M8_OnlineBookingHappyPath(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		cfg := w.pub.expect(t, 200, "GET", w.P+"/booking/config", nil)
		if cfg["enabled"] != true || cfg["payable"] != true || len(jsonList(cfg, "categories")) != 1 {
			t.Fatalf("config %v", cfg)
		}
		av := w.pub.expect(t, 200, "GET", w.P+"/booking/availability?arrival="+m8Arrival+"&departure="+m8Departure+"&loa_cm=1000&beam_cm=350", nil)
		cats := jsonList(av, "categories")
		if len(cats) != 1 || cats[0].(map[string]any)["available"] != true || cats[0].(map[string]any)["from_cents"] == nil {
			t.Fatalf("availability %v", av)
		}
		if strings.Contains(string(mustJSON(t, av)), "A1") {
			t.Fatal("availability leaks berth codes")
		}
		q := w.pub.expect(t, 200, "POST", w.P+"/booking/quote", map[string]any{
			"arrival": m8Arrival, "departure": m8Departure, "category": "L12", "vessel": map[string]any{"loa_cm": 1000, "beam_cm": 350},
		})
		if q["total_cents"].(float64) <= 0 || q["due_now_cents"] != q["total_cents"] {
			t.Fatalf("quote %v", q)
		}
		b := w.mustCreate(t, "k-happy-0001")
		if str(b, "status") != "held" || str(b, "booking_token") == "" || len(str(b, "reference")) != 8 {
			t.Fatalf("created %v", b)
		}
		c := w.contractOf(t, str(b, "booking_id"))
		if c.Channel != "online" || c.Status != "held" {
			t.Fatalf("transit %+v", c)
		}
		w.invariants(t) // INV-08: the held online transit occupies a concrete resource
		bk := w.details(t, b, "anna@example.test")
		if bk["details_complete"] != true {
			t.Fatalf("details %v", bk)
		}
		pdf, err := os.ReadFile(filepath.Join("testdata", "registry", "sample.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		if r := w.pub.raw(t, "POST", w.P+"/bookings/"+str(b, "booking_id")+"/documents?type=insurance_policy", "application/pdf", pdf,
			"Porto-Booking-Token", str(b, "booking_token")); r.status != 201 {
			t.Fatalf("upload %d %s", r.status, r.body)
		}
		paid := w.pay(t, b)
		if str(paid, "status") != "pending_payment" || !strings.HasPrefix(str(paid, "redirect_url"), "https://pay.fake.test/") || str(paid, "status_token") == "" {
			t.Fatalf("pay %v", paid)
		}
		if co := w.fake.LastCheckout(); !strings.Contains(co.SuccessURL, "/b/online/r/"+str(b, "booking_id")+"#") || co.CaptureManual {
			t.Fatalf("checkout %+v", co)
		}
		// The browser comes back: status by the status token alone, still not confirmed (INV-09).
		st := w.pub.expect(t, 200, "GET", w.P+"/bookings/"+str(b, "booking_id")+"/status", nil, "Porto-Status-Token", str(paid, "status_token"))
		if str(st, "status") != "pending_payment" {
			t.Fatalf("status %v", st)
		}
		w.succeed(t, "evt_m8_ok", "pi_m8_ok")
		c = w.contractOf(t, str(b, "booking_id"))
		if c.Status != "confirmed" || c.ConfirmationSource != "provider" {
			t.Fatalf("not confirmed %+v", c)
		}
		w.h.drainMail()
		if len(w.h.mailTo("anna@example.test", "booking_confirmed")) != 1 {
			t.Fatal("confirmation email missing")
		}
		txt := w.h.mailTo("anna@example.test", "booking_confirmed")[0]
		if !strings.Contains(txt, "PT1:"+c.ID) || !strings.Contains(txt, str(b, "reference")) {
			t.Fatalf("confirmation email %s", txt)
		}
		var outcome string
		if err := db.NewSelect().TableExpr("inquiries").Column("outcome").Where("contract_id = ?", c.ID).Scan(context.Background(), &outcome); err != nil || outcome != "booked" {
			t.Fatalf("inquiry %q %v", outcome, err)
		}
		w.invariants(t)
		// Customer area (booking side): the cancellation preview applies the snapshot tiers (8 days before: 100%).
		prev, err := w.h.app.Booking.PreviewCancellation(context.Background(), w.scope, c.CustomerID, c.ID)
		if err != nil || !prev.Allowed || prev.Paid != prev.Refund || prev.Refund <= 0 {
			t.Fatalf("preview %+v %v", prev, err)
		}
		res, err := w.h.app.Booking.CancelByCustomer(context.Background(), w.scope, c.CustomerID, c.ID, int64(prev.Refund))
		if err != nil || res.Status != "cancelled" {
			t.Fatalf("cancel %+v %v", res, err)
		}
		if len(w.fake.Refunds) != 1 || w.fake.Refunds[0].AmountCents != int64(prev.Refund) {
			t.Fatalf("provider refunds %+v", w.fake.Refunds)
		}
		w.invariants(t)
	})
}

// TestBooking_CreateIdempotent: the same Idempotency-Key and body give the same booking and token; another body is
// 422 idempotency_mismatch; 100 parallel requests with one key create one booking.
func TestBooking_CreateIdempotent(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		a := w.mustCreate(t, "k-idem-0001")
		b := w.mustCreate(t, "k-idem-0001")
		if str(a, "booking_id") != str(b, "booking_id") || str(a, "booking_token") != str(b, "booking_token") {
			t.Fatalf("replay %v %v", a, b)
		}
		if r := w.create(t, "k-idem-0001", 1100, 350, m8Arrival, m8Departure); r.status != 422 || r.problemType() != "idempotency_mismatch" {
			t.Fatalf("other body %d %s", r.status, r.body)
		}
		n, _ := db.NewSelect().TableExpr("online_bookings").Where("marina_id = ?", w.marina).Count(context.Background())
		if n != 1 {
			t.Fatalf("bookings %d", n)
		}
	})
}

// TestBooking_TokenScoped and TestBooking_CrossMarina404 (INV-88): a booking resolves only with its own token and
// slug; anything else is 404 link_invalid.
func TestBooking_TokenScoped(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		a := w.mustCreate(t, "k-scope-0001")
		b := w.mustCreate(t, "k-scope-0002")
		cases := []struct{ path, tok string }{
			{w.P + "/bookings/" + str(a, "booking_id"), str(b, "booking_token")},
			{w.P + "/bookings/" + str(a, "booking_id"), ""},
			{w.P + "/bookings/" + str(a, "booking_id"), "x" + str(a, "booking_token")},
			{w.P + "/bookings/" + store.NewID(), str(a, "booking_token")},
			{"/api/public/v1/marina-assente/bookings/" + str(a, "booking_id"), str(a, "booking_token")},
		}
		for _, c := range cases {
			r := w.pub.do(t, "GET", c.path, nil, "Porto-Booking-Token", c.tok)
			if r.status != 404 || r.problemType() != "link_invalid" {
				t.Fatalf("%s: %d %s", c.path, r.status, r.body)
			}
		}
		if r := w.pub.do(t, "GET", w.P+"/bookings/"+str(a, "booking_id"), nil, "Porto-Booking-Token", str(a, "booking_token")); r.status != 200 {
			t.Fatalf("own token %d", r.status)
		}
		if r := w.pub.do(t, "GET", w.P+"/bookings/"+str(a, "booking_id")+"/status", nil, "Porto-Status-Token", str(a, "booking_token")); r.status != 404 {
			t.Fatalf("booking token as status token for status works only through Porto-Booking-Token: %d", r.status)
		}
		// Booking disabled: every public operation but config is 404 link_invalid.
		if _, err := db.NewUpdate().Table("marinas").Set("booking_enabled = ?", false).Where("id = ?", w.marina).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if r := w.create(t, "k-scope-0003", 1000, 350, m8Arrival, m8Departure); r.status != 404 || r.problemType() != "link_invalid" {
			t.Fatalf("disabled create %d", r.status)
		}
		if cfg := w.pub.expect(t, 200, "GET", w.P+"/booking/config", nil); cfg["enabled"] != false {
			t.Fatalf("disabled config %v", cfg)
		}
	})
}

// TestBooking_AvailabilityFiltersOnlineSellableAndQuota: only online sellable resources are offered, and a group
// whose quota is used up hides its resources (m8-plan §3.2, §3.3).
func TestBooking_AvailabilityFiltersOnlineSellableAndQuota(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		ctx := context.Background()
		avail := func() bool {
			av := w.pub.expect(t, 200, "GET", w.P+"/booking/availability?arrival="+m8Arrival+"&departure="+m8Departure+"&loa_cm=1000&beam_cm=350", nil)
			for _, c := range jsonList(av, "categories") {
				if c.(map[string]any)["available"] == true {
					return true
				}
			}
			return false
		}
		if !avail() {
			t.Fatal("nothing available")
		}
		// A count quota of 1 on a group with every berth: one booking uses it, the next search finds nothing.
		g := &store.BerthGroup{
			ID: store.NewID(), MarinaID: w.marina, Code: "ONLINE", Name: store.JSON(`{"it":"Online"}`), Kind: "custom",
			OnlineQuotaKind: "count", OnlineQuotaValue: 1, Version: 1, CreatedAt: store.UTC(w.clock.Now()), UpdatedAt: store.UTC(w.clock.Now()),
		}
		if _, err := db.NewInsert().Model(g).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		for _, id := range w.harbour.Berths {
			if _, err := db.NewInsert().Model(&store.BerthGroupMember{
				ID: store.NewID(), MarinaID: w.marina, GroupID: g.ID, MapID: w.harbour.MapID, ResourceType: "berth", ResourceID: id,
				CreatedAt: store.UTC(w.clock.Now()),
			}).Exec(ctx); err != nil {
				t.Fatal(err)
			}
		}
		w.mustCreate(t, "k-quota-0001")
		if avail() {
			t.Fatal("quota exhausted but availability shown")
		}
		if r := w.create(t, "k-quota-0002", 1000, 350, m8Arrival, m8Departure); r.status != 409 || r.problemType() != "booking_unavailable" {
			t.Fatalf("over quota %d %s", r.status, r.body)
		}
		w.invariants(t) // Q19: used = claims ≤ cap
		// Not online sellable: nothing at all, even with the quota released.
		if _, err := db.NewUpdate().Table("berth_groups").Set("online_quota_kind = 'none'").Where("id = ?", g.ID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewUpdate().Table("berths").Set("online_sellable = ?", false).Where("marina_id = ?", w.marina).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if avail() {
			t.Fatal("berths not sold online are offered")
		}
	})
}

// TestLostDemand_AnonymousRowDeduped and TestAcceptance_M8_LostDemandInR4: an unanswered search becomes one
// anonymous inquiry (deduped), visible in the R4 report without any change to R4.
func TestAcceptance_M8_LostDemandInR4(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		q := w.P + "/booking/availability?arrival=" + m8Arrival + "&departure=" + m8Departure
		w.pub.expect(t, 200, "GET", q+"&loa_cm=3000&beam_cm=800", nil) // too large: declined_size
		w.pub.expect(t, 200, "GET", q+"&loa_cm=3000&beam_cm=800", nil) // same client, dims, dates: deduped
		for i := range 3 {
			w.mustCreate(t, "k-lost-000"+string(rune('1'+i)))
		}
		av := w.pub.expect(t, 200, "GET", q+"&loa_cm=1000&beam_cm=350", nil) // every berth held: declined_no_space
		for _, c := range jsonList(av, "categories") {
			if c.(map[string]any)["available"] == true {
				t.Fatalf("still available %v", av)
			}
		}
		type row struct {
			Outcome    string `bun:"outcome"`
			Channel    string `bun:"channel"`
			Source     string `bun:"source"`
			CustomerID string `bun:"customer_id"`
			ContractID string `bun:"contract_id"`
		}
		var rows []row
		if err := db.NewSelect().TableExpr("inquiries").Column("outcome", "channel", "source").
			ColumnExpr("coalesce(CAST(customer_id AS TEXT), '') AS customer_id, coalesce(CAST(contract_id AS TEXT), '') AS contract_id").
			Where("marina_id = ?", w.marina).Where("contract_id IS NULL").OrderExpr("outcome").Scan(context.Background(), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].Outcome != "declined_no_space" || rows[1].Outcome != "declined_size" ||
			rows[0].Channel != "online" || rows[0].Source != "public" || rows[0].CustomerID != "" {
			t.Fatalf("lost demand rows %+v", rows)
		}
		rep := w.owner.expect(t, 200, "GET", w.M+"/reports/lost-demand?from=2027-06-01&to=2027-06-30&group_by=reason", nil)
		if len(jsonList(rep, "records")) < 2 {
			t.Fatalf("R4 %v", rep)
		}
	})
}

// TestBooking_HoldExpiryReleasesQuotaAndMarksLost: an unpaid online hold expires with the M3 job, gives its quota
// back and its inquiry becomes lost (m8-plan §4.4).
func TestBooking_HoldExpiryReleasesQuotaAndMarksLost(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		ctx := context.Background()
		g := &store.BerthGroup{
			ID: store.NewID(), MarinaID: w.marina, Code: "Q", Name: store.JSON(`{"it":"Q"}`), Kind: "custom", OnlineQuotaKind: "count",
			OnlineQuotaValue: 5, Version: 1, CreatedAt: store.UTC(w.clock.Now()), UpdatedAt: store.UTC(w.clock.Now()),
		}
		if _, err := db.NewInsert().Model(g).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewInsert().Model(&store.BerthGroupMember{
			ID: store.NewID(), MarinaID: w.marina, GroupID: g.ID, MapID: w.harbour.MapID, ResourceType: "berth", ResourceID: w.harbour.Berths[0],
			CreatedAt: store.UTC(w.clock.Now()),
		}).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		b := w.mustCreate(t, "k-exp-0001")
		claims := func() int {
			n, _ := db.NewSelect().TableExpr("online_quota_claims").Where("marina_id = ?", w.marina).Count(ctx)
			return int(n)
		}
		if claims() != 2 { // two nights on A1
			t.Fatalf("claims %d", claims())
		}
		w.setClock(t, w.clock.Now().Add(11*time.Minute))
		w.h.runJobs("contracts.expire")
		c := w.contractOf(t, str(b, "booking_id"))
		if c.Status != "expired" || claims() != 0 {
			t.Fatalf("after expiry %s claims %d", c.Status, claims())
		}
		var outcome string
		if err := db.NewSelect().TableExpr("inquiries").Column("outcome").Where("contract_id = ?", c.ID).Scan(ctx, &outcome); err != nil || outcome != "lost" {
			t.Fatalf("inquiry %q %v", outcome, err)
		}
		w.invariants(t)
	})
}

// TestCheckout_BrowserReturnDoesNotConfirm and TestCheckout_ConfirmOnlyFromWebhook: only the signed webhook
// confirms; a bad signature changes nothing; abandon releases a held booking.
func TestCheckout_ConfirmOnlyFromWebhook(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		b := w.mustCreate(t, "k-conf-0001")
		w.details(t, b, "c@example.test")
		w.pay(t, b)
		w.pub.expect(t, 200, "GET", w.P+"/bookings/"+str(b, "booking_id"), nil, "Porto-Booking-Token", str(b, "booking_token"))
		req := w.fake.LastCheckout()
		bad := ext.PaymentEvent{
			EventID: "evt_bad", Kind: ext.PaymentEventSucceeded, PaymentRef: "pi_bad", AmountCents: req.AmountCents,
			Metadata: map[string]string{"marina_id": w.marina, "intent_id": req.IntentID},
		}
		if code := w.postWebhook(t, bad, w.clock.Now().Add(-time.Hour)); code != 401 {
			t.Fatalf("stale signature %d", code)
		}
		if c := w.contractOf(t, str(b, "booking_id")); c.Status != "pending_payment" {
			t.Fatalf("confirmed without webhook: %s", c.Status)
		}
		w.succeed(t, "evt_conf", "pi_conf")
		if c := w.contractOf(t, str(b, "booking_id")); c.Status != "confirmed" {
			t.Fatalf("not confirmed: %s", c.Status)
		}
		// Abandon: a held booking is cancelled by its token holder.
		h := w.mustCreate(t, "k-conf-0002")
		w.pub.expect(t, 204, "DELETE", w.P+"/bookings/"+str(h, "booking_id"), nil, "Porto-Booking-Token", str(h, "booking_token"))
		if c := w.contractOf(t, str(h, "booking_id")); c.Status != "cancelled" {
			t.Fatalf("abandon %s", c.Status)
		}
		w.invariants(t)
	})
}

// TestCheckout_LatePaymentRecoveredOrRefunded (m8-plan §5.5): money arriving after the hold expired confirms the
// booking when its berth is still free, and is refunded in full when it is not.
func TestCheckout_LatePaymentRecoveredOrRefunded(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		b := w.mustCreate(t, "k-late-0001")
		w.details(t, b, "late@example.test")
		w.pay(t, b)
		w.setClock(t, w.clock.Now().Add(31*time.Minute))
		w.h.runJobs("contracts.expire")
		if c := w.contractOf(t, str(b, "booking_id")); c.Status != "expired" {
			t.Fatalf("not expired %s", c.Status)
		}
		w.succeed(t, "evt_late_ok", "pi_late_ok")
		if c := w.contractOf(t, str(b, "booking_id")); c.Status != "confirmed" {
			t.Fatalf("late payment not recovered: %s", c.Status)
		}
		w.invariants(t)

		// Second booking: its berth is resold at the desk after expiry → full automatic refund.
		b2 := w.mustCreate(t, "k-late-0002")
		w.details(t, b2, "late2@example.test")
		w.pay(t, b2)
		c2 := w.contractOf(t, str(b2, "booking_id"))
		rows, _ := store.AllocationsOfContracts(context.Background(), db.DB, w.scope, []string{c2.ID})
		berth := rows[0].ResourceID
		w.setClock(t, w.clock.Now().Add(31*time.Minute))
		w.h.runJobs("contracts.expire")
		vessel := w.vessel(t, "Scirocco", 1000, 350)
		cust := w.customer(t, "Verdi")
		w.owner.expect(t, 201, "POST", w.M+"/transits", map[string]any{
			"vessel_id": vessel, "customer_id": cust, "arrival": m8Arrival, "departure": m8Departure,
			"placements": []any{berthPlacement(berth, m8Arrival, "2027-06-11")},
		})
		w.succeed(t, "evt_late_ko", "pi_late_ko")
		if c := w.contractOf(t, str(b2, "booking_id")); c.Status != "expired" {
			t.Fatalf("late payment confirmed on a taken berth: %s", c.Status)
		}
		if len(w.fake.Refunds) != 1 {
			t.Fatalf("refunds %+v", w.fake.Refunds)
		}
		w.h.drainMail()
		if len(w.h.mailTo("late2@example.test", "booking_late_refund")) != 1 {
			t.Fatal("late refund email missing")
		}
		w.invariants(t)
	})
}
