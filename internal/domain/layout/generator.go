package layout

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/coste360/porto/internal/geo"
)

// Generators (spec 8.3). They are pure and deterministic, produce elements identified by client keys
// (KeyPrefix + "s1", "b1", "f1", ...), never overlap among themselves and fail only on bad parameters
// (ErrBadParams wrapping a *ValidationError).

// Generated is the output of a generator: new elements, not saved.
type Generated struct {
	Structures    []Structure
	Berths        []Berth
	Areas         []DynamicArea
	LandResources []LandResource
	Pedestals     []Pedestal
	Decorations   []Decoration
	Warnings      []Issue
}

// Count returns the number of generated elements.
func (g *Generated) Count() int {
	return len(g.Structures) + len(g.Berths) + len(g.Areas) + len(g.LandResources) + len(g.Pedestals) + len(g.Decorations)
}

// Numbering schemes.
const (
	NumberContinuous   = "continuous"
	NumberOddLeftEven  = "odd_left_even_right"
	NumberPerSide      = "per_side"
	maxCodeLen         = 32
	maxKeyPrefixLength = 48
)

// Numbering is how generated elements are coded.
type Numbering struct {
	Scheme string // continuous | odd_left_even_right | per_side
	Prefix string // e.g. "B-"; per_side adds L/R/H after the prefix
	Start  int64  // default 1
	Pad    int    // minimum number of digits, 0..3
}

func (n Numbering) start() int64 {
	if n.Start <= 0 {
		return 1
	}
	return n.Start
}

// code formats number k (side letter only for per_side).
func (n Numbering) code(k int64, side Side) string {
	s := strconv.FormatInt(k, 10)
	if len(s) < n.Pad {
		s = strings.Repeat("0", n.Pad-len(s)) + s
	}
	letter := ""
	if n.Scheme == NumberPerSide {
		letter = map[Side]string{SideLeft: "L", SideRight: "R", SideHead: "H"}[side]
	}
	return n.Prefix + letter + s
}

var keyPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_:-]*$`)

// ValidCode reports whether s is a valid element code: 1..32 characters, no control characters, no leading or
// trailing space.
func ValidCode(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > maxCodeLen || strings.TrimSpace(s) != s || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func checkCommon(v *ValidationError, keyPrefix string, n *Numbering) {
	if len(keyPrefix) > maxKeyPrefixLength || !keyPrefixPattern.MatchString(keyPrefix) {
		v.Add("key_prefix", "invalid")
	}
	if n == nil {
		return
	}
	switch n.Scheme {
	case NumberContinuous, NumberOddLeftEven, NumberPerSide:
	case "":
		n.Scheme = NumberContinuous
	default:
		v.Add("numbering.scheme", "invalid")
	}
	if n.Pad < 0 || n.Pad > 3 {
		v.Add("numbering.pad", "invalid")
	}
	if n.Start < 0 || n.Start > 1_000_000 {
		v.Add("numbering.start", "invalid")
	}
	if utf8.RuneCountInString(n.Prefix) > 16 || strings.ContainsFunc(n.Prefix, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		v.Add("numbering.prefix", "invalid")
	}
}

func positive(v *ValidationError, field string, x geo.Cm) {
	if x <= 0 || x > MaxMapSide {
		v.Add(field, "invalid")
	}
}

// rectSide checks a generated berth or land resource side: at least MinRectSide, like a drawn one.
func rectSide(v *ValidationError, field string, x geo.Cm) {
	if x < MinRectSide || x > MaxMapSide {
		v.Add(field, "invalid")
	}
}

// rectSideOrZero is rectSide where 0 means "not set".
func rectSideOrZero(v *ValidationError, field string, x geo.Cm) {
	if x != 0 {
		rectSide(v, field, x)
	}
}

func nonNegative(v *ValidationError, field string, x geo.Cm) {
	if x < 0 || x > MaxMapSide {
		v.Add(field, "invalid")
	}
}

// gapFor is the space left between generated neighbours: zero on axis-aligned rotations (exact), 2 cm otherwise,
// so that the rounding of integer anchors never makes neighbours overlap by more than the 1 cm tolerance.
func gapFor(rot geo.Angle) geo.Cm {
	if rot%900 == 0 {
		return 0
	}
	return 2
}

// taper returns the value of index i of n, linear from a to b (b = 0: constant), half-even.
func taper(a, b geo.Cm, i, n int) geo.Cm {
	if b <= 0 || n <= 1 {
		return a
	}
	return a + geo.Cm(geo.DivRoundHalfEven(int64(b-a)*int64(i), int64(n-1)))
}

func validRotation(r geo.Angle) bool { return r >= 0 && r <= 3599 }

func codesValid(v *ValidationError, g *Generated) {
	for i := range g.Berths {
		if !ValidCode(g.Berths[i].Code) {
			v.Add("numbering.prefix", "code_too_long")
			return
		}
	}
	for i := range g.LandResources {
		if !ValidCode(g.LandResources[i].Code) {
			v.Add("numbering.prefix", "code_too_long")
			return
		}
	}
}

func finish(g *Generated) (Generated, error) {
	if g.Count() > MaxGenerated {
		return Generated{}, badParam("generator", "too_many")
	}
	v := &ValidationError{}
	codesValid(v, g)
	if err := v.Err(); err != nil {
		return Generated{}, err
	}
	return *g, nil
}

// PierParams are the parameters of GeneratePier.
type PierParams struct {
	KeyPrefix                     string
	Origin                        geo.Point // root of the pier
	Rotation                      geo.Angle // direction from root to head
	Length, Width                 geo.Cm
	Floating                      bool
	Code                          string
	Root                          Ref // optional quay
	Sides                         []Side
	BerthsPerSide                 int    // 0 = fill the length
	StartOffset                   geo.Cm // free length at the root
	BerthWidth, BerthLength       geo.Cm
	BerthWidthEnd, BerthLengthEnd geo.Cm // 0 = constant; else linear taper towards the head
	FingerEvery                   int    // 0 = no fingers
	FingerWidth, FingerLength     geo.Cm // defaults 60 and BerthLength·3/4
	HeadBerths                    int    // 0..4
	Mooring                       MooringType
	Numbering                     Numbering
	Depth                         geo.Cm
	PriceRule                     PriceClassRule
}

// slot is one berth or finger position along a side, in cm from the root.
type slot struct {
	start, width geo.Cm
	finger       bool
	index        int // berth index on the side
}

// pierSlots lays n berths (and fingers) from start; it returns the slots and the end of the last one.
func pierSlots(p *PierParams, n int, gap geo.Cm) ([]slot, geo.Cm) {
	s := p.StartOffset
	out := make([]slot, 0, n+n/max(p.FingerEvery, 1))
	end := s
	for i := range n {
		w := taper(p.BerthWidth, p.BerthWidthEnd, i, n)
		out = append(out, slot{start: s, width: w, index: i})
		end = s + w
		s = end + gap
		if p.FingerEvery > 0 && (i+1)%p.FingerEvery == 0 && i < n-1 {
			out = append(out, slot{start: s, width: p.FingerWidth, finger: true})
			end = s + p.FingerWidth
			s = end + gap
		}
	}
	return out, end
}

// GeneratePier generates a pier with berths on its sides, fingers and head berths (spec 8.3).
func GeneratePier(p PierParams) (Generated, error) {
	v := &ValidationError{}
	checkCommon(v, p.KeyPrefix, &p.Numbering)
	positive(v, "length_cm", p.Length)
	positive(v, "width_cm", p.Width)
	rectSide(v, "berth_width_cm", p.BerthWidth)
	rectSide(v, "berth_length_cm", p.BerthLength)
	rectSideOrZero(v, "berth_width_end_cm", p.BerthWidthEnd)
	rectSideOrZero(v, "berth_length_end_cm", p.BerthLengthEnd)
	nonNegative(v, "start_offset_cm", p.StartOffset)
	nonNegative(v, "finger_width_cm", p.FingerWidth)
	nonNegative(v, "finger_length_cm", p.FingerLength)
	if !validRotation(p.Rotation) {
		v.Add("rotation_dd", "invalid")
	}
	if !ValidCode(p.Code) {
		v.Add("code", "invalid")
	}
	if p.BerthsPerSide < 0 || p.BerthsPerSide > MaxGenerated {
		v.Add("berths_per_side", "invalid")
	}
	if p.FingerEvery < 0 || p.FingerEvery > 100 {
		v.Add("finger_every", "invalid")
	}
	if p.HeadBerths < 0 || p.HeadBerths > 4 {
		v.Add("head_berths", "invalid")
	}
	if len(p.Sides) == 0 && p.HeadBerths == 0 {
		v.Add("sides", "required")
	}
	for _, s := range p.Sides {
		if s != SideLeft && s != SideRight {
			v.Add("sides", "invalid")
		}
	}
	if p.Mooring == "" {
		p.Mooring = MooringLine
		if p.FingerEvery > 0 {
			p.Mooring = MooringFinger
		}
	} else if !slices.Contains(MooringTypes, p.Mooring) || p.Mooring == MooringBuoy {
		v.Add("mooring_type", "invalid")
	}
	if p.StartOffset >= p.Length && p.Length > 0 {
		v.Add("start_offset_cm", "invalid")
	}
	if err := v.Err(); err != nil {
		return Generated{}, err
	}
	if p.FingerWidth == 0 {
		p.FingerWidth = 60
	}
	if p.FingerLength == 0 {
		p.FingerLength = geo.Cm(geo.DivRoundHalfEven(int64(p.BerthLength)*3, 4))
	}
	gap := gapFor(p.Rotation)
	avail := p.Length
	if p.HeadBerths > 0 {
		avail -= gap
	}
	n := p.BerthsPerSide
	if n == 0 && len(p.Sides) > 0 {
		for k := 1; k <= MaxGenerated; k++ {
			if _, end := pierSlots(&p, k, gap); end > avail {
				break
			}
			n = k
		}
	} else if len(p.Sides) > 0 {
		if _, end := pierSlots(&p, n, gap); end > avail {
			return Generated{}, badParam("berths_per_side", "does_not_fit")
		}
	}
	if len(p.Sides) > 0 && n == 0 {
		return Generated{}, badParam("length_cm", "does_not_fit")
	}
	slots, _ := pierSlots(&p, n, gap)

	pierRef := Ref(p.KeyPrefix + "s1")
	sides := slices.Clone(p.Sides)
	if p.HeadBerths > 0 {
		sides = append(sides, SideHead)
	}
	pier := Structure{
		Ref: pierRef, Kind: StructurePier, Code: p.Code, Width: p.Width, Parent: p.Root, Sides: sides, Floating: p.Floating,
		Points: []geo.Point{p.Origin, offset(p.Origin, p.Rotation, 2*int64(p.Length), 0)},
	}
	if p.Depth > 0 {
		pier.Depth = []DepthSpan{{From: 0, To: p.Length, Depth: p.Depth}}
	}
	g := &Generated{Structures: []Structure{pier}}

	var number func(side Side, i int) int64
	start := p.Numbering.start()
	both := slices.Contains(p.Sides, SideLeft) && slices.Contains(p.Sides, SideRight)
	switch p.Numbering.Scheme {
	case NumberOddLeftEven:
		number = func(side Side, i int) int64 {
			if side == SideRight {
				return start + 1 + 2*int64(i)
			}
			return start + 2*int64(i)
		}
	case NumberPerSide:
		number = func(_ Side, i int) int64 { return start + int64(i) }
	default:
		number = func(side Side, i int) int64 {
			if side == SideRight && both {
				return start + int64(n) + int64(i)
			}
			return start + int64(i)
		}
	}
	maxNumber := start - 1
	berthKey, fingerKey := 0, 0
	for _, side := range []Side{SideLeft, SideRight} {
		if !slices.Contains(p.Sides, side) {
			continue
		}
		across2 := int64(p.Width)
		rot := geo.NewAngle(int64(p.Rotation) - 900)
		if side == SideRight {
			across2, rot = -across2, p.Rotation.Add(900)
		}
		for _, sl := range slots {
			along2 := 2*int64(sl.start) + int64(sl.width)
			anchor := offset(p.Origin, p.Rotation, along2, across2)
			if sl.finger {
				fingerKey++
				r := geo.Rect{Anchor: anchor, Width: sl.width, Length: p.FingerLength, Rotation: rot}
				g.Structures = append(g.Structures, Structure{
					Ref: Ref(p.KeyPrefix + "f" + strconv.Itoa(fingerKey)), Kind: StructureFinger,
					Code: p.Code + "-F" + strconv.Itoa(fingerKey), Rect: &r, Parent: pierRef, Floating: p.Floating,
				})
				continue
			}
			berthKey++
			l := taper(p.BerthLength, p.BerthLengthEnd, sl.index, n)
			k := number(side, sl.index)
			maxNumber = max(maxNumber, k)
			g.Berths = append(g.Berths, newBerth(Ref(p.KeyPrefix+"b"+strconv.Itoa(berthKey)), p.Numbering.code(k, side), pierRef, side,
				int64(sl.index)*10, geo.Rect{Anchor: anchor, Width: sl.width, Length: l, Rotation: rot}, p.Mooring, p.Depth, p.PriceRule))
		}
	}
	if p.HeadBerths > 0 {
		hw, hl := p.BerthWidth, p.BerthLength
		if p.BerthWidthEnd > 0 {
			hw = p.BerthWidthEnd
		}
		if p.BerthLengthEnd > 0 {
			hl = p.BerthLengthEnd
		}
		total := int64(p.HeadBerths)*int64(hw) + int64(p.HeadBerths-1)*int64(gap)
		for j := range p.HeadBerths {
			across2 := total - 2*int64(j)*int64(hw+gap) - int64(hw) // left to right
			anchor := offset(p.Origin, p.Rotation, 2*int64(p.Length), across2)
			k := maxNumber + 1 + int64(j)
			if p.Numbering.Scheme == NumberPerSide {
				k = start + int64(j)
			}
			berthKey++
			mooring := p.Mooring
			if mooring == MooringFinger {
				mooring = MooringLine
			}
			g.Berths = append(g.Berths, newBerth(Ref(p.KeyPrefix+"b"+strconv.Itoa(berthKey)), p.Numbering.code(k, SideHead), pierRef, SideHead,
				int64(j)*10, geo.Rect{Anchor: anchor, Width: hw, Length: hl, Rotation: p.Rotation}, mooring, p.Depth, p.PriceRule))
		}
	}
	return finish(g)
}

func newBerth(ref Ref, code string, structure Ref, side Side, pos int64, r geo.Rect, m MooringType, depth geo.Cm, rule PriceClassRule) Berth {
	return Berth{
		Ref: ref, Code: code, Structure: structure, Side: side, Position: pos, Rect: r, Mooring: m,
		MaxLOA: r.Length, MaxBeam: DefaultMaxBeam(r.Width), Depth: depth, PriceClass: PriceClassFor(r.Width, rule), Active: true,
	}
}

// DefaultMaxBeam is the default max beam of a berth: width − 2 × 25 cm fender margin, at least 1.
func DefaultMaxBeam(width geo.Cm) geo.Cm { return max(1, width-2*FenderMargin) }

// QuayBerthsParams are the parameters of GenerateQuayBerths.
type QuayBerthsParams struct {
	KeyPrefix   string
	Quay        Structure
	From, To    geo.Cm
	Side        Side
	BerthWidth  geo.Cm
	Count       int // 0 = fill by width
	BerthLength geo.Cm
	Mooring     MooringType
	Numbering   Numbering
	PriceRule   PriceClassRule
}

// GenerateQuayBerths places berths along an existing polyline structure between From and To. A slot never
// spans a vertex, and a slot that would overlap a berth placed before it (inside corners) is moved on.
func GenerateQuayBerths(p QuayBerthsParams) (Generated, error) {
	v := &ValidationError{}
	checkCommon(v, p.KeyPrefix, &p.Numbering)
	rectSide(v, "berth_width_cm", p.BerthWidth)
	rectSide(v, "berth_length_cm", p.BerthLength)
	if p.Side != SideLeft && p.Side != SideRight {
		v.Add("side", "invalid")
	}
	if p.Count < 0 || p.Count > MaxGenerated {
		v.Add("count", "invalid")
	}
	if p.Quay.Closed || p.Quay.Rect != nil || len(p.Quay.Points) < 2 {
		v.Add("quay_id", "invalid")
	}
	if p.Mooring == "" {
		p.Mooring = MooringLine
	} else if !slices.Contains(MooringTypes, p.Mooring) || p.Mooring == MooringBuoy {
		v.Add("mooring_type", "invalid")
	}
	if err := v.Err(); err != nil {
		return Generated{}, err
	}
	total := SupportLength(&p.Quay, p.Side)
	if p.From < 0 || p.To > total || p.From >= p.To {
		return Generated{}, badParam("to_cm", "outside_structure")
	}
	g := &Generated{}
	var boxes []geo.Box
	s := p.From
	w := p.BerthWidth
	limit := p.Count
	if limit == 0 {
		limit = MaxGenerated + 1
	}
	for s+w <= p.To && len(g.Berths) < limit {
		r, err := PlaceAlong(&p.Quay, p.Side, s, w, p.BerthLength)
		if err != nil {
			s += Snap // the slot spans a vertex: try further on
			continue
		}
		b := r.Bounds()
		clash := false
		for i := range g.Berths {
			if boxes[i].Intersects(b) && BerthsOverlap(g.Berths[i].Rect, r) {
				clash = true
				break
			}
		}
		if clash {
			s += Snap
			continue
		}
		i := len(g.Berths)
		g.Berths = append(g.Berths, newBerth(Ref(p.KeyPrefix+"b"+strconv.Itoa(i+1)), p.Numbering.code(p.Numbering.start()+int64(i), p.Side),
			p.Quay.Ref, p.Side, int64(i)*10, r, p.Mooring, 0, p.PriceRule))
		boxes = append(boxes, b)
		s += w + gapFor(r.Rotation)
	}
	if len(g.Berths) == 0 || (p.Count > 0 && len(g.Berths) < p.Count) {
		return Generated{}, badParam("count", "does_not_fit")
	}
	return finish(g)
}

// LinearAreaParams are the parameters of GenerateLinearArea.
type LinearAreaParams struct {
	KeyPrefix, Code string
	Structure       Structure
	Side            Side
	From, To        geo.Cm
	Mode            MooringMode
	Cell, Margin    geo.Cm
	MaxLOA, MaxBeam geo.Cm
}

// GenerateLinearArea creates a linear dynamic area on a pier side or a quay stretch.
func GenerateLinearArea(p LinearAreaParams) (Generated, error) {
	v := &ValidationError{}
	checkCommon(v, p.KeyPrefix, nil)
	if !ValidCode(p.Code) {
		v.Add("code", "invalid")
	}
	if p.Mode != ModeSternTo && p.Mode != ModeAlongside {
		v.Add("mode", "invalid")
	}
	if p.Structure.Kind != StructurePier && p.Structure.Kind != StructureQuay {
		v.Add("structure_id", "invalid")
	} else if !slices.Contains(usableSides(&p.Structure), p.Side) {
		v.Add("side", "invalid")
	}
	if p.Cell == 0 {
		p.Cell = 50
	}
	if p.Cell < Snap || p.Cell > 1000 {
		v.Add("cell_cm", "invalid")
	}
	if p.Margin < 0 || p.Margin > 1000 {
		v.Add("margin_cm", "invalid")
	}
	nonNegative(v, "max_loa_cm", p.MaxLOA)
	nonNegative(v, "max_beam_cm", p.MaxBeam)
	if err := v.Err(); err != nil {
		return Generated{}, err
	}
	if l := SupportLength(&p.Structure, p.Side); p.From < 0 || p.To > l || p.From >= p.To {
		return Generated{}, badParam("to_cm", "outside_structure")
	}
	return Generated{Areas: []DynamicArea{{
		Ref: Ref(p.KeyPrefix + "a1"), Code: p.Code, Kind: AreaLinear, Structure: p.Structure.Ref, Side: p.Side,
		From: p.From, To: p.To, Mode: p.Mode, Cell: p.Cell, Margin: p.Margin, MaxLOA: p.MaxLOA, MaxBeam: p.MaxBeam,
	}}}, nil
}

// BuoyFieldParams are the parameters of GenerateBuoyField.
type BuoyFieldParams struct {
	KeyPrefix   string
	Outline     geo.Polygon
	SwingRadius geo.Cm
	Layout      string // grid | staggered
	Spacing     geo.Cm // default 2·SwingRadius
	MaxLOA      geo.Cm
	Numbering   Numbering
	PriceRule   PriceClassRule
}

// GenerateBuoyField fills a polygon with buoys whose swing circles stay inside it and never overlap.
func GenerateBuoyField(p BuoyFieldParams) (Generated, error) {
	v := &ValidationError{}
	checkCommon(v, p.KeyPrefix, &p.Numbering)
	positive(v, "swing_radius_cm", p.SwingRadius)
	nonNegative(v, "max_loa_cm", p.MaxLOA)
	if p.Layout != "grid" && p.Layout != "staggered" {
		v.Add("layout", "invalid")
	}
	if p.Outline.Validate() != nil {
		v.Add("outline", "invalid")
	}
	if p.Spacing == 0 {
		p.Spacing = 2 * p.SwingRadius
	}
	if p.Spacing < 2*p.SwingRadius || p.Spacing > MaxMapSide {
		v.Add("spacing_cm", "too_small")
	}
	if err := v.Err(); err != nil {
		return Generated{}, err
	}
	r, sp := p.SwingRadius, p.Spacing
	rowStep := sp
	if p.Layout == "staggered" {
		n := uint64(ceilDiv(3*int64(sp)*int64(sp), 4)) //nolint:gosec // positive
		q := ISqrt(n)
		if q*q < n {
			q++
		}
		rowStep = geo.Cm(q) //nolint:gosec // bounded
	}
	length := p.MaxLOA
	if length <= 0 {
		length = r
	}
	width := max(1, geo.Cm(geo.DivRoundHalfEven(int64(length), 3)))
	b := p.Outline.Bounds()
	g := &Generated{}
	inside := func(c geo.Point) bool {
		if !p.Outline.ContainsStrict(c) {
			return false
		}
		for i := range p.Outline {
			if cmpSegDist(c, p.Outline[i], p.Outline[(i+1)%len(p.Outline)], r) < 0 {
				return false
			}
		}
		return true
	}
	row := 0
	for y := b.Min.Y + r; y+r <= b.Max.Y; y += rowStep {
		x0 := b.Min.X + r
		if p.Layout == "staggered" && row%2 == 1 {
			x0 += sp / 2
		}
		for x := x0; x+r <= b.Max.X; x += sp {
			c := geo.Point{X: x, Y: y}
			if !inside(c) {
				continue
			}
			i := len(g.Berths)
			if i >= MaxGenerated {
				return Generated{}, badParam("generator", "too_many")
			}
			br := geo.Rect{Anchor: c, Width: width, Length: length}
			bt := newBerth(Ref(p.KeyPrefix+"b"+strconv.Itoa(i+1)), p.Numbering.code(p.Numbering.start()+int64(i), SideNone), "", SideNone,
				int64(i)*10, br, MooringBuoy, 0, p.PriceRule)
			bt.SwingRadius = r
			g.Berths = append(g.Berths, bt)
		}
		row++
	}
	if len(g.Berths) == 0 {
		return Generated{}, badParam("outline", "does_not_fit")
	}
	return finish(g)
}

// LandGridParams are the parameters of GenerateLandGrid.
type LandGridParams struct {
	KeyPrefix             string
	Origin                geo.Point
	Rotation              geo.Angle // direction of the rows; items extend towards Rotation + 900
	Rows, Cols            int
	ItemWidth, ItemLength geo.Cm
	Aisle                 geo.Cm
	AisleEvery            int // an aisle after every AisleEvery rows (0 = none)
	Kind                  LandKind
	Numbering             Numbering
}

// GenerateLandGrid creates Rows × Cols land resources, numbered row by row.
func GenerateLandGrid(p LandGridParams) (Generated, error) {
	v := &ValidationError{}
	checkCommon(v, p.KeyPrefix, &p.Numbering)
	rectSide(v, "item_width_cm", p.ItemWidth)
	rectSide(v, "item_length_cm", p.ItemLength)
	nonNegative(v, "aisle_cm", p.Aisle)
	if p.Rows <= 0 || p.Cols <= 0 || p.Rows*p.Cols > MaxGenerated {
		v.Add("rows", "invalid")
	}
	if p.AisleEvery < 0 {
		v.Add("aisle_every", "invalid")
	}
	if !slices.Contains(LandKinds, p.Kind) {
		v.Add("kind", "invalid")
	}
	if !validRotation(p.Rotation) {
		v.Add("rotation_dd", "invalid")
	}
	if err := v.Err(); err != nil {
		return Generated{}, err
	}
	gap := gapFor(p.Rotation)
	g := &Generated{}
	for r := range p.Rows {
		y := int64(r) * int64(p.ItemLength+gap)
		if p.AisleEvery > 0 {
			y += int64(r/p.AisleEvery) * int64(p.Aisle)
		}
		for c := range p.Cols {
			along2 := 2*int64(c)*int64(p.ItemWidth+gap) + int64(p.ItemWidth)
			anchor := offset(p.Origin, p.Rotation, along2, -2*y)
			i := len(g.LandResources)
			g.LandResources = append(g.LandResources, LandResource{
				Ref: Ref(p.KeyPrefix + "l" + strconv.Itoa(i+1)), Code: p.Numbering.code(p.Numbering.start()+int64(i), SideNone), Kind: p.Kind,
				Rect: geo.Rect{Anchor: anchor, Width: p.ItemWidth, Length: p.ItemLength, Rotation: p.Rotation.Add(900)},
			})
		}
	}
	return finish(g)
}

// Lane is a corridor of a surface area: its cells are blocked.
type Lane struct {
	From, To geo.Point
	Width    geo.Cm
}

// SurfaceParams are the parameters of GenerateSurfaceArea.
type SurfaceParams struct {
	KeyPrefix, Code string
	Outline         geo.Polygon
	Cell            geo.Cm
	GridRotation    geo.Angle
	Exits           []geo.Point // map points; their cells are exits
	Lanes           []Lane
	Margin          geo.Cm
	MaxLOA          geo.Cm
	MaxHeight       geo.Cm
}

// GenerateSurfaceArea creates a yard or shed surface: lanes become blocked cells, exit points exit cells.
func GenerateSurfaceArea(p SurfaceParams) (Generated, error) {
	v := &ValidationError{}
	checkCommon(v, p.KeyPrefix, nil)
	if !ValidCode(p.Code) {
		v.Add("code", "invalid")
	}
	if p.Cell < Snap || p.Cell > 1000 {
		v.Add("cell_cm", "invalid")
	}
	if p.GridRotation%900 != 0 || !validRotation(p.GridRotation) {
		v.Add("grid_rotation_dd", "invalid")
	}
	if p.Margin < 0 || p.Margin > 1000 {
		v.Add("margin_cm", "invalid")
	}
	nonNegative(v, "max_loa_cm", p.MaxLOA)
	nonNegative(v, "max_height_cm", p.MaxHeight)
	if p.Outline.Validate() != nil {
		v.Add("outline", "invalid")
	}
	if err := v.Err(); err != nil {
		return Generated{}, err
	}
	a, err := surfaceArea(p.Outline, p.GridRotation, p.Cell, nil)
	if err != nil {
		return Generated{}, badParam("cell_cm", "too_many_cells")
	}
	c := int64(p.Cell)
	var exits []int
	for i, e := range p.Exits {
		q := MapToGrid(p.Outline, p.GridRotation, e)
		col, row := floorDiv(int64(q.X), c), floorDiv(int64(q.Y), c)
		if col < 0 || row < 0 || col >= int64(a.Cols) || row >= int64(a.Rows) {
			return Generated{}, badParam("exits["+strconv.Itoa(i)+"]", "outside_area")
		}
		exits = append(exits, int(row)*a.Cols+int(col))
	}
	blocked := map[int]bool{}
	for i, l := range p.Lanes {
		f, t := MapToGrid(p.Outline, p.GridRotation, l.From), MapToGrid(p.Outline, p.GridRotation, l.To)
		if f == t || l.Width <= 0 {
			return Generated{}, badParam("lanes["+strconv.Itoa(i)+"]", "invalid")
		}
		lr := geo.Rect{Anchor: f, Width: l.Width, Length: segLen(f, t), Rotation: AngleOf(int64(t.X-f.X), int64(t.Y-f.Y))}
		lp := lr.Polygon()
		lb := lp.Bounds()
		c0, c1 := max(0, floorDiv(int64(lb.Min.X), c)), min(int64(a.Cols)-1, floorDiv(int64(lb.Max.X), c))
		r0, r1 := max(0, floorDiv(int64(lb.Min.Y), c)), min(int64(a.Rows)-1, floorDiv(int64(lb.Max.Y), c))
		for r := r0; r <= r1; r++ {
			for col := c0; col <= c1; col++ {
				x0, y0 := geo.Cm(col*c), geo.Cm(r*c)
				cell := geo.Polygon{{X: x0, Y: y0}, {X: x0 + p.Cell, Y: y0}, {X: x0 + p.Cell, Y: y0 + p.Cell}, {X: x0, Y: y0 + p.Cell}}
				if geo.ConvexOverlap(cell, lp) {
					blocked[int(r)*a.Cols+int(col)] = true
				}
			}
		}
	}
	bl := make([]int, 0, len(blocked))
	for k := range blocked {
		bl = append(bl, k)
	}
	slices.Sort(bl)
	slices.Sort(exits)
	exits = slices.Compact(exits)
	return Generated{Areas: []DynamicArea{{
		Ref: Ref(p.KeyPrefix + "a1"), Code: p.Code, Kind: AreaSurface, Outline: slices.Clone(p.Outline), GridRotation: p.GridRotation,
		Cell: p.Cell, Margin: p.Margin, MaxLOA: p.MaxLOA, MaxHeight: p.MaxHeight, Blocked: bl, Exits: exits,
	}}}, nil
}
