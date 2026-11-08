package fit

import (
	"slices"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
)

// Hull types that need a multihull-capable berth.
const (
	HullCatamaran = "catamaran"
	HullTrimaran  = "trimaran"
)

// Land kinds that hold a boat and are checked against its size; the others never block.
const (
	LandCradle      = "cradle"
	LandTrailerSpot = "trailer_spot"
)

// FullBP is 100 % in basis points.
const FullBP int64 = 10000

// IsMultihull reports whether a hull type is a catamaran or a trimaran.
func IsMultihull(hull string) bool { return hull == HullCatamaran || hull == HullTrimaran }

// holdsBoat reports whether the target holds the vessel itself (all but the land kinds that do not).
func holdsBoat(t Target) bool {
	return t.Kind != TargetLand || t.LandKind == LandCradle || t.LandKind == LandTrailerSpot
}

// Check returns the compatibility of v with t for a stay whose last night is lastNight (m3-plan §2.1, §13 D19).
//
// Blocking codes: loa_exceeds (LOA > MaxLOA + tolerance), beam_exceeds (Beam > MaxBeam, no tolerance),
// draft_exceeds (Draft + clearance > Depth), air_draft_exceeds, multihull_not_allowed and vessel_invalid (not
// overridable). A zero MaxLOA or MaxBeam is unlimited; nil draft, depth, air draft or limits skip the check.
// Land kinds other than cradle and trailer_spot never block and have no warnings.
func Check(v Vessel, t Target, lastNight calendar.Date, r Rules) Result {
	if v.LOA <= 0 || v.Beam <= 0 {
		return Result{Blocking: []Issue{{Code: CodeVesselInvalid}}, Warnings: []Issue{}}
	}
	res := Result{Blocking: []Issue{}, Warnings: []Issue{}}
	if !holdsBoat(t) {
		res.OK, res.Overridable = true, true
		return res
	}
	block := func(code string, limit, actual geo.Cm) {
		res.Blocking = append(res.Blocking, Issue{Code: code, Limit: limit, Actual: actual})
	}
	warn := func(code string, limit, actual geo.Cm) {
		res.Warnings = append(res.Warnings, Issue{Code: code, Limit: limit, Actual: actual})
	}
	if t.MaxLOA > 0 {
		switch {
		case v.LOA > t.MaxLOA+r.LOATolerance:
			block(CodeLOAExceeds, t.MaxLOA+r.LOATolerance, v.LOA)
		case v.LOA > t.MaxLOA && !t.OverhangAllowed:
			warn(CodeOverhang, t.MaxLOA, v.LOA)
		}
	}
	if t.MaxBeam > 0 && v.Beam > t.MaxBeam {
		block(CodeBeamExceeds, t.MaxBeam, v.Beam)
	}
	if v.Draft != nil && t.Depth != nil {
		need := *v.Draft + r.SafetyClearance
		switch {
		case need > *t.Depth:
			block(CodeDraftExceeds, *t.Depth, need)
		case need+r.DepthWarnMargin > *t.Depth:
			warn(CodeDepthNearLimit, *t.Depth, need)
		}
	}
	if v.AirDraft != nil && t.MaxAirDraft != nil && *v.AirDraft > *t.MaxAirDraft {
		block(CodeAirDraftExceeds, *t.MaxAirDraft, *v.AirDraft)
	}
	if IsMultihull(v.Hull) && !t.MultihullOK {
		block(CodeMultihullNotAllowed, 0, 0)
	}
	res.FillBP = fillBP(v, t)
	if t.Kind == TargetBerth && res.FillBP < r.WasteThresholdBP {
		warn(CodeFillBelowThreshold, 0, 0)
	}
	if t.Kind != TargetLand && v.PowerRequiredA != nil && *v.PowerRequiredA > 0 && (t.PowerA == nil || *t.PowerA < *v.PowerRequiredA) {
		warn(CodePowerInsufficient, 0, 0)
	}
	switch {
	case v.InsuranceUntil == nil:
		warn(CodeInsuranceMissing, 0, 0)
	case v.InsuranceUntil.Before(lastNight):
		warn(CodeInsuranceExpiresInPeriod, 0, 0)
	}
	byCode := func(a, b Issue) int { return strings.Compare(a.Code, b.Code) }
	slices.SortFunc(res.Blocking, byCode)
	slices.SortFunc(res.Warnings, byCode)
	res.OK = len(res.Blocking) == 0
	res.Overridable = true
	return res
}

// FillBP is the fill ratio Check reports (LOA×Beam over Length×Width in bp, half-even, capped at 10000; 10000 for
// areas): the allocation waste of the reports is 10000 − FillBP (m7-plan §3.3).
func FillBP(v Vessel, t Target) int64 { return fillBP(v, t) }

// fillBP is LOA×Beam over Length×Width in basis points (half-even), capped at 10000; areas are always full.
func fillBP(v Vessel, t Target) int64 {
	if t.Kind == TargetLinearArea || t.Kind == TargetSurfaceArea {
		return FullBP
	}
	if t.Length <= 0 || t.Width <= 0 {
		return 0
	}
	// Lengths are below 10^6 cm: the products fit int64 with room to spare.
	bp := geo.DivRoundHalfEven(int64(v.LOA)*int64(v.Beam)*FullBP, int64(t.Length)*int64(t.Width))
	return min(bp, FullBP)
}

// BlockingCodeList returns the blocking codes of a result.
func (r Result) BlockingCodeList() []string {
	out := make([]string, len(r.Blocking))
	for i, is := range r.Blocking {
		out[i] = is.Code
	}
	return out
}

// WarningCodeList returns the warning codes of a result.
func (r Result) WarningCodeList() []string {
	out := make([]string, len(r.Warnings))
	for i, is := range r.Warnings {
		out[i] = is.Code
	}
	return out
}

// Worse returns the worse of two results: blocked before fitting, then more blocking codes, then lower fill.
func Worse(a, b Result) Result {
	switch {
	case a.OK != b.OK:
		if !a.OK {
			return a
		}
		return b
	case len(a.Blocking) != len(b.Blocking):
		if len(a.Blocking) > len(b.Blocking) {
			return a
		}
		return b
	case len(a.Warnings) != len(b.Warnings):
		if len(a.Warnings) > len(b.Warnings) {
			return a
		}
		return b
	}
	if b.FillBP < a.FillBP {
		return b
	}
	return a
}
