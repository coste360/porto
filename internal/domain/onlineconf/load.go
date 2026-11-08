package onlineconf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Deps are the dependencies of the service, wired by internal/app.
type Deps struct {
	DB     *store.DB
	Clock  clock.Clock
	Audit  *audit.Writer
	Outbox *event.Outbox
	Log    *slog.Logger
}

// Service manages the online booking settings and texts.
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

// Load returns the settings of the marina of scope in db (a transaction or the pool): the stored row, or
// Defaults() when the marina never saved them. It does not authorise: callers already hold a scope.
func Load(ctx context.Context, db bun.IDB, scope store.MarinaScope) (Settings, error) {
	row, err := store.GetOnlineBookingSettings(ctx, db, scope)
	if errors.Is(err, store.ErrNotFound) {
		return Defaults(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	return fromRow(row)
}

func fromRow(r *store.OnlineBookingSettings) (Settings, error) {
	s := Defaults()
	s.MinNights, s.MaxNights = r.MinNights, r.MaxNights
	s.MinAdvanceHours, s.MaxAdvanceDays = r.MinAdvanceHours, r.MaxAdvanceDays
	s.ArrivalFrom, s.ArrivalTo = r.ArrivalFrom, r.ArrivalTo
	s.PaymentMode, s.DepositBP, s.RefundAs = r.PaymentMode, r.DepositBP, r.RefundAs
	s.RevealBerth = r.RevealBerth
	s.LatePaymentRecovery, s.AllowWithoutProvider = r.LatePaymentRecovery, r.AllowWithoutProvider
	s.MaxLiveHolds, s.ThemePreset = r.MaxLiveHolds, r.ThemePreset
	s.HideHeaderInEmbed, s.ShowFeet = r.HideHeaderInEmbed, r.ShowFeet
	s.Version = r.Version
	var days []int
	if err := decode(r.ClosedWeekdays, &days, "closed_weekdays"); err != nil {
		return Settings{}, err
	}
	s.ClosedWeekdays = make([]time.Weekday, 0, len(days))
	for _, d := range days {
		s.ClosedWeekdays = append(s.ClosedWeekdays, time.Weekday(d))
	}
	if err := decode(r.CancellationTiers, &s.CancellationTiers, "cancellation_tiers"); err != nil {
		return Settings{}, err
	}
	if err := decode(r.Approval, &s.Approval, "approval"); err != nil {
		return Settings{}, err
	}
	if s.Approval.GroupIDs == nil {
		s.Approval.GroupIDs = []string{}
	}
	if err := decode(r.RequiredFields, &s.RequiredFields, "required_fields"); err != nil {
		return Settings{}, err
	}
	if s.RequiredFields.Documents == nil {
		s.RequiredFields.Documents = []string{}
	}
	if err := decode(r.ArrivalInfo, &s.ArrivalInfo, "arrival_info"); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// decode unmarshals a JSON column; an empty column keeps the default already in v.
func decode(raw store.JSON, v any, column string) error {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("onlineconf: decode %s: %w", column, err)
	}
	return nil
}

// Get returns the settings of the marina (marina.configure).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID string) (Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return Settings{}, err
	}
	return Load(ctx, s.d.DB.DB, scope)
}
