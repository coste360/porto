package docs

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs/layout"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/store"
)

// Map print (M3 D25, m4-plan §15 D30): the berths of a map on a date with their codes and the silhouettes of the
// moored vessels, shaded by a lens. The PDF carries no customer names. It is rendered on request and not stored.

// Lenses of the map print.
const (
	LensOccupancy    = "occupancy"
	LensContractType = "contract_type"
	LensVesselType   = "vessel_type"
)

//nolint:misspell // Italian legend of the map print
var lensLegend = map[string]map[string][][2]string{
	"it": {
		LensOccupancy:    {{"245", "Libero"}, {"200", "Occupato"}},
		LensContractType: {{"150", "Annuale"}, {"180", "Stagionale"}, {"210", "Transito"}, {"120", "Charter / diritto d'uso"}, {"245", "Libero"}},
		LensVesselType:   {{"170", "Motore"}, {"210", "Vela"}, {"140", "Multiscafo"}, {"230", "Altro"}, {"245", "Libero"}},
	},
	"en": {
		LensOccupancy:    {{"245", "Free"}, {"200", "Occupied"}},
		LensContractType: {{"150", "Annual"}, {"180", "Seasonal"}, {"210", "Transit"}, {"120", "Charter / berth right"}, {"245", "Free"}},
		LensVesselType:   {{"170", "Motor"}, {"210", "Sail"}, {"140", "Multihull"}, {"230", "Other"}, {"245", "Free"}},
	},
}

type berthDraw struct {
	code   string
	rect   geo.Rect
	fill   int
	vessel *store.Vessel
}

func fillFor(lens string, a *store.DayAllocation, v *store.Vessel) int {
	if a == nil {
		return 245
	}
	switch lens {
	case LensContractType:
		switch {
		case a.Kind == "owner" || a.ContractKind == "charter":
			return 120
		case a.ContractTerm == "annual":
			return 150
		case a.ContractTerm == "seasonal":
			return 180
		default:
			return 210
		}
	case LensVesselType:
		if v == nil {
			return 230
		}
		switch v.HullType {
		case "monohull_motor", "rib", "gozzo":
			return 170
		case "monohull_sail":
			return 210
		case "catamaran", "trimaran":
			return 140
		default:
			return 230
		}
	}
	return 200
}

func i18nName(raw []byte, locale string) string {
	var m map[string]string
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if v := m[normLocale(locale)]; v != "" {
		return v
	}
	if v := m["it"]; v != "" {
		return v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if m[k] != "" {
			return m[k]
		}
	}
	return ""
}

// PrintMap renders the PDF of a map on a date with a lens (map.view). date "" = today in the marina.
func (s *Service) PrintMap(ctx context.Context, p *auth.Principal, marinaID, mapID, date, lens, locale string) (*Rendered, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return nil, err
	}
	switch lens {
	case "":
		lens = LensOccupancy
	case LensOccupancy, LensContractType, LensVesselType:
	default:
		return nil, validationErr("lens", "invalid")
	}
	db := s.d.DB.DB
	mp, err := store.GetMap(ctx, db, scope, mapID)
	if err != nil {
		return nil, notFound(err)
	}
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	if locale == "" {
		locale = m.DefaultLocale
	}
	now := s.now()
	day := s.today(ctx, db, scope, now)
	if date != "" {
		d, err := calendar.Parse(date)
		if err != nil {
			return nil, validationErr("date", "invalid")
		}
		day = d.String()
	}
	on := calendar.MustParse(day)
	res, err := store.LoadMapResources(ctx, db, scope, mapID, true)
	if err != nil {
		return nil, err
	}
	allocs, err := store.DayAllocations(ctx, db, scope, mapID, on, on, nil)
	if err != nil {
		return nil, err
	}
	byRes := map[string]*store.DayAllocation{}
	var vesselIDs []string
	for i := range allocs {
		a := &allocs[i]
		if a.ResourceType != "berth" {
			continue
		}
		if _, ok := byRes[a.ResourceID]; !ok {
			byRes[a.ResourceID] = a
			if a.VesselID != "" {
				vesselIDs = append(vesselIDs, a.VesselID)
			}
		}
	}
	vessels := map[string]*store.Vessel{}
	if len(vesselIDs) > 0 {
		vs, err := store.VesselsByIDs(ctx, db, scope, vesselIDs)
		if err != nil {
			return nil, err
		}
		for i := range vs {
			vessels[vs[i].ID] = &vs[i]
		}
	}
	draws := make([]berthDraw, 0, len(res.Berths))
	for _, b := range res.Berths {
		d := berthDraw{code: b.Code, rect: geo.Rect{
			Anchor: geo.Point{X: geo.Cm(b.AnchorXCm), Y: geo.Cm(b.AnchorYCm)}, Width: geo.Cm(b.WidthCm),
			Length: geo.Cm(b.LengthCm), Rotation: geo.Angle(b.RotationDd),
		}}
		a := byRes[b.ID]
		if a != nil {
			d.vessel = vessels[a.VesselID]
		}
		d.fill = fillFor(lens, a, d.vessel)
		draws = append(draws, d)
	}
	header, err := MarinaHeader(ctx, db, scope, s.d.Keys)
	if err != nil {
		return nil, err
	}
	view := views.MapPrintView{Marina: header, MapName: i18nName(mp.Name, locale), Date: on, Lens: lens}
	src, _, _ := Builtin(KindMapPrint, locale)
	loc := normLocale(locale)
	if _, ok := lensLegend[loc]; !ok {
		loc = "it"
	}
	return Render(RenderInput{
		Kind: KindMapPrint, Source: src, View: view, Locale: loc, Today: day, At: now,
		Title: Title(KindMapPrint, loc) + " " + view.MapName, Author: m.Name,
		Extra: drawMap(draws, mp.WidthCm, mp.HeightCm, lensLegend[loc][lens]),
	})
}

// mapDrawing is the drawing of a composed map print row: stored map prints carry no geometry, so nothing is added.
func (s *Service) mapDrawing(any) func(*layout.Layout) { return nil }

// drawMap adds the berths, codes, silhouettes and legend to the first page, scaled to the free area.
func drawMap(draws []berthDraw, mapW, mapH int64, legend [][2]string) func(*layout.Layout) {
	return func(l *layout.Layout) {
		if len(l.Pages) == 0 {
			l.Pages = []layout.Page{{}}
		}
		pg := &l.Pages[0]
		top := int64(0)
		for _, b := range pg.Boxes {
			if b.Y < layout.PageHeight/2 {
				top = max(top, b.Y+b.H)
			}
		}
		x0, y0 := int64(2000), top+500
		w, h := layout.PageWidth-4000, layout.PageHeight-y0-4500
		if mapW <= 0 || mapH <= 0 {
			minX, minY, maxX, maxY := int64(math.MaxInt64), int64(math.MaxInt64), int64(math.MinInt64), int64(math.MinInt64)
			for _, d := range draws {
				for _, c := range d.rect.Corners() {
					minX, minY = min(minX, int64(c.X)), min(minY, int64(c.Y))
					maxX, maxY = max(maxX, int64(c.X)), max(maxY, int64(c.Y))
				}
			}
			mapW, mapH = max(maxX, 1), max(maxY, 1)
		}
		// scale: 1/100 mm per cm of map, as a fraction num/den.
		num, den := w, mapW
		if h*mapW < w*mapH {
			num, den = h, mapH
		}
		tx := func(x, y float64) [2]int64 {
			return [2]int64{x0 + int64(roundHalfEven(x*float64(num)/float64(den))), y0 + int64(roundHalfEven(y*float64(num)/float64(den)))}
		}
		pg.Boxes = append(pg.Boxes, layout.Box{
			X: x0, Y: y0, W: mapW * num / den, H: mapH * num / den, Kind: layout.KindCell,
		})
		for _, d := range draws {
			c := d.rect.Corners()
			pts := make([][2]int64, 4)
			var cx, cy float64
			for i, p := range c {
				pts[i] = tx(float64(p.X), float64(p.Y))
				cx += float64(p.X) / 4
				cy += float64(p.Y) / 4
			}
			pg.Boxes = append(pg.Boxes, layout.Box{Kind: layout.KindPolygon, Points: pts, Fill: d.fill})
			if d.vessel != nil {
				rot := float64(d.rect.Rotation) / 10 * math.Pi / 180
				ux, uy := math.Cos(rot), math.Sin(rot)
				// Stern at the far end, bow toward the anchor (the quay), centred on the berth axis.
				far := float64(d.rect.Length)
				off := (far - float64(d.vessel.LOACm)) / 2
				for _, poly := range PathPolygons(VesselPath(d.vessel.LOACm, d.vessel.BeamCm, d.vessel.HullType)) {
					vp := make([][2]int64, len(poly))
					for i, q := range poly {
						along := far - off - q[0]
						x := float64(d.rect.Anchor.X) + ux*along - uy*q[1]
						y := float64(d.rect.Anchor.Y) + uy*along + ux*q[1]
						vp[i] = tx(x, y)
					}
					pg.Boxes = append(pg.Boxes, layout.Box{Kind: layout.KindPolygon, Points: vp, Fill: 255})
				}
			}
			size := int64(50)
			tw := layout.TextWidth(d.code, false, false, size)
			p := tx(cx, cy)
			pg.Boxes = append(pg.Boxes, layout.Box{
				X: p[0] - tw/2, Y: p[1] - 100, W: tw, H: 200, Kind: layout.KindText, Text: d.code, Style: layout.Style{Size: size},
			})
		}
		ly := layout.PageHeight - 3500
		lx := int64(2000)
		for _, e := range legend {
			fill := 0
			for _, c := range e[0] {
				fill = fill*10 + int(c-'0')
			}
			pg.Boxes = append(pg.Boxes, layout.Box{Kind: layout.KindPolygon, Fill: fill, Points: [][2]int64{
				{lx, ly}, {lx + 400, ly}, {lx + 400, ly + 400}, {lx, ly + 400},
			}})
			tw := layout.TextWidth(e[1], false, false, 80)
			pg.Boxes = append(pg.Boxes, layout.Box{
				X: lx + 550, Y: ly, W: tw, H: 400, Kind: layout.KindText, Text: e[1], Style: layout.Style{Size: 80},
			})
			lx += 550 + tw + 600
		}
	}
}
