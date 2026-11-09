// Package price is the pure money side of rentals (m11-plan §5): the booking price snapshot over
// pricing.RentalQuote, the licence rule, the late fee from real elapsed minutes, metered fuel, the return totals
// and the deposit proposal and settlement (a deposit is never revenue until retained, INV-98). Money is
// money.Cents, litres and hours qty.Milli, half-even rounding. No store, no clock, no float. Owned by FLEET after
// the contract phase.
package price
