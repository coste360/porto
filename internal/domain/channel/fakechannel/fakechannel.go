// Package fakechannel is an in-memory ext.BookingChannel for tests and development (m8-plan §8.4): reservations
// injected with Inject are returned by PullReservations, pushes are recorded, Webhook builds a signed callback
// request (header Porto-Channel-Signature "t=<unix>,v1=<hex HMAC-SHA256 of t.body>"), and the acknowledgement
// outcome can be made to fail. It is registered in the app only in tests and with PORTO_DEV_FAKE_CHANNEL in
// development. It is goroutine-safe and never touches the store.
package fakechannel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

// Code is the channel code of the fake channel.
const Code = "fake"

// SignatureHeader is the header carrying the webhook signature.
const SignatureHeader = "Porto-Channel-Signature"

// Tolerance is the accepted clock skew of a webhook signature.
const Tolerance = 5 * time.Minute

// Channel is the fake channel.
type Channel struct {
	mu       sync.Mutex
	queue    []ext.ChannelReservation
	avail    []ext.ChannelAvailability
	rates    []ext.ChannelRates
	acks     []ext.ChannelAck
	ackErr   error
	pushErr  error
	pulls    int
	code     string
	maxBatch int
}

// New returns an empty fake channel with code "fake".
func New() *Channel { return &Channel{code: Code, maxBatch: 100} }

// NewWithCode returns a fake channel with another code (several channels in one test).
func NewWithCode(code string) *Channel { return &Channel{code: code, maxBatch: 100} }

var _ ext.BookingChannel = (*Channel)(nil)

// Code implements ext.BookingChannel.
func (c *Channel) Code() string { return c.code }

// Capabilities implements ext.BookingChannel.
func (c *Channel) Capabilities() ext.ChannelCapabilities {
	return ext.ChannelCapabilities{Push: true, Pull: true, Webhook: true, Ack: true}
}

// Inject queues reservations for the next pulls.
func (c *Channel) Inject(res ...ext.ChannelReservation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = append(c.queue, res...)
}

// PushAvailability implements ext.BookingChannel (recorded).
func (c *Channel) PushAvailability(_ context.Context, a ext.ChannelAvailability) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pushErr != nil {
		return c.pushErr
	}
	c.avail = append(c.avail, a)
	return nil
}

// PushRates implements ext.BookingChannel (recorded).
func (c *Channel) PushRates(_ context.Context, r ext.ChannelRates) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pushErr != nil {
		return c.pushErr
	}
	c.rates = append(c.rates, r)
	return nil
}

// PullReservations implements ext.BookingChannel: the cursor is the index of the next queued reservation.
func (c *Channel) PullReservations(_ context.Context, since string) ([]ext.ChannelReservation, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pulls++
	from := 0
	if since != "" {
		n, err := strconv.Atoi(since)
		if err != nil || n < 0 {
			return nil, since, errors.New("fakechannel: bad cursor")
		}
		from = n
	}
	if from > len(c.queue) {
		from = len(c.queue)
	}
	to := min(len(c.queue), from+c.maxBatch)
	out := append([]ext.ChannelReservation(nil), c.queue[from:to]...)
	return out, strconv.Itoa(to), nil
}

// Acknowledge implements ext.BookingChannel (recorded, or the injected error).
func (c *Channel) Acknowledge(_ context.Context, ack ext.ChannelAck) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ackErr != nil {
		return c.ackErr
	}
	c.acks = append(c.acks, ack)
	return nil
}

// FailAcknowledge makes Acknowledge return err (nil restores it).
func (c *Channel) FailAcknowledge(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ackErr = err
}

// FailPush makes the pushes return err (nil restores them).
func (c *Channel) FailPush(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pushErr = err
}

// Pushed returns copies of the recorded availability and rate pushes.
func (c *Channel) Pushed() ([]ext.ChannelAvailability, []ext.ChannelRates) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ext.ChannelAvailability(nil), c.avail...), append([]ext.ChannelRates(nil), c.rates...)
}

// Acks returns a copy of the recorded acknowledgements.
func (c *Channel) Acks() []ext.ChannelAck {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ext.ChannelAck(nil), c.acks...)
}

// Pulls returns how many pulls were made.
func (c *Channel) Pulls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pulls
}

// payload is the webhook body: a list of reservations.
type payload struct {
	Reservations []wireReservation `json:"reservations"`
}

type wireReservation struct {
	EventID       string `json:"event_id"`
	ExternalRef   string `json:"external_ref"`
	Kind          string `json:"kind"`
	MarinaRef     string `json:"marina_ref"`
	CategoryCode  string `json:"category_code"`
	Arrival       string `json:"arrival"`
	Departure     string `json:"departure"`
	LOACm         int64  `json:"loa_cm"`
	BeamCm        int64  `json:"beam_cm"`
	DraftCm       int64  `json:"draft_cm"`
	HullType      string `json:"hull_type"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Country       string `json:"country"`
	Locale        string `json:"locale"`
	AmountCents   int64  `json:"amount_cents"`
	Currency      string `json:"currency"`
	PaidToChannel bool   `json:"paid_to_channel"`
}

func toWire(r ext.ChannelReservation) wireReservation {
	return wireReservation{
		EventID: r.EventID, ExternalRef: r.ExternalRef, Kind: r.Kind, MarinaRef: r.MarinaRef, CategoryCode: r.CategoryCode,
		Arrival: r.Arrival, Departure: r.Departure, LOACm: r.LOACm, BeamCm: r.BeamCm, DraftCm: r.DraftCm, HullType: r.HullType,
		FirstName: r.Guest.FirstName, LastName: r.Guest.LastName, Email: r.Guest.Email, Phone: r.Guest.Phone,
		Country: r.Guest.Country, Locale: r.Guest.Locale, AmountCents: r.AmountCents, Currency: r.Currency,
		PaidToChannel: r.PaidToChannel,
	}
}

func fromWire(w wireReservation, at time.Time) ext.ChannelReservation {
	return ext.ChannelReservation{
		EventID: w.EventID, ExternalRef: w.ExternalRef, Kind: w.Kind, MarinaRef: w.MarinaRef, CategoryCode: w.CategoryCode,
		Arrival: w.Arrival, Departure: w.Departure, LOACm: w.LOACm, BeamCm: w.BeamCm, DraftCm: w.DraftCm, HullType: w.HullType,
		Guest:       ext.ChannelGuest{FirstName: w.FirstName, LastName: w.LastName, Email: w.Email, Phone: w.Phone, Country: w.Country, Locale: w.Locale},
		AmountCents: w.AmountCents, Currency: w.Currency, PaidToChannel: w.PaidToChannel, ReceivedAt: at,
	}
}

// Sign returns the signature header value of body at t.
func Sign(secret []byte, body []byte, t time.Time) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

// Body encodes reservations as a webhook body.
func Body(res ...ext.ChannelReservation) []byte {
	p := payload{Reservations: make([]wireReservation, 0, len(res))}
	for _, r := range res {
		p.Reservations = append(p.Reservations, toWire(r))
	}
	b, _ := json.Marshal(p)
	return b
}

// Webhook builds a signed callback request for target (the webhook URL of a marina) at t.
func Webhook(target string, secret []byte, t time.Time, res ...ext.ChannelReservation) *http.Request {
	body := Body(res...)
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, target, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(SignatureHeader, Sign(secret, body, t))
	return r
}

// ParseWebhook implements ext.BookingChannel: the signature must match the secret and be within Tolerance of now.
func (c *Channel) ParseWebhook(r *http.Request, secret []byte, now time.Time) ([]ext.ChannelReservation, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10+1))
	if err != nil || len(body) > 256<<10 {
		return nil, ext.ErrInvalidSignature
	}
	if !Verify(secret, r.Header.Get(SignatureHeader), body, now) {
		return nil, ext.ErrInvalidSignature
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, ext.ErrIgnoredEvent
	}
	if len(p.Reservations) == 0 {
		return nil, ext.ErrIgnoredEvent
	}
	out := make([]ext.ChannelReservation, 0, len(p.Reservations))
	for _, w := range p.Reservations {
		out = append(out, fromWire(w, now))
	}
	return out, nil
}

// Verify checks a signature header against body and now.
func Verify(secret []byte, header string, body []byte, now time.Time) bool {
	if len(secret) == 0 {
		return false
	}
	var ts, sig string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return false
	}
	at := time.Unix(sec, 0)
	if at.Before(now.Add(-Tolerance)) || at.After(now.Add(Tolerance)) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(Sign(secret, body, at), "t="+ts+",v1="))
	if err != nil {
		return false
	}
	got, err := hex.DecodeString(sig)
	return err == nil && hmac.Equal(want, got)
}
