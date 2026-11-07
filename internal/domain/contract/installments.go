package contract

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/schedule"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Instalments (spec 13.7; m4-plan §3.3, INV-31, INV-32): the plan of a contract is computed by the pure schedule
// package from the effective payment policy, stored with a plan version, issued by the installments.issue job (or
// "issue now"), settled only through settle.go and replaced (never edited) by a replan.

// Plan choices.
const (
	PlanStandard     = "standard"
	PlanEarlyPayment = "early_payment"
)

const (
	entityInstallment = "installment"
	issueBatch        = 500
	maxBulk           = 500
	maxCSVRows        = 20_000
)

// PlanView is the live instalment plan of a contract.
type PlanView struct {
	PlanVersion  int
	Items        []store.Installment // live rows (not cancelled), by plan version and seq
	Total        money.Cents         // contract total
	Sum          money.Cents         // Σ live rows (= Total, INV-31)
	EarlyPayment *schedule.Alternative
}

// planInput is the planner input of a contract for the amount still to plan.
func (s *Service) planInput(c *store.Contract, today calendar.Date, total money.Cents, pay PaymentPolicy) schedule.Input {
	return schedule.Input{Total: total, StartsOn: c.StartsOn, EndsOn: c.EndsOn, Today: today, Payment: pay.Planner()}
}

// planTx writes a new instalment plan of the contract for amount (the total, or the remainder of a replan) and
// emits installment.changed (planned); the caller's audit row records it (planDiff):
// the effective payment policy is resolved (contract, marina default, built-in) and stored on the contract,
// choice early_payment applies the discount as an INV-27 adjustment and plans one single instalment. The contract
// columns it changes (policy, total, snapshot, plan version) are written by setPlanTx without a version bump.
func (s *Service) planTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract,
	cs *store.ContractSettings, amount money.Cents, choice string,
) ([]store.Installment, error) {
	today := s.today(ctx, tx, scope)
	pol, err := DecodePolicy(c.Policy)
	if err != nil {
		return nil, err
	}
	pay, _, _ := effective(pol, cs)
	if pol.Payment == nil {
		pol.Payment = &pay
		if pol.Version == 0 {
			pol.Version = PolicyVersion
		}
		c.Policy = store.MustJSON(pol)
	}
	in := s.planInput(c, today, amount, pay)
	var items []schedule.Item
	switch choice {
	case PlanEarlyPayment:
		alt, err := schedule.EarlyPaymentPlan(in)
		if err != nil {
			return nil, planErr(err)
		}
		if alt == nil {
			v := &auth.ValidationError{}
			v.Add("plan", "no_early_payment")
			return nil, v
		}
		if err := applyAdjustment(c, PriceAdjustment{Kind: AdjustmentEarlyPayment, OldCents: int64(amount), NewCents: int64(alt.Total), From: today, To: today}); err != nil {
			return nil, err
		}
		items = alt.Items
	default:
		items, err = schedule.Plan(in)
		if err != nil {
			return nil, planErr(err)
		}
	}
	version := c.InstallmentPlanVersion + 1
	now := s.now()
	rows := make([]store.Installment, 0, len(items))
	for _, it := range items {
		rows = append(rows, store.Installment{
			ID: store.NewID(), MarinaID: scope.ID(), ContractID: c.ID, PlanVersion: version, Seq: it.Seq, Kind: it.Kind,
			IssueOn: it.IssueOn, DueOn: it.DueOn, AmountCents: int64(it.Amount), Status: store.InstallmentScheduled,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := store.InsertInstallments(ctx, tx, rows); err != nil {
		return nil, err
	}
	c.InstallmentPlanVersion = version
	if err := store.SetContractPlan(ctx, tx, scope, c, now); err != nil {
		return nil, err
	}
	return rows, s.emit(ctx, tx, scope, event.InstallmentChanged, event.InstallmentChangedData{
		ContractID: c.ID, Change: event.ChangePlanned, PlanVersion: version,
	})
}

// planDiff summarises a plan for the audit diff of the state change that made it.
func planDiff(c *store.Contract, rows []store.Installment) map[string]any {
	var sum int64
	for _, r := range rows {
		sum += r.AmountCents
	}
	return map[string]any{"plan_version": c.InstallmentPlanVersion, "count": len(rows), "sum_cents": sum}
}

func choiceOr(c string) string {
	if c == "" {
		return PlanStandard
	}
	return c
}

func planErr(err error) error {
	if errors.Is(err, schedule.ErrInvalidPolicy) || errors.Is(err, schedule.ErrNegativeTotal) {
		v := &auth.ValidationError{}
		v.Add("policy.payment", "invalid")
		return v
	}
	return err
}

// applyAdjustment records a price adjustment on the snapshot (when the contract has one) and moves the total by
// its delta (INV-27: total = breakdown + Σ adjustments).
func applyAdjustment(c *store.Contract, adj PriceAdjustment) error {
	delta := adj.NewCents - adj.OldCents
	if c.TotalCents+delta < 0 {
		return ErrRepriceRefused
	}
	if snap := DecodeSnapshot(c.PriceSnapshot); snap != nil {
		snap.Adjustments = append(snap.Adjustments, adj)
		raw, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		c.PriceSnapshot = raw
	}
	c.TotalCents += delta
	return nil
}

// planOnCreateTx plans a newly created contract from its total.
func (s *Service) planOnCreateTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, choice string) ([]store.Installment, error) {
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	return s.planTx(ctx, tx, scope, c, cs, money.Cents(c.TotalCents), choice)
}

// replanTx cancels the scheduled instalments and plans the remainder (total − Σ issued and paid) under a new
// plan version (addenda, change of vessel, early payment, explicit replan). With early_payment the discount
// applies to the remainder.
func (s *Service) replanTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract, choice string) ([]store.Installment, error) {
	live, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{c.ID}, true)
	if err != nil {
		return nil, err
	}
	var fixed int64
	scheduled := 0
	for _, r := range live {
		if r.Status != store.InstallmentScheduled {
			fixed += r.AmountCents
		} else {
			scheduled++
		}
	}
	next := c.InstallmentPlanVersion + 1
	cancelled, err := store.CancelInstallments(ctx, tx, scope, c.ID, []string{store.InstallmentScheduled}, &next, s.now())
	if errors.Is(err, store.ErrVersionMismatch) {
		return nil, ErrVersionMismatch
	}
	if err != nil {
		return nil, err
	}
	if len(cancelled) != scheduled {
		// A scheduled row changed after the read above (a writer that skipped the contract lock): fixed would
		// be wrong and the new plan would not sum to the total (INV-31).
		return nil, ErrVersionMismatch
	}
	rest := c.TotalCents - fixed
	if rest < 0 {
		return nil, ErrRepriceRefused
	}
	cs, err := s.settingsTx(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	rows, err := s.planTx(ctx, tx, scope, c, cs, money.Cents(rest), choice)
	if err != nil {
		return nil, err
	}
	if err := s.writeAudit(ctx, tx, scope, a, "installment.replan", entityContract, c.ID, map[string]any{
		"plan_version": c.InstallmentPlanVersion, "cancelled": cancelled, "fixed_cents": fixed, "rest_cents": rest,
	}); err != nil {
		return nil, err
	}
	return rows, s.emit(ctx, tx, scope, event.InstallmentChanged, event.InstallmentChangedData{
		ContractID: c.ID, Change: event.ChangeReplanned, PlanVersion: c.InstallmentPlanVersion,
	})
}

// cancelPlanTx cancels the scheduled (and, when withIssued, issued) instalments of a contract (termination,
// cancellation); the caller's audit row lists the ids.
func (s *Service) cancelPlanTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *store.Contract, withIssued bool) ([]string, error) {
	st := []string{store.InstallmentScheduled}
	if withIssued {
		st = append(st, store.InstallmentIssued)
	}
	ids, err := store.CancelInstallments(ctx, tx, scope, c.ID, st, nil, s.now())
	if errors.Is(err, store.ErrVersionMismatch) {
		return nil, ErrVersionMismatch
	}
	if err != nil || len(ids) == 0 {
		return ids, err
	}
	return ids, s.emit(ctx, tx, scope, event.InstallmentChanged, event.InstallmentChangedData{
		ContractID: c.ID, Change: event.ChangeCancelled, PlanVersion: c.InstallmentPlanVersion,
	})
}

// issueFirstTx issues the first scheduled instalment of the contract at signature or activation (the deposit,
// spec 13.3 step 5) and queues its payment request. No instalment is not an error.
func (s *Service) issueFirstTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract) (*store.Installment, error) {
	live, err := store.InstallmentsOfContracts(ctx, tx, scope, []string{c.ID}, true)
	if err != nil {
		return nil, err
	}
	for i := range live {
		r := &live[i]
		if r.Status != store.InstallmentScheduled {
			continue
		}
		ok, err := s.issueOneTx(ctx, tx, scope, a, r)
		if err != nil || !ok {
			return nil, err
		}
		return r, nil
	}
	return nil, nil
}

// issueOneTx marks one scheduled instalment issued (conditional), writes its audit row and event and queues the
// payment request email; false when it was not scheduled any more.
func (s *Service) issueOneTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, r *store.Installment) (bool, error) {
	now := s.now()
	if _, err := store.LockContract(ctx, tx, scope, r.ContractID); err != nil { // lock order: contract first (INV-31)
		return false, err
	}
	ok, err := store.IssueInstallmentRow(ctx, tx, scope, r.ID, now)
	if err != nil || !ok {
		return false, err
	}
	r.Status, r.IssuedAt = store.InstallmentIssued, now
	r.Version++
	if err := s.writeAudit(ctx, tx, scope, a, "installment.issue", entityInstallment, r.ID, map[string]any{
		"contract_id": r.ContractID, "seq": r.Seq, "due_on": r.DueOn, "amount_cents": r.AmountCents,
	}); err != nil {
		return false, err
	}
	if err := s.emit(ctx, tx, scope, event.InstallmentChanged, event.InstallmentChangedData{
		InstallmentID: r.ID, ContractID: r.ContractID, Change: event.ChangeIssued, PlanVersion: r.PlanVersion,
	}); err != nil {
		return false, err
	}
	if s.billingActive() {
		c, err := store.GetContract(ctx, tx, scope, r.ContractID)
		if err != nil {
			return false, err
		}
		if err := s.billing().OnInstallmentIssuedTx(ctx, tx, scope, a.audit(), c, r); err != nil {
			return false, err
		}
		return true, nil
	}
	if r.AmountCents > 0 {
		if _, err := s.installmentMailTx(ctx, tx, scope, r, MailInstallmentPaymentRequest, "installment:"+r.ID+":issued"); err != nil &&
			!errors.Is(err, ErrCustomerEmailMissing) {
			return false, err
		}
	}
	return true, nil
}

// installmentMailTx queues the payment request or reminder of an instalment to the contract's billing customer.
func (s *Service) installmentMailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.Installment, template, dedupe string) (string, error) {
	c, err := store.GetContract(ctx, tx, scope, r.ContractID)
	if err != nil {
		return "", err
	}
	cust := c.BillingCustomerID
	if cust == "" {
		cust = c.CustomerID
	}
	vals := map[string]string{
		"contract_code": Code(c), "seq": strconv.Itoa(r.Seq), "amount": money.Cents(r.AmountCents).String(),
		"due_on": r.DueOn.String(),
	}
	if cs, err := s.settingsTx(ctx, tx, scope); err == nil {
		if b := s.bankDetails(cs); b != nil {
			vals["iban"], vals["bank_holder"], vals["bank"], vals["bic"] = b.IBAN, b.Holder, b.Bank, b.BIC
		}
	}
	return s.queueMailTx(ctx, tx, scope, Mail{Template: template, CustomerID: cust, DedupeKey: dedupe, Values: vals})
}

// queueMailTx resolves the customer's first email and locale and queues the mail (no mailer = no email).
func (s *Service) queueMailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, m Mail) (string, error) {
	to, locale, err := customerEmail(ctx, tx, scope, m.CustomerID)
	if err != nil {
		return "", err
	}
	if to == "" {
		return "", ErrCustomerEmailMissing
	}
	if s.mailer == nil {
		return "", nil
	}
	m.To, m.Locale = to, locale
	return s.mailer.QueueCustomerMailTx(ctx, tx, scope, m)
}

// customerEmail returns the first email and the locale of a customer ("" when none).
func customerEmail(ctx context.Context, db bun.IDB, scope store.MarinaScope, customerID string) (string, string, error) {
	cs, err := store.CustomersByIDs(ctx, db, scope, []string{customerID})
	if err != nil {
		return "", "", err
	}
	if len(cs) == 0 {
		return "", "", notFound(store.ErrNotFound)
	}
	var emails []struct {
		Value string `json:"value"`
	}
	_ = json.Unmarshal(cs[0].Emails, &emails)
	for _, e := range emails {
		if v := strings.TrimSpace(e.Value); v != "" {
			return v, cs[0].Locale, nil
		}
	}
	return "", cs[0].Locale, nil
}

// ContractInstallments returns the live plan of a contract and its early-payment alternative while nothing is
// issued (contracts.read).
func (s *Service) ContractInstallments(ctx context.Context, p *auth.Principal, marinaID, contractID string) (*PlanView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	c, err := store.GetContract(ctx, db, scope, contractID)
	if err != nil {
		return nil, notFound(err)
	}
	if c.Kind == KindTransit {
		return nil, auth.ErrNotFound
	}
	live, err := store.InstallmentsOfContracts(ctx, db, scope, []string{c.ID}, true)
	if err != nil {
		return nil, err
	}
	out := &PlanView{PlanVersion: c.InstallmentPlanVersion, Items: live, Total: money.Cents(c.TotalCents)}
	allScheduled := true
	for _, r := range live {
		out.Sum += money.Cents(r.AmountCents)
		if r.Status != store.InstallmentScheduled {
			allScheduled = false
		}
	}
	if allScheduled && !hasEarlyPayment(c) {
		cs, err := s.settingsTx(ctx, db, scope)
		if err != nil {
			return nil, err
		}
		pol, _ := DecodePolicy(c.Policy)
		pay, _, _ := effective(pol, cs)
		if alt, err := schedule.EarlyPaymentPlan(s.planInput(c, s.today(ctx, db, scope), money.Cents(c.TotalCents), pay)); err == nil {
			out.EarlyPayment = alt
		}
	}
	return out, nil
}

func hasEarlyPayment(c *store.Contract) bool {
	if snap := DecodeSnapshot(c.PriceSnapshot); snap != nil {
		for _, a := range snap.Adjustments {
			if a.Kind == AdjustmentEarlyPayment {
				return true
			}
		}
	}
	return false
}

// ReplanInput replaces the scheduled instalments of a contract.
type ReplanInput struct {
	Plan    string         // standard, early_payment
	Payment *PaymentPolicy // replaces the contract's payment clause
}

// ReplanInstallments replaces the scheduled instalments with a new plan of the remainder (contracts.write,
// If-Match on the contract).
func (s *Service) ReplanInstallments(ctx context.Context, p *auth.Principal, marinaID, contractID string, ifMatch int, in ReplanInput) (*PlanView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, contractID, KindMooring, ifMatch)
		if err != nil {
			return err
		}
		v := &auth.ValidationError{}
		if in.Plan != PlanStandard && in.Plan != PlanEarlyPayment {
			v.Add("plan", "invalid")
		}
		validatePayment(v, in.Payment)
		if err := v.Err(); err != nil {
			return err
		}
		t, err := Next(MooringTransitions, Status(c.Status), ActionUpdate)
		if err != nil {
			return err
		}
		if in.Plan == PlanEarlyPayment && hasEarlyPayment(c) {
			return &TransitionError{From: c.Status, Action: ActionUpdate}
		}
		if in.Payment != nil {
			pol, err := DecodePolicy(c.Policy)
			if err != nil {
				return err
			}
			pay := *in.Payment
			pol.Payment, pol.Version = &pay, PolicyVersion
			c.Policy = store.MustJSON(pol)
		}
		if _, err := s.replanTx(ctx, tx, scope, a, c, in.Plan); err != nil {
			return err
		}
		if err := updateContract(ctx, tx, scope, c.Version, c, s.now()); err != nil {
			return err
		}
		if err := s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: event.ChangeReplanned}); err != nil {
			return err
		}
		reason := RepriceReplan
		if in.Plan == PlanEarlyPayment {
			reason = RepriceEarlyPayment
		}
		return s.repricedTx(ctx, tx, scope, a, c, reason)
	})
	if err != nil {
		return nil, err
	}
	return s.ContractInstallments(ctx, p, marinaID, contractID)
}

// InstallmentRow is an instalment of the scadenzario with its contract.
type InstallmentRow struct {
	store.Installment
	ContractCode string
	Customer     *Party
	Overdue      bool
	Reference    string // settlement reference, only with payments.collect
}

// InstallmentFilter narrows ListInstallments.
type InstallmentFilter struct {
	From, To   *calendar.Date
	Status     string // scheduled, issued, partially_paid, paid, cancelled, overdue ("" = live)
	CustomerID string
	ContractID string
	GroupID    string
}

var installmentStatuses = []string{
	store.InstallmentScheduled, store.InstallmentIssued, store.InstallmentPartiallyPaid, store.InstallmentPaid,
	store.InstallmentCancelled, "overdue",
}

func (f InstallmentFilter) store(today calendar.Date) (store.InstallmentFilter, error) {
	if f.Status != "" && !contains(installmentStatuses, f.Status) {
		v := &auth.ValidationError{}
		v.Add("status", "invalid")
		return store.InstallmentFilter{}, v
	}
	return store.InstallmentFilter{
		From: f.From, To: f.To, Status: f.Status, Today: today, CustomerID: f.CustomerID,
		ContractID: f.ContractID, GroupID: f.GroupID,
	}, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ListInstallments returns a page of the scadenzario (contracts.read).
func (s *Service) ListInstallments(ctx context.Context, p *auth.Principal, marinaID string, f InstallmentFilter, cursor string, limit int) (store.Page[InstallmentRow], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return store.Page[InstallmentRow]{}, err
	}
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	sf, err := f.store(today)
	if err != nil {
		return store.Page[InstallmentRow]{}, err
	}
	page, err := store.ListInstallmentRows(ctx, db, scope, sf, cursor, limit)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCursor) {
			v := &auth.ValidationError{}
			v.Add("cursor", "invalid")
			return store.Page[InstallmentRow]{}, v
		}
		return store.Page[InstallmentRow]{}, err
	}
	rows, err := s.installmentRows(ctx, db, scope, p, page.Items, today)
	return store.Page[InstallmentRow]{Items: rows, NextCursor: page.NextCursor}, err
}

func (s *Service) installmentRows(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, items []store.Installment, today calendar.Date) ([]InstallmentRow, error) {
	cids := make([]string, 0, len(items))
	for _, r := range items {
		cids = append(cids, r.ContractID)
	}
	cs, err := store.ContractsByIDs(ctx, db, scope, cids)
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.Contract{}
	var custIDs []string
	for i := range cs {
		byID[cs[i].ID] = &cs[i]
		custIDs = append(custIDs, cs[i].CustomerID)
	}
	ps, err := parties(ctx, db, scope, custIDs, s.can(ctx, p, scope.ID(), auth.PermCustomersRead))
	if err != nil {
		return nil, err
	}
	withRef := s.can(ctx, p, scope.ID(), auth.PermPaymentsCollect)
	out := make([]InstallmentRow, 0, len(items))
	for _, r := range items {
		row := InstallmentRow{Installment: r, Overdue: r.Status == store.InstallmentIssued && r.DueOn.Before(today)}
		if c := byID[r.ContractID]; c != nil {
			row.ContractCode = Code(c)
			row.Customer = partyOf(ps, c.CustomerID)
		}
		if withRef && r.SettlementRefEnc != "" && s.d.Keys != nil {
			row.Reference, _ = s.d.Keys.DecryptString(r.SettlementRefEnc, settlementAAD(r.ID))
		}
		out = append(out, row)
	}
	return out, nil
}

// InstallmentsCSV exports the scadenzario of the filter as CSV (Italian: ';', decimal comma; contracts.read).
// Customer names appear only with customers.read.
func (s *Service) InstallmentsCSV(ctx context.Context, p *auth.Principal, marinaID string, f InstallmentFilter) ([]byte, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	today := s.today(ctx, db, scope)
	sf, err := f.store(today)
	if err != nil {
		return nil, err
	}
	items, err := store.AllInstallmentRows(ctx, db, scope, sf, maxCSVRows)
	if err != nil {
		return nil, err
	}
	rows, err := s.installmentRows(ctx, db, scope, p, items, today)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	_ = w.Write([]string{"contratto", "cliente", "rata", "tipo", "emissione", "scadenza", "importo", "stato", "scaduta", "saldata_il", "metodo"})
	for _, r := range rows {
		name := ""
		if r.Customer != nil {
			name = r.Customer.Name
		}
		overdue := "no"
		if r.Overdue {
			overdue = "si"
		}
		settled := ""
		if !r.SettledOn.IsZero() {
			settled = r.SettledOn.String()
		}
		_ = w.Write([]string{
			r.ContractCode, csvSafe(name), strconv.Itoa(r.Seq), r.Kind, r.IssueOn.String(), r.DueOn.String(),
			strings.Replace(money.Cents(r.AmountCents).String(), ".", ",", 1), r.Status, overdue, settled, r.SettlementMethod,
		})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// csvSafe neutralises spreadsheet formulas in free text.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// BulkResult counts a bulk action.
type BulkResult struct {
	Done, Skipped int
}

func checkBulkIDs(ids []string) error {
	v := &auth.ValidationError{}
	if len(ids) == 0 {
		v.Add("ids", "required")
	} else if len(ids) > maxBulk {
		v.Add("ids", "too_many")
	}
	return v.Err()
}

// IssueInstallments issues the scheduled instalments among ids now (contracts.write); the others are skipped.
func (s *Service) IssueInstallments(ctx context.Context, p *auth.Principal, marinaID string, ids []string) (BulkResult, error) {
	var res BulkResult
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return res, err
	}
	if err := checkBulkIDs(ids); err != nil {
		return res, err
	}
	a := actor{p}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		res = BulkResult{}
		rows, err := store.InstallmentsByIDs(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		res.Skipped = len(uniq(ids)) - len(rows)
		for i := range rows {
			ok, err := s.issueOneTx(ctx, tx, scope, a, &rows[i])
			if err != nil {
				return err
			}
			if ok {
				res.Done++
			} else {
				res.Skipped++
			}
		}
		return nil
	})
	return res, err
}

func uniq(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// RemindResult counts a reminder send.
type RemindResult struct {
	Queued, SkippedNoEmail, Skipped int
}

// RemindInstallments queues a reminder email for each issued instalment among ids (contracts.write).
func (s *Service) RemindInstallments(ctx context.Context, p *auth.Principal, marinaID string, ids []string) (RemindResult, error) {
	var res RemindResult
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return res, err
	}
	if err := checkBulkIDs(ids); err != nil {
		return res, err
	}
	a := actor{p}
	day := s.now().Format(time.DateOnly)
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		res = RemindResult{}
		rows, err := store.InstallmentsByIDs(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		res.Skipped = len(uniq(ids)) - len(rows)
		for i := range rows {
			r := &rows[i]
			if r.Status != store.InstallmentIssued {
				res.Skipped++
				continue
			}
			if _, err := s.installmentMailTx(ctx, tx, scope, r, MailInstallmentReminder, "installment:"+r.ID+":reminder:"+day); err != nil {
				if errors.Is(err, ErrCustomerEmailMissing) {
					res.SkippedNoEmail++
					continue
				}
				return err
			}
			res.Queued++
			if err := s.writeAudit(ctx, tx, scope, a, "installment.remind", entityInstallment, r.ID, map[string]any{
				"contract_id": r.ContractID, "seq": r.Seq,
			}); err != nil {
				return err
			}
			if err := s.emit(ctx, tx, scope, event.InstallmentChanged, event.InstallmentChangedData{
				InstallmentID: r.ID, ContractID: r.ContractID, Change: event.ChangeReminded, PlanVersion: r.PlanVersion,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return res, err
}

// IssueResult counts one run of the installments.issue job.
type IssueResult struct {
	Issued, Overdue, Holder int
}

// RunInstallmentsIssue is the installments.issue job (every 10 minutes, idempotent): per marina, scheduled
// instalments with issue_on ≤ today become issued (payment request email, dedupe installment:<id>:issued) in
// batches of 500, one transaction per batch; issued instalments past due are notified once (installment.overdue);
// holder instalments are issued by rights (HolderIssuer).
func (s *Service) RunInstallmentsIssue(ctx context.Context) error {
	_, err := s.RunInstallmentsIssueCount(ctx)
	return err
}

// RunInstallmentsIssueCount is RunInstallmentsIssue returning the counts (tests, seed).
func (s *Service) RunInstallmentsIssueCount(ctx context.Context) (IssueResult, error) {
	var total IssueResult
	ids, err := store.MarinaIDsForJobs(ctx, s.d.DB.DB)
	if err != nil {
		return total, err
	}
	var errs []error
	for _, id := range ids {
		r, err := s.IssueMarina(ctx, id)
		if err != nil {
			s.d.Log.Error("installments issue failed", "marina_id", id, "err", err)
			errs = append(errs, err)
		}
		total.Issued += r.Issued
		total.Overdue += r.Overdue
		total.Holder += r.Holder
	}
	return total, errors.Join(errs...)
}

// IssueMarina runs the issue job for one marina.
func (s *Service) IssueMarina(ctx context.Context, marinaID string) (IssueResult, error) {
	var r IssueResult
	scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, marinaID)
	if err != nil {
		return r, err
	}
	today := s.today(ctx, s.d.DB.DB, scope)
	for range 100 { // at most 50 000 rows per run
		n := 0
		err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			n = 0
			rows, err := store.DueToIssue(ctx, tx, scope, today, issueBatch)
			if err != nil {
				return err
			}
			// Contracts are locked in id order (issueOneTx) so parallel runs cannot deadlock.
			slices.SortStableFunc(rows, func(a, b store.Installment) int { return strings.Compare(a.ContractID, b.ContractID) })
			for i := range rows {
				ok, err := s.issueOneTx(ctx, tx, scope, system, &rows[i])
				if err != nil {
					return fmt.Errorf("issue %s: %w", rows[i].ID, err)
				}
				if ok {
					n++
				}
			}
			if len(rows) < issueBatch {
				n = -n - 1 // last batch
			}
			return nil
		})
		if err != nil {
			return r, err
		}
		if n < 0 {
			r.Issued += -n - 1
			break
		}
		r.Issued += n
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		rows, err := store.OverdueToNotify(ctx, tx, scope, today, issueBatch)
		if err != nil {
			return err
		}
		r.Overdue = 0
		for _, row := range rows {
			ok, err := store.MarkOverdueNotified(ctx, tx, scope, row.ID, s.now())
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			r.Overdue++
			if err := s.emit(ctx, tx, scope, event.InstallmentOverdue, event.InstallmentOverdueData{
				InstallmentID: row.ID, ContractID: row.ContractID, Seq: row.Seq, DueOn: row.DueOn, AmountCents: row.AmountCents,
			}); err != nil {
				return err
			}
		}
		if s.d.Holders != nil {
			h, err := s.d.Holders.IssueDueTx(ctx, tx, scope, today, issueBatch)
			if err != nil {
				return fmt.Errorf("holder instalments: %w", err)
			}
			r.Holder = h
		}
		return nil
	})
	return r, err
}
