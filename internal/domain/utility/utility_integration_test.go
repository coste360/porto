//go:build integration

package utility

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// uenv is the backend of the utility tests: contracttest's harbour (berths A1..A4, transits priced), the charge
// service, a power item UTL-KWH priced 0.30 €/kWh by an active list and a power meter on berth A1.
type uenv struct {
	*contracttest.Env
	s       *Service
	charges *charge.Service
	scope   store.MarinaScope
	item    string
	meter   string
}

func ctxT() context.Context { return fixture.Ctx() }

func newUenv(t *testing.T, db *store.DB) *uenv {
	t.Helper()
	e := contracttest.New(t, db)
	ch := charge.NewService(charge.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: e.Files, Prices: e.Prices, Log: e.Log})
	u := &uenv{Env: e, charges: ch, scope: e.Scope(t)}
	u.s = NewService(Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Keys: e.Keys, Files: e.Files, Charges: ch,
		Contracts: e.Contracts, Prices: e.Prices,
	})
	ctx := ctxT()
	classes, err := e.Catalog.ListRevenueClasses(ctx, e.Owner, e.MarinaID, false)
	if err != nil || len(classes) == 0 {
		t.Fatalf("revenue classes: %v", err)
	}
	incl, bp := true, int64(2200)
	it, err := e.Catalog.CreateItem(ctx, e.Owner, e.MarinaID, catalog.ItemInput{
		Code: "UTL-KWH", Name: catalog.I18nText{It: "Energia"}, ItemType: "service", Unit: "kwh", PriceIncludesVAT: &incl,
		VATRateBP: &bp, RevenueClassID: classes[0].ID,
	})
	if err != nil {
		t.Fatalf("item: %v", err)
	}
	u.item = it.ID
	u.priceItem(t, it.ID, 30)
	set, err := u.s.GetSettings(ctx, e.Owner, e.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.s.UpdateSettings(ctx, e.Owner, e.MarinaID, int64(set.Version), SettingsInput{PowerItemID: &it.ID, Mode: store.UtilityModeWithInstallment}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	m, err := u.s.CreateMeter(ctx, e.Owner, e.MarinaID, MeterInput{
		Code: "A1-E", Kind: store.MeterPower, Unit: "kwh", ResourceType: store.ResourceBerth, ResourceID: e.Berths[0],
	})
	if err != nil {
		t.Fatalf("meter: %v", err)
	}
	u.meter = m.Row.ID
	return u
}

// priceItem writes an active list (2027) with one base rule pricing the item at unitCents per unit.
func (u *uenv) priceItem(t *testing.T, itemID string, unitCents int64) {
	t.Helper()
	now := store.UTC(u.Clock.Now())
	l := &store.PriceList{
		ID: store.NewID(), MarinaID: u.MarinaID, Code: "UTL2027-" + itemID[len(itemID)-8:], Name: store.JSON(`{"it":"Utenze"}`), Status: "draft",
		ValidFrom: contracttest.D("2027-01-01"), ValidTo: contracttest.D("2027-12-31"), ContractTypes: store.Strings{},
		Settings: store.JSON(`{}`), NextRuleSeq: 2, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	r := &store.PriceRule{
		ID: store.NewID(), MarinaID: u.MarinaID, PriceListID: l.ID, Seq: 1, Stage: "base", Name: "Energia", ItemID: itemID, Active: true,
		Body:      store.MustJSON(map[string]any{"stage": "base", "name": "Energia", "item_id": itemID, "basis": "metered", "unit_cents": unitCents}),
		CreatedAt: now, UpdatedAt: now,
	}
	u.Insert(t, l, r)
	if _, err := u.DB.NewUpdate().Table("price_lists").Set("status = 'active'").Set("content_sha256 = 'test'").
		Where("id = ?", l.ID).Exec(ctxT()); err != nil {
		t.Fatal(err)
	}
}

func (u *uenv) at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// read records a reading of the berth meter at instant at (the clock moves there first).
func (u *uenv) read(t *testing.T, at, value, ref string) *ReadingView {
	t.Helper()
	when := u.at(t, at)
	if when.After(u.Clock.Now()) {
		u.Clock.Set(when)
	}
	r, err := u.s.RecordReading(ctxT(), u.Owner, u.MarinaID, ReadingInput{MeterID: u.meter, Value: value, ReadAt: &when, ClientRef: ref})
	if err != nil {
		t.Fatalf("reading %s: %v", value, err)
	}
	return r
}

func (u *uenv) consumptions(t *testing.T) []store.UtilityConsumption {
	t.Helper()
	rows, err := store.ConsumptionsOfMeter(ctxT(), u.DB.DB, u.scope, u.meter)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (u *uenv) invariants(t *testing.T) {
	t.Helper()
	vs, err := store.CheckUtilityInvariants(ctxT(), u.DB.DB, u.MarinaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("invariant %s violated: %v %s", v.Query, v.IDs, v.Detail)
	}
}

// twoTransits books two transits on berth A1: 2027-06-02..04 and 2027-06-05..07 (nights).
func (u *uenv) twoTransits(t *testing.T) (string, string) {
	t.Helper()
	a := u.Transit(t, u.Berths[0], "2027-06-02", "2027-06-05", true)
	other := u.AddVessel(t, "Maestrale", 1000, 350, "monohull_sail")
	b, err := u.Contracts.CreateTransit(ctxT(), u.Owner, u.MarinaID, transitInput(u, other, "2027-06-05", "2027-06-08"))
	if err != nil {
		t.Fatalf("second transit: %v", err)
	}
	return a.Row.ID, b.Row.ID
}

func TestUtility_SplitAcrossOccupants(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		a, b := u.twoTransits(t)
		u.read(t, "2027-06-02T08:30:00Z", "1000", "r1")
		r2 := u.read(t, "2027-06-08T08:30:00Z", "3000.000", "r2")
		if r2.Delta == nil || *r2.Delta != 2_000_000 {
			t.Fatalf("delta %v", r2.Delta)
		}
		rows := u.consumptions(t)
		got := map[string]int64{}
		for _, r := range rows {
			if r.Status != store.ConsumptionPending {
				t.Errorf("status %s", r.Status)
			}
			got[r.ContractID] += r.QtyMilli
		}
		if len(rows) != 2 || got[a] != 1_000_000 || got[b] != 1_000_000 {
			t.Fatalf("shares %+v (%d rows)", got, len(rows))
		}
		u.invariants(t)
	})
}

func TestUtility_ReadingReplaySameKey(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		queued := u.Clock.Now().Add(-2 * time.Hour)
		in := ReadingInput{MeterID: u.meter, Value: "12,5", ClientRef: "op-1", QueuedAt: queued}
		r1, err := u.s.RecordReading(ctxT(), u.Owner, u.MarinaID, in)
		if err != nil {
			t.Fatal(err)
		}
		if !r1.Row.ReadAt.Equal(queued.UTC()) {
			t.Fatalf("read_at %s, want the queued instant %s", r1.Row.ReadAt, queued)
		}
		r2, err := u.s.RecordReading(ctxT(), u.Owner, u.MarinaID, in)
		if err != nil {
			t.Fatal(err)
		}
		if r1.Row.ID != r2.Row.ID || r2.Row.ValueMilli != 12_500 {
			t.Fatalf("replay gave %s (%d), want %s", r2.Row.ID, r2.Row.ValueMilli, r1.Row.ID)
		}
		n, err := db.NewSelect().TableExpr("meter_readings").Where("marina_id = ?", u.MarinaID).Count(ctxT())
		if err != nil || n != 1 {
			t.Fatalf("%d readings (%v)", n, err)
		}
		// A queued instant older than 48 hours falls back to now.
		old := u.Clock.Now().Add(-72 * time.Hour)
		r3, err := u.s.RecordReading(ctxT(), u.Owner, u.MarinaID, ReadingInput{MeterID: u.meter, Value: "13", ClientRef: "op-2", QueuedAt: old})
		if err != nil {
			t.Fatal(err)
		}
		if !r3.Row.ReadAt.Equal(u.Clock.Now().UTC()) {
			t.Fatalf("read_at %s", r3.Row.ReadAt)
		}
		// Bad values and foreign marinas.
		if _, err := u.s.RecordReading(ctxT(), u.Owner, u.MarinaID, ReadingInput{MeterID: u.meter, Value: "1.2345"}); err == nil {
			t.Fatal("4 decimals accepted")
		}
		_, otherMarina, other := u.OtherOrganization(t)
		if _, err := u.s.RecordReading(ctxT(), other, otherMarina, ReadingInput{MeterID: u.meter, Value: "1"}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign meter: %v", err)
		}
	})
}

func TestUtility_AnomalyConfirmAndVoid(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		u.Transit(t, u.Berths[0], "2027-06-02", "2027-06-05", true)
		u.read(t, "2027-06-02T08:30:00Z", "1000", "a1")
		bad := u.read(t, "2027-06-03T08:30:00Z", "900", "a2")
		if bad.Row.Status != store.ReadingAnomaly || AnomalyCodesOf(&bad.Row)[0] != "negative" {
			t.Fatalf("anomaly %s %v", bad.Row.Status, AnomalyCodesOf(&bad.Row))
		}
		for _, c := range u.consumptions(t) {
			if c.Status != store.ConsumptionAnomaly || c.QtyMilli != 0 {
				t.Fatalf("consumption %+v", c)
			}
		}
		v, err := u.s.VoidReading(ctxT(), u.Owner, u.MarinaID, bad.Row.ID, int64(bad.Row.Version), "typo")
		if err != nil || v.Row.Status != store.ReadingVoid {
			t.Fatalf("void: %v", err)
		}
		if n := len(u.consumptions(t)); n != 0 {
			t.Fatalf("%d consumptions after void", n)
		}
		hi := u.read(t, "2027-06-04T08:30:00Z", "1500", "a3")
		if hi.Row.Status != store.ReadingValid {
			t.Fatalf("status %s", hi.Row.Status)
		}
		if _, err := u.s.ConfirmReading(ctxT(), u.Owner, u.MarinaID, hi.Row.ID, int64(hi.Row.Version)); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("confirm valid: %v", err)
		}
		u.invariants(t)
	})
}

func TestUtility_TransitChargedAtDeparture(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		a, b := u.twoTransits(t)
		u.read(t, "2027-06-02T08:30:00Z", "1000", "c1")
		u.read(t, "2027-06-08T08:30:00Z", "3000", "c2")
		// On 2027-06-08 both transits are over: the job charges them.
		if err := u.s.RunCharge(ctxT()); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{a, b} {
			chs := u.utilityCharges(t, id)
			if len(chs) != 1 {
				t.Fatalf("contract %s: %d utility charges", id, len(chs))
			}
			// 1000 kWh × 0.30 € = 300.00 € VAT included.
			if chs[0].TotalCents != 30000 {
				t.Fatalf("total %d", chs[0].TotalCents)
			}
		}
		for _, c := range u.consumptions(t) {
			if c.Status != store.ConsumptionCharged || c.ChargeID == "" {
				t.Fatalf("consumption %+v", c)
			}
		}
		// Idempotent.
		if err := u.s.RunCharge(ctxT()); err != nil {
			t.Fatal(err)
		}
		if len(u.utilityCharges(t, a)) != 1 {
			t.Fatal("second run charged again")
		}
		u.invariants(t)
	})
}

func (u *uenv) utilityCharges(t *testing.T, contractID string) []store.Charge {
	t.Helper()
	var out []store.Charge
	if err := u.DB.NewSelect().Model(&out).Where("marina_id = ?", u.MarinaID).Where("contract_id = ?", contractID).
		Where("origin_type = ?", store.ChargeOriginUtility).OrderExpr("created_at, id").Scan(ctxT()); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUtility_ChargeWithInstallment(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		cid, _ := u.twoTransits(t)
		_ = cid
		u.read(t, "2027-06-02T08:30:00Z", "0", "i1")
		u.read(t, "2027-06-04T08:30:00Z", "10", "i2")
		// The transit is still running on 2027-06-04: the job leaves it pending.
		res, err := u.s.Run(ctxT(), u.Owner, u.MarinaID, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Charges) != 0 {
			t.Fatalf("charged too early: %+v", res.Charges)
		}
		// A manual run on the contract charges now, with one line per consumption.
		res, err = u.s.Run(ctxT(), u.Owner, u.MarinaID, cid)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Charges) != 1 || res.Charges[0].Lines != 1 || res.Charges[0].TotalCents != 300 {
			t.Fatalf("run %+v", res)
		}
		u.invariants(t)
	})
}

func TestUtility_BackdatedReadingFrozenWhenCharged(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		a, b := u.twoTransits(t)
		u.read(t, "2027-06-02T08:30:00Z", "1000", "b1")
		u.read(t, "2027-06-08T08:30:00Z", "3000", "b2")
		for _, id := range []string{a, b} {
			if _, err := u.s.Run(ctxT(), u.Owner, u.MarinaID, id); err != nil {
				t.Fatal(err)
			}
		}
		before := u.consumptions(t)
		// The late reading taken at the occupant change (2027-06-05) shows the first boat used 1800 kWh.
		late := u.read(t, "2027-06-05T08:00:00Z", "2800", "b3")
		if late.Row.Status != store.ReadingAnomaly || !contains(AnomalyCodesOf(&late.Row), "out_of_order") {
			t.Fatalf("late reading %s %v", late.Row.Status, AnomalyCodesOf(&late.Row))
		}
		if after := u.consumptions(t); len(after) != len(before) {
			t.Fatalf("frozen interval changed: %d → %d rows", len(before), len(after))
		}
		if _, err := u.s.VoidReading(ctxT(), u.Owner, u.MarinaID, u.consumptions(t)[0].ToReadingID, 1, "x"); !errors.Is(err, ErrReadingCharged) {
			t.Fatalf("void charged: %v", err)
		}
		// Confirming it reallocates the charged interval: +800 for the first, −800 for the second.
		if _, err := u.s.ConfirmReading(ctxT(), u.Owner, u.MarinaID, late.Row.ID, int64(late.Row.Version)); err != nil {
			t.Fatal(err)
		}
		deltas := map[string]int64{}
		for _, c := range u.consumptions(t) {
			if c.Status == store.ConsumptionPending {
				deltas[c.ContractID] += c.QtyMilli
			}
		}
		if deltas[a] != 800_000 || deltas[b] != -800_000 {
			t.Fatalf("deltas %+v", deltas)
		}
		// TestUtility_AdjustmentAfterExport: the next run charges both deltas as adjustments of the first charges.
		for _, id := range []string{a, b} {
			res, err := u.s.Run(ctxT(), u.Owner, u.MarinaID, id)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Charges) != 1 || !res.Charges[0].Adjustment {
				t.Fatalf("contract %s: %+v", id, res)
			}
			chs := u.utilityCharges(t, id)
			if len(chs) != 2 || chs[1].CorrectsChargeID != chs[0].ID {
				t.Fatalf("contract %s: adjustment not linked: %+v", id, chs)
			}
		}
		u.invariants(t)
	})
}

func TestUtility_ExcludeAndAssign(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		cid := u.Transit(t, u.Berths[0], "2027-06-03", "2027-06-04", true).Row.ID
		u.read(t, "2027-06-02T08:30:00Z", "0", "x1")
		u.read(t, "2027-06-04T08:30:00Z", "20", "x2")
		var un, pend *store.UtilityConsumption
		for _, c := range u.consumptions(t) {
			switch c.Status {
			case store.ConsumptionUnassigned:
				un = &c
			case store.ConsumptionPending:
				pend = &c
			}
		}
		if un == nil || pend == nil || un.QtyMilli != 10_000 {
			t.Fatalf("rows %+v", u.consumptions(t))
		}
		got, err := u.s.AssignConsumption(ctxT(), u.Owner, u.MarinaID, un.ID, int64(un.Version), cid)
		if err != nil || got.Row.Status != store.ConsumptionPending || got.Row.ContractID != cid {
			t.Fatalf("assign: %v", err)
		}
		ex, err := u.s.ExcludeConsumption(ctxT(), u.Owner, u.MarinaID, pend.ID, int64(pend.Version), "guasto")
		if err != nil || ex.Row.Status != store.ConsumptionExcluded {
			t.Fatalf("exclude: %v", err)
		}
		if _, err := u.s.ExcludeConsumption(ctxT(), u.Owner, u.MarinaID, pend.ID, int64(ex.Row.Version), "again"); !errors.Is(err, ErrConsumptionFrozen) {
			t.Fatalf("exclude excluded: %v", err)
		}
		res, err := u.s.Run(ctxT(), u.Owner, u.MarinaID, cid)
		if err != nil || len(res.Charges) != 1 || res.Charges[0].TotalCents != 300 {
			t.Fatalf("run %+v %v", res, err)
		}
		u.invariants(t)
	})
}

// TestConcurrency_UtilityChargeOnce: 100 parallel runs (manual and job) on one contract: each consumption is in
// exactly one charge (INV-90).
func TestConcurrency_UtilityChargeOnce(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		a, _ := u.twoTransits(t)
		u.read(t, "2027-06-02T08:30:00Z", "0", "k1")
		u.read(t, "2027-06-08T08:30:00Z", "100", "k2")
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for i := range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				if i%4 == 0 {
					err = u.s.RunCharge(ctxT())
				} else {
					_, err = u.s.Run(ctxT(), u.Owner, u.MarinaID, a)
				}
				if err != nil {
					errs <- err
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("run: %v", err)
		}
		if n := len(u.utilityCharges(t, a)); n != 1 {
			t.Fatalf("%d charges for one contract", n)
		}
		u.invariants(t)
	})
}

func TestUtility_MetersAndSettings(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		ctx := ctxT()
		if _, err := u.s.CreateMeter(ctx, u.Owner, u.MarinaID, MeterInput{Code: "A1-E", Kind: store.MeterPower, Unit: "kwh"}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("duplicate code: %v", err)
		}
		if _, err := u.s.CreateMeter(ctx, u.Owner, u.MarinaID, MeterInput{Code: "H1", Kind: store.MeterHours, Unit: "kwh"}); err == nil {
			t.Fatal("wrong unit accepted")
		}
		if _, err := u.s.CreateMeter(ctx, u.Owner, u.MarinaID, MeterInput{
			Code: "W1", Kind: store.MeterWater, Unit: "m3", ResourceType: store.ResourceBerth, ResourceID: store.NewID(),
		}); !errors.Is(err, ErrElementInvalid) {
			t.Fatalf("unknown berth: %v", err)
		}
		m, err := u.s.GetMeter(ctx, u.Owner, u.MarinaID, u.meter)
		if err != nil {
			t.Fatal(err)
		}
		up, err := u.s.UpdateMeter(ctx, u.Owner, u.MarinaID, u.meter, int64(m.Row.Version), MeterPatch{Set: map[string]any{
			PatchSerial: "SN-1", PatchMultiplierMilli: ptr64(2000),
		}})
		if err != nil || up.Row.Serial != "SN-1" || up.Row.MultiplierMilli != 2000 {
			t.Fatalf("update: %v %+v", err, up)
		}
		if _, err := u.s.UpdateMeter(ctx, u.Owner, u.MarinaID, u.meter, int64(m.Row.Version), MeterPatch{}); !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("stale version: %v", err)
		}
		list, _, err := u.s.ListMeters(ctx, u.Owner, u.MarinaID, MeterFilter{Route: "dock", Q: "a1"})
		if err != nil || len(list) != 1 {
			t.Fatalf("dock list %d %v", len(list), err)
		}
		dock := u.AddUser(t, "dock@porto.test", auth.RoleDock, u.OrgID, u.MarinaID)
		if _, err := u.s.CreateMeter(ctx, dock, u.MarinaID, MeterInput{Code: "Z", Kind: store.MeterPower, Unit: "kwh"}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("dock creates meter: %v", err)
		}
		if _, err := u.s.RecordReading(ctx, dock, u.MarinaID, ReadingInput{MeterID: u.meter, Value: "1"}); err != nil {
			t.Fatalf("dock reading: %v", err)
		}
	})
}

func ptr64(v int64) *int64 { return &v }

func transitInput(u *uenv, vessel, arrival, departure string) contract.TransitInput {
	return contract.TransitInput{
		VesselID: vessel, CustomerID: u.Customer, Arrival: contracttest.D(arrival), Departure: contracttest.D(departure),
		Placements: []contract.Segment{contracttest.BerthSeg(u.Berths[0], arrival, contracttest.D(departure).AddDays(-1).String())},
		Confirm:    &contract.ManualConfirmation{Reference: "cassa"},
	}
}

func (u *uenv) billings(t *testing.T) []store.UtilityBilling {
	t.Helper()
	var out []store.UtilityBilling
	if err := u.DB.NewSelect().Model(&out).Where("marina_id = ?", u.MarinaID).OrderExpr("id").Scan(ctxT()); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestUtility_ZeroTotalNoOrphanBilling: a metered item priced 0 gives a zero total: no charge, no billing row, the
// consumptions are closed (zero_amount) and the hourly job stays idempotent.
func TestUtility_ZeroTotalNoOrphanBilling(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		classes, err := u.Catalog.ListRevenueClasses(ctxT(), u.Owner, u.MarinaID, false)
		if err != nil {
			t.Fatal(err)
		}
		incl := true
		free, err := u.Catalog.CreateItem(ctxT(), u.Owner, u.MarinaID, catalog.ItemInput{
			Code: "UTL-FREE", Name: catalog.I18nText{It: "Energia omaggio"}, ItemType: "service", Unit: "kwh", PriceIncludesVAT: &incl,
			RevenueClassID: classes[0].ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		u.priceItem(t, free.ID, 0)
		set, err := u.s.GetSettings(ctxT(), u.Owner, u.MarinaID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := u.s.UpdateSettings(ctxT(), u.Owner, u.MarinaID, int64(set.Version), SettingsInput{PowerItemID: &free.ID, Mode: store.UtilityModeWithInstallment}); err != nil {
			t.Fatal(err)
		}
		a, _ := u.twoTransits(t)
		u.read(t, "2027-06-02T08:30:00Z", "0", "z1")
		u.read(t, "2027-06-08T08:30:00Z", "100", "z2")
		for range 3 {
			if err := u.s.RunCharge(ctxT()); err != nil {
				t.Fatal(err)
			}
		}
		if n := len(u.billings(t)); n != 0 {
			t.Fatalf("%d orphan billing rows", n)
		}
		if n := len(u.utilityCharges(t, a)); n != 0 {
			t.Fatalf("%d charges", n)
		}
		for _, c := range u.consumptions(t) {
			if c.ContractID == a && (c.Status != store.ConsumptionExcluded || c.ExcludeReason != "zero_amount") {
				t.Fatalf("consumption %s %s", c.Status, c.ExcludeReason)
			}
		}
		u.invariants(t)
	})
}

// TestUtility_CancelledChargeReopensConsumptions (INV-90): cancelling a utility charge puts its consumptions back to
// pending in the same transaction; Q21 stays clean and the next run charges them again, once.
func TestUtility_CancelledChargeReopensConsumptions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		a, _ := u.twoTransits(t)
		u.read(t, "2027-06-02T08:30:00Z", "0", "q1")
		u.read(t, "2027-06-08T08:30:00Z", "100", "q2")
		if _, err := u.s.Run(ctxT(), u.Owner, u.MarinaID, a); err != nil {
			t.Fatal(err)
		}
		chs := u.utilityCharges(t, a)
		if len(chs) != 1 {
			t.Fatalf("%d charges", len(chs))
		}
		if _, err := u.charges.Cancel(ctxT(), u.Owner, u.MarinaID, chs[0].ID, chs[0].Version, "errore"); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		for _, c := range u.consumptions(t) {
			if c.ContractID == a && (c.Status != store.ConsumptionPending || c.ChargeID != "" || c.BillingID != "") {
				t.Fatalf("consumption not reopened: %+v", c)
			}
		}
		u.invariants(t)
		if _, err := u.s.Run(ctxT(), u.Owner, u.MarinaID, a); err != nil {
			t.Fatal(err)
		}
		live := 0
		for _, c := range u.utilityCharges(t, a) {
			if c.PaymentStatus != store.ChargeCancelled {
				live++
			}
		}
		if live != 1 {
			t.Fatalf("%d live utility charges", live)
		}
		u.invariants(t)
	})
}

// TestConcurrency_OfflineReplay_Reading (INV-83): 100 parallel replays of one queued reading (same client_ref):
// one row, the same reading in every response, one audit row and one event.
func TestConcurrency_OfflineReplay_Reading(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		u := newUenv(t, db)
		in := ReadingInput{MeterID: u.meter, Value: "42", ClientRef: "op-par", QueuedAt: u.Clock.Now().Add(-time.Hour)}
		var wg sync.WaitGroup
		var mu sync.Mutex
		ids := map[string]int{}
		var errs []error
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := u.s.RecordReading(ctxT(), u.Owner, u.MarinaID, in)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, err)
					return
				}
				ids[r.Row.ID]++
			}()
		}
		wg.Wait()
		if len(errs) > 0 {
			t.Fatalf("replay: %v", errs[0])
		}
		if len(ids) != 1 {
			t.Fatalf("responses name %d readings", len(ids))
		}
		n, err := db.NewSelect().TableExpr("meter_readings").Where("marina_id = ?", u.MarinaID).Count(ctxT())
		if err != nil || n != 1 {
			t.Fatalf("%d readings (%v)", n, err)
		}
		if a := u.AuditCount(t, "meter_reading.record"); a != 1 {
			t.Fatalf("%d audit rows", a)
		}
		recorded := 0
		for _, ev := range u.Events(t, "meter.reading_recorded") {
			var pl struct {
				Entity string `json:"entity"`
			}
			if json.Unmarshal(ev.Payload, &pl) == nil && pl.Entity == "reading" {
				recorded++
			}
		}
		if recorded != 1 {
			t.Fatalf("%d reading events", recorded)
		}
	})
}
