package watch

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/maintenance"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/domain/sensor/reconcile"
	"github.com/coste360/porto/internal/store"
)

// alertIdempotencyKey is the presence idempotency key of the confirm of an alert: concurrent confirms of the same
// alert record one presence (and one transit transition) and the losers meet version_conflict on the alert.
func alertIdempotencyKey(alertID string) string { return "sensor-alert:" + alertID }

func validationErr(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v.Err()
}

// Act decides an alert (m12-plan §8.3). Reading the alert needs map.view, so another marina's alert is 404 first;
// then the action's permission (presence.write, occupancy.move, maintenance.manage or maintenance.report), If-Match,
// the kind and the condition: a condition that no longer holds resolves the alert in the same call and answers
// ErrAlertStale. dismiss and create_task run in one transaction that locks the alert; confirm_arrival,
// confirm_release and revoke_release call the domain service with the user's principal (its own transaction and
// checks: presence.Record, occupancy.CreateRelease, occupancy.RevokeRelease), then lock the alert again and record
// the decision only if it is still open at the same version.
func (s *Service) Act(ctx context.Context, p *auth.Principal, marinaID, alertID string, ifMatch int, in ActionInput) (*AlertView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView, store.SensorTableAlerts, alertID)
	if err != nil {
		return nil, err
	}
	if _, ok := map[string]bool{ActionDismiss: true, ActionConfirmArrival: true, ActionConfirmRelease: true, ActionRevokeRelease: true, ActionCreateTask: true}[in.Action]; !ok {
		return nil, validationErr("action", "invalid")
	}
	if !canAct(p, scope, in.Action) {
		return nil, auth.ErrForbidden
	}
	if utf8.RuneCountInString(in.Note) > 500 {
		return nil, validationErr("note", "too_long")
	}
	db := s.d.DB.DB
	a, err := store.GetSensorAlert(ctx, db, scope, alertID)
	if err != nil {
		return nil, notFound(err)
	}
	if a.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	if a.Status != store.SensorAlertOpen || !actionAllowed(a, in.Action) {
		return nil, ErrActionNotAllowed
	}
	refs := parseRefs(a.Refs)
	stale, err := s.stale(ctx, db, scope, a)
	if err != nil {
		return nil, err
	}
	if stale {
		if err := s.resolveStale(ctx, scope, a.ID, ifMatch); err != nil {
			return nil, err
		}
		return nil, ErrAlertStale
	}
	var outcome string
	switch in.Action {
	case ActionDismiss, ActionCreateTask:
		// one transaction below
	case ActionConfirmArrival:
		if refs.VesselID == "" {
			return nil, ErrActionNotAllowed
		}
		pin := presence.Input{VesselID: refs.VesselID, Event: presence.EventArrived, AllocationID: refs.AllocationID, IdempotencyKey: alertIdempotencyKey(a.ID)}
		if refs.PhysicalSince != nil {
			at := *refs.PhysicalSince
			pin.At = &at
		}
		res, err := s.d.Presence.Record(ctx, p, marinaID, pin)
		if errors.Is(err, presence.ErrInvalidTransition) {
			if err := s.resolveStale(ctx, scope, a.ID, ifMatch); err != nil {
				return nil, err
			}
			return nil, ErrAlertStale
		}
		if err != nil {
			return nil, err
		}
		outcome = res.Presence.Row.ID
	case ActionConfirmRelease:
		if !in.ConsentConfirmed {
			return nil, ErrConsentRequired
		}
		from, to, err := proposalRange(refs, in)
		if err != nil {
			return nil, err
		}
		rel, err := s.d.Occupancy.CreateRelease(ctx, p, marinaID, occupancy.ReleaseInput{AllocationID: refs.AllocationID, From: from, To: to, Source: "office"})
		if err != nil {
			return nil, err
		}
		outcome = rel.ID
	case ActionRevokeRelease:
		r, err := store.GetRelease(ctx, db, scope, refs.ReleaseID)
		if err != nil {
			return nil, notFound(err)
		}
		today := calendar.In(s.now(), s.location(ctx, db, scope))
		from := today
		if from.Before(r.FromNight) {
			from = r.FromNight
		}
		res, err := s.d.Occupancy.RevokeRelease(ctx, p, marinaID, r.ID, r.Version, &from, nil)
		if err != nil {
			return nil, err
		}
		outcome = res.Release.ID
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.LockSensorAlert(ctx, tx, scope, a.ID)
		if err != nil {
			return notFound(err)
		}
		open := cur.Version == ifMatch && cur.Status == store.SensorAlertOpen
		// The service call of this action succeeded and its effect made a concurrent request resolve the alert as
		// stale meanwhile: the decision is still this one (the presence or release exists because of it).
		resolvedByUs := outcome != "" && cur.Status == store.SensorAlertResolved && cur.DecidedAt == nil
		if !open && !resolvedByUs {
			return ErrVersionMismatch
		}
		expected := cur.Version
		now := s.now()
		switch in.Action {
		case ActionDismiss:
			cur.Status = store.SensorAlertDismissed
			cur.Note = in.Note
		case ActionCreateTask:
			if s.d.Maintenance == nil {
				return ErrActionNotAllowed
			}
			dev, err := store.GetSensorDevice(ctx, tx, scope, cur.DeviceID)
			if err != nil {
				return notFound(err)
			}
			t, err := s.d.Maintenance.CreateSensorTaskTx(ctx, tx, scope, p, maintenance.SensorTaskInput{
				DeviceID: dev.ID, DeviceCode: dev.Code, MapID: dev.MapID, XCm: dev.XCm, YCm: dev.YCm, AlertID: cur.ID,
				AlertKind: cur.Kind, Priority: in.Priority,
			})
			if err != nil {
				return err
			}
			outcome = t.ID
			cur.Status = store.SensorAlertConfirmed
			cur.Note = in.Note
		default:
			cur.Status = store.SensorAlertConfirmed
			cur.Note = in.Note
		}
		cur.DecidedAt, cur.DecidedByUserID, cur.OutcomeRef = &now, p.UserID, outcome
		if err := store.UpdateSensorAlertDecision(ctx, tx, scope, cur, expected, now); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		return s.alertEvent(ctx, tx, scope, p.Actor(), "sensor_alert."+in.Action, cur)
	})
	if err != nil {
		return nil, err
	}
	return s.alertView(ctx, db, scope, p, a.ID)
}

// proposalRange validates the confirmed release range inside the proposal (default: the whole proposal).
func proposalRange(refs store.SensorAlertRefs, in ActionInput) (calendar.Date, calendar.Date, error) {
	pf, err1 := calendar.Parse(refs.ProposedFrom)
	pt, err2 := calendar.Parse(refs.ProposedTo)
	if err1 != nil || err2 != nil || refs.AllocationID == "" {
		return calendar.Date{}, calendar.Date{}, ErrActionNotAllowed
	}
	from, to := pf, pt
	if in.From != nil {
		from = *in.From
	}
	if in.To != nil {
		to = *in.To
	}
	if from.Before(pf) || from.After(pt) {
		return calendar.Date{}, calendar.Date{}, validationErr("from", "outside_proposal")
	}
	if to.Before(from) || to.After(pt) {
		return calendar.Date{}, calendar.Date{}, validationErr("to", "outside_proposal")
	}
	return from, to, nil
}

// stale re-evaluates the rule of the alert on the current facts of its device (reconcile.Stale).
func (s *Service) stale(ctx context.Context, db bun.IDB, scope store.MarinaScope, a *store.SensorAlert) (bool, error) {
	sn, err := s.loadSnapshot(ctx, db, scope, store.SensorWatchDeviceFilter{DeviceIDs: []string{a.DeviceID}}, s.now(), nil)
	if err != nil {
		return false, err
	}
	sub, ok := sn.subjects[a.DeviceID]
	if !ok {
		return true, nil
	}
	return reconcile.Stale(sn.input(nil), sub, a.Kind, a.ConditionKey), nil
}

// resolveStale clears the condition of a stale alert (resolved, cleared) with its audit and event.
func (s *Service) resolveStale(ctx context.Context, scope store.MarinaScope, alertID string, ifMatch int) error {
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		cur, err := store.LockSensorAlert(ctx, tx, scope, alertID)
		if err != nil {
			return notFound(err)
		}
		if cur.Version != ifMatch || cur.Status != store.SensorAlertOpen {
			return ErrVersionMismatch
		}
		rows, err := store.ClearSensorAlertCondition(ctx, tx, scope, cur.DeviceID, cur.Kind, s.now())
		if err != nil {
			return err
		}
		for i := range rows {
			if err := s.alertEvent(ctx, tx, scope, audit.System, "sensor_alert.resolve", &rows[i]); err != nil {
				return err
			}
		}
		return nil
	})
}
