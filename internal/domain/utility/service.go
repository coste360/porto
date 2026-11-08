package utility

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
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricelist"
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
	Charges   *charge.Service
	Contracts *contract.Service
	Prices    *pricelist.Service
}

// Service is the utilities service.
type Service struct {
	d Deps
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	s := &Service{d: d}
	if d.Charges != nil {
		d.Charges.SetCancelHook(store.ChargeOriginUtility, s.onChargeCancelledTx)
	}
	return s
}

// Meter kinds and their units (CHECK utility_meters_kind_unit_check).
var unitOf = map[string]string{store.MeterPower: "kwh", store.MeterWater: "m3", store.MeterHours: "h", store.MeterCycles: "cycle"}

// Resource types a meter may feed.
var meterResourceTypes = []string{store.ResourceBerth, store.ResourceArea, store.ResourceLand}

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

// changed writes meter.reading_recorded for an entity (reading, meter, consumption, settings, billing): ids and
// enums only, never values (INV-70).
func (s *Service) changed(ctx context.Context, tx bun.IDB, scope store.MarinaScope, entity, id, meterID, status string) error {
	if s.d.Outbox == nil {
		return nil
	}
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.MeterReadingRecorded, event.MeterReadingRecordedData{
		Entity: entity, ID: id, MeterID: meterID, Status: status,
	})
	return err
}

// ---- settings ----

// GetSettings reads the utility settings (marina.configure or charges.manage).
func (s *Service) GetSettings(ctx context.Context, p *auth.Principal, marinaID string) (*store.UtilitySettings, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMarinaConfigure, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	return store.GetUtilitySettings(ctx, s.d.DB.DB, scope, s.now())
}

// checkItem verifies that an item belongs to the marina.
func checkItem(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) error {
	if id == "" {
		return nil
	}
	if !store.ValidID(id) {
		return ErrElementInvalid
	}
	items, err := store.ItemsByIDs(ctx, db, scope, []string{id})
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return ErrElementInvalid
	}
	return nil
}

// UpdateSettings replaces the utility settings (marina.configure, If-Match).
func (s *Service) UpdateSettings(ctx context.Context, p *auth.Principal, marinaID string, version int64, in SettingsInput) (*store.UtilitySettings, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if in.Mode != store.UtilityModeWithInstallment && in.Mode != store.UtilityModeMonthly {
		v.Add("mode", "invalid")
	}
	for k, t := range map[string]*int64{"power_daily_threshold_milli": in.PowerDailyThresholdMilli, "water_daily_threshold_milli": in.WaterDailyThresholdMilli} {
		if t != nil && *t <= 0 {
			v.Add(k, "invalid")
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	row := &store.UtilitySettings{
		Mode: in.Mode, PowerDailyThresholdMilli: in.PowerDailyThresholdMilli, WaterDailyThresholdMilli: in.WaterDailyThresholdMilli,
	}
	if in.PowerItemID != nil {
		row.PowerItemID = *in.PowerItemID
	}
	if in.WaterItemID != nil {
		row.WaterItemID = *in.WaterItemID
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		for _, id := range []string{row.PowerItemID, row.WaterItemID} {
			if err := checkItem(ctx, tx, scope, id); err != nil {
				return err
			}
		}
		if err := store.UpdateUtilitySettings(ctx, tx, scope, row, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "utility_settings.update", "marina", scope.ID(),
			map[string]any{"mode": row.Mode, "power_item_id": row.PowerItemID, "water_item_id": row.WaterItemID}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "settings", scope.ID(), "", "")
	})
	if err != nil {
		return nil, err
	}
	return store.GetUtilitySettings(ctx, s.d.DB.DB, scope, s.now())
}

// ---- meters ----

func validateMeter(m *store.UtilityMeter, v *auth.ValidationError) {
	var ok bool
	if m.Code, ok = cleanText(m.Code, 32); !ok || m.Code == "" {
		v.Add("code", "invalid")
	}
	if unitOf[m.Kind] == "" {
		v.Add("kind", "invalid")
	} else if m.Unit != unitOf[m.Kind] {
		v.Add("unit", "invalid")
	}
	if m.SocketCode, ok = cleanText(m.SocketCode, 32); !ok {
		v.Add("socket_code", "too_long")
	}
	if m.Serial, ok = cleanText(m.Serial, 64); !ok {
		v.Add("serial", "too_long")
	}
	if (m.ResourceType == "") != (m.ResourceID == "") {
		v.Add("resource_id", "invalid")
	}
	if m.ResourceType != "" && !contains(meterResourceTypes, m.ResourceType) {
		v.Add("resource_type", "invalid")
	}
	if m.ResourceType != "" && m.Kind != store.MeterPower && m.Kind != store.MeterWater {
		v.Add("resource_type", "not_chargeable")
	}
	if m.MultiplierMilli <= 0 {
		v.Add("multiplier_milli", "invalid")
	}
	if m.RolloverMilli != nil && *m.RolloverMilli <= 0 {
		v.Add("rollover_milli", "invalid")
	}
	if m.DailyThresholdMilli != nil && *m.DailyThresholdMilli <= 0 {
		v.Add("daily_threshold_milli", "invalid")
	}
}

// checkMeterRefs verifies the pedestal, resource, equipment and item of a meter.
func checkMeterRefs(ctx context.Context, db bun.IDB, scope store.MarinaScope, m *store.UtilityMeter) error {
	refs := map[string][]string{}
	if m.PedestalID != "" {
		refs[store.ElementPedestal] = []string{m.PedestalID}
	}
	if m.EquipmentID != "" {
		refs[store.ElementEquipment] = []string{m.EquipmentID}
	}
	if len(refs) > 0 {
		infos, err := store.ElementInfos(ctx, db, scope, refs)
		if err != nil {
			return err
		}
		if len(infos) != len(refs) {
			return ErrElementInvalid
		}
	}
	if m.ResourceID != "" {
		ok, err := store.ResourceExists(ctx, db, scope, m.ResourceType, m.ResourceID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrElementInvalid
		}
	}
	return checkItem(ctx, db, scope, m.ItemID)
}

func (s *Service) meterViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.UtilityMeter) ([]MeterView, error) {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	last, err := store.LastMeterReadings(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := make([]MeterView, 0, len(rows))
	for _, r := range rows {
		v := MeterView{Row: r}
		if l, ok := last[r.ID]; ok {
			v.Last = &l
		}
		out = append(out, v)
	}
	return out, nil
}

// ListMeters lists meters (marina.configure or presence.write). Route "dock" lists the active power and water
// meters (the dock reading round, offline allowlist).
func (s *Service) ListMeters(ctx context.Context, p *auth.Principal, marinaID string, f MeterFilter) ([]MeterView, string, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMarinaConfigure, auth.PermPresenceWrite)
	if err != nil {
		return nil, "", err
	}
	sf := store.UtilityMeterFilter{PedestalID: f.PedestalID, ResourceID: f.ResourceID, Active: f.Active, Q: f.Q}
	v := &auth.ValidationError{}
	if f.Kind != "" {
		if unitOf[f.Kind] == "" {
			v.Add("kind", "invalid")
		}
		sf.Kinds = []string{f.Kind}
	}
	switch f.Route {
	case "":
	case "dock":
		active := true
		sf.Active = &active
		if sf.Kinds == nil {
			sf.Kinds = []string{store.MeterPower, store.MeterWater}
		}
	default:
		v.Add("route", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, "", err
	}
	page, err := store.ListUtilityMeters(ctx, s.d.DB.DB, scope, sf, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.meterViews(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

func (s *Service) writeMeter(ctx context.Context, scope store.MarinaScope, p *auth.Principal, m *store.UtilityMeter, expected int, action string) error {
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := checkMeterRefs(ctx, tx, scope, m); err != nil {
			return err
		}
		var err error
		if expected == 0 {
			err = store.InsertUtilityMeter(ctx, tx, scope, m)
		} else {
			err = store.UpdateUtilityMeter(ctx, tx, scope, m, expected, s.now())
		}
		switch {
		case err != nil && store.IsUniqueViolation(err):
			return ErrCodeTaken
		case err != nil && store.IsForeignKeyViolation(err):
			return ErrElementInvalid
		case err != nil:
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), action, "utility_meter", m.ID,
			map[string]any{"code": m.Code, "kind": m.Kind, "resource_type": m.ResourceType, "active": m.Active}); err != nil {
			return err
		}
		if expected != 0 {
			// The resource or the multiplier may have changed: the open consumptions follow.
			if _, err := s.recompute(ctx, tx, scope, m); err != nil {
				return err
			}
		}
		return s.changed(ctx, tx, scope, "meter", m.ID, m.ID, "")
	})
}

// CreateMeter creates a meter (marina.configure).
func (s *Service) CreateMeter(ctx context.Context, p *auth.Principal, marinaID string, in MeterInput) (*MeterView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	now := s.now()
	m := &store.UtilityMeter{
		ID: store.NewID(), Code: in.Code, Kind: in.Kind, Unit: in.Unit, PedestalID: in.PedestalID, SocketCode: in.SocketCode,
		Serial: in.Serial, ResourceType: in.ResourceType, ResourceID: in.ResourceID, EquipmentID: in.EquipmentID, ItemID: in.ItemID,
		MultiplierMilli: in.MultiplierMilli, RolloverMilli: in.RolloverMilli, DailyThresholdMilli: in.DailyThresholdMilli,
		Active: in.Active == nil || *in.Active, CreatedAt: now, UpdatedAt: now,
	}
	if m.MultiplierMilli == 0 {
		m.MultiplierMilli = 1000
	}
	v := &auth.ValidationError{}
	validateMeter(m, v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := s.writeMeter(ctx, scope, p, m, 0, "utility_meter.create"); err != nil {
		return nil, err
	}
	return s.GetMeter(ctx, p, marinaID, m.ID)
}

// GetMeter reads a meter (marina.configure or presence.write).
func (s *Service) GetMeter(ctx context.Context, p *auth.Principal, marinaID, id string) (*MeterView, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermMarinaConfigure, auth.PermPresenceWrite)
	if err != nil {
		return nil, err
	}
	m, err := store.GetUtilityMeter(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	vs, err := s.meterViews(ctx, s.d.DB.DB, scope, []store.UtilityMeter{*m})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// Meter patch keys (the JSON names of UtilityMeterPatch). String values are string or nil; numbers *int64 or nil.
const (
	PatchCode                = "code"
	PatchPedestalID          = "pedestal_id"
	PatchSocketCode          = "socket_code"
	PatchSerial              = "serial"
	PatchResourceType        = "resource_type"
	PatchResourceID          = "resource_id"
	PatchEquipmentID         = "equipment_id"
	PatchItemID              = "item_id"
	PatchMultiplierMilli     = "multiplier_milli"
	PatchRolloverMilli       = "rollover_milli"
	PatchDailyThresholdMilli = "daily_threshold_milli"
	PatchActive              = "active"
)

func patchString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if s, ok := v.(*string); ok && s != nil {
		return *s
	}
	return ""
}

func patchInt(v any) *int64 {
	switch x := v.(type) {
	case *int64:
		return x
	case int64:
		return &x
	}
	return nil
}

// UpdateMeter changes a meter (marina.configure, If-Match). Kind and unit never change; a changed resource or
// multiplier recomputes the open consumptions (charged ones stay frozen).
func (s *Service) UpdateMeter(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch MeterPatch) (*MeterView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMarinaConfigure)
	if err != nil {
		return nil, err
	}
	m, err := store.GetUtilityMeter(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if int64(m.Version) != version {
		return nil, ErrVersionMismatch
	}
	for k, val := range patch.Set {
		switch k {
		case PatchCode:
			m.Code = patchString(val)
		case PatchPedestalID:
			m.PedestalID = patchString(val)
		case PatchSocketCode:
			m.SocketCode = patchString(val)
		case PatchSerial:
			m.Serial = patchString(val)
		case PatchResourceType:
			m.ResourceType = patchString(val)
		case PatchResourceID:
			m.ResourceID = patchString(val)
		case PatchEquipmentID:
			m.EquipmentID = patchString(val)
		case PatchItemID:
			m.ItemID = patchString(val)
		case PatchMultiplierMilli:
			if n := patchInt(val); n != nil {
				m.MultiplierMilli = *n
			} else {
				m.MultiplierMilli = 1000
			}
		case PatchRolloverMilli:
			m.RolloverMilli = patchInt(val)
		case PatchDailyThresholdMilli:
			m.DailyThresholdMilli = patchInt(val)
		case PatchActive:
			if b, ok := val.(bool); ok {
				m.Active = b
			} else if b, ok := val.(*bool); ok && b != nil {
				m.Active = *b
			}
		}
	}
	v := &auth.ValidationError{}
	validateMeter(m, v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	if err := s.writeMeter(ctx, scope, p, m, int(version), "utility_meter.update"); err != nil {
		return nil, err
	}
	return s.GetMeter(ctx, p, marinaID, id)
}
