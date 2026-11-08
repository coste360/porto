package utility

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/charge"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Charging (m9-plan §9.4, D22, INV-90): the pending consumptions of a contract become one charge per run through
// charge.CreateTx with origin utility (origin_id = the utility_billings row), one line per consumption priced at
// the active price of the meter item (or the utility settings item of its kind) on its last night. Under the
// contract lock the consumptions are claimed with a conditional update; a shortfall means another run took some:
// the transaction rolls back and is retried once. Correction deltas of a charged interval become an adjustment
// charge linked to the charge they correct (INV-51).

// Skip reasons of a run.
const (
	SkipNoPrice   = "no_price"
	SkipNotDue    = "not_due"
	SkipNoItem    = SkipNoPrice // no tariff item configured: the price is missing as well
	reasonZeroQty = "zero_quantity"
	// reasonZeroAmount closes priced rows whose charge totals zero (0 price, or corrections netting to zero).
	reasonZeroAmount = "zero_amount"
)

var errClaimRace = errors.New("utility: consumptions claimed by another run")

// Run charges the pending consumptions now: one contract (whatever its schedule), or every contract due today
// (charges.manage).
func (s *Service) Run(ctx context.Context, p *auth.Principal, marinaID, contractID string) (*RunResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if contractID != "" {
		if _, err := store.GetContract(ctx, s.d.DB.DB, scope, contractID); err != nil {
			return nil, notFound(err)
		}
		res := &RunResult{Charges: []RunCharge{}, Skipped: []RunSkip{}}
		if err := s.chargeContract(ctx, scope, p.Actor(), contractID, true, res); err != nil {
			return nil, err
		}
		return res, nil
	}
	return s.chargeMarina(ctx, scope, p.Actor())
}

// RunCharge is the hourly utilities.charge job (registered after installments.issue): every contract due today in
// every marina. Idempotent.
func (s *Service) RunCharge(ctx context.Context) error {
	ids, err := store.ActiveMarinaIDs(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var first error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			continue
		}
		if _, err := s.chargeMarina(ctx, scope, audit.System); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *Service) chargeMarina(ctx context.Context, scope store.MarinaScope, actor audit.Actor) (*RunResult, error) {
	res := &RunResult{Charges: []RunCharge{}, Skipped: []RunSkip{}}
	ids, err := store.ContractsWithPendingConsumptions(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if err := s.chargeContract(ctx, scope, actor, id, false, res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// dueToday tells whether the pending consumptions of a contract are charged today and with which due date (zero =
// the billing default): transits after departure or their last night; other contracts with the instalment charge
// issued today (mode with_installment), on the 1st (mode monthly), or once ended.
func (s *Service) dueToday(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.Contract, mode string, today calendar.Date) (calendar.Date, bool, error) {
	if c.Kind == store.ContractKindTransit {
		if c.Status == string(contract.StatusDeparted) {
			return calendar.Date{}, true, nil
		}
		last, err := store.LastNightOfContract(ctx, db, scope, c.ID)
		if err != nil {
			return calendar.Date{}, false, err
		}
		return calendar.Date{}, !last.IsZero() && last.Before(today), nil
	}
	if contains([]string{"expired", "terminated", "cancelled", "renewed"}, c.Status) || (!c.EndsOn.IsZero() && !c.EndsOn.After(today)) {
		return calendar.Date{}, true, nil
	}
	if mode == store.UtilityModeMonthly {
		return calendar.Date{}, today.Day() == 1, nil
	}
	return store.InstallmentChargeIssued(ctx, db, scope, c.ID, today)
}

// chargeContract charges the pending consumptions of one contract (force: whatever the schedule), retrying once
// when another run claimed some of them.
func (s *Service) chargeContract(ctx context.Context, scope store.MarinaScope, actor audit.Actor, contractID string, force bool, res *RunResult) error {
	var out *RunResult
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		out = &RunResult{}
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			out = &RunResult{}
			return s.chargeContractTx(ctx, tx, scope, actor, contractID, force, out)
		})
		if !errors.Is(err, errClaimRace) {
			break
		}
	}
	if errors.Is(err, errClaimRace) {
		res.Skipped = append(res.Skipped, RunSkip{ContractID: contractID, Reason: SkipNotDue})
		return nil
	}
	if err != nil {
		return err
	}
	res.Charges = append(res.Charges, out.Charges...)
	res.Skipped = append(res.Skipped, out.Skipped...)
	return nil
}

// itemOf returns the tariff item of a meter: its own, else the settings item of its kind.
func itemOf(m *store.UtilityMeter, set *store.UtilitySettings) string {
	if m.ItemID != "" {
		return m.ItemID
	}
	switch m.Kind {
	case store.MeterPower:
		return set.PowerItemID
	case store.MeterWater:
		return set.WaterItemID
	}
	return ""
}

var kindLabel = map[string]string{store.MeterPower: "Energia", store.MeterWater: "Acqua"}

var unitLabel = map[string]string{"kwh": "kWh", "m3": "m³", "h": "h", "cycle": "cicli"}

func shortDate(d calendar.Date) string { return fmt.Sprintf("%02d/%02d", d.Day(), int(d.Month())) }

// lineDescription is the charge line text (Italian, the source language of documents): "Energia 12,345 kWh
// 01/06–30/06, P12-A".
func lineDescription(m *store.UtilityMeter, c *store.UtilityConsumption) string {
	label := kindLabel[m.Kind]
	if label == "" {
		label = m.Kind
	}
	text := label + " " + qty.Milli(c.QtyMilli).Format(3, ',') + " " + unitLabel[m.Unit] + " " + shortDate(c.FirstNight) + "–" +
		shortDate(c.LastNight) + ", " + m.Code
	if strings.HasPrefix(c.ShareKey, store.ConsumptionCorrectionPrefix) {
		text = "Rettifica " + text
	}
	return text
}

type chargeGroup struct {
	corrects string // the charge corrected (adjustments), "" for the primary charge
	rows     []store.UtilityConsumption
}

func (s *Service) chargeContractTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, contractID string, force bool,
	out *RunResult,
) error {
	c, err := store.LockContract(ctx, tx, scope, contractID)
	if err != nil {
		return notFound(err)
	}
	rows, err := store.PendingConsumptionsOfContract(ctx, tx, scope, contractID)
	if err != nil || len(rows) == 0 {
		return err
	}
	now := s.now()
	today := s.today(ctx, tx, scope)
	set, err := store.GetUtilitySettings(ctx, tx, scope, now)
	if err != nil {
		return err
	}
	dueOn, due := calendar.Date{}, force
	if !force {
		if dueOn, due, err = s.dueToday(ctx, tx, scope, c, set.Mode, today); err != nil {
			return err
		}
	}
	if !due {
		out.Skipped = append(out.Skipped, RunSkip{ContractID: contractID, Reason: SkipNotDue})
		return nil
	}
	meters := map[string]*store.UtilityMeter{}
	// Correction rows correct the charge of their interval when this contract was charged in it.
	all, err := s.chargedOfContract(ctx, tx, scope, contractID)
	if err != nil {
		return err
	}
	groups := map[string]*chargeGroup{"": {}}
	order := []string{""}
	for _, r := range rows {
		if meters[r.MeterID] == nil {
			m, err := store.GetUtilityMeter(ctx, tx, scope, r.MeterID)
			if err != nil {
				return err
			}
			meters[r.MeterID] = m
		}
		key := ""
		if strings.HasPrefix(r.ShareKey, store.ConsumptionCorrectionPrefix) {
			key = all[pair{r.FromReadingID, r.ToReadingID}]
		}
		g := groups[key]
		if g == nil {
			g = &chargeGroup{corrects: key}
			groups[key] = g
			order = append(order, key)
		}
		g.rows = append(g.rows, r)
	}
	slices.Sort(order[1:])
	customerID := c.BillingCustomerID
	if customerID == "" {
		customerID = c.CustomerID
	}
	for _, key := range order {
		g := groups[key]
		if len(g.rows) == 0 {
			continue
		}
		if err := s.chargeGroup(ctx, tx, scope, actor, c, customerID, dueOn, today, set, meters, g, out); err != nil {
			return err
		}
	}
	return nil
}

// chargedOfContract maps the intervals charged to a contract to their charge (the latest).
func (s *Service) chargedOfContract(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string) (map[pair]string, error) {
	page, err := store.ListUtilityConsumptions(ctx, tx, scope, store.UtilityConsumptionFilter{Status: store.ConsumptionCharged, ContractID: contractID}, "", store.MaxLimit)
	if err != nil {
		return nil, err
	}
	out := map[pair]string{}
	for {
		for _, c := range page.Items {
			if !strings.HasPrefix(c.ShareKey, store.ConsumptionCorrectionPrefix) {
				out[pair{c.FromReadingID, c.ToReadingID}] = c.ChargeID
			}
		}
		if page.NextCursor == "" {
			return out, nil
		}
		if page, err = store.ListUtilityConsumptions(ctx, tx, scope, store.UtilityConsumptionFilter{Status: store.ConsumptionCharged, ContractID: contractID}, page.NextCursor, store.MaxLimit); err != nil {
			return nil, err
		}
	}
}

func (s *Service) chargeGroup(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, c *store.Contract, customerID string,
	dueOn, today calendar.Date, set *store.UtilitySettings, meters map[string]*store.UtilityMeter, g *chargeGroup, out *RunResult,
) error {
	var lines []charge.ItemLine
	var claim, zero []string
	negative := false
	for i := range g.rows {
		r := &g.rows[i]
		m := meters[r.MeterID]
		if r.QtyMilli == 0 {
			zero = append(zero, r.ID)
			continue
		}
		item := itemOf(m, set)
		if item == "" {
			out.Skipped = append(out.Skipped, RunSkip{ContractID: c.ID, ConsumptionID: r.ID, Reason: SkipNoItem})
			continue
		}
		unit, err := s.d.Charges.ItemPrice(ctx, tx, scope, item, r.LastNight)
		if err != nil {
			if errors.Is(err, pricing.ErrNoPrice) {
				out.Skipped = append(out.Skipped, RunSkip{ContractID: c.ID, ConsumptionID: r.ID, Reason: SkipNoPrice})
				continue
			}
			return err
		}
		u := money.Cents(unit)
		lines = append(lines, charge.ItemLine{
			ItemID: item, Qty: qty.Milli(r.QtyMilli), Unit: &u, Description: lineDescription(m, r), ServiceFrom: r.FirstNight, ServiceTo: r.LastNight,
		})
		claim = append(claim, r.ID)
		negative = negative || r.QtyMilli < 0
	}
	now := s.now()
	// Zero shares are closed without a charge (nothing to bill, never pending forever).
	if err := s.exclude(ctx, tx, scope, zero, reasonZeroQty, now); err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	priced, err := s.d.Charges.PriceLines(ctx, tx, scope, lines, negative || g.corrects != "")
	if err != nil {
		return err
	}
	b := &store.UtilityBilling{ID: store.NewID(), ContractID: c.ID, Kind: "primary", RunOn: today, CreatedAt: now}
	if actor.Type == audit.ActorUser {
		b.CreatedByUserID = actor.ID
	}
	in := charge.CreateInput{
		OriginType: store.ChargeOriginUtility, OriginID: b.ID, CustomerID: customerID, ContractID: c.ID, VesselID: c.VesselID,
		DueOn: dueOn, Description: "Consumi utenze " + contract.Code(c), Lines: priced,
	}
	if g.corrects != "" {
		b.Kind = "adjustment"
		in.CorrectsChargeID, in.CorrectionKind = g.corrects, "delta"
		in.Description = "Rettifica consumi utenze " + contract.Code(c)
	}
	ch, chLines, err := s.d.Charges.CreateTx(ctx, tx, scope, actor, in)
	if err != nil {
		return err
	}
	if ch == nil {
		// A zero total (a 0 unit price, or corrections netting to zero): nothing to bill. The rows are closed with
		// a reason and no billing row is written, so the next run does not retry them forever.
		return s.exclude(ctx, tx, scope, claim, reasonZeroAmount, now)
	}
	// The billing is written only with its charge (never a billing without a charge).
	b.ChargeID = ch.ID
	if err := store.InsertUtilityBilling(ctx, tx, scope, b); err != nil {
		return err
	}
	n, err := store.ClaimConsumptions(ctx, tx, scope, claim, ch.ID, b.ID, now)
	if err != nil {
		return err
	}
	if n != len(claim) {
		return errClaimRace
	}
	if err := s.audit(ctx, tx, scope, actor, "utility.charge", "contract", c.ID,
		map[string]any{"billing_id": b.ID, "charge_id": ch.ID, "consumptions": len(claim), "kind": b.Kind}); err != nil {
		return err
	}
	if err := s.changed(ctx, tx, scope, "billing", b.ID, "", store.ConsumptionCharged); err != nil {
		return err
	}
	out.Charges = append(out.Charges, RunCharge{
		ChargeID: ch.ID, BillingID: b.ID, ContractID: c.ID, Lines: len(chLines), TotalCents: ch.TotalCents, Adjustment: g.corrects != "",
	})
	return nil
}

// exclude closes pending consumptions without a charge, with a reason.
func (s *Service) exclude(ctx context.Context, tx bun.IDB, scope store.MarinaScope, ids []string, reason string, now time.Time) error {
	for _, id := range ids {
		row, err := store.GetUtilityConsumption(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		v := row.Version
		row.Status, row.ExcludeReason = store.ConsumptionExcluded, reason
		if err := store.UpdateUtilityConsumption(ctx, tx, scope, row, v, now); err != nil {
			return err
		}
	}
	return nil
}

// onChargeCancelledTx releases the consumptions of a cancelled utility charge (inside the cancel transaction): they
// go back to pending, unlinked, so the next run charges them again (INV-90). Audit and outbox in the same tx.
func (s *Service) onChargeCancelledTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, ch *store.Charge) error {
	ids, err := store.ReopenConsumptionsOfCharge(ctx, tx, scope, ch.ID, s.now())
	if err != nil || len(ids) == 0 {
		return err
	}
	if err := s.audit(ctx, tx, scope, actor, "utility.charge_cancelled", "charge", ch.ID,
		map[string]any{"billing_id": ch.OriginID, "consumptions": len(ids)}); err != nil {
		return err
	}
	return s.changed(ctx, tx, scope, "billing", ch.OriginID, "", store.ConsumptionPending)
}
