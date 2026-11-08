// Package price computes yard quote lines from formulas and catalogue unit prices (m10-plan §4.2, §3.6). It is
// pure (INV-21): no store, no clock, no randomness, no float. Recompute(snapshot) reproduces the stored lines
// exactly (INV-28), so packages, variables, tables and price lists edited later never change an existing quote.
//
// Owned by the QUOTE builder after the contract phase; ORDERS imports the types (budget lines, VAT data).
package price
