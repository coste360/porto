// Package vectors builds the Go ↔ TypeScript parity vectors of fit.Check (testdata/fit/cases.json, m3-plan
// §2.1): hand-written edge cases with hand-written expectations, followed by seeded random cases whose
// expectations are the Go results. The random generator is a local splitmix64, so the package takes no
// randomness from the standard library and the output is identical on every platform.
package vectors

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/fit"
	"github.com/coste360/porto/internal/geo"
)

// File is the content of testdata/fit/cases.json.
type File struct {
	Version int    `json:"version"`
	Cases   []Case `json:"cases"`
}

// Vessel is the wire form of fit.Vessel (nil = unknown).
type Vessel struct {
	LOACm          int64   `json:"loa_cm"`
	BeamCm         int64   `json:"beam_cm"`
	DraftCm        *int64  `json:"draft_cm,omitempty"`
	AirDraftCm     *int64  `json:"air_draft_cm,omitempty"`
	Hull           string  `json:"hull"`
	PowerRequiredA *int64  `json:"power_required_a,omitempty"`
	InsuranceUntil *string `json:"insurance_until,omitempty"`
}

// Target is the wire form of fit.Target.
type Target struct {
	Kind            string `json:"kind"`
	MaxLOACm        int64  `json:"max_loa_cm"`
	MaxBeamCm       int64  `json:"max_beam_cm"`
	LengthCm        int64  `json:"length_cm"`
	WidthCm         int64  `json:"width_cm"`
	DepthCm         *int64 `json:"depth_cm,omitempty"`
	MaxAirDraftCm   *int64 `json:"max_air_draft_cm,omitempty"`
	MultihullOK     bool   `json:"multihull_ok"`
	OverhangAllowed bool   `json:"overhang_allowed"`
	PowerA          *int64 `json:"power_a,omitempty"`
	LandKind        string `json:"land_kind,omitempty"`
}

// Rules is the wire form of fit.Rules.
type Rules struct {
	LOAToleranceCm    int64 `json:"loa_tolerance_cm"`
	SafetyClearanceCm int64 `json:"safety_clearance_cm"`
	DepthWarnMarginCm int64 `json:"depth_warn_margin_cm"`
	WasteThresholdBP  int64 `json:"waste_threshold_bp"`
}

// Expect is the expected result: codes only (limits are not part of the parity contract).
type Expect struct {
	OK       bool     `json:"ok"`
	Blocking []string `json:"blocking"`
	Warnings []string `json:"warnings"`
	FillBP   int64    `json:"fill_bp"`
}

// Case is one vector.
type Case struct {
	Name      string `json:"name"`
	Vessel    Vessel `json:"vessel"`
	Target    Target `json:"target"`
	LastNight string `json:"last_night"`
	Rules     Rules  `json:"rules"`
	Expect    Expect `json:"expect"`
}

func cmPtr(p *int64) *geo.Cm {
	if p == nil {
		return nil
	}
	v := geo.Cm(*p)
	return &v
}

// ToFit converts a case to the arguments of fit.Check.
func (c Case) ToFit() (fit.Vessel, fit.Target, calendar.Date, fit.Rules) {
	v := fit.Vessel{
		LOA: geo.Cm(c.Vessel.LOACm), Beam: geo.Cm(c.Vessel.BeamCm), Draft: cmPtr(c.Vessel.DraftCm),
		AirDraft: cmPtr(c.Vessel.AirDraftCm), Hull: c.Vessel.Hull, PowerRequiredA: c.Vessel.PowerRequiredA,
	}
	if c.Vessel.InsuranceUntil != nil {
		d := calendar.MustParse(*c.Vessel.InsuranceUntil)
		v.InsuranceUntil = &d
	}
	t := fit.Target{
		Kind: fit.TargetKind(c.Target.Kind), MaxLOA: geo.Cm(c.Target.MaxLOACm), MaxBeam: geo.Cm(c.Target.MaxBeamCm),
		Length: geo.Cm(c.Target.LengthCm), Width: geo.Cm(c.Target.WidthCm), Depth: cmPtr(c.Target.DepthCm),
		MaxAirDraft: cmPtr(c.Target.MaxAirDraftCm), MultihullOK: c.Target.MultihullOK,
		OverhangAllowed: c.Target.OverhangAllowed, PowerA: c.Target.PowerA, LandKind: c.Target.LandKind,
	}
	r := fit.Rules{
		LOATolerance: geo.Cm(c.Rules.LOAToleranceCm), SafetyClearance: geo.Cm(c.Rules.SafetyClearanceCm),
		DepthWarnMargin: geo.Cm(c.Rules.DepthWarnMarginCm), WasteThresholdBP: c.Rules.WasteThresholdBP,
	}
	return v, t, calendar.MustParse(c.LastNight), r
}

// Run evaluates a case with fit.Check.
func Run(c Case) Expect {
	res := fit.Check(c.ToFit())
	return Expect{OK: res.OK, Blocking: res.BlockingCodeList(), Warnings: res.WarningCodeList(), FillBP: res.FillBP}
}

func p(v int64) *int64 { return &v }

func s(v string) *string { return &v }

// defaults of the hand cases: a 12 m × 4 m berth, 3 m deep, with power 16 A and water.
var (
	baseRules  = Rules{LOAToleranceCm: 50, SafetyClearanceCm: 30, DepthWarnMarginCm: 10, WasteThresholdBP: 6000}
	baseBerth  = Target{Kind: "berth", MaxLOACm: 1200, MaxBeamCm: 400, LengthCm: 1200, WidthCm: 450, DepthCm: p(300), PowerA: p(16)}
	baseVessel = Vessel{LOACm: 1100, BeamCm: 380, DraftCm: p(150), Hull: "monohull_sail", InsuranceUntil: s("2027-12-31")}
	lastNight  = "2027-07-10"
)

func codes(c ...string) []string {
	if c == nil {
		return []string{}
	}
	return c
}

type edit func(c *Case)

func hand(name string, e Expect, edits ...edit) Case {
	c := Case{Name: name, Vessel: baseVessel, Target: baseBerth, LastNight: lastNight, Rules: baseRules}
	// copy the pointer fields so that edits never alias the defaults
	c.Vessel.DraftCm, c.Target.DepthCm, c.Target.PowerA = p(*baseVessel.DraftCm), p(*baseBerth.DepthCm), p(*baseBerth.PowerA)
	for _, ed := range edits {
		ed(&c)
	}
	if e.Blocking == nil {
		e.Blocking = []string{}
	}
	if e.Warnings == nil {
		e.Warnings = []string{}
	}
	c.Expect = e
	return c
}

func ok(fill int64, warnings ...string) Expect {
	return Expect{OK: true, Blocking: []string{}, Warnings: codes(warnings...), FillBP: fill}
}

func blocked(fill int64, blocking []string, warnings ...string) Expect {
	return Expect{Blocking: blocking, Warnings: codes(warnings...), FillBP: fill}
}

func loa(v int64) edit         { return func(c *Case) { c.Vessel.LOACm = v } }
func beam(v int64) edit        { return func(c *Case) { c.Vessel.BeamCm = v } }
func draft(v *int64) edit      { return func(c *Case) { c.Vessel.DraftCm = v } }
func airDraft(v *int64) edit   { return func(c *Case) { c.Vessel.AirDraftCm = v } }
func hull(h string) edit       { return func(c *Case) { c.Vessel.Hull = h } }
func power(v *int64) edit      { return func(c *Case) { c.Vessel.PowerRequiredA = v } }
func insurance(v *string) edit { return func(c *Case) { c.Vessel.InsuranceUntil = v } }
func depth(v *int64) edit      { return func(c *Case) { c.Target.DepthCm = v } }
func maxAir(v *int64) edit     { return func(c *Case) { c.Target.MaxAirDraftCm = v } }
func targetPower(v *int64) edit {
	return func(c *Case) { c.Target.PowerA = v }
}
func multihullOK() edit { return func(c *Case) { c.Target.MultihullOK = true } }
func overhangOK() edit  { return func(c *Case) { c.Target.OverhangAllowed = true } }
func maxLOA(v int64) edit {
	return func(c *Case) { c.Target.MaxLOACm = v }
}
func maxBeam(v int64) edit { return func(c *Case) { c.Target.MaxBeamCm = v } }
func size(l, w int64) edit { return func(c *Case) { c.Target.LengthCm, c.Target.WidthCm = l, w } }
func kind(k string) edit   { return func(c *Case) { c.Target.Kind = k } }
func land(k string) edit {
	return func(c *Case) {
		c.Target = Target{Kind: "land", LandKind: k, MaxLOACm: 1300, MaxBeamCm: 450, LengthCm: 1300, WidthCm: 450}
	}
}
func tolerance(v int64) edit { return func(c *Case) { c.Rules.LOAToleranceCm = v } }
func clearance(v int64) edit { return func(c *Case) { c.Rules.SafetyClearanceCm = v } }
func waste(v int64) edit     { return func(c *Case) { c.Rules.WasteThresholdBP = v } }

// fill of the default vessel on the default berth: 1100×380 / (1200×450) = 0.774074… → 7741 bp.
const baseFill = 7741

// Handwritten returns the edge cases (every code, every boundary ±1 cm, nil fields, every land kind).
func Handwritten() []Case {
	B := func(c ...string) []string { return c }
	return []Case{
		hand("default fits", ok(baseFill)),
		hand("loa = max", ok(8444), loa(1200)),
		hand("loa = max + 1 overhang", ok(8451, fit.CodeOverhang), loa(1201)),
		hand("loa = max + tolerance overhang", ok(8796, fit.CodeOverhang), loa(1250)),
		hand("loa = max + tolerance + 1 blocks", blocked(8803, B(fit.CodeLOAExceeds)), loa(1251)),
		hand("overhang allowed", ok(8451), loa(1201), overhangOK()),
		hand("overhang allowed still blocks beyond tolerance", blocked(8803, B(fit.CodeLOAExceeds)), loa(1251), overhangOK()),
		hand("tolerance 0 blocks at max + 1", blocked(8451, B(fit.CodeLOAExceeds)), loa(1201), tolerance(0)),
		hand("tolerance 0 at max", ok(8444), loa(1200), tolerance(0)),
		hand("beam = max", ok(8148), beam(400)),
		hand("beam = max + 1 blocks (no tolerance)", blocked(8169, B(fit.CodeBeamExceeds)), beam(401)),
		hand("beam = max - 1", ok(8128), beam(399)),
		hand("draft + clearance = depth", ok(baseFill, fit.CodeDepthNearLimit), draft(p(270))),
		hand("draft + clearance = depth + 1 blocks", blocked(baseFill, B(fit.CodeDraftExceeds)), draft(p(271))),
		hand("draft near limit lower edge", ok(baseFill, fit.CodeDepthNearLimit), draft(p(261))),
		hand("draft just outside warn margin", ok(baseFill), draft(p(260))),
		hand("unknown draft skips", ok(baseFill), draft(nil)),
		hand("unknown depth skips", ok(baseFill), draft(p(900)), depth(nil)),
		hand("clearance 0", ok(baseFill, fit.CodeDepthNearLimit), draft(p(300)), clearance(0)),
		hand("clearance 0 depth + 1", blocked(baseFill, B(fit.CodeDraftExceeds)), draft(p(301)), clearance(0)),
		hand("air draft = max", ok(baseFill), airDraft(p(1500)), maxAir(p(1500))),
		hand("air draft = max + 1 blocks", blocked(baseFill, B(fit.CodeAirDraftExceeds)), airDraft(p(1501)), maxAir(p(1500))),
		hand("air draft unknown limit skips", ok(baseFill), airDraft(p(5000))),
		hand("air draft unknown vessel skips", ok(baseFill), maxAir(p(100))),
		hand("catamaran without multihull_ok", blocked(baseFill, B(fit.CodeMultihullNotAllowed)), hull(fit.HullCatamaran)),
		hand("trimaran without multihull_ok", blocked(baseFill, B(fit.CodeMultihullNotAllowed)), hull(fit.HullTrimaran)),
		hand("catamaran with multihull_ok", ok(baseFill), hull(fit.HullCatamaran), multihullOK()),
		hand("motor monohull", ok(baseFill), hull("monohull_motor")),
		hand("rib", ok(baseFill), hull("rib")),
		hand("gozzo", ok(baseFill), hull("gozzo")),
		hand("other hull", ok(baseFill), hull("other")),
		hand("loa zero invalid", blocked(0, B(fit.CodeVesselInvalid)), loa(0)),
		hand("beam zero invalid", blocked(0, B(fit.CodeVesselInvalid)), beam(0)),
		hand("negative loa invalid", blocked(0, B(fit.CodeVesselInvalid)), loa(-5)),
		hand("several blocking sorted", blocked(9505, B(fit.CodeBeamExceeds, fit.CodeDraftExceeds, fit.CodeLOAExceeds, fit.CodeMultihullNotAllowed)),
			loa(1300), beam(400+15), draft(p(290)), hull(fit.HullCatamaran), func(c *Case) { c.Vessel.BeamCm = 401; c.Vessel.LOACm = 1250 + 30 }),
		hand("fill below threshold", ok(5185, fit.CodeFillBelowThreshold), loa(800), beam(350)),
		hand("fill at threshold", ok(6000), loa(1200), beam(270)),
		hand("fill just below threshold", ok(5978, fit.CodeFillBelowThreshold), loa(1200), beam(269)),
		hand("waste threshold 0 never warns", ok(889), loa(400), beam(120), waste(0)),
		hand("berth without size has fill 0", ok(0, fit.CodeFillBelowThreshold), size(0, 0)),
		hand("berth without size and threshold 0", ok(0), size(0, 0), waste(0)),
		hand("power sufficient", ok(baseFill), power(p(16))),
		hand("power insufficient", ok(baseFill, fit.CodePowerInsufficient), power(p(32))),
		hand("power missing on berth", ok(baseFill, fit.CodePowerInsufficient), power(p(16)), targetPower(nil)),
		hand("power zero required", ok(baseFill), power(p(0)), targetPower(nil)),
		hand("insurance missing", ok(baseFill, fit.CodeInsuranceMissing), insurance(nil)),
		hand("insurance until last night", ok(baseFill), insurance(s(lastNight))),
		hand("insurance expires the day before the last night", ok(baseFill, fit.CodeInsuranceExpiresInPeriod), insurance(s("2027-07-09"))),
		hand("warnings sorted", ok(5185, fit.CodeDepthNearLimit, fit.CodeFillBelowThreshold, fit.CodeInsuranceMissing, fit.CodePowerInsufficient),
			loa(800), beam(350), draft(p(265)), insurance(nil), power(p(32))),
		hand("unlimited max loa", ok(10000), loa(5000), maxLOA(0)),
		hand("unlimited max beam", ok(10000), beam(900), maxBeam(0)),
		hand("linear area fill 10000", ok(10000), kind("linear_area")),
		hand("linear area no fill warning", ok(10000), kind("linear_area"), loa(400), beam(150)),
		hand("surface area fill 10000", ok(10000), kind("surface_area")),
		hand("surface area loa blocks", blocked(10000, B(fit.CodeLOAExceeds)), kind("surface_area"), loa(1300)),
		hand("surface max height", blocked(10000, B(fit.CodeAirDraftExceeds)), kind("surface_area"), airDraft(p(801)), maxAir(p(800))),
		hand("buoy with no beam limit", ok(10000), maxBeam(0), size(0, 0), waste(0), func(c *Case) { c.Target.MaxLOACm = 1500 }, func(c *Case) {
			c.Target.LengthCm, c.Target.WidthCm = 1100, 380
		}),
		hand("land parking never blocks", ok(0), land("parking"), loa(9000), hull(fit.HullCatamaran)),
		hand("land box never blocks", ok(0), land("box"), loa(9000)),
		hand("land storeroom never blocks", ok(0), land("storeroom"), beam(9000)),
		hand("land locker never blocks", ok(0), land("locker"), insurance(nil)),
		hand("land custom never blocks", ok(0), land("custom"), power(p(63))),
		hand("land parking invalid vessel", blocked(0, B(fit.CodeVesselInvalid)), land("parking"), loa(0)),
		hand("cradle fits", ok(7145), land("cradle")),
		hand("cradle loa = length + tolerance", ok(8769, fit.CodeOverhang), land("cradle"), loa(1350)),
		hand("cradle loa = length + tolerance + 1", blocked(8776, B(fit.CodeLOAExceeds)), land("cradle"), loa(1351)),
		hand("trailer spot beam + 1", blocked(8480, B(fit.CodeBeamExceeds)), land("trailer_spot"), beam(451), loa(1100)),
		hand("trailer spot fits", ok(7145), land("trailer_spot")),
		hand("cradle catamaran blocks", blocked(7145, B(fit.CodeMultihullNotAllowed)), land("cradle"), hull(fit.HullCatamaran)),
	}
}

// splitmix is a small deterministic generator (splitmix64).
type splitmix struct{ x uint64 }

func (g *splitmix) next() uint64 {
	g.x += 0x9e3779b97f4a7c15
	z := g.x
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// in returns an integer in [lo, hi].
func (g *splitmix) in(lo, hi int64) int64 { return lo + int64(g.next()%uint64(hi-lo+1)) } //nolint:gosec // small ranges

func (g *splitmix) chance(pct int64) bool { return g.in(1, 100) <= pct }

var (
	hulls     = []string{"monohull_motor", "monohull_sail", "catamaran", "trimaran", "rib", "gozzo", "other"}
	kinds     = []string{"berth", "berth", "berth", "linear_area", "surface_area", "land"}
	landKinds = []string{"parking", "box", "storeroom", "locker", "cradle", "trailer_spot", "custom"}
)

// Random returns n seeded random cases; their expectations are the Go results.
func Random(seed uint64, n int) []Case {
	g := &splitmix{x: seed}
	out := make([]Case, 0, n)
	opt := func(pct, lo, hi int64) *int64 {
		if g.chance(pct) {
			return p(g.in(lo, hi))
		}
		return nil
	}
	for i := range n {
		maxLOA := g.in(400, 2500)
		v := Vessel{
			LOACm: g.in(-10, maxLOA+150), BeamCm: g.in(0, 800), DraftCm: opt(70, 50, 400), AirDraftCm: opt(40, 200, 2500),
			Hull: hulls[g.in(0, int64(len(hulls)-1))], PowerRequiredA: opt(40, 0, 63),
		}
		if g.chance(70) {
			d := calendar.New(2027, 1, 1).AddDays(int(g.in(0, 400)))
			v.InsuranceUntil = s(d.String())
		}
		t := Target{
			Kind: kinds[g.in(0, int64(len(kinds)-1))], MaxLOACm: maxLOA, MaxBeamCm: g.in(0, 700), LengthCm: g.in(0, 2600),
			WidthCm: g.in(0, 800), DepthCm: opt(70, 100, 600), MaxAirDraftCm: opt(30, 300, 2500), MultihullOK: g.chance(30),
			OverhangAllowed: g.chance(30), PowerA: opt(60, 6, 63),
		}
		if t.Kind == "land" {
			t.LandKind = landKinds[g.in(0, int64(len(landKinds)-1))]
		}
		r := Rules{LOAToleranceCm: g.in(0, 100), SafetyClearanceCm: g.in(0, 60), DepthWarnMarginCm: g.in(0, 30), WasteThresholdBP: g.in(0, 10000)}
		c := Case{Name: "random " + strconv.Itoa(i+1), Vessel: v, Target: t, LastNight: calendar.New(2027, 6, 1).AddDays(int(g.in(0, 120))).String(), Rules: r}
		c.Expect = Run(c)
		out = append(out, c)
	}
	return out
}

// Cases returns the hand cases followed by n random cases.
func Cases(seed uint64, n int) []Case { return append(Handwritten(), Random(seed, n)...) }

// Encode returns the JSON file of the cases (indented, trailing newline).
func Encode(cases []Case) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(File{Version: 1, Cases: cases}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
