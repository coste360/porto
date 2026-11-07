// Package levels selects the next reminder level of a charge from its due date, today, the marina policy and
// the last level sent (m5-plan §5.5, §16 D13: pre-due, due, courteous, formal, final notice). It is pure
// (depguard pure-engines): no store, no clock, no I/O. Owned by CASH-DRIVERS.
package levels
