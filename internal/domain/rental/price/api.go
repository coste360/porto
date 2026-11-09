package price

import (
	"time"

	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Licence modes of rental units.
const (
	LicenceNever     = "never"
	LicenceAlways    = "always"
	LicenceThreshold = "threshold"
)

// LicenceRequired applies D7: never false, always true, threshold when the total engine power exceeds the
// threshold; never required when a skipper is booked.
func LicenceRequired(mode string, engineKWMilli, thresholdKWMilli int64, withSkipper bool) bool {
	if withSkipper {
		return false
	}
	switch mode {
	case LicenceAlways:
		return true
	case LicenceThreshold:
		return engineKWMilli > thresholdKWMilli
	}
	return false
}

// Build prices a booking through pricing.RentalQuote and returns its snapshot.
func Build(cats []pricing.Catalog, in SnapshotInput) (Snapshot, error) {
	days := in.Days
	if days < 1 {
		days = 1
	}
	b, err := pricing.RentalQuote(cats, pricing.RentalInput{
		ItemID: in.UnitItemID, Kind: in.Kind, Start: in.Start, Days: days, Slots: in.Slots, Band: in.Band,
		SkipperItemID: in.SkipperItemID, Extras: in.Extras, Channel: in.Channel, CustomerCategory: in.CustomerCategory,
		QuotedOn: in.QuotedOn,
	})
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		UnitID: in.UnitID, FormulaID: in.FormulaID, FormulaCode: in.FormulaCode, Kind: in.Kind, Start: in.Start,
		Days: days, Slots: in.Slots, Band: in.Band, LicenceRequired: in.LicenceRequired,
		WithSkipper: in.SkipperItemID != "", Breakdown: b, EngineVersion: pricing.RentalEngineVersion,
	}, nil
}

// LateFee returns the late fee of a return at actual for a planned end (m11-plan §5.4): real elapsed minutes
// minus grace, in blocks of stepMin rounded up, each block = hourCents x step / 60 half-even. Early returns and
// returns within the grace give no fee; a missing late item (hourCents 0) gives a zero fee.
func LateFee(plannedEnd, actual time.Time, graceMin, stepMin int, hourCents money.Cents) LateFeeResult {
	if stepMin <= 0 {
		stepMin = 30
	}
	late := int(actual.Sub(plannedEnd)/time.Minute) - graceMin
	if late <= 0 {
		return LateFeeResult{LateMinutes: max(late, 0)}
	}
	blocks := (late + stepMin - 1) / stepMin
	block, err := hourCents.MulRatChecked(int64(stepMin), 60)
	if err != nil {
		return LateFeeResult{LateMinutes: late, Blocks: blocks}
	}
	fee, err := block.MulRatChecked(int64(blocks), 1)
	if err != nil {
		fee = 0
	}
	return LateFeeResult{LateMinutes: late, Blocks: blocks, Fee: fee}
}

// Fuel returns the metered fuel of a return (m11-plan §5.5): an explicit refuel wins, otherwise (out - in) eighths
// of the tank, half-even to the milli and clamped at zero; the amount is litres x unit price, half-even.
func Fuel(in FuelInput) FuelResult {
	var litres qty.Milli
	if in.Refuel != nil {
		litres = *in.Refuel
	} else if diff := in.OutEighths - in.InEighths; diff > 0 && in.Tank > 0 {
		v, err := in.Tank.MulInt(int64(diff))
		if err == nil {
			litres, err = v.DivInt(8)
		}
		if err != nil {
			litres = 0
		}
	}
	if litres < 0 {
		litres = 0
	}
	amount, err := litres.MulCents(in.UnitCents)
	if err != nil || amount < 0 {
		amount = 0
	}
	return FuelResult{Litres: litres, Amount: amount}
}

// DepositProposal returns min(deposit, estimates) (D17), never negative.
func DepositProposal(deposit, estimates money.Cents) money.Cents {
	return max(min(deposit, estimates), 0)
}

// Return returns the return totals: damage excess = max(0, estimates - retained), total = late fee + fuel + extras
// + excess. A retention above the deposit is ErrDepositExceeds.
func Return(in ReturnInput) (ReturnTotals, error) {
	st, err := Settle(in.Deposit, in.Retained, in.Estimates)
	if err != nil {
		return ReturnTotals{}, err
	}
	total := in.LateFee + in.Fuel + in.Extras + st.Excess
	if in.LateFee < 0 || in.Fuel < 0 || in.Extras < 0 || total < 0 {
		return ReturnTotals{}, money.ErrOverflow
	}
	return ReturnTotals{DamageExcess: st.Excess, Total: total, Proposal: DepositProposal(in.Deposit, in.Estimates)}, nil
}

// Settle splits a deposit for a retention decision; retained > deposit (or negative) is ErrDepositExceeds.
// Retained + released = deposit (INV-98); the excess is the part of the estimates not covered by the retention.
func Settle(deposit, retained, estimates money.Cents) (Settlement, error) {
	if retained < 0 || retained > deposit || deposit < 0 {
		return Settlement{}, ErrDepositExceeds
	}
	return Settlement{Retained: retained, Released: deposit - retained, Excess: max(estimates-retained, 0)}, nil
}

// Days returns the rental days of a kind for per-day extras (hourly, half day and day = 1).
func Days(kind pricing.RentalKind, dates int) int { return pricing.RentalDays(kind, dates) }
