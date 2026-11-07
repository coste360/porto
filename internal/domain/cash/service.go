// Package cash runs the cash registers of a marina (spec 16.3; m5-plan §5.1, §5.3): sessions with
// float, movements, the close with counted amounts and difference, the Z report, counter sales and courtesy
// receipts ("Non valido ai fini fiscali"). At most one open session per register (INV-55, DB partial unique).
package cash

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/cash/summary"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB       *store.DB
	Clock    clock.Clock
	Audit    *audit.Writer
	Outbox   *event.Outbox
	Charges  *charge.Service
	Payments *payment.Service
	Fiscal   *fiscal.Service
	Prices   *pricelist.Service
	Docs     *docs.Service
	Notify   *notify.Service
	Log      *slog.Logger
}

// Service is the cash service.
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{d: d}
}

// Register is a cash register with its open session (if any).
type Register struct {
	store.CashRegister
	OpenSessionID string
}

// Session is a cash session with its live (open) or frozen (closed) summary and its movements.
type Session struct {
	store.CashSession
	RegisterCode string
	Summary      summary.Result
	Movements    []store.CashMovement
}

// RegisterInput creates a register.
type RegisterInput struct {
	Code   string
	Name   string
	Active bool
}

// RegisterPatch changes a register (nil = unchanged).
type RegisterPatch struct {
	Name      *string
	Active    *bool
	IsDefault *bool
}

// authorizeCollect accepts payments.collect or payments.collect_cash; cashOnly is true for the latter alone.
func (s *Service) authorizeCollect(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, bool, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	if err == nil {
		return scope, false, nil
	}
	if !errors.Is(err, auth.ErrForbidden) {
		return scope, false, err
	}
	scope, err = p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollectCash)
	return scope, true, err
}

// EnsureDefaultRegister returns the default register, creating "main" lazily.
func (s *Service) EnsureDefaultRegister(ctx context.Context, tx bun.IDB, scope store.MarinaScope) (*store.CashRegister, error) {
	return store.EnsureDefaultCashRegister(ctx, tx, scope, defaultRegisterName(ctx, tx, scope), s.now())
}

func defaultRegisterName(ctx context.Context, db bun.IDB, scope store.MarinaScope) string {
	if m, err := store.GetMarina(ctx, db, scope); err == nil && strings.HasPrefix(m.DefaultLocale, "en") {
		return "Main register"
	}
	return "Cassa principale"
}

// ListRegisters returns the registers (payments.collect or payments.collect_cash); the default one is created on
// first use.
func (s *Service) ListRegisters(ctx context.Context, p *auth.Principal, marinaID string) ([]Register, error) {
	scope, _, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureDefault(ctx, scope); err != nil {
		return nil, err
	}
	rows, err := store.ListCashRegisters(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	open, err := store.OpenCashSessionIDs(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Register, 0, len(rows))
	for _, r := range rows {
		out = append(out, Register{CashRegister: r, OpenSessionID: open[r.ID]})
	}
	return out, nil
}

func (s *Service) ensureDefault(ctx context.Context, scope store.MarinaScope) error {
	if _, err := store.DefaultCashRegister(ctx, s.d.DB.DB, scope); err == nil {
		return nil
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, err := s.EnsureDefaultRegister(ctx, tx, scope)
		return err
	})
}

// CreateRegister creates a register (marina.configure).
func (s *Service) CreateRegister(ctx context.Context, p *auth.Principal, marinaID string, in RegisterInput) (*Register, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	code := strings.ToLower(strings.TrimSpace(in.Code))
	name := strings.TrimSpace(in.Name)
	v := &auth.ValidationError{}
	if code == "" || len(code) > 32 {
		v.Add("code", "invalid")
	}
	if len(name) > 120 {
		v.Add("name", "too_long")
	}
	if len(v.Fields) > 0 {
		return nil, v
	}
	now := s.now()
	row := &store.CashRegister{ID: store.NewID(), MarinaID: scope.ID(), Code: code, Name: name, Active: in.Active, Version: 1, CreatedAt: now, UpdatedAt: now}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := s.EnsureDefaultRegister(ctx, tx, scope); err != nil {
			return err
		}
		if err := store.InsertCashRegister(ctx, tx, row); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrCodeTaken
			}
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "cash_register.create", "cash_register", row.ID, map[string]any{"code": code, "active": in.Active}); err != nil {
			return err
		}
		return s.emitCash(ctx, tx, scope, row.ID, "", event.ChangeCreated)
	})
	if err != nil {
		return nil, err
	}
	return &Register{CashRegister: *row}, nil
}

// UpdateRegister changes a register (marina.configure, If-Match). A register with an open session cannot be
// deactivated; making a register the default clears the flag on the others; the default cannot be deactivated.
func (s *Service) UpdateRegister(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in RegisterPatch) (*Register, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	var out *Register
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetCashRegister(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		openID := ""
		if sess, err := store.OpenCashSessionOf(ctx, tx, scope, r.ID); err == nil {
			openID = sess.ID
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if in.Name != nil {
			name := strings.TrimSpace(*in.Name)
			if len(name) > 120 {
				return invalid("name", "too_long")
			}
			r.Name = name
		}
		if in.IsDefault != nil && *in.IsDefault && !r.IsDefault {
			if err := store.ClearDefaultCashRegister(ctx, tx, scope, r.ID, s.now()); err != nil {
				return err
			}
			r.IsDefault = true
		}
		if in.Active != nil {
			if !*in.Active && (openID != "" || r.IsDefault) {
				return ErrRegisterInUse
			}
			r.Active = *in.Active
		}
		if err := store.UpdateCashRegister(ctx, tx, scope, r, ifMatch, s.now()); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "cash_register.update", "cash_register", r.ID, map[string]any{
			"active": r.Active, "is_default": r.IsDefault,
		}); err != nil {
			return err
		}
		out = &Register{CashRegister: *r, OpenSessionID: openID}
		return s.emitCash(ctx, tx, scope, r.ID, "", event.ChangeUpdated)
	})
	return out, err
}

// resolveRegister returns the register of id, or the default one (created lazily) when id is empty.
func (s *Service) resolveRegister(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) (*store.CashRegister, error) {
	if id == "" {
		return s.EnsureDefaultRegister(ctx, tx, scope)
	}
	return store.GetCashRegister(ctx, tx, scope, id)
}

// OpenSession opens a session of a register (default when empty) with its float (payments.collect or
// payments.collect_cash). A second open session of the register is ErrSessionOpen (INV-55, DB partial unique).
func (s *Service) OpenSession(ctx context.Context, p *auth.Principal, marinaID, registerID string, float money.Cents) (*Session, error) {
	scope, _, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if float < 0 {
		return nil, invalid("float_cents", "invalid")
	}
	var out *Session
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		reg, err := s.resolveRegister(ctx, tx, scope, registerID)
		if err != nil {
			return err
		}
		if !reg.Active {
			return ErrRegisterInUse
		}
		now := s.now()
		row := &store.CashSession{
			ID: store.NewID(), MarinaID: scope.ID(), RegisterID: reg.ID, Status: store.CashSessionOpen,
			OpenedByUserID: userOf(p.Actor()), OpenedAt: now, BusinessDate: s.today(ctx, tx, scope), FloatCents: int64(float),
			Expected: store.JSON(`{}`), Counted: store.JSON(`{}`), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertCashSession(ctx, tx, row); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrSessionOpen
			}
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "cash_session.open", "cash_session", row.ID, map[string]any{
			"register_id": reg.ID, "float_cents": row.FloatCents, "business_date": row.BusinessDate,
		}); err != nil {
			return err
		}
		if err := s.emitCash(ctx, tx, scope, reg.ID, row.ID, event.ChangeOpened); err != nil {
			return err
		}
		out = &Session{CashSession: *row, RegisterCode: reg.Code, Summary: summary.Compute(summary.Input{Float: float})}
		return nil
	})
	return out, err
}

// CurrentSession returns the open session of a register (default when empty); none is ErrNotFound.
func (s *Service) CurrentSession(ctx context.Context, p *auth.Principal, marinaID, registerID string) (*Session, error) {
	scope, _, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	var reg *store.CashRegister
	if registerID == "" {
		reg, err = store.DefaultCashRegister(ctx, db, scope)
	} else {
		reg, err = store.GetCashRegister(ctx, db, scope, registerID)
	}
	if err != nil {
		return nil, err
	}
	sess, err := store.OpenCashSessionOf(ctx, db, scope, reg.ID)
	if err != nil {
		return nil, err
	}
	return s.sessionView(ctx, db, scope, sess, reg.Code)
}

// GetSession returns a session with its summary and movements.
func (s *Service) GetSession(ctx context.Context, p *auth.Principal, marinaID, id string) (*Session, error) {
	scope, _, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	sess, err := store.GetCashSession(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	return s.sessionView(ctx, s.d.DB.DB, scope, sess, "")
}

// ListSessions returns sessions newest first with their summaries.
func (s *Service) ListSessions(ctx context.Context, p *auth.Principal, marinaID string, f store.CashSessionFilter, cursor string, limit int) (store.Page[Session], error) {
	scope, _, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return store.Page[Session]{}, err
	}
	page, err := store.ListCashSessions(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[Session]{}, err
	}
	out := store.Page[Session]{NextCursor: page.NextCursor, Items: make([]Session, 0, len(page.Items))}
	for i := range page.Items {
		v, err := s.sessionView(ctx, s.d.DB.DB, scope, &page.Items[i], "")
		if err != nil {
			return store.Page[Session]{}, err
		}
		v.Movements = nil
		out.Items = append(out.Items, *v)
	}
	return out, nil
}

func (s *Service) sessionView(ctx context.Context, db bun.IDB, scope store.MarinaScope, sess *store.CashSession, code string) (*Session, error) {
	totals, err := store.SessionCashTotals(ctx, db, scope, sess.ID)
	if err != nil {
		return nil, err
	}
	in := summaryInput(sess, totals)
	if sess.Status == store.CashSessionClosed {
		in.Counted = decodeAmounts(sess.Counted)
	}
	movs, err := store.ListCashMovements(ctx, db, scope, sess.ID)
	if err != nil {
		return nil, err
	}
	if code == "" {
		if r, err := store.GetCashRegister(ctx, db, scope, sess.RegisterID); err == nil {
			code = r.Code
		}
	}
	return &Session{CashSession: *sess, RegisterCode: code, Summary: summary.Compute(in), Movements: movs}, nil
}

func summaryInput(sess *store.CashSession, t store.CashTotals) summary.Input {
	in := summary.Input{
		Float: money.Cents(sess.FloatCents), Payments: map[string]money.Cents{}, Refunds: map[string]money.Cents{},
		MovementsIn: money.Cents(t.MovementsIn), MovementsOut: money.Cents(t.MovementsOut),
	}
	for m, v := range t.Payments {
		in.Payments[m] = money.Cents(v)
	}
	for m, v := range t.Refunds {
		in.Refunds[m] = money.Cents(v)
	}
	return in
}

func decodeAmounts(raw store.JSON) map[string]money.Cents {
	var m map[string]int64
	_ = json.Unmarshal(raw, &m)
	out := map[string]money.Cents{}
	for k, v := range m {
		out[k] = money.Cents(v)
	}
	return out
}

func encodeAmounts(list []summary.MethodAmount) store.JSON {
	m := map[string]int64{}
	for _, a := range list {
		m[a.Method] = int64(a.Amount)
	}
	return store.MustJSON(m)
}

// Movement kinds.
const (
	MovementIn  = "in"
	MovementOut = "out"
)

// AddMovement records cash in or out of an open session. A withdrawal above the expected cash is
// ErrInsufficient.
func (s *Service) AddMovement(ctx context.Context, p *auth.Principal, marinaID, sessionID, kind string, amount money.Cents, reason string) (*store.CashMovement, error) {
	scope, _, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	v := &auth.ValidationError{}
	if kind != MovementIn && kind != MovementOut {
		v.Add("kind", "invalid")
	}
	if amount <= 0 {
		v.Add("amount_cents", "invalid")
	}
	if reason == "" || len(reason) > 500 {
		v.Add("reason", "invalid")
	}
	if len(v.Fields) > 0 {
		return nil, v
	}
	var out *store.CashMovement
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		sess, err := store.LockCashSession(ctx, tx, scope, sessionID)
		if err != nil {
			return err
		}
		if sess.Status != store.CashSessionOpen {
			return ErrSessionClosed
		}
		if kind == MovementOut {
			totals, err := store.SessionCashTotals(ctx, tx, scope, sess.ID)
			if err != nil {
				return err
			}
			if summary.ExpectedCash(summaryInput(sess, totals)) < amount {
				return ErrInsufficient
			}
		}
		ok, err := store.TouchOpenCashSession(ctx, tx, scope, sess.ID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrSessionClosed
		}
		row := &store.CashMovement{
			ID: store.NewID(), MarinaID: scope.ID(), SessionID: sess.ID, Kind: kind, AmountCents: int64(amount),
			Reason: reason, CreatedByUserID: userOf(p.Actor()), CreatedAt: s.now(),
		}
		if err := store.InsertCashMovement(ctx, tx, row); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "cash_session.movement", "cash_session", sess.ID, map[string]any{
			"movement_id": row.ID, "kind": kind, "amount_cents": row.AmountCents,
		}); err != nil {
			return err
		}
		out = row
		return s.emitCash(ctx, tx, scope, sess.RegisterID, sess.ID, event.ChangeMovement)
	})
	return out, err
}

// CloseSession closes an open session with the counted amounts per method (If-Match): the summary is computed
// under the session lock, so every payment that landed is in it, and frozen (INV-55).
func (s *Service) CloseSession(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, counted []CountedInput, notes string) (*Session, error) {
	scope, _, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	cnt := map[string]money.Cents{}
	for _, c := range counted {
		if c.Amount < 0 || c.Method == "" {
			return nil, invalid("counted", "invalid")
		}
		cnt[c.Method] += c.Amount
	}
	if len(notes) > 2000 {
		return nil, invalid("notes", "too_long")
	}
	var out *Session
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		sess, err := store.LockCashSession(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if sess.Status != store.CashSessionOpen {
			return ErrSessionClosed
		}
		if sess.Version != ifMatch {
			return ErrVersionMismatch
		}
		totals, err := store.SessionCashTotals(ctx, tx, scope, sess.ID)
		if err != nil {
			return err
		}
		in := summaryInput(sess, totals)
		in.Counted = cnt
		sum := summary.Compute(in)
		sess.ClosedByUserID = userOf(p.Actor())
		sess.Expected, sess.Counted = encodeAmounts(sum.Expected), encodeAmounts(sum.Counted)
		sess.DifferenceCents, sess.Notes = int64(sum.Difference), strings.TrimSpace(notes)
		if err := store.CloseCashSession(ctx, tx, scope, sess, ifMatch, s.now()); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "cash_session.close", "cash_session", sess.ID, map[string]any{
			"expected_cents": int64(sum.ExpectedTotal), "counted_cents": int64(sum.CountedTotal), "difference_cents": sess.DifferenceCents,
		}); err != nil {
			return err
		}
		if err := s.emitCash(ctx, tx, scope, sess.RegisterID, sess.ID, event.ChangeClosed); err != nil {
			return err
		}
		movs, err := store.ListCashMovements(ctx, tx, scope, sess.ID)
		if err != nil {
			return err
		}
		out = &Session{CashSession: *sess, Summary: sum, Movements: movs}
		if r, err := store.GetCashRegister(ctx, tx, scope, sess.RegisterID); err == nil {
			out.RegisterCode = r.Code
		}
		return nil
	})
	return out, err
}

// SessionForPaymentTx implements payment.CashSessions: the open session of the register (default when empty)
// locked and re-checked with a no-op conditional UPDATE, so a payment lands before a close or fails
// ErrSessionClosed (m5-plan §5.1).
func (s *Service) SessionForPaymentTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, registerID string, required bool) (string, error) {
	var reg *store.CashRegister
	var err error
	if registerID == "" {
		reg, err = store.DefaultCashRegister(ctx, tx, scope)
	} else {
		reg, err = store.GetCashRegister(ctx, tx, scope, registerID)
	}
	if errors.Is(err, store.ErrNotFound) {
		if registerID != "" {
			return "", ErrNotFound
		}
		if required {
			return "", ErrSessionRequired
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sess, err := store.OpenCashSessionOf(ctx, tx, scope, reg.ID)
	if errors.Is(err, store.ErrNotFound) {
		if required {
			return "", ErrSessionRequired
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	locked, err := store.LockCashSession(ctx, tx, scope, sess.ID)
	if err != nil {
		return "", err
	}
	if locked.Status != store.CashSessionOpen {
		if required {
			return "", ErrSessionClosed
		}
		return "", nil
	}
	ok, err := store.TouchOpenCashSession(ctx, tx, scope, sess.ID)
	if err != nil {
		return "", err
	}
	if !ok {
		if required {
			return "", ErrSessionClosed
		}
		return "", nil
	}
	return sess.ID, nil
}

var _ payment.CashSessions = (*Service)(nil)

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	loc := time.UTC
	if tz, err := store.MarinaTimezone(ctx, db, scope); err == nil {
		if l, err := calendar.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return occupancy.Today(s.d.Clock, loc)
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

func (s *Service) emitCash(ctx context.Context, tx bun.IDB, scope store.MarinaScope, registerID, sessionID, change string) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.CashChanged, event.CashChangedData{
		RegisterID: registerID, SessionID: sessionID, Change: change,
	})
	return err
}

func userOf(a audit.Actor) string {
	if a.Type == audit.ActorUser {
		return a.ID
	}
	return ""
}

func invalid(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}
