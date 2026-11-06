// Package vectors builds the Go ↔ TypeScript parity vectors of layout.Footprint and layout.AreaOf
// (testdata/footprint/cases.json and area-cells.json, m1-plan §2.8): hand-written edge cases with hand-written
// expectations, plus seeded random cases whose expectations come from the Go implementation. The output is
// deterministic for a seed, so the committed files can be checked for drift.
package vectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand/v2"

	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
)

// File is the content of cases.json.
type File struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}

// Area is the footprint view of an area.
type Area struct {
	Kind     string `json:"kind"`
	LengthCm int64  `json:"length_cm"`
	Mode     string `json:"mode"`
	CellCm   int64  `json:"cell_cm"`
	MarginCm int64  `json:"margin_cm"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Usable   []bool `json:"usable"`
	Blocked  []int  `json:"blocked"`
}

// Vessel is the vessel of a case.
type Vessel struct {
	LOACm  int64 `json:"loa_cm"`
	BeamCm int64 `json:"beam_cm"`
}

// Placement is the placement of a case.
type Placement struct {
	XCm        int64 `json:"x_cm"`
	YCm        int64 `json:"y_cm"`
	RotationDd int64 `json:"rotation_dd"`
}

// Cells is an expected result: First/Last for linear areas, Indexes for surfaces.
type Cells struct {
	First   *int  `json:"first,omitempty"`
	Last    *int  `json:"last,omitempty"`
	Indexes []int `json:"indexes,omitempty"`
}

// Expect is either cells or an error slug.
type Expect struct {
	Cells *Cells `json:"cells,omitempty"`
	Error string `json:"error,omitempty"`
}

// Case is one parity vector.
type Case struct {
	Name      string    `json:"name"`
	Area      Area      `json:"area"`
	Vessel    Vessel    `json:"vessel"`
	Placement Placement `json:"placement"`
	Expect    Expect    `json:"expect"`
}

// ToLayout converts the case inputs.
func (c Case) ToLayout() (layout.Vessel, layout.Placement, layout.Area) {
	a := layout.Area{
		Kind: layout.AreaKind(c.Area.Kind), Length: geo.Cm(c.Area.LengthCm), Mode: layout.MooringMode(c.Area.Mode),
		Cell: geo.Cm(c.Area.CellCm), Margin: geo.Cm(c.Area.MarginCm), Cols: c.Area.Cols, Rows: c.Area.Rows,
		Usable: c.Area.Usable, Blocked: c.Area.Blocked,
	}
	return layout.Vessel{LOA: geo.Cm(c.Vessel.LOACm), Beam: geo.Cm(c.Vessel.BeamCm)},
		layout.Placement{X: geo.Cm(c.Placement.XCm), Y: geo.Cm(c.Placement.YCm), Rotation: geo.Angle(c.Placement.RotationDd)}, a
}

// ErrorSlug returns the slug of a Footprint error.
func ErrorSlug(err error) string {
	switch {
	case errors.Is(err, layout.ErrUnsnapped):
		return "unsnapped"
	case errors.Is(err, layout.ErrOutsideArea):
		return "outside_area"
	case errors.Is(err, layout.ErrBlockedCell):
		return "blocked_cell"
	case errors.Is(err, layout.ErrInvalidVessel):
		return "invalid_vessel"
	case err != nil:
		return "invalid_area"
	}
	return ""
}

// Run computes the result of a case with the Go implementation.
func Run(c Case) Expect {
	v, p, a := c.ToLayout()
	cells, err := layout.Footprint(v, p, a)
	if err != nil {
		return Expect{Error: ErrorSlug(err)}
	}
	if a.Kind == layout.AreaLinear {
		f, l := cells.First, cells.Last
		return Expect{Cells: &Cells{First: &f, Last: &l}}
	}
	return Expect{Cells: &Cells{Indexes: cells.Indexes}}
}

func lin(name, mode string, length, cell, margin, loa, beam, x int64, e Expect) Case {
	return Case{
		Name: name, Area: Area{Kind: "linear", LengthCm: length, Mode: mode, CellCm: cell, MarginCm: margin, Usable: []bool{}, Blocked: []int{}},
		Vessel: Vessel{loa, beam}, Placement: Placement{XCm: x}, Expect: e,
	}
}

func span(f, l int) Expect { return Expect{Cells: &Cells{First: &f, Last: &l}} }

func idx(i ...int) Expect { return Expect{Cells: &Cells{Indexes: i}} }

func fail(slug string) Expect { return Expect{Error: slug} }

// grid4x3 is a 4×3 surface of 100 cm cells; unusable lists unusable cells, blocked the blocked ones among them.
func grid4x3(name string, margin int64, unusable, blocked []int, loa, beam, x, y, rot int64, e Expect) Case {
	u := make([]bool, 12)
	for i := range u {
		u[i] = true
	}
	for _, i := range unusable {
		u[i] = false
	}
	if blocked == nil {
		blocked = []int{}
	}
	return Case{
		Name: name, Area: Area{Kind: "surface", CellCm: 100, MarginCm: margin, Cols: 4, Rows: 3, Usable: u, Blocked: blocked},
		Vessel: Vessel{loa, beam}, Placement: Placement{XCm: x, YCm: y, RotationDd: rot}, Expect: e,
	}
}

// Handwritten returns the hand-written edge cases with their expected results.
func Handwritten() []Case {
	st, al := "stern_to", "alongside"
	return []Case{
		lin("linear: x below half the margin", st, 1000, 50, 50, 800, 300, 20, fail("outside_area")),
		lin("linear: first placement after half margin", st, 1000, 50, 50, 800, 300, 30, span(0, 7)),
		lin("linear: start on a cell boundary", st, 1000, 50, 50, 800, 250, 50, span(1, 6)),
		lin("linear: last cell, exact end", st, 1000, 50, 50, 800, 300, 650, span(13, 19)),
		lin("linear: one snap beyond the end", st, 1000, 50, 50, 800, 300, 660, fail("outside_area")),
		lin("linear: unsnapped position", st, 1000, 50, 50, 800, 300, 35, fail("unsnapped")),
		lin("linear: zero beam", st, 1000, 50, 50, 800, 0, 30, fail("invalid_vessel")),
		lin("linear: negative loa", st, 1000, 50, 50, -1, 300, 30, fail("invalid_vessel")),
		lin("linear: alongside uses loa", al, 2000, 50, 100, 1200, 400, 50, span(1, 26)),
		lin("linear: odd margin, half margin rounds down", st, 1000, 50, 55, 800, 300, 20, fail("outside_area")),
		lin("linear: odd margin placement", st, 1000, 50, 55, 800, 300, 30, span(0, 7)),
		lin("linear: zero margin at the origin", st, 1000, 50, 0, 300, 100, 0, span(0, 1)),
		lin("linear: footprint filling one cell exactly", st, 1000, 100, 1, 500, 99, 100, span(1, 1)),
		lin("linear: one cm into the next cell", st, 1000, 100, 1, 500, 100, 100, span(1, 2)),
		lin("linear: the whole length", st, 1000, 100, 0, 500, 1000, 0, span(0, 9)),
		lin("linear: negative position", st, 1000, 50, 50, 800, 300, -10, fail("outside_area")),
		lin("linear: alongside in the last cell", al, 1000, 50, 0, 10, 300, 990, span(19, 19)),
		lin("linear: length not a multiple of the cell", st, 1020, 50, 0, 300, 20, 1000, span(20, 20)),
		lin("linear: small cells", st, 1000, 10, 10, 300, 5, 10, span(1, 2)),
		lin("linear: stern_to ignores loa", st, 1000, 50, 50, 5000, 300, 30, span(0, 7)),
		lin("linear: alongside ignores beam", al, 1000, 50, 50, 300, 5000, 30, span(0, 7)),
		grid4x3("surface: rotation 0 at the origin", 0, nil, nil, 200, 100, 0, 0, 0, idx(0, 1)),
		grid4x3("surface: rotation 900 swaps the axes", 0, nil, nil, 200, 100, 0, 0, 900, idx(0, 4)),
		grid4x3("surface: rotation 1800", 0, nil, nil, 200, 100, 100, 100, 1800, idx(5, 6)),
		grid4x3("surface: rotation 2700 on the last column", 0, nil, nil, 200, 100, 300, 0, 2700, idx(3, 7)),
		grid4x3("surface: partial cells count", 0, nil, nil, 100, 100, 50, 50, 0, idx(0, 1, 4, 5)),
		grid4x3("surface: exact fit in the corner", 0, nil, nil, 200, 200, 200, 100, 0, idx(6, 7, 10, 11)),
		grid4x3("surface: beyond the right edge", 0, nil, nil, 200, 100, 300, 0, 0, fail("outside_area")),
		grid4x3("surface: beyond the bottom edge", 0, nil, nil, 100, 100, 0, 250, 0, fail("outside_area")),
		grid4x3("surface: negative x", 0, nil, nil, 100, 100, -10, 0, 0, fail("outside_area")),
		grid4x3("surface: free rotation refused", 0, nil, nil, 100, 100, 0, 0, 450, fail("unsnapped")),
		grid4x3("surface: blocked cell", 0, []int{5}, []int{5}, 100, 100, 100, 100, 0, fail("blocked_cell")),
		grid4x3("surface: cell outside the outline", 0, []int{0}, nil, 100, 100, 0, 0, 0, fail("outside_area")),
		grid4x3("surface: first bad cell is outside the outline", 0, []int{5, 6}, []int{6}, 200, 100, 100, 100, 0, fail("outside_area")),
		grid4x3("surface: first bad cell is blocked", 0, []int{5, 6}, []int{5}, 200, 100, 100, 100, 0, fail("blocked_cell")),
		grid4x3("surface: margin on both axes", 50, nil, nil, 150, 50, 0, 0, 0, idx(0, 1)),
		grid4x3("surface: margin with rotation 900", 50, nil, nil, 150, 50, 0, 0, 900, idx(0, 4)),
		grid4x3("surface: zero beam", 0, nil, nil, 100, 0, 0, 0, 0, fail("invalid_vessel")),
		grid4x3("surface: the whole grid", 0, nil, nil, 400, 300, 0, 0, 0, idx(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11)),
		grid4x3("surface: inside one cell", 0, nil, nil, 80, 80, 10, 10, 0, idx(0)),
		grid4x3("surface: touching the far corner", 0, nil, nil, 100, 100, 300, 200, 0, idx(11)),
		grid4x3("surface: one cm over a boundary", 0, nil, nil, 101, 100, 0, 0, 0, idx(0, 1)),
	}
}

// Random returns n seeded random cases, with expectations from the Go implementation.
func Random(seed uint64, n int) []Case {
	r := rand.New(rand.NewPCG(seed, 0x706f72746f)) //nolint:gosec // deterministic test vectors
	pick := func(vals ...int64) int64 { return vals[r.IntN(len(vals))] }
	between := func(lo, hi int64) int64 { return lo + r.Int64N(hi-lo+1) }
	out := make([]Case, 0, n)
	for i := range n {
		var c Case
		if r.IntN(2) == 0 {
			mode := "stern_to"
			if r.IntN(2) == 1 {
				mode = "alongside"
			}
			length := between(300, 6000)
			x := between(-2, length/20) * 10
			if r.IntN(10) == 0 {
				x += 5
			}
			c = lin("", mode, length, pick(10, 25, 50, 100), between(0, 150), between(0, 2000), between(0, 700), x, Expect{})
		} else {
			cols, rows := int(between(1, 10)), int(between(1, 10))
			cell := pick(50, 100, 120)
			u := make([]bool, cols*rows)
			blocked := []int{}
			for k := range u {
				switch r.IntN(20) {
				case 0:
					blocked = append(blocked, k)
				case 1:
				default:
					u[k] = true
				}
			}
			c = Case{
				Area:   Area{Kind: "surface", CellCm: cell, MarginCm: between(0, 80), Cols: cols, Rows: rows, Usable: u, Blocked: blocked},
				Vessel: Vessel{between(0, 3*cell), between(0, 2*cell)},
				Placement: Placement{
					XCm: between(-cell/4, int64(cols)*cell/2), YCm: between(-cell/4, int64(rows)*cell/2),
					RotationDd: pick(0, 900, 1800, 2700, 0, 900, 450),
				},
			}
		}
		c.Name = "random " + itoa(i+1)
		c.Expect = Run(c)
		out = append(out, c)
	}
	return out
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// Encode writes a vectors file: one case per line, deterministic.
func Encode(cases []Case) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{\"version\": 1, \"cases\": [\n")
	for i, c := range cases {
		b, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		buf.Write(b)
		if i < len(cases)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("]}\n")
	return buf.Bytes(), nil
}

// Cases returns the complete content of cases.json for a seed and a number of random cases.
func Cases(seed uint64, n int) []Case { return append(Handwritten(), Random(seed, n)...) }

// AreaCase is one AreaOf / CellCount vector (area-cells.json).
type AreaCase struct {
	Name           string      `json:"name"`
	Kind           string      `json:"kind"`
	Outline        []geo.Point `json:"outline,omitempty"`
	GridRotationDd int64       `json:"grid_rotation_dd"`
	CellCm         int64       `json:"cell_cm"`
	Blocked        []int       `json:"blocked,omitempty"`
	FromCm         int64       `json:"from_cm,omitempty"`
	ToCm           int64       `json:"to_cm,omitempty"`
	Expect         AreaExpect  `json:"expect"`
}

// AreaExpect is the expected grid.
type AreaExpect struct {
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Usable    []bool `json:"usable,omitempty"`
	CellCount int    `json:"cell_count"`
}

func pts(xy ...int64) []geo.Point {
	out := make([]geo.Point, 0, len(xy)/2)
	for i := 0; i+1 < len(xy); i += 2 {
		out = append(out, geo.Point{X: geo.Cm(xy[i]), Y: geo.Cm(xy[i+1])})
	}
	return out
}

func trues(n int) []bool {
	out := make([]bool, n)
	for i := range out {
		out[i] = true
	}
	return out
}

// AreaCases returns the hand-written AreaOf vectors.
func AreaCases() []AreaCase {
	T, F := true, false
	blockedRect := trues(12)
	blockedRect[0], blockedRect[11] = false, false
	return []AreaCase{
		{
			Name: "rectangle 4×3", Kind: "surface", Outline: pts(1000, 2000, 1400, 2000, 1400, 2300, 1000, 2300), CellCm: 100,
			Expect: AreaExpect{Cols: 4, Rows: 3, Usable: trues(12), CellCount: 12},
		},
		{
			Name: "rectangle with blocked cells", Kind: "surface", Outline: pts(1000, 2000, 1400, 2000, 1400, 2300, 1000, 2300), CellCm: 100,
			Blocked: []int{0, 11}, Expect: AreaExpect{Cols: 4, Rows: 3, Usable: blockedRect, CellCount: 12},
		},
		{
			Name: "partial last column", Kind: "surface", Outline: pts(0, 0, 450, 0, 450, 300, 0, 300), CellCm: 100,
			Expect: AreaExpect{Cols: 5, Rows: 3, Usable: []bool{T, T, T, T, F, T, T, T, T, F, T, T, T, T, F}, CellCount: 15},
		},
		{
			Name: "L shape", Kind: "surface", Outline: pts(0, 0, 300, 0, 300, 100, 100, 100, 100, 200, 0, 200), CellCm: 100,
			Expect: AreaExpect{Cols: 3, Rows: 2, Usable: []bool{T, T, T, T, F, F}, CellCount: 6},
		},
		{
			Name: "grid rotated by 900", Kind: "surface", Outline: pts(0, 0, 300, 0, 300, 400, 0, 400), GridRotationDd: 900, CellCm: 100,
			Expect: AreaExpect{Cols: 4, Rows: 3, Usable: trues(12), CellCount: 12},
		},
		{
			Name: "grid rotated by 1800", Kind: "surface", Outline: pts(500, 500, 700, 500, 700, 600, 500, 600), GridRotationDd: 1800, CellCm: 100,
			Expect: AreaExpect{Cols: 2, Rows: 1, Usable: []bool{T, T}, CellCount: 2},
		},
		{
			Name: "triangle", Kind: "surface", Outline: pts(0, 0, 200, 0, 0, 200), CellCm: 100,
			Expect: AreaExpect{Cols: 2, Rows: 2, Usable: []bool{T, F, F, F}, CellCount: 4},
		},
		{Name: "linear 1050 cm in 50 cm cells", Kind: "linear", FromCm: 100, ToCm: 1150, CellCm: 50, Expect: AreaExpect{CellCount: 21}},
		{Name: "linear with a partial last cell", Kind: "linear", FromCm: 0, ToCm: 1000, CellCm: 300, Expect: AreaExpect{CellCount: 4}},
	}
}

// RunArea computes an AreaOf vector with the Go implementation.
func RunArea(c AreaCase) (AreaExpect, error) {
	d := layout.DynamicArea{
		Kind: layout.AreaKind(c.Kind), Outline: c.Outline, GridRotation: geo.Angle(c.GridRotationDd), Cell: geo.Cm(c.CellCm),
		Blocked: c.Blocked, From: geo.Cm(c.FromCm), To: geo.Cm(c.ToCm), Mode: layout.ModeSternTo,
	}
	a, err := layout.AreaOf(d, layout.Map{})
	if err != nil {
		return AreaExpect{}, err
	}
	out := AreaExpect{CellCount: layout.CellCount(a)}
	if a.Kind == layout.AreaSurface {
		out.Cols, out.Rows, out.Usable = a.Cols, a.Rows, a.Usable
	}
	return out, nil
}

// EncodeAreas writes area-cells.json.
func EncodeAreas(cases []AreaCase) ([]byte, error) {
	b, err := json.MarshalIndent(struct {
		Version int        `json:"version"`
		Cases   []AreaCase `json:"cases"`
	}{1, cases}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
