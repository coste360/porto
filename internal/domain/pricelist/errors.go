package pricelist

import (
	"errors"
	"fmt"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
)

// Sentinel errors (m2-plan §4.6), mapped to problem types in internal/http/respond/errmap.go.
var (
	// ErrImmutable means the list is active or archived: only drafts change (INV-22).
	ErrImmutable = errors.New("pricelist: price list immutable")
	// ErrOverlap means activation would leave two active lists for one night and contract type (INV-24); the
	// error is an *OverlapError.
	ErrOverlap = errors.New("pricelist: price list overlap")
	// ErrInvalid means the list has blocking validation issues; the error is an *InvalidError.
	ErrInvalid = errors.New("pricelist: price list invalid")
	// ErrVersionMismatch means If-Match did not match the current version.
	ErrVersionMismatch = errors.New("pricelist: version mismatch")
	// ErrNoActiveList means no active list serves the contract type on the arrival night (no_price, reason
	// no_active_list).
	ErrNoActiveList = errors.New("pricelist: no active price list")
)

// ReasonNoActiveList is the no_price reason of ErrNoActiveList.
const ReasonNoActiveList = "no_active_list"

// Conflict is an active list that blocks an activation.
type Conflict struct {
	ID        string        `json:"id"`
	Code      string        `json:"code"`
	ValidFrom calendar.Date `json:"valid_from"`
	ValidTo   calendar.Date `json:"valid_to"`
}

// OverlapError lists the active lists an activation would overlap.
type OverlapError struct {
	Conflicts []Conflict
}

func (e *OverlapError) Error() string {
	return fmt.Sprintf("pricelist: overlaps %d active price lists", len(e.Conflicts))
}

// Is makes errors.Is(err, ErrOverlap) true.
func (e *OverlapError) Is(target error) bool { return target == ErrOverlap }

// InvalidError carries the issues that block an activation.
type InvalidError struct {
	Issues []pricing.Issue
}

func (e *InvalidError) Error() string {
	return fmt.Sprintf("pricelist: %d blocking issues", len(e.Issues))
}

// Is makes errors.Is(err, ErrInvalid) true.
func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }
