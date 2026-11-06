package layout

import (
	"errors"
	"slices"

	"github.com/coste360/porto/internal/geo"
)

// Footprint errors (spec 7.5, ADR 0003).
var (
	ErrUnsnapped     = errors.New("layout: placement not snapped")
	ErrOutsideArea   = errors.New("layout: placement outside the area")
	ErrBlockedCell   = errors.New("layout: placement on a blocked cell")
	ErrInvalidVessel = errors.New("layout: invalid vessel dimensions")
	ErrInvalidArea   = errors.New("layout: invalid area")
)

// Snap is the placement step of linear areas (spec 7.5: positions snap to 10 cm).
const Snap geo.Cm = 10

// Vessel is the size of a vessel for Footprint.
type Vessel struct{ LOA, Beam geo.Cm }

// Placement is where a vessel is put in an area. Linear: X is the position along the axis from From (Y and
// Rotation unused). Surface: X, Y is the top-left corner of the footprint in grid space and Rotation is 0, 900,
// 1800 or 2700 relative to the grid.
type Placement struct {
	X, Y     geo.Cm
	Rotation geo.Angle
}

// Area is the footprint view of a dynamic area (AreaOf).
type Area struct {
	Kind         AreaKind
	Length       geo.Cm // linear: To − From
	Mode         MooringMode
	Cell, Margin geo.Cm
	Cols, Rows   int    // surface grid size
	Usable       []bool // surface: cell fully inside the outline and not blocked (len = Cols*Rows)
	Blocked      []int  // surface: sorted blocked cell indexes (they give ErrBlockedCell instead of ErrOutsideArea)
}

// Cells are the cells occupied by a placement. Linear: First..Last inclusive (Indexes nil). Surface: sorted
// Indexes (First and Last are the smallest and largest).
type Cells struct {
	First, Last int
	Indexes     []int
}

// CellCount returns the number of cells of an area: linear ⌈Length/Cell⌉, surface Cols×Rows.
func CellCount(a Area) int {
	if a.Kind == AreaLinear {
		if a.Cell <= 0 || a.Length <= 0 {
			return 0
		}
		return int(ceilDiv(int64(a.Length), int64(a.Cell)))
	}
	return a.Cols * a.Rows
}

// Footprint returns the cells a vessel occupies at a placement (spec 7.5). Cells are every cell the footprint
// touches, rounding outwards, so that disjoint cells imply disjoint footprints. On a surface, the first cell (in
// index order) that is not usable decides the error: ErrBlockedCell when it is blocked, else ErrOutsideArea.
func Footprint(v Vessel, p Placement, a Area) (Cells, error) {
	if v.LOA <= 0 || v.Beam <= 0 {
		return Cells{}, ErrInvalidVessel
	}
	if a.Cell <= 0 || a.Margin < 0 {
		return Cells{}, ErrInvalidArea
	}
	c := int64(a.Cell)
	switch a.Kind {
	case AreaLinear:
		w := v.Beam + a.Margin
		if a.Mode == ModeAlongside {
			w = v.LOA + a.Margin
		}
		if p.X%Snap != 0 {
			return Cells{}, ErrUnsnapped
		}
		if p.X < a.Margin/2 || p.X+w > a.Length {
			return Cells{}, ErrOutsideArea
		}
		first := floorDiv(int64(p.X), c)
		last := ceilDiv(int64(p.X+w), c) - 1
		return Cells{First: int(first), Last: int(last)}, nil
	case AreaSurface:
		if p.Rotation%900 != 0 || p.Rotation < 0 || p.Rotation > 2700 {
			return Cells{}, ErrUnsnapped
		}
		w, h := v.LOA+a.Margin, v.Beam+a.Margin
		if p.Rotation == 900 || p.Rotation == 2700 {
			w, h = h, w
		}
		if p.X < 0 || p.Y < 0 || int64(p.X+w) > int64(a.Cols)*c || int64(p.Y+h) > int64(a.Rows)*c {
			return Cells{}, ErrOutsideArea
		}
		c0, c1 := floorDiv(int64(p.X), c), ceilDiv(int64(p.X+w), c)-1
		r0, r1 := floorDiv(int64(p.Y), c), ceilDiv(int64(p.Y+h), c)-1
		idx := make([]int, 0, (c1-c0+1)*(r1-r0+1))
		for r := r0; r <= r1; r++ {
			for col := c0; col <= c1; col++ {
				i := int(r)*a.Cols + int(col)
				if i >= len(a.Usable) || !a.Usable[i] {
					if _, blocked := slices.BinarySearch(a.Blocked, i); blocked {
						return Cells{}, ErrBlockedCell
					}
					return Cells{}, ErrOutsideArea
				}
				idx = append(idx, i)
			}
		}
		return Cells{First: idx[0], Last: idx[len(idx)-1], Indexes: idx}, nil
	}
	return Cells{}, ErrInvalidArea
}

// SurfaceGrid returns the outline in grid space (rotated by −rotation around its first vertex and translated so
// that the minimum of its bounds is the origin) and the grid origin in map space before rotation.
func SurfaceGrid(outline geo.Polygon, rotation geo.Angle) geo.Polygon {
	if len(outline) == 0 {
		return nil
	}
	center := outline[0]
	inv := geo.NewAngle(-int64(rotation))
	rot := make(geo.Polygon, len(outline))
	for i, p := range outline {
		rot[i] = geo.Rotate(p, center, inv)
	}
	b := rot.Bounds()
	for i := range rot {
		rot[i] = rot[i].Sub(b.Min)
	}
	return rot
}

// GridToMap converts a grid-space point back to map coordinates for an outline and grid rotation.
func GridToMap(outline geo.Polygon, rotation geo.Angle, p geo.Point) geo.Point {
	center := outline[0]
	inv := geo.NewAngle(-int64(rotation))
	minX, minY := geo.MaxCoord, geo.MaxCoord
	for _, q := range outline {
		r := geo.Rotate(q, center, inv)
		minX, minY = min(minX, r.X), min(minY, r.Y)
	}
	return geo.Rotate(p.Add(geo.Point{X: minX, Y: minY}), center, rotation)
}

// MapToGrid converts a map point into grid space.
func MapToGrid(outline geo.Polygon, rotation geo.Angle, p geo.Point) geo.Point {
	center := outline[0]
	inv := geo.NewAngle(-int64(rotation))
	minX, minY := geo.MaxCoord, geo.MaxCoord
	for _, q := range outline {
		r := geo.Rotate(q, center, inv)
		minX, minY = min(minX, r.X), min(minY, r.Y)
	}
	return geo.Rotate(p, center, inv).Sub(geo.Point{X: minX, Y: minY})
}

// surfaceArea computes the grid of a surface outline: size, usable cells (four corners inside, not blocked).
func surfaceArea(outline geo.Polygon, rotation geo.Angle, cell geo.Cm, blocked []int) (Area, error) {
	if cell <= 0 || rotation%900 != 0 || outline.Validate() != nil {
		return Area{}, ErrInvalidArea
	}
	g := SurfaceGrid(outline, rotation)
	b := g.Bounds()
	cols := ceilDiv(int64(b.Max.X), int64(cell))
	rows := ceilDiv(int64(b.Max.Y), int64(cell))
	if cols <= 0 || rows <= 0 || cols*rows > MaxSurfaceCells {
		return Area{}, ErrInvalidArea
	}
	n := int(cols * rows)
	bl := make([]bool, n)
	sortedBlocked := make([]int, 0, len(blocked))
	for _, i := range blocked {
		if i >= 0 && i < n && !bl[i] {
			bl[i] = true
			sortedBlocked = append(sortedBlocked, i)
		}
	}
	slices.Sort(sortedBlocked)
	// Corner (i, j) of the grid is shared by up to four cells: test each once.
	inside := make([]bool, int((cols+1)*(rows+1)))
	for j := int64(0); j <= rows; j++ {
		for i := int64(0); i <= cols; i++ {
			inside[j*(cols+1)+i] = g.Contains(geo.Point{X: geo.Cm(i) * cell, Y: geo.Cm(j) * cell})
		}
	}
	usable := make([]bool, n)
	for r := int64(0); r < rows; r++ {
		for c := int64(0); c < cols; c++ {
			k := r*(cols+1) + c
			usable[r*cols+c] = !bl[r*cols+c] && inside[k] && inside[k+1] && inside[k+cols+1] && inside[k+cols+2]
		}
	}
	return Area{Kind: AreaSurface, Cell: cell, Cols: int(cols), Rows: int(rows), Usable: usable, Blocked: sortedBlocked}, nil
}

// AreaOf returns the footprint view of a dynamic area of m. Linear: the length comes from From and To (the
// support is checked by ValidateMap). Surface: the grid comes from the outline rotated by −GridRotation around
// its first vertex, with its origin at the minimum of the rotated bounds; a cell is usable when its four
// corners are inside the outline (inclusive) and it is not blocked.
func AreaOf(d DynamicArea, _ Map) (Area, error) {
	switch d.Kind {
	case AreaLinear:
		if d.Cell <= 0 || d.Margin < 0 || d.To <= d.From {
			return Area{}, ErrInvalidArea
		}
		return Area{Kind: AreaLinear, Length: d.To - d.From, Mode: d.Mode, Cell: d.Cell, Margin: d.Margin}, nil
	case AreaSurface:
		a, err := surfaceArea(d.Outline, d.GridRotation, d.Cell, d.Blocked)
		if err != nil {
			return Area{}, err
		}
		a.Margin = d.Margin
		return a, nil
	}
	return Area{}, ErrInvalidArea
}
