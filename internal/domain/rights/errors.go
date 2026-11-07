package rights

import (
	"errors"
	"fmt"

	"github.com/coste360/porto/internal/store"
)

// Sentinel errors (m4-plan §7.4; names fixed), mapped in internal/http/respond/errmap.go.
var (
	// ErrSharesInvalid means holder shares are not 1..1000 or do not sum to 1000 (INV-40); the error is a
	// *SumError. 422 shares_invalid.
	ErrSharesInvalid = errors.New("rights: holder shares do not sum to 1000")
	// ErrTableIncomplete means a millesimal table does not sum to 1000 (INV-41); the error is a *SumError. 422
	// table_incomplete.
	ErrTableIncomplete = errors.New("rights: millesimal table does not sum to 1000")
	// ErrTableActive means an active (or archived) millesimal table cannot change. 409 table_active.
	ErrTableActive = errors.New("rights: millesimal table is active")
	// ErrTransferRequirements means the outgoing holders have unsettled overdue instalments and the policy is
	// block; the error is a *RequirementsError. 422 transfer_requirements.
	ErrTransferRequirements = errors.New("rights: transfer requirements not met")
	// ErrMandateOverlap means another mandate of the right covers some of the nights. 409 mandate_overlap.
	ErrMandateOverlap = errors.New("rights: mandate overlap")
	// ErrInvalidTransition means the action is not allowed in the current state. 409 invalid_transition.
	ErrInvalidTransition = errors.New("rights: invalid transition")
	// ErrVersionMismatch means If-Match did not match the current version. 412 version_conflict.
	ErrVersionMismatch = errors.New("rights: version mismatch")
	// ErrNotFound wraps store.ErrNotFound. 404.
	ErrNotFound = store.ErrNotFound
	// ErrNotImplemented is returned by the CONTRACT stubs until RIGHTS-CHARGES lands the service.
	ErrNotImplemented = errors.New("rights: not implemented")
)

// SumError carries the sum that broke INV-40 or INV-41 (details.sum_millis).
type SumError struct {
	Err       error // ErrSharesInvalid or ErrTableIncomplete
	SumMillis int64
}

func (e *SumError) Error() string { return fmt.Sprintf("%s (sum %d)", e.Err.Error(), e.SumMillis) }

// Unwrap returns the sentinel.
func (e *SumError) Unwrap() error { return e.Err }

// RequirementIssue is one unmet transfer requirement (ids and amounts only).
type RequirementIssue struct {
	Code                 string   `json:"code"` // unsettled_fees, unsettled_common_charges
	CustomerID           string   `json:"customer_id,omitempty"`
	HolderInstallmentIDs []string `json:"holder_installment_ids,omitempty"`
	AmountCents          int64    `json:"amount_cents"`
}

// RequirementsError carries the issues of a blocked transfer (details.issues).
type RequirementsError struct {
	Issues []RequirementIssue
}

func (e *RequirementsError) Error() string {
	return fmt.Sprintf("%s: %d issue(s)", ErrTransferRequirements.Error(), len(e.Issues))
}

// Is makes errors.Is(err, ErrTransferRequirements) true.
func (e *RequirementsError) Is(target error) bool { return target == ErrTransferRequirements }
