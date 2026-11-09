// Package reconcile is the pure reconciliation of M12 (spec 24.4; m12-plan §8): the contractual expectation of a
// sensor subject (Expect), the discrepancy rule and the rules R1-R8 (Evaluate) that turn physical states,
// contractual facts and presences into alerts, proposals and the only automatic effect, the presence of annual
// mooring contracts when the marina enables it. No store, no clock, no randomness, no float: Now and Loc are in
// the input. Actions never touch contracts or occupancy (INV-100).
package reconcile
