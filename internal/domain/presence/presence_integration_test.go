//go:build integration

package presence_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/document"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func at(e *contracttest.Env, h int) *time.Time {
	t := e.Clock.Now().Add(time.Duration(h) * time.Hour)
	return &t
}

func TestPresence_ArrivalDepartureState(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := contracttest.New(t, db)
		ctx := fixture.Ctx()
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		c := e.Transit(t, e.Berths[0], "2027-06-02", "2027-06-06", true)

		r, err := e.Presence.Record(ctx, dock, e.MarinaID, presence.Input{VesselID: e.Vessel, Event: presence.EventArrived, Notes: "ormeggiato a poppa"})
		if err != nil {
			t.Fatal(err)
		}
		if r.Presence.Row.AllocationID != c.Allocations[0].ID || r.Presence.Row.ContractID != c.Row.ID || r.Contract == nil ||
			r.Contract.Row.Status != string(contract.StatusArrived) || r.Presence.Row.Night.String() != "2027-06-02" {
			t.Fatalf("arrival %+v contract %+v", r.Presence.Row, r.Contract)
		}
		vp, err := store.GetVesselPresence(ctx, db.DB, e.Scope(t), e.Vessel)
		if err != nil || vp.State != presence.StateInPort || vp.AllocationID != c.Allocations[0].ID {
			t.Fatalf("state %+v %v", vp, err)
		}
		// A second arrival is refused (§13 D21).
		if _, err := e.Presence.Record(ctx, dock, e.MarinaID, presence.Input{VesselID: e.Vessel, Event: presence.EventArrived}); !errors.Is(err, presence.ErrInvalidTransition) {
			t.Fatalf("double arrival: %v", err)
		}
		// Early departure on the 4th frees the nights of the 4th and 5th.
		e.Clock.Set(time.Date(2027, 6, 4, 8, 0, 0, 0, time.UTC))
		ret := contracttest.D("2027-06-20")
		r, err = e.Presence.Record(ctx, dock, e.MarinaID, presence.Input{VesselID: e.Vessel, Event: presence.EventDeparted, ExpectedReturnOn: &ret})
		if err != nil {
			t.Fatal(err)
		}
		if r.Contract.Row.Status != string(contract.StatusDeparted) || e.Rows(t, c.Allocations[0].ID) != 2 {
			t.Fatalf("departure: %s rows %d", r.Contract.Row.Status, e.Rows(t, c.Allocations[0].ID))
		}
		vp, _ = store.GetVesselPresence(ctx, db.DB, e.Scope(t), e.Vessel)
		if vp.State != presence.StateOut || vp.ExpectedReturnOn.String() != "2027-06-20" {
			t.Fatalf("after departure %+v", vp)
		}
		if _, err := e.Presence.Record(ctx, dock, e.MarinaID, presence.Input{VesselID: e.Vessel, Event: presence.EventDeparted}); !errors.Is(err, presence.ErrInvalidTransition) {
			t.Fatalf("double departure: %v", err)
		}
		// Events carry ids only.
		for _, typ := range []string{event.PresenceArrived, event.PresenceDeparted} {
			evs := e.Events(t, typ)
			if len(evs) != 1 || strings.Contains(string(evs[0].Payload), "poppa") {
				t.Fatalf("%s events %d %s", typ, len(evs), evs[0].Payload)
			}
		}
		// Notes are hidden without customers.read.
		yard := e.AddUser(t, "yard@porto.test", auth.RoleYard, e.OrgID, e.MarinaID)
		d := contracttest.D("2027-06-02")
		page, err := e.Presence.List(ctx, yard, e.MarinaID, presence.Filter{Night: &d}, "", 0)
		if err != nil || len(page.Items) != 1 || page.Items[0].Row.Notes != "" {
			t.Fatalf("yard list %+v %v", page.Items, err)
		}
		page, _ = e.Presence.List(ctx, dock, e.MarinaID, presence.Filter{Night: &d}, "", 0)
		if page.Items[0].Row.Notes == "" {
			t.Fatal("dock does not see the notes")
		}
		// The future is refused.
		if _, err := e.Presence.Record(ctx, dock, e.MarinaID, presence.Input{VesselID: e.Vessel, Event: presence.EventArrived, At: at(e, 2)}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("future arrival: %v", err)
		}
		// Another marina's vessel is 404.
		_, other, otherOwner := e.OtherOrganization(t)
		if _, err := e.Presence.Record(ctx, otherOwner, other, presence.Input{VesselID: e.Vessel, Event: presence.EventArrived}); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("foreign vessel: %v", err)
		}
	})
}

func TestPresence_HeldTransitArrivalChainsOnConfirm(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := contracttest.New(t, db)
		ctx := fixture.Ctx()
		c := e.Transit(t, e.Berths[0], "2027-06-02", "2027-06-04", false)
		r, err := e.Presence.Record(ctx, e.Owner, e.MarinaID, presence.Input{VesselID: e.Vessel, Event: presence.EventArrived})
		if err != nil {
			t.Fatal(err)
		}
		if r.Contract.Row.Status != string(contract.StatusHeld) {
			t.Fatalf("held transit after arrival: %s", r.Contract.Row.Status)
		}
		got, err := e.Contracts.ConfirmTransit(ctx, e.Owner, e.MarinaID, c.Row.ID, r.Contract.Row.Version, contract.ManualConfirmation{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Row.Status != string(contract.StatusArrived) || len(e.Events(t, event.TransitArrived)) != 1 || len(e.Events(t, event.TransitConfirmed)) != 1 {
			t.Fatalf("chained %s", got.Row.Status)
		}
	})
}

func TestPresence_UnexpectedArrival(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := contracttest.New(t, db)
		ctx := fixture.Ctx()
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		r, err := e.Presence.UnexpectedArrival(ctx, dock, e.MarinaID, presence.UnexpectedInput{
			NewVessel: &contract.NewVessel{Name: "Maestrale", LOACm: 950, BeamCm: 330, HullType: "monohull_sail"},
			Target:    contract.Segment{ResourceType: store.ResourceBerth, ResourceID: e.Berths[1]},
			Notes:     "arrivata di notte",
		})
		if err != nil {
			t.Fatal(err)
		}
		c := r.Contract
		if c == nil || c.Row.Status != string(contract.StatusHeld) || c.Row.CustomerID != "" || c.Row.StartsOn.String() != "2027-06-02" ||
			c.Row.EndsOn.String() != "2027-06-03" || c.Vessel.Name != "Maestrale" || c.Row.TotalCents != 3*950 {
			t.Fatalf("unexpected transit %+v", c)
		}
		if r.Presence.Row.Event != presence.EventArrived || r.Presence.Row.AllocationID != c.Allocations[0].ID || e.Rows(t, c.Allocations[0].ID) != 1 {
			t.Fatalf("presence %+v", r.Presence.Row)
		}
		// Arrived without booking in the movements of the day.
		m, err := e.Presence.Movements(ctx, dock, e.MarinaID, nil, "")
		if err != nil || len(m.ArrivedWithoutBooking) != 1 || len(m.Arrived) != 1 || m.ArrivedWithoutBooking[0].Resource.Code != "A2" {
			t.Fatalf("movements %+v %v", m, err)
		}
		// Confirmation needs a customer first.
		if _, err := e.Contracts.ConfirmTransit(ctx, dock, e.MarinaID, c.Row.ID, c.Row.Version, contract.ManualConfirmation{}); !errors.Is(err, contract.ErrCustomerRequired) {
			t.Fatalf("confirm without customer: %v", err)
		}
		// The berth is taken: a second unexpected arrival there conflicts and writes nothing.
		before, _ := store.CountContracts(ctx, db.DB, e.Scope(t))
		_, err = e.Presence.UnexpectedArrival(ctx, dock, e.MarinaID, presence.UnexpectedInput{
			VesselID: e.Vessel, Target: contract.Segment{ResourceType: store.ResourceBerth, ResourceID: e.Berths[1]},
		})
		if err == nil {
			t.Fatal("second vessel on the same berth")
		}
		after, _ := store.CountContracts(ctx, db.DB, e.Scope(t))
		if after != before {
			t.Fatal("failed unexpected arrival wrote a transit")
		}
		// The yard role cannot.
		yard := e.AddUser(t, "yard@porto.test", auth.RoleYard, e.OrgID, e.MarinaID)
		if _, err := e.Presence.UnexpectedArrival(ctx, yard, e.MarinaID, presence.UnexpectedInput{VesselID: e.Vessel}); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard: %v", err)
		}
	})
}

func TestPresence_Movements(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := contracttest.New(t, db)
		ctx := fixture.Ctx()
		e.Transit(t, e.Berths[0], "2027-06-02", "2027-06-04", true) // arrives today
		e.Transit(t, e.Berths[1], "2027-05-30", "2027-06-02", true) // leaves today
		other := e.AddVessel(t, "Grecale", 900, 300, "monohull_motor")
		_, err := e.Contracts.CreateContract(ctx, e.Owner, e.MarinaID, contract.MooringInput{
			Term: contract.TermAnnual, CustomerID: e.Customer, VesselID: other, StartsOn: contracttest.D("2027-01-01"),
			Placements: []contract.Segment{contracttest.BerthSeg(e.Berths[2], "2027-01-01", "2027-12-31")}, Activate: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Presence.Record(ctx, e.Owner, e.MarinaID, presence.Input{VesselID: other, Event: presence.EventDeparted, At: at(e, -1)}); err != nil {
			t.Fatal(err)
		}
		m, err := e.Presence.Movements(ctx, e.Owner, e.MarinaID, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(m.ArrivalsExpected) != 1 || m.ArrivalsExpected[0].ExpectedTime != "14:00" || m.ArrivalsExpected[0].Resource.Code != "A1" ||
			len(m.DeparturesExpected) != 1 || m.DeparturesExpected[0].Resource.Code != "A2" ||
			len(m.VesselsOut) != 1 || m.VesselsOut[0].Vessel.ID != other || len(m.Departed) != 1 {
			t.Fatalf("movements %+v", m)
		}
		// Another map: nothing.
		m, _ = e.Presence.Movements(ctx, e.Owner, e.MarinaID, nil, store.NewID())
		if len(m.ArrivalsExpected)+len(m.DeparturesExpected)+len(m.VesselsOut)+len(m.Departed) != 0 {
			t.Fatalf("filtered %+v", m)
		}
	})
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPresence_Photo(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := contracttest.New(t, db)
		ctx := fixture.Ctx()
		dock := e.AddUser(t, "dock@porto.test", auth.RoleDock, e.OrgID, e.MarinaID)
		r, err := e.Presence.Record(ctx, dock, e.MarinaID, presence.Input{VesselID: e.Vessel, Event: presence.EventArrived})
		if err != nil {
			t.Fatal(err)
		}
		id := r.Presence.Row.ID
		if _, err := e.Presence.UploadPhoto(ctx, dock, e.MarinaID, id, "application/pdf", bytes.NewReader([]byte("%PDF"))); !errors.Is(err, document.ErrUnsupportedMedia) {
			t.Fatalf("pdf: %v", err)
		}
		if _, err := e.Presence.UploadPhoto(ctx, dock, e.MarinaID, id, "image/png", bytes.NewReader([]byte("nope"))); err == nil {
			t.Fatal("garbage accepted")
		}
		fileID, err := e.Presence.UploadPhoto(ctx, dock, e.MarinaID, id, "image/png", bytes.NewReader(pngBytes(t)))
		if err != nil || fileID == "" {
			t.Fatal(err)
		}
		l, err := e.Presence.PhotoLink(ctx, dock, e.MarinaID, id)
		if err != nil {
			t.Fatal(err)
		}
		q := l.URL[strings.Index(l.URL, "?")+1:]
		var exp int64
		var sig string
		for _, kv := range strings.Split(q, "&") {
			k, v, _ := strings.Cut(kv, "=")
			if k == "exp" {
				for _, c := range v {
					exp = exp*10 + int64(c-'0')
				}
			}
			if k == "sig" {
				sig = v
			}
		}
		f, err := e.Presence.OpenPhoto(ctx, dock, e.MarinaID, id, exp, sig)
		if err != nil || f.ContentType != "image/png" || !bytes.Equal(f.Content, pngBytes(t)) {
			t.Fatalf("open %v", err)
		}
		if _, err := e.Presence.OpenPhoto(ctx, dock, e.MarinaID, id, exp, sig+"x"); !errors.Is(err, document.ErrLinkExpired) {
			t.Fatalf("tampered: %v", err)
		}
		e.Clock.Advance(6 * time.Minute)
		if _, err := e.Presence.OpenPhoto(ctx, dock, e.MarinaID, id, exp, sig); !errors.Is(err, document.ErrLinkExpired) {
			t.Fatalf("expired: %v", err)
		}
		yard := e.AddUser(t, "yard@porto.test", auth.RoleYard, e.OrgID, e.MarinaID)
		if _, err := e.Presence.PhotoLink(context.Background(), yard, e.MarinaID, id); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard link: %v", err)
		}
	})
}
