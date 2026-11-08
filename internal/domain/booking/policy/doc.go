// Package policy holds the pure rules of online booking (m8-plan §3.1, §3.3, §6, §7.2): stay rules (nights,
// advance, arrival weekdays, in the marina time zone), approval rules, deposit, cancellation refund from the
// tiers of a policy snapshot, and online quota caps.
//
// The package is pure: no store, no clock, no randomness, no time.Now. "Now" and the time zone are arguments.
// It imports only calendar, money and geo (depguard in .golangci.yml).
package policy
