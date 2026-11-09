package watch

import (
	"context"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// actionsOfKind are the actions valid for each alert kind (m12-plan §8.3).
var actionsOfKind = map[string][]string{
	store.SensorAlertUnexpectedOccupied: {ActionDismiss},
	store.SensorAlertTransitArrival:     {ActionConfirmArrival, ActionDismiss},
	store.SensorAlertReturnDetected:     {ActionConfirmArrival, ActionDismiss},
	store.SensorAlertReleaseProposal:    {ActionConfirmRelease, ActionDismiss},
	store.SensorAlertEarlyReturn:        {ActionRevokeRelease, ActionDismiss},
	store.SensorAlertDeviceSilent:       {ActionCreateTask, ActionDismiss},
	store.SensorAlertBatteryLow:         {ActionCreateTask, ActionDismiss},
}

// actionAllowed reports whether the action suits the alert (kind, and resold for revoke_release).
func actionAllowed(a *store.SensorAlert, action string) bool {
	if !slices.Contains(actionsOfKind[a.Kind], action) {
		return false
	}
	if action == ActionRevokeRelease {
		r := parseRefs(a.Refs)
		return r.Resold != nil && !*r.Resold && r.ReleaseID != ""
	}
	return true
}

// canAct reports whether the principal holds the permission of an action (D18).
func canAct(p *auth.Principal, scope store.MarinaScope, action string) bool {
	can := func(perm auth.Permission) bool { return p.Can(scope.OrgID(), scope.ID(), perm) }
	switch action {
	case ActionDismiss, ActionConfirmArrival:
		return can(auth.PermPresenceWrite)
	case ActionConfirmRelease, ActionRevokeRelease:
		return can(auth.PermOccupancyMove)
	case ActionCreateTask:
		return can(auth.PermMaintenanceManage) || can(auth.PermMaintenanceReport)
	}
	return false
}

// views decorates alerts with their device and resource codes and the actions the principal may take.
func (s *Service) views(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.SensorAlert) ([]AlertView, error) {
	out := make([]AlertView, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	var devIDs, resIDs []string
	for _, a := range rows {
		devIDs = append(devIDs, a.DeviceID)
		resIDs = append(resIDs, a.ResourceID)
	}
	devCodes, err := store.SensorDeviceCodes(ctx, db, scope, devIDs)
	if err != nil {
		return nil, err
	}
	resCodes, err := store.SensorResourceCodesOf(ctx, db, scope, resIDs)
	if err != nil {
		return nil, err
	}
	for _, a := range rows {
		v := AlertView{SensorAlert: a, DeviceCode: devCodes[a.DeviceID], ResourceCode: resCodes[a.ResourceID], AllowedActions: []string{}}
		if a.Status == store.SensorAlertOpen && p != nil {
			for _, act := range actionsOfKind[a.Kind] {
				if actionAllowed(&a, act) && canAct(p, scope, act) {
					v.AllowedActions = append(v.AllowedActions, act)
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// ListAlerts lists the alerts, newest first (map.view). An empty status filter lists the open ones.
func (s *Service) ListAlerts(ctx context.Context, p *auth.Principal, marinaID string, f AlertFilter, cursor string, limit int) (store.Page[AlertView], error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView)
	if err != nil {
		return store.Page[AlertView]{}, err
	}
	status := f.Status
	switch status {
	case "":
		status = store.SensorAlertOpen
	case "all":
		status = ""
	}
	db := s.d.DB.DB
	page, err := store.ListSensorAlerts(ctx, db, scope, store.SensorAlertFilter{Status: status, Kind: f.Kind, MapID: f.MapID, DeviceID: f.DeviceID}, cursor, limit)
	if err != nil {
		return store.Page[AlertView]{}, err
	}
	views, err := s.views(ctx, db, scope, p, page.Items)
	if err != nil {
		return store.Page[AlertView]{}, err
	}
	return store.Page[AlertView]{Items: views, NextCursor: page.NextCursor}, nil
}

// GetAlert returns an alert (map.view).
func (s *Service) GetAlert(ctx context.Context, p *auth.Principal, marinaID, alertID string) (*AlertView, error) {
	scope, err := s.authorize(ctx, p, marinaID, auth.PermMapView, store.SensorTableAlerts, alertID)
	if err != nil {
		return nil, err
	}
	return s.alertView(ctx, s.d.DB.DB, scope, p, alertID)
}

func (s *Service) alertView(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, alertID string) (*AlertView, error) {
	a, err := store.GetSensorAlert(ctx, db, scope, alertID)
	if err != nil {
		return nil, notFound(err)
	}
	views, err := s.views(ctx, db, scope, p, []store.SensorAlert{*a})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}
