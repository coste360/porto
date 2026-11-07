//go:build integration

package contract

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// tally counts the audit rows of an entity and the outbox events by type.
type tally struct {
	audit  int
	events map[string]int
}

func (e *tenv) tally(t *testing.T, entityID string) tally {
	t.Helper()
	ctx := context.Background()
	n, err := e.DB.NewSelect().Model((*store.AuditEntry)(nil)).Where("entity_id = ?", entityID).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows []store.OutboxEvent
	if err := e.DB.NewSelect().Model(&rows).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	out := tally{audit: int(n), events: map[string]int{}}
	for _, r := range rows {
		out.events[r.Type]++
	}
	return out
}

// expectTransition asserts that between two tallies exactly one audit row of the entity and the events of the
// transition row were written.
func expectTransition(t *testing.T, name string, before, after tally, tr Transition) {
	t.Helper()
	if after.audit-before.audit != 1 {
		t.Errorf("%s: %d audit rows, want 1", name, after.audit-before.audit)
	}
	for _, typ := range tr.Events {
		if typ == event.OccupancyHoldExpired {
			continue // only when holds actually expired
		}
		if after.events[typ]-before.events[typ] < 1 {
			t.Errorf("%s: no %s event", name, typ)
		}
	}
}

func next(t *testing.T, table []Transition, from Status, a Action) Transition {
	t.Helper()
	tr, err := Next(table, from, a)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// TestContract_StateChangeWritesAuditAndOutbox is INV-30: every transition of m3-plan §4.1–§4.3 writes one audit
// row and its outbox events in the same transaction; a failure after the state write leaves neither.
func TestContract_StateChangeWritesAuditAndOutbox(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		o := e.Owner

		// Transit: create held, confirm, arrive (hook), change stay, update, depart (hook).
		c := e.transit(t, o, e.berths[0], "2027-06-02", "2027-06-05", false)
		id := c.Row.ID
		if e.tally(t, id).audit != 1 {
			t.Fatal("create: audit")
		}
		b := e.tally(t, id)
		c, err := e.svc.ConfirmTransit(ctx, o, e.MarinaID, id, c.Row.Version, ManualConfirmation{})
		if err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "confirm", b, e.tally(t, id), next(t, TransitTransitions, StatusHeld, ActionConfirm))
		b = e.tally(t, id)
		dep := d("2027-06-06")
		if c, _, err = e.svc.ChangeStay(ctx, o, e.MarinaID, id, c.Row.Version, StayChange{Departure: &dep}); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "stay", b, e.tally(t, id), next(t, TransitTransitions, StatusConfirmed, ActionChangeStay))
		b = e.tally(t, id)
		notes := "ormeggio di poppa"
		if _, err = e.svc.UpdateTransit(ctx, o, e.MarinaID, id, c.Row.Version, TransitPatch{Notes: &notes}); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "update", b, e.tally(t, id), next(t, TransitTransitions, StatusConfirmed, ActionUpdate))
		scope := e.scope(t)
		b = e.tally(t, id)
		err = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			_, err := e.svc.OnArrivalTx(ctx, tx, scope, o, id)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "arrive", b, e.tally(t, id), next(t, TransitTransitions, StatusConfirmed, ActionArrive))
		b = e.tally(t, id)
		err = db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			_, err := e.svc.OnDepartureTx(ctx, tx, scope, o, id, d("2027-06-04"))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "depart", b, e.tally(t, id), next(t, TransitTransitions, StatusArrived, ActionDepart))

		// Transit cancel and no-show.
		c1 := e.transit(t, o, e.berths[1], "2027-06-02", "2027-06-03", false)
		b = e.tally(t, c1.Row.ID)
		if _, err := e.svc.CancelTransit(ctx, o, e.MarinaID, c1.Row.ID, c1.Row.Version, ""); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "cancel", b, e.tally(t, c1.Row.ID), next(t, TransitTransitions, StatusHeld, ActionCancel))
		c2 := e.transit(t, o, e.berths[1], "2027-06-02", "2027-06-03", true)
		b = e.tally(t, c2.Row.ID)
		if _, err := e.svc.MarkNoShow(ctx, o, e.MarinaID, c2.Row.ID, c2.Row.Version); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "no_show", b, e.tally(t, c2.Row.ID), next(t, TransitTransitions, StatusConfirmed, ActionNoShow))

		// Mooring: create draft, update, activate, terminate; another draft cancelled.
		m := e.mooring(t, o, e.berths[2], "2027-06-02", false)
		if e.tally(t, m.Row.ID).audit != 1 {
			t.Fatal("mooring create audit")
		}
		b = e.tally(t, m.Row.ID)
		if m, err = e.svc.UpdateContract(ctx, o, e.MarinaID, m.Row.ID, m.Row.Version, MooringPatch{Notes: &notes}); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "mooring update", b, e.tally(t, m.Row.ID), next(t, MooringTransitions, StatusDraft, ActionUpdate))
		b = e.tally(t, m.Row.ID)
		if m, err = e.svc.ActivateContract(ctx, o, e.MarinaID, m.Row.ID, m.Row.Version); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "activate", b, e.tally(t, m.Row.ID), next(t, MooringTransitions, StatusDraft, ActionActivate))
		b = e.tally(t, m.Row.ID)
		if _, err = e.svc.TerminateContract(ctx, o, e.MarinaID, m.Row.ID, m.Row.Version, d("2027-08-01"), "vendita barca"); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "terminate", b, e.tally(t, m.Row.ID), next(t, MooringTransitions, StatusActive, ActionTerminate))
		m2 := e.mooring(t, o, e.berths[3], "2027-06-02", false)
		b = e.tally(t, m2.Row.ID)
		if _, err := e.svc.CancelContract(ctx, o, e.MarinaID, m2.Row.ID, m2.Row.Version, ""); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "mooring cancel", b, e.tally(t, m2.Row.ID), next(t, MooringTransitions, StatusDraft, ActionCancel))

		// Quote: create, send, decline; another accepted.
		q := e.quote(t, o, KindTransit, "", "2027-07-01", "2027-07-03", true, e.berths[3])
		if e.tally(t, q.Row.ID).audit != 1 {
			t.Fatal("quote create audit")
		}
		b = e.tally(t, q.Row.ID)
		if q, err = e.svc.MarkQuoteSent(ctx, o, e.MarinaID, q.Row.ID, q.Row.Version); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "send", b, e.tally(t, q.Row.ID), next(t, QuoteTransitions, QuoteDraft, ActionSend))
		b = e.tally(t, q.Row.ID)
		if _, err = e.svc.DeclineQuote(ctx, o, e.MarinaID, q.Row.ID, q.Row.Version, "troppo caro"); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "decline", b, e.tally(t, q.Row.ID), next(t, QuoteTransitions, QuoteSent, ActionDecline))
		q2 := e.quote(t, o, KindTransit, "", "2027-07-01", "2027-07-03", true, e.berths[3])
		b = e.tally(t, q2.Row.ID)
		if _, _, err = e.svc.AcceptQuote(ctx, o, e.MarinaID, q2.Row.ID, q2.Row.Version, AcceptInput{OptionID: q2.Options[0].Row.ID}); err != nil {
			t.Fatal(err)
		}
		expectTransition(t, "accept", b, e.tally(t, q2.Row.ID), next(t, QuoteTransitions, QuoteDraft, ActionAccept))

		// A failure after the state write leaves neither the change, nor the audit row, nor the event.
		c3 := e.transit(t, o, e.berths[0], "2027-07-10", "2027-07-12", false)
		b = e.tally(t, c3.Row.ID)
		boom := errors.New("boom")
		e.svc.beforeCommit = func() error { return boom }
		if _, err := e.svc.ConfirmTransit(ctx, o, e.MarinaID, c3.Row.ID, c3.Row.Version, ManualConfirmation{}); !errors.Is(err, boom) {
			t.Fatalf("forced failure: %v", err)
		}
		e.svc.beforeCommit = nil
		a := e.tally(t, c3.Row.ID)
		if a.audit != b.audit || a.events[event.TransitConfirmed] != b.events[event.TransitConfirmed] {
			t.Fatalf("rolled back confirm left audit %d→%d events %d→%d", b.audit, a.audit, b.events[event.TransitConfirmed], a.events[event.TransitConfirmed])
		}
		got, _ := e.svc.GetContract(ctx, o, e.MarinaID, c3.Row.ID)
		if got.Row.Status != string(StatusHeld) || got.Allocations[0].Kind != store.AllocKindHold {
			t.Fatalf("rolled back confirm changed state: %s %s", got.Row.Status, got.Allocations[0].Kind)
		}
		e.invariants(t)
	})
}

// mooring creates an annual mooring contract on a berth from starts (draft or active).
func (e *tenv) mooring(t testing.TB, p *auth.Principal, berth, starts string, activate bool) *Detail {
	t.Helper()
	s := d(starts)
	ends, _ := DefaultEndsOn(TermAnnual, s)
	c, err := e.svc.CreateContract(fixture.Ctx(), p, e.MarinaID, MooringInput{
		Term: TermAnnual, CustomerID: e.cust, VesselID: e.vessel, StartsOn: s,
		Placements: []Segment{berthSeg(berth, starts, ends.AddDays(-1).String())}, Activate: activate,
	})
	if err != nil {
		t.Fatalf("create mooring: %v", err)
	}
	return c
}

// quote creates a quote with one option per berth (segments of the whole stay), holding when hold is true.
func (e *tenv) quote(t testing.TB, p *auth.Principal, kind, term, starts, ends string, hold bool, berths ...string) *QuoteDetail {
	t.Helper()
	in := QuoteInput{Kind: kind, Term: term, CustomerID: e.cust, VesselID: e.vessel, StartsOn: d(starts), EndsOn: d(ends)}
	for _, b := range berths {
		in.Options = append(in.Options, OptionInput{Hold: hold, Placements: []Segment{berthSeg(b, starts, d(ends).AddDays(-1).String())}})
	}
	q, err := e.svc.CreateQuote(fixture.Ctx(), p, e.MarinaID, in)
	if err != nil {
		t.Fatalf("create quote: %v", err)
	}
	return q
}

func TestContract_TransitionsRefusedByServices(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		o := e.Owner
		active := e.mooring(t, o, e.berths[0], "2027-06-02", true)
		if active.Row.Status != string(StatusActive) || active.Allocations[0].Kind != store.AllocKindContract {
			t.Fatalf("active: %s %s", active.Row.Status, active.Allocations[0].Kind)
		}
		v := active.Row.Version
		checks := []struct {
			name string
			call func() error
		}{
			{"activate active", func() error { _, err := e.svc.ActivateContract(ctx, o, e.MarinaID, active.Row.ID, v); return err }},
			{"cancel active", func() error { _, err := e.svc.CancelContract(ctx, o, e.MarinaID, active.Row.ID, v, ""); return err }},
		}
		held := e.transit(t, o, e.berths[1], "2027-06-10", "2027-06-12", false)
		checks = append(checks,
			struct {
				name string
				call func() error
			}{"no-show held", func() error {
				_, err := e.svc.MarkNoShow(ctx, o, e.MarinaID, held.Row.ID, held.Row.Version)
				return err
			}},
		)
		confirmed := e.transit(t, o, e.berths[2], "2027-06-10", "2027-06-12", true)
		checks = append(checks,
			struct {
				name string
				call func() error
			}{"no-show before arrival", func() error {
				_, err := e.svc.MarkNoShow(ctx, o, e.MarinaID, confirmed.Row.ID, confirmed.Row.Version)
				return err
			}},
		)
		for _, c := range checks {
			if err := c.call(); !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("%s: %v", c.name, err)
			}
		}
		// A transit id on the contract routes and vice versa is not found.
		if _, err := e.svc.ActivateContract(ctx, o, e.MarinaID, held.Row.ID, held.Row.Version); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("activate transit: %v", err)
		}
		if _, err := e.svc.ConfirmTransit(ctx, o, e.MarinaID, active.Row.ID, v, ManualConfirmation{}); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("confirm mooring: %v", err)
		}
		// Active contracts accept notes only.
		bc := e.cust
		if _, err := e.svc.UpdateContract(ctx, o, e.MarinaID, active.Row.ID, v, MooringPatch{BillingCustomerID: &bc}); err == nil {
			t.Error("billing customer of an active contract changed")
		}
	})
}

// TestContract_TransitConfirmOnlyManualDriver is the runtime half of INV-09: confirmed transits carry the
// manual driver's fields; nothing else confirms (creation without confirm, arrival of a held transit).
func TestContract_TransitConfirmOnlyManualDriver(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		held := e.transit(t, e.Owner, e.berths[0], "2027-06-02", "2027-06-04", false)
		scope := e.scope(t)
		// An arrival of a held transit leaves it held.
		err := db.RunInTx(ctx, func(ctx context.Context, tx bunTx) error {
			_, err := e.svc.OnArrivalTx(ctx, tx, scope, e.Owner, held.Row.ID)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, held.Row.ID)
		if got.Row.Status != string(StatusHeld) || !got.Row.ConfirmedAt.IsZero() || got.Row.ConfirmationSource != "" {
			t.Fatalf("held after arrival: %+v", got.Row)
		}
		c, err := e.svc.ConfirmTransit(ctx, e.Owner, e.MarinaID, held.Row.ID, got.Row.Version, ManualConfirmation{Reference: "POS 3"})
		if err != nil {
			t.Fatal(err)
		}
		if c.Row.ConfirmationSource != SourceManual || c.Row.ConfirmedByUserID != e.OwnerID || c.Row.ConfirmedAt.IsZero() || c.Row.ConfirmationRef != "POS 3" {
			t.Fatalf("manual driver fields: %+v", c.Row)
		}
		evs := e.Events(t, event.TransitConfirmed)
		if len(evs) != 1 {
			t.Fatalf("events %d", len(evs))
		}
		if p := string(evs[0].Payload); p == "" || strings.Contains(p, "POS 3") || strings.Contains(p, "confirmation_ref") {
			t.Fatalf("event payload leaks the reference: %s", evs[0].Payload)
		}
	})
}

// TestContract_HoldTransitHasAllocation is INV-08 for the desk and api channels: a held or confirmed transit has
// active allocations covering its nights, and a transit whose hold cannot be written does not exist.
func TestContract_HoldTransitHasAllocation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		for i, ch := range []string{ChannelDesk, ChannelAPI} {
			in := TransitInput{
				VesselID: e.vessel, CustomerID: e.cust, Arrival: d("2027-06-10"), Departure: d("2027-06-13"), Channel: ch,
				Placements: []Segment{berthSeg(e.berths[i], "2027-06-10", "2027-06-12")},
			}
			c, err := e.svc.CreateTransit(ctx, e.Owner, e.MarinaID, in)
			if err != nil {
				t.Fatal(err)
			}
			if c.Row.Channel != ch || len(activeOnly(c.Allocations)) != 1 || e.rows(t, c.Allocations[0].ID) != 3 {
				t.Fatalf("%s: allocations %+v", ch, c.Allocations)
			}
			// The same berth again: no transit is created.
			before, _ := store.CountContracts(ctx, db.DB, e.scope(t))
			if _, err := e.svc.CreateTransit(ctx, e.Owner, e.MarinaID, in); !errors.Is(err, occupancy.ErrResourceTaken) {
				t.Fatalf("%s duplicate: %v", ch, err)
			}
			after, _ := store.CountContracts(ctx, db.DB, e.scope(t))
			if after != before {
				t.Fatalf("%s: a transit without allocation was written", ch)
			}
		}
		// Online is M8.
		_, err := e.svc.CreateTransit(ctx, e.Owner, e.MarinaID, TransitInput{
			VesselID: e.vessel, Arrival: d("2027-06-20"), Departure: d("2027-06-21"), Channel: "online",
			Placements: []Segment{berthSeg(e.berths[3], "2027-06-20", "2027-06-20")},
		})
		if !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("online channel: %v", err)
		}
	})
}

func TestQuote_OptionHoldLifecycle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		q := e.quote(t, e.Owner, KindTransit, "", "2027-07-01", "2027-07-04", true, e.berths[0], e.berths[1])
		if q.Code != "PRV-2027-0001" || len(q.Options) != 2 || q.Row.ValidUntil.String() != "2027-06-09" {
			t.Fatalf("quote %s options %d valid %s", q.Code, len(q.Options), q.Row.ValidUntil)
		}
		var holds []string
		for _, o := range q.Options {
			if o.Row.TotalCents != 3*3420 || o.Snapshot == nil || o.Row.PriceListID != e.list {
				t.Fatalf("option price %+v", o.Row)
			}
			holds = append(holds, o.Placements[0].AllocationID)
			if e.rows(t, o.Placements[0].AllocationID) != 3 {
				t.Fatal("hold rows")
			}
		}
		// The held berths are taken for transits.
		_, err := e.svc.CreateTransit(ctx, e.Owner, e.MarinaID, TransitInput{
			VesselID: e.vessel, CustomerID: e.cust, Arrival: d("2027-07-02"), Departure: d("2027-07-03"),
			Placements: []Segment{berthSeg(e.berths[0], "2027-07-02", "2027-07-02")},
		})
		if !errors.Is(err, occupancy.ErrResourceTaken) {
			t.Fatalf("held berth sold: %v", err)
		}
		// Update replaces the options: old holds cancelled, the new one allocated.
		in := QuoteInput{
			Kind: KindTransit, CustomerID: e.cust, VesselID: e.vessel, StartsOn: d("2027-07-01"), EndsOn: d("2027-07-04"),
			Options: []OptionInput{{Hold: true, Placements: []Segment{berthSeg(e.berths[2], "2027-07-01", "2027-07-03")}}},
		}
		q, err = e.svc.UpdateQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, in)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range holds {
			if e.rows(t, h) != 0 {
				t.Fatal("old hold kept rows")
			}
		}
		if len(q.Options) != 1 || e.rows(t, q.Options[0].Placements[0].AllocationID) != 3 {
			t.Fatalf("new hold: %+v", q.Options)
		}
		// Declined: holds gone.
		q, err = e.svc.DeclineQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, "")
		if err != nil {
			t.Fatal(err)
		}
		if q.Row.Status != string(QuoteDeclined) || q.Options[0].Placements[0].AllocationID != "" {
			t.Fatalf("declined: %s %+v", q.Row.Status, q.Options[0].Placements)
		}
		if _, err := e.svc.UpdateQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, in); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("update declined: %v", err)
		}
		e.invariants(t)
	})
}

func TestQuote_AcceptConvertsHold(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		// Mooring quote with two options and holds; accept option 2 → draft contract, option 1 hold gone.
		q := e.quote(t, e.Owner, KindMooring, TermAnnual, "2027-07-01", "2028-07-01", true, e.berths[0], e.berths[1])
		h1, h2 := q.Options[0].Placements[0].AllocationID, q.Options[1].Placements[0].AllocationID
		qd, c, err := e.svc.AcceptQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, AcceptInput{OptionID: q.Options[1].Row.ID})
		if err != nil {
			t.Fatal(err)
		}
		if qd.Row.Status != string(QuoteAccepted) || qd.Row.ContractID != c.Row.ID || c.Row.Status != string(StatusDraft) || c.Row.QuoteID != q.Row.ID {
			t.Fatalf("accept: quote %s contract %s", qd.Row.Status, c.Row.Status)
		}
		if e.rows(t, h1) != 0 || e.rows(t, h2) != 0 {
			t.Fatal("option holds kept rows")
		}
		act := activeOnly(c.Allocations)
		if len(act) != 1 || act[0].ResourceID != e.berths[1] || act[0].MovedFromID != "" || act[0].Kind != store.AllocKindHold || e.rows(t, act[0].ID) != 366 {
			t.Fatalf("contract allocation %+v", act)
		}
		if c.Row.TotalCents != q.Options[1].Row.TotalCents || string(c.Row.PriceSnapshot) == "" {
			t.Fatalf("snapshot not copied: %d vs %d", c.Row.TotalCents, q.Options[1].Row.TotalCents)
		}
		// Accepting again: invalid transition.
		if _, _, err := e.svc.AcceptQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, qd.Row.Version, AcceptInput{OptionID: q.Options[0].Row.ID}); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("second accept: %v", err)
		}
		// Transit quote accepted with the manual driver → confirmed transit on the held berth.
		tq := e.quote(t, e.Owner, KindTransit, "", "2027-06-20", "2027-06-22", true, e.berths[2])
		_, tc, err := e.svc.AcceptQuote(ctx, e.Owner, e.MarinaID, tq.Row.ID, tq.Row.Version, AcceptInput{
			OptionID: tq.Options[0].Row.ID, Confirm: &ManualConfirmation{Reference: "bonifico"},
		})
		if err != nil {
			t.Fatal(err)
		}
		ta := activeOnly(tc.Allocations)
		if tc.Row.Status != string(StatusConfirmed) || len(ta) != 1 || ta[0].Kind != store.AllocKindTransit || e.rows(t, ta[0].ID) != 2 {
			t.Fatalf("accepted transit %s %+v", tc.Row.Status, ta)
		}
		// Expired quote: quote_expired.
		eq := e.quote(t, e.Owner, KindTransit, "", "2027-08-01", "2027-08-02", false, e.berths[3])
		e.Clock.Set(eq.Row.ExpiresAt.Add(time.Second))
		if _, _, err := e.svc.AcceptQuote(ctx, e.Owner, e.MarinaID, eq.Row.ID, eq.Row.Version, AcceptInput{OptionID: eq.Options[0].Row.ID}); !errors.Is(err, ErrQuoteExpired) {
			t.Fatalf("expired accept: %v", err)
		}
		e.invariants(t)
	})
}

func TestQuote_AcceptConflictCreatesNothing(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		q := e.quote(t, e.Owner, KindTransit, "", "2027-07-01", "2027-07-04", false, e.berths[0])
		e.transit(t, e.Owner, e.berths[0], "2027-07-03", "2027-07-05", true)
		before, _ := store.CountContracts(ctx, db.DB, e.scope(t))
		_, _, err := e.svc.AcceptQuote(ctx, e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, AcceptInput{OptionID: q.Options[0].Row.ID})
		var ce *occupancy.ConflictError
		if !errors.As(err, &ce) || ce.ResourceID != e.berths[0] {
			t.Fatalf("accept on a taken berth: %v", err)
		}
		after, _ := store.CountContracts(ctx, db.DB, e.scope(t))
		got, _ := e.svc.GetQuote(ctx, e.Owner, e.MarinaID, q.Row.ID)
		if after != before || got.Row.Status != string(QuoteDraft) || got.Row.Version != q.Row.Version {
			t.Fatalf("conflict left contracts %d→%d quote %s v%d", before, after, got.Row.Status, got.Row.Version)
		}
	})
}

// TestContract_SnapshotStored is INV-20: the stored snapshot quotes again to the same total.
func TestContract_SnapshotStored(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		for _, c := range []*Detail{
			e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-15", true),
			e.mooring(t, e.Owner, e.berths[1], "2027-06-02", true),
		} {
			snap := DecodeSnapshot(c.Row.PriceSnapshot)
			if snap == nil || snap.EngineVersion != pricing.EngineVersion || snap.Resolved.PriceListID != e.list ||
				int64(snap.Breakdown.Total) != c.Row.TotalCents || c.Row.EngineVersion != pricing.EngineVersion {
				t.Fatalf("snapshot %+v row %+v", snap, c.Row)
			}
			// Days later, the pinned request prices the same.
			e.Clock.Advance(72 * time.Hour)
			b, err := e.svc.Recompute(ctx, e.Owner, e.MarinaID, snap)
			if err != nil {
				t.Fatal(err)
			}
			if b.Total != snap.Breakdown.Total || b.CatalogHash != snap.Breakdown.CatalogHash {
				t.Fatalf("recomputed %d (%s), stored %d (%s)", b.Total, b.CatalogHash, snap.Breakdown.Total, snap.Breakdown.CatalogHash)
			}
		}
	})
}

func TestContract_PriceListChangedDuringCreate(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		e.svc.afterPrice = func() {
			if _, err := db.NewUpdate().Table("price_lists").Set("status = 'archived'").Where("id = ?", e.list).Exec(context.Background()); err != nil {
				t.Error(err)
			}
		}
		_, err := e.svc.CreateTransit(ctx, e.Owner, e.MarinaID, TransitInput{
			VesselID: e.vessel, CustomerID: e.cust, Arrival: d("2027-06-10"), Departure: d("2027-06-11"),
			Placements: []Segment{berthSeg(e.berths[0], "2027-06-10", "2027-06-10")},
		})
		if !errors.Is(err, ErrPriceListChanged) {
			t.Fatalf("changed list: %v", err)
		}
		if n, _ := store.CountContracts(ctx, db.DB, e.scope(t)); n != 0 {
			t.Fatal("transit written")
		}
	})
}

func TestContract_Numbering(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		t1 := e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-11", false)
		t2 := e.transit(t, e.Owner, e.berths[1], "2027-06-10", "2027-06-11", false)
		m1 := e.mooring(t, e.Owner, e.berths[2], "2027-06-10", false)
		q1 := e.quote(t, e.Owner, KindTransit, "", "2027-06-20", "2027-06-21", false, e.berths[3])
		got := []string{t1.Code, t2.Code, m1.Code, q1.Code}
		want := []string{"TRN-2027-0001", "TRN-2027-0002", "ORM-2027-0001", "PRV-2027-0001"}
		if !slices.Equal(got, want) {
			t.Fatalf("codes %v, want %v", got, want)
		}
		// A failed creation does not consume a number.
		_, err := e.svc.CreateTransit(fixture.Ctx(), e.Owner, e.MarinaID, TransitInput{
			VesselID: e.vessel, CustomerID: e.cust, Arrival: d("2027-06-10"), Departure: d("2027-06-11"),
			Placements: []Segment{berthSeg(e.berths[0], "2027-06-10", "2027-06-10")},
		})
		if !errors.Is(err, occupancy.ErrResourceTaken) {
			t.Fatal(err)
		}
		t3 := e.transit(t, e.Owner, e.berths[3], "2027-06-10", "2027-06-11", false)
		if t3.Code != "TRN-2027-0003" {
			t.Fatalf("after a failure: %s", t3.Code)
		}
		// The unique index is the guarantee.
		dup := t3.Row
		dup.ID = store.NewID()
		if err := store.InsertContract(context.Background(), db.DB, &dup); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("duplicate number: %v", err)
		}
	})
}

func TestContract_TerminateTruncates(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		c, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, MooringInput{
			Term: TermAnnual, CustomerID: e.cust, VesselID: e.vessel, StartsOn: d("2027-06-02"),
			Placements: []Segment{berthSeg(e.berths[0], "2027-06-02", "2028-06-01")},
			Extras:     []ExtraResource{{LandResourceID: e.lands[0]}}, Activate: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.Row.EndsOn.String() != "2028-06-02" || len(c.Allocations) != 2 || len(c.Lines) != 2 {
			t.Fatalf("contract ends %s allocations %d lines %d", c.Row.EndsOn, len(c.Allocations), len(c.Lines))
		}
		for _, a := range c.Allocations {
			if e.rows(t, a.ID) != 366 {
				t.Fatalf("rows of %s: %d", a.ResourceType, e.rows(t, a.ID))
			}
		}
		if _, err := e.svc.TerminateContract(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, d("2028-07-01"), "x"); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("termination after the end: %v", err)
		}
		c, err = e.svc.TerminateContract(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, d("2027-09-01"), "vendita")
		if err != nil {
			t.Fatal(err)
		}
		want := int(d("2027-06-02").DaysUntil(d("2027-09-01")))
		for _, a := range c.Allocations {
			if a.Status != store.AllocActive || a.LastNight.String() != "2027-08-31" || e.rows(t, a.ID) != want {
				t.Fatalf("truncated %+v rows %d want %d", a, e.rows(t, a.ID), want)
			}
		}
		if c.Row.Status != string(StatusTerminated) || c.Row.TerminatedOn.String() != "2027-09-01" {
			t.Fatalf("terminated %+v", c.Row)
		}
		// The freed nights can be sold.
		e.transit(t, e.Owner, e.berths[0], "2027-09-01", "2027-09-03", true)
		e.invariants(t)
	})
}

func TestTransit_StayChange(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		c := e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-13", true)
		e.transit(t, e.Owner, e.berths[0], "2027-06-14", "2027-06-16", true)
		// Extending into the next transit: 409, unchanged.
		dep := d("2027-06-15")
		if _, _, err := e.svc.ChangeStay(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, StayChange{Departure: &dep}); !errors.Is(err, occupancy.ErrResourceTaken) {
			t.Fatalf("extension on a taken night: %v", err)
		}
		got, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, c.Row.ID)
		if got.Row.Version != c.Row.Version || e.rows(t, c.Allocations[0].ID) != 3 {
			t.Fatal("failed stay change wrote")
		}
		// With an extension target the added nights go to another berth.
		ext := Segment{ResourceType: store.ResourceBerth, ResourceID: e.berths[1]}
		got, eff, err := e.svc.ChangeStay(ctx, e.Owner, e.MarinaID, c.Row.ID, c.Row.Version, StayChange{Departure: &dep, ExtensionTarget: &ext})
		if err != nil {
			t.Fatal(err)
		}
		act := activeOnly(got.Allocations)
		if len(act) != 2 || act[1].ResourceID != e.berths[1] || act[1].MovedFromID != act[0].ID || act[1].ContractLineID != act[0].ContractLineID || e.rows(t, act[1].ID) != 2 ||
			act[1].Kind != store.AllocKindTransit || eff.DeltaCents != 2*3420 {
			t.Fatalf("extension %+v effect %+v", act, eff)
		}
		// Shorten: arrival later, departure earlier; the extension is cancelled.
		arr, dep2 := d("2027-06-11"), d("2027-06-12")
		got, eff, err = e.svc.ChangeStay(ctx, e.Owner, e.MarinaID, c.Row.ID, got.Row.Version, StayChange{Arrival: &arr, Departure: &dep2})
		if err != nil {
			t.Fatal(err)
		}
		act = activeOnly(got.Allocations)
		if len(act) != 1 || act[0].FirstNight.String() != "2027-06-11" || act[0].LastNight.String() != "2027-06-11" || e.rows(t, act[0].ID) != 1 ||
			got.Row.TotalCents != 3420 || eff.NewTotalCents != 3420 {
			t.Fatalf("shortened %+v total %d", act, got.Row.TotalCents)
		}
		e.invariants(t)
	})
}

func TestTransit_EarlyDepartureFreesNights(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		c := e.transit(t, e.Owner, e.berths[0], "2027-06-02", "2027-06-07", true)
		scope := e.scope(t)
		run := func(fn func(ctx context.Context, tx bunTx) error) {
			t.Helper()
			if err := db.RunInTx(ctx, fn); err != nil {
				t.Fatal(err)
			}
		}
		run(func(ctx context.Context, tx bunTx) error {
			_, err := e.svc.OnArrivalTx(ctx, tx, scope, e.Owner, c.Row.ID)
			return err
		})
		run(func(ctx context.Context, tx bunTx) error {
			_, err := e.svc.OnDepartureTx(ctx, tx, scope, e.Owner, c.Row.ID, d("2027-06-04"))
			return err
		})
		got, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, c.Row.ID)
		if got.Row.Status != string(StatusDeparted) || got.Allocations[0].LastNight.String() != "2027-06-03" || e.rows(t, got.Allocations[0].ID) != 2 ||
			got.Row.TotalCents != c.Row.TotalCents {
			t.Fatalf("departed %s last %s rows %d total %d", got.Row.Status, got.Allocations[0].LastNight, e.rows(t, got.Allocations[0].ID), got.Row.TotalCents)
		}
		e.transit(t, e.Owner, e.berths[0], "2027-06-04", "2027-06-07", false)
		e.invariants(t)
	})
}

func TestContract_ExpireJob(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		ctx := fixture.Ctx()
		held := e.transit(t, e.Owner, e.berths[0], "2027-06-10", "2027-06-12", false)
		draft := e.mooring(t, e.Owner, e.berths[1], "2027-06-20", false)
		q := e.quote(t, e.Owner, KindTransit, "", "2027-07-01", "2027-07-03", true, e.berths[2])
		ended, err := e.svc.CreateContract(ctx, e.Owner, e.MarinaID, MooringInput{
			Term: TermMonthly, CustomerID: e.cust, VesselID: e.vessel, StartsOn: d("2027-06-02"),
			Placements: []Segment{berthSeg(e.berths[3], "2027-06-02", "2027-07-01")}, Activate: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Nothing to do yet.
		if r, err := e.svc.ExpireMarina(ctx, e.MarinaID); err != nil || r != (ExpiryResult{}) {
			t.Fatalf("early run %+v %v", r, err)
		}
		// After 25 h the desk hold is gone; the draft (7 days) and the quote (7 days) are not.
		e.Clock.Advance(25 * time.Hour)
		r, err := e.svc.ExpireMarina(ctx, e.MarinaID)
		if err != nil || r.Transits != 1 || r.Quotes != 0 || r.DraftsLost != 0 {
			t.Fatalf("after 25 h %+v %v", r, err)
		}
		got, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, held.Row.ID)
		if got.Row.Status != string(StatusExpired) || got.Allocations[0].Status != store.AllocExpired || e.rows(t, got.Allocations[0].ID) != 0 {
			t.Fatalf("expired transit %s %+v", got.Row.Status, got.Allocations)
		}
		if n := len(e.Events(t, event.OccupancyHoldExpired)); n != 1 {
			t.Fatalf("hold_expired events %d", n)
		}
		// After 8 days: the quote expires, the draft loses its hold.
		e.Clock.Advance(7 * 24 * time.Hour)
		r, err = e.svc.ExpireMarina(ctx, e.MarinaID)
		if err != nil || r.Quotes != 1 || r.DraftsLost != 1 {
			t.Fatalf("after 8 days %+v %v", r, err)
		}
		gq, _ := e.svc.GetQuote(ctx, e.Owner, e.MarinaID, q.Row.ID)
		gd, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, draft.Row.ID)
		if gq.Row.Status != string(QuoteExpired) || gd.Row.Status != string(StatusDraft) || !gd.Row.HoldLost || e.rows(t, gd.Allocations[0].ID) != 0 {
			t.Fatalf("quote %s draft %s lost %v", gq.Row.Status, gd.Row.Status, gd.Row.HoldLost)
		}
		// Activation of a draft that lost its hold allocates again.
		gd, err = e.svc.ActivateContract(ctx, e.Owner, e.MarinaID, gd.Row.ID, gd.Row.Version)
		if err != nil {
			t.Fatal(err)
		}
		act := activeOnly(gd.Allocations)
		if gd.Row.Status != string(StatusActive) || gd.Row.HoldLost || len(act) != 1 || act[0].Kind != store.AllocKindContract || e.rows(t, act[0].ID) != 366 {
			t.Fatalf("reactivated %+v", act)
		}
		// The monthly contract expires at its end.
		e.Clock.Set(time.Date(2027, 7, 2, 10, 0, 0, 0, time.UTC))
		if _, err := e.svc.RunExpiryCount(ctx); err != nil {
			t.Fatal(err)
		}
		ge, _ := e.svc.GetContract(ctx, e.Owner, e.MarinaID, ended.Row.ID)
		if ge.Row.Status != string(StatusExpired) {
			t.Fatalf("monthly %s", ge.Row.Status)
		}
		// Idempotent.
		if r, err := e.svc.ExpireMarina(ctx, e.MarinaID); err != nil || r.Transits+r.Quotes+r.DraftsLost+r.Contracts != 0 {
			t.Fatalf("second run %+v %v", r, err)
		}
		e.invariants(t)
	})
}

// TestConcurrency_NumberingUnique: 100 parallel creations get numbers 1..100, no gaps, no duplicates.
func TestConcurrency_NumberingUnique(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		const n = 100
		scope := e.scope(t)
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := db.RunInTx(context.Background(), func(ctx context.Context, tx bunTx) error {
					num, err := store.NextNumber(ctx, tx, scope, SeriesQuote, 2027)
					if err != nil {
						return err
					}
					now := store.UTC(e.Clock.Now())
					return store.InsertQuote(ctx, tx, &store.Quote{
						ID: store.NewID(), MarinaID: e.MarinaID, Number: num, Year: 2027, Status: "draft", Kind: KindTransit,
						CustomerID: e.cust, VesselID: e.vessel, StartsOn: d("2027-07-01").AddDays(i % 10), EndsOn: d("2027-07-20"),
						ValidUntil: d("2027-06-10"), ExpiresAt: now.Add(time.Hour), Channel: "desk", Policy: store.JSON(`{}`),
						Version: 1, CreatedAt: now, UpdatedAt: now,
					})
				})
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var nums []int64
		if err := db.NewSelect().Model((*store.Quote)(nil)).Column("number").Where("marina_id = ?", e.MarinaID).OrderExpr("number").Scan(context.Background(), &nums); err != nil {
			t.Fatal(err)
		}
		if len(nums) != n {
			t.Fatalf("%d quotes", len(nums))
		}
		for i, v := range nums {
			if v != int64(i+1) {
				t.Fatalf("numbers %v", nums)
			}
		}
	})
}

// TestConcurrency_QuoteAcceptVsTransit: a held-less quote option and 100 desk transits race for the same berth
// and nights; exactly one wins, every loser is resource_taken and wrote nothing.
func TestConcurrency_QuoteAcceptVsTransit(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		q := e.quote(t, e.Owner, KindTransit, "", "2027-07-01", "2027-07-04", false, e.berths[0])
		const n = 100
		var wg sync.WaitGroup
		var mu sync.Mutex
		wins, taken := 0, 0
		acceptWon := false
		start := make(chan struct{})
		for i := range n + 1 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var err error
				if i == 0 {
					_, _, err = e.svc.AcceptQuote(fixture.Ctx(), e.Owner, e.MarinaID, q.Row.ID, q.Row.Version, AcceptInput{OptionID: q.Options[0].Row.ID})
				} else {
					first := d("2027-07-01").AddDays(i % 3)
					_, err = e.svc.CreateTransit(fixture.Ctx(), e.Owner, e.MarinaID, TransitInput{
						VesselID: e.vessel, CustomerID: e.cust, Arrival: first, Departure: first.AddDays(1),
						Placements: []Segment{{ResourceType: store.ResourceBerth, ResourceID: e.berths[0], First: first, Last: first}},
					})
				}
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					wins++
					if i == 0 {
						acceptWon = true
					}
				case errors.Is(err, occupancy.ErrResourceTaken):
					taken++
				default:
					t.Errorf("attempt %d: %v", i, err)
				}
			}()
		}
		close(start)
		wg.Wait()
		// The quote covers the three nights, each transit one of them: either the quote wins alone or up to three
		// transits (one per night) win.
		if acceptWon && wins != 1 || !acceptWon && (wins < 1 || wins > 3) || wins+taken != n+1 {
			t.Fatalf("wins %d (accept %v) taken %d", wins, acceptWon, taken)
		}
		var rows []store.BerthOccupancy
		if err := db.NewSelect().Model(&rows).Where("berth_id = ?", e.berths[0]).Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, r := range rows {
			k := r.Night.String()
			if seen[k] {
				t.Fatalf("night %s twice", k)
			}
			seen[k] = true
		}
		var contracts int
		contracts, _ = store.CountContracts(context.Background(), db.DB, e.scope(t))
		if contracts != wins {
			t.Fatalf("%d contracts for %d wins", contracts, wins)
		}
		e.invariants(t)
	})
}

// TestConcurrency_ConvertVsReclaim (INV-05, INV-01): a held transit's hold expires at T; half of 100 goroutines
// confirm it with a clock at T − 1s (ConvertTx turns the hold into a transit allocation) while the other half
// allocate the same berth and nights at T + 1s (lazy reclaim of the expired hold). Exactly one side wins and keeps
// its rows: a reclaim never expires a hold converted in the meantime (on PostgreSQL READ COMMITTED the UPDATE
// re-checks kind and expiry after waiting for the row lock), and the invariant queries find nothing.
func TestConcurrency_ConvertVsReclaim(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newTenv(t, db)
		const n = 100
		vessels := make([]string, n)
		for i := range vessels {
			vessels[i] = e.addVessel(t, "Rival", 1000, 350, "monohull_sail")
		}
		var held []*Detail
		for _, b := range e.berths[:3] {
			held = append(held, e.transit(t, e.Owner, b, "2027-06-10", "2027-06-12", false))
		}
		expiry := held[0].Allocations[0].ExpiresAt
		e.Clock.Set(expiry.Add(-time.Second))
		late := expiry.Add(time.Second)
		scope := e.scope(t)
		for round, h := range held {
			berth := e.berths[round]
			var mu sync.Mutex
			confirmed, allocated := 0, 0
			var winner *store.Allocation
			race := make(chan struct{})
			var wg sync.WaitGroup
			for i := range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-race
					if i%2 == 0 {
						_, err := e.svc.ConfirmTransit(fixture.Ctx(), e.Owner, e.MarinaID, h.Row.ID, h.Row.Version, ManualConfirmation{})
						mu.Lock()
						defer mu.Unlock()
						switch {
						case err == nil:
							confirmed++
						case errors.Is(err, ErrVersionMismatch), errors.Is(err, occupancy.ErrHoldExpired),
							errors.Is(err, occupancy.ErrResourceTaken), errors.Is(err, ErrInvalidTransition):
						default:
							t.Errorf("confirm: %v", err)
						}
						return
					}
					var a *store.Allocation
					err := db.RunInTx(context.Background(), func(ctx context.Context, tx bunTx) error {
						var err error
						v := vessels[i]
						a, err = e.occ.AllocateTx(ctx, tx, scope, late, occupancy.NewAllocation{
							VesselID: &v, Kind: occupancy.KindTransit, UserID: e.OwnerID,
							Spec: Segment{ResourceType: store.ResourceBerth, ResourceID: berth, First: d("2027-06-10"), Last: d("2027-06-11")}.spec(d("2027-06-10"), d("2027-06-11")),
						})
						return err
					})
					mu.Lock()
					defer mu.Unlock()
					switch {
					case err == nil:
						allocated++
						winner = a
					case errors.Is(err, occupancy.ErrResourceTaken):
					default:
						t.Errorf("allocate: %v", err)
					}
				}()
			}
			close(race)
			wg.Wait()
			if confirmed+allocated != 1 {
				t.Fatalf("round %d: %d confirmations and %d allocations won", round, confirmed, allocated)
			}
			c, err := store.GetContract(fixture.Ctx(), db.DB, scope, h.Row.ID)
			if err != nil {
				t.Fatal(err)
			}
			allocs, err := contractAllocs(fixture.Ctx(), db.DB, scope, h.Row.ID)
			if err != nil {
				t.Fatal(err)
			}
			act := activeOnly(allocs)
			if confirmed == 1 {
				if c.Status != string(StatusConfirmed) || len(act) != 1 || act[0].Kind != store.AllocKindTransit || e.rows(t, act[0].ID) != 2 {
					t.Fatalf("round %d: confirm won but the transit lost its rows: %s %+v", round, c.Status, allocs)
				}
			} else {
				if c.Status != string(StatusHeld) || len(act) != 0 || e.rows(t, winner.ID) != 2 {
					t.Fatalf("round %d: allocation won but rows are wrong: %s %+v", round, c.Status, allocs)
				}
			}
			if rows, err := db.NewSelect().TableExpr("berth_occupancies").Where("berth_id = ?", berth).Count(context.Background()); err != nil || rows != 2 {
				t.Fatalf("round %d: %d rows on the berth (%v)", round, rows, err)
			}
			e.invariants(t)
		}
	})
}
