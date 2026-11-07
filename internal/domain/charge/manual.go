package charge

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/customer"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Staff operations on charges (m5-plan §3.5, §7.3.1): manual charges and counter sales on account, edit in place,
// cancel, adjust (delta, replace), invoice reference, termination charge.

// ItemLine is a staff line: an item, a quantity and optionally a unit price (default: the active price list) and
// a discount, both in the item's price terms (gross when the item price includes VAT).
type ItemLine struct {
	ItemID      string
	Qty         qty.Milli
	Unit        *money.Cents
	Discount    money.Cents
	Description string
	ServiceFrom calendar.Date
	ServiceTo   calendar.Date
}

// ManualInput is a manual charge or a counter sale on account (POST charges).
type ManualInput struct {
	OriginType  string // manual, counter_sale
	CustomerID  string
	ContractID  string
	VesselID    string
	DueOn       calendar.Date
	Description string
	FiscalMode  string // counter sales without a customer: receipt
	Lines       []ItemLine
}

// Create creates a manual charge or a counter sale (charges.manage; counter sales also payments.collect).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in ManualInput) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ch, err := s.CreateManualTx(ctx, tx, scope, p.Actor(), in)
		if err != nil {
			return err
		}
		id = ch.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// CreateManualTx prices the item lines and creates the charge in tx (also used by cash counter sales).
func (s *Service) CreateManualTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, in ManualInput) (*store.Charge, error) {
	if in.OriginType != store.ChargeOriginManual && in.OriginType != store.ChargeOriginCounterSale {
		return nil, invalid("origin_type", "invalid")
	}
	if err := s.checkRefs(ctx, tx, scope, in.CustomerID, in.ContractID, in.VesselID); err != nil {
		return nil, err
	}
	lines, err := s.PriceLines(ctx, tx, scope, in.Lines, false)
	if err != nil {
		return nil, err
	}
	ch, _, err := s.CreateTx(ctx, tx, scope, actor, CreateInput{
		OriginType: in.OriginType, CustomerID: in.CustomerID, ContractID: in.ContractID, VesselID: in.VesselID,
		DueOn: in.DueOn, Description: strings.TrimSpace(in.Description), FiscalMode: in.FiscalMode, Lines: lines,
	})
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, invalid("lines", "zero_total")
	}
	return ch, nil
}

// checkRefs verifies that the referenced customer, contract and vessel belong to the marina (404 otherwise).
func (s *Service) checkRefs(ctx context.Context, db bun.IDB, scope store.MarinaScope, customerID, contractID, vesselID string) error {
	if customerID != "" {
		cs, err := store.CustomersByIDs(ctx, db, scope, []string{customerID})
		if err != nil {
			return err
		}
		if len(cs) == 0 {
			return auth404()
		}
	}
	if contractID != "" {
		if _, err := store.GetContract(ctx, db, scope, contractID); err != nil {
			return notFound(err)
		}
	}
	if vesselID != "" {
		if _, err := store.GetVessel(ctx, db, scope, vesselID); err != nil {
			return notFound(err)
		}
	}
	return nil
}

// PriceLines prices staff lines: unit price from the line or the active price list (pricing.ErrNoPrice when
// none), amount = qty × unit − discount in the item's price terms, VAT split per line. allowNegative accepts
// negative quantities and prices (delta adjustments).
func (s *Service) PriceLines(ctx context.Context, db bun.IDB, scope store.MarinaScope, in []ItemLine, allowNegative bool) ([]LineInput, error) {
	if len(in) == 0 || len(in) > 200 {
		return nil, invalid("lines", "count")
	}
	ids := make([]string, 0, len(in))
	for _, l := range in {
		ids = append(ids, l.ItemID)
	}
	items, err := store.ItemsByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.Item{}
	for i := range items {
		byID[items[i].ID] = &items[i]
	}
	today := s.today(ctx, db, scope)
	out := make([]LineInput, 0, len(in))
	for i, l := range in {
		it := byID[l.ItemID]
		if it == nil {
			return nil, auth404()
		}
		if l.Qty == 0 || (!allowNegative && l.Qty < 0) {
			return nil, invalid("lines.qty_milli", "invalid")
		}
		unit := l.Unit
		if unit == nil {
			u, err := s.ItemPrice(ctx, db, scope, it.ID, today)
			if err != nil {
				return nil, err
			}
			unit = &u
		}
		if !allowNegative && (*unit < 0 || l.Discount < 0) {
			return nil, invalid("lines.unit_cents", "invalid")
		}
		amount, err := l.Qty.MulCents(*unit)
		if err != nil {
			return nil, invalid("lines.unit_cents", "overflow")
		}
		amount -= l.Discount
		if !allowNegative && amount < 0 {
			return nil, invalid("lines.discount_cents", "invalid")
		}
		class, err := s.revenueClassCode(ctx, db, scope, it.RevenueClassID)
		if err != nil {
			return nil, err
		}
		var net, vat, gross money.Cents
		if it.PriceIncludesVAT {
			gross = amount
			net, vat = money.VATSplit(gross, it.VATRateBP)
		} else {
			net = amount
			vat = net.PercentBP(it.VATRateBP)
			gross = net + vat
		}
		desc := strings.TrimSpace(l.Description)
		if desc == "" {
			desc = itemNameIn(it, marinaLocale(ctx, db, scope))
		}
		_ = i
		out = append(out, LineInput{
			ItemID: it.ID, ItemCode: it.Code, Description: desc, Qty: l.Qty, Unit: *unit, Discount: l.Discount,
			Net: net, VAT: vat, Gross: gross, VATRateBP: it.VATRateBP, VATNature: it.VATNature,
			PriceIncludesVAT: it.PriceIncludesVAT, RevenueClassCode: class, ExternalCode: it.ExternalCode,
			ServiceFrom: l.ServiceFrom, ServiceTo: l.ServiceTo,
		})
	}
	return out, nil
}

// ItemPrice returns the unit price of an item on a day from the active price lists (the base rule of the item
// with the highest priority, then the most recent; its unit price, else its flat amount), in the item's price
// terms. pricing.ErrNoPrice when no active list prices it.
func (s *Service) ItemPrice(ctx context.Context, db bun.IDB, scope store.MarinaScope, itemID string, day calendar.Date) (money.Cents, error) {
	rules, err := store.ActiveBaseRulesOfItem(ctx, db, scope, itemID, day)
	if err != nil {
		return 0, err
	}
	for _, r := range rules {
		var body pricing.Rule
		if json.Unmarshal(r.Body, &body) != nil {
			continue
		}
		if body.UnitCents != nil {
			return *body.UnitCents, nil
		}
		if body.AmountCents != nil {
			return *body.AmountCents, nil
		}
	}
	return 0, &pricing.NoPriceError{ItemID: itemID, Night: day, Reason: "no_rule"}
}

// EditInput is an in-place edit of a charge (PATCH charges/{id}).
type EditInput struct {
	CustomerID  *string
	DueOn       *calendar.Date
	Description *string
	Lines       []ItemLine // nil = unchanged
}

// Update edits a charge in place (charges.manage, If-Match): lines and billing customer only while to_invoice,
// unpaid and not offset; due date and description also on exported charges (audited, INV-51).
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in EditInput) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ch, err := store.LockCharge(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if ch.Version != ifMatch {
			return ErrVersionMismatch
		}
		if ch.PaymentStatus == store.ChargeCancelled {
			return ErrCancelled
		}
		e := store.ChargeEdit{DueOn: in.DueOn, Description: in.Description}
		diff := map[string]any{}
		structural := in.Lines != nil || (in.CustomerID != nil && *in.CustomerID != ch.CustomerID)
		if structural {
			ok, err := s.editable(ctx, tx, scope, ch)
			if err != nil {
				return err
			}
			if !ok {
				return ErrImmutable
			}
		}
		if in.CustomerID != nil && *in.CustomerID != ch.CustomerID {
			if *in.CustomerID == "" && ch.FiscalMode != FiscalReceipt {
				return ErrCustomerRequired
			}
			if err := s.checkRefs(ctx, tx, scope, *in.CustomerID, "", ""); err != nil {
				return err
			}
			snap := ""
			if *in.CustomerID != "" {
				if snap, err = s.customerSnapshot(ctx, tx, scope, *in.CustomerID, ch.ID); err != nil {
					return err
				}
			}
			e.CustomerID, e.FiscalSnapshot = in.CustomerID, &snap
			diff["customer_changed"] = true
		}
		if in.Lines != nil {
			if ch.OriginType != store.ChargeOriginManual && ch.OriginType != store.ChargeOriginCounterSale {
				return ErrImmutable // system charges change through their origin (sync) or an adjustment
			}
			priced, err := s.PriceLines(ctx, tx, scope, in.Lines, false)
			if err != nil {
				return err
			}
			lines, t, err := buildLines(scope, ch.ID, priced, s.now())
			if err != nil {
				return err
			}
			if t.gross <= 0 {
				return invalid("lines", "zero_total")
			}
			e.Lines, e.Total, e.Net, e.VAT, e.ServiceFrom, e.ServiceTo = lines, int64(t.gross), int64(t.net), int64(t.vat), t.from, t.to
			diff["total_cents"], diff["previous_total_cents"], diff["lines"] = int64(t.gross), ch.TotalCents, len(lines)
		}
		if in.DueOn != nil {
			diff["due_on"], diff["previous_due_on"] = *in.DueOn, ch.DueOn
		}
		if in.Description != nil {
			diff["description_changed"] = true
		}
		if len(diff) == 0 {
			return nil
		}
		if err := store.EditCharge(ctx, tx, scope, ch.ID, ifMatch, e, s.now()); err != nil {
			return mapStore(err)
		}
		if err := s.write(ctx, tx, scope, p.Actor(), "charge.update", "charge", ch.ID, diff); err != nil {
			return err
		}
		change := event.ChangeUpdated
		if !structural && in.DueOn != nil {
			change = event.ChangeDueChanged
		}
		return s.emit(ctx, tx, scope, event.ChargeChanged, event.ChargeChangedData{ChargeID: ch.ID, Change: change})
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// Cancel cancels an open, unexported, unpaid charge with a reason (charges.manage, If-Match); otherwise
// ErrImmutable (use an adjustment).
func (s *Service) Cancel(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, invalid("reason", "required")
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ch, err := store.LockCharge(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if ch.Version != ifMatch {
			return ErrVersionMismatch
		}
		if ch.PaymentStatus == store.ChargeCancelled {
			return ErrCancelled
		}
		matched, err := store.HasMatches(ctx, tx, scope, ch.ID)
		if err != nil {
			return err
		}
		if matched {
			return ErrImmutable
		}
		ok, err := s.cancelIfPossibleTx(ctx, tx, scope, p.Actor(), ch, reason)
		if err != nil {
			return err
		}
		if !ok {
			return ErrImmutable
		}
		if ch.OriginType == store.ChargeOriginInstallment || ch.OriginType == store.ChargeOriginHolderInstallment {
			// The instalment keeps its charge link; the backfill never re-creates a cancelled origin charge.
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// AdjustInput is an adjustment of a charge (POST charges/{id}/adjust).
type AdjustInput struct {
	Mode   string // delta, replace
	Lines  []ItemLine
	Reason string
}

// Adjust corrects a charge that can no longer be edited in place (exported, paid, offset): delta = a linked
// adjustment charge of the given lines (negative allowed); replace = the full reversal of the original plus a
// replacement charge with the given lines (m5-plan §3.5). charges.manage, plus refunds.issue when a created charge
// is negative. An editable charge is ErrUseEdit, a cancelled one ErrCancelled.
func (s *Service) Adjust(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in AdjustInput) ([]Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		return nil, invalid("reason", "required")
	}
	if in.Mode != "delta" && in.Mode != "replace" {
		return nil, invalid("mode", "invalid")
	}
	canRefund := p.Can(scope.OrgID(), scope.ID(), auth.PermRefundsIssue)
	var ids []string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ids = ids[:0]
		orig, err := store.LockCharge(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if orig.Version != ifMatch {
			return ErrVersionMismatch
		}
		if orig.PaymentStatus == store.ChargeCancelled {
			return ErrCancelled
		}
		if orig.TotalCents < 0 {
			return ErrImmutable // negative charges are corrected through the charge they correct
		}
		ok, err := s.editable(ctx, tx, scope, orig)
		if err != nil {
			return err
		}
		if ok {
			return ErrUseEdit
		}
		actor := p.Actor()
		priced, err := s.PriceLines(ctx, tx, scope, in.Lines, in.Mode == "delta")
		if err != nil {
			return err
		}
		due := s.dueDefault(ctx, tx, scope)
		if in.Mode == "delta" {
			if sumLines(priced) < 0 && !canRefund {
				return auth.ErrForbidden
			}
			desc := s.describe(ctx, tx, scope, "adjustment", orig.Number, "")
			ch, _, err := s.CreateTx(ctx, tx, scope, actor, CreateInput{
				OriginType: store.ChargeOriginAdjustment, OriginID: orig.ID, CustomerID: orig.CustomerID, ContractID: orig.ContractID,
				VesselID: orig.VesselID, RightID: orig.RightID, DueOn: due, FiscalMode: orig.FiscalMode, CorrectsChargeID: orig.ID,
				CorrectionKind: "delta", Description: desc, Lines: priced,
			})
			if err != nil {
				return err
			}
			if ch == nil {
				return invalid("lines", "zero_total")
			}
			ids = append(ids, ch.ID)
			return s.write(ctx, tx, scope, actor, "charge.adjust", "charge", orig.ID, map[string]any{
				"mode": in.Mode, "adjustment_id": ch.ID, "total_cents": ch.TotalCents, "reason": in.Reason,
			})
		}
		if !canRefund {
			return auth.ErrForbidden // the reversal is a negative charge
		}
		lines, err := store.ChargeLinesOf(ctx, tx, scope, []string{orig.ID})
		if err != nil {
			return err
		}
		reversal := make([]LineInput, 0, len(lines))
		for _, l := range lines {
			reversal = append(reversal, LineInput{
				ItemID: l.ItemID, ItemCode: l.ItemCode, Description: l.Description, Qty: qtyOf(-l.QtyMilli), Unit: money.Cents(l.UnitCents),
				Discount: money.Cents(-l.DiscountCents), Net: money.Cents(-l.NetCents), VAT: money.Cents(-l.VATCents), Gross: money.Cents(-l.GrossCents),
				VATRateBP: l.VATRateBP, VATNature: l.VATNature, PriceIncludesVAT: l.PriceIncludesVAT, RevenueClassCode: l.RevenueClassCode,
				ExternalCode: l.ExternalCode, ServiceFrom: l.ServiceFrom, ServiceTo: l.ServiceTo,
			})
		}
		rev, _, err := s.CreateTx(ctx, tx, scope, actor, CreateInput{
			OriginType: store.ChargeOriginAdjustment, OriginID: orig.ID, CustomerID: orig.CustomerID, ContractID: orig.ContractID,
			VesselID: orig.VesselID, RightID: orig.RightID, DueOn: due, FiscalMode: orig.FiscalMode, CorrectsChargeID: orig.ID,
			CorrectionKind: "replace", Description: s.describe(ctx, tx, scope, "reversal", orig.Number, ""), Lines: reversal,
		})
		if err != nil {
			return err
		}
		repl, _, err := s.CreateTx(ctx, tx, scope, actor, CreateInput{
			OriginType: orig.OriginType, OriginID: orig.OriginID, CustomerID: orig.CustomerID, ContractID: orig.ContractID,
			VesselID: orig.VesselID, RightID: orig.RightID, DueOn: due, FiscalMode: orig.FiscalMode, CorrectsChargeID: orig.ID,
			CorrectionKind: "replace", Description: s.describe(ctx, tx, scope, "replace", orig.Number, ""), Lines: priced,
		})
		if err != nil {
			return err
		}
		if rev != nil {
			ids = append(ids, rev.ID)
		}
		if repl != nil {
			ids = append(ids, repl.ID)
		}
		return s.write(ctx, tx, scope, actor, "charge.adjust", "charge", orig.ID, map[string]any{
			"mode": in.Mode, "adjustment_ids": ids, "reason": in.Reason,
		})
	})
	if err != nil {
		return nil, err
	}
	out := make([]Detail, 0, len(ids))
	for _, cid := range ids {
		d, err := s.detail(ctx, s.d.DB.DB, scope, p, cid)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, nil
}

func sumLines(lines []LineInput) money.Cents {
	var t money.Cents
	for _, l := range lines {
		t += l.Gross
	}
	return t
}

// SetInvoice records the external invoice reference of an exported or invoiced charge (charges.export;
// information only, re-entry audited).
func (s *Service) SetInvoice(ctx context.Context, p *auth.Principal, marinaID, id, number string, date calendar.Date) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, err
	}
	number = strings.TrimSpace(number)
	if number == "" || len(number) > 64 {
		return nil, invalid("number", "invalid")
	}
	if date.IsZero() {
		return nil, invalid("date", "required")
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ch, err := store.LockCharge(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if ch.PaymentStatus == store.ChargeCancelled {
			return ErrCancelled
		}
		ok, err := store.SetChargeInvoice(ctx, tx, scope, ch.ID, number, date, s.now())
		if err != nil {
			return mapStore(err)
		}
		if !ok {
			return ErrNotExported
		}
		return s.invoicedTx(ctx, tx, scope, p.Actor(), ch, date, "manual", ch.InvoicingStatus == store.ChargeInvoiced)
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// invoicedTx writes the audit row and event of an invoice reference (never the number in the event).
func (s *Service) invoicedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge, date calendar.Date, source string, reentry bool) error {
	if err := s.write(ctx, tx, scope, actor, "charge.invoice", "charge", ch.ID, map[string]any{
		"invoice_date": date, "source": source, "reentry": reentry,
	}); err != nil {
		return err
	}
	return s.emit(ctx, tx, scope, event.ChargeInvoiced, event.ChargeInvoicedData{ChargeID: ch.ID, InvoiceDate: date, Source: source})
}

// CreateTermination creates the termination charge of a terminated contract from its stored termination amount
// (13.4: penalty > 0, refund < 0 needs refunds.issue), once (INV-54); If-Match is the contract version.
func (s *Service) CreateTermination(ctx context.Context, p *auth.Principal, marinaID, contractID string, ifMatch int) (*Detail, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	var id string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.LockContract(ctx, tx, scope, contractID)
		if err != nil {
			return notFound(err)
		}
		if c.Version != ifMatch {
			return ErrVersionMismatch
		}
		if c.TerminationAmountCents == nil || *c.TerminationAmountCents == 0 {
			return ErrNoTerminationAmount
		}
		amount := *c.TerminationAmountCents
		if amount < 0 && !p.Can(scope.OrgID(), scope.ID(), auth.PermRefundsIssue) {
			return auth.ErrForbidden
		}
		if _, err := store.LivePrimaryCharge(ctx, tx, scope, store.ChargeOriginTermination, c.ID); err == nil {
			return ErrChargeExists
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		key := store.OriginItemTerminationPen
		corrects := ""
		if amount < 0 {
			key = store.OriginItemTerminationRef
			_, live, err := chargedOfContract(ctx, tx, scope, c.ID)
			if err != nil {
				return err
			}
			for i := len(live) - 1; i >= 0; i-- {
				if live[i].TotalCents > 0 {
					corrects = live[i].ID
					break
				}
			}
		}
		day := c.TerminatedOn
		if day.IsZero() {
			day = s.today(ctx, tx, scope)
		}
		line, err := s.originLine(ctx, tx, scope, key, money.Cents(amount), day, day)
		if err != nil {
			return err
		}
		desc := s.describe(ctx, tx, scope, "termination", contract.Code(c), "")
		in := linesFromParts(planAll(line))
		ch, _, err := s.CreateTx(ctx, tx, scope, p.Actor(), CreateInput{
			OriginType: store.ChargeOriginTermination, OriginID: c.ID, CustomerID: billingCustomer(c), ContractID: c.ID,
			VesselID: c.VesselID, DueOn: s.dueDefault(ctx, tx, scope), CorrectsChargeID: corrects, Description: desc,
			Lines: s.describeLines(ctx, tx, scope, in, desc),
		})
		if err != nil {
			return err
		}
		id = ch.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

// Detail is a charge with everything its page shows.
type Detail struct {
	Charge       store.Charge
	Lines        []store.ChargeLine
	Customer     *Party
	ContractCode string
	Overdue      bool
	Corrects     *store.Charge
	Corrections  []store.Charge
	Matches      []store.ChargeMatchRow
	Offsets      []store.ChargeOffset
	Exports      []store.ChargeExportRefRow
	Reminders    []store.ChargeReminderRow
}

// Party is the customer of a charge (display name only with customers.read).
type Party struct {
	ID          string
	DisplayName string
}

// Row is a charge of a list.
type Row struct {
	Charge       store.Charge
	Customer     *Party
	ContractCode string
	Overdue      bool
}

// canRead authorises a read (charges.manage or payments.collect).
func (s *Service) canRead(ctx context.Context, p *auth.Principal, marinaID string) (store.MarinaScope, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if errors.Is(err, auth.ErrForbidden) {
		scope, err = p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	}
	return scope, err
}

// Get returns a charge with its lines, matches, offsets, corrections, exports and reminders.
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, id string) (*Detail, error) {
	scope, err := s.canRead(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, s.d.DB.DB, scope, p, id)
}

func (s *Service) detail(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, id string) (*Detail, error) {
	ch, err := store.GetCharge(ctx, db, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	rows, err := s.rows(ctx, db, scope, p, []store.Charge{*ch})
	if err != nil {
		return nil, err
	}
	d := &Detail{Charge: *ch, Customer: rows[0].Customer, ContractCode: rows[0].ContractCode, Overdue: rows[0].Overdue}
	if d.Lines, err = store.ChargeLinesOf(ctx, db, scope, []string{id}); err != nil {
		return nil, err
	}
	if ch.CorrectsChargeID != "" {
		if d.Corrects, err = store.GetCharge(ctx, db, scope, ch.CorrectsChargeID); err != nil {
			return nil, err
		}
	}
	if d.Corrections, err = store.CorrectionsOf(ctx, db, scope, id); err != nil {
		return nil, err
	}
	if d.Matches, err = store.ChargeMatches(ctx, db, scope, id); err != nil {
		return nil, err
	}
	if d.Offsets, err = store.OffsetsOf(ctx, db, scope, []string{id}); err != nil {
		return nil, err
	}
	if d.Exports, err = store.ChargeExportRefs(ctx, db, scope, id); err != nil {
		return nil, err
	}
	if d.Reminders, err = store.ChargeReminders(ctx, db, scope, id); err != nil {
		return nil, err
	}
	return d, nil
}

// rows decorates charges with the customer (names only with customers.read), the contract code and overdue.
func (s *Service) rows(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, cs []store.Charge) ([]Row, error) {
	names := p.Can(scope.OrgID(), scope.ID(), auth.PermCustomersRead)
	var custIDs, contractIDs []string
	for _, c := range cs {
		if c.CustomerID != "" {
			custIDs = append(custIDs, c.CustomerID)
		}
		if c.ContractID != "" {
			contractIDs = append(contractIDs, c.ContractID)
		}
	}
	slices.Sort(custIDs)
	custIDs = slices.Compact(custIDs)
	slices.Sort(contractIDs)
	contractIDs = slices.Compact(contractIDs)
	display := map[string]string{}
	if names && len(custIDs) > 0 {
		custs, err := store.CustomersByIDs(ctx, db, scope, custIDs)
		if err != nil {
			return nil, err
		}
		for i := range custs {
			display[custs[i].ID] = customer.DisplayName(&custs[i]) // as in the registry, empty once anonymised
		}
	}
	codes := map[string]string{}
	if len(contractIDs) > 0 {
		ctrs, err := store.ContractsByIDs(ctx, db, scope, contractIDs)
		if err != nil {
			return nil, err
		}
		for i := range ctrs {
			codes[ctrs[i].ID] = contract.Code(&ctrs[i])
		}
	}
	today := s.today(ctx, db, scope)
	out := make([]Row, 0, len(cs))
	for _, c := range cs {
		r := Row{Charge: c, ContractCode: codes[c.ContractID], Overdue: c.ResidualCents() > 0 && c.DueOn.Before(today)}
		if c.CustomerID != "" {
			r.Customer = &Party{ID: c.CustomerID, DisplayName: display[c.CustomerID]}
		}
		out = append(out, r)
	}
	return out, nil
}

func displayName(c *store.Customer) string {
	if c.Kind == "company" && c.CompanyName != "" {
		return c.CompanyName
	}
	return strings.TrimSpace(c.FirstName + " " + c.LastName)
}

// ListFilter is the filter of the charge list (API form).
type ListFilter = store.ChargeFilter

// List returns a page of charges (charges.manage or payments.collect). Overdue in the filter uses today.
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter, overdue bool, cursor string, limit int) (store.Page[Row], error) {
	scope, err := s.canRead(ctx, p, marinaID)
	if err != nil {
		return store.Page[Row]{}, err
	}
	db := s.d.DB.DB
	if overdue {
		f.OverdueOn = s.today(ctx, db, scope)
	}
	page, err := store.ListCharges(ctx, db, scope, f, cursor, limit)
	if err != nil {
		return store.Page[Row]{}, err
	}
	rows, err := s.rows(ctx, db, scope, p, page.Items)
	if err != nil {
		return store.Page[Row]{}, err
	}
	return store.Page[Row]{Items: rows, NextCursor: page.NextCursor}, nil
}

// planAll is the single part of a one-line target.
func planAll(l chargeplan.Line) []chargeplan.Part {
	return chargeplan.All([]chargeplan.Remainder{{Line: l, Gross: l.Gross, VAT: l.VAT}})
}
