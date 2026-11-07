// Package engine is the pure core of the occupancy engine (spec 10; m3-plan §2.2, §3): the occupancy rows of an
// allocation (Footprint × nights − released nights, INV-03), free intervals of linear areas, the search solver
// with its integer score and segments, compaction proposals, conflict grouping and the day-view anomalies.
//
// Pure: no store, no clock, no time.Now, no randomness, no I/O (depguard pure-engines). "Today" and "now" are
// explicit inputs. Lengths are geo.Cm, nights calendar.Date.
package engine
