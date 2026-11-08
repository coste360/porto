package yardquote

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/store"
)

// AcceptedQuote is what the order side receives when a quote is accepted, inside the accept transaction (quote row
// locked and already set to accepted).
type AcceptedQuote struct {
	Quote      store.YardQuote
	Lines      []store.YardQuoteLine
	Snapshot   price.Snapshot
	AcceptedAt time.Time
	Via        string // panel, link
}

// OrderRef identifies the work order created (or changed by a variation) by an acceptance.
type OrderRef struct {
	WorkOrderID     string
	Number          string // C<year>-NNNNNN
	AdvanceChargeID string // advance billing charge, "" when none (deposit 0 or advance_on_accept off)
}

// OrderCreator creates the work order of an accepted job quote, or applies an accepted variation quote to its
// order, in the caller's transaction (m10-plan §4.3, INV-96: work_orders.quote_id and
// work_order_variations.quote_id are unique). Implemented by workorder.Service and wired in internal/app, so that
// yardquote never imports workorder. A failure rolls back the acceptance.
type OrderCreator interface {
	CreateFromQuoteTx(ctx context.Context, tx bun.Tx, scope store.MarinaScope, actor audit.Actor, q AcceptedQuote) (OrderRef, error)
	ApplyVariationTx(ctx context.Context, tx bun.Tx, scope store.MarinaScope, actor audit.Actor, q AcceptedQuote) (OrderRef, error)
}

// PriceOrderLinesTx prices the catalogue selections of a work order created without a quote with the quote line
// engine (same snapshot building and price.Compute as a quote, no deposit), in the caller's transaction. It
// implements workorder.LinePricer, wired in internal/app.
func (s *Service) PriceOrderLinesTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, customerID, vesselID string,
	on calendar.Date, selections []Selection, jobInputs map[string]Value,
) ([]price.Line, price.Snapshot, error) {
	zero := int64(0)
	b, err := s.build(ctx, tx, scope, buildInput{
		CustomerID: customerID, VesselID: vesselID, Selections: selections, JobInputs: jobInputs, QuotedOn: on,
		DepositBP: &zero,
	})
	if err != nil {
		return nil, price.Snapshot{}, err
	}
	return b.lines, b.snap, nil
}
