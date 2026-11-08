// Package calc builds every M7 report from raw store rows (spec 21.3, m7-plan §3, §6): one Build<Report>(Input)
// per report returning a Result whose Totals are only the fold of its Records, so a dashboard tile (Tile<Report>,
// a read of Totals) equals the sum of its drill-down records by construction (INV-62).
//
// The result, record and tile types in types.go are the frozen CONTRACT of M7 (handlers, export and the
// dashboard read them); the Input types and the Build/Fold/Tile functions belong to the engines.
//
// It is pure: no store, no clock, no randomness, no float (depguard pure-engines). Money is money.Cents, lengths
// geo.Cm, areas geo.Cm2, ratios integer basis points (BP) computed half-even from Σ numerator / Σ denominator,
// never averaged.
package calc
