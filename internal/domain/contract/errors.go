package contract

import (
	"errors"

	"github.com/coste360/porto/internal/store"
)

// Sentinel errors (m3-plan §7.4; names fixed), mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrInvalidTransition means the action is not allowed in the current state (§4 tables); the error is a
	// *TransitionError. 409 invalid_transition.
	ErrInvalidTransition = errors.New("contract: invalid transition")
	// ErrQuoteExpired means the quote is past its validity. 409 quote_expired.
	ErrQuoteExpired = errors.New("contract: quote expired")
	// ErrCustomerRequired means a transit cannot be confirmed without a customer. 422 customer_required.
	ErrCustomerRequired = errors.New("contract: customer required")
	// ErrPriceListChanged means the price list of the snapshot is no longer the active one for the arrival
	// (priced before the write transaction, m3-plan §4.6). 409 conflict: the client retries.
	ErrPriceListChanged = errors.New("contract: price list changed")
	// ErrRepriceRefused means a move (or its undo) would replace the price of a contract that has no price
	// snapshot, or bring its total below zero (m3-plan §13 D34). 409 conflict: nothing is written.
	ErrRepriceRefused = errors.New("contract: reprice refused")
	// ErrVersionMismatch means If-Match did not match the current version. 412 version_conflict.
	ErrVersionMismatch = errors.New("contract: version mismatch")
	// ErrNotFound wraps store.ErrNotFound. 404.
	ErrNotFound = store.ErrNotFound

	// M4 (m4-plan §7.4; names fixed).

	// ErrRenewalExists means the contract already has an open renewal proposal or a successor (INV-33). 409
	// renewal_exists.
	ErrRenewalExists = errors.New("contract: renewal exists")
	// ErrProposalClosed means the renewal proposal is no longer open (declined, expired, completed). 409
	// proposal_closed.
	ErrProposalClosed = errors.New("contract: proposal closed")
	// ErrInsuranceRequired means the insurance policy is block and the vessel has no valid insurance document on
	// the contract start; the error is an *InsuranceError. 422 insurance_required.
	ErrInsuranceRequired = errors.New("contract: insurance required")
	// ErrCustomerEmailMissing means a quote or proposal cannot be emailed: the customer has no email. 422
	// customer_email_missing.
	ErrCustomerEmailMissing = errors.New("contract: customer email missing")
	// ErrPaperScanRequired means require_paper_scan is set and no scan was uploaded. 422 paper_scan_required.
	ErrPaperScanRequired = errors.New("contract: paper scan required")
	// ErrVesselInOtherFleet means a vessel is already in another active charter fleet. 409 vessel_in_other_fleet.
	ErrVesselInOtherFleet = errors.New("contract: vessel in another fleet")
	// ErrVesselNotInFleet means a rotation names a vessel outside the charter's fleet. 422 vessel_not_in_fleet.
	ErrVesselNotInFleet = errors.New("contract: vessel not in fleet")
	// ErrSettleConflict means a bulk settlement found an instalment not settleable (status or version); the error
	// is a *SettleError naming the first failing id. 409 conflict.
	ErrSettleConflict = errors.New("contract: settle conflict")
	// ErrNotInArrears means the arrears suspension found, under the contract lock, no overdue amount reaching the
	// policy minimum (or an active customer exclusion): the contract is left as it is. Internal (job only).
	ErrNotInArrears = errors.New("contract: not in arrears")
	// ErrNotImplementedM4 is returned by the CONTRACT skeleton (m4_skeleton.go) until CONTRACTS lands M4.
	ErrNotImplementedM4 = errors.New("contract: not implemented (M4)")
	// ErrNotImplementedM5 is returned by the M5 entry points of payments.go until PAYMENTS lands them.
	ErrNotImplementedM5 = errors.New("contract: not implemented (M5)")
)

// InsuranceError names the vessel without a valid insurance (details.vessel_id).
type InsuranceError struct {
	VesselID string
}

func (e *InsuranceError) Error() string {
	return ErrInsuranceRequired.Error() + ": vessel " + e.VesselID
}

// Is makes errors.Is(err, ErrInsuranceRequired) true.
func (e *InsuranceError) Is(target error) bool { return target == ErrInsuranceRequired }

// SettleError names the first instalment of a bulk settlement that could not be settled (details.id).
type SettleError struct {
	ID string
}

func (e *SettleError) Error() string { return ErrSettleConflict.Error() + ": " + e.ID }

// Is makes errors.Is(err, ErrSettleConflict) true.
func (e *SettleError) Is(target error) bool { return target == ErrSettleConflict }

// TransitionError names the refused transition.
type TransitionError struct {
	From   string // current status
	Action Action
}

func (e *TransitionError) Error() string {
	return ErrInvalidTransition.Error() + ": " + string(e.Action) + " from " + e.From
}

// Is makes errors.Is(err, ErrInvalidTransition) true.
func (e *TransitionError) Is(target error) bool { return target == ErrInvalidTransition }
