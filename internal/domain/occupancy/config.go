package occupancy

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Settings are the occupancy settings of a marina (occupancy_settings, m3-plan §5.1).
type Settings struct {
	SafetyClearanceCm    int64
	WasteThresholdBP     int64
	DraftHoldDays        int
	DeskHoldHours        int
	QuoteValidityDays    int
	MaxSegments          int
	AbsentDaysThreshold  int
	ArrivalGraceMinutes  int
	DefaultArrivalTime   string
	DefaultDepartureTime string
	RepriceOnMove        bool
	ReleasePolicy        string
	Version              int
}

func settingsView(o *store.OccupancySettings) Settings {
	return Settings{
		SafetyClearanceCm: o.SafetyClearanceCm, WasteThresholdBP: o.WasteThresholdBP, DraftHoldDays: o.DraftHoldDays,
		DeskHoldHours: o.DeskHoldHours, QuoteValidityDays: o.QuoteValidityDays, MaxSegments: o.MaxSegments,
		AbsentDaysThreshold: o.AbsentDaysThreshold, ArrivalGraceMinutes: o.ArrivalGraceMinutes,
		DefaultArrivalTime: o.DefaultArrivalTime, DefaultDepartureTime: o.DefaultDepartureTime,
		RepriceOnMove: o.RepriceOnMove, ReleasePolicy: o.ReleasePolicy, Version: o.Version,
	}
}

// GetSettings returns the occupancy settings (map.view); never stored = the defaults at version 1.
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	o, _, err := settings(ctx, s.d.DB.DB, scope, s.now())
	if err != nil {
		return nil, err
	}
	out := settingsView(o)
	return &out, nil
}

// validTime reports whether s is HH:MM.
func validTime(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	h, m := s[:2], s[3:]
	for _, c := range h + m {
		if c < '0' || c > '9' {
			return false
		}
	}
	return h <= "23" && m <= "59"
}

// ParseMinutes returns the minutes from midnight of HH:MM (-1 when invalid).
func ParseMinutes(s string) int {
	if !validTime(s) {
		return -1
	}
	return int(s[0]-'0')*600 + int(s[1]-'0')*60 + int(s[3]-'0')*10 + int(s[4]-'0')
}

// UpdateSettings replaces the occupancy settings when their version equals ifMatch (harbor.configure).
func (s *Service) UpdateSettings(ctx context.Context, p *auth.Principal, marinaID string, ifMatch int, in Settings) (*Settings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	rng := func(field string, x, lo, hi int64) {
		if x < lo || x > hi {
			v.Add(field, "out_of_range")
		}
	}
	rng("safety_clearance_cm", in.SafetyClearanceCm, 0, 300)
	rng("waste_threshold_bp", in.WasteThresholdBP, 0, 10000)
	rng("draft_hold_days", int64(in.DraftHoldDays), 1, 60)
	rng("desk_hold_hours", int64(in.DeskHoldHours), 1, 168)
	rng("quote_validity_days", int64(in.QuoteValidityDays), 1, 120)
	rng("max_segments", int64(in.MaxSegments), 0, 2)
	rng("absent_days_threshold", int64(in.AbsentDaysThreshold), 1, 365)
	rng("arrival_grace_minutes", int64(in.ArrivalGraceMinutes), 0, 1440)
	if !validTime(in.DefaultArrivalTime) {
		v.Add("default_arrival_time", "invalid")
	}
	if !validTime(in.DefaultDepartureTime) {
		v.Add("default_departure_time", "invalid")
	}
	if in.ReleasePolicy != "never" && in.ReleasePolicy != "with_consent" {
		v.Add("release_policy", "invalid")
	}
	if len(v.Fields) > 0 {
		return nil, v
	}
	var out Settings
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		o, err := store.GetOccupancySettings(ctx, tx, scope)
		if errors.Is(err, store.ErrNotFound) {
			o = store.DefaultOccupancySettings(scope.ID(), now)
			if err := store.InsertOccupancySettings(ctx, tx, scope, o); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		o.SafetyClearanceCm, o.WasteThresholdBP, o.DraftHoldDays, o.DeskHoldHours = in.SafetyClearanceCm, in.WasteThresholdBP, in.DraftHoldDays, in.DeskHoldHours
		o.QuoteValidityDays, o.MaxSegments, o.AbsentDaysThreshold, o.ArrivalGraceMinutes = in.QuoteValidityDays, in.MaxSegments, in.AbsentDaysThreshold, in.ArrivalGraceMinutes
		o.DefaultArrivalTime, o.DefaultDepartureTime, o.RepriceOnMove, o.ReleasePolicy = in.DefaultArrivalTime, in.DefaultDepartureTime, in.RepriceOnMove, in.ReleasePolicy
		o.UpdatedAt = now
		if err := store.UpdateOccupancySettings(ctx, tx, scope, o, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "occupancy_settings.update",
			EntityType: "occupancy_settings", EntityID: scope.ID(), Diff: map[string]any{"version": o.Version},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigOccupancyChange, event.OccupancyConfigChangedData{Version: o.Version, Change: "settings"}); err != nil {
			return err
		}
		out = settingsView(o)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Closure is a closure of arrivals (spec 10.8).
type Closure struct {
	ID           string
	ScopeType    string
	ScopeID      string
	ResourceType string
	From, To     calendar.Date
	Reason       string
	Version      int
}

func closureView(c *store.Closure) Closure {
	return Closure{ID: c.ID, ScopeType: c.ScopeType, ScopeID: c.ScopeID, ResourceType: c.ResourceType, From: c.FromDate, To: c.ToDate, Reason: c.Reason, Version: c.Version}
}

// ListClosures returns the closures intersecting [from, to] (map.view; zero dates = unbounded).
func (s *Service) ListClosures(ctx context.Context, p *auth.Principal, marinaID string, from, to calendar.Date) ([]Closure, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	cs, err := store.ListClosures(ctx, s.d.DB.DB, scope, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]Closure, len(cs))
	for i := range cs {
		out[i] = closureView(&cs[i])
	}
	return out, nil
}

// CreateClosure closes a scope to new arrivals for dates (harbor.configure).
func (s *Service) CreateClosure(ctx context.Context, p *auth.Principal, marinaID string, in Closure) (*Closure, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	v := &auth.ValidationError{}
	switch in.ScopeType {
	case store.ClosureMarina:
		if in.ScopeID != "" {
			v.Add("scope_id", "not_allowed")
		}
	case store.ClosureMap, store.ClosureStructure, store.ClosureGroup, store.ClosureResource:
		if in.ScopeID == "" {
			v.Add("scope_id", "required")
		}
	default:
		v.Add("scope_type", "invalid")
	}
	if in.ScopeType == store.ClosureResource {
		if in.ResourceType != store.ResourceBerth && in.ResourceType != store.ResourceArea && in.ResourceType != store.ResourceLand {
			v.Add("resource_type", "required")
		}
	} else {
		in.ResourceType = ""
	}
	if in.Reason == "" || utf8.RuneCountInString(in.Reason) > 200 {
		v.Add("reason", "invalid")
	}
	if in.To.Before(in.From) {
		v.Add("to_date", "before_from")
	}
	if len(v.Fields) > 0 {
		return nil, v
	}
	var out Closure
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if in.ScopeType != store.ClosureMarina {
			ok, err := store.ClosureScopeExists(ctx, tx, scope, in.ScopeType, in.ScopeID, in.ResourceType)
			if err != nil {
				return err
			}
			if !ok {
				return ErrNotFound
			}
		}
		now := s.now()
		c := &store.Closure{
			ID: store.NewID(), ScopeType: in.ScopeType, ScopeID: in.ScopeID, ResourceType: in.ResourceType, FromDate: in.From,
			ToDate: in.To, Reason: in.Reason, CreatedByUserID: p.UserID, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertClosure(ctx, tx, scope, c); err != nil {
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "closure.create", EntityType: "closure",
			EntityID: c.ID, Diff: map[string]any{"scope_type": c.ScopeType, "from": c.FromDate, "to": c.ToDate},
		}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigOccupancyChange, event.OccupancyConfigChangedData{ClosureID: c.ID, Change: "closure"}); err != nil {
			return err
		}
		out = closureView(c)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteClosure deletes a closure when its version equals ifMatch (harbor.configure).
func (s *Service) DeleteClosure(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.DeleteClosure(ctx, tx, scope, id, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if err := s.d.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "closure.delete", EntityType: "closure", EntityID: id,
		}); err != nil {
			return err
		}
		_, err := s.d.Outbox.Write(ctx, tx, &scope, event.ConfigOccupancyChange, event.OccupancyConfigChangedData{ClosureID: id, Change: "closure"})
		return err
	})
}
