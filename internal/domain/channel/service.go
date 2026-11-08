package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/booking"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB       *store.DB
	Clock    clock.Clock
	Audit    *audit.Writer
	Outbox   *event.Outbox
	Log      *slog.Logger
	Keys     *crypto.KeyRing
	Booking  *booking.Service
	Channels []ext.BookingChannel
	BaseURL  string
}

// Push horizon, full push period and the reference size of pushed rates (m8-plan §8.2).
const (
	PushDays       = 365
	FullPushEvery  = 6 * time.Hour
	RefLOACm       = 1000
	RefBeamCm      = 350
	applyAttempts  = 3
	maxCategoryMap = 100
)

// Service manages channel connections and reservations.
type Service struct {
	d        Deps
	running  sync.Map // connection id → struct{} (one pull or push at a time per connection)
	pushDays int
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	d.BaseURL = strings.TrimRight(d.BaseURL, "/")
	return &Service{d: d, pushDays: PushDays}
}

// SetPushDays changes the push horizon (tests).
func (s *Service) SetPushDays(n int) { s.pushDays = max(n, 1) }

// Registered returns the registered channel drivers.
func (s *Service) Registered() []ext.BookingChannel { return s.d.Channels }

func (s *Service) driver(code string) (ext.BookingChannel, error) {
	for _, c := range s.d.Channels {
		if c.Code() == code {
			return c, nil
		}
	}
	return nil, ErrUnknownChannel
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func secretAAD(marinaID, code string) string { return "channel:" + marinaID + ":" + code }

// WebhookURL is the callback URL of a marina's connection.
func (s *Service) WebhookURL(code, marinaID string) string {
	return s.d.BaseURL + "/webhooks/channels/" + code + "/" + marinaID
}

func (s *Service) connectionView(code, marinaID string, c *store.ChannelConnection) *Connection {
	if c == nil {
		return nil
	}
	m := map[string]string{}
	_ = json.Unmarshal(c.CategoryMap, &m)
	return &Connection{
		Enabled: c.Enabled, PushEnabled: c.PushEnabled, CategoryMap: m, HasCredentials: c.CredentialsEnc != "",
		HasWebhookSecret: c.WebhookSecretEnc != "", WebhookURL: s.WebhookURL(code, marinaID), LastPushAt: c.LastPushAt,
		LastPullAt: c.LastPullAt, LastError: c.LastError, Version: c.Version,
	}
}

// List returns the registered channels with the marina's connections (providers.configure).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string) ([]Info, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return nil, err
	}
	conns, err := store.ChannelConnections(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(s.d.Channels))
	for _, d := range s.d.Channels {
		info := Info{Code: d.Code(), Capabilities: d.Capabilities()}
		if c, ok := conns[d.Code()]; ok {
			info.Connection = s.connectionView(d.Code(), scope.ID(), &c)
		}
		out = append(out, info)
	}
	return out, nil
}

// Save creates or updates the connection to a channel (providers.configure, ifMatch 0 when creating).
func (s *Service) Save(ctx context.Context, p *auth.Principal, marinaID, code string, ifMatch int, in Update) (*Info, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return nil, err
	}
	drv, err := s.driver(code)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if len(in.CategoryMap) > maxCategoryMap {
		v.Add("category_map", "too_many")
	}
	for k, val := range in.CategoryMap {
		if k == "" || len(k) > 64 || val == "" || len(val) > 64 {
			v.Add("category_map", "invalid")
			break
		}
	}
	if in.WebhookSecret != nil && (len(*in.WebhookSecret) < 16 || len(*in.WebhookSecret) > 256) {
		v.Add("webhook_secret", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	aad := secretAAD(scope.ID(), code)
	var out *Info
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		c, err := store.LockChannelConnection(ctx, tx, scope, code)
		creating := errors.Is(err, store.ErrNotFound)
		if err != nil && !creating {
			return err
		}
		if creating {
			if ifMatch != 0 {
				return ErrVersionMismatch
			}
			c = &store.ChannelConnection{ID: store.NewID(), MarinaID: scope.ID(), ChannelCode: code, Version: 1, CreatedAt: now, UpdatedAt: now}
		} else if c.Version != ifMatch {
			return ErrVersionMismatch
		}
		var fields []string
		c.Enabled, c.PushEnabled, c.PushDirty = in.Enabled, in.PushEnabled, in.PushEnabled
		c.CategoryMap = store.MustJSON(nonNil(in.CategoryMap))
		if in.Credentials != nil {
			raw, err := json.Marshal(in.Credentials)
			if err != nil {
				return err
			}
			if c.CredentialsEnc, err = s.d.Keys.EncryptString(string(raw), aad); err != nil {
				return err
			}
			fields = append(fields, "credentials")
		}
		if in.WebhookSecret != nil {
			if c.WebhookSecretEnc, err = s.d.Keys.EncryptString(*in.WebhookSecret, aad); err != nil {
				return err
			}
			fields = append(fields, "webhook_secret")
		}
		if creating {
			if err := store.InsertChannelConnection(ctx, tx, c); err != nil {
				return err
			}
		} else if err := store.UpdateChannelConnection(ctx, tx, scope, ifMatch, c, now); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "channel.update", EntityType: "channel_connection",
			EntityID: c.ID, Diff: map[string]any{
				"channel_code": code, "enabled": c.Enabled, "push_enabled": c.PushEnabled,
				"categories": len(in.CategoryMap), "secrets": fields,
			},
		}); err != nil {
			return err
		}
		out = &Info{Code: code, Capabilities: drv.Capabilities(), Connection: s.connectionView(code, scope.ID(), c)}
		return nil
	})
	return out, err
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// credentials returns ctx with the decrypted credentials of a connection.
func (s *Service) credentials(ctx context.Context, c *store.ChannelConnection) (context.Context, error) {
	creds := map[string]string{}
	if c.CredentialsEnc != "" {
		raw, err := s.d.Keys.DecryptString(c.CredentialsEnc, secretAAD(c.MarinaID, c.ChannelCode))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &creds); err != nil {
			return nil, err
		}
	}
	return ext.WithChannelCredentials(ctx, creds), nil
}

// errorCode reduces a driver error to a code that may be stored and shown (never a payload).
func errorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ext.ErrNotSupported):
		return "not_supported"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return "channel_error"
}

// Test checks the connection's credentials against the channel (providers.configure).
func (s *Service) Test(ctx context.Context, p *auth.Principal, marinaID, code string) (*TestResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return nil, err
	}
	drv, err := s.driver(code)
	if err != nil {
		return nil, err
	}
	c, err := store.GetChannelConnection(ctx, s.d.DB.DB, scope, code)
	if err != nil {
		return nil, ErrNoConnection
	}
	cctx, err := s.credentials(ctx, c)
	if err != nil {
		return &TestResult{ErrorCode: "credentials"}, nil
	}
	if drv.Capabilities().Pull {
		_, _, err = drv.PullReservations(cctx, c.PullCursor)
	} else {
		err = drv.PushAvailability(cctx, ext.ChannelAvailability{MarinaRef: ext.ChannelCredentials(cctx)["marina_ref"]})
	}
	return &TestResult{OK: err == nil, ErrorCode: errorCode(err)}, nil
}

// Sync pulls reservations and pushes availability and rates now (providers.configure).
func (s *Service) Sync(ctx context.Context, p *auth.Principal, marinaID, code string) (*SyncResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermProvidersConfigure)
	if err != nil {
		return nil, err
	}
	drv, err := s.driver(code)
	if err != nil {
		return nil, err
	}
	c, err := store.GetChannelConnection(ctx, s.d.DB.DB, scope, code)
	if err != nil || !c.Enabled {
		return nil, ErrNoConnection
	}
	res := &SyncResult{}
	if drv.Capabilities().Pull {
		if res.Pulled, res.Booked, res.Rejected, err = s.pull(ctx, scope, drv, c); err != nil {
			return nil, err
		}
	}
	if drv.Capabilities().Push && c.PushEnabled {
		if err := s.push(ctx, scope, drv, c, true); err != nil {
			return nil, err
		}
		res.Pushed = true
	}
	return res, nil
}

// Reservations lists the received reservations, newest first (transits.desk).
func (s *Service) Reservations(ctx context.Context, p *auth.Principal, marinaID string, f ReservationFilter) (*ReservationPage, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermTransitsDesk)
	if err != nil {
		return nil, err
	}
	page, err := store.ListChannelReservations(ctx, s.d.DB.DB, scope, store.ChannelReservationFilter{Channel: f.Channel, Status: f.Status}, f.Cursor, f.Limit)
	if err != nil {
		return nil, err
	}
	out := &ReservationPage{Items: []Reservation{}, NextCursor: page.NextCursor}
	for _, r := range page.Items {
		out.Items = append(out.Items, Reservation{
			ID: r.ID, ChannelCode: r.ChannelCode, ExternalRef: r.ExternalRef, Status: r.Status, RejectReason: r.RejectReason,
			ContractID: r.ContractID, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// WebhookChannel returns the driver and the decrypted webhook secret of the marina's enabled connection to a
// channel (ErrUnknownChannel, ErrNoConnection). The HTTP handler verifies the callback with
// driver.ParseWebhook(r, secret, clock.Now()) (domain packages do not import net/http), then calls ApplyWebhook.
func (s *Service) WebhookChannel(ctx context.Context, code, marinaID string) (ext.BookingChannel, []byte, error) {
	drv, err := s.driver(code)
	if err != nil {
		return nil, nil, err
	}
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return nil, nil, ErrNoConnection
	}
	c, err := store.GetChannelConnection(ctx, s.d.DB.DB, scope, code)
	if err != nil || !c.Enabled || c.WebhookSecretEnc == "" || !drv.Capabilities().Webhook {
		return nil, nil, ErrNoConnection
	}
	secret, err := s.d.Keys.DecryptString(c.WebhookSecretEnc, secretAAD(scope.ID(), code))
	if err != nil {
		return nil, nil, ErrNoConnection
	}
	return drv, []byte(secret), nil
}

// ApplyWebhook applies the verified reservations of a callback: each one in its own transaction, once (INV-89),
// acknowledged after commit.
func (s *Service) ApplyWebhook(ctx context.Context, code, marinaID string, res []ext.ChannelReservation) error {
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return ErrNoConnection
	}
	var errs []error
	for _, r := range res {
		if err := s.ApplyReservation(ctx, scope, code, r); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// outcome of one applied reservation, acknowledged after commit.
type outcome struct {
	ack     *ext.ChannelAck
	booked  bool
	ignored bool
}

// errRetry rolls a reservation's transaction back so it is applied again (a solution was taken meanwhile).
var errRetry = errors.New("channel: retry")

// ApplyReservation applies one reservation event of a channel to the marina (new, cancel, modify) in its own
// transaction and acknowledges it after commit.
func (s *Service) ApplyReservation(ctx context.Context, scope store.MarinaScope, code string, res ext.ChannelReservation) error {
	_, err := s.apply(ctx, scope, code, res)
	return err
}

func (s *Service) apply(ctx context.Context, scope store.MarinaScope, code string, res ext.ChannelReservation) (outcome, error) {
	drv, err := s.driver(code)
	if err != nil {
		return outcome{}, err
	}
	if res.EventID == "" || res.ExternalRef == "" || len(res.EventID) > 128 || len(res.ExternalRef) > 128 {
		return outcome{}, fmt.Errorf("channel: reservation without event id or reference: %w", ErrInvalidReservation)
	}
	var out outcome
	for attempt := 1; ; attempt++ {
		lastTry := attempt == applyAttempts
		out = outcome{}
		var plan *booking.ChannelPlan
		if res.Kind == ext.ChannelReservationNew {
			// Search and price before the write transaction (SQLite has one writer; PostgreSQL holds no lock meanwhile).
			if plan, err = s.plan(ctx, scope, code, res); err != nil {
				return out, err
			}
		}
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			var err error
			out, err = s.applyTx(ctx, tx, scope, code, res, lastTry, plan)
			return err
		})
		if errors.Is(err, errRetry) && !lastTry {
			continue
		}
		break
	}
	if errors.Is(err, errRetryExhausted) {
		// Every search lost its solution to a concurrent booking: the reservation is rejected for lack of space.
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			var err error
			out, err = s.applyNoSpaceTx(ctx, tx, scope, code, res)
			return err
		})
	}
	if err != nil {
		return out, err
	}
	if out.ack != nil && drv.Capabilities().Ack {
		c, cerr := store.GetChannelConnection(ctx, s.d.DB.DB, scope, code)
		if cerr == nil {
			if cctx, cerr := s.credentials(ctx, c); cerr == nil {
				if aerr := drv.Acknowledge(cctx, *out.ack); aerr != nil {
					s.d.Log.WarnContext(ctx, "channel acknowledgement failed", "marina_id", scope.ID(), "channel", code, "code", errorCode(aerr))
				}
			}
		}
	}
	return out, nil
}

// ErrInvalidReservation means a reservation lacks its identifiers (it is not recorded).
var ErrInvalidReservation = errors.New("channel: invalid reservation")

func payloadHash(res ext.ChannelReservation) string {
	b, _ := json.Marshal(struct {
		Kind, Category, Arrival, Departure string
		LOA, Beam, Draft, Amount           int64
		Paid                               bool
	}{res.Kind, res.CategoryCode, res.Arrival, res.Departure, res.LOACm, res.BeamCm, res.DraftCm, res.AmountCents, res.PaidToChannel})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func rejected(res ext.ChannelReservation, reason string) *ext.ChannelAck {
	r := reason
	if r == store.ChannelRejectNotFound {
		r = ext.ChannelRejectInvalid
	}
	return &ext.ChannelAck{ExternalRef: res.ExternalRef, EventID: res.EventID, Outcome: ext.ChannelAckRejected, Reason: r}
}

// applyTx applies a reservation event in tx: the event row first (applied once, INV-89), then the reservation.
func (s *Service) applyTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, code string, res ext.ChannelReservation, lastTry bool,
	plan *booking.ChannelPlan,
) (outcome, error) {
	now := s.now()
	ev := &store.ChannelEvent{ID: store.NewID(), MarinaID: scope.ID(), ChannelCode: code, EventID: res.EventID, Status: store.ChannelEventApplied, ReceivedAt: now, CreatedAt: now}
	inserted, err := store.InsertChannelEvent(ctx, tx, ev)
	if err != nil {
		return outcome{}, err
	}
	if !inserted {
		return outcome{ignored: true}, nil // the same event again: already applied
	}
	conn, err := store.GetChannelConnection(ctx, tx, scope, code)
	if err != nil || !conn.Enabled {
		return outcome{}, ErrNoConnection
	}
	who := contract.ChannelActor(code)
	switch res.Kind {
	case ext.ChannelReservationNew:
		row := &store.ChannelReservation{
			ID: store.NewID(), MarinaID: scope.ID(), ChannelCode: code, ExternalRef: res.ExternalRef, Status: store.ChannelResReceived,
			PayloadSHA256: payloadHash(res), CreatedAt: now, UpdatedAt: now,
		}
		ok, err := store.InsertChannelReservation(ctx, tx, row)
		if err != nil {
			return outcome{}, err
		}
		if !ok {
			// The reservation exists already (a new event for a known reference): nothing more to book.
			return outcome{ignored: true}, store.SetChannelEventOutcome(ctx, tx, scope, ev.ID, store.ChannelEventIgnored, "")
		}
		if err := store.SetChannelEventOutcome(ctx, tx, scope, ev.ID, store.ChannelEventApplied, row.ID); err != nil {
			return outcome{}, err
		}
		cmap := map[string]string{}
		_ = json.Unmarshal(conn.CategoryMap, &cmap)
		category := cmap[res.CategoryCode]
		reject := func(reason string) (outcome, error) {
			row.Status, row.RejectReason = store.ChannelResRejected, reason
			return outcome{ack: rejected(res, reason)}, store.UpdateChannelReservation(ctx, tx, scope, row, now)
		}
		if category == "" {
			return reject(store.ChannelRejectUnmappedCategory)
		}
		in, ok := channelInput(code, category, res)
		if !ok {
			return reject(store.ChannelRejectInvalid)
		}
		in.Plan = plan
		r, err := s.d.Booking.CreateChannelBookingTx(ctx, tx, scope, in)
		if errors.Is(err, occupancy.ErrResourceTaken) || errors.Is(err, occupancy.ErrQuotaExhausted) || errors.Is(err, occupancy.ErrClosed) {
			if !lastTry {
				return outcome{}, errRetry
			}
			return outcome{}, errRetryExhausted
		}
		if err != nil {
			return outcome{}, err
		}
		if !r.Booked {
			return reject(r.RejectReason)
		}
		row.Status, row.ContractID = store.ChannelResBooked, r.ContractID
		if err := store.UpdateChannelReservation(ctx, tx, scope, row, now); err != nil {
			return outcome{}, err
		}
		return outcome{booked: true, ack: &ext.ChannelAck{ExternalRef: res.ExternalRef, EventID: res.EventID, Outcome: ext.ChannelAckAccepted, Reference: r.Reference}}, nil
	case ext.ChannelReservationCancel:
		row, err := store.LockChannelReservation(ctx, tx, scope, code, res.ExternalRef)
		if errors.Is(err, store.ErrNotFound) {
			return outcome{ack: rejected(res, store.ChannelRejectNotFound)}, store.SetChannelEventOutcome(ctx, tx, scope, ev.ID, store.ChannelEventIgnored, "")
		}
		if err != nil {
			return outcome{}, err
		}
		if err := store.SetChannelEventOutcome(ctx, tx, scope, ev.ID, store.ChannelEventApplied, row.ID); err != nil {
			return outcome{}, err
		}
		if row.Status != store.ChannelResBooked {
			return outcome{}, nil
		}
		if _, err := s.d.Booking.CancelChannelBookingTx(ctx, tx, scope, who, row.ContractID); err != nil {
			return outcome{}, err
		}
		row.Status = store.ChannelResCancelled
		return outcome{ack: &ext.ChannelAck{ExternalRef: res.ExternalRef, EventID: res.EventID, Outcome: ext.ChannelAckAccepted}},
			store.UpdateChannelReservation(ctx, tx, scope, row, now)
	case ext.ChannelReservationModify:
		// m8-plan §16 D15: channels send cancel plus new; a modify is refused and the office is told.
		rid := ""
		if row, err := store.LockChannelReservation(ctx, tx, scope, code, res.ExternalRef); err == nil {
			rid = row.ID
			if err := store.RaiseWorkItem(ctx, tx, scope, WorkModifyUnsupported, row.ID, "event:"+res.EventID, now); err != nil {
				return outcome{}, err
			}
		}
		if err := store.SetChannelEventOutcome(ctx, tx, scope, ev.ID, store.ChannelEventIgnored, rid); err != nil {
			return outcome{}, err
		}
		return outcome{ack: rejected(res, store.ChannelRejectModifyUnsupported)}, nil
	}
	return outcome{ack: rejected(res, store.ChannelRejectInvalid)}, store.SetChannelEventOutcome(ctx, tx, scope, ev.ID, store.ChannelEventIgnored, "")
}

// WorkModifyUnsupported is the worklist item of a refused channel modification.
const WorkModifyUnsupported = "channel_modify"

// errRetryExhausted turns the last lost race into a no-space rejection (applied by applyRejectedTx).
var errRetryExhausted = errors.New("channel: no solution left")

// RunPull pulls every enabled connection (job channels.pull, every 5 minutes, one run per connection at a time).
func (s *Service) RunPull(ctx context.Context) error {
	conns, err := store.EnabledChannelConnections(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var errs []error
	for i := range conns {
		c := &conns[i]
		drv, err := s.driver(c.ChannelCode)
		if err != nil || !drv.Capabilities().Pull {
			continue
		}
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, c.MarinaID)
		if err != nil {
			continue
		}
		if _, _, _, err := s.pull(ctx, scope, drv, c); err != nil && !errors.Is(err, ErrSyncRunning) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// pull fetches the reservations after the stored cursor and applies them; the cursor advances only when every
// reservation was applied.
func (s *Service) pull(ctx context.Context, scope store.MarinaScope, drv ext.BookingChannel, c *store.ChannelConnection) (pulled, booked, rej int, err error) {
	if _, busy := s.running.LoadOrStore("pull:"+c.ID, struct{}{}); busy {
		return 0, 0, 0, ErrSyncRunning
	}
	defer s.running.Delete("pull:" + c.ID)
	cctx, err := s.credentials(ctx, c)
	if err != nil {
		return 0, 0, 0, err
	}
	cur, err := store.GetChannelConnection(ctx, s.d.DB.DB, scope, c.ChannelCode)
	if err != nil {
		return 0, 0, 0, err
	}
	res, next, perr := drv.PullReservations(cctx, cur.PullCursor)
	if perr != nil {
		return 0, 0, 0, store.SetChannelPullState(ctx, s.d.DB.DB, scope, c.ID, "", errorCode(perr), s.now())
	}
	var failed error
	for _, r := range res {
		out, err := s.apply(ctx, scope, c.ChannelCode, r)
		if err != nil {
			failed = err
			continue
		}
		if out.ignored {
			continue
		}
		pulled++
		if out.booked {
			booked++
		} else if out.ack != nil && out.ack.Outcome == ext.ChannelAckRejected {
			rej++
		}
	}
	if failed != nil {
		_ = store.SetChannelPullState(ctx, s.d.DB.DB, scope, c.ID, "", "apply_failed", s.now())
		return pulled, booked, rej, failed
	}
	return pulled, booked, rej, store.SetChannelPullState(ctx, s.d.DB.DB, scope, c.ID, next, "", s.now())
}

// RunPush pushes availability and rates of the dirty connections, and a full push every 6 hours (job
// channels.push, every minute).
func (s *Service) RunPush(ctx context.Context) error {
	conns, err := store.EnabledChannelConnections(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	now := s.now()
	var errs []error
	for i := range conns {
		c := &conns[i]
		drv, err := s.driver(c.ChannelCode)
		if err != nil || !drv.Capabilities().Push || !c.PushEnabled {
			continue
		}
		full := c.LastFullPushAt.IsZero() || now.Sub(c.LastFullPushAt) >= FullPushEvery
		if !c.PushDirty && !full {
			continue
		}
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, c.MarinaID)
		if err != nil {
			continue
		}
		if err := s.push(ctx, scope, drv, c, full); err != nil && !errors.Is(err, ErrSyncRunning) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// push sends the availability (and, on a full push, the rates) of the mapped categories for the next pushDays
// nights: per night, the free online solutions of the category for a reference vessel (quotas already applied).
func (s *Service) push(ctx context.Context, scope store.MarinaScope, drv ext.BookingChannel, c *store.ChannelConnection, full bool) error {
	if _, busy := s.running.LoadOrStore("push:"+c.ID, struct{}{}); busy {
		return ErrSyncRunning
	}
	defer s.running.Delete("push:" + c.ID)
	cctx, err := s.credentials(ctx, c)
	if err != nil {
		return err
	}
	cmap := map[string]string{}
	_ = json.Unmarshal(c.CategoryMap, &cmap)
	if len(cmap) == 0 {
		return store.SetChannelPushState(ctx, s.d.DB.DB, scope, c.ID, full, "", s.now())
	}
	nights, err := s.d.Booking.ChannelNights(ctx, scope, s.pushDays, booking.Vessel{LOACm: RefLOACm, BeamCm: RefBeamCm}, full)
	if err != nil {
		return err
	}
	ref := ext.ChannelCredentials(cctx)["marina_ref"]
	avail := ext.ChannelAvailability{MarinaRef: ref}
	rates := ext.ChannelRates{MarinaRef: ref, Currency: "EUR", RefLOACm: RefLOACm, RefBeamCm: RefBeamCm}
	for extCode, porto := range cmap {
		ac := ext.ChannelCategory{Code: extCode}
		rc := ext.ChannelCategory{Code: extCode}
		for _, n := range nights[porto] {
			day := n.Night.String()
			ac.Nights = append(ac.Nights, ext.ChannelNight{Night: day, Available: n.Units > 0, Units: n.Units})
			rc.Nights = append(rc.Nights, ext.ChannelNight{Night: day, Available: n.Units > 0, Units: n.Units, Cents: int64(n.Cents)})
		}
		avail.Categories = append(avail.Categories, ac)
		rates.Categories = append(rates.Categories, rc)
	}
	perr := drv.PushAvailability(cctx, avail)
	if perr == nil && full {
		perr = drv.PushRates(cctx, rates)
	}
	return store.SetChannelPushState(ctx, s.d.DB.DB, scope, c.ID, full, errorCode(perr), s.now())
}

// MarkDirty flags the connections of a marina for the next push (outbox subscriber on occupancy.*, transit.*,
// config.* events).
func (s *Service) MarkDirty(ctx context.Context, marinaID string) error {
	if !store.ValidID(marinaID) {
		return nil
	}
	return store.MarkChannelsDirty(ctx, s.d.DB.DB, marinaID)
}

// applyNoSpaceTx records a new reservation as rejected for lack of space (event and reservation once, INV-89).
func (s *Service) applyNoSpaceTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, code string, res ext.ChannelReservation) (outcome, error) {
	now := s.now()
	ev := &store.ChannelEvent{ID: store.NewID(), MarinaID: scope.ID(), ChannelCode: code, EventID: res.EventID, Status: store.ChannelEventApplied, ReceivedAt: now, CreatedAt: now}
	inserted, err := store.InsertChannelEvent(ctx, tx, ev)
	if err != nil || !inserted {
		return outcome{ignored: !inserted}, err
	}
	row := &store.ChannelReservation{
		ID: store.NewID(), MarinaID: scope.ID(), ChannelCode: code, ExternalRef: res.ExternalRef, Status: store.ChannelResRejected,
		RejectReason: store.ChannelRejectNoSpace, PayloadSHA256: payloadHash(res), CreatedAt: now, UpdatedAt: now,
	}
	ok, err := store.InsertChannelReservation(ctx, tx, row)
	if err != nil {
		return outcome{}, err
	}
	if !ok {
		return outcome{ignored: true}, store.SetChannelEventOutcome(ctx, tx, scope, ev.ID, store.ChannelEventIgnored, "")
	}
	if err := store.SetChannelEventOutcome(ctx, tx, scope, ev.ID, store.ChannelEventApplied, row.ID); err != nil {
		return outcome{}, err
	}
	arr, err1 := calendar.Parse(res.Arrival)
	dep, err2 := calendar.Parse(res.Departure)
	if err1 == nil && err2 == nil && dep.After(arr) && res.LOACm > 0 && res.BeamCm > 0 {
		if err := store.InsertAnonymousInquiry(ctx, tx, &store.Inquiry{
			MarinaID: scope.ID(), Channel: store.OnlineChannelExternal, Source: "channel", LOACm: res.LOACm, BeamCm: res.BeamCm,
			DraftCm: res.DraftCm, Arrival: arr, Departure: dep, Outcome: store.InquiryDeclinedNoSpace,
		}, now); err != nil {
			return outcome{}, err
		}
	}
	return outcome{ack: rejected(res, store.ChannelRejectNoSpace)}, nil
}

// Name implements ext.EventSink.
func (s *Service) Name() string { return "channels" }

// Handle implements ext.EventSink: an occupancy, transit or configuration event of a marina marks its push-enabled
// connections dirty (the push itself runs in the channels.push job, never in the dispatcher).
func (s *Service) Handle(ctx context.Context, ev ext.Event) error {
	if ev.MarinaID == "" || len(s.d.Channels) == 0 {
		return nil
	}
	for _, p := range []string{"occupancy.", "transit.", "config.", "closure."} {
		if strings.HasPrefix(ev.Type, p) {
			return s.MarkDirty(ctx, ev.MarinaID)
		}
	}
	return nil
}

var _ ext.EventSink = (*Service)(nil)

// channelInput converts a reservation to the booking input (false when its dates are not valid).
func channelInput(code, category string, res ext.ChannelReservation) (booking.ChannelBookingInput, bool) {
	arr, err1 := calendar.Parse(res.Arrival)
	dep, err2 := calendar.Parse(res.Departure)
	if err1 != nil || err2 != nil {
		return booking.ChannelBookingInput{}, false
	}
	return booking.ChannelBookingInput{
		ChannelCode: code, ExternalRef: res.ExternalRef, Category: category, Arrival: arr, Departure: dep,
		Vessel: booking.Vessel{LOACm: geo.Cm(res.LOACm), BeamCm: geo.Cm(res.BeamCm), DraftCm: geo.Cm(res.DraftCm), HullType: res.HullType},
		Guest: booking.CustomerDetails{
			FirstName: res.Guest.FirstName, LastName: res.Guest.LastName, Email: res.Guest.Email, Phone: res.Guest.Phone,
			Country: res.Guest.Country, Locale: res.Guest.Locale,
		},
		AmountCents: money.Cents(res.AmountCents), PaidToChannel: res.PaidToChannel,
	}, true
}

// plan computes the read-only part of a new reservation before its transaction (nil when the reservation is
// rejected anyway: unmapped category or invalid dates are decided in the transaction).
func (s *Service) plan(ctx context.Context, scope store.MarinaScope, code string, res ext.ChannelReservation) (*booking.ChannelPlan, error) {
	conn, err := store.GetChannelConnection(ctx, s.d.DB.DB, scope, code)
	if err != nil {
		return nil, nil
	}
	cmap := map[string]string{}
	_ = json.Unmarshal(conn.CategoryMap, &cmap)
	category := cmap[res.CategoryCode]
	if category == "" {
		return nil, nil
	}
	in, ok := channelInput(code, category, res)
	if !ok {
		return nil, nil
	}
	return s.d.Booking.PlanChannelBooking(ctx, scope, in)
}
