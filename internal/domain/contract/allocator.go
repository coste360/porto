package contract

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// allocator is the subset of the occupancy tx primitives the contract service uses (m3-plan §2.3, §11.3). It is
// satisfied by *occupancy.Service; tests may substitute a fake.
type allocator interface {
	AllocateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, a occupancy.NewAllocation) (*store.Allocation, error)
	ConvertTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, allocIDs []string, kind occupancy.Kind, expiresAt *time.Time) error
	TruncateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, allocID string, fromNight calendar.Date) error
	CancelTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, allocID string, status occupancy.AllocStatus) error
	ResizeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time, allocID string, first, last calendar.Date) error
	ExpireHoldsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, now time.Time) ([]store.Allocation, error)
}

var _ allocator = (*occupancy.Service)(nil)
