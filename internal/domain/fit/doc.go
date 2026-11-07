// Package fit checks whether a vessel is compatible with a berth, a dynamic area or a land resource (spec 10.2;
// m3-plan §2.1). It is pure: no store, no clock, no randomness, integers only (lengths geo.Cm, ratios in basis
// points). Overlap is never a fit issue: no double occupancy is a database guarantee (INV-01).
//
// Go and TypeScript (web/shared/src/map/fit.ts) give the same answers on the vectors of testdata/fit/cases.json.
package fit
