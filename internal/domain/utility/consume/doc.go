// Package consume turns meter readings into consumptions (spec 20.4; m9-plan §9.2, §9.3). It is pure: no store, no
// clock, no randomness, no float; quantities are qty.Milli.
//
// Delta: (cur − prev) × multiplier / 1000, half-even; a replacement pair restarts the index; with a rollover
// register a negative difference wraps (rollover − prev + cur) and is flagged. Nights of an interval are
// prev.read_on … cur.read_on − 1 (a same-day pair goes to night prev.read_on). Split shares Δ among the occupant
// segments in proportion to their nights with the largest remainder method in milli (ties to the later segment);
// Σ shares = Δ exactly (INV-91); nights without occupant form the unassigned share.
package consume
