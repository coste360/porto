// Package schedule computes instalment plans (spec 13.7; m4-plan §3.3): an optional deposit at signature, then
// instalments at fixed dates, at intervals or in a single amount, with an optional early-payment alternative.
//
// It is pure: no store, no clock, no randomness (depguard pure-engines). Plan always returns amounts that sum to
// the total exactly (INV-31): parts are floored to the rounding unit and the remainder goes to the last
// instalment; deposits in basis points use half-even (money.Cents.PercentBP).
package schedule
