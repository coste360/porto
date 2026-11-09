package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/profile/aggregate"
	"github.com/coste360/porto/internal/domain/profile/jsonld"
	"github.com/coste360/porto/internal/domain/profile/simplemap"
	"github.com/coste360/porto/internal/domain/slug"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Public operations (m12-plan §12.2): no principal, the marina is resolved from the slug; an unpublished profile,
// an unknown slug or a section switched off is ErrNotPublished (404 with the body of an unknown slug). Callers set
// the documented Cache-Control and never set cookies.

// cacheTTL is the life of the cached head and aggregate occupancy.
const cacheTTL = 60 * time.Second

// published is a resolved published profile.
type published struct {
	scope  store.MarinaScope
	marina *store.Marina
	row    *store.MarinaProfile
}

// resolve returns the published profile of slug, or ErrNotPublished.
func (s *Service) resolve(ctx context.Context, db bun.IDB, slugValue string) (*published, error) {
	if slug.Validate(slugValue) != nil {
		return nil, ErrNotPublished
	}
	m, err := store.MarinaBySlug(ctx, db, slugValue)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotPublished
		}
		return nil, err
	}
	scope, err := store.SystemMarinaScope(ctx, db, m.ID)
	if err != nil {
		return nil, ErrNotPublished
	}
	row, err := store.GetMarinaProfile(ctx, db, scope)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotPublished
		}
		return nil, err
	}
	if !row.Published {
		return nil, ErrNotPublished
	}
	return &published{scope: scope, marina: m, row: row}, nil
}

// langOf returns lang when the profile publishes it, else it.
func langOf(row *store.MarinaProfile, m *store.Marina, lang string) (string, []string) {
	var locales []string
	decodeJSON(row.Locales, &locales)
	allowed := marinaLocales(m)
	out := []string{"it"}
	for _, l := range locales {
		if slices.Contains(allowed, l) && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	if !slices.Contains(out, lang) {
		lang = "it"
	}
	return lang, out
}

// pick resolves a text in lang, falling back to it.
func pick(t Text, lang string) string {
	if v := t[lang]; v != "" {
		return v
	}
	return t["it"]
}

func (s *Service) photoURL(slugValue, fileID string) string {
	return "/api/public/v1/" + slugValue + "/profile/photos/" + fileID
}

func (s *Service) publicPhotos(slugValue string, raw store.JSON, lang string) ([]PublicPhoto, []string) {
	var ps []Photo
	decodeJSON(raw, &ps)
	slices.SortStableFunc(ps, func(a, b Photo) int { return a.Sort - b.Sort })
	out := make([]PublicPhoto, 0, len(ps))
	ids := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, PublicPhoto{URL: s.photoURL(slugValue, p.FileID), Alt: pick(p.Alt, lang)})
		ids = append(ids, p.FileID)
	}
	return out, ids
}

// amenities maps the services to the JSON-LD amenity keys.
func amenities(sv Services) map[string]bool {
	return map[string]bool{
		"water": sv.Water, "power": len(sv.PowerAmps) > 0, "fuel": sv.Fuel != nil && (sv.Fuel.Petrol || sv.Fuel.Diesel),
		"wifi": sv.WiFi, "toilets": sv.Toilets, "showers": sv.Showers, "laundry": sv.Laundry, "pump_out": sv.PumpOut,
		"waste": sv.Waste, "travel_lift": sv.TravelLiftKg != nil && *sv.TravelLiftKg > 0, "crane": sv.CraneKg != nil && *sv.CraneKg > 0,
		"slipway": sv.Slipway, "yard": sv.Yard, "storage": sv.Storage, "rental": sv.Rental, "parking": sv.Parking,
		"restaurant": sv.Restaurant, "shops": sv.Shops, "transport": sv.Transport,
	}
}

// publicOf builds the public JSON and the JSON-LD of a profile row (published or draft) in lang.
func (s *Service) publicOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.MarinaProfile, m *store.Marina, lang string) (*Public, map[string]any, error) {
	lang, locales := langOf(row, m, lang)
	base := s.baseURL()
	var addr Address
	var vhf []int
	var office, dock []Hours
	var hoursNote, accessNotes, lights, description Text
	var shelter []string
	var sv Services
	decodeJSON(row.Address, &addr)
	decodeJSON(row.VHFChannels, &vhf)
	decodeJSON(row.OfficeHours, &office)
	decodeJSON(row.DockHours, &dock)
	decodeJSON(row.HoursNote, &hoursNote)
	decodeJSON(row.AccessNotes, &accessNotes)
	decodeJSON(row.Lights, &lights)
	decodeJSON(row.Description, &description)
	decodeJSON(row.Shelter, &shelter)
	decodeJSON(row.Services, &sv)
	total, transit, err := berthFigures(ctx, db, scope, row)
	if err != nil {
		return nil, nil, err
	}
	sections, err := s.sections(ctx, db, scope, row, m)
	if err != nil {
		return nil, nil, err
	}
	photos, photoIDs := s.publicPhotos(m.Slug, row.Photos, lang)
	access, _ := s.publicPhotos(m.Slug, row.AccessImages, lang)
	d := &PublicData{
		Slug: m.Slug, Name: m.Name, Lang: lang, Locales: locales, Description: pick(description, lang),
		EntranceLatE7: row.EntranceLatE7, EntranceLonE7: row.EntranceLonE7, Phone: row.Phone, Email: row.Email,
		Website: row.Website, VHFChannels: vhf, OfficeHours: office, DockHours: dock, HoursNote: pick(hoursNote, lang),
		BerthsTotal: &total, TransitBerths: &transit, MaxLoaCm: row.MaxLoaCm, DepthEntranceCm: row.DepthEntranceCm,
		DepthQuayCm: row.DepthQuayCm, DepthMinCm: row.DepthMinCm, DepthMaxCm: row.DepthMaxCm, Seabed: row.Seabed,
		Shelter: shelter, AccessNotes: pick(accessNotes, lang), AccessImages: access, Lights: pick(lights, lang),
		Photos: photos, Sections: sections, PageURL: pageURL(base, m.Slug), Version: row.Version,
	}
	if row.EntranceLatE7 != nil && row.EntranceLonE7 != nil {
		d.Latitude, d.Longitude = jsonld.FormatE7(*row.EntranceLatE7), jsonld.FormatE7(*row.EntranceLonE7)
	}
	if addr != (Address{}) {
		d.Address = &addr
	}
	if sv.Fuel != nil {
		d.FuelHours = pick(sv.Fuel.Hours, lang)
		f := *sv.Fuel
		f.Hours = nil
		sv.Fuel = &f
	}
	d.Services = &sv
	if m.BookingEnabled {
		d.BookingURL = "/b/" + m.Slug
	}
	hours := make([]jsonld.Hours, 0, len(office))
	for _, h := range office {
		hours = append(hours, jsonld.Hours{Weekday: h.Weekday, From: h.From, To: h.To})
	}
	ld := jsonld.Build(jsonld.Profile{
		Slug: m.Slug, Name: m.Name, Description: d.Description, Phone: row.Phone, Email: row.Email, Website: row.Website,
		Street: addr.Street, PostalCode: addr.PostalCode, City: addr.City, Province: addr.Province, Country: addr.Country,
		LatE7: row.EntranceLatE7, LonE7: row.EntranceLonE7, OfficeHours: hours, Amenities: amenities(sv),
		PhotoIDs: photoIDs, BookingEnabled: m.BookingEnabled,
	}, lang, base)
	// The body carries live figures (berth counts, section flags) besides the profile row: the ETag is a hash of
	// what is served, so a change of any of them is never answered 304.
	body, err := json.Marshal(struct {
		D  *PublicData
		LD map[string]any
	}{d, ld})
	if err != nil {
		return nil, nil, err
	}
	etag := `"p` + shortHash(string(body)) + `"`
	pub := &Public{Slug: m.Slug, Name: m.Name, Lang: lang, Locales: locales, Version: row.Version, ETag: etag, Data: d}
	return pub, ld, nil
}

// sections returns which optional sections the page shows: each needs its switch and its data.
func (s *Service) sections(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.MarinaProfile, m *store.Marina) (PublicSections, error) {
	out := PublicSections{}
	if row.ShowMap {
		mp, err := mapOf(ctx, db, scope, row)
		if err != nil {
			return out, err
		}
		out.Map = mp != nil
	}
	if row.ShowPrices {
		ok, err := s.pricesAvailable(ctx, db, scope, m)
		if err != nil {
			return out, err
		}
		out.Prices = ok
	}
	var cats []string
	decodeJSON(row.OccupancyCategories, &cats)
	out.Occupancy = row.ShowOccupancy && len(cats) > 0
	return out, nil
}

// mapOf returns the map of the profile (map_id, else the first map), nil when the marina has none.
func mapOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, row *store.MarinaProfile) (*store.Map, error) {
	if row.MapID != "" {
		mp, err := store.GetMap(ctx, db, scope, row.MapID)
		if err == nil {
			return mp, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	maps, err := store.ListMaps(ctx, db, scope)
	if err != nil || len(maps) == 0 {
		return nil, err
	}
	return &maps[0], nil
}

// PublicProfile returns the published profile in lang (getPublicProfile).
func (s *Service) PublicProfile(ctx context.Context, slugValue, lang string) (*Public, error) {
	var out *Public
	err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pp, err := s.resolve(ctx, tx, slugValue)
		if err != nil {
			return err
		}
		out, _, err = s.publicOf(ctx, tx, pp.scope, pp.row, pp.marina, lang)
		return err
	})
	return out, err
}

// structureGeom is the geometry column of harbor_structures.
type structureGeom struct {
	Points []geo.Point `json:"points"`
	Closed bool        `json:"closed"`
	Rect   *struct {
		Anchor     geo.Point `json:"anchor"`
		WidthCm    int64     `json:"width_cm"`
		LengthCm   int64     `json:"length_cm"`
		RotationDd int64     `json:"rotation_dd"`
	} `json:"rect"`
}

func pts(ps []geo.Point) []simplemap.Point {
	out := make([]simplemap.Point, 0, len(ps))
	for _, p := range ps {
		out = append(out, simplemap.Point{int64(p.X), int64(p.Y)})
	}
	return out
}

func rectPts(r geo.Rect) []simplemap.Point {
	c := r.Corners()
	return pts(c[:])
}

// circlePts approximates a swing circle with 16 points from the fixed-point trigonometry table.
func circlePts(c geo.Point, r int64) []simplemap.Point {
	out := make([]simplemap.Point, 0, 16)
	for i := range 16 {
		a := geo.NewAngle(int64(i) * 225)
		out = append(out, simplemap.Point{
			int64(c.X) + geo.DivRoundHalfEven(r*geo.Cos(a), geo.TrigScale),
			int64(c.Y) + geo.DivRoundHalfEven(r*geo.Sin(a), geo.TrigScale),
		})
	}
	return out
}

// mapInput converts the stored elements of a map to the simplified map input (geometry only).
func mapInput(mp *store.Map, el *store.MapElements, pois []POI) simplemap.Input {
	in := simplemap.Input{MaxXCm: mp.WidthCm, MaxYCm: mp.HeightCm, Version: mp.Version}
	structs := map[string]layout.Structure{}
	for _, st := range el.Structures {
		var g structureGeom
		decodeJSON(st.Geometry, &g)
		var attrs struct {
			Sides []string `json:"sides"`
		}
		decodeJSON(st.Attributes, &attrs)
		ls := layout.Structure{Kind: layout.StructureKind(st.Kind), Points: g.Points, Closed: g.Closed, Width: geo.Cm(st.WidthCm)}
		for _, sd := range attrs.Sides {
			ls.Sides = append(ls.Sides, layout.Side(sd))
		}
		structs[st.ID] = ls
		switch {
		case g.Rect != nil:
			r := geo.Rect{Anchor: g.Rect.Anchor, Width: geo.Cm(g.Rect.WidthCm), Length: geo.Cm(g.Rect.LengthCm), Rotation: geo.Angle(g.Rect.RotationDd)}
			in.Structures = append(in.Structures, simplemap.Shape{Points: rectPts(r), Closed: true})
		default:
			in.Structures = append(in.Structures, simplemap.Shape{Points: pts(g.Points), Closed: g.Closed, WidthCm: st.WidthCm})
		}
	}
	for _, b := range el.Berths {
		if !b.Active {
			continue
		}
		var shape []simplemap.Point
		if b.MooringType == string(layout.MooringBuoy) {
			shape = circlePts(geo.Point{X: geo.Cm(b.AnchorXCm), Y: geo.Cm(b.AnchorYCm)}, max(b.SwingRadiusCm, 100))
		} else {
			shape = rectPts(geo.Rect{
				Anchor: geo.Point{X: geo.Cm(b.AnchorXCm), Y: geo.Cm(b.AnchorYCm)}, Width: geo.Cm(b.WidthCm),
				Length: geo.Cm(b.LengthCm), Rotation: geo.Angle(b.RotationDd),
			})
		}
		in.Berths = append(in.Berths, simplemap.Shape{Points: shape, Closed: true, Category: b.PriceClass})
	}
	for _, a := range el.Areas {
		if !a.Active {
			continue
		}
		if a.Kind == string(layout.AreaSurface) {
			var outline []geo.Point
			decodeJSON(a.Outline, &outline)
			in.Areas = append(in.Areas, simplemap.Shape{Points: pts(outline), Closed: true})
			continue
		}
		la := layout.DynamicArea{Kind: layout.AreaLinear, Side: layout.Side(a.Side), Mode: layout.MooringMode(a.MooringMode), Margin: geo.Cm(a.MarginCm), MaxLOA: geo.Cm(a.MaxLOACm), MaxBeam: geo.Cm(a.MaxBeamCm)}
		if a.FromCm != nil {
			la.From = geo.Cm(*a.FromCm)
		}
		if a.ToCm != nil {
			la.To = geo.Cm(*a.ToCm)
		}
		if r, ok := layout.LinearBand(la, structs[a.StructureID]); ok {
			in.Areas = append(in.Areas, simplemap.Shape{Points: rectPts(r), Closed: true})
		}
	}
	for _, l := range el.LandResources {
		if !l.Active {
			continue
		}
		r := geo.Rect{Anchor: geo.Point{X: geo.Cm(l.XCm), Y: geo.Cm(l.YCm)}, Width: geo.Cm(l.WidthCm), Length: geo.Cm(l.LengthCm), Rotation: geo.Angle(l.RotationDd)}
		in.Land = append(in.Land, simplemap.Shape{Points: rectPts(r), Closed: true})
	}
	for _, p := range pois {
		in.POI = append(in.POI, simplemap.POI{Kind: p.Kind, XCm: p.XCm, YCm: p.YCm})
	}
	return in
}

func publicShapes(in []simplemap.Shape) []PublicShape {
	out := make([]PublicShape, 0, len(in))
	for _, s := range in {
		ps := PublicShape{Points: make([][2]int64, 0, len(s.Points)), Closed: s.Closed, Category: s.Category}
		for _, p := range s.Points {
			ps.Points = append(ps.Points, [2]int64(p))
		}
		if s.WidthCm > 0 {
			w := s.WidthCm
			ps.WidthCm = &w
		}
		out = append(out, ps)
	}
	return out
}

// PublicMap returns the simplified map (getPublicProfileMap) and its ETag; ErrNotPublished when the section is off.
func (s *Service) PublicMap(ctx context.Context, slugValue string) (*PublicMapData, string, error) {
	var out *PublicMapData
	var etag string
	err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pp, err := s.resolve(ctx, tx, slugValue)
		if err != nil {
			return err
		}
		if !pp.row.ShowMap {
			return ErrNotPublished
		}
		mp, err := mapOf(ctx, tx, pp.scope, pp.row)
		if err != nil {
			return err
		}
		if mp == nil {
			return ErrNotPublished
		}
		el, err := store.LoadMapElements(ctx, tx, pp.scope, mp.ID)
		if err != nil {
			return err
		}
		var pois []POI
		decodeJSON(pp.row.POI, &pois)
		m := simplemap.Build(mapInput(mp, el, pois))
		out = &PublicMapData{
			MinXCm: m.MinXCm, MinYCm: m.MinYCm, MaxXCm: m.MaxXCm, MaxYCm: m.MaxYCm, Structures: publicShapes(m.Structures),
			Berths: publicShapes(m.Berths), Areas: publicShapes(m.Areas), Land: publicShapes(m.Land), POI: []POI{}, Version: m.Version,
		}
		for _, p := range m.POI {
			out.POI = append(out.POI, POI{Kind: p.Kind, XCm: p.XCm, YCm: p.YCm})
		}
		etag = fmt.Sprintf(`"m%d-%d"`, m.Version, pp.row.Version)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return out, etag, nil
}

func shortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:8])
}

// PublicPrices returns the transit price matrix (getPublicProfilePrices) and its ETag; ErrNotPublished when the
// section is off, pricelist.ErrNoPublicMatrix when no list publishes it (both 404).
func (s *Service) PublicPrices(ctx context.Context, slugValue, lang string) (*PublicPrices, string, error) {
	var pp *published
	err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		pp, err = s.resolve(ctx, tx, slugValue)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	if !pp.row.ShowPrices || s.d.Prices == nil {
		return nil, "", ErrNotPublished
	}
	lang, _ = langOf(pp.row, pp.marina, lang)
	day := s.today(pp.marina)
	pm, err := s.d.Prices.PublicTransitMatrix(ctx, pp.scope, lang, day)
	if err != nil {
		return nil, "", err
	}
	out := &PublicPrices{
		QuotedOn: pm.QuotedOn.String(), Currency: pm.Currency, PriceIncludesVAT: pm.PriceIncludesVAT,
		Rows: []PublicPriceRow{}, Columns: []PublicPriceColumn{}, Cells: []PublicPriceCell{},
	}
	for _, r := range pm.Rows {
		out.Rows = append(out.Rows, PublicPriceRow{Key: r.Key, Label: r.Label, MaxLoaCm: r.MaxLoaCm, MaxBeamCm: r.MaxBeamCm})
	}
	for _, c := range pm.Columns {
		out.Columns = append(out.Columns, PublicPriceColumn{Key: c.Key, Label: c.Label, Arrival: c.Arrival.String(), Departure: c.Departure.String()})
	}
	for _, c := range pm.Cells {
		out.Cells = append(out.Cells, PublicPriceCell{Row: c.Row, Col: c.Col, TotalCents: c.TotalCents, Unit: c.Unit})
	}
	etag := `"x` + shortHash(pm.PriceListID, strconv.Itoa(pm.PriceListVersion), day.String(), lang) + `"`
	return out, etag, nil
}

// closedBerths returns the berths a closure covers on night (scopes marina, map, structure with its parent,
// group, resource).
func closedBerths(ctx context.Context, db bun.IDB, scope store.MarinaScope, night calendar.Date, berths []store.PublicBerthRow) (map[string]bool, error) {
	closures, err := store.ListClosures(ctx, db, scope, night, night)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	if len(closures) == 0 {
		return out, nil
	}
	parents, err := store.StructureParents(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	groups, err := store.PublicBerthGroups(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	for _, c := range closures {
		for _, b := range berths {
			hit := false
			switch c.ScopeType {
			case store.ClosureMarina:
				hit = true
			case store.ClosureMap:
				hit = b.MapID == c.ScopeID
			case store.ClosureStructure:
				hit = b.StructureID != "" && (b.StructureID == c.ScopeID || parents[b.StructureID] == c.ScopeID)
			case store.ClosureGroup:
				hit = slices.Contains(groups[b.ID], c.ScopeID)
			case store.ClosureResource:
				hit = c.ResourceType == "berth" && b.ID == c.ScopeID
			}
			if hit {
				out[b.ID] = true
			}
		}
	}
	return out, nil
}

// PublicOccupancy returns the aggregate occupancy (getPublicOccupancy), computed at most every 60 s per marina;
// ErrNotPublished when the option is off.
func (s *Service) PublicOccupancy(ctx context.Context, slugValue string) (*PublicOccupancyData, string, error) {
	var pp *published
	err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		pp, err = s.resolve(ctx, tx, slugValue)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	var cats []string
	decodeJSON(pp.row.OccupancyCategories, &cats)
	if !pp.row.ShowOccupancy || len(cats) == 0 {
		return nil, "", ErrNotPublished
	}
	now := s.d.Clock.Now()
	s.mu.Lock()
	if e, ok := s.occupancy[pp.scope.ID()]; ok && now.Before(e.expires) {
		s.mu.Unlock()
		return e.data, e.etag, nil
	}
	s.mu.Unlock()
	var out *PublicOccupancyData
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		night := s.today(pp.marina)
		rows, err := store.PublicActiveBerths(ctx, tx, pp.scope)
		if err != nil {
			return err
		}
		occupied, err := store.PublicOccupiedBerths(ctx, tx, pp.scope, night, store.UTC(now))
		if err != nil {
			return err
		}
		closed, err := closedBerths(ctx, tx, pp.scope, night, rows)
		if err != nil {
			return err
		}
		physical, err := store.SensorPhysicalByBerth(ctx, tx, pp.scope)
		if err != nil {
			return err
		}
		berths := make([]aggregate.Berth, 0, len(rows))
		for _, r := range rows {
			berths = append(berths, aggregate.Berth{ID: r.ID, Category: r.PriceClass, Occupied: occupied[r.ID], Closed: closed[r.ID]})
		}
		counts := aggregate.Count(cats, berths, physical)
		all, err := store.ListBerthCategories(ctx, tx, pp.scope, true)
		if err != nil {
			return err
		}
		names := map[string]CategoryName{}
		for _, c := range all {
			var n struct {
				It string `json:"it"`
				En string `json:"en"`
			}
			decodeJSON(c.Name, &n)
			cn := CategoryName{It: n.It}
			if cn.It == "" {
				cn.It = c.Code
			}
			if n.En != "" {
				en := n.En
				cn.En = &en
			}
			names[c.Code] = cn
		}
		out = &PublicOccupancyData{AsOf: store.UTC(now).Truncate(5 * time.Minute), Night: night.String(), Categories: []PublicOccupancyCategory{}}
		for _, c := range counts {
			n, ok := names[c.Code]
			if !ok {
				n = CategoryName{It: c.Code}
			}
			out.Categories = append(out.Categories, PublicOccupancyCategory{Code: c.Code, Name: n, FreeBerths: c.Free, TotalBerths: c.Total})
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	body, _ := json.Marshal(out)
	etag := `"o` + shortHash(string(body)) + `"`
	s.mu.Lock()
	s.occupancy[pp.scope.ID()] = occupancyEntry{data: out, etag: etag, expires: now.Add(cacheTTL)}
	s.mu.Unlock()
	return out, etag, nil
}

// PublicPhoto returns a photo referenced by the published profile (getPublicProfilePhoto).
func (s *Service) PublicPhoto(ctx context.Context, slugValue, fileID string) (*Image, error) {
	var pp *published
	err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		pp, err = s.resolve(ctx, tx, slugValue)
		return err
	})
	if err != nil {
		return nil, err
	}
	var photos, access []Photo
	decodeJSON(pp.row.Photos, &photos)
	decodeJSON(pp.row.AccessImages, &access)
	ref := func(p Photo) bool { return p.FileID == fileID }
	if !slices.ContainsFunc(photos, ref) && !slices.ContainsFunc(access, ref) {
		return nil, ErrNotPublished
	}
	img, err := s.readPhoto(ctx, pp.scope, fileID)
	if errors.Is(err, ErrNotPublished) || isNotFound(err) {
		return nil, ErrNotPublished
	}
	return img, err
}

// Published reports whether the marina of slug has a published profile (getPublicMarina profile_published).
func (s *Service) Published(ctx context.Context, slugValue string) bool {
	_, err := s.resolve(ctx, s.d.DB.DB, slugValue)
	return err == nil
}

// PageHead renders the head of /p/{slug} in lang (cached 60 s per marina, lang and version): title, meta
// description, canonical, hreflang alternates, Open Graph and the JSON-LD script. Every value is HTML-escaped and
// the JSON-LD is marshalled with HTML escaping, so no profile text can leave its element.
func (s *Service) PageHead(ctx context.Context, slugValue, lang string) (*Head, error) {
	var pp *published
	err := s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		pp, err = s.resolve(ctx, tx, slugValue)
		return err
	})
	if err != nil {
		return nil, err
	}
	lang, _ = langOf(pp.row, pp.marina, lang)
	key := fmt.Sprintf("%s|%s|%d|%d|%t", pp.scope.ID(), lang, pp.row.Version, pp.marina.Version, pp.marina.BookingEnabled)
	now := s.d.Clock.Now()
	s.mu.Lock()
	if h, ok := s.heads[key]; ok && now.Before(h.Expires) {
		s.mu.Unlock()
		return h, nil
	}
	s.mu.Unlock()
	var head *Head
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pub, ld, err := s.publicOf(ctx, tx, pp.scope, pp.row, pp.marina, lang)
		if err != nil {
			return err
		}
		html, err := renderHead(pub, ld, s.baseURL())
		if err != nil {
			return err
		}
		head = &Head{HTML: html, ETag: `"h` + shortHash(html) + `"`, Version: pp.row.Version, Expires: now.Add(cacheTTL)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.heads[key] = head
	s.mu.Unlock()
	return head, nil
}

func renderHead(pub *Public, ld map[string]any, base string) (string, error) {
	d := pub.Data
	script, err := jsonld.Marshal(ld)
	if err != nil {
		return "", err
	}
	esc := html.EscapeString
	page := d.PageURL
	canonical := page
	if pub.Lang != "it" {
		canonical = page + "?lang=" + pub.Lang
	}
	title := d.Name
	var b strings.Builder
	b.WriteString("<title>" + esc(title) + "</title>\n")
	if d.Description != "" {
		b.WriteString(`<meta name="description" content="` + esc(d.Description) + "\">\n")
	}
	b.WriteString(`<link rel="canonical" href="` + esc(canonical) + "\">\n")
	for _, l := range pub.Locales {
		href := page
		if l != "it" {
			href = page + "?lang=" + l
		}
		b.WriteString(`<link rel="alternate" hreflang="` + esc(l) + `" href="` + esc(href) + "\">\n")
	}
	b.WriteString(`<link rel="alternate" hreflang="x-default" href="` + esc(page) + "\">\n")
	b.WriteString(`<meta property="og:type" content="website">` + "\n")
	b.WriteString(`<meta property="og:title" content="` + esc(title) + "\">\n")
	b.WriteString(`<meta property="og:url" content="` + esc(canonical) + "\">\n")
	if d.Description != "" {
		b.WriteString(`<meta property="og:description" content="` + esc(d.Description) + "\">\n")
	}
	if len(d.Photos) > 0 {
		b.WriteString(`<meta property="og:image" content="` + esc(base+d.Photos[0].URL) + "\">\n")
	}
	b.WriteString(`<meta property="og:locale" content="` + map[string]string{"it": "it_IT", "en": "en_GB"}[pub.Lang] + "\">\n")
	b.WriteString(`<script type="application/ld+json">` + string(script) + "</script>\n")
	return b.String(), nil
}
