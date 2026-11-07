package collect

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/collect/levels"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/notify"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/store"
)

// Reminders (m5-plan §5.5, §16 D13, INV-58): once per marina day after reminder_hour, every open charge not
// excluded gets the next level reached (pre-due, due, courteous, formal, final notice), each (charge, level) at most
// once (DB unique); one email per customer and level per run with a payment link; final notices wait for approval;
// a charge paid meanwhile is skipped (re-read under its lock). The run also applies the arrears policy.

// MaxManualCharges bounds a manual "sollecita".
const MaxManualCharges = 500

// ReminderTemplate returns the email template of a level.
func ReminderTemplate(level string) string {
	switch level {
	case levels.PreDue:
		return notify.TemplateReminderPreDue
	case levels.Due:
		return notify.TemplateReminderDue
	case levels.Courteous:
		return notify.TemplateReminderCourteous
	case levels.Formal:
		return notify.TemplateReminderFormal
	}
	return notify.TemplateReminderFinalNotice
}

// ArrearsPolicy is billing_settings.arrears_policy.
type ArrearsPolicy struct {
	Enabled        bool  `json:"enabled"`
	DaysOverdue    int   `json:"days_overdue"`
	MinAmountCents int64 `json:"min_amount_cents"`
}

func parseArrears(raw []byte) ArrearsPolicy {
	p := ArrearsPolicy{DaysOverdue: 60}
	_ = json.Unmarshal(raw, &p)
	if p.DaysOverdue < 1 {
		p.DaysOverdue = 60
	}
	return p
}

// RunReminders is the job payments.reminders: each marina runs once per business day after its reminder hour.
func (s *Service) RunReminders(ctx context.Context) error {
	ids, err := store.MarinaIDsForJobs(ctx, s.d.DB.DB)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		scope, err := store.SystemMarinaScope(ctx, s.d.DB.DB, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		bs, err := store.GetBillingSettings(ctx, s.d.DB.DB, scope, s.now())
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if s.d.Clock.Now().In(s.location(ctx, s.d.DB.DB, scope)).Hour() < bs.ReminderHour {
			continue
		}
		if _, err := s.RunDayTx(ctx, scope, jobActor("payments.reminders")); err != nil {
			s.d.Log.ErrorContext(ctx, "reminders run failed", "marina_id", id, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RunNow runs today's reminders of a marina (charges.manage); a day that already ran returns its run.
func (s *Service) RunNow(ctx context.Context, p *auth.Principal, marinaID string) (*store.ReminderRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	return s.RunDayTx(ctx, scope, p.Actor())
}

type reminderPick struct {
	charge store.Charge
	level  string
}

// RunDayTx runs the reminders of today in one transaction: the run row (unique per day) makes a second concurrent
// run a no-op that returns the first one.
func (s *Service) RunDayTx(ctx context.Context, scope store.MarinaScope, actor audit.Actor) (*store.ReminderRun, error) {
	var out *store.ReminderRun
	err := s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		today := s.today(ctx, tx, scope)
		now := s.now()
		run, created, err := store.InsertReminderRun(ctx, tx, scope, &store.ReminderRun{
			ID: store.NewID(), MarinaID: scope.ID(), RunOn: today, Status: store.ReminderRunRunning, Counts: store.JSON(`{}`),
			StartedAt: now, CreatedByActor: actorString(actor), CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		out = run
		if !created {
			return nil
		}
		bs, err := store.GetBillingSettings(ctx, tx, scope, now)
		if err != nil {
			return err
		}
		pol := levels.ParsePolicy(bs.ReminderPolicy)
		cands, err := store.OpenChargesForReminders(ctx, tx, scope, today.AddDays(pol.PreDueDays))
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(cands))
		for _, c := range cands {
			ids = append(ids, c.ID)
		}
		sent, err := store.ReminderLevelsSent(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		var picks []reminderPick
		for _, c := range cands {
			if l, ok := levels.Next(c.DueOn, today, pol, lastLevel(sent[c.ID])); ok {
				picks = append(picks, reminderPick{charge: c, level: l})
			}
		}
		// Arrears first: it locks contracts, queueTx then locks charges (lock order contract, payment, charges).
		suspended, err := s.arrearsTx(ctx, tx, scope, today, parseArrears(bs.ArrearsPolicy))
		if err != nil {
			return err
		}
		counts, err := s.queueTx(ctx, tx, scope, actor, run.ID, today, picks)
		if err != nil {
			return err
		}
		if suspended > 0 {
			counts["suspended"] = suspended
		}
		run.Status, run.Counts, run.FinishedAt = store.ReminderRunDone, store.MustJSON(counts), s.now()
		if err := store.FinishReminderRun(ctx, tx, scope, run.ID, run.Status, run.Counts, run.FinishedAt); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, actor, "reminder.run", "reminder_run", run.ID, map[string]any{"run_on": today, "counts": counts}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.ReminderChanged, event.ReminderChangedData{RunID: run.ID, Change: event.ChangeRun, Counts: counts})
	})
	return out, err
}

func lastLevel(sent []string) string {
	last := ""
	for _, l := range sent {
		if levels.Rank(l) > levels.Rank(last) {
			last = l
		}
	}
	return last
}

// queueTx records the items of picks and sends one email per customer and level (grouping the charges of the
// customer at that level, with a payment request for them). runID "" means a manual reminder.
func (s *Service) queueTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, runID string, today calendar.Date, picks []reminderPick) (map[string]int, error) {
	counts := map[string]int{}
	if len(picks) == 0 {
		return counts, nil
	}
	ids := make([]string, 0, len(picks))
	for _, p := range picks {
		ids = append(ids, p.charge.ID)
	}
	locked, err := store.LockChargesSorted(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	fresh := map[string]store.Charge{}
	for _, c := range locked {
		fresh[c.ID] = c
	}
	excl, err := store.ActiveExclusions(ctx, tx, scope, today)
	if err != nil {
		return nil, err
	}
	exCust, exCharge := map[string]bool{}, map[string]bool{}
	for _, e := range excl {
		if e.CustomerID != "" {
			exCust[e.CustomerID] = true
		}
		if e.ChargeID != "" {
			exCharge[e.ChargeID] = true
		}
	}
	type group struct {
		customer, level string
		items           []*store.ReminderItem
		charges         []store.Charge
	}
	groups := map[string]*group{}
	var order []string
	now := s.now()
	for _, p := range picks {
		c := fresh[p.charge.ID]
		it := &store.ReminderItem{
			ID: store.NewID(), MarinaID: scope.ID(), RunID: runID, ChargeID: c.ID, CustomerID: c.CustomerID, Level: p.level,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		switch {
		case c.ResidualCents() <= 0:
			it.Status = ItemSkippedPaid
		case exCust[c.CustomerID] || exCharge[c.ID]:
			it.Status = ItemSkippedExcluded
		case levels.NeedsApproval(p.level):
			it.Status = ItemAwaitingApproval
		default:
			it.Status = ItemQueued
		}
		ok, err := store.InsertReminderItem(ctx, tx, it)
		if err != nil {
			return nil, err
		}
		if !ok {
			counts["duplicate"]++
			continue // INV-58: this level was already recorded for the charge
		}
		if it.Status != ItemQueued {
			counts[it.Status]++
			continue
		}
		k := c.CustomerID + "|" + p.level
		g, ok := groups[k]
		if !ok {
			g = &group{customer: c.CustomerID, level: p.level}
			groups[k] = g
			order = append(order, k)
		}
		g.items = append(g.items, it)
		g.charges = append(g.charges, c)
	}
	for _, k := range order {
		g := groups[k]
		dedupe := "reminder:" + g.customer + ":" + g.level + ":" + today.String()
		reqID, msgID, err := s.reminderMailTx(ctx, tx, scope, actor, g.customer, g.level, g.charges, dedupe)
		status := ItemSent
		if errors.Is(err, ErrCustomerEmailMissing) {
			status, err = ItemSkippedNoEmail, nil
		}
		if err != nil {
			return nil, err
		}
		for _, it := range g.items {
			it.Status, it.RequestID, it.MessageID = status, reqID, msgID
			if _, err := store.UpdateReminderItem(ctx, tx, scope, it, []string{ItemQueued}, now); err != nil {
				return nil, err
			}
			counts[status]++
		}
	}
	return counts, nil
}

// reminderMailTx creates (or refreshes) the payment request of the charges and queues the reminder email.
func (s *Service) reminderMailTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, customerID, level string, charges []store.Charge, dedupe string) (string, string, error) {
	to, locale, name, err := customerContact(ctx, tx, scope, customerID)
	if err != nil {
		return "", "", err
	}
	if to == "" {
		return "", "", ErrCustomerEmailMissing
	}
	today := s.today(ctx, tx, scope)
	exp := today.AddDays(DefaultRequestDays).Time(s.location(ctx, tx, scope))
	r, tok, err := s.createRequestTx(ctx, tx, scope, actor, customerID, RequestReminder, charges, 0, exp)
	if err != nil {
		return "", "", err
	}
	if s.d.Notify == nil {
		return r.ID, "", nil
	}
	slug, marinaName, err := marinaSlug(ctx, tx, scope)
	if err != nil {
		return "", "", err
	}
	vals := s.mailValues(ctx, tx, scope, marinaName, name, charges, r.AmountCents)
	vals[notify.ValueLink] = s.PayURL(slug, tok)
	msgID, _, err := s.d.Notify.QueueMailTx(ctx, tx, notify.Mail{
		Template: ReminderTemplate(level), MarinaID: scope.ID(), CustomerID: customerID, To: to, Locale: locale,
		DedupeKey: dedupe, SourceType: "reminder", SourceID: r.ID, Values: vals,
	})
	if err != nil {
		return "", "", err
	}
	r.LastMessageID = msgID
	if err := store.UpdateRequestSent(ctx, tx, scope, r, 0, s.now()); err != nil {
		return "", "", err
	}
	return r.ID, msgID, nil
}

// SendResult counts a manual reminder.
type SendResult struct {
	Queued  int
	Skipped int
}

// SendReminders reminds up to 500 charges at their next level (or level when given and above the last one)
// through the same path as the daily run (charges.manage).
func (s *Service) SendReminders(ctx context.Context, p *auth.Principal, marinaID string, chargeIDs []string, level string) (*SendResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	if len(chargeIDs) == 0 || len(chargeIDs) > MaxManualCharges {
		return nil, invalid("charge_ids", "count")
	}
	if level != "" && levels.Rank(level) < 0 {
		return nil, invalid("level", "invalid")
	}
	out := &SendResult{}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		*out = SendResult{}
		cs, err := store.CollectChargesByIDs(ctx, tx, scope, uniq(chargeIDs))
		if err != nil {
			return err
		}
		out.Skipped = len(uniq(chargeIDs)) - len(cs)
		bs, err := store.GetBillingSettings(ctx, tx, scope, s.now())
		if err != nil {
			return err
		}
		pol := levels.ParsePolicy(bs.ReminderPolicy)
		today := s.today(ctx, tx, scope)
		ids := make([]string, 0, len(cs))
		for _, c := range cs {
			ids = append(ids, c.ID)
		}
		sent, err := store.ReminderLevelsSent(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		var picks []reminderPick
		for _, c := range cs {
			last := lastLevel(sent[c.ID])
			if c.CustomerID == "" || c.ResidualCents() <= 0 {
				out.Skipped++
				continue
			}
			l := level
			if l == "" || levels.Rank(l) <= levels.Rank(last) {
				var ok bool
				if l, ok = levels.NextAfter(c.DueOn, today, pol, last); !ok {
					out.Skipped++
					continue
				}
			}
			picks = append(picks, reminderPick{charge: c, level: l})
		}
		counts, err := s.queueTx(ctx, tx, scope, p.Actor(), "", today, picks)
		if err != nil {
			return err
		}
		out.Queued = counts[ItemSent] + counts[ItemAwaitingApproval]
		out.Skipped += len(picks) - out.Queued
		if err := s.audit(ctx, tx, scope, p.Actor(), "reminder.queue", "reminder", scope.ID(), map[string]any{"counts": counts}); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.ReminderChanged, event.ReminderChangedData{Change: event.ChangeQueued, Counts: counts})
	})
	return out, err
}

// ItemView is a reminder item with its charge.
type ItemView struct {
	Item   store.ReminderItem
	Charge *store.Charge
}

// ListRuns returns the runs (charges.manage).
func (s *Service) ListRuns(ctx context.Context, p *auth.Principal, marinaID, cursor string, limit int) (store.Page[store.ReminderRun], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return store.Page[store.ReminderRun]{}, err
	}
	return store.ListReminderRuns(ctx, s.d.DB.DB, scope, cursor, limit)
}

// GetRun returns a run with a page of its items (charges.manage).
func (s *Service) GetRun(ctx context.Context, p *auth.Principal, marinaID, id, cursor string, limit int) (*store.ReminderRun, store.Page[ItemView], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, store.Page[ItemView]{}, err
	}
	run, err := store.GetReminderRun(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, store.Page[ItemView]{}, err
	}
	items, err := s.ListItems(ctx, scope, store.ReminderItemFilter{RunID: id}, cursor, limit)
	return run, items, err
}

// ListItems returns items with their charges (scope already authorised).
func (s *Service) ListItems(ctx context.Context, scope store.MarinaScope, f store.ReminderItemFilter, cursor string, limit int) (store.Page[ItemView], error) {
	page, err := store.ListReminderItems(ctx, s.d.DB.DB, scope, f, cursor, limit)
	if err != nil {
		return store.Page[ItemView]{}, err
	}
	ids := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		ids = append(ids, it.ChargeID)
	}
	cs, err := store.CollectChargesByIDs(ctx, s.d.DB.DB, scope, uniq(ids))
	if err != nil {
		return store.Page[ItemView]{}, err
	}
	byID := map[string]*store.Charge{}
	for i := range cs {
		byID[cs[i].ID] = &cs[i]
	}
	out := store.Page[ItemView]{NextCursor: page.NextCursor, Items: make([]ItemView, 0, len(page.Items))}
	for _, it := range page.Items {
		out.Items = append(out.Items, ItemView{Item: it, Charge: byID[it.ChargeID]})
	}
	return out, nil
}

// PendingApprovals returns the final notices waiting for the office (worklist, charges.manage).
func (s *Service) PendingApprovals(ctx context.Context, p *auth.Principal, marinaID, cursor string, limit int) (store.Page[ItemView], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return store.Page[ItemView]{}, err
	}
	return s.ListItems(ctx, scope, store.ReminderItemFilter{Status: ItemAwaitingApproval}, cursor, limit)
}

// ApproveItem sends a final notice waiting for approval (charges.manage); a charge paid meanwhile is skipped.
func (s *Service) ApproveItem(ctx context.Context, p *auth.Principal, marinaID, id string) (*ItemView, error) {
	return s.decide(ctx, p, marinaID, id, true)
}

// DismissItem drops a final notice waiting for approval (charges.manage).
func (s *Service) DismissItem(ctx context.Context, p *auth.Principal, marinaID, id string) (*ItemView, error) {
	return s.decide(ctx, p, marinaID, id, false)
}

func (s *Service) decide(ctx context.Context, p *auth.Principal, marinaID, id string, approve bool) (*ItemView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	var out *ItemView
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		it, err := store.GetReminderItem(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if it.Status != ItemAwaitingApproval {
			return ErrItemDecided
		}
		chs, err := store.LockChargesSorted(ctx, tx, scope, []string{it.ChargeID})
		if err != nil {
			return err
		}
		ch := chs[0]
		it.DecidedByUserID = userOf(p.Actor())
		action := "reminder.dismiss"
		change := event.ChangeDismissed
		switch {
		case !approve:
			it.Status = ItemDismissed
		case ch.ResidualCents() <= 0:
			it.Status = ItemSkippedPaid
			action, change = "reminder.approve", event.ChangeApproved
		default:
			action, change = "reminder.approve", event.ChangeApproved
			reqID, msgID, err := s.reminderMailTx(ctx, tx, scope, p.Actor(), it.CustomerID, it.Level, []store.Charge{ch}, "reminder-item:"+it.ID)
			switch {
			case errors.Is(err, ErrCustomerEmailMissing):
				it.Status = ItemSkippedNoEmail
			case err != nil:
				return err
			default:
				it.Status, it.RequestID, it.MessageID = ItemSent, reqID, msgID
			}
		}
		ok, err := store.UpdateReminderItem(ctx, tx, scope, it, []string{ItemAwaitingApproval}, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrItemDecided
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), action, "reminder_item", it.ID, map[string]any{"status": it.Status, "level": it.Level, "charge_id": it.ChargeID}); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, scope, event.ReminderChanged, event.ReminderChangedData{ItemID: it.ID, Level: it.Level, Change: change}); err != nil {
			return err
		}
		out = &ItemView{Item: *it, Charge: &ch}
		return nil
	})
	return out, err
}

// ExclusionInput excludes a customer or a charge from reminders.
type ExclusionInput struct {
	CustomerID string
	ChargeID   string
	Kind       string
	Until      calendar.Date
	Note       string
}

// Exclusion kinds.
var exclusionKinds = []string{"payment_plan", "disputed", "other"}

// ListExclusions returns exclusions (charges.manage).
func (s *Service) ListExclusions(ctx context.Context, p *auth.Principal, marinaID string, f store.DunningExclusionFilter, active bool, cursor string, limit int) (store.Page[store.DunningExclusion], calendar.Date, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return store.Page[store.DunningExclusion]{}, calendar.Date{}, err
	}
	today := s.today(ctx, s.d.DB.DB, scope)
	if active {
		f.ActiveOn = today
	}
	page, err := store.ListDunningExclusions(ctx, s.d.DB.DB, scope, f, cursor, limit)
	return page, today, err
}

// CreateExclusion creates an exclusion (charges.manage): exactly one of customer and charge.
func (s *Service) CreateExclusion(ctx context.Context, p *auth.Principal, marinaID string, in ExclusionInput) (*store.DunningExclusion, calendar.Date, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, calendar.Date{}, err
	}
	v := &auth.ValidationError{}
	if (in.CustomerID == "") == (in.ChargeID == "") {
		v.Add("customer_id", "exactly_one")
	}
	if !slices.Contains(exclusionKinds, in.Kind) {
		v.Add("kind", "invalid")
	}
	in.Note = strings.TrimSpace(in.Note)
	if len(in.Note) > 1000 {
		v.Add("note", "too_long")
	}
	today := s.today(ctx, s.d.DB.DB, scope)
	if !in.Until.IsZero() && in.Until.Before(today) {
		v.Add("until", "in_past")
	}
	if len(v.Fields) > 0 {
		return nil, today, v
	}
	var out *store.DunningExclusion
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if in.CustomerID != "" {
			if err := s.customerExists(ctx, tx, scope, in.CustomerID); err != nil {
				return err
			}
		} else if cs, err := store.CollectChargesByIDs(ctx, tx, scope, []string{in.ChargeID}); err != nil {
			return err
		} else if len(cs) == 0 {
			return ErrNotFound
		}
		now := s.now()
		e := &store.DunningExclusion{
			ID: store.NewID(), MarinaID: scope.ID(), CustomerID: in.CustomerID, ChargeID: in.ChargeID, Kind: in.Kind,
			UntilOn: in.Until, Note: in.Note, CreatedByUserID: userOf(p.Actor()), CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertDunningExclusion(ctx, tx, e); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "dunning_exclusion.create", "dunning_exclusion", e.ID, map[string]any{
			"customer_id": e.CustomerID, "charge_id": e.ChargeID, "kind": e.Kind, "until": e.UntilOn,
		}); err != nil {
			return err
		}
		out = e
		return s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "exclusion", ID: e.ID, Change: event.ChangeCreated})
	})
	return out, today, err
}

// RemoveExclusion removes an exclusion (charges.manage).
func (s *Service) RemoveExclusion(ctx context.Context, p *auth.Principal, marinaID, id string) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetDunningExclusion(ctx, tx, scope, id); err != nil {
			return err
		}
		ok, err := store.RemoveDunningExclusion(ctx, tx, scope, id, userOf(p.Actor()), s.now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrExclusionRemoved
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "dunning_exclusion.remove", "dunning_exclusion", id, nil); err != nil {
			return err
		}
		return s.emit(ctx, tx, scope, event.BillingChanged, event.BillingChangedData{Entity: "exclusion", ID: id, Change: event.ChangeDeleted})
	})
}

// ExclusionActive reports whether an exclusion is active on day.
func ExclusionActive(e *store.DunningExclusion, day calendar.Date) bool {
	return e.RemovedAt.IsZero() && (e.UntilOn.IsZero() || !e.UntilOn.Before(day))
}

// arrearsTx applies the arrears policy (§16 D15): active mooring or charter contracts whose open charges overdue
// more than N days reach the minimum, without an active exclusion of the customer (charges with an active
// exclusion of their own do not count), are suspended (reason arrears); transits never; resume stays manual. A
// contract suspended for arrears with no overdue charge left raises an arrears_paid worklist item so that the
// office can resume it (m5-plan §5.5), whether or not the policy is still enabled.
func (s *Service) arrearsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, today calendar.Date, pol ArrearsPolicy) (int, error) {
	kinds := []string{store.ContractKindMooring, store.ContractKindCharter}
	suspended, err := store.CollectContractsByStatus(ctx, tx, scope, kinds, "suspended")
	if err != nil {
		return 0, err
	}
	var forArrears []store.Contract
	for _, c := range suspended {
		if c.SuspendReason == contract.ArrearsReason {
			forArrears = append(forArrears, c)
		}
	}
	if len(forArrears) > 0 {
		due, err := store.OverdueByContract(ctx, tx, scope, today, today)
		if err != nil {
			return 0, err
		}
		for _, c := range forArrears {
			if due[c.ID] > 0 {
				continue
			}
			if err := store.RaiseWorkItem(ctx, tx, scope, payment.WorkArrearsPaid, c.ID, arrearsOccurrence(&c), s.now()); err != nil {
				return 0, err
			}
		}
	}
	if !pol.Enabled {
		return 0, nil
	}
	overdue, err := store.OverdueByContract(ctx, tx, scope, today.AddDays(-pol.DaysOverdue), today)
	if err != nil || len(overdue) == 0 {
		return 0, err
	}
	active, err := store.CollectContractsByStatus(ctx, tx, scope, kinds, "active")
	if err != nil {
		return 0, err
	}
	excl, err := store.ActiveExclusions(ctx, tx, scope, today)
	if err != nil {
		return 0, err
	}
	exCust := map[string]bool{}
	for _, e := range excl {
		if e.CustomerID != "" {
			exCust[e.CustomerID] = true
		}
	}
	n := 0
	for _, c := range active {
		sum, ok := overdue[c.ID]
		if !ok || sum <= 0 || sum < pol.MinAmountCents {
			continue
		}
		cust := c.BillingCustomerID
		if cust == "" {
			cust = c.CustomerID
		}
		if exCust[cust] {
			continue
		}
		chk := contract.ArrearsCheck{Cutoff: today.AddDays(-pol.DaysOverdue), Today: today, MinAmountCents: pol.MinAmountCents}
		if _, err := s.d.Contracts.SuspendForArrearsTx(ctx, tx, scope, c.ID, chk); err != nil {
			// Only the domain refusals are skipped (they write nothing); any other error, a SQL one above all,
			// fails the run: on PostgreSQL the transaction is aborted and every later statement would fail.
			if !errors.Is(err, contract.ErrInvalidTransition) && !errors.Is(err, contract.ErrVersionMismatch) &&
				!errors.Is(err, contract.ErrNotFound) && !errors.Is(err, contract.ErrNotInArrears) {
				return 0, err
			}
			s.d.Log.WarnContext(ctx, "arrears suspension skipped", "marina_id", scope.ID(), "contract_id", c.ID, "error_kind", errorKind(err))
			continue
		}
		n++
	}
	return n, nil
}

// arrearsOccurrence keys the arrears_paid item of a contract by its suspension, so that a later arrears cycle
// of the same contract reopens an item the office has already closed.
func arrearsOccurrence(c *store.Contract) string {
	return "suspended:" + c.SuspendedAt.UTC().Format(time.RFC3339Nano)
}

// errorKind names a skipped domain error for the logs (no message text).
func errorKind(err error) string {
	switch {
	case errors.Is(err, contract.ErrInvalidTransition):
		return "invalid_transition"
	case errors.Is(err, contract.ErrVersionMismatch):
		return "version_mismatch"
	case errors.Is(err, contract.ErrNotFound):
		return "not_found"
	case errors.Is(err, contract.ErrNotInArrears):
		return "not_in_arrears"
	}
	return "other"
}
