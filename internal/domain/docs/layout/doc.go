// Package layout lays a Markdown block AST out on A4 pages of positioned boxes in integer units of 1/100 mm,
// with headers, footers, widow/orphan control and table rows never split, and records the signature fields
// (m4-plan §6.3). Layout.Text gives the golden text per page used by the regression tests (ADR 0005).
//
// It is pure: no store, no clock, no randomness (depguard pure-engines).
package layout
