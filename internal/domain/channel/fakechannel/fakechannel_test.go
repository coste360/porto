package fakechannel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

func TestFakeChannel_Signature(t *testing.T) {
	c := New()
	secret := []byte("0123456789abcdef-secret")
	now := time.Date(2027, 7, 1, 10, 0, 0, 0, time.UTC)
	res := ext.ChannelReservation{
		EventID: "e1", ExternalRef: "r1", Kind: ext.ChannelReservationNew, CategoryCode: "A",
		Arrival: "2027-07-10", Departure: "2027-07-12", LOACm: 1000, BeamCm: 350, PaidToChannel: true,
		Guest: ext.ChannelGuest{LastName: "Rossi", Email: "a@example.org"},
	}
	got, err := c.ParseWebhook(Webhook("http://x/webhooks/channels/fake/m", secret, now, res), secret, now.Add(time.Minute))
	if err != nil || len(got) != 1 || got[0].ExternalRef != "r1" || !got[0].PaidToChannel || got[0].Guest.Email != "a@example.org" {
		t.Fatalf("parse %+v %v", got, err)
	}
	cases := map[string]struct {
		secret []byte
		at     time.Time
	}{
		"wrong secret": {[]byte("another-secret-0123456"), now},
		"too old":      {secret, now.Add(-10 * time.Minute)},
		"empty secret": {nil, now},
	}
	for name, cs := range cases {
		r := Webhook("http://x", secret, cs.at, res)
		if _, err := c.ParseWebhook(r, cs.secret, now); !errors.Is(err, ext.ErrInvalidSignature) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	r := Webhook("http://x", secret, now, res)
	r.Header.Set(SignatureHeader, "t=1,v1=00")
	if _, err := c.ParseWebhook(r, secret, now); !errors.Is(err, ext.ErrInvalidSignature) {
		t.Fatalf("tampered: %v", err)
	}
}

func TestFakeChannel_PullAckPush(t *testing.T) {
	c := New()
	c.Inject(ext.ChannelReservation{EventID: "e1"}, ext.ChannelReservation{EventID: "e2"})
	res, next, err := c.PullReservations(context.Background(), "")
	if err != nil || len(res) != 2 || next != "2" {
		t.Fatalf("pull %v %q %v", res, next, err)
	}
	if res, _, _ := c.PullReservations(context.Background(), next); len(res) != 0 {
		t.Fatalf("second pull %v", res)
	}
	c.FailAcknowledge(errors.New("down"))
	if err := c.Acknowledge(context.Background(), ext.ChannelAck{EventID: "e1"}); err == nil {
		t.Fatal("ack error not injected")
	}
	c.FailAcknowledge(nil)
	_ = c.Acknowledge(context.Background(), ext.ChannelAck{EventID: "e1"})
	_ = c.PushAvailability(context.Background(), ext.ChannelAvailability{MarinaRef: "x"})
	if a, _ := c.Pushed(); len(a) != 1 || len(c.Acks()) != 1 || c.Pulls() != 2 {
		t.Fatalf("records %v %v", a, c.Acks())
	}
}
