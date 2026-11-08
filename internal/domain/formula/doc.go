// Package formula is the pure formula language of the yard catalogue (spec 18.2, ADR 0006, m10-plan §3.2-§3.4):
// a recursive-descent parser, a type checker and a fixed-point evaluator over qty.Milli values.
//
// Numbers are qty.Milli with half-even rounding to the milli at every * and / (left to right); + and - are exact
// and overflow is an error. Booleans and enums (strings) are compared exactly; and/or short-circuit and if is lazy.
// Functions: if, min, max, round, ceil, floor, band. No loops, no user functions, no external data.
//
// The package is pure (INV-21, INV-29): no store, no clock, no randomness, no float, no I/O. Parsing and evaluation
// never panic on any input (FuzzParse, FuzzEval). LanguageVersion is stored in every quote snapshot; a change of
// semantics bumps it and keeps the old evaluator path for recompute (INV-28).
//
// Owned by the QUOTE builder after the contract phase.
package formula
