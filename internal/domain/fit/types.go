package fit

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/geo"
)

// Vessel is the effective size of a vessel for the check: measured dimensions when dimensions_source is
// measured (spec 11.2). Nil pointers are unknown values: the related check is skipped.
type Vessel struct {
	LOA, Beam       geo.Cm
	Draft, AirDraft *geo.Cm
	Hull            string // vessels.hull_type
	PowerRequiredA  *int64
	InsuranceUntil  *calendar.Date // latest expires_on of a valid insurance_policy document; nil = none on file
	// Support is how the boat stands ashore on a surface (storage_contract_details.support: cradle, trailer, rack,
	// none); "" skips the support check (m9-plan §3.3).
	Support string
	// DisplacementKg is the boat weight; nil skips the weight check.
	DisplacementKg *int64
}

// TargetKind is the kind of resource checked.
type TargetKind string

// Target kinds.
const (
	TargetBerth       TargetKind = "berth"
	TargetLinearArea  TargetKind = "linear_area"
	TargetSurfaceArea TargetKind = "surface_area"
	TargetLand        TargetKind = "land"
)

// Target is the resource a vessel is checked against.
type Target struct {
	Kind            TargetKind
	MaxLOA, MaxBeam geo.Cm  // berth max_loa/max_beam; area max_loa/max_beam (0 = unlimited); land length/width
	Length, Width   geo.Cm  // berth length/width (fill ratio); land length/width
	Depth           *geo.Cm // berth depth_cm; area: min depth of the occupied interval (the caller computes it)
	MaxAirDraft     *geo.Cm // berth max_air_draft_cm; surface max_height_cm
	MultihullOK     bool    // berth attribute tag "multihull_ok"; areas: true
	OverhangAllowed bool
	PowerA          *int64
	LandKind        string // land only: parking, box, storeroom, locker, cradle, trailer_spot, custom
	// Supports are the supports a surface accepts (dynamic_areas.supports); empty = any (m9-plan §3.3).
	Supports []string
	// MaxWeightKg is the weight the hull cells of the placement bear (area max_weight_kg × hull cells, computed by
	// the caller); nil = no limit.
	MaxWeightKg *int64
}

// Rules are the marina parameters of the check.
type Rules struct {
	LOATolerance     geo.Cm // harbor_settings.loa_tolerance_cm
	SafetyClearance  geo.Cm // occupancy_settings.safety_clearance_cm (default 30)
	DepthWarnMargin  geo.Cm // 10
	WasteThresholdBP int64  // occupancy_settings.waste_threshold_bp (default 6000)
}

// DefaultDepthWarnMargin is the margin of the depth_near_limit warning.
const DefaultDepthWarnMargin geo.Cm = 10

// Issue is one blocking issue or warning. Limit and Actual are set for dimensional codes.
type Issue struct {
	Code          string
	Limit, Actual geo.Cm
}

// Result is the outcome of Check.
type Result struct {
	OK          bool    // no blocking issue
	Blocking    []Issue // sorted by code
	Warnings    []Issue // sorted by code
	FillBP      int64   // vessel LOA×Beam / (Length×Width) in bp, half-even; 10000 for areas; 0 for land non-boat kinds
	Overridable bool    // no blocking code is physical (vessel_invalid, support_not_allowed): fit_override allowed
}

// Blocking issue codes (m3-plan §2.1).
const (
	CodeLOAExceeds          = "loa_exceeds"
	CodeBeamExceeds         = "beam_exceeds"
	CodeDraftExceeds        = "draft_exceeds"
	CodeAirDraftExceeds     = "air_draft_exceeds"
	CodeMultihullNotAllowed = "multihull_not_allowed"
	CodeVesselInvalid       = "vessel_invalid"      // not overridable
	CodeSupportNotAllowed   = "support_not_allowed" // not overridable (physical, m9-plan D6)
	CodeWeightExceeds       = "weight_exceeds"
)

// Warning codes (m3-plan §2.1).
const (
	CodeFillBelowThreshold       = "fill_below_threshold"
	CodeDepthNearLimit           = "depth_near_limit"
	CodePowerInsufficient        = "power_insufficient"
	CodeInsuranceMissing         = "insurance_missing"
	CodeInsuranceExpiresInPeriod = "insurance_expires_in_period"
	CodeOverhang                 = "overhang"
)

// BlockingCodes returns every blocking code, sorted.
func BlockingCodes() []string {
	return []string{
		CodeAirDraftExceeds, CodeBeamExceeds, CodeDraftExceeds, CodeLOAExceeds, CodeMultihullNotAllowed,
		CodeSupportNotAllowed, CodeVesselInvalid, CodeWeightExceeds,
	}
}

// WarningCodes returns every warning code, sorted.
func WarningCodes() []string {
	return []string{
		CodeDepthNearLimit, CodeFillBelowThreshold, CodeInsuranceExpiresInPeriod, CodeInsuranceMissing, CodeOverhang,
		CodePowerInsufficient,
	}
}
