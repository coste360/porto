// Package due computes when a maintenance plan target is due (spec 20.1; m9-plan §8.2). It is pure: no store, no
// clock, no randomness, no float. months = last done (or the plan start) + N months with end-of-month clamp;
// fixed_date = the next occurrence of MM-DD (29 February falls back to 28 February in common years); cycles = due
// as soon as the meter advanced by every units since the last done.
package due
