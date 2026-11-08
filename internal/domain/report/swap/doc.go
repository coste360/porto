// Package swap suggests swaps and moves that reduce allocation waste (spec 21.3 "Spreco allocativo", m7-plan
// §3.3): pairs of allocations on two berths of one map that fit each other's berth without blocking issues and
// without conflicts, and single moves found with the occupancy engine on a snapshot of the report rows. The
// suggestions are advisory and read-only (D21): applying one goes through the M3 move/swap operations.
//
// It is pure: no store, no clock, no randomness, no float (depguard pure-engines).
package swap
