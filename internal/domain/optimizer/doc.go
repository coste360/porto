// Package optimizer arranges boats on a surface area (a yard or shed dynamic area) and judges their accessibility
// (spec 17.4, 17.5; m9-plan §5, §6). It is pure: no store, no clock, no math/rand, no float. Randomness comes from
// its own seeded PRNG (splitmix64 seeding xoshiro256**), and the only notion of time is the caller's
// context.Context deadline, checked every 64 iterations: with an iteration budget the deadline does not cut, the
// result is a function of (Input, Seed) only.
//
// Grids are cells of the layout.Area surface grid (index = row·Cols + col). Grids larger than MaxCells are
// coarsened by the smallest integer factor k (a coarse cell is free only if all its fine cells are) and every
// output placement is re-validated on the fine grid with layout.Footprint.
//
// Accessibility is a documented heuristic (m9-plan §5): the boat's footprint box translates cell by cell (or
// rotates in place when the max(w,h) square is free) through the free grid of the date until a whole side lies on
// the grid border inside one gate (a 4-connected component of exit cells). Exits of a day are extracted greedily
// nearest to a gate; the blocking set is found by removal in gate-distance order and pruned by reverse
// re-insertion (minimal, not minimum).
//
// The objective is lexicographic on int64 terms (Score): unplaced, exits blocked, arrivals blocked, moves,
// −largest free rectangle on the start night. Arrange builds two constructions (keep current; exit-ordered
// bottom-left fill from the gates), improves the better one by late-acceptance hill climbing and checks the best
// with Validate, an independent checker: a failure is a bug and never ships an invalid layout.
package optimizer
