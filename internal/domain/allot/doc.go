// Package allot holds the pure allocation rules of rights and common expenses (spec 14.4–14.5; m4-plan §4.4,
// §4.5): splits among holders by share (money.Allocate, largest remainder), millesimal allocation of budget items
// with Σ = item amount exactly (INV-42), management fee bases, ISTAT update and pro rata.
//
// It is pure: no store, no clock, no randomness (depguard pure-engines).
package allot
