package price

import (
	"errors"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// ErrNotImplemented was returned by the CONTRACT skeleton (kept for callers that still reference it).
var ErrNotImplemented = errors.New("rental price: not implemented")

// ErrDepositExceeds is a retention above the deposit (422 deposit_exceeds).
var ErrDepositExceeds = errors.New("rental price: retention exceeds the deposit")

// SnapshotInput is what a booking prices: the unit item, the formula kind and quantity, the skipper item when a
// skipper is booked, the extras with their per-day flag.
type SnapshotInput struct {
	UnitID           string
	UnitItemID       string
	FormulaID        string
	FormulaCode      string
	Kind             pricing.RentalKind
	Start            calendar.Date
	Days             int
	Slots            int
	Band             string
	SkipperItemID    string
	Extras           []pricing.RentalExtra
	Channel          pricing.Channel
	CustomerCategory string
	QuotedOn         calendar.Date
	LicenceRequired  bool
}

// Snapshot is the price stored on a booking (rental_bookings.price_snapshot, engine_version): recomputing it with
// the same catalogue gives the same total (INV-20 extended).
type Snapshot struct {
	UnitID          string                  `json:"unit_id"`
	FormulaID       string                  `json:"formula_id"`
	FormulaCode     string                  `json:"formula_code"`
	Kind            pricing.RentalKind      `json:"kind"`
	Start           calendar.Date           `json:"start"`
	Days            int                     `json:"days"`
	Slots           int                     `json:"slots,omitempty"`
	Band            string                  `json:"band,omitempty"`
	LicenceRequired bool                    `json:"licence_required"`
	WithSkipper     bool                    `json:"with_skipper"`
	Breakdown       pricing.RentalBreakdown `json:"breakdown"`
	EngineVersion   int                     `json:"engine_version"`
}

// Total returns the snapshot total.
func (s Snapshot) Total() money.Cents { return s.Breakdown.Total }

// LateFeeResult is the late fee of a return: real minutes late after grace and the fee.
type LateFeeResult struct {
	LateMinutes int
	Blocks      int
	Fee         money.Cents
}

// FuelInput is the metered fuel of a return: eighths out and in, the tank, an explicit refuel typed by the
// operator (wins when set) and the fuel unit price.
type FuelInput struct {
	OutEighths int
	InEighths  int
	Tank       qty.Milli
	Refuel     *qty.Milli
	UnitCents  money.Cents
}

// FuelResult is the litres and the amount of metered fuel.
type FuelResult struct {
	Litres qty.Milli
	Amount money.Cents
}

// ReturnInput collects the return charge parts (m11-plan §5.5).
type ReturnInput struct {
	LateFee   money.Cents
	Fuel      money.Cents
	Extras    money.Cents
	Deposit   money.Cents
	Estimates money.Cents // sum of open damage estimates
	Retained  money.Cents // deposit retained (decision)
}

// ReturnTotals are the return charge total and the damage excess (estimates beyond the retained deposit).
type ReturnTotals struct {
	DamageExcess money.Cents
	Total        money.Cents
	Proposal     money.Cents
}

// Settlement is a deposit decision: retained + released = deposit, retained <= deposit (INV-98).
type Settlement struct {
	Retained money.Cents
	Released money.Cents
	Excess   money.Cents
}
