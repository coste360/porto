package charge

import (
	"errors"
	"strconv"

	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/store"
)

// Errors (m5-plan §7.4). Mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrImmutable: the charge is exported, invoiced, paid or offset; use an adjustment (409 charge_immutable).
	ErrImmutable = errors.New("charge: immutable")
	// ErrCancelled: the charge is cancelled (409 charge_cancelled).
	ErrCancelled = errors.New("charge: cancelled")
	// ErrUseEdit: the charge can still be edited in place (409 use_edit).
	ErrUseEdit = errors.New("charge: edit in place instead")
	// ErrExportEmpty: nothing to export in the selection (422 export_empty).
	ErrExportEmpty = errors.New("charge: nothing to export")
	// ErrExportTooLarge: more than 20 000 charges (422 export_too_large, details.count); see *ExportTooLargeError.
	ErrExportTooLarge = errors.New("charge: export too large")
	// ErrChargeExists: the origin already has its live primary charge (409 charge_exists, INV-54).
	ErrChargeExists = errors.New("charge: origin already charged")
	// ErrNoTerminationAmount: the contract has no termination amount to charge (409 no_termination_amount).
	ErrNoTerminationAmount = errors.New("charge: no termination amount")
	// ErrNotExported: an invoice reference needs an exported charge (409 not_exported).
	ErrNotExported = errors.New("charge: not exported")
	// ErrBillingItemMissing: no origin item configured for a charge without a snapshot (422 billing_item_missing).
	ErrBillingItemMissing = errors.New("charge: billing item missing")
	// ErrTargetMismatch is chargeplan.ErrTargetMismatch.
	ErrTargetMismatch = chargeplan.ErrTargetMismatch
	// ErrCustomerRequired: an invoiceable charge needs a customer (422 customer_required).
	ErrCustomerRequired = errors.New("charge: customer required")
	// ErrExporterUnknown: no ChargeExporter with that code (422 provider_unknown).
	ErrExporterUnknown = errors.New("charge: exporter unknown")
	// ErrImportNotUndoable: an opening balance import whose charges have matches or offsets (409).
	ErrImportNotUndoable = errors.New("charge: import not undoable")
	// ErrVersionMismatch: If-Match differs (412).
	ErrVersionMismatch = errors.New("charge: version mismatch")
	// ErrNotFound is the store sentinel (404).
	ErrNotFound = store.ErrNotFound
	// ErrNotImplemented is returned by the contract-phase skeleton until CHARGES-EXPORT lands M5.
	ErrNotImplemented = errors.New("charge: not implemented")
)

// MaxExportCharges is the largest export (m5-plan §16 D27).
const MaxExportCharges = 20000

// ExportTooLargeError carries the number of charges selected (details.count).
type ExportTooLargeError struct {
	Count int
}

func (e *ExportTooLargeError) Error() string {
	return ErrExportTooLarge.Error() + ": " + strconv.Itoa(e.Count)
}

// Is makes errors.Is(err, ErrExportTooLarge) true.
func (e *ExportTooLargeError) Is(target error) bool { return target == ErrExportTooLarge }
