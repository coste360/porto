package yardquote

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Yard quotes: preview, create, read, update, recompute (m10-plan §4.2, §4.3).

var billingModes = []string{store.BillingModeActual, store.BillingModeFixed, store.BillingModeProgress}

// Number formats a yard quote number: PC<year>-NNNNNN (D12).
func Number(year int, n int64) string { return fmt.Sprintf("PC%d-%06d", year, n) }

func valuesJSON(m map[string]Value) store.JSON {
	if m == nil {
		m = map[string]Value{}
	}
	return store.MustJSON(m)
}

func valuesOf(j store.JSON) map[string]Value {
	out := map[string]Value{}
	if len(j) > 0 {
		_ = json.Unmarshal(j, &out)
	}
	return out
}

func snapshotOf(j store.JSON) price.Snapshot {
	var s price.Snapshot
	if len(j) > 0 {
		_ = json.Unmarshal(j, &s)
	}
	return s
}

func intPtr(p *int64) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

// checkTerms validates the conditions of a quote row (deposit, billing mode, dates, texts).
func checkTerms(v *auth.ValidationError, r *store.YardQuote) {
	if !contains(billingModes, r.BillingMode) {
		v.Add("billing_mode", "invalid")
	}
	if r.DepositBP != nil && r.DepositCents != nil {
		v.Add("deposit_cents", "deposit_one_of")
	}
	if r.DepositBP != nil && (*r.DepositBP < 0 || *r.DepositBP > 10000) {
		v.Add("deposit_bp", "invalid")
	}
	if r.DepositCents != nil && *r.DepositCents < 0 {
		v.Add("deposit_cents", "invalid")
	}
	if r.ValidUntil.Before(r.QuotedOn) {
		v.Add("valid_until", "before_quoted_on")
	}
	if !r.PlannedStartOn.IsZero() && !r.PlannedDeliveryOn.IsZero() && r.PlannedDeliveryOn.Before(r.PlannedStartOn) {
		v.Add("planned_delivery_on", "before_start")
	}
	var ok bool
	if r.Conditions, ok = cleanText(r.Conditions, 4000); !ok {
		v.Add("conditions", "invalid")
	}
	if r.Notes, ok = cleanText(r.Notes, 2000); !ok {
		v.Add("notes", "invalid")
	}
}

func depositPtrs(r *store.YardQuote) (*int64, *int64) {
	var bp, c *int64
	if r.DepositBP != nil {
		x := int64(*r.DepositBP)
		bp = &x
	}
	if r.DepositCents != nil {
		x := *r.DepositCents
		c = &x
	}
	return bp, c
}

// applyBuilt copies the computed snapshot to the row (totals, hours, snapshot) and returns the line rows.
func applyBuilt(r *store.YardQuote, b *built) []store.YardQuoteLine {
	r.Snapshot = store.MustJSON(b.snap)
	r.NetCents, r.VATCents, r.GrossCents = int64(b.totals.Net), int64(b.totals.VAT), int64(b.totals.Gross)
	r.DepositAmountCents = int64(b.totals.Deposit)
	hours := map[string]int64{}
	for q, h := range b.totals.EstimatedHours {
		hours[q] = int64(h)
	}
	r.EstimatedHours = store.MustJSON(hours)
	return lineRows(b.lines)
}

// lineRows maps priced lines to yard_quote_lines rows.
func lineRows(lines []price.Line) []store.YardQuoteLine {
	out := make([]store.YardQuoteLine, 0, len(lines))
	for _, l := range lines {
		row := store.YardQuoteLine{
			ID: store.NewID(), Seq: l.Spec.Seq, Source: l.Spec.Source, ItemID: l.Spec.ItemID, WorkTypeID: l.Spec.WorkTypeID,
			QualificationID: l.QualificationID, Description: l.Spec.Description, QtyFormula: l.Spec.QtyFormula,
			PriceFormula: l.Spec.PriceFormula, QtyMilli: int64(l.Qty), UnitCents: int64(l.Unit), DiscountCents: int64(l.Discount),
			QtyOverridden: l.QtyOverridden, PriceOverridden: l.PriceOverridden, NetCents: int64(l.Net), VATCents: int64(l.VAT),
			GrossCents: int64(l.Gross), VATRateBP: int(l.VATData.RateBP), VATNature: l.VATData.Nature,
			PriceIncludesVAT: l.VATData.PriceIncludesVAT, UnitCostCents: int64(l.UnitCost), EstimatedHoursMilli: int64(l.EstimatedHours),
			PriceListID: l.PriceListID, RuleID: l.RuleID, Issues: store.MustJSON(nonNilIssues(l.Issues)),
		}
		if row.QtyMilli < 0 {
			row.QtyMilli = 0
		}
		if l.Spec.Source == price.SourcePackage {
			pv, ps := l.Spec.PackageVersion, l.Spec.PackageLineSeq
			row.PackageID, row.PackageVersion, row.PackageLineSeq = l.Spec.PackageID, &pv, &ps
		}
		if l.Spec.Task != nil {
			row.Task = store.MustJSON(l.Spec.Task)
		}
		out = append(out, row)
	}
	return out
}

func nonNilIssues(is []price.LineIssue) []price.LineIssue {
	if is == nil {
		return []price.LineIssue{}
	}
	return is
}

// view builds the client view of a stored quote by recomputing its snapshot (INV-28: equal to the stored lines).
func (s *Service) view(ctx context.Context, db bun.IDB, scope store.MarinaScope, r *store.YardQuote) (*QuoteView, error) {
	snap := snapshotOf(r.Snapshot)
	lines, totals, err := price.Recompute(snap)
	if err != nil {
		return nil, err
	}
	quals, err := s.qualMap(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	return &QuoteView{Row: *r, Number: Number(r.Year, r.Number), Snapshot: snap, Preview: previewOf(snap, lines, totals, quals)}, nil
}

// Preview computes the lines, totals, estimated hours, variables and issues of an input (yard.read, no write).
func (s *Service) Preview(ctx context.Context, p *auth.Principal, marinaID string, in QuoteInput) (*Preview, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var out *Preview
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		quotedOn := s.today(ctx, tx, scope)
		if in.QuotedOn != nil {
			quotedOn = *in.QuotedOn
		}
		set, err := s.SettingsTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		bp, c := in.DepositBP, in.DepositCents
		if bp == nil && c == nil {
			x := int64(set.DepositBP)
			bp = &x
		}
		b, err := s.build(ctx, tx, scope, buildInput{
			CustomerID: in.CustomerID, VesselID: in.VesselID, Selections: in.Selections, JobInputs: in.JobInputs,
			VesselOverrides: in.VesselOverrides, Overrides: in.Overrides, ManualLines: in.ManualLines, QuotedOn: quotedOn,
			DepositBP: bp, DepositCents: c,
		})
		if err != nil {
			return err
		}
		quals, err := s.qualMap(ctx, tx, scope)
		if err != nil {
			return err
		}
		pv := previewOf(b.snap, b.lines, b.totals, quals)
		out = &pv
		return nil
	})
	return out, err
}

// newQuoteRow builds a draft row from an input (defaults from the yard settings).
func (s *Service) newQuoteRow(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, in QuoteInput) (*store.YardQuote, error) {
	set, err := s.SettingsTx(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	now := s.now()
	quotedOn := s.today(ctx, db, scope)
	if in.QuotedOn != nil {
		quotedOn = *in.QuotedOn
	}
	r := &store.YardQuote{
		ID: store.NewID(), Kind: store.YardQuoteJob, CustomerID: in.CustomerID, VesselID: in.VesselID, Status: store.YardQuoteDraft,
		QuotedOn: quotedOn, ValidUntil: quotedOn.AddDays(set.QuoteValidityDays), BillingMode: in.BillingMode,
		DepositBP: intPtr(in.DepositBP), DepositCents: in.DepositCents, Conditions: in.Conditions, Notes: in.Notes,
		JobInputs: valuesJSON(in.JobInputs), VesselOverrides: valuesJSON(in.VesselOverrides), CreatedByUserID: p.UserID,
		CreatedAt: now, UpdatedAt: now,
	}
	if r.BillingMode == "" {
		r.BillingMode = store.BillingModeActual
	}
	if r.DepositBP == nil && r.DepositCents == nil {
		bp := set.DepositBP
		r.DepositBP = &bp
	}
	if in.ValidUntil != nil {
		r.ValidUntil = *in.ValidUntil
	}
	if in.PlannedStartOn != nil {
		r.PlannedStartOn = *in.PlannedStartOn
	}
	if in.PlannedDeliveryOn != nil {
		r.PlannedDeliveryOn = *in.PlannedDeliveryOn
	}
	v := &auth.ValidationError{}
	checkTerms(v, r)
	if err := v.Err(); err != nil {
		return nil, err
	}
	r.ExpiresAt = endOfDay(ctx, db, scope, r.ValidUntil)
	return r, nil
}

// insertQuote builds, numbers and stores a draft inside tx.
func (s *Service) insertQuote(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a audit.Actor, r *store.YardQuote, in QuoteInput) error {
	bp, c := depositPtrs(r)
	b, err := s.build(ctx, tx, scope, buildInput{
		CustomerID: r.CustomerID, VesselID: r.VesselID, Selections: in.Selections, JobInputs: in.JobInputs,
		VesselOverrides: in.VesselOverrides, Overrides: in.Overrides, ManualLines: in.ManualLines, QuotedOn: r.QuotedOn,
		DepositBP: bp, DepositCents: c,
	})
	if err != nil {
		return err
	}
	n, err := store.NextNumber(ctx, tx, scope, store.SeriesYardQuote, r.QuotedOn.Year())
	if err != nil {
		return err
	}
	r.Number, r.Year = n, r.QuotedOn.Year()
	lines := applyBuilt(r, b)
	if err := store.InsertYardQuote(ctx, tx, scope, r); err != nil {
		return err
	}
	if err := store.ReplaceYardQuoteLines(ctx, tx, scope, r.ID, lines); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, scope, a, "yard_quote.create", "yard_quote", r.ID, map[string]any{
		"number": Number(r.Year, r.Number), "kind": r.Kind, "work_order_id": r.WorkOrderID, "lines": len(lines),
		"gross_cents": r.GrossCents, "blocking": b.totals.Blocking,
	}); err != nil {
		return err
	}
	return s.emitQuote(ctx, tx, scope, event.YardQuoteChanged, r)
}

func (s *Service) emitQuote(ctx context.Context, tx bun.IDB, scope store.MarinaScope, typ string, r *store.YardQuote) error {
	return s.emit(ctx, tx, scope, typ, event.YardQuoteData{
		QuoteID: r.ID, Kind: r.Kind, Status: r.Status, CustomerID: r.CustomerID, VesselID: r.VesselID, WorkOrderID: r.WorkOrderID,
		GrossCents: r.GrossCents,
	})
}

// Create creates a draft job quote: lines priced by formulas and the price lists, snapshot stored (yard.manage).
func (s *Service) Create(ctx context.Context, p *auth.Principal, marinaID string, in QuoteInput) (*QuoteView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var r *store.YardQuote
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err = s.newQuoteRow(ctx, tx, scope, p, in)
		if err != nil {
			return err
		}
		return s.insertQuote(ctx, tx, scope, p.Actor(), r, in)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, r)
}

// CreateVariationTx creates a draft variation quote of a work order inside the caller's transaction (the work
// order side validated and locked the order, D27). Customer and vessel are the order's.
func (s *Service) CreateVariationTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, order VariationOrder, in QuoteInput) (*QuoteView, error) {
	if !p.Can(scope.OrgID(), scope.ID(), auth.PermYardManage) {
		return nil, auth.ErrForbidden
	}
	in.CustomerID, in.VesselID = order.CustomerID, order.VesselID
	r, err := s.newQuoteRow(ctx, tx, scope, p, in)
	if err != nil {
		return nil, err
	}
	r.Kind, r.WorkOrderID = store.YardQuoteVariation, order.WorkOrderID
	if err := s.insertQuote(ctx, tx, scope, p.Actor(), r, in); err != nil {
		return nil, err
	}
	return s.view(ctx, tx, scope, r)
}

// Get returns a quote (yard.read).
func (s *Service) Get(ctx context.Context, p *auth.Principal, marinaID, id string) (*QuoteView, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	r, err := store.GetYardQuote(ctx, s.d.DB.DB, scope, id, false)
	if err != nil {
		return nil, notFound(err)
	}
	return s.view(ctx, s.d.DB.DB, scope, r)
}

// List lists the quotes, newest first (yard.read).
func (s *Service) List(ctx context.Context, p *auth.Principal, marinaID string, f QuoteFilter, cursor string, limit int) ([]QuoteView, string, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, "", err
	}
	db := s.d.DB.DB
	page, err := store.ListYardQuotes(ctx, db, scope, store.YardQuoteFilter{
		Status: f.Status, Kind: f.Kind, CustomerID: f.CustomerID, VesselID: f.VesselID, WorkOrderID: f.WorkOrderID,
	}, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]QuoteView, 0, len(page.Items))
	for i := range page.Items {
		v, err := s.view(ctx, db, scope, &page.Items[i])
		if err != nil {
			return nil, "", err
		}
		out = append(out, *v)
	}
	return out, page.NextCursor, nil
}

// lockDraft locks a quote, checks If-Match and that it is a draft.
func lockDraft(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string, ifMatch int) (*store.YardQuote, error) {
	r, err := store.GetYardQuote(ctx, tx, scope, id, true)
	if err != nil {
		return nil, notFound(err)
	}
	if r.Version != ifMatch {
		return nil, ErrVersionMismatch
	}
	if r.Status != store.YardQuoteDraft {
		return nil, ErrInvalidTransition
	}
	return r, nil
}

// Update changes a draft (yard.manage, If-Match): new selections are priced with the current catalogue; other
// changes (inputs, overrides, manual lines, deposit) recompute the stored snapshot.
func (s *Service) Update(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in QuotePatch) (*QuoteView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var r *store.YardQuote
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err = lockDraft(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		snap := snapshotOf(r.Snapshot)
		jobInputs, overridesV := valuesOf(r.JobInputs), valuesOf(r.VesselOverrides)
		if in.JobInputs != nil {
			jobInputs = *in.JobInputs
		}
		if in.VesselOverrides != nil {
			overridesV = *in.VesselOverrides
		}
		overrides := userOverrides(snap)
		if in.Overrides != nil {
			overrides = *in.Overrides
		}
		manual := manualLinesOf(snap)
		if in.ManualLines != nil {
			manual = *in.ManualLines
		}
		if in.ValidUntil != nil {
			r.ValidUntil = *in.ValidUntil
		}
		if in.BillingMode != nil {
			r.BillingMode = *in.BillingMode
		}
		if in.DepositBP.Set {
			r.DepositBP = intPtr(in.DepositBP.V)
			if in.DepositBP.V != nil {
				r.DepositCents = nil
			}
		}
		if in.DepositCents.Set {
			r.DepositCents = in.DepositCents.V
			if in.DepositCents.V != nil {
				r.DepositBP = nil
			}
		}
		if in.Conditions != nil {
			r.Conditions = *in.Conditions
		}
		if in.Notes != nil {
			r.Notes = *in.Notes
		}
		if in.PlannedStartOn.Set {
			r.PlannedStartOn = dateOr(in.PlannedStartOn.V)
		}
		if in.PlannedDeliveryOn.Set {
			r.PlannedDeliveryOn = dateOr(in.PlannedDeliveryOn.V)
		}
		v := &auth.ValidationError{}
		checkTerms(v, r)
		if err := v.Err(); err != nil {
			return err
		}
		r.ExpiresAt = endOfDay(ctx, tx, scope, r.ValidUntil)
		r.JobInputs, r.VesselOverrides = valuesJSON(jobInputs), valuesJSON(overridesV)
		bp, c := depositPtrs(r)
		var b *built
		if in.Selections != nil {
			b, err = s.build(ctx, tx, scope, buildInput{
				CustomerID: r.CustomerID, VesselID: r.VesselID, Selections: *in.Selections, JobInputs: jobInputs,
				VesselOverrides: overridesV, Overrides: overrides, ManualLines: manual, QuotedOn: r.QuotedOn, DepositBP: bp, DepositCents: c,
			})
		} else {
			b, err = s.rebuildSnapshot(snap, jobInputs, overridesV, overrides, manual, bp, c)
		}
		if err != nil {
			return err
		}
		lines := applyBuilt(r, b)
		if err := store.UpdateYardQuote(ctx, tx, scope, r, ifMatch, s.now()); err != nil {
			return versionErr(err)
		}
		if err := store.ReplaceYardQuoteLines(ctx, tx, scope, r.ID, lines); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "yard_quote.update", "yard_quote", r.ID, map[string]any{
			"gross_cents": r.GrossCents, "lines": len(lines), "selections_changed": in.Selections != nil,
		}); err != nil {
			return err
		}
		return s.emitQuote(ctx, tx, scope, event.YardQuoteChanged, r)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, r)
}

func dateOr(d *calendar.Date) calendar.Date {
	if d == nil {
		return calendar.Date{}
	}
	return *d
}

// userOverrides are the overrides of a snapshot without the discounts the manual lines carry.
func userOverrides(snap price.Snapshot) []price.Override {
	manual := map[int]bool{}
	for _, l := range snap.Lines {
		if l.Source == price.SourceManual {
			manual[l.Seq] = true
		}
	}
	out := []price.Override{}
	for _, o := range snap.Overrides {
		if !manual[o.Seq] {
			out = append(out, o)
		}
	}
	return out
}

// rebuildSnapshot changes the inputs of a stored snapshot without touching the catalogue data it froze.
func (s *Service) rebuildSnapshot(snap price.Snapshot, jobInputs, vesselOverrides map[string]Value, overrides []price.Override,
	manual []ManualLine, bp, c *int64,
) (*built, error) {
	issues := append(jobInputIssues(snap.JobVars, jobInputs), vesselOverrideIssues(vesselOverrides)...)
	if len(issues) > 0 {
		return nil, &FormulaError{Issues: issues}
	}
	specs := []price.LineSpec{}
	maxSeq := 0
	for _, l := range snap.Lines {
		if l.Source != price.SourceManual {
			specs = append(specs, l)
			maxSeq = max(maxSeq, l.Seq)
		}
	}
	for _, m := range manual {
		if m.Qty < 0 || m.Unit < 0 || m.Discount < 0 {
			return nil, validationErr("manual_lines", "invalid")
		}
		if _, ok := snap.Items[m.ItemID]; !ok {
			return nil, validationErr("manual_lines.item_id", "not_in_quote")
		}
		maxSeq++
		q, u := m.Qty, m.Unit
		wt := m.WorkTypeID
		if wt == "" && len(specs) > 0 {
			wt = specs[0].WorkTypeID
		}
		d, ok := cleanText(m.Description, 500)
		if !ok {
			return nil, validationErr("manual_lines.description", "invalid")
		}
		specs = append(specs, price.LineSpec{Seq: maxSeq, Source: price.SourceManual, ItemID: m.ItemID, WorkTypeID: wt, Description: d, ManualQty: &q, ManualUnit: &u})
		if m.Discount > 0 {
			dd := m.Discount
			overrides = append(overrides, price.Override{Seq: maxSeq, Discount: &dd})
		}
	}
	seqs := map[int]bool{}
	for _, l := range specs {
		seqs[l.Seq] = true
	}
	for i, o := range overrides {
		if !seqs[o.Seq] {
			return nil, validationErr(fmt.Sprintf("overrides[%d].seq", i), "not_found")
		}
	}
	snap.Lines = specs
	snap.Overrides = overrides
	snap.JobInputs = map[string]Value{}
	for _, jv := range snap.JobVars {
		if x, ok := jobInputs[jv.Name]; ok {
			snap.JobInputs[jv.Name] = x
		} else if jv.Default != nil {
			snap.JobInputs[jv.Name] = *jv.Default
		}
	}
	snap.VesselOverrides = nil
	if len(vesselOverrides) > 0 {
		snap.VesselOverrides = vesselOverrides
	}
	snap.DepositBP, snap.DepositCents = nil, nil
	if c != nil {
		cc := money.Cents(*c)
		snap.DepositCents = &cc
	} else if bp != nil {
		b := *bp
		snap.DepositBP = &b
	}
	return computeBuilt(snap)
}

// Recompute re-prices a draft with the current catalogue, variables and price lists (Ricalcola, yard.manage,
// If-Match): a new snapshot, audited. Overrides follow their line.
func (s *Service) Recompute(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*QuoteView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var r *store.YardQuote
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err = lockDraft(ctx, tx, scope, id, ifMatch)
		if err != nil {
			return err
		}
		old := snapshotOf(r.Snapshot)
		today := s.today(ctx, tx, scope)
		if r.QuotedOn.Before(today) {
			days := r.QuotedOn.DaysUntil(r.ValidUntil)
			r.QuotedOn, r.ValidUntil = today, today.AddDays(days)
			r.ExpiresAt = endOfDay(ctx, tx, scope, r.ValidUntil)
		}
		bp, c := depositPtrs(r)
		b, err := s.build(ctx, tx, scope, buildInput{
			CustomerID: r.CustomerID, VesselID: r.VesselID, Selections: old.Selections, JobInputs: valuesOf(r.JobInputs),
			VesselOverrides: valuesOf(r.VesselOverrides), ManualLines: manualLinesOf(old), QuotedOn: r.QuotedOn,
			DepositBP: bp, DepositCents: c, AllowInactive: true,
		})
		if err != nil {
			return err
		}
		remapped := remapOverrides(userOverrides(old), old.Lines, b.snap.Lines)
		if len(remapped) > 0 {
			b.snap.Overrides = append(remapped, b.snap.Overrides...)
			if b, err = computeBuilt(b.snap); err != nil {
				return err
			}
		}
		lines := applyBuilt(r, b)
		if err := store.UpdateYardQuote(ctx, tx, scope, r, ifMatch, s.now()); err != nil {
			return versionErr(err)
		}
		if err := store.ReplaceYardQuoteLines(ctx, tx, scope, r.ID, lines); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "yard_quote.recompute", "yard_quote", r.ID, map[string]any{
			"quoted_on": r.QuotedOn.String(), "gross_cents": r.GrossCents, "lines": len(lines),
		}); err != nil {
			return err
		}
		return s.emitQuote(ctx, tx, scope, event.YardQuoteChanged, r)
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.d.DB.DB, scope, r)
}
