package occupancy

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
)

// Allocation is an allocation as the API shows it.
type Allocation struct {
	ID             string
	ContractLineID string
	ContractID     string
	VesselID       string
	ResourceType   engine.ResourceType
	ResourceID     string
	ResourceCode   string
	MapID          string
	Placement      *engine.Placement
	First, Last    calendar.Date
	Kind           Kind
	Status         AllocStatus
	ExpiresAt      *time.Time
	FitOverride    *FitOverride
	BlockReason    string
	MovedFromID    string
	Released       []engine.NightRange
	Version        int
}

// AllocationDetail is an allocation with its contract summary, releases and moved-from chain.
type AllocationDetail struct {
	Allocation
	Contract     *ContractBrief
	Releases     []Release
	MovedFromIDs []string // predecessors, nearest first
}

// ContractBrief is the contract of an allocation as embedded in occupancy views.
type ContractBrief struct {
	ID, Kind, Term, Status, Code string
}

// contractCode renders a contract number as ORM-2026-0001 (mooring) or TRN-2026-0001 (transit).
func contractCode(kind string, year int, number int64) string {
	prefix := "ORM"
	switch kind {
	case store.ContractKindTransit:
		prefix = "TRN"
	case store.ContractKindMooring:
	default:
		prefix = "CTR"
	}
	return fmt.Sprintf("%s-%04d-%04d", prefix, year, number)
}

func briefOf(id, kind, term, status string, year int, number int64) *ContractBrief {
	if id == "" {
		return nil
	}
	return &ContractBrief{ID: id, Kind: kind, Term: term, Status: status, Code: contractCode(kind, year, number)}
}

// Release is a release as the API shows it.
type Release struct {
	ID                 string
	AllocationID       string
	ContractID         string
	ResourceType       engine.ResourceType
	ResourceID         string
	ResourceCode       string
	From, To           calendar.Date
	Status             string
	Source             string
	ResoldAllocationID string
	ResoldNights       int
	CreditCents        int64
	Notes              *string // only with customers.read
	RevokedAt          *time.Time
	CreatedAt          time.Time
	Version            int
}

func releaseOf(r *store.Release, code string, withNotes bool) Release {
	out := Release{
		ID: r.ID, AllocationID: r.AllocationID, ContractID: r.ContractID, ResourceType: engine.ResourceType(r.ResourceType),
		ResourceID: r.ResourceID, ResourceCode: code, From: r.FromNight, To: r.ToNight, Status: r.Status, Source: r.Source,
		ResoldAllocationID: r.ResoldAllocationID, ResoldNights: int(r.ResoldNights), CreditCents: r.CreditCents,
		CreatedAt: r.CreatedAt, Version: r.Version,
	}
	if withNotes {
		n := r.Notes
		out.Notes = &n
	}
	if !r.RevokedAt.IsZero() {
		t := r.RevokedAt
		out.RevokedAt = &t
	}
	return out
}

// allocationOf converts a stored allocation (releases: its active releases).
func allocationOf(a *store.Allocation, code, contractID string, releases []store.Release) Allocation {
	out := Allocation{
		ID: a.ID, ContractLineID: a.ContractLineID, ContractID: contractID, VesselID: a.VesselID,
		ResourceType: engine.ResourceType(a.ResourceType), ResourceID: a.ResourceID, ResourceCode: code, MapID: a.MapID,
		Placement: placementOf(a), First: a.FirstNight, Last: a.LastNight, Kind: Kind(a.Kind), Status: AllocStatus(a.Status),
		BlockReason: a.BlockReason, MovedFromID: a.MovedFromID, Version: a.Version, Released: []engine.NightRange{},
	}
	if !a.ExpiresAt.IsZero() {
		t := a.ExpiresAt
		out.ExpiresAt = &t
	}
	if len(a.FitOverride) > 0 && string(a.FitOverride) != "null" {
		o := unmarshalJSON[FitOverride](a.FitOverride)
		out.FitOverride = &o
	}
	for _, r := range releases {
		if r.AllocationID == a.ID && r.Status == store.ReleaseActive {
			out.Released = append(out.Released, engine.NightRange{From: r.FromNight, To: r.ToNight})
		}
	}
	return out
}

// allocationsOf converts allocations with their resource codes, contracts and releases (three queries).
func allocationsOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, as []store.Allocation) ([]Allocation, error) {
	ids := make([]string, len(as))
	res := make([]string, len(as))
	for i := range as {
		ids[i], res[i] = as[i].ID, as[i].ResourceID
	}
	refs, err := store.ResourceRefs(ctx, db, scope, res)
	if err != nil {
		return nil, err
	}
	contracts, err := store.ContractsOfAllocations(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	rels, err := store.ActiveReleasesOf(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Allocation, len(as))
	for i := range as {
		out[i] = allocationOf(&as[i], refs[as[i].ResourceID].Code, contracts[as[i].ID], rels)
	}
	return out, nil
}
