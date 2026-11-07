package chargeplan

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Errors.
var (
	// ErrTargetMismatch: Σ target gross differs from the contract total (the issue of that instalment fails and a
	// worklist item billing_target_mismatch is opened; no wrong charge is ever written).
	ErrTargetMismatch = errors.New("chargeplan: target does not match the contract total")
	// ErrNoLines: a split needs at least one target line with a non-zero gross.
	ErrNoLines = errors.New("chargeplan: no line to charge")
)

// Line is one billable line of a target or of a charge. Key is stable across charges of the same origin:
// "snap:<seq>" for snapshot lines, "add:<addendum_id>" for services added by addendum.
type Line struct {
	Key              string
	ItemID           string
	ItemCode         string
	Description      string
	RevenueClass     string
	ExternalCode     string
	Qty              qty.Milli
	VATRateBP        int64
	VATNature        string
	PriceIncludesVAT bool
	Net, VAT, Gross  money.Cents
	ServiceFrom      calendar.Date
	ServiceTo        calendar.Date
}

// Target is what an origin must charge in total.
type Target struct {
	Lines []Line
}

// Adjustment is a snapshot adjustment (INV-27) as the planner needs it: its kind (move, move_undo,
// vessel_change, early_payment, addendum price_change, service_add, service_remove), its signed delta and, for
// service_add, the added line.
type Adjustment struct {
	Kind       string
	AddendumID string
	DeltaCents money.Cents
	// LineKeys restricts the adjustment to these line keys (berth lines for moves; the removed line for
	// service_remove); empty = every line.
	LineKeys []string
	// Added is the new line of a service_add addendum.
	Added *Line
}

// ChargedLine is a line already charged for the origin (from charge_lines: plan_key, gross, vat).
type ChargedLine struct {
	Key        string
	GrossCents money.Cents
	VATCents   money.Cents
	// Line describes the charged line (item, VAT data); used when the key is no longer in the target, so that
	// the remainder can be written back as a negative line.
	Line *Line
}

// Remainder is what is still to charge on one target line (signed).
type Remainder struct {
	Line  Line
	Gross money.Cents
	VAT   money.Cents
}

// Part is one line of one charge.
type Part struct {
	Line  Line
	Qty   qty.Milli
	Unit  money.Cents // informative unit price in the item's price terms (gross or net)
	Net   money.Cents
	VAT   money.Cents
	Gross money.Cents
}

// VATBucket sums lines per (rate, nature), sorted like pricing.Breakdown.VATSummary.
type VATBucket = pricing.VATBucket
