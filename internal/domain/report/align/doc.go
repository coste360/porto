// Package align computes report periods and comparisons (spec 21.3 "Comparazione delle statistiche", m7-plan
// §5): the previous period k years back with date alignment (Feb 29 → Feb 28) or weekday alignment (shift of
// 7 × round(365.2425 k / 7) days, D19), the mapping of each previous day onto a current day (never by index), and
// the day / ISO week / calendar month buckets of a series.
//
// It is pure: no store, no clock, no randomness, no float (depguard pure-engines).
package align
