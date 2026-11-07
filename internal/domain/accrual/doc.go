// Package accrual spreads amounts over days (spec 21.3, INV-60): the quota of each day of a line's period, with
// the remainder on the last day so the quotas sum to the amount exactly. Used in M4 by the management fee pro
// rata and the mandate revenues; the accrual reports are M7.
//
// It is pure: no store, no clock, no randomness (depguard pure-engines).
package accrual
