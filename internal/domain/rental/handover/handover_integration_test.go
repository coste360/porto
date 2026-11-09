//go:build integration

package handover

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

func missingOf(t *testing.T, err error) []string {
	t.Helper()
	var ie *IncompleteError
	if !errors.As(err, &ie) {
		t.Fatalf("want handover_incomplete, got %v", err)
	}
	return ie.Missing
}

// TestAcceptance_M11_CheckOutCheckIn (server side, domain level) runs the worked example of m11-plan §8.2 with its
// exact numbers: walk-in renter, receipt mode, cash deposit 500.00, half day morning with FRIGO and SNORKEL, late
// return at 13:50 with fuel 5/8 and one damage of 180.00.
func TestHandover_CheckOutCheckIn(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		p := e.Owner
		e.at(t, "2027-06-12", 8, 35)
		e.openSession(t)
		b := e.booking(t, "", false)

		_, err := e.svc.CheckOut(ctx(), p, e.MarinaID, b.ID, b.Version, CheckOutInput{})
		got := missingOf(t, err)
		for _, m := range []string{MissingIdentity, MissingLicence, MissingContract, MissingDeposit, MissingChecklist, MissingFuel, MissingEngineHours, MissingPhotos} {
			if !slices.Contains(got, m) {
				t.Fatalf("missing %v lacks %s", got, m)
			}
		}
		e.prepareCheckOut(t, b)
		v, err := e.svc.GetHandover(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckout)
		if err != nil || len(v.Missing) != 0 || !v.ContractSigned || v.Identity.DocumentNumberMasked != "******5XY" {
			t.Fatalf("draft %+v %v", v, err)
		}

		qc, err := e.svc.QuickCash(ctx(), p, e.MarinaID, b.ID, QuickCashInput{Method: MethodCash})
		if err != nil {
			t.Fatalf("quick cash main: %v", err)
		}
		if len(qc.FiscalDocumentIDs) != 1 || len(qc.Charges) != 1 || qc.Charges[0].Total != 16500 || qc.Charges[0].Paid != 16500 {
			t.Fatalf("quick cash %+v", qc)
		}
		if _, err := e.svc.QuickCash(ctx(), p, e.MarinaID, b.ID, QuickCashInput{Method: MethodCash}); !errors.Is(err, ErrNothingToCollect) {
			t.Fatalf("second quick cash: %v", err)
		}
		cur := e.reload(t, b.ID)
		if _, err := e.svc.CheckOut(ctx(), p, e.MarinaID, b.ID, cur.Version+1, CheckOutInput{}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale If-Match: %v", err)
		}
		if _, err := e.svc.CheckOut(ctx(), p, e.MarinaID, b.ID, cur.Version, CheckOutInput{}); err != nil {
			t.Fatalf("check-out: %v", err)
		}
		if _, err := e.svc.SaveChecklist(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckout, ChecklistInput{ClientRef: "late-replay-1"}); !errors.Is(err, ErrHandoverLocked) {
			t.Fatalf("locked: %v", err)
		}

		e.at(t, "2027-06-12", 13, 50)
		e.checklist(t, b.ID, store.RentalPhaseCheckin, 5, 125_000)
		if _, err := e.svc.UploadPhoto(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckin, PhotoInput{Kind: store.RentalFileBoat, ContentType: "image/png", Body: photo(t, 90)}); err != nil {
			t.Fatal(err)
		}
		dm, err := e.svc.CreateDamage(ctx(), p, e.MarinaID, b.ID, DamageInput{Description: "Graffio sul tubolare", Estimate: 18000})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.UploadPhoto(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckin, PhotoInput{Kind: store.RentalFileDamage, DamageID: dm.ID, ContentType: "image/png", Body: photo(t, 91)}); err != nil {
			t.Fatal(err)
		}
		in := CheckInInput{ReturnedAt: e.Clock.Now(), DepositDecision: DecisionRetain, Retain: 18000}
		pv, err := e.svc.PreviewCheckIn(ctx(), p, e.MarinaID, b.ID, in)
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		if pv.LateMinutes != 35 || pv.LateFee != 6000 || pv.FuelLitres != 22_500 || pv.Fuel != 4725 || pv.ReturnTotal != 10725 ||
			pv.DepositProposal != 18000 || pv.DamageExcess != 0 || pv.Deposit != 50000 {
			t.Fatalf("preview %+v", pv)
		}
		if _, err := e.svc.CheckIn(ctx(), p, e.MarinaID, b.ID, cur.Version+1, CheckInInput{ReturnedAt: e.Clock.Now(), DepositDecision: DecisionRetain, Retain: 60000}); !errors.Is(err, ErrDepositExceeds) {
			t.Fatalf("retain above the deposit: %v", err)
		}
		if _, err := e.svc.CheckIn(ctx(), p, e.MarinaID, b.ID, cur.Version+1, in); err != nil {
			t.Fatalf("check-in: %v", err)
		}
		done := e.reload(t, b.ID)
		if done.Status != store.RentalStatusClosed || done.DepositStatus != store.RentalDepositStatusPartiallyRetained || done.DepositRetainedCents != 18000 {
			t.Fatalf("closed booking %+v", done)
		}
		ret, err := e.svc.QuickCash(ctx(), p, e.MarinaID, b.ID, QuickCashInput{Target: TargetReturn, Method: MethodCardPOS})
		if err != nil || len(ret.FiscalDocumentIDs) != 1 {
			t.Fatalf("quick cash return %+v %v", ret, err)
		}

		if got := e.sum(t, "SELECT COALESCE(SUM(total_cents),0) FROM charges WHERE origin_type = 'rental' AND origin_id = ?", b.ID); got != 27225 {
			t.Fatalf("rental charges %d, want 27225", got)
		}
		dep, err := store.GetPayment(ctx(), db.DB, e.scope, done.DepositPaymentID)
		if err != nil || dep.AllocatedCents != 18000 || dep.RefundedCents != 32000 || dep.Kind != store.PaymentKindDeposit {
			t.Fatalf("deposit payment %+v %v", dep, err)
		}
		cashIn := e.sum(t, "SELECT COALESCE(SUM(amount_cents),0) FROM payments WHERE method = 'cash' AND marina_id = ?", e.MarinaID)
		cashOut := e.sum(t, "SELECT COALESCE(SUM(amount_cents),0) FROM refunds WHERE method = 'cash' AND marina_id = ?", e.MarinaID)
		if cashIn-cashOut != 34500 {
			t.Fatalf("cash %d - %d, want 345.00", cashIn, cashOut)
		}
		if got := e.sum(t, "SELECT COALESCE(SUM(total_cents),0) FROM fiscal_documents WHERE marina_id = ?", e.MarinaID); got != 45225 {
			t.Fatalf("corrispettivi %d, want 452.25 (the deposit is never in them)", got)
		}
		unit, _ := store.GetRentalUnit(ctx(), db.DB, e.scope, e.unit.ID)
		if unit.EngineHoursMilli != 125_000 {
			t.Fatalf("engine hours %d", unit.EngineHoursMilli)
		}
		d2, _ := store.GetRentalDamage(ctx(), db.DB, e.scope, dm.ID)
		if d2.Status != store.RentalDamageCharged || d2.RetainedCents != 18000 || d2.ChargedCents != 0 {
			t.Fatalf("damage %+v", d2)
		}
		// INV-98: the deposit payment is matched only to its deposit_retention charge.
		if n := e.sum(t, `SELECT COUNT(*) FROM payment_matches m JOIN charges c ON c.id = m.charge_id
			WHERE m.payment_id = ? AND c.origin_type <> 'deposit_retention'`, dep.ID); n != 0 {
			t.Fatalf("deposit matched to %d non-retention charges", n)
		}
		e.invariants(t)
		if n := e.sum(t, "SELECT COUNT(*) FROM generated_documents WHERE kind = 'rental_handover' AND marina_id = ?", e.MarinaID); n != 2 {
			t.Fatalf("handover documents %d", n)
		}
	})
}

// TestHandover_OfflineReplay: a queued checklist replayed twice has one effect (INV-83), a replay outside the
// device-time window is refused, and identity photos are online only.
func TestHandover_OfflineReplay(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.at(t, "2027-06-12", 8, 0)
		b := e.booking(t, "", false)
		fuel := 8
		in := ChecklistInput{ClientRef: "offline-ref-0001", Items: []ChecklistEntry{{Key: "hull", OK: true}}, FuelEighths: &fuel, DeviceAt: e.Clock.Now().Add(-time.Hour)}
		q := reqctx.With(ctx(), &reqctx.Info{OfflineQueuedAt: e.Clock.Now()})
		v1, err := e.svc.SaveChecklist(q, e.Owner, e.MarinaID, b.ID, store.RentalPhaseCheckout, in)
		if err != nil {
			t.Fatal(err)
		}
		v2, err := e.svc.SaveChecklist(q, e.Owner, e.MarinaID, b.ID, store.RentalPhaseCheckout, in)
		if err != nil || v2.Handover.Version != v1.Handover.Version {
			t.Fatalf("replay: %v versions %d %d", err, v1.Handover.Version, v2.Handover.Version)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM audit_log WHERE action = 'rental_handover.save'"); n != 1 {
			t.Fatalf("audit rows %d, want 1", n)
		}
		old := in
		old.ClientRef, old.DeviceAt = "offline-ref-0002", e.Clock.Now().Add(-49*time.Hour)
		if _, err := e.svc.SaveChecklist(q, e.Owner, e.MarinaID, b.ID, store.RentalPhaseCheckout, old); !errors.Is(err, ErrOfflineTimeOutOfRange) {
			t.Fatalf("device time: %v", err)
		}
		if _, err := e.svc.UploadPhoto(q, e.Owner, e.MarinaID, b.ID, store.RentalPhaseCheckout, PhotoInput{Kind: store.RentalFileIDDocument, ContentType: "image/png", Body: photo(t, 1)}); err == nil {
			t.Fatal("identity photo accepted offline")
		}
		f1, err := e.svc.UploadPhoto(q, e.Owner, e.MarinaID, b.ID, store.RentalPhaseCheckout, PhotoInput{Kind: store.RentalFileBoat, ClientRef: "photo-ref-001", ContentType: "image/png", Body: photo(t, 2)})
		if err != nil {
			t.Fatal(err)
		}
		f2, err := e.svc.UploadPhoto(q, e.Owner, e.MarinaID, b.ID, store.RentalPhaseCheckout, PhotoInput{Kind: store.RentalFileBoat, ClientRef: "photo-ref-001", ContentType: "image/png", Body: photo(t, 2)})
		if err != nil || f1.ID != f2.ID {
			t.Fatalf("photo replay %v %v", f1, f2)
		}
		// A draft never changes the booking (INV-71).
		if cur := e.reload(t, b.ID); cur.Status != store.RentalStatusConfirmed || cur.Version != b.Version {
			t.Fatalf("booking changed by a draft: %+v", cur)
		}
	})
}

// TestHandover_Preauth: pre-authorisation through the fake provider: authorized webhook, check-in with a partial
// retention captured after commit, succeeded webhook recording and retaining the captured amount, then close.
func TestHandover_Preauth(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		p := e.Owner
		e.at(t, "2027-06-12", 8, 0)
		walk := e.booking(t, "", false)
		if _, err := e.svc.StartDeposit(ctx(), p, e.MarinaID, walk.ID, DepositInput{Mode: store.RentalDepositPreauth}); !errors.Is(err, ErrPreauthUnavailable) {
			t.Fatalf("no provider: %v", err)
		}
		e.enableFake(t)
		if _, err := e.svc.StartDeposit(ctx(), p, e.MarinaID, walk.ID, DepositInput{Mode: store.RentalDepositPreauth}); !errors.Is(err, ErrCustomerRequired) {
			t.Fatalf("walk-in preauth: %v", err)
		}
		b := e.booking(t, e.Customer, false)
		dv, err := e.svc.StartDeposit(ctx(), p, e.MarinaID, b.ID, DepositInput{Mode: store.RentalDepositPreauth})
		if err != nil || dv.Status != store.RentalDepositStatusPending || dv.CheckoutURL == "" {
			t.Fatalf("preauth %+v %v", dv, err)
		}
		in, _ := store.GetPaymentIntent(ctx(), db.DB, e.scope, dv.IntentID)
		e.webhook(t, ext.PaymentEvent{EventID: "ev-auth", Kind: ext.PaymentEventAuthorized, Metadata: map[string]string{"intent_id": in.ID, "marina_id": e.MarinaID}, SessionRef: in.ProviderSessionRef, PaymentRef: "pi_1", AmountCents: 50000})
		if cur := e.reload(t, b.ID); cur.DepositStatus != store.RentalDepositStatusAuthorized {
			t.Fatalf("after authorization %s", cur.DepositStatus)
		}
		e.prepareCheckOutNoDeposit(t, b)
		cur := e.reload(t, b.ID)
		if _, err := e.svc.CheckOut(ctx(), p, e.MarinaID, b.ID, cur.Version, CheckOutInput{}); err != nil {
			t.Fatalf("check-out: %v", err)
		}
		e.at(t, "2027-06-12", 12, 55)
		e.checklist(t, b.ID, store.RentalPhaseCheckin, 8, 121_000)
		if _, err := e.svc.UploadPhoto(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckin, PhotoInput{Kind: store.RentalFileBoat, ContentType: "image/png", Body: photo(t, 70)}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CreateDamage(ctx(), p, e.MarinaID, b.ID, DamageInput{Description: "Elica", Estimate: 12000}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CheckIn(ctx(), p, e.MarinaID, b.ID, cur.Version+1, CheckInInput{ReturnedAt: e.Clock.Now(), DepositDecision: DecisionRetain, Retain: 12000}); err != nil {
			t.Fatalf("check-in: %v", err)
		}
		cur = e.reload(t, b.ID)
		if cur.Status != store.RentalStatusReturned || cur.DepositStatus != store.RentalDepositStatusCapturePending {
			t.Fatalf("after check-in %s %s", cur.Status, cur.DepositStatus)
		}
		if len(e.fake.Captures) != 1 || e.fake.Captures[0].AmountCents != 12000 {
			t.Fatalf("captures %+v", e.fake.Captures)
		}
		e.webhook(t, ext.PaymentEvent{EventID: "ev-cap", Kind: ext.PaymentEventSucceeded, Metadata: map[string]string{"intent_id": in.ID, "marina_id": e.MarinaID}, SessionRef: in.ProviderSessionRef, PaymentRef: "pi_1", AmountCents: 12000})
		e.webhook(t, ext.PaymentEvent{EventID: "ev-cap-2", Kind: ext.PaymentEventSucceeded, Metadata: map[string]string{"intent_id": in.ID, "marina_id": e.MarinaID}, SessionRef: in.ProviderSessionRef, PaymentRef: "pi_1", AmountCents: 12000})
		cur = e.reload(t, b.ID)
		if cur.Status != store.RentalStatusClosed || cur.DepositStatus != store.RentalDepositStatusPartiallyRetained || cur.DepositRetainedCents != 12000 {
			t.Fatalf("after capture %+v", cur)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM payments WHERE kind = 'deposit' AND intent_id = ?", in.ID); n != 1 {
			t.Fatalf("deposit payments %d", n)
		}
		if n := e.sum(t, "SELECT COALESCE(SUM(total_cents),0) FROM charges WHERE origin_type = 'deposit_retention' AND origin_id = ?", b.ID); n != 12000 {
			t.Fatalf("retention %d", n)
		}
		e.invariants(t)
	})
}

// prepareCheckOutNoDeposit fills the check-out steps except the deposit.
func (e *env) prepareCheckOutNoDeposit(t testing.TB, b *store.RentalBooking) {
	t.Helper()
	p := e.Owner
	if _, err := e.svc.PutIdentity(ctx(), p, e.MarinaID, b.ID, IdentityInput{
		DocumentType: "passport", DocumentNumber: "YA0000001", DocumentExpiresOn: e.today(t).AddDays(1000),
		LicenceNumber: "LIC-1", LicenceExpiresOn: e.today(t).AddDays(1000),
	}); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{store.RentalFileIDDocument, store.RentalFileLicence, store.RentalFileBoat, store.RentalFileBoat} {
		if _, err := e.svc.UploadPhoto(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckout, PhotoInput{Kind: kind, ContentType: "image/png", Body: photo(t, uint8(30+i))}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.SignContract(ctx(), p, e.MarinaID, b.ID, SignInput{Driver: SignScreen, PNG: photo(t, 201)}); err != nil {
		t.Fatal(err)
	}
	e.checklist(t, b.ID, store.RentalPhaseCheckout, 8, 120_500)
}

func (e *env) today(_ testing.TB) calendar.Date {
	return e.svc.today(ctx(), e.DB.DB, e.scope)
}

// TestRental_DepositNeverRevenue (INV-98): a cash deposit released in full leaves no charge and no fiscal
// document; the cash session holds nothing of it after the refund.
func TestRental_DepositNeverRevenue(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		p := e.Owner
		e.at(t, "2027-06-12", 8, 0)
		e.openSession(t)
		b := e.booking(t, "", false)
		e.prepareCheckOut(t, b)
		cur := e.reload(t, b.ID)
		if _, err := e.svc.CheckOut(ctx(), p, e.MarinaID, b.ID, cur.Version, CheckOutInput{}); err != nil {
			t.Fatal(err)
		}
		e.at(t, "2027-06-12", 12, 50)
		e.checklist(t, b.ID, store.RentalPhaseCheckin, 8, 121_000)
		if _, err := e.svc.UploadPhoto(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckin, PhotoInput{Kind: store.RentalFileBoat, ContentType: "image/png", Body: photo(t, 77)}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CheckIn(ctx(), p, e.MarinaID, b.ID, cur.Version+1, CheckInInput{ReturnedAt: e.Clock.Now(), DepositDecision: DecisionRelease}); err != nil {
			t.Fatal(err)
		}
		done := e.reload(t, b.ID)
		if done.Status != store.RentalStatusClosed || done.DepositStatus != store.RentalDepositStatusReleased {
			t.Fatalf("%+v", done)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM charges WHERE origin_type = 'deposit_retention'"); n != 0 {
			t.Fatalf("retention charges %d", n)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM fiscal_documents"); n != 0 {
			t.Fatalf("fiscal documents %d (main charge unpaid, deposit released)", n)
		}
		dep, _ := store.GetPayment(ctx(), db.DB, e.scope, done.DepositPaymentID)
		if dep.RefundedCents != 50000 || dep.AllocatedCents != 0 {
			t.Fatalf("deposit %+v", dep)
		}
		e.invariants(t)
	})
}

// TestConcurrency_RentalCheckOutOnce (INV-99): 100 concurrent check-outs of one booking give one transition, one
// main charge and one deposit payment.
func TestConcurrency_RentalCheckOutOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.at(t, "2027-06-12", 8, 0)
		e.openSession(t)
		b := e.booking(t, "", false)
		e.prepareCheckOut(t, b)
		cur := e.reload(t, b.ID)
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := 0
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := e.svc.CheckOut(ctx(), e.Owner, e.MarinaID, b.ID, cur.Version, CheckOutInput{}); err == nil {
					mu.Lock()
					ok++
					mu.Unlock()
				} else if !errors.Is(err, ErrVersionMismatch) && !errors.Is(err, ErrInvalidTransition) && !store.IsSerializationFailure(err) {
					t.Errorf("unexpected: %v", err)
				}
			}()
		}
		wg.Wait()
		if ok != 1 {
			t.Fatalf("%d check-outs succeeded", ok)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM rental_booking_charges WHERE kind = 'main' AND booking_id = ?", b.ID); n != 1 {
			t.Fatalf("main charges %d", n)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM payments WHERE kind = 'deposit'"); n != 1 {
			t.Fatalf("deposit payments %d", n)
		}
	})
}

// TestConcurrency_RentalQuickCashOnce (INV-99, INV-52): 100 concurrent quick-cash calls on one booking give one main
// charge, matches within its total, and the losers find nothing to collect.
func TestConcurrency_RentalQuickCashOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		e.at(t, "2027-06-12", 8, 0)
		b := e.booking(t, "", false)
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := 0
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := e.svc.QuickCash(ctx(), e.Owner, e.MarinaID, b.ID, QuickCashInput{Method: MethodCardPOS})
				switch {
				case err == nil:
					mu.Lock()
					ok++
					mu.Unlock()
				case errors.Is(err, ErrNothingToCollect), store.IsSerializationFailure(err):
				default:
					var le *payment.LimitError
					if !errors.As(err, &le) {
						t.Errorf("unexpected: %v", err)
					}
				}
			}()
		}
		wg.Wait()
		if ok != 1 {
			t.Fatalf("%d quick cash succeeded", ok)
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM rental_booking_charges WHERE kind = 'main' AND booking_id = ?", b.ID); n != 1 {
			t.Fatalf("main charges %d", n)
		}
		if paid := e.sum(t, "SELECT COALESCE(SUM(paid_amount_cents),0) FROM charges WHERE origin_id = ?", b.ID); paid != 16500 {
			t.Fatalf("paid %d", paid)
		}
		e.invariants(t)
	})
}

// TestHandover_IdentityRetention: identity data and document photos are wiped after closed_at + retention, audited.
func TestHandover_IdentityRetention(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		p := e.Owner
		e.at(t, "2027-06-12", 8, 0)
		e.openSession(t)
		b := e.booking(t, "", false)
		e.prepareCheckOut(t, b)
		cur := e.reload(t, b.ID)
		if _, err := e.svc.CheckOut(ctx(), p, e.MarinaID, b.ID, cur.Version, CheckOutInput{}); err != nil {
			t.Fatal(err)
		}
		e.at(t, "2027-06-12", 12, 50)
		e.checklist(t, b.ID, store.RentalPhaseCheckin, 8, 121_000)
		if _, err := e.svc.UploadPhoto(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckin, PhotoInput{Kind: store.RentalFileBoat, ContentType: "image/png", Body: photo(t, 55)}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.CheckIn(ctx(), p, e.MarinaID, b.ID, cur.Version+1, CheckInInput{ReturnedAt: e.Clock.Now(), DepositDecision: DecisionRelease}); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.RunIdentityRetention(ctx()); err != nil {
			t.Fatal(err)
		}
		if h, _ := store.GetRentalHandover(ctx(), db.DB, e.scope, b.ID, store.RentalPhaseCheckout); h.IdentityEnc == "" {
			t.Fatal("purged before the retention")
		}
		e.Clock.Set(e.Clock.Now().Add(366 * 24 * time.Hour))
		if err := e.svc.RunIdentityRetention(ctx()); err != nil {
			t.Fatal(err)
		}
		h, _ := store.GetRentalHandover(ctx(), db.DB, e.scope, b.ID, store.RentalPhaseCheckout)
		if h.IdentityEnc != "" || h.IdentityPurgedAt.IsZero() {
			t.Fatalf("not purged %+v", h)
		}
		v, err := e.svc.GetHandover(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckout)
		if err != nil || v.Identity == nil || !v.Identity.Purged {
			t.Fatalf("view %+v %v", v, err)
		}
		for _, f := range v.Files {
			if f.Kind == store.RentalFileIDDocument || f.Kind == store.RentalFileLicence {
				t.Fatalf("identity photo kept: %+v", f)
			}
		}
		if n := e.sum(t, "SELECT COUNT(*) FROM audit_log WHERE action = 'rental_handover.identity_purge'"); n != 1 {
			t.Fatalf("audit %d", n)
		}
	})
}

// TestHandover_CrossMarina: another marina's booking is 404 on every handover call.
func TestHandover_CrossMarina(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		b := e.booking(t, "", false)
		_, _, other := e.OtherOrganization(t)
		if _, err := e.svc.GetHandover(ctx(), other, e.MarinaID, b.ID, store.RentalPhaseCheckout); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("get: %v", err)
		}
		if _, err := e.svc.QuickCash(ctx(), other, e.MarinaID, b.ID, QuickCashInput{Method: MethodCash}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("quick cash: %v", err)
		}
		if _, err := e.svc.CheckIn(ctx(), other, e.MarinaID, b.ID, 1, CheckInInput{DepositDecision: DecisionRelease}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("check-in: %v", err)
		}
		_ = money.Cents(0)
	})
}

// TestHandover_CheckInSameMinuteAsCheckOut: the phone sends the return time at minute precision, so a boat taken
// back in the minute of its check-out (08:35:40 out, 08:35 back) is accepted and stored at the check-out instant;
// an earlier minute is still refused.
func TestHandover_CheckInSameMinuteAsCheckOut(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		p := e.Owner
		out := e.at(t, "2027-06-12", 8, 35).Add(40 * time.Second)
		e.Clock.Set(out)
		e.openSession(t)
		b := e.booking(t, "", false)
		e.prepareCheckOut(t, b)
		if _, err := e.svc.QuickCash(ctx(), p, e.MarinaID, b.ID, QuickCashInput{Method: MethodCash}); err != nil {
			t.Fatalf("quick cash: %v", err)
		}
		cur := e.reload(t, b.ID)
		if _, err := e.svc.CheckOut(ctx(), p, e.MarinaID, b.ID, cur.Version, CheckOutInput{}); err != nil {
			t.Fatalf("check-out: %v", err)
		}
		e.Clock.Advance(10 * time.Second)
		e.checklist(t, b.ID, store.RentalPhaseCheckin, 8, 121_000)
		if _, err := e.svc.UploadPhoto(ctx(), p, e.MarinaID, b.ID, store.RentalPhaseCheckin, PhotoInput{Kind: store.RentalFileBoat, ContentType: "image/png", Body: photo(t, 92)}); err != nil {
			t.Fatal(err)
		}
		cur = e.reload(t, b.ID)
		minute := out.Truncate(time.Minute)
		_, err := e.svc.CheckIn(ctx(), p, e.MarinaID, b.ID, cur.Version, CheckInInput{ReturnedAt: minute.Add(-time.Minute), DepositDecision: DecisionRelease})
		var ve *auth.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("a minute before the check-out: %v", err)
		}
		if _, err := e.svc.CheckIn(ctx(), p, e.MarinaID, b.ID, cur.Version, CheckInInput{ReturnedAt: minute, DepositDecision: DecisionRelease}); err != nil {
			t.Fatalf("check-in in the check-out minute: %v", err)
		}
		done := e.reload(t, b.ID)
		if !done.ReturnedAt.Equal(done.CheckedOutAt) {
			t.Fatalf("returned_at %v, checked_out_at %v", done.ReturnedAt, done.CheckedOutAt)
		}
		e.invariants(t)
	})
}
