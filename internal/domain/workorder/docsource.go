package workorder

import (
	"context"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// The data source of the yard_work_order document kind (the work sheet of an order): registered with docs by the
// constructor (NewService) when documents are configured.

type dataSource struct{ s *Service }

func moneyOf(c int64) money.Cents { return money.Cents(c) }

func (dataSource) Kinds() []docs.Kind { return []docs.Kind{docs.KindYardWorkOrder} }

func (d dataSource) View(ctx context.Context, tx bun.IDB, scope store.MarinaScope, _ docs.Kind, _ docs.SubjectType, subjectID, locale string) (any, error) {
	o, err := store.GetWorkOrder(ctx, tx, scope, subjectID)
	if err != nil {
		return nil, notFound(err)
	}
	return d.s.docView(ctx, tx, scope, o, locale)
}

func (dataSource) DemoView(kind docs.Kind, _ string) (any, error) { return docs.DemoView(kind), nil }

func fmtQty(m qty.Milli, locale string) string {
	s := m.String()
	if !strings.HasPrefix(strings.ToLower(locale), "en") {
		s = strings.ReplaceAll(s, ".", ",")
	}
	return s
}

func (s *Service) docView(ctx context.Context, db bun.IDB, scope store.MarinaScope, o *store.WorkOrder, locale string) (docs.YardWorkOrderView, error) {
	marina, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys)
	if err != nil {
		return docs.YardWorkOrderView{}, err
	}
	out := docs.YardWorkOrderView{
		Marina: marina,
		Order: docs.YardWorkOrderInfo{
			Number: FormatNumber(o.Year, o.Number), Status: o.Status, BillingMode: o.BillingMode, PlannedStartOn: o.PlannedStartOn,
			PlannedDeliveryOn: o.PlannedDeliveryOn, Position: o.PositionNote, Completion: views.BP(o.CompletionBP), Notes: views.Notes(o.Notes),
		},
		Lines: []docs.YardWorkOrderLine{}, Tasks: []docs.YardWorkOrderTask{},
	}
	if c, err := store.GetCustomer(ctx, db, scope, o.CustomerID); err == nil {
		out.Customer = docs.PartyOf(c)
	}
	if v, err := store.GetVessel(ctx, db, scope, o.VesselID); err == nil {
		out.Vessel = views.Vessel{
			Name: v.Name, Registration: v.Registration, Flag: v.Flag, HullType: v.HullType, LOA: views.Cm(v.LOACm), Beam: views.Cm(v.BeamCm),
			Draft: views.Cm(v.DraftCm),
		}
	}
	if o.QuoteID != "" {
		var q store.YardQuote
		if err := db.NewSelect().Model(&q).Where("marina_id = ?", scope.ID()).Where("id = ?", o.QuoteID).Scan(ctx); err == nil {
			out.Order.QuoteNumber = yardquote.Number(q.Year, q.Number)
		}
	}
	if o.AllocationID != "" {
		if l, err := store.WOAllocationLabels(ctx, db, scope, []string{o.AllocationID}); err == nil && l[o.AllocationID].Code != "" {
			out.Order.Position = strings.TrimSpace(l[o.AllocationID].Code + " " + o.PositionNote)
		}
	}
	lines, err := store.WorkOrderLines(ctx, db, scope, []string{o.ID})
	if err != nil {
		return out, err
	}
	for _, l := range lines {
		out.Lines = append(out.Lines, docs.YardWorkOrderLine{
			Seq: l.Seq, Description: l.Description, Qty: fmtQty(qty.Milli(l.QtyMilli), locale), UnitPrice: moneyOf(l.UnitCents), Net: moneyOf(l.NetCents),
		})
		out.Net += moneyOf(l.NetCents)
	}
	tasks, err := store.WorkTasksOfOrders(ctx, db, scope, []string{o.ID})
	if err != nil {
		return out, err
	}
	for _, t := range tasks {
		if t.Status == store.WorkTaskCancelled {
			continue
		}
		var cl []string
		for _, c := range ChecklistOf(t.Checklist) {
			cl = append(cl, c.Label)
		}
		if cl == nil {
			cl = []string{}
		}
		out.Tasks = append(out.Tasks, docs.YardWorkOrderTask{
			Seq: t.Seq, Title: t.Title, EstimatedHours: fmtQty(qty.Milli(t.EstimatedHoursMilli), locale), Progress: views.BP(t.ProgressBP),
			Status: t.Status, Checklist: cl,
		})
	}
	if av, err := s.accountView(ctx, db, scope, o); err == nil {
		out.Revenue, out.Charged = av.Account.Totals.Revenue, av.ChargedGross
	}
	return out, nil
}
