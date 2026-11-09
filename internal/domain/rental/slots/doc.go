// Package slots is the pure time model of rentals (m11-plan §4, D2, D6, D8): a slot s in 0..47 is the wall-clock
// half hour [s x 30 min, (s+1) x 30 min) of a local date in the marina time zone; a range runs from (date, slot)
// inclusive to (date, slot) exclusive with an end slot 1..48. Expand lists the slot keys of a range plus the buffer
// after it (carrying over midnight). The open window and every band exclude slots 4-5 (02:00-03:00), the only
// labels that DST removes or repeats. No store, no clock, no float. Owned by BOOKING after the contract phase.
package slots
