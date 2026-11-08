package yardquote

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// The data source of the yard_quote document kind (m10-plan §4.3): registered with docs by NewService.

type dataSource struct{ s *Service }

func (dataSource) Kinds() []docs.Kind { return []docs.Kind{docs.KindYardQuote} }

func (d dataSource) View(ctx context.Context, tx bun.IDB, scope store.MarinaScope, kind docs.Kind, _ docs.SubjectType, subjectID, locale string) (any, error) {
	r, err := store.GetYardQuote(ctx, tx, scope, subjectID, false)
	if err != nil {
		return nil, notFound(err)
	}
	return d.s.quoteDocView(ctx, tx, scope, r, locale)
}

func (dataSource) DemoView(kind docs.Kind, _ string) (any, error) { return docs.DemoView(kind), nil }

var unitLabels = map[string][2]string{
	"hour": {"ore", "h"}, "piece": {"pz", "pcs"}, "lump": {"a corpo", "lump sum"}, "m2": {"m²", "m²"}, "m3": {"m³", "m³"},
	"night": {"notti", "nights"}, "month": {"mesi", "months"}, "season": {"stagioni", "seasons"}, "year": {"anni", "years"},
}

func unitLabel(u, locale string) string {
	if l, ok := unitLabels[u]; ok {
		if strings.HasPrefix(strings.ToLower(locale), "en") {
			return l[1]
		}
		return l[0]
	}
	return u
}

// formatQty prints a quantity without trailing zeros, with a decimal comma in Italian.
func formatQty(m qty.Milli, locale string) string {
	s := m.String()
	if !strings.HasPrefix(strings.ToLower(locale), "en") {
		s = strings.ReplaceAll(s, ".", ",")
	}
	return s
}

// quoteDocView builds the document view of a quote from its snapshot (the numbers are the stored ones, INV-28).
func (s *Service) quoteDocView(ctx context.Context, db bun.IDB, scope store.MarinaScope, r *store.YardQuote, locale string) (docs.YardQuoteView, error) {
	marina, err := docs.MarinaHeader(ctx, db, scope, s.d.Keys)
	if err != nil {
		return docs.YardQuoteView{}, err
	}
	out := docs.YardQuoteView{
		Marina: marina,
		Quote: docs.YardQuoteInfo{
			Number: Number(r.Year, r.Number), Kind: r.Kind, IssuedOn: r.QuotedOn, ValidUntil: r.ValidUntil, BillingMode: r.BillingMode,
			PlannedStartOn: r.PlannedStartOn, PlannedDeliveryOn: r.PlannedDeliveryOn, Conditions: views.Notes(r.Conditions),
			Notes: views.Notes(r.Notes),
		},
		Lines: []docs.YardQuoteLine{}, VAT: []views.VATRow{}, Hours: []docs.YardQuoteHours{},
	}
	if c, err := store.GetCustomer(ctx, db, scope, r.CustomerID); err == nil {
		out.Customer = docs.PartyOf(c)
	}
	if v, err := store.GetVessel(ctx, db, scope, r.VesselID); err == nil {
		d := vesselDataOf(v)
		out.Vessel = views.Vessel{
			Name: v.Name, Registration: v.Registration, Flag: v.Flag, HullType: v.HullType, LOA: views.Cm(d.LOA), Beam: views.Cm(d.Beam),
			Draft: views.Cm(d.Draft),
		}
	}
	if r.WorkOrderID != "" {
		out.Quote.WorkOrderNumber = s.workOrderNumber(ctx, db, scope, r.WorkOrderID)
	}
	snap := snapshotOf(r.Snapshot)
	lines, totals, err := price.Recompute(snap)
	if err != nil {
		return docs.YardQuoteView{}, err
	}
	for _, l := range lines {
		it := snap.Items[l.Spec.ItemID]
		out.Lines = append(out.Lines, docs.YardQuoteLine{
			Seq: l.Spec.Seq, Description: l.Spec.Description, Unit: unitLabel(it.Unit, locale), Qty: formatQty(l.Qty, locale),
			UnitPrice: l.Unit, Discount: l.Discount, Net: l.Net, VATRate: views.BP(l.VATData.RateBP),
		})
	}
	for _, b := range totals.Buckets {
		out.VAT = append(out.VAT, views.VATRow{RateBP: views.BP(b.RateBP), Nature: b.Nature, Net: b.Net, VAT: b.VAT, Gross: b.Gross})
	}
	quals, err := s.qualMap(ctx, db, scope)
	if err != nil {
		return docs.YardQuoteView{}, err
	}
	qids := make([]string, 0, len(totals.EstimatedHours))
	for q := range totals.EstimatedHours {
		qids = append(qids, q)
	}
	sort.Strings(qids)
	for _, q := range qids {
		out.Hours = append(out.Hours, docs.YardQuoteHours{
			Qualification: localized(i18nOf(quals[q].Name), locale), Hours: formatQty(totals.EstimatedHours[q], locale),
		})
	}
	out.Net, out.VATTotal, out.Total, out.Deposit = totals.Net, totals.VAT, totals.Gross, totals.Deposit
	return out, nil
}

// workOrderNumber reads the display number of a work order (C<year>-NNNNNN); "" when unknown.
func (s *Service) workOrderNumber(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) string {
	year, n, err := store.YardWorkOrderNumber(ctx, db, scope, id)
	if err != nil || n == 0 {
		return ""
	}
	return fmt.Sprintf("C%d-%06d", year, n)
}
