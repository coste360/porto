package harbor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// ExportFormat is the version tag of the JSON map export (spec 8.7).
const ExportFormat = "porto-map/1"

// ExportMember is a group member by element code.
type ExportMember struct {
	ResourceType string `json:"resource_type"`
	Code         string `json:"code"`
}

// ExportGroup is a group with its members on the exported map.
type ExportGroup struct {
	Code    string         `json:"code"`
	Name    I18nText       `json:"name"`
	Kind    string         `json:"kind"`
	Members []ExportMember `json:"members"`
}

// Export is the versioned JSON export of a map (MapExport). Ids are kept as provenance.
type Export struct {
	Format     string        `json:"format"`
	ExportedAt time.Time     `json:"exported_at"`
	Map        SaveRequest   `json:"map"`
	Groups     []ExportGroup `json:"groups"`
}

// ExportMap returns the JSON export of a map (harbor.configure).
func (s *Service) ExportMap(ctx context.Context, p *auth.Principal, marinaID, mapID string) (*Export, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	var out *Export
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.GetMap(ctx, tx, scope, mapID)
		if err != nil {
			return err
		}
		d, err := loadDocument(ctx, tx, scope, m)
		if err != nil {
			return err
		}
		gs, err := store.ListBerthGroups(ctx, tx, scope)
		if err != nil {
			return err
		}
		codes := map[string]ExportMember{}
		for _, b := range d.Berths {
			codes[b.ID] = ExportMember{resourceBerth, b.Code}
		}
		for _, a := range d.DynamicAreas {
			codes[a.ID] = ExportMember{resourceArea, a.Code}
		}
		for _, l := range d.LandResources {
			codes[l.ID] = ExportMember{resourceLand, l.Code}
		}
		members, err := store.GroupMembersOfMap(ctx, tx, scope, m.ID)
		if err != nil {
			return err
		}
		byGroup := map[string][]ExportMember{}
		for _, mb := range members {
			if c, ok := codes[mb.ResourceID]; ok {
				byGroup[mb.GroupID] = append(byGroup[mb.GroupID], c)
			}
		}
		out = &Export{Format: ExportFormat, ExportedAt: s.now(), Map: d.Request(), Groups: []ExportGroup{}}
		for _, g := range gs {
			list := byGroup[g.ID]
			if len(list) == 0 {
				continue
			}
			slices.SortFunc(list, func(a, b ExportMember) int {
				if a.ResourceType != b.ResourceType {
					return compare(a.ResourceType, b.ResourceType)
				}
				return compare(a.Code, b.Code)
			})
			out.Groups = append(out.Groups, ExportGroup{Code: g.Code, Name: unmarshal[I18nText](g.Name), Kind: g.Kind, Members: list})
		}
		return nil
	})
	return out, err
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// ImportMap creates a new map from a JSON export (harbor.configure). Every id becomes a key of a new element;
// groups are matched by code and created when missing; a background file of another marina is dropped. The map
// is validated like a save: an invalid export creates nothing.
func (s *Service) ImportMap(ctx context.Context, p *auth.Principal, marinaID string, in Export) (*Summary, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	if in.Format != ExportFormat {
		return nil, fieldError("format", "invalid")
	}
	req := cloneRequest(in.Map)
	rekey := func(id, key *string) {
		if *id != "" {
			*key, *id = *id, ""
		}
	}
	for i := range req.Structures {
		rekey(&req.Structures[i].ID, &req.Structures[i].Key)
	}
	for i := range req.Pedestals {
		rekey(&req.Pedestals[i].ID, &req.Pedestals[i].Key)
	}
	for i := range req.Berths {
		rekey(&req.Berths[i].ID, &req.Berths[i].Key)
		req.Berths[i].GroupIDs = []string{}
	}
	for i := range req.DynamicAreas {
		rekey(&req.DynamicAreas[i].ID, &req.DynamicAreas[i].Key)
		req.DynamicAreas[i].GroupIDs = []string{}
		req.DynamicAreas[i].CellCount = nil
	}
	for i := range req.LandResources {
		rekey(&req.LandResources[i].ID, &req.LandResources[i].Key)
		req.LandResources[i].GroupIDs = []string{}
	}
	for i := range req.Decorations {
		rekey(&req.Decorations[i].ID, &req.Decorations[i].Key)
	}
	var out *Summary
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if b := req.Background; b != nil {
			if f, err := store.GetStoredFile(ctx, tx, scope, b.FileID); err != nil || f.Purpose != purposeBackground {
				req.Background = nil
			}
		}
		if err := s.importGroups(ctx, tx, scope, &req, in.Groups); err != nil {
			return err
		}
		c := checker{&auth.ValidationError{}}
		checkCreate := MapCreate{Kind: req.Kind, Name: req.Name, Icon: req.Icon, WidthCm: req.WidthCm, HeightCm: req.HeightCm, GridCm: req.GridCm}
		if req.Icon == "" {
			checkCreate.Icon = "map"
			req.Icon = "map"
		}
		checkMeta(c, &Meta{Kind: req.Kind, Name: req.Name, Icon: req.Icon, WidthCm: req.WidthCm, HeightCm: req.HeightCm, GridCm: req.GridCm})
		if err := c.v.Err(); err != nil {
			return err
		}
		m, err := s.newMapRow(ctx, tx, scope, checkCreate)
		if err != nil {
			return err
		}
		if _, err := s.applyDocument(ctx, tx, p, scope, m, m.Version, &req, "map.import", event.ChangeImported); err != nil {
			return err
		}
		counts, err := store.CountMapElements(ctx, tx, scope, m.ID)
		if err != nil {
			return err
		}
		sum := summaryOf(m, counts[m.ID])
		out = &sum
		return nil
	})
	return out, err
}

// importGroups resolves the exported groups by code (creating the missing ones) and sets the group ids of their
// members in the document.
func (s *Service) importGroups(ctx context.Context, tx bun.IDB, scope store.MarinaScope, req *SaveRequest, groups []ExportGroup) error {
	if len(groups) == 0 {
		return nil
	}
	existingGroups, err := store.ListBerthGroups(ctx, tx, scope)
	if err != nil {
		return err
	}
	byCode := map[string]string{}
	for _, g := range existingGroups {
		byCode[g.Code] = g.ID
	}
	index := map[ExportMember]*[]string{}
	for i := range req.Berths {
		index[ExportMember{resourceBerth, req.Berths[i].Code}] = &req.Berths[i].GroupIDs
	}
	for i := range req.DynamicAreas {
		index[ExportMember{resourceArea, req.DynamicAreas[i].Code}] = &req.DynamicAreas[i].GroupIDs
	}
	for i := range req.LandResources {
		index[ExportMember{resourceLand, req.LandResources[i].Code}] = &req.LandResources[i].GroupIDs
	}
	for gi, g := range groups {
		id, ok := byCode[g.Code]
		if !ok {
			c := checker{&auth.ValidationError{}}
			kind := g.Kind
			if kind == "" {
				kind = "custom"
			}
			c.code("groups.code", g.Code)
			c.i18n("groups.name", g.Name, true)
			c.enum("groups.kind", kind, groupKinds)
			if err := c.v.Err(); err != nil {
				return fieldError("groups["+itoa(gi)+"]", "invalid")
			}
			ng, err := s.insertGroup(ctx, tx, scope, GroupInput{Code: g.Code, Name: g.Name, Kind: kind})
			if err != nil {
				return err
			}
			id = ng.ID
			byCode[g.Code] = id
		}
		for _, mb := range g.Members {
			if dst, ok := index[mb]; ok && !slices.Contains(*dst, id) {
				*dst = append(*dst, id)
			}
		}
	}
	return nil
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// ---- GeoJSON ----

// ExportGeoJSON returns the map as a GeoJSON FeatureCollection in WGS84 (harbor.configure). Numbers are written
// as exact decimals from integers (layout.FormatE7). ErrNotGeoreferenced without a georeference.
func (s *Service) ExportGeoJSON(ctx context.Context, p *auth.Principal, marinaID, mapID string) ([]byte, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	var d *Document
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		m, err := store.GetMap(ctx, tx, scope, mapID)
		if err != nil {
			return err
		}
		d, err = loadDocument(ctx, tx, scope, m)
		return err
	})
	if err != nil {
		return nil, err
	}
	if d.Georef == nil {
		return nil, ErrNotGeoreferenced
	}
	g, err := georefOf(d.Georef)
	if err != nil {
		return nil, ErrNotGeoreferenced
	}
	return geoJSONOf(d, g)
}

type feature struct {
	Type       string         `json:"type"`
	Geometry   any            `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type geometry struct {
	Type        string `json:"type"`
	Coordinates any    `json:"coordinates"`
}

func geoJSONOf(d *Document, g layout.Georef) ([]byte, error) {
	pos := func(p geo.Point) []json.Number {
		lat, lon := g.ToWGS84(p)
		return []json.Number{json.Number(layout.FormatE7(lon)), json.Number(layout.FormatE7(lat))}
	}
	ring := func(pts []geo.Point) [][][]json.Number {
		r := make([][]json.Number, 0, len(pts)+1)
		for _, p := range pts {
			r = append(r, pos(p))
		}
		if len(pts) > 0 {
			r = append(r, pos(pts[0]))
		}
		return [][][]json.Number{r}
	}
	line := func(pts []geo.Point) [][]json.Number {
		r := make([][]json.Number, 0, len(pts))
		for _, p := range pts {
			r = append(r, pos(p))
		}
		return r
	}
	var fs []feature
	add := func(gt string, coords any, props map[string]any) {
		fs = append(fs, feature{Type: "Feature", Geometry: geometry{Type: gt, Coordinates: coords}, Properties: props})
	}
	if len(d.ConcessionOutline) > 0 {
		add("Polygon", ring(d.ConcessionOutline), map[string]any{"element_type": "concession"})
	}
	for _, w := range d.WaterPolygons {
		add("Polygon", ring(w), map[string]any{"element_type": "water"})
	}
	for _, l := range d.LandPolygons {
		add("Polygon", ring(l), map[string]any{"element_type": "land"})
	}
	structs := map[string]layout.Structure{}
	for _, s := range d.Structures {
		props := map[string]any{"element_type": layout.KindStructure, "id": s.ID, "code": s.Code, "kind": s.Kind}
		switch {
		case s.Rect != nil:
			r := geo.Rect{Anchor: s.Rect.Anchor, Width: geo.Cm(s.Rect.WidthCm), Length: geo.Cm(s.Rect.LengthCm), Rotation: geo.Angle(s.Rect.RotationDd)}
			c := r.Corners()
			add("Polygon", ring(c[:]), props)
		case s.Closed:
			add("Polygon", ring(s.Points), props)
		default:
			if s.WidthCm > 0 {
				props["width_cm"] = s.WidthCm
			}
			add("LineString", line(s.Points), props)
		}
		ls := layout.Structure{Ref: layout.Ref(s.ID), Kind: layout.StructureKind(s.Kind), Points: s.Points, Closed: s.Closed, Width: geo.Cm(s.WidthCm)}
		for _, side := range s.Sides {
			ls.Sides = append(ls.Sides, layout.Side(side))
		}
		structs[s.ID] = ls
	}
	for _, b := range d.Berths {
		props := map[string]any{"element_type": layout.KindBerth, "id": b.ID, "code": b.Code, "mooring_type": b.MooringType}
		if b.PriceClass != "" {
			props["price_class"] = b.PriceClass
		}
		if b.MooringType == string(layout.MooringBuoy) {
			props["swing_radius_cm"] = b.SwingRadiusCm
			add("Point", pos(b.Anchor), props)
			continue
		}
		r := geo.Rect{Anchor: b.Anchor, Width: geo.Cm(b.WidthCm), Length: geo.Cm(b.LengthCm), Rotation: geo.Angle(b.RotationDd)}
		c := r.Corners()
		add("Polygon", ring(c[:]), props)
	}
	for _, a := range d.DynamicAreas {
		props := map[string]any{"element_type": layout.KindDynamicArea, "id": a.ID, "code": a.Code, "kind": a.Kind}
		if a.Kind == string(layout.AreaSurface) {
			add("Polygon", ring(a.Outline), props)
			continue
		}
		la := layout.DynamicArea{
			Kind: layout.AreaLinear, Side: layout.Side(a.Side), From: cm(a.FromCm), To: cm(a.ToCm),
			Mode: layout.MooringMode(a.MooringMode), Margin: cm(a.MarginCm), MaxLOA: geo.Cm(a.MaxLOACm), MaxBeam: geo.Cm(a.MaxBeamCm),
		}
		if r, ok := layout.LinearBand(la, structs[a.StructureID]); ok {
			c := r.Corners()
			add("Polygon", ring(c[:]), props)
		}
	}
	for _, l := range d.LandResources {
		r := geo.Rect{Anchor: l.Anchor, Width: geo.Cm(l.WidthCm), Length: geo.Cm(l.LengthCm), Rotation: geo.Angle(l.RotationDd)}
		c := r.Corners()
		add("Polygon", ring(c[:]), map[string]any{"element_type": layout.KindLandResource, "id": l.ID, "code": l.Code, "kind": l.Kind})
	}
	for _, p := range d.Pedestals {
		add("Point", pos(p.Position), map[string]any{"element_type": layout.KindPedestal, "id": p.ID, "code": p.Code})
	}
	for _, dec := range d.Decorations {
		props := map[string]any{"element_type": layout.KindDecoration, "id": dec.ID, "kind": dec.Kind}
		if dec.Asset != "" {
			props["asset"] = dec.Asset
		}
		switch {
		case dec.Position != nil:
			add("Point", pos(*dec.Position), props)
		case len(dec.Points) > 1:
			add("LineString", line(dec.Points), props)
		}
	}
	if fs == nil {
		fs = []feature{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	err := enc.Encode(map[string]any{
		"type": "FeatureCollection", "features": fs,
		"properties": map[string]any{"map_id": d.ID, "version": d.Version},
	})
	return buf.Bytes(), err
}

// ConvertConcession converts a GeoJSON polygon in WGS84 into a map outline in cm, with the given or the map's
// georeference (harbor.configure). Nothing is written. Numbers are parsed as exact decimals.
func (s *Service) ConvertConcession(ctx context.Context, p *auth.Principal, marinaID, mapID string, raw json.RawMessage, georef *Georef) ([]geo.Point, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	m, err := store.GetMap(ctx, s.d.DB.DB, scope, mapID)
	if err != nil {
		return nil, err
	}
	if georef == nil {
		georef = metaOf(m).Georef
	} else {
		c := checker{&auth.ValidationError{}}
		checkMeta(c, &Meta{Kind: "water", Name: I18nText{It: "x"}, Icon: "map", WidthCm: 100, HeightCm: 100, GridCm: 1, Georef: georef})
		if err := c.v.Err(); err != nil {
			return nil, err
		}
	}
	if georef == nil {
		return nil, ErrNotGeoreferenced
	}
	g, err := georefOf(georef)
	if err != nil {
		return nil, ErrNotGeoreferenced
	}
	ringPts, err := exteriorRing(raw)
	if err != nil {
		return nil, fieldError("geojson", "invalid")
	}
	out := make([]geo.Point, 0, len(ringPts))
	for _, pos := range ringPts {
		if len(pos) < 2 {
			return nil, fieldError("geojson", "invalid")
		}
		lon, err1 := layout.ParseE7(pos[0].String())
		lat, err2 := layout.ParseE7(pos[1].String())
		if err1 != nil || err2 != nil || lat < -900_000_000 || lat > 900_000_000 {
			return nil, fieldError("geojson", "invalid_coordinate")
		}
		out = append(out, g.FromWGS84(lat, lon))
	}
	if len(out) > 1 && out[0] == out[len(out)-1] {
		out = out[:len(out)-1]
	}
	if geo.Polygon(out).Validate() != nil {
		return nil, fieldError("geojson", "invalid_polygon")
	}
	return out, nil
}

var errNoPolygon = errors.New("harbor: no polygon in GeoJSON")

// exteriorRing returns the exterior ring of the first polygon of a GeoJSON object (Polygon, MultiPolygon,
// Feature or FeatureCollection).
func exteriorRing(raw json.RawMessage) ([][]json.Number, error) {
	var obj struct {
		Type        string            `json:"type"`
		Coordinates json.RawMessage   `json:"coordinates"`
		Geometry    json.RawMessage   `json:"geometry"`
		Features    []json.RawMessage `json:"features"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	decodeNumbers := func(b json.RawMessage, v any) error {
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		return d.Decode(v)
	}
	switch obj.Type {
	case "Polygon":
		var rings [][][]json.Number
		if err := decodeNumbers(obj.Coordinates, &rings); err != nil || len(rings) == 0 {
			return nil, errNoPolygon
		}
		return rings[0], nil
	case "MultiPolygon":
		var polys [][][][]json.Number
		if err := decodeNumbers(obj.Coordinates, &polys); err != nil || len(polys) == 0 || len(polys[0]) == 0 {
			return nil, errNoPolygon
		}
		return polys[0][0], nil
	case "Feature":
		return exteriorRing(obj.Geometry)
	case "FeatureCollection":
		for _, f := range obj.Features {
			if r, err := exteriorRing(f); err == nil {
				return r, nil
			}
		}
	}
	return nil, errNoPolygon
}
