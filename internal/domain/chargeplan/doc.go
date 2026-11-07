// Package chargeplan is the pure planner of charge lines (m5-plan §3.4): the billable target of an origin from
// its price snapshot breakdown and adjustments, the remainders still to charge, the split of an instalment amount
// over them with cumulative VAT rounding, and the totals per (rate, nature).
//
// It is pure (depguard pure-engines): no store, no clock, no time.Now, no randomness, no I/O. Money is
// money.Cents and every split goes through money.Allocate, money.VATSplit or money.DivRoundHalfEven, so that the
// charges of an origin sum to its target per line and per (rate, nature) exactly (INV-57).
package chargeplan
