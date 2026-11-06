package vessel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Enumerations and limits (m1-plan §3.1).
var (
	hullTypes   = []string{"monohull_motor", "monohull_sail", "catamaran", "trimaran", "rib", "gozzo", "other"}
	fuels       = []string{"petrol", "diesel", "electric", "hybrid", "none", "other"}
	powerAmps   = []int64{16, 32, 63, 125}
	roles       = []string{"owner", "operator", "skipper", "billing", "contact"}
	sources     = []string{"declared", "measured"}
	maxLengthCm = int64(200_000) // 2 km: far above any vessel, rejects unit mistakes (mm typed as cm)
)

// Party roles.
const (
	RoleOwner = "owner"
	maxShare  = 1000
	maxParty  = 20
	maxEngine = 8
)

// Unit classes (spec 11.2): informative, computed from the effective length.
const (
	UnitNatante      = "natante"
	UnitImbarcazione = "imbarcazione"
	UnitNave         = "nave"
)

// DefaultUnitThresholdsCm are the default limits between natante, imbarcazione and nave (m1-plan D18).
var DefaultUnitThresholdsCm = [2]int64{1000, 2400}

// Engine is one engine of a vessel.
type Engine struct {
	Make    string `json:"make,omitempty"`
	Model   string `json:"model,omitempty"`
	PowerKW *int64 `json:"power_kw,omitempty"`
	PowerHP *int64 `json:"power_hp,omitempty"`
	Serial  string `json:"serial,omitempty"`
	Hours   *int64 `json:"hours,omitempty"`
}

// Party is a customer's role on a vessel.
type Party struct {
	CustomerID  string
	DisplayName string
	Role        string
	ShareMillis int64 // owners only
}

// PartyInput is one party of a replacement.
type PartyInput struct {
	CustomerID  string
	Role        string
	ShareMillis *int64
}

// UserRef names the staff user who measured a vessel.
type UserRef = customer.UserRef

// Vessel is the API view of a vessel (documents are added by the document service).
type Vessel struct {
	Row              store.Vessel
	Engines          []Engine
	Parties          []Party
	MeasuredBy       *UserRef
	EffectiveLOACm   int64
	EffectiveBeamCm  int64
	EffectiveDraftCm int64 // 0 = unknown
	UnitClass        string
}

// Owner is an owner in vessel lists.
type Owner struct {
	CustomerID, DisplayName string
}

// Summary is a vessel in lists.
type Summary struct {
	ID, Name, Registration, HullType string
	LOACm, BeamCm                    int64
	EffectiveLOACm, EffectiveBeamCm  int64
	DimensionsSource                 string
	UnitClass                        string
	Owners                           []Owner
	ArchivedAt                       time.Time
}

// Patch lists the fields to set (nil = unchanged). For nullable fields the zero value clears them ("" for
// text, 0 for numbers).
type Patch struct {
	Name, Registration, RegistryOffice, Flag, HomePort *string
	HullType, Model, Builder, HullColor, HullMaterial  *string
	YearBuilt                                          *int64
	LOACm, LWLCm, BeamCm, DraftCm, AirDraftCm          *int64
	DisplacementKg                                     *int64
	DimensionsSource                                   *string
	Engines                                            *[]Engine
	Fuel                                               *string
	PowerRequiredA                                     *int64
	Notes                                              *string
}

// Measurement is a dock measurement.
type Measurement struct {
	LOACm, BeamCm, DraftCm, AirDraftCm int64 // draft and air draft optional (0)
}

func (s *Service) now() time.Time { return store.UTC(s.d.Clock.Now()) }

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

// Effective returns the dimensions used by price lists and fit: the measured ones when the source is
// "measured" and they are present, else the declared ones.
func Effective(v *store.Vessel) (loa, beam, draft int64) {
	loa, beam, draft = v.LOACm, v.BeamCm, v.DraftCm
	if v.DimensionsSource == "measured" {
		if v.MeasuredLOACm > 0 {
			loa = v.MeasuredLOACm
		}
		if v.MeasuredBeamCm > 0 {
			beam = v.MeasuredBeamCm
		}
		if v.MeasuredDraftCm > 0 {
			draft = v.MeasuredDraftCm
		}
	}
	return loa, beam, draft
}

// UnitClass returns the legal unit class of a length: natante below the first threshold, nave above the
// second, imbarcazione in between (bounds included).
func UnitClass(loaCm int64, thresholds [2]int64) string {
	switch {
	case loaCm < thresholds[0]:
		return UnitNatante
	case loaCm > thresholds[1]:
		return UnitNave
	default:
		return UnitImbarcazione
	}
}

// thresholds reads marinas.settings.registry.unit_class_thresholds_cm, falling back to the defaults.
func thresholds(ctx context.Context, db bun.IDB, scope store.MarinaScope) [2]int64 {
	raw, err := store.MarinaSettings(ctx, db, scope)
	if err != nil || len(raw) == 0 {
		return DefaultUnitThresholdsCm
	}
	var doc struct {
		Registry struct {
			Thresholds []int64 `json:"unit_class_thresholds_cm"`
		} `json:"registry"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return DefaultUnitThresholdsCm
	}
	t := doc.Registry.Thresholds
	if len(t) != 2 || t[0] <= 0 || t[1] <= t[0] {
		return DefaultUnitThresholdsCm
	}
	return [2]int64{t[0], t[1]}
}

func decodeEngines(j store.JSON) []Engine {
	out := []Engine{}
	if len(j) > 0 {
		_ = json.Unmarshal(j, &out)
	}
	if out == nil {
		out = []Engine{}
	}
	return out
}

// searchText is the normalised name and registration (with and without spaces).
func searchText(v *store.Vessel) string {
	parts := []string{customer.Fold(v.Name)}
	if v.Registration != "" {
		r := customer.Fold(v.Registration)
		parts = append(parts, r)
		if compact := strings.ReplaceAll(r, " ", ""); compact != r {
			parts = append(parts, compact)
		}
	}
	return strings.Join(parts, " ")
}

func clean(s string, maxRunes int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, len([]rune(s)) <= maxRunes
}

// apply validates p and writes it onto row, returning the names of the changed fields.
func apply(row *store.Vessel, p Patch, creating bool) ([]string, error) {
	v := &auth.ValidationError{}
	var changed []string
	mark := func(field string, cond bool) {
		if cond {
			changed = append(changed, field)
		}
	}
	text := func(field string, in *string, dst *string, maxRunes int) {
		if in == nil {
			return
		}
		t, ok := clean(*in, maxRunes)
		if !ok {
			v.Add(field, "too_long")
			return
		}
		mark(field, *dst != t)
		*dst = t
	}
	length := func(field string, in *int64, dst *int64, required bool) {
		if in == nil {
			return
		}
		n := *in
		switch {
		case n < 0 || n > maxLengthCm, required && n == 0:
			v.Add(field, "invalid")
			return
		}
		mark(field, *dst != n)
		*dst = n
	}
	text("name", p.Name, &row.Name, 100)
	text("registration", p.Registration, &row.Registration, 40)
	text("registry_office", p.RegistryOffice, &row.RegistryOffice, 100)
	text("home_port", p.HomePort, &row.HomePort, 100)
	text("model", p.Model, &row.Model, 100)
	text("builder", p.Builder, &row.Builder, 100)
	text("hull_color", p.HullColor, &row.HullColor, 40)
	text("hull_material", p.HullMaterial, &row.HullMaterial, 40)
	text("notes", p.Notes, &row.Notes, 5000)
	if p.Flag != nil {
		f := strings.ToUpper(strings.TrimSpace(*p.Flag))
		if f != "" && (len(f) != 2 || f[0] < 'A' || f[0] > 'Z' || f[1] < 'A' || f[1] > 'Z') {
			v.Add("flag", "invalid")
		}
		mark("flag", row.Flag != f)
		row.Flag = f
	}
	if p.HullType != nil {
		if !slices.Contains(hullTypes, *p.HullType) {
			v.Add("hull_type", "invalid")
		}
		mark("hull_type", row.HullType != *p.HullType)
		row.HullType = *p.HullType
	} else if creating {
		row.HullType = "other"
	}
	if p.YearBuilt != nil {
		y := *p.YearBuilt
		if y != 0 && (y < 1800 || y > 2200) {
			v.Add("year_built", "invalid")
		}
		mark("year_built", row.YearBuilt != y)
		row.YearBuilt = y
	}
	length("loa_cm", p.LOACm, &row.LOACm, true)
	length("beam_cm", p.BeamCm, &row.BeamCm, true)
	length("lwl_cm", p.LWLCm, &row.LWLCm, false)
	length("draft_cm", p.DraftCm, &row.DraftCm, false)
	length("air_draft_cm", p.AirDraftCm, &row.AirDraftCm, false)
	if p.DisplacementKg != nil {
		if *p.DisplacementKg < 0 || *p.DisplacementKg > 100_000_000 {
			v.Add("displacement_kg", "invalid")
		}
		mark("displacement_kg", row.DisplacementKg != *p.DisplacementKg)
		row.DisplacementKg = *p.DisplacementKg
	}
	if creating && (row.LOACm == 0) {
		v.Add("loa_cm", "required")
	}
	if creating && (row.BeamCm == 0) {
		v.Add("beam_cm", "required")
	}
	if p.DimensionsSource != nil {
		if !slices.Contains(sources, *p.DimensionsSource) {
			v.Add("dimensions_source", "invalid")
		} else if *p.DimensionsSource == "measured" && (row.MeasuredLOACm == 0 || row.MeasuredBeamCm == 0) {
			v.Add("dimensions_source", "not_measured")
		}
		mark("dimensions_source", row.DimensionsSource != *p.DimensionsSource)
		row.DimensionsSource = *p.DimensionsSource
	} else if creating {
		row.DimensionsSource = "declared"
	}
	if p.Engines != nil {
		es, ok := cleanEngines(*p.Engines)
		if !ok {
			v.Add("engines", "invalid")
		}
		b := store.MustJSON(es)
		mark("engines", string(row.Engines) != string(b))
		row.Engines = b
	} else if creating {
		row.Engines = store.JSON("[]")
	}
	if p.Fuel != nil {
		if *p.Fuel != "" && !slices.Contains(fuels, *p.Fuel) {
			v.Add("fuel", "invalid")
		}
		mark("fuel", row.Fuel != *p.Fuel)
		row.Fuel = *p.Fuel
	}
	if p.PowerRequiredA != nil {
		if *p.PowerRequiredA != 0 && !slices.Contains(powerAmps, *p.PowerRequiredA) {
			v.Add("power_required_a", "invalid")
		}
		mark("power_required_a", row.PowerRequiredA != *p.PowerRequiredA)
		row.PowerRequiredA = *p.PowerRequiredA
	}
	if row.Name == "" {
		v.Add("name", "required")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	row.SearchText = searchText(row)
	return changed, nil
}

func cleanEngines(in []Engine) ([]Engine, bool) {
	if len(in) > maxEngine {
		return nil, false
	}
	out := make([]Engine, 0, len(in))
	for _, e := range in {
		var ok1, ok2, ok3 bool
		e.Make, ok1 = clean(e.Make, 100)
		e.Model, ok2 = clean(e.Model, 100)
		e.Serial, ok3 = clean(e.Serial, 100)
		if !ok1 || !ok2 || !ok3 {
			return nil, false
		}
		for _, n := range []*int64{e.PowerKW, e.PowerHP, e.Hours} {
			if n != nil && (*n < 0 || *n > 1_000_000) {
				return nil, false
			}
		}
		out = append(out, e)
	}
	return out, true
}

// validateParties checks a party list against the customers of the marina (m1-plan §4.4).
func validateParties(ctx context.Context, db bun.IDB, scope store.MarinaScope, in []PartyInput) ([]store.VesselParty, error) {
	v := &auth.ValidationError{}
	if len(in) > maxParty {
		v.Add("parties", "too_many")
		return nil, v
	}
	ids := make([]string, 0, len(in))
	for _, p := range in {
		ids = append(ids, p.CustomerID)
	}
	custs, err := store.CustomersByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.Customer{}
	for i := range custs {
		byID[custs[i].ID] = &custs[i]
	}
	var out []store.VesselParty
	var total int64
	type key struct{ c, r string }
	seen := map[key]bool{}
	var conflict error
	for i, p := range in {
		field := fmt.Sprintf("parties[%d]", i)
		c, ok := byID[p.CustomerID]
		switch {
		case !ok:
			v.Add(field+".customer_id", "unknown_customer")
			continue
		case !c.AnonymizedAt.IsZero():
			conflict = customer.ErrAnonymized
		case c.MergedIntoID != "":
			conflict = customer.ErrMerged
		}
		if !slices.Contains(roles, p.Role) {
			v.Add(field+".role", "invalid")
			continue
		}
		if seen[key{p.CustomerID, p.Role}] {
			v.Add(field, "duplicate")
			continue
		}
		seen[key{p.CustomerID, p.Role}] = true
		row := store.VesselParty{ID: store.NewID(), MarinaID: scope.ID(), CustomerID: p.CustomerID, Role: p.Role}
		if p.Role == RoleOwner {
			if p.ShareMillis == nil || *p.ShareMillis < 1 || *p.ShareMillis > maxShare {
				v.Add(field+".share_millis", "owner_share_required")
				continue
			}
			row.ShareMillis = *p.ShareMillis
			total += row.ShareMillis
		} else if p.ShareMillis != nil {
			v.Add(field+".share_millis", "share_not_allowed")
			continue
		}
		out = append(out, row)
	}
	if total > maxShare {
		v.Add("parties", "owner_shares_exceed")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	if conflict != nil {
		return nil, conflict
	}
	return out, nil
}

func (s *Service) writeAudit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, action, id string, diff any) error {
	return s.d.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action,
		EntityType: "vessel", EntityID: id, Diff: diff,
	})
}

func (s *Service) emit(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id, change string) error {
	_, err := s.d.Outbox.Write(ctx, tx, &scope, event.VesselChanged, event.VesselChangedData{VesselID: id, Change: change})
	return err
}

// load builds the view of a vessel row.
func (s *Service) load(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.Vessel) (*Vessel, error) {
	out, err := s.views(ctx, db, scope, []store.Vessel{*row})
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

func (s *Service) views(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []store.Vessel) ([]Vessel, error) {
	ids := make([]string, 0, len(rows))
	var userIDs []string
	for i := range rows {
		ids = append(ids, rows[i].ID)
		userIDs = append(userIDs, rows[i].MeasuredByUserID)
	}
	parties, err := partiesOf(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	users, err := customer.UserRefs(ctx, db, userIDs)
	if err != nil {
		return nil, err
	}
	th := thresholds(ctx, db, scope)
	out := make([]Vessel, 0, len(rows))
	for i := range rows {
		r := rows[i]
		loa, beam, draft := Effective(&r)
		ps := parties[r.ID]
		if ps == nil {
			ps = []Party{}
		}
		out = append(out, Vessel{
			Row: r, Engines: decodeEngines(r.Engines), Parties: ps, MeasuredBy: users[r.MeasuredByUserID],
			EffectiveLOACm: loa, EffectiveBeamCm: beam, EffectiveDraftCm: draft, UnitClass: UnitClass(loa, th),
		})
	}
	return out, nil
}

// partiesOf returns the parties of the vessels with the customers' display names, owners first.
func partiesOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, vesselIDs []string) (map[string][]Party, error) {
	rows, err := store.PartiesOfVessels(ctx, db, scope, vesselIDs)
	if err != nil {
		return nil, err
	}
	cids := make([]string, 0, len(rows))
	for _, r := range rows {
		cids = append(cids, r.CustomerID)
	}
	custs, err := store.CustomersByIDs(ctx, db, scope, cids)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for i := range custs {
		names[custs[i].ID] = customer.DisplayName(&custs[i])
	}
	out := map[string][]Party{}
	for _, r := range rows {
		out[r.VesselID] = append(out[r.VesselID], Party{CustomerID: r.CustomerID, DisplayName: names[r.CustomerID], Role: r.Role, ShareMillis: r.ShareMillis})
	}
	for id := range out {
		slices.SortStableFunc(out[id], func(a, b Party) int {
			return slices.Index(roles, a.Role) - slices.Index(roles, b.Role)
		})
	}
	return out, nil
}

// Create adds a vessel, optionally with its parties (customers.write).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in Patch, parties []PartyInput) (*Vessel, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	now := s.now()
	row := &store.Vessel{ID: store.NewID(), MarinaID: scope.ID(), Version: 1, CreatedAt: now, UpdatedAt: now}
	changed, err := apply(row, in, true)
	if err != nil {
		return nil, err
	}
	var out *Vessel
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ps, err := validateParties(ctx, tx, scope, parties)
		if err != nil {
			return err
		}
		if err := store.InsertVessel(ctx, tx, row); err != nil {
			return err
		}
		for i := range ps {
			ps[i].VesselID, ps[i].CreatedAt, ps[i].UpdatedAt = row.ID, now, now
		}
		if err := store.ReplaceVesselParties(ctx, tx, scope, row.ID, ps); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, p, "vessel.create", row.ID, map[string]any{"fields": changed, "parties": len(ps)}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, row.ID, event.ChangeCreated); err != nil {
			return err
		}
		out, err = s.load(ctx, tx, scope, row)
		return err
	})
	return out, err
}

// Get returns a vessel with its parties (customers.read).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, id string) (*Vessel, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetVessel(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	return s.load(ctx, s.d.DB.DB, scope, row)
}

// GetMany returns the vessels with the given ids (unknown ids skipped), ordered by id (customers.read).
func (s *Service) GetMany(ctx context.Context, p *auth.Principal, marinaID string, ids []string) ([]Vessel, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	rows, err := store.VesselsByIDs(ctx, s.d.DB.DB, scope, ids)
	if err != nil {
		return nil, err
	}
	return s.views(ctx, s.d.DB.DB, scope, rows)
}

// Filter narrows List.
type Filter = store.VesselFilter

// List returns a page of vessels (customers.read).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f Filter, cursor string, limit int) (store.Page[Summary], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return store.Page[Summary]{}, err
	}
	if f.HullType != "" && !slices.Contains(hullTypes, f.HullType) {
		return store.Page[Summary]{}, &auth.ValidationError{Fields: []auth.FieldError{{Field: "hull_type", Code: "invalid"}}}
	}
	f.Search = customer.SearchTerm(f.Search)
	page, err := store.ListVessels(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[Summary]{}, err
	}
	views, err := s.views(ctx, s.d.DB.DB, scope, page.Items)
	if err != nil {
		return store.Page[Summary]{}, err
	}
	return store.Page[Summary]{Items: Summaries(views), NextCursor: page.NextCursor}, nil
}

// Summaries converts vessel views to list summaries.
func Summaries(vs []Vessel) []Summary {
	out := make([]Summary, 0, len(vs))
	for i := range vs {
		v := &vs[i]
		owners := []Owner{}
		for _, p := range v.Parties {
			if p.Role == RoleOwner {
				owners = append(owners, Owner{CustomerID: p.CustomerID, DisplayName: p.DisplayName})
			}
		}
		out = append(out, Summary{
			ID: v.Row.ID, Name: v.Row.Name, Registration: v.Row.Registration, HullType: v.Row.HullType,
			LOACm: v.Row.LOACm, BeamCm: v.Row.BeamCm, EffectiveLOACm: v.EffectiveLOACm, EffectiveBeamCm: v.EffectiveBeamCm,
			DimensionsSource: v.Row.DimensionsSource, UnitClass: v.UnitClass, Owners: owners, ArchivedAt: v.Row.ArchivedAt,
		})
	}
	return out
}

// Update changes a vessel when its version equals ifMatch (customers.write).
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in Patch) (*Vessel, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	var out *Vessel
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetVessel(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		changed, err := apply(row, in, false)
		if err != nil {
			return err
		}
		row.UpdatedAt = s.now()
		if err := store.UpdateVessel(ctx, tx, scope, ifMatch, row); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, p, "vessel.update", row.ID, map[string]any{"fields": changed}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, row.ID, event.ChangeUpdated); err != nil {
			return err
		}
		out, err = s.load(ctx, tx, scope, row)
		return err
	})
	return out, err
}

// ReplaceParties replaces the parties of a vessel and bumps its version (customers.write, If-Match).
func (s *Service) ReplaceParties(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, parties []PartyInput) (*Vessel, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	var out *Vessel
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetVessel(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if row.Version != ifMatch {
			return ErrVersionMismatch
		}
		ps, err := validateParties(ctx, tx, scope, parties)
		if err != nil {
			return err
		}
		now := s.now()
		for i := range ps {
			ps[i].VesselID, ps[i].CreatedAt, ps[i].UpdatedAt = row.ID, now, now
		}
		if err := store.ReplaceVesselParties(ctx, tx, scope, row.ID, ps); err != nil {
			return err
		}
		row.UpdatedAt = now
		if err := store.UpdateVessel(ctx, tx, scope, ifMatch, row); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, p, "vessel.parties", row.ID, map[string]any{"parties": len(ps)}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, row.ID, event.ChangeParties); err != nil {
			return err
		}
		out, err = s.load(ctx, tx, scope, row)
		return err
	})
	return out, err
}

// RecordMeasurement stores the dimensions measured at the dock and makes them the effective ones
// (customers.write or presence.write, m1-plan D12).
func (s *Service) RecordMeasurement(ctx context.Context, p *auth.Principal, marinaID, id string, m Measurement) (*Vessel, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if errors.Is(err, auth.ErrForbidden) {
		scope, err = p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPresenceWrite)
	}
	if err != nil {
		return nil, err
	}
	var out *Vessel
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetVessel(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		v := &auth.ValidationError{}
		for _, f := range []struct {
			name     string
			n        int64
			required bool
		}{{"loa_cm", m.LOACm, true}, {"beam_cm", m.BeamCm, true}, {"draft_cm", m.DraftCm, false}, {"air_draft_cm", m.AirDraftCm, false}} {
			if f.n < 0 || f.n > maxLengthCm || (f.required && f.n == 0) {
				v.Add(f.name, "invalid")
			}
		}
		if err := v.Err(); err != nil {
			return err
		}
		now := s.now()
		row.MeasuredLOACm, row.MeasuredBeamCm, row.MeasuredDraftCm, row.MeasuredAirDraftCm = m.LOACm, m.BeamCm, m.DraftCm, m.AirDraftCm
		row.MeasuredAt, row.MeasuredByUserID, row.DimensionsSource, row.UpdatedAt = now, p.UserID, "measured", now
		if err := store.UpdateVessel(ctx, tx, scope, row.Version, row); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, p, "vessel.measure", row.ID, map[string]any{
			"loa_cm": m.LOACm, "beam_cm": m.BeamCm, "draft_cm": m.DraftCm, "air_draft_cm": m.AirDraftCm,
		}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, row.ID, event.ChangeMeasured); err != nil {
			return err
		}
		out, err = s.load(ctx, tx, scope, row)
		return err
	})
	return out, err
}

// Archive hides a vessel from the default list (customers.write).
func (s *Service) Archive(ctx context.Context, p *auth.Principal, marinaID, id string) error {
	return s.setArchived(ctx, p, marinaID, id, true)
}

// Unarchive restores an archived vessel (customers.write).
func (s *Service) Unarchive(ctx context.Context, p *auth.Principal, marinaID, id string) error {
	return s.setArchived(ctx, p, marinaID, id, false)
}

func (s *Service) setArchived(ctx context.Context, p *auth.Principal, marinaID, id string, archived bool) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetVessel(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if archived == !row.ArchivedAt.IsZero() {
			return store.ErrConflict
		}
		now := s.now()
		action, change := "vessel.unarchive", event.ChangeUnarchived
		row.ArchivedAt = time.Time{}
		if archived {
			action, change = "vessel.archive", event.ChangeArchived
			row.ArchivedAt = now
		}
		row.UpdatedAt = now
		if err := store.UpdateVessel(ctx, tx, scope, row.Version, row); err != nil {
			return versionErr(err)
		}
		if err := s.writeAudit(ctx, tx, scope, p, action, row.ID, nil); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, row.ID, change)
	})
}
