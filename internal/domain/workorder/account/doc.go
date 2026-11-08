// Package account builds the final account (consuntivo) of a work order from approved time entries, material
// movements, consumed service lines and external costs, with margins per line, per work type and per order and the
// comparison with the quote (m10-plan §9). It is pure (INV-21): no store, no clock, no randomness, no float. The
// reconciliation equations of §9.3 hold by construction (INV-93, TestRapid_AccountReconciles).
//
// Owned by the ORDERS builder after the contract phase.
package account
