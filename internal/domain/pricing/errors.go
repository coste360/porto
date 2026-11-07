package pricing

import (
	"errors"
	"fmt"

	"github.com/coste360/porto/internal/calendar"
)

// Sentinel errors of the engine (m2-plan §2.2). Typed errors wrap them: errors.Is works on both.
var (
	// ErrNotImplemented is returned by the CONTRACT stubs until the engine lands.
	ErrNotImplemented = errors.New("pricing: not implemented")
	// ErrNoPrice means no rule prices the input; the error is a *NoPriceError.
	ErrNoPrice = errors.New("pricing: no price")
	// ErrInvalidInput means the input is not acceptable; the error is an *InputError.
	ErrInvalidInput = errors.New("pricing: invalid input")
	// ErrPromoNotApplicable means a promo code was given but no promo discount applies to the quote.
	ErrPromoNotApplicable = errors.New("pricing: promo code not applicable")
	// ErrOverflow means an amount went beyond the engine limits (m2-plan §2.4 step 14).
	ErrOverflow = errors.New("pricing: amount overflow")
	// ErrInvalidCatalog means a tool (Matrix, Renew, Diff) was given a catalog it cannot work on.
	ErrInvalidCatalog = errors.New("pricing: invalid catalog")
)

// Reasons of a NoPriceError.
const (
	ReasonNoRule          = "no_rule"
	ReasonNoBand          = "no_band"
	ReasonNoAmount        = "no_amount"
	ReasonOutsideValidity = "outside_validity"
	ReasonNoPrimaryItem   = "no_primary_item"
	ReasonUnitMissing     = "unit_missing"
)

// NoPriceError tells which item, unit and night could not be priced. It carries ids and codes only.
type NoPriceError struct {
	ItemID       string        `json:"item_id"`
	ContractType ContractType  `json:"contract_type"`
	Night        calendar.Date `json:"night"`
	Unit         TimeUnit      `json:"unit,omitempty"`
	Category     string        `json:"category,omitempty"`
	Band         string        `json:"band,omitempty"`
	Reason       string        `json:"reason"`
}

func (e *NoPriceError) Error() string {
	return fmt.Sprintf("pricing: no price for item %s (%s) on %s: %s", e.ItemID, e.ContractType, e.Night, e.Reason)
}

// Is makes errors.Is(err, ErrNoPrice) true.
func (e *NoPriceError) Is(target error) bool { return target == ErrNoPrice }

// Codes of an InputError.
const (
	InputDimensionsRequired     = "dimensions_required"
	InputDepartureBeforeArrival = "departure_before_arrival"
	InputTooManyNights          = "too_many_nights"
	InputUnknownItem            = "unknown_item"
	InputContractTypeNotServed  = "contract_type_not_served"
	InputInvalidChannel         = "invalid_channel"
)

// InputError names the invalid input field (snake_case, as in the API) and a stable code.
type InputError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

func (e *InputError) Error() string {
	return fmt.Sprintf("pricing: invalid input %s: %s", e.Field, e.Code)
}

// Is makes errors.Is(err, ErrInvalidInput) true.
func (e *InputError) Is(target error) bool { return target == ErrInvalidInput }
