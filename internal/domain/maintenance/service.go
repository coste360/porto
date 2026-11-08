package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/deadline"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/filestore"
	"github.com/coste360/porto/internal/store"
)

// Deps are the collaborators of the service.
type Deps struct {
	DB        *store.DB
	Clock     clock.Clock
	Audit     *audit.Writer
	Outbox    *event.Outbox
	Log       *slog.Logger
	Keys      *crypto.KeyRing
	Files     *filestore.Store
	Occupancy *occupancy.Service
	Deadlines *deadline.Service
}

// Service is the maintenance service.
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

// bookableKinds are bookable in lifting slots by default (M10, m10-plan §11).
var bookableKinds = []string{"travel_lift", "crane", "forklift", "trailer"}

// Equipment kinds (spec 6.8).
var equipmentKinds = []string{"travel_lift", "crane", "forklift", "trailer", "vehicle", "plant", "other"}

// Element types that can be targeted.
var elementTypes = []string{
	store.ElementBerth, store.ElementStructure, store.ElementDynamicArea, store.ElementLandResource, store.ElementPedestal,
	store.ElementEquipment,
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

// location returns the marina time zone (UTC when unknown).
func location(ctx context.Context, db bun.IDB, scope store.MarinaScope) *time.Location {
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return time.UTC
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// today is the business date in the marina time zone.
func (s *Service) today(ctx context.Context, db bun.IDB, scope store.MarinaScope) calendar.Date {
	return calendar.In(s.d.Clock.Now(), location(ctx, db, scope))
}

// authorizeAny authorises the first permission the principal holds (404 for another marina, 403 when none).
func authorizeAny(ctx context.Context, db bun.IDB, p *auth.Principal, marinaID string, perms ...auth.Permission) (store.MarinaScope, auth.Permission, error) {
	var last error
	for _, perm := range perms {
		scope, err := p.Authorize(ctx, db, marinaID, perm)
		if err == nil {
			return scope, perm, nil
		}
		if !errors.Is(err, auth.ErrForbidden) {
			return store.MarinaScope{}, "", err
		}
		last = err
	}
	return store.MarinaScope{}, "", last
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}

func versionErr(err error) error {
	if errors.Is(err, store.ErrVersionMismatch) {
		return ErrVersionMismatch
	}
	return notFound(err)
}

func cleanText(s string, maxRunes int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= maxRunes
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Service) audit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, action, entity, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: a, Action: action, EntityType: entity, EntityID: id, Diff: diff,
	})
}

// changed writes maintenance.status_changed for an entity (task, plan, equipment): ids and enums only (INV-70).
func (s *Service) changed(ctx context.Context, tx bun.IDB, scope store.MarinaScope, entity, id, elementType, elementID, status string) error {
	if s.d.Outbox == nil {
		return nil
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.MaintenanceStatusChanged, event.MaintenanceStatusChangedData{
		Entity: entity, ID: id, ElementType: elementType, ElementID: elementID, Status: status,
	})
	return err
}

// mapExists reports whether a map belongs to the marina.
func mapExists(ctx context.Context, db bun.IDB, scope store.MarinaScope, mapID string) (bool, error) {
	return store.ClosureScopeExists(ctx, db, scope, store.ClosureMap, mapID, "")
}

// ---- equipment ----

func (s *Service) equipmentView(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Equipment) ([]EquipmentView, error) {
	out := make([]EquipmentView, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	oos, err := s.outOfService(ctx, db, scope, s.today(ctx, db, scope))
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, EquipmentView{Row: r, OutOfService: oos[store.ElementEquipment+":"+r.ID]})
	}
	return out, nil
}

// outOfService returns the elements (type:id) held out of service on date by a scheduled or in-progress task.
func (s *Service) outOfService(ctx context.Context, db bun.IDB, scope store.MarinaScope, date calendar.Date) (map[string]bool, error) {
	tasks, err := store.LiveTasksWithElement(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for i := range tasks {
		if isOutOfService(&tasks[i], date) {
			out[tasks[i].ElementType+":"+tasks[i].ElementID] = true
		}
	}
	return out, nil
}

// isOutOfService: a scheduled or in-progress task flagged out of service whose nights cover date (no nights = from
// the start of the work until it is done).
func isOutOfService(t *store.MaintenanceTask, date calendar.Date) bool {
	if !t.OutOfService || (t.Status != store.TaskScheduled && t.Status != store.TaskInProgress) {
		return false
	}
	if !t.ScheduledFrom.IsZero() && date.Before(t.ScheduledFrom) {
		return false
	}
	if !t.ScheduledTo.IsZero() && date.After(t.ScheduledTo) {
		return false
	}
	return !t.ScheduledFrom.IsZero() || t.Status != store.TaskScheduled
}

func validateEquipment(e *store.Equipment, v *auth.ValidationError) {
	var ok bool
	if e.Code, ok = cleanText(e.Code, 32); !ok || e.Code == "" {
		v.Add("code", "invalid")
	}
	if e.Name, ok = cleanText(e.Name, 100); !ok || e.Name == "" {
		v.Add("name", "invalid")
	}
	if !contains(equipmentKinds, e.Kind) {
		v.Add("kind", "invalid")
	}
	if (e.XCm == nil) != (e.YCm == nil) {
		v.Add("x_cm", "position_incomplete")
	}
	if e.XCm != nil && e.MapID == "" {
		v.Add("map_id", "required")
	}
	if e.CapacityKg != nil && *e.CapacityKg <= 0 {
		v.Add("capacity_kg", "invalid")
	}
	if e.DefaultMinutes < 15 || e.DefaultMinutes > 1440 || e.DefaultMinutes%15 != 0 {
		v.Add("default_minutes", "invalid")
	}
}

func (s *Service) checkEquipmentMap(ctx context.Context, db bun.IDB, scope store.MarinaScope, e *store.Equipment) error {
	if e.MapID == "" {
		return nil
	}
	ok, err := mapExists(ctx, db, scope, e.MapID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrElementInvalid
	}
	return nil
}

func (s *Service) writeEquipment(ctx context.Context, scope store.MarinaScope, p *auth.Principal, e *store.Equipment, expected int, action string) error {
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := s.checkEquipmentMap(ctx, tx, scope, e); err != nil {
			return err
		}
		var err error
		if expected == 0 {
			err = store.InsertEquipment(ctx, tx, scope, e)
		} else {
			err = store.UpdateEquipment(ctx, tx, scope, e, expected, s.now())
		}
		if err != nil {
			if store.IsUniqueViolation(err) {
				return ErrCodeTaken
			}
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), action, "equipment", e.ID, map[string]any{"code": e.Code, "kind": e.Kind, "active": e.Active}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "equipment", e.ID, store.ElementEquipment, e.ID, "")
	})
	return err
}

// ListEquipment lists the equipment (maintenance.manage or maintenance.report).
func (s *Service) ListEquipment(ctx context.Context, p *auth.Principal, marinaID, kind string, active *bool, cursor string,
	limit int,
) ([]EquipmentView, string, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return nil, "", err
	}
	if kind != "" && !contains(equipmentKinds, kind) {
		v := &auth.ValidationError{}
		v.Add("kind", "invalid")
		return nil, "", v
	}
	page, err := store.ListEquipment(ctx, s.d.DB.DB, scope, store.EquipmentFilter{Kind: kind, Active: active}, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.equipmentView(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

// CreateEquipment registers equipment (maintenance.manage).
func (s *Service) CreateEquipment(ctx context.Context, p *auth.Principal, marinaID string, in EquipmentInput) (*EquipmentView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	now := s.now()
	e := &store.Equipment{
		ID: store.NewID(), Code: in.Code, Name: in.Name, Kind: in.Kind, MapID: in.MapID, XCm: in.X, YCm: in.Y,
		CapacityKg: in.CapacityKg, Active: in.Active == nil || *in.Active, CreatedAt: now, UpdatedAt: now,
		Bookable: contains(bookableKinds, in.Kind), DefaultMinutes: 30,
	}
	if in.Bookable != nil {
		e.Bookable = *in.Bookable
	}
	if in.DefaultMinutes != nil {
		e.DefaultMinutes = *in.DefaultMinutes
	}
	v := &auth.ValidationError{}
	validateEquipment(e, v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := s.writeEquipment(ctx, scope, p, e, 0, "equipment.create"); err != nil {
		return nil, err
	}
	return s.GetEquipment(ctx, p, marinaID, e.ID)
}

// GetEquipment reads equipment (maintenance.manage or maintenance.report).
func (s *Service) GetEquipment(ctx context.Context, p *auth.Principal, marinaID, id string) (*EquipmentView, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMaintenanceManage, auth.PermMaintenanceReport)
	if err != nil {
		return nil, err
	}
	e, err := store.GetEquipment(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	views, err := s.equipmentView(ctx, s.d.DB.DB, scope, []store.Equipment{*e})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// UpdateEquipment changes equipment (maintenance.manage, If-Match).
func (s *Service) UpdateEquipment(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch EquipmentPatch) (*EquipmentView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	e, err := store.GetEquipment(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if int64(e.Version) != version {
		return nil, ErrVersionMismatch
	}
	if patch.Code != nil {
		e.Code = *patch.Code
	}
	if patch.Name != nil {
		e.Name = *patch.Name
	}
	if patch.Kind != nil {
		e.Kind = *patch.Kind
	}
	if patch.MapID != nil {
		e.MapID = *patch.MapID
	}
	if patch.ClearPosition {
		e.XCm, e.YCm = nil, nil
	}
	if patch.X != nil {
		e.XCm = patch.X
	}
	if patch.Y != nil {
		e.YCm = patch.Y
	}
	if patch.ClearCapacity {
		e.CapacityKg = nil
	}
	if patch.CapacityKg != nil {
		e.CapacityKg = patch.CapacityKg
	}
	if patch.Active != nil {
		e.Active = *patch.Active
	}
	if patch.Bookable != nil {
		e.Bookable = *patch.Bookable
	}
	if patch.DefaultMinutes != nil {
		e.DefaultMinutes = *patch.DefaultMinutes
	}
	v := &auth.ValidationError{}
	validateEquipment(e, v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := s.writeEquipment(ctx, scope, p, e, int(version), "equipment.update"); err != nil {
		return nil, err
	}
	return s.GetEquipment(ctx, p, marinaID, id)
}
