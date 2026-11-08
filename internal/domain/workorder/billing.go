package workorder

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/workorder/account"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// The final account (m10-plan §9) and the billing through the M5 charges (§10): every billing charges parts of the
// remainder of a target per plan key (chargeplan), each with at most one primary charge whose origin is the billing
// (INV-94); exported charges are never edited, corrections are linked adjustment charges (INV-51).

// accountData is what the account and the billing of an order read.
type accountData struct {
	in    account.Input
	items map[string]store.Item
	desc  map[string]string
	lines []store.WorkOrderLine
}

func itemName(it store.Item) string {
	var m map[string]string
	_ = json.Unmarshal(it.Name, &m)
	if n := i18nPick(m); n != "" {
		return n
	}
	return it.Code
}

// loadAccount reads the inputs of the account of an order.
func (s *Service) loadAccount(ctx context.Context, db bun.IDB, scope store.MarinaScope, o *store.WorkOrder) (*accountData, error) {
	set, err := s.settings(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	lines, err := store.WorkOrderLines(ctx, db, scope, []string{o.ID})
	if err != nil {
		return nil, err
	}
	entries, err := store.ApprovedEntriesOfOrders(ctx, db, scope, []string{o.ID})
	if err != nil {
		return nil, err
	}
	moves, err := store.StockMovementsOfOrders(ctx, db, scope, []string{o.ID})
	if err != nil {
		return nil, err
	}
	exts, err := store.ExternalCostsOfOrders(ctx, db, scope, []string{o.ID})
	if err != nil {
		return nil, err
	}
	tasks, err := store.WorkTasksOfOrders(ctx, db, scope, []string{o.ID})
	if err != nil {
		return nil, err
	}
	r := rateSnapshotOf(o.RateSnapshot)
	ids := []string{}
	for _, l := range lines {
		ids = append(ids, l.ItemID)
	}
	for _, it := range r.LabourItems {
		ids = append(ids, it)
	}
	for _, m := range moves {
		ids = append(ids, m.ItemID)
	}
	for _, x := range exts {
		if x.ItemID != "" {
			ids = append(ids, x.ItemID)
		}
	}
	its, err := store.ItemsByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	items := map[string]store.Item{}
	vat := map[string]account.VAT{}
	for _, it := range its {
		items[it.ID] = it
		vat[it.ID] = account.VAT{RateBP: it.VATRateBP, Nature: it.VATNature, PriceIncludesVAT: it.PriceIncludesVAT}
	}
	costs, err := store.WOItemCosts(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	src := account.Source{
		Mode: o.BillingMode, FixedIncludesExternal: set.FixedIncludesExternal, OrderWorkType: o.WorkTypeID,
		Units: r.Units, LabourItems: r.LabourItems, SnapshotVAT: r.VAT, ItemType: map[string]string{}, ItemVAT: vat,
		ItemCost: map[string]money.Cents{}, TaskWorkType: map[string]string{}, EmployeeQuals: map[string][]string{},
	}
	for id, it := range items {
		src.ItemType[id] = it.ItemType
	}
	for id, c := range costs {
		src.ItemCost[id] = money.Cents(c)
	}
	desc := map[string]string{}
	for _, l := range lines {
		src.Lines = append(src.Lines, account.BudgetLine{
			LineID: l.ID, ItemID: l.ItemID, WorkTypeID: l.WorkTypeID, QualificationID: l.QualificationID, Qty: qty.Milli(l.QtyMilli),
			Unit: money.Cents(l.UnitCents), Discount: money.Cents(l.DiscountCents), Net: money.Cents(l.NetCents),
			VAT: money.Cents(l.VATCents), Gross: money.Cents(l.GrossCents), UnitCost: money.Cents(l.UnitCostCents),
			ConsumedQty: (*qty.Milli)(l.ConsumedQtyMilli),
		})
		k := account.Key(l.ItemID, orOr(l.WorkTypeID, o.WorkTypeID))
		if _, ok := desc[k]; !ok && l.Description != "" {
			desc[k] = l.Description
		}
	}
	for _, t := range tasks {
		src.TaskWorkType[t.ID] = t.WorkTypeID
	}
	for _, e := range entries {
		if e.QualificationID == "" {
			if _, ok := src.EmployeeQuals[e.EmployeeID]; !ok {
				qs, err := store.WOEmployeeQualifications(ctx, db, scope, e.EmployeeID)
				if err != nil {
					return nil, err
				}
				src.EmployeeQuals[e.EmployeeID] = qs
			}
		}
		src.Entries = append(src.Entries, account.SourceEntry{
			ID: e.ID, EmployeeID: e.EmployeeID, TaskID: e.TaskID, QualificationID: e.QualificationID, WorkTypeID: e.WorkTypeID,
			Hours: qty.Milli(e.HoursMilli), CostCents: money.Cents(e.CostCents), Late: e.Late,
		})
	}
	for _, m := range moves {
		src.Movements = append(src.Movements, account.SourceMovement{
			ID: m.ID, TaskID: m.TaskID, ItemID: m.ItemID, Return: m.Kind == store.StockReturn, Qty: qty.Milli(m.QtyMilli),
			UnitCost: money.Cents(m.UnitCostCents), UnitPrice: money.Cents(m.UnitPriceCents), Late: m.Late,
		})
	}
	for _, x := range exts {
		src.External = append(src.External, account.ExternalCost{
			ID: x.ID, ItemID: x.ItemID, WorkTypeID: x.WorkTypeID, Cost: money.Cents(x.CostCents), Billable: x.Billable,
			Price: money.Cents(x.PriceCents), Late: x.Late,
		})
		d := x.Description
		if d == "" {
			d = itemName(items[x.ItemID])
		}
		desc[account.ExternalKey(x.ID)] = d
	}
	in := account.Assemble(src)
	return &accountData{in: in, items: items, desc: desc, lines: lines}, nil
}

func orOr(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

// chargedLines returns the lines of the live charges of an order as chargeplan charged lines.
func chargedLines(ctx context.Context, db bun.IDB, scope store.MarinaScope, orderID string) ([]chargeplan.ChargedLine, []store.Charge, error) {
	live, err := store.LiveChargesOfWorkOrder(ctx, db, scope, orderID)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(live))
	for _, c := range live {
		ids = append(ids, c.ID)
	}
	rows, err := store.ChargeLinesOf(ctx, db, scope, ids)
	if err != nil {
		return nil, nil, err
	}
	out := make([]chargeplan.ChargedLine, 0, len(rows))
	for _, l := range rows {
		cl := chargeplan.Line{
			Key: l.PlanKey, ItemID: l.ItemID, ItemCode: l.ItemCode, Description: l.Description, RevenueClass: l.RevenueClassCode,
			ExternalCode: l.ExternalCode, VATRateBP: l.VATRateBP, VATNature: l.VATNature, PriceIncludesVAT: l.PriceIncludesVAT,
		}
		out = append(out, chargeplan.ChargedLine{Key: l.PlanKey, GrossCents: money.Cents(l.GrossCents), VATCents: money.Cents(l.VATCents), Line: &cl})
	}
	return out, live, nil
}

func buckets(lines []account.Line) []VATBucket {
	type key struct {
		rate   int64
		nature string
	}
	m := map[key]*VATBucket{}
	for _, l := range lines {
		if l.Revenue == 0 && l.VATCents == 0 {
			continue
		}
		k := key{l.VAT.RateBP, l.VAT.Nature}
		b := m[k]
		if b == nil {
			b = &VATBucket{RateBP: k.rate, Nature: k.nature}
			m[k] = b
		}
		b.Net += l.Revenue
		b.VAT += l.VATCents
		b.Gross += l.Gross
	}
	out := make([]VATBucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	slices.SortFunc(out, func(a, b VATBucket) int {
		if a.RateBP != b.RateBP {
			if a.RateBP > b.RateBP {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Nature, b.Nature)
	})
	return out
}

// Account returns the final account of an order (yard.read or yard.manage): lines, totals, margins per work type,
// comparison with the quote, what is charged and what remains.
func (s *Service) Account(ctx context.Context, p *auth.Principal, marinaID, orderID string) (*AccountView, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	o, err := store.GetWorkOrder(ctx, db, scope, orderID)
	if err != nil {
		return nil, notFound(err)
	}
	return s.accountView(ctx, db, scope, o)
}

func (s *Service) accountView(ctx context.Context, db bun.IDB, scope store.MarinaScope, o *store.WorkOrder) (*AccountView, error) {
	data, err := s.loadAccount(ctx, db, scope, o)
	if err != nil {
		return nil, err
	}
	acc, err := account.Build(data.in)
	if err != nil {
		return nil, err
	}
	charged, _, err := chargedLines(ctx, db, scope, o.ID)
	if err != nil {
		return nil, err
	}
	var cg money.Cents
	for _, c := range charged {
		cg += c.GrossCents
	}
	for _, l := range acc.Lines {
		if _, ok := data.desc[l.Key]; !ok && l.ItemID != "" {
			data.desc[l.Key] = itemName(data.items[l.ItemID])
		}
	}
	return &AccountView{
		Order: *o, Account: acc, Buckets: buckets(acc.Lines), ChargedGross: cg, RemainingGross: account.TargetGross(acc.Target) - cg,
		Items: data.items, Descriptions: data.desc,
	}, nil
}

// ListBillings lists the billings of an order with their charge numbers (yard.read or yard.manage).
func (s *Service) ListBillings(ctx context.Context, p *auth.Principal, marinaID, orderID string) ([]BillingView, money.Cents, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, 0, err
	}
	db := s.d.DB.DB
	if _, err := store.GetWorkOrder(ctx, db, scope, orderID); err != nil {
		return nil, 0, notFound(err)
	}
	rows, err := store.WOBillingsOfOrder(ctx, db, scope, orderID)
	if err != nil {
		return nil, 0, err
	}
	var ids []string
	for _, b := range rows {
		if b.ChargeID != "" {
			ids = append(ids, b.ChargeID)
		}
	}
	charges, err := store.ChargesByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, 0, err
	}
	nums := map[string]string{}
	for _, c := range charges {
		nums[c.ID] = c.Number
	}
	_, live, err := chargedLines(ctx, db, scope, orderID)
	if err != nil {
		return nil, 0, err
	}
	var cg money.Cents
	for _, c := range live {
		cg += money.Cents(c.TotalCents)
	}
	out := make([]BillingView, 0, len(rows))
	for _, b := range rows {
		out = append(out, billingView(b, nums[b.ChargeID]))
	}
	return out, cg, nil
}

type requested struct {
	PercentBP *int64       `json:"percent_bp,omitempty"`
	Amount    *money.Cents `json:"amount_cents,omitempty"`
}

func billingView(b store.WorkOrderBilling, number string) BillingView {
	var r requested
	_ = json.Unmarshal(b.Requested, &r)
	return BillingView{Row: b, ChargeNumber: number, PercentBP: r.PercentBP, Amount: r.Amount}
}

// CreateBilling bills progress, final or adjustment through the M5 charges (charges.manage; a negative remainder
// needs refunds.issue). With dryRun it returns the preview and writes nothing.
func (s *Service) CreateBilling(ctx context.Context, p *auth.Principal, marinaID, orderID string, in BillingInput, dryRun bool) (*BillingResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	switch in.Kind {
	case BillingProgress, BillingFinal, BillingAdjustment:
	default:
		return nil, invalid("kind", "invalid")
	}
	if in.Kind != BillingProgress && (in.Amount != nil || in.PercentBP != nil) {
		return nil, invalid("amount_cents", "not_allowed")
	}
	if in.Amount != nil && in.PercentBP != nil {
		return nil, invalid("amount_cents", "exclusive")
	}
	if in.PercentBP != nil && (*in.PercentBP <= 0 || *in.PercentBP > 10000) {
		return nil, invalid("percent_bp", "invalid")
	}
	if in.Amount != nil && *in.Amount <= 0 {
		return nil, invalid("amount_cents", "invalid")
	}
	canRefund := p.Can(scope.OrgID(), scope.ID(), auth.PermRefundsIssue)
	var res *BillingResult
	run := func(ctx context.Context, tx bun.Tx) error {
		lock := store.LockWorkOrder
		if dryRun {
			lock = store.GetWorkOrder // the preview runs in a read-only transaction
		}
		o, err := lock(ctx, tx, scope, orderID)
		if err != nil {
			return notFound(err)
		}
		res, err = s.billTx(ctx, tx, scope, p.Actor(), o, in, dryRun, canRefund)
		return err
	}
	if dryRun {
		err = s.d.DB.ReadTx(ctx, run)
	} else {
		err = s.d.DB.RunInTx(ctx, run)
	}
	return res, err
}

// billTx plans and (unless dryRun) writes one billing of a locked order.
func (s *Service) billTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, o *store.WorkOrder, in BillingInput,
	dryRun, canRefund bool,
) (*BillingResult, error) {
	data, err := s.loadAccount(ctx, tx, scope, o)
	if err != nil {
		return nil, err
	}
	charged, live, err := chargedLines(ctx, tx, scope, o.ID)
	if err != nil {
		return nil, err
	}
	var chargedGross money.Cents
	for _, c := range charged {
		chargedGross += c.GrossCents
	}
	budget := account.BudgetTarget(data.in.Budget, data.in.ItemVAT, o.WorkTypeID)
	var parts []chargeplan.Part
	switch in.Kind {
	case BillingAdvance:
		if parts, err = account.Partial(budget, charged, *in.Amount); err != nil {
			return nil, ErrNothingToBill
		}
	case BillingProgress:
		if !Open(o.Status) && o.Status != store.WorkOrderDelivered {
			return nil, ErrInvalidTransition
		}
		acc, err := account.Build(data.in)
		if err != nil {
			return nil, err
		}
		var amount money.Cents
		switch {
		case in.Amount != nil:
			amount = *in.Amount
		case in.PercentBP != nil:
			amount = account.TargetGross(budget).PercentBP(*in.PercentBP) - chargedGross
		case o.BillingMode == store.BillingModeProgress:
			amount = account.TargetGross(budget).PercentBP(int64(o.CompletionBP)) - chargedGross
		default:
			return nil, invalid("amount_cents", "required")
		}
		if amount <= 0 {
			return nil, ErrNothingToBill
		}
		rem := chargeplan.Remaining(account.PlanTarget(acc.Target), charged)
		if amount > account.PositiveRemaining(rem) {
			if account.PositiveRemaining(rem) <= 0 {
				return nil, ErrNothingToBill
			}
			return nil, invalid("amount_cents", "above_remaining")
		}
		if parts, err = account.Partial(acc.Target, charged, amount); err != nil {
			return nil, err
		}
	case BillingFinal, BillingAdjustment:
		if in.Kind == BillingFinal && !Billable(o.Status) {
			return nil, ErrInvalidTransition
		}
		if in.Kind == BillingAdjustment && o.Status != store.WorkOrderInvoiced {
			return nil, ErrInvalidTransition
		}
		acc, err := account.Build(data.in)
		if err != nil {
			return nil, err
		}
		parts = account.Final(acc.Target, charged)
	}
	gross := account.SumParts(parts)
	res := &BillingResult{DryRun: dryRun, Kind: in.Kind, Gross: gross, Lines: make([]BillingLine, 0, len(parts))}
	for _, pt := range parts {
		res.Lines = append(res.Lines, BillingLine{
			PlanKey: pt.Line.Key, Description: s.partDescription(data, pt), Net: pt.Net, VAT: pt.VAT, Gross: pt.Gross, VATRateBP: pt.Line.VATRateBP,
		})
	}
	if in.Kind == BillingAdjustment && gross == 0 {
		return nil, ErrNothingToBill
	}
	var corrects string
	if (in.Kind == BillingAdjustment || gross < 0) && len(live) > 0 {
		corrects = live[len(live)-1].ID
		res.CorrectsChargeID = corrects
	}
	if gross < 0 && !canRefund {
		return nil, auth.ErrForbidden
	}
	if dryRun {
		return res, nil
	}
	seq, err := store.NextWOBillingSeq(ctx, tx, scope, o.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	b := &store.WorkOrderBilling{
		ID: store.NewID(), WorkOrderID: o.ID, Seq: seq, Kind: in.Kind, Status: store.WOBillingSkippedZero,
		CreatedByUserID: actorUser(actor), CreatedAt: now,
	}
	b.Requested, _ = json.Marshal(requested{PercentBP: in.PercentBP, Amount: in.Amount})
	if gross != 0 && len(parts) > 0 {
		lines, err := s.chargeLines(ctx, tx, scope, data, parts)
		if err != nil {
			return nil, err
		}
		ci := charge.CreateInput{
			OriginType: store.ChargeOriginWorkOrder, OriginID: b.ID, CustomerID: o.CustomerID, VesselID: o.VesselID, DueOn: in.DueOn,
			Description: billingDescription(o, in.Kind), Lines: lines, CorrectsChargeID: corrects,
		}
		if corrects != "" {
			ci.CorrectionKind = "delta"
		}
		c, _, err := s.d.Charges.CreateTx(ctx, tx, scope, actor, ci)
		if err != nil {
			return nil, err
		}
		if c != nil {
			b.Status, b.ChargeID, b.GrossCents = store.WOBillingCharged, c.ID, c.TotalCents
			res.ChargeID = c.ID
		}
	}
	if err := store.InsertWOBilling(ctx, tx, scope, b); err != nil {
		return nil, err
	}
	if in.Kind == BillingFinal {
		from := o.Status
		o.Status, o.InvoicedAt = store.WorkOrderInvoiced, now
		if err := store.UpdateWorkOrder(ctx, tx, scope, o, o.Version, now, "status", "invoiced_at"); err != nil {
			return nil, err
		}
		if err := s.statusChanged(ctx, tx, scope, o.ID, from, o.Status); err != nil {
			return nil, err
		}
	}
	if err := s.audit(ctx, tx, scope, actor, "work_order.bill", "work_order", o.ID, map[string]any{
		"billing_id": b.ID, "kind": b.Kind, "status": b.Status, "charge_id": b.ChargeID, "gross_cents": b.GrossCents,
		"corrects_charge_id": corrects,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, scope, event.WorkOrderBilled, event.WorkOrderBilledData{
		WorkOrderID: o.ID, BillingID: b.ID, Kind: b.Kind, ChargeID: b.ChargeID, GrossCents: b.GrossCents,
	}); err != nil {
		return nil, err
	}
	v := billingView(*b, "")
	res.Billing = &v
	return res, nil
}

func billingDescription(o *store.WorkOrder, kind string) string {
	label := map[string]string{
		BillingAdvance: "acconto", BillingProgress: "stato avanzamento", BillingFinal: "saldo", BillingAdjustment: "conguaglio",
	}[kind]
	return "Commessa " + FormatNumber(o.Year, o.Number) + " – " + label
}

func (s *Service) partDescription(data *accountData, p chargeplan.Part) string {
	if d := data.desc[p.Line.Key]; d != "" {
		return clip(d, 500)
	}
	if it, ok := data.items[p.Line.ItemID]; ok {
		return clip(itemName(it), 500)
	}
	if p.Line.Description != "" {
		return p.Line.Description
	}
	return p.Line.Key
}

// chargeLines turns billing parts into charge lines with the item data (code, revenue class, external code).
func (s *Service) chargeLines(ctx context.Context, tx bun.IDB, scope store.MarinaScope, data *accountData, parts []chargeplan.Part) ([]charge.LineInput, error) {
	var classIDs []string
	for _, it := range data.items {
		if it.RevenueClassID != "" {
			classIDs = append(classIDs, it.RevenueClassID)
		}
	}
	classes, err := store.WORevenueClassCodes(ctx, tx, scope, classIDs)
	if err != nil {
		return nil, err
	}
	out := make([]charge.LineInput, 0, len(parts))
	for _, p := range parts {
		it := data.items[p.Line.ItemID]
		code, class, ext := p.Line.ItemCode, p.Line.RevenueClass, p.Line.ExternalCode
		if it.ID != "" {
			code, class, ext = it.Code, classes[it.RevenueClassID], it.ExternalCode
		}
		out = append(out, charge.LineInput{
			ItemID: p.Line.ItemID, ItemCode: code, Description: s.partDescription(data, p), Qty: p.Qty, Unit: p.Unit,
			Net: p.Net, VAT: p.VAT, Gross: p.Gross, VATRateBP: p.Line.VATRateBP, VATNature: p.Line.VATNature,
			PriceIncludesVAT: p.Line.PriceIncludesVAT, RevenueClassCode: class, ExternalCode: ext, PlanKey: p.Line.Key,
		})
	}
	return out, nil
}
