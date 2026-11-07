package occupancy

import (
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
)

// Kind is an allocation kind (allocations.kind).
type Kind string

// Kinds. M3 produces hold, contract, transit and block; owner (M4) and maintenance (M9) have no producer yet.
const (
	KindHold        Kind = store.AllocKindHold
	KindContract    Kind = store.AllocKindContract
	KindTransit     Kind = store.AllocKindTransit
	KindOwner       Kind = store.AllocKindOwner
	KindBlock       Kind = store.AllocKindBlock
	KindMaintenance Kind = store.AllocKindMaintenance
)

// AllocStatus is an allocation status (m3-plan §3.2): only active allocations have rows.
type AllocStatus string

// Allocation statuses.
const (
	StatusActive     AllocStatus = store.AllocActive
	StatusCancelled  AllocStatus = store.AllocCancelled
	StatusExpired    AllocStatus = store.AllocExpired
	StatusSuperseded AllocStatus = store.AllocSuperseded
)

// FitOverride is the recorded override of blocking fit codes (INV-07): who, why, which codes. Stored as
// allocations.fit_override JSON.
type FitOverride struct {
	UserID   string   `json:"user_id"`              // the user, or the creator of the API key (INV-07)
	APIKeyID string   `json:"api_key_id,omitempty"` // set when an API key gave the override
	Reason   string   `json:"reason"`
	Codes    []string `json:"codes"`
}

// NewFitOverride records who gives a fit override and why: the user, or the API key with its creator (INV-07).
func NewFitOverride(p *auth.Principal, reason string) *FitOverride {
	o := &FitOverride{Reason: reason}
	if p != nil {
		o.UserID = p.UserID
		if p.Kind == auth.KindAPIKey {
			o.APIKeyID = p.APIKeyID
		}
	}
	return o
}

// NewAllocation is the input of AllocateTx (m3-plan §2.3).
type NewAllocation struct {
	ContractLineID *string
	VesselID       *string // nil only for blocks
	Kind           Kind
	Spec           engine.AllocSpec
	ExpiresAt      *time.Time
	FitOverride    *FitOverride
	BlockReason    string
	BlockSize      *layout.Vessel // blocks on dynamic areas: the footprint size (LOA, beam) of the blocked space
	MovedFromID    *string
	UserID         string
	Arrival        bool // true applies closures to Spec.First
}

// Target is where an allocation is moved (a resource and, for dynamic areas, a placement).
type Target struct {
	ResourceType engine.ResourceType
	ResourceID   string
	Placement    *engine.Placement
}

// MoveSpec is the input of MoveTx (m3-plan §3.5).
type MoveSpec struct {
	AllocationID    string
	ExpectedVersion int
	Target          Target
	FromNight       *calendar.Date // default today
	ToNight         *calendar.Date // interval move: the vessel returns to the source after ToNight
	FitOverride     *FitOverride
	Reprice         *bool
	UserID          string
	Operation       string // move, swap, layout (undo log)
}

// LayoutOp places one allocation in the area of a layout save.
type LayoutOp struct {
	AllocationID string
	Placement    engine.Placement
	FitOverride  *FitOverride
}

// BaseVersion is an allocation of the area as the client saw it.
type BaseVersion struct {
	AllocationID string
	Version      int
}

// LayoutSpec is the input of LayoutTx (m3-plan §3.5, §13 D17).
type LayoutSpec struct {
	AreaID    string
	Date      calendar.Date
	FromNight *calendar.Date
	Base      []BaseVersion
	Ops       []LayoutOp
	UserID    string
}

// MoveResult is what a move, swap, layout save or undo wrote.
type MoveResult struct {
	Allocations []Allocation // the new and changed allocations
	UndoID      string
	UndoUntil   time.Time
	PriceEffect *PriceEffect // move price effect (m3-plan §3.5 step 6); nil when nothing was priced
}
