package rights

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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

// Holder instalments (m4-plan §4.6, §15 D19): the due amounts of the holders of rights for fees, common charges
// and transfer fees. Planned with the pure schedule package (Σ = amount, INV-31), issued by the
// installments.issue job (IssueDueTx) and paid only through SettleHolderInstallment(s) (INV-32).

// Plan is the payment policy of a run, with the JSON shape of the API PaymentPolicy (same as contract
// policy.payment).
type Plan struct {
	Deposit *struct {
		BP    *int64 `json:"bp,omitempty"`
		Cents *int64 `json:"cents,omitempty"`
	} `json:"deposit,omitempty"`
	DepositDueDays int `json:"deposit_due_days,omitempty"`
	Schedule       *struct {
		Mode  string `json:"mode"`
		Dates []struct {
			DueOn string `json:"due_on"`
			BP    int64  `json:"bp"`
		} `json:"dates,omitempty"`
		EveryMonths          int `json:"every_months,omitempty"`
		Count                int `json:"count,omitempty"`
		FirstDueOffsetMonths int `json:"first_due_offset_months,omitempty"`
		DueOffsetDays        int `json:"due_offset_days,omitempty"`
	} `json:"schedule,omitempty"`
	IssueDaysBeforeDue int   `json:"issue_days_before_due,omitempty"`
	RoundingUnitCents  int64 `json:"rounding_unit_cents,omitempty"`
}

// DefaultPlan is the plan of a run that names none: one instalment due 30 days after issue.
const DefaultPlan = `{"schedule":{"mode":"single","due_offset_days":30}}`

// ParsePlan decodes and validates a plan (empty = DefaultPlan) and returns it normalised.
func ParsePlan(raw []byte) (store.JSON, schedule.PaymentPolicy, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "{}" || string(raw) == "null" {
		raw = []byte(DefaultPlan)
	}
	var p Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, schedule.PaymentPolicy{}, invalidField("plan", "invalid")
	}
	if p.Schedule == nil {
		var d Plan
		_ = json.Unmarshal([]byte(DefaultPlan), &d)
		p.Schedule = d.Schedule
	}
	pol := schedule.PaymentPolicy{
		DepositDueDays: p.DepositDueDays, IssueDaysBeforeDue: p.IssueDaysBeforeDue, RoundingUnit: money.Cents(p.RoundingUnitCents),
		Schedule: schedule.Schedule{
			Mode: p.Schedule.Mode, EveryMonths: p.Schedule.EveryMonths, Count: p.Schedule.Count,
			FirstDueOffsetMonths: p.Schedule.FirstDueOffsetMonths, DueOffsetDays: p.Schedule.DueOffsetDays,
		},
	}
	if p.Deposit != nil {
		pol.Deposit = &schedule.Deposit{}
		if p.Deposit.BP != nil {
			pol.Deposit.BP = *p.Deposit.BP
		}
		if p.Deposit.Cents != nil {
			pol.Deposit.Cents = money.Cents(*p.Deposit.Cents)
		}
	}
	for _, d := range p.Schedule.Dates {
		fd := schedule.FixedDue{BP: d.BP}
		if len(d.DueOn) == 10 {
			date, err := calendar.Parse(d.DueOn)
			if err != nil {
				return nil, schedule.PaymentPolicy{}, invalidField("plan", "invalid")
			}
			fd.Date = date
		} else {
			fd.MonthDay = d.DueOn
		}
		pol.Schedule.Dates = append(pol.Schedule.Dates, fd)
	}
	if err := schedule.Validate(pol); err != nil {
		return nil, schedule.PaymentPolicy{}, invalidField("plan", "invalid")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, schedule.PaymentPolicy{}, err
	}
	return store.JSON(b), pol, nil
}

func invalidField(field, code string) error {
	v := &auth.ValidationError{}
	v.Add(field, code)
	return v
}

// planHolder returns the instalments of one holder amount over a period [from, to) with the plan (a negative
// amount, a common charge credit, is one credit due at the period start or today). Σ = amount (INV-31).
func planHolder(amount money.Cents, pol schedule.PaymentPolicy, from, to, today calendar.Date) ([]schedule.Item, error) {
	if amount < 0 {
		due := from
		if due.Before(today) {
			due = today
		}
		return []schedule.Item{{Seq: 1, Kind: schedule.KindSingle, IssueOn: today, DueOn: due, Amount: amount}}, nil
	}
	if !to.After(today) {
		to = today.AddDays(1)
	}
	return schedule.Plan(schedule.Input{Total: amount, StartsOn: from, EndsOn: to, Today: today, Payment: pol})
}

// holderRowsOf builds the holder instalment rows of planned items.
func holderRowsOf(items []schedule.Item, source, sourceID, runID, rightID, customerID string, firstSeq int, now time.Time) []store.HolderInstallment {
	out := make([]store.HolderInstallment, 0, len(items))
	for i, it := range items {
		kind := "installment"
		if it.Amount < 0 {
			kind = "credit"
		}
		out = append(out, store.HolderInstallment{
			ID: store.NewID(), SourceType: source, SourceID: sourceID, RunID: runID, RightID: rightID, CustomerID: customerID,
			Seq: firstSeq + i, Kind: kind, IssueOn: it.IssueOn, DueOn: it.DueOn, AmountCents: int64(it.Amount),
			Status: store.InstallmentScheduled, Version: 1, CreatedAt: now, UpdatedAt: now,
		})
	}
	return out
}

// HolderInstallment is a holder instalment with its overdue flag. DisplayName only with customers.read.
type HolderInstallment struct {
	store.HolderInstallment
	BerthID     string
	DisplayName string
	Overdue     bool
}

// HolderInstallmentFilter narrows ListHolderInstallments.
type HolderInstallmentFilter = store.HolderInstallmentFilter

var installmentStatuses = []string{"scheduled", "issued", "partially_paid", "paid", "cancelled", "overdue"}

func (s *Service) holderViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.HolderInstallment, today calendar.Date) ([]HolderInstallment, error) {
	var cids, rids []string
	for _, r := range rows {
		cids = append(cids, r.CustomerID)
		if r.RightID != "" {
			rids = append(rids, r.RightID)
		}
	}
	nm, err := names(ctx, db, scope, can(p, scope, auth.PermCustomersRead), cids)
	if err != nil {
		return nil, err
	}
	berth := map[string]string{}
	for _, id := range rids {
		if _, ok := berth[id]; ok {
			continue
		}
		r, err := store.GetBerthRight(ctx, db, scope, id)
		if err == nil {
			berth[id] = r.BerthID
		}
	}
	out := make([]HolderInstallment, len(rows))
	for i, r := range rows {
		out[i] = HolderInstallment{
			HolderInstallment: r, BerthID: berth[r.RightID], DisplayName: nm[r.CustomerID],
			Overdue: (r.Status == store.InstallmentIssued || r.Status == store.InstallmentPartiallyPaid) && r.DueOn.Before(today),
		}
	}
	return out, nil
}

// ListHolderInstallments returns a page of holder instalments by due date (rights.manage).
func (s *Service) ListHolderInstallments(ctx context.Context, p *auth.Principal, marinaID string, f HolderInstallmentFilter, cursor string, limit int) (store.Page[HolderInstallment], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return store.Page[HolderInstallment]{}, err
	}
	if f.Status != "" && !slices.Contains(installmentStatuses, f.Status) {
		return store.Page[HolderInstallment]{}, invalidField("status", "invalid")
	}
	var out store.Page[HolderInstallment]
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		f.Today = s.today(ctx, tx, scope)
		page, err := store.ListHolderInstallments(ctx, tx, scope, f, cursor, limit)
		if err != nil {
			return err
		}
		items, err := s.holderViews(ctx, tx, scope, p, page.Items, f.Today)
		out = store.Page[HolderInstallment]{Items: items, NextCursor: page.NextCursor}
		return err
	})
	return out, err
}

// AllHolderInstallments returns every holder instalment of f (CSV export, rights.manage).
func (s *Service) AllHolderInstallments(ctx context.Context, p *auth.Principal, marinaID string, f HolderInstallmentFilter) ([]HolderInstallment, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	if f.Status != "" && !slices.Contains(installmentStatuses, f.Status) {
		return nil, invalidField("status", "invalid")
	}
	var out []HolderInstallment
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		f.Today = s.today(ctx, tx, scope)
		rows, err := store.AllHolderInstallments(ctx, tx, scope, f)
		if err != nil {
			return err
		}
		out, err = s.holderViews(ctx, tx, scope, p, rows, f.Today)
		return err
	})
	return out, err
}

// SettleInput is a manual settlement of a holder instalment.
type SettleInput struct {
	Method    string
	Reference string // stored encrypted (AAD holder-installment-ref:<id>), never logged or emitted
	SettledOn calendar.Date
}

// SettleItem is one instalment of a bulk settlement with the version the client saw (0 = any).
type SettleItem struct {
	ID      string
	Version int
}

var settlementMethods = []string{"bank_transfer", "cash", "card_pos", "cheque", "other"}

func holderRefAAD(id string) string { return "holder-installment-ref:" + id }

func (s *Service) checkSettle(ctx context.Context, scope store.MarinaScope, in *SettleInput) error {
	v := &auth.ValidationError{}
	if !slices.Contains(settlementMethods, in.Method) {
		v.Add("method", "invalid")
	}
	in.Reference = strings.TrimSpace(in.Reference)
	if len(in.Reference) > 200 {
		v.Add("reference", "too_long")
	}
	today := s.today(ctx, s.d.DB.DB, scope)
	if in.SettledOn.IsZero() {
		in.SettledOn = today
	} else if in.SettledOn.After(today) {
		v.Add("settled_on", "in_future")
	}
	return v.Err()
}

// SettleHolderInstallment records the manual settlement of a holder instalment (payments.collect, If-Match on
// the instalment version). It is, with SettleHolderInstallments, the only path that makes one paid (INV-32);
// M5 payment matching will call settleTx with source payment.
func (s *Service) SettleHolderInstallment(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in SettleInput) (*HolderInstallment, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	if err != nil {
		return nil, err
	}
	if err := s.checkSettle(ctx, scope, &in); err != nil {
		return nil, err
	}
	var out *HolderInstallment
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		h, err := store.GetHolderInstallment(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if err := lockHolderRights(ctx, tx, scope, []store.HolderInstallment{*h}); err != nil {
			return err
		}
		if h, err = store.GetHolderInstallment(ctx, tx, scope, id); err != nil {
			return err
		}
		if ifMatch != 0 && h.Version != ifMatch {
			return ErrVersionMismatch
		}
		if !settleable(h.Status) {
			return ErrInvalidTransition
		}
		if err := s.settleTx(ctx, tx, scope, p, h, h.Version, store.SettlementManual, in); err != nil {
			return err
		}
		vs, err := s.holderViews(ctx, tx, scope, p, []store.HolderInstallment{*h}, s.today(ctx, tx, scope))
		if err != nil {
			return err
		}
		out = &vs[0]
		return nil
	})
	return out, err
}

func settleable(status string) bool {
	return status == store.InstallmentScheduled || status == store.InstallmentIssued || status == store.InstallmentPartiallyPaid
}

// SettleHolderInstallments settles up to 500 holder instalments in one transaction, all or nothing
// (payments.collect): an instalment already paid, cancelled or at another version fails the call with 409.
func (s *Service) SettleHolderInstallments(ctx context.Context, p *auth.Principal, marinaID string, items []SettleItem, in SettleInput) (int, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPaymentsCollect)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 || len(items) > 500 {
		return 0, invalidField("items", "count")
	}
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.ID] {
			return 0, invalidField("items", "duplicate")
		}
		seen[it.ID] = true
	}
	if err := s.checkSettle(ctx, scope, &in); err != nil {
		return 0, err
	}
	n := 0
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		n = 0
		pre := make([]store.HolderInstallment, 0, len(items))
		for _, it := range items {
			h, err := store.GetHolderInstallment(ctx, tx, scope, it.ID)
			if err != nil {
				return err
			}
			pre = append(pre, *h)
		}
		if err := lockHolderRights(ctx, tx, scope, pre); err != nil {
			return err
		}
		for _, it := range items {
			h, err := store.GetHolderInstallment(ctx, tx, scope, it.ID)
			if err != nil {
				return err
			}
			expected := it.Version
			if expected == 0 {
				expected = h.Version
			}
			if h.Version != expected || !settleable(h.Status) {
				return store.ErrConflict
			}
			if err := s.settleTx(ctx, tx, scope, p, h, expected, store.SettlementManual, in); err != nil {
				if errors.Is(err, store.ErrVersionMismatch) {
					return store.ErrConflict
				}
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// settleTx marks one holder instalment paid (conditional on status and version), audits it and emits
// rights.changed.
func (s *Service) settleTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, h *store.HolderInstallment, expected int, source string, in SettleInput) error {
	// Re-entrant when the caller already holds it; M5 payment matching gets the lock order for free.
	if err := lockHolderRights(ctx, tx, scope, []store.HolderInstallment{*h}); err != nil {
		return err
	}
	now := s.now()
	from := h.Status
	h.Status, h.SettlementSource, h.SettlementMethod = store.InstallmentPaid, source, in.Method
	h.SettledOn, h.SettledAt, h.SettledByUserID, h.UpdatedAt = in.SettledOn, now, userOf(p), now
	if in.Reference != "" {
		enc, err := s.d.Keys.EncryptString(in.Reference, holderRefAAD(h.ID))
		if err != nil {
			return err
		}
		h.SettlementRefEnc = enc
	}
	if err := store.SettleHolderInstallmentRow(ctx, tx, scope, h, expected); err != nil {
		return err
	}
	return s.write(ctx, tx, actorOf(p), scope, "holder_installment.settle", "holder_installment", h.ID, map[string]any{
		"source_type": h.SourceType, "amount_cents": h.AmountCents, "from_status": from, "status": h.Status, "source": source,
		"method": in.Method, "settled_on": in.SettledOn,
	}, changed("holder_installment", h.ID, event.ChangeSettled))
}

// cancelRightInstallments cancels the scheduled holder instalments of a right due on or after from (zero = all).
func (s *Service) cancelRightInstallments(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rightID string, from calendar.Date, now time.Time) error {
	rows, err := store.AllHolderInstallments(ctx, tx, scope, store.HolderInstallmentFilter{RightID: rightID, Status: store.InstallmentScheduled, From: from})
	if err != nil {
		return err
	}
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	n, err := store.CancelHolderInstallments(ctx, tx, scope, ids, now)
	if err != nil {
		return err
	}
	if n != len(ids) {
		return ErrVersionMismatch // issued or paid since the read: the caller retries on fresh data
	}
	return nil
}

// lockHolderRights takes the per-right lock (store.LockBerthRight) of the rights of some holder instalments, in
// right id order. Issuing and settling a holder instalment hold it so that a transfer or cancellation of the right
// (which reads and cancels its scheduled instalments under the same lock) never races them: lock order right →
// holder instalments (INV-31). Instalments without a right (a common charge of a holder of several rights) are
// never moved by right changes and need no lock.
func lockHolderRights(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rows []store.HolderInstallment) error {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		if rows[i].RightID != "" {
			ids = append(ids, rows[i].RightID)
		}
	}
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		if _, err := store.LockBerthRight(ctx, tx, scope, id); err != nil {
			return err
		}
	}
	return nil
}

// queuePaymentRequest queues the payment request email of an issued holder instalment. The M4 email templates and
// their enqueue entry point belong to DOCUMENTS (internal/domain/notify); until it exposes one for holder
// instalments, issuing only changes the status (the scadenzario shows it as issued).
func (s *Service) queuePaymentRequest(ctx context.Context, tx bun.IDB, scope store.MarinaScope, h *store.HolderInstallment) error {
	if pr, ok := any(s.d.Notify).(holderPaymentRequester); ok && s.d.Notify != nil {
		return pr.EnqueueHolderPaymentRequest(ctx, tx, scope, h.ID, h.CustomerID)
	}
	return nil
}

// holderPaymentRequester is the notify entry point for the payment request of a holder instalment (implemented
// by DOCUMENTS when the M4 templates land; the dedupe key is per instalment).
type holderPaymentRequester interface {
	EnqueueHolderPaymentRequest(ctx context.Context, tx bun.IDB, scope store.MarinaScope, holderInstallmentID, customerID string) error
}
