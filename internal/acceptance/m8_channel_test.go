//go:build integration

package acceptance

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/coste360/porto/internal/domain/channel"
	"github.com/coste360/porto/internal/domain/channel/fakechannel"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

const m8ChannelSecret = "channel-secret-0123456789"

// connectChannel enables the marina's connection to the fake channel (category EXT-A = L12).
func (w *m8World) connectChannel(t *testing.T) {
	t.Helper()
	secret := m8ChannelSecret
	if _, err := w.h.app.Channels.Save(context.Background(), w.deskPrincipal(t), w.marina, fakechannel.Code, 0, channel.Update{
		Enabled: true, PushEnabled: true, CategoryMap: map[string]string{"EXT-A": "L12"},
		Credentials: map[string]string{"marina_ref": "M-1"}, WebhookSecret: &secret,
	}); err != nil {
		t.Fatal(err)
	}
}

func m8Reservation(ref, eventID, kind string, paid bool) ext.ChannelReservation {
	return ext.ChannelReservation{
		EventID: eventID, ExternalRef: ref, Kind: kind, CategoryCode: "EXT-A", Arrival: m8Arrival, Departure: m8Departure,
		LOACm: 1000, BeamCm: 350, PaidToChannel: paid, AmountCents: 9000, Currency: "EUR",
		Guest: ext.ChannelGuest{FirstName: "Hans", LastName: "Muller", Email: "hans@example.test", Country: "DE", Locale: "en"},
	}
}

// postChannel delivers a signed fake channel callback over HTTP.
func (w *m8World) postChannel(t *testing.T, res ...ext.ChannelReservation) int {
	t.Helper()
	req := fakechannel.Webhook(w.h.srv.URL+"/webhooks/channels/"+fakechannel.Code+"/"+w.marina, []byte(m8ChannelSecret), w.clock.Now(), res...)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func (w *m8World) reservation(t *testing.T, ref string) *store.ChannelReservation {
	t.Helper()
	r := new(store.ChannelReservation)
	if err := w.h.app.DB.NewSelect().Model(r).Where("marina_id = ? AND external_ref = ?", w.marina, ref).Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestChannel_ApplyNewPaidConfirms (ADR 0013): a signed reservation paid to the channel books and confirms with
// source channel, once (INV-89); an unknown channel or a bad signature changes nothing.
func TestChannel_ApplyNewPaidConfirms(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.connectChannel(t)
		res := m8Reservation("EXT-1", "ev-1", ext.ChannelReservationNew, true)
		for range 2 {
			if code := w.postChannel(t, res); code != 200 {
				t.Fatalf("webhook %d", code)
			}
		}
		r := w.reservation(t, "EXT-1")
		if r.Status != store.ChannelResBooked || r.ContractID == "" {
			t.Fatalf("reservation %+v", r)
		}
		c, err := store.GetContract(context.Background(), db.DB, w.scope, r.ContractID)
		if err != nil || c.Status != "confirmed" || c.ConfirmationSource != "channel" || c.Channel != "external" {
			t.Fatalf("transit %+v %v", c, err)
		}
		if acks := w.ch.Acks(); len(acks) != 1 || acks[0].Outcome != ext.ChannelAckAccepted || len(acks[0].Reference) != 8 {
			t.Fatalf("acks %+v", acks)
		}
		n, _ := db.NewSelect().TableExpr("online_bookings").Where("marina_id = ?", w.marina).Count(context.Background())
		if n != 1 {
			t.Fatalf("bookings %d", n)
		}
		bad := fakechannel.Webhook(w.h.srv.URL+"/webhooks/channels/fake/"+w.marina, []byte("wrong-secret-0123456789"), w.clock.Now(), res)
		if resp, _ := http.DefaultClient.Do(bad); resp.StatusCode != 401 {
			t.Fatalf("bad signature %d", resp.StatusCode)
		}
		unknown := fakechannel.Webhook(w.h.srv.URL+"/webhooks/channels/nope/"+w.marina, []byte(m8ChannelSecret), w.clock.Now(), res)
		if resp, _ := http.DefaultClient.Do(unknown); resp.StatusCode != 404 {
			t.Fatalf("unknown channel %d", resp.StatusCode)
		}
		// Unpaid: held for the desk.
		if code := w.postChannel(t, m8Reservation("EXT-2", "ev-2", ext.ChannelReservationNew, false)); code != 200 {
			t.Fatalf("webhook %d", code)
		}
		r2 := w.reservation(t, "EXT-2")
		if c2, _ := store.GetContract(context.Background(), db.DB, w.scope, r2.ContractID); c2 == nil || c2.Status != "held" {
			t.Fatalf("unpaid channel booking %+v", c2)
		}
		w.invariants(t)
	})
}

// TestChannel_NoSpaceRejectsAndRecordsInquiry and TestChannel_CancelReleases: a reservation with no place left is
// rejected (inquiry declined_no_space); a cancel frees the place; a modify is refused.
func TestChannel_NoSpaceRejectsAndRecordsInquiry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.connectChannel(t)
		for i := range 3 {
			if code := w.postChannel(t, m8Reservation("R"+strconv.Itoa(i), "e"+strconv.Itoa(i), ext.ChannelReservationNew, true)); code != 200 {
				t.Fatalf("webhook %d", code)
			}
		}
		if code := w.postChannel(t, m8Reservation("R9", "e9", ext.ChannelReservationNew, true)); code != 200 {
			t.Fatalf("webhook %d", code)
		}
		if r := w.reservation(t, "R9"); r.Status != store.ChannelResRejected || r.RejectReason != store.ChannelRejectNoSpace {
			t.Fatalf("no space %+v", r)
		}
		var n int64
		n, _ = db.NewSelect().TableExpr("inquiries").Where("marina_id = ? AND channel = 'external' AND outcome = 'declined_no_space'", w.marina).Count(context.Background())
		if n != 1 {
			t.Fatalf("inquiries %d", n)
		}
		acks := w.ch.Acks()
		if last := acks[len(acks)-1]; last.Outcome != ext.ChannelAckRejected || last.Reason != ext.ChannelRejectNoSpace {
			t.Fatalf("ack %+v", last)
		}
		// Cancel R0: the transit is cancelled and its berth is free again for R9's retry under another reference.
		if code := w.postChannel(t, m8Reservation("R0", "e-cancel", ext.ChannelReservationCancel, true)); code != 200 {
			t.Fatalf("cancel %d", code)
		}
		r0 := w.reservation(t, "R0")
		if c, _ := store.GetContract(context.Background(), db.DB, w.scope, r0.ContractID); r0.Status != store.ChannelResCancelled || c.Status != "cancelled" {
			t.Fatalf("cancel %+v %s", r0, c.Status)
		}
		if code := w.postChannel(t, m8Reservation("R10", "e10", ext.ChannelReservationNew, true)); code != 200 {
			t.Fatalf("webhook %d", code)
		}
		if r := w.reservation(t, "R10"); r.Status != store.ChannelResBooked {
			t.Fatalf("after cancel %+v", r)
		}
		if code := w.postChannel(t, m8Reservation("R1", "e-mod", ext.ChannelReservationModify, true)); code != 200 {
			t.Fatalf("modify %d", code)
		}
		acks = w.ch.Acks()
		if last := acks[len(acks)-1]; last.Reason != ext.ChannelRejectModifyUnsupported {
			t.Fatalf("modify ack %+v", last)
		}
		w.invariants(t)
	})
}

// TestChannel_PushAfterOccupancyChange: the first push is a full push; an occupancy change marks the connection
// dirty and the next push sends fewer units. Pull applies injected reservations.
func TestChannel_PushAfterOccupancyChange(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newM8World(t, db, true)
		w.connectChannel(t)
		w.h.app.Channels.SetPushDays(12)
		w.h.runJobs("channels.push")
		avail, rates := w.ch.Pushed()
		if len(avail) != 1 || len(rates) != 1 || avail[0].MarinaRef != "M-1" {
			t.Fatalf("first push %+v %+v", avail, rates)
		}
		units := func(a ext.ChannelAvailability) int {
			for _, n := range a.Categories[0].Nights {
				if n.Night == m8Arrival {
					return n.Units
				}
			}
			return -1
		}
		if units(avail[0]) != 3 {
			t.Fatalf("units %d", units(avail[0]))
		}
		w.h.runJobs("channels.push") // nothing changed: no push
		if a, _ := w.ch.Pushed(); len(a) != 1 {
			t.Fatalf("pushed again without changes: %d", len(a))
		}
		w.mustCreate(t, "k-push-0001")
		if err := w.h.app.Channels.Handle(context.Background(), ext.Event{Type: "occupancy.allocated", MarinaID: w.marina}); err != nil {
			t.Fatal(err)
		}
		w.h.runJobs("channels.push")
		avail, _ = w.ch.Pushed()
		if len(avail) != 2 || units(avail[1]) != 2 {
			t.Fatalf("push after change %d units %d", len(avail), units(avail[len(avail)-1]))
		}
		w.ch.Inject(m8Reservation("PULL-1", "pe-1", ext.ChannelReservationNew, true))
		w.h.runJobs("channels.pull")
		if r := w.reservation(t, "PULL-1"); r.Status != store.ChannelResBooked {
			t.Fatalf("pulled %+v", r)
		}
		w.invariants(t)
	})
}
