package contract

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Contracts CSV import (spec 11.6; m4-plan §3.7, §15 D27): one CSV row per instalment of the residual plan,
// grouped by ref into active mooring contracts. Customer by id or fiscal code, vessel by id or registration,
// berth by code; the allocation of the whole period goes through the occupancy engine (a conflict fails
// the file), the plan rows are written as given (Σ = total, INV-31) and paid rows are settled through the
// settlement entry point (INV-32). Undo terminates the contracts on their start while they are untouched.

// Fields of the contracts import mapping.
var importFields = []string{
	"ref", "customer_id", "customer_fiscal_code", "vessel_id", "vessel_registration", "berth_code", "term", "starts_on",
	"ends_on", "total", "due_on", "amount", "status",
}

type contractImport struct{ s *Service }

// ImportHandler returns the CSV import handler of the contracts entity.
func (s *Service) ImportHandler() csvimport.EntityHandler { return contractImport{s} }

func (contractImport) Entity() string   { return csvimport.EntityContracts }
func (contractImport) Fields() []string { return importFields }

// importedContract is one contract of the file with its plan rows.
type importedContract struct {
	ref                string
	line               int
	customerID         string
	vesselID           string
	berthID            string
	term               string
	startsOn, endsOn   calendar.Date
	total              int64
	plan               []importedRow
	fiscalCode, vesReg string
}

type importedRow struct {
	line   int
	dueOn  calendar.Date
	amount int64
	status string
}

// parse groups and validates the rows; rejected rows are recorded in rep and their contract is skipped.
func (h contractImport) parse(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) ([]*importedContract, error) {
	byRef := map[string]*importedContract{}
	var order []string
	bad := map[string]bool{}
	var codes, regs []string
	for _, r := range rows {
		ref := strings.TrimSpace(r.Get("ref"))
		if ref == "" {
			rep.Reject(r.Line, "required", "ref", "")
			continue
		}
		c := byRef[ref]
		if c == nil {
			c = &importedContract{ref: ref, line: r.Line}
			c.customerID = strings.TrimSpace(r.Get("customer_id"))
			c.fiscalCode = strings.ToUpper(strings.TrimSpace(r.Get("customer_fiscal_code")))
			c.vesselID = strings.TrimSpace(r.Get("vessel_id"))
			c.vesReg = strings.ToUpper(strings.TrimSpace(r.Get("vessel_registration")))
			c.term = strings.TrimSpace(r.Get("term"))
			var err error
			if c.startsOn, err = calendar.Parse(strings.TrimSpace(r.Get("starts_on"))); err != nil {
				rep.Reject(r.Line, "invalid", "starts_on", ref)
				bad[ref] = true
			}
			if c.endsOn, err = calendar.Parse(strings.TrimSpace(r.Get("ends_on"))); err != nil || !c.endsOn.After(c.startsOn) {
				rep.Reject(r.Line, "invalid", "ends_on", ref)
				bad[ref] = true
			}
			t, err := money.ParseCents(r.Get("total"))
			if err != nil || t < 0 {
				rep.Reject(r.Line, "invalid", "total", ref)
				bad[ref] = true
			}
			c.total = int64(t)
			if !contains(terms, c.term) {
				rep.Reject(r.Line, "invalid", "term", ref)
				bad[ref] = true
			}
			c.berthID = strings.TrimSpace(r.Get("berth_code"))
			if c.fiscalCode != "" {
				codes = append(codes, c.fiscalCode)
			}
			if c.vesReg != "" {
				regs = append(regs, c.vesReg)
			}
			byRef[ref] = c
			order = append(order, ref)
		}
		due, err := calendar.Parse(strings.TrimSpace(r.Get("due_on")))
		if err != nil {
			rep.Reject(r.Line, "invalid", "due_on", ref)
			bad[ref] = true
			continue
		}
		amt, err := money.ParseCents(r.Get("amount"))
		if err != nil || amt < 0 {
			rep.Reject(r.Line, "invalid", "amount", ref)
			bad[ref] = true
			continue
		}
		st := strings.TrimSpace(r.Get("status"))
		if st == "" {
			st = store.InstallmentScheduled
		}
		if st != store.InstallmentScheduled && st != store.InstallmentIssued && st != store.InstallmentPaid {
			rep.Reject(r.Line, "invalid", "status", ref)
			bad[ref] = true
			continue
		}
		c.plan = append(c.plan, importedRow{line: r.Line, dueOn: due, amount: int64(amt), status: st})
	}
	fiscal, err := store.CustomerIDsByFiscalCode(ctx, db, scope, codes)
	if err != nil {
		return nil, err
	}
	vessels, err := store.VesselIDsByRegistration(ctx, db, scope, regs)
	if err != nil {
		return nil, err
	}
	berths, err := store.BerthIDsByCode(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	var out []*importedContract
	for _, ref := range order {
		c := byRef[ref]
		if bad[ref] {
			continue
		}
		if c.customerID == "" {
			c.customerID = fiscal[c.fiscalCode]
		}
		if c.vesselID == "" {
			c.vesselID = vessels[c.vesReg]
		}
		id, ok := berths[c.berthID]
		switch {
		case c.customerID == "" || checkParties(ctx, db, scope, "", c.customerID) != nil:
			rep.Reject(c.line, "not_found", "customer", ref)
			continue
		case c.vesselID == "" || checkParties(ctx, db, scope, c.vesselID) != nil:
			rep.Reject(c.line, "not_found", "vessel", ref)
			continue
		case !ok:
			rep.Reject(c.line, "not_found", "berth_code", ref)
			continue
		}
		c.berthID = id
		var sum int64
		for _, p := range c.plan {
			sum += p.amount
		}
		if sum != c.total {
			rep.Reject(c.line, "plan_sum_mismatch", "amount", ref) // INV-31
			continue
		}
		sort.SliceStable(c.plan, func(i, j int) bool { return c.plan[i].dueOn.Before(c.plan[j].dueOn) })
		out = append(out, c)
	}
	return out, nil
}

// DryRun validates the file without writing.
func (h contractImport) DryRun(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) error {
	cs, err := h.parse(ctx, db, scope, rows, rep)
	if err != nil {
		return err
	}
	for _, c := range cs {
		rep.Valid += len(c.plan)
	}
	return nil
}

// Apply writes the contracts of the file in the caller's transaction; any allocation conflict fails the file.
func (h contractImport) Apply(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) ([]csvimport.Created, error) {
	s := h.s
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	cs, err := h.parse(ctx, tx, scope, rows, rep)
	if err != nil {
		return nil, err
	}
	st, err := store.ContractOccupancySettings(ctx, tx, scope, s.now())
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, tx, scope)
	var created []csvimport.Created
	for _, ic := range cs {
		first := ic.startsOn // the whole period: an active contract covers its nights (Q7)
		if !today.Before(ic.endsOn) {
			rep.Reject(ic.line, "ended", "ends_on", ic.ref)
			continue
		}
		total := ic.total
		pol := defaultPolicy(st)
		c, err := s.createMooringTx(ctx, tx, scope, system, newContract{
			kind: KindMooring, term: ic.term, customerID: ic.customerID, vesselID: ic.vesselID, startsOn: ic.startsOn,
			endsOn: ic.endsOn, channel: ChannelAPI, policy: pol, manualTotal: &total, skipPlan: true,
			segments:       []Segment{{ResourceType: store.ResourceBerth, ResourceID: ic.berthID, First: first, Last: ic.endsOn.AddDays(-1)}},
			priceIsBinding: true,
		}, true, st)
		if err != nil {
			if _, ok := errors.AsType[*occupancy.ConflictError](err); ok || isTaken(err) {
				rep.Reject(ic.line, "resource_taken", "berth_code", ic.ref)
			}
			return nil, err
		}
		if err := s.importPlanTx(ctx, tx, scope, c, ic); err != nil {
			return nil, err
		}
		rep.Valid += len(ic.plan)
		created = append(created, csvimport.Created{EntityType: entityContract, EntityID: c.ID})
	}
	if rep.Created == nil {
		rep.Created = map[string]int{}
	}
	rep.Created[entityContract] += len(created)
	return created, nil
}

// importPlanTx writes the residual plan of an imported contract: scheduled and issued rows as given, paid rows
// settled through settleTx (source manual, method other, on their due date).
func (s *Service) importPlanTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, ic *importedContract) error {
	now := s.now()
	version := c.InstallmentPlanVersion + 1
	rows := make([]store.Installment, 0, len(ic.plan))
	for i, p := range ic.plan {
		status := store.InstallmentScheduled
		if p.status == store.InstallmentIssued {
			status = store.InstallmentIssued
		}
		r := store.Installment{
			ID: store.NewID(), MarinaID: scope.ID(), ContractID: c.ID, PlanVersion: version, Seq: i + 1, Kind: KindInstallmentRow,
			IssueOn: p.dueOn, DueOn: p.dueOn, AmountCents: p.amount, Status: status, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if status == store.InstallmentIssued {
			r.IssuedAt = now
		}
		rows = append(rows, r)
	}
	if err := store.InsertInstallments(ctx, tx, rows); err != nil {
		return err
	}
	c.InstallmentPlanVersion = version
	if err := store.SetContractPlan(ctx, tx, scope, c, now); err != nil {
		return err
	}
	for i, p := range ic.plan {
		if p.status != store.InstallmentPaid {
			continue
		}
		on := p.dueOn
		if ok, err := s.settleTx(ctx, tx, scope, system, &rows[i], 0, store.SettlementManual, SettleInput{Method: "other", SettledOn: &on}); err != nil || !ok {
			if err == nil {
				err = ErrSettleConflict
			}
			return err
		}
	}
	return nil
}

// KindInstallmentRow is the kind of imported plan rows.
const KindInstallmentRow = "installment"

// Undo terminates the imported contracts on their start (allocations released, open instalments cancelled)
// while each is untouched (version 1, no proposal, no addendum); otherwise csvimport.ErrNotUndoable.
func (h contractImport) Undo(ctx context.Context, tx bun.IDB, scope store.MarinaScope, created []csvimport.Created) error {
	s := h.s
	if err := s.requireAllocator(); err != nil {
		return err
	}
	for _, cr := range created {
		if cr.EntityType != entityContract {
			continue
		}
		c, err := lockContract(ctx, tx, scope, cr.EntityID, KindMooring, 0)
		if err != nil {
			return err
		}
		if c.Version != 1 || c.Status != string(StatusActive) {
			return csvimport.ErrNotUndoable
		}
		if pr, err := store.OpenProposalOfSource(ctx, tx, scope, c.ID); err != nil || pr != nil {
			if err == nil {
				err = csvimport.ErrNotUndoable
			}
			return err
		}
		if ads, err := store.AddendaOfContract(ctx, tx, scope, c.ID); err != nil || len(ads) > 0 {
			if err == nil {
				err = csvimport.ErrNotUndoable
			}
			return err
		}
		t, err := Next(MooringTransitions, Status(c.Status), ActionTerminate)
		if err != nil {
			return err
		}
		allocs, err := contractAllocs(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		if err := s.cancelAllocs(ctx, tx, scope, allocs, occupancy.StatusCancelled, event.ReleasedCancel); err != nil {
			return err
		}
		cancelled, err := s.cancelPlanTx(ctx, tx, scope, c, true)
		if err != nil {
			return err
		}
		zero := int64(0)
		c.Status, c.TerminatedOn, c.TerminateReason, c.TerminationAmountCents = string(t.To), c.StartsOn, "import undone", &zero
		if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return err
		}
		if err := s.writeAudit(ctx, tx, scope, system, "contract.terminate", entityContract, c.ID, map[string]any{
			"from_status": string(StatusActive), "status": c.Status, "import_undo": true, "installment_ids": cancelled,
		}); err != nil {
			return err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c}); err != nil {
			return err
		}
	}
	return nil
}
