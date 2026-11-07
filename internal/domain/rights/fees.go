package rights

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Management fee tables and yearly runs (spec 14.4; m4-plan §4.4). The fee of a right is computed by the pure
// allot.FeeAmount, split among the holders by share (money.Allocate) and planned into holder instalments on issue.
// One issued run per marina, year and table (partial unique index): concurrent issues, one wins.

// FeeRow is a row of a fee table.
type FeeRow struct {
	CategoryID     string
	FromCm, ToCm   *int64
	AmountCents    *int64
	RateCentsPerM2 *int64
}

// FeeTable is a management fee table with its rows.
type FeeTable struct {
	ID      string
	Year    int
	Name    string
	Basis   string
	IstatBP int64
	Status  string
	Rows    []FeeRow
	Version int
}

// FeeTableInput are the fields of a new fee table.
type FeeTableInput struct {
	Year    int
	Name    string
	Basis   string
	IstatBP int64
	Rows    []FeeRow
}

// FeeTablePatch lists the fields to change (nil = unchanged).
type FeeTablePatch struct {
	Name    *string
	IstatBP *int64
	Rows    *[]FeeRow
}

// Fee table statuses.
const (
	FeeTableDraft  = "draft"
	FeeTableActive = "active"
)

func feeTableOf(t *store.ManagementFeeTable, rows []store.ManagementFeeTableRow) FeeTable {
	out := FeeTable{ID: t.ID, Year: t.Year, Name: t.Name, Basis: t.Basis, IstatBP: t.IstatBP, Status: t.Status, Rows: []FeeRow{}, Version: t.Version}
	for _, r := range rows {
		out.Rows = append(out.Rows, FeeRow{CategoryID: r.CategoryID, FromCm: r.FromCm, ToCm: r.ToCm, AmountCents: r.AmountCents, RateCentsPerM2: r.RateCentsPerM2})
	}
	return out
}

func checkFeeRows(ctx context.Context, db bun.IDB, scope store.MarinaScope, basis string, rows []FeeRow, v *auth.ValidationError) {
	if len(rows) > 200 {
		v.Add("rows", "too_many")
	}
	seen := map[string]bool{}
	for _, r := range rows {
		neg := (r.AmountCents != nil && *r.AmountCents < 0) || (r.RateCentsPerM2 != nil && *r.RateCentsPerM2 < 0)
		if neg {
			v.Add("rows", "out_of_range")
			return
		}
		switch basis {
		case allot.BasisPerCategory:
			if r.CategoryID == "" || r.AmountCents == nil || seen[r.CategoryID] {
				v.Add("rows", "invalid")
				return
			}
			seen[r.CategoryID] = true
			if _, err := store.GetBerthCategory(ctx, db, scope, r.CategoryID); err != nil {
				v.Add("rows", "unknown_category")
				return
			}
		case allot.BasisPerM2Berth:
			if r.RateCentsPerM2 == nil {
				v.Add("rows", "invalid")
				return
			}
		case allot.BasisPerLengthBand:
			if r.FromCm == nil || r.AmountCents == nil || *r.FromCm < 0 || (r.ToCm != nil && *r.ToCm <= *r.FromCm) {
				v.Add("rows", "invalid")
				return
			}
		}
	}
}

func feeRowsOf(rows []FeeRow, now time.Time) []store.ManagementFeeTableRow {
	out := make([]store.ManagementFeeTableRow, len(rows))
	for i, r := range rows {
		out[i] = store.ManagementFeeTableRow{
			ID: store.NewID(), CategoryID: r.CategoryID, FromCm: r.FromCm, ToCm: r.ToCm, AmountCents: r.AmountCents,
			RateCentsPerM2: r.RateCentsPerM2, Sort: i, CreatedAt: now, UpdatedAt: now,
		}
	}
	return out
}

// ListFeeTables returns the fee tables (year 0 = all) (rights.manage).
func (s *Service) ListFeeTables(ctx context.Context, p *auth.Principal, marinaID string, year int) ([]FeeTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out []FeeTable
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ts, err := store.ListFeeTables(ctx, tx, scope, year)
		if err != nil {
			return err
		}
		ids := make([]string, len(ts))
		for i := range ts {
			ids[i] = ts[i].ID
		}
		rows, err := store.RowsOfFeeTables(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		out = make([]FeeTable, len(ts))
		for i := range ts {
			out[i] = feeTableOf(&ts[i], rows[ts[i].ID])
		}
		return nil
	})
	return out, err
}

func (s *Service) feeTableTx(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string) (*store.ManagementFeeTable, FeeTable, error) {
	t, err := store.GetFeeTable(ctx, db, scope, id)
	if err != nil {
		return nil, FeeTable{}, err
	}
	rows, err := store.RowsOfFeeTables(ctx, db, scope, []string{id})
	if err != nil {
		return nil, FeeTable{}, err
	}
	return t, feeTableOf(t, rows[id]), nil
}

// GetFeeTable returns a fee table (rights.manage).
func (s *Service) GetFeeTable(ctx context.Context, p *auth.Principal, marinaID, id string) (*FeeTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	_, out, err := s.feeTableTx(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateFeeTable creates a draft fee table (rights.manage).
func (s *Service) CreateFeeTable(ctx context.Context, p *auth.Principal, marinaID string, in FeeTableInput) (*FeeTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out FeeTable
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		v := &auth.ValidationError{}
		in.Name = strings.TrimSpace(in.Name)
		if in.Year < 2000 || in.Year > 9999 {
			v.Add("year", "out_of_range")
		}
		if in.Name == "" || len(in.Name) > 120 {
			v.Add("name", "invalid")
		}
		if !slices.Contains(allot.Bases, in.Basis) {
			v.Add("basis", "invalid")
		}
		if in.IstatBP < -5000 || in.IstatBP > 5000 {
			v.Add("istat_bp", "out_of_range")
		}
		checkFeeRows(ctx, tx, scope, in.Basis, in.Rows, v)
		if err := v.Err(); err != nil {
			return err
		}
		now := s.now()
		t := &store.ManagementFeeTable{
			ID: store.NewID(), Year: in.Year, Name: in.Name, Basis: in.Basis, IstatBP: in.IstatBP,
			Status: FeeTableDraft, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		rows := feeRowsOf(in.Rows, now)
		if err := store.InsertFeeTable(ctx, tx, scope, t, rows); err != nil {
			return err
		}
		out = feeTableOf(t, rows)
		return s.write(ctx, tx, actorOf(p), scope, "fee_table.create", "fee_table", t.ID, map[string]any{"year": t.Year, "basis": t.Basis, "rows": len(rows)},
			changed("fee_table", t.ID, "created"))
	})
	return &out, err
}

// UpdateFeeTable changes a draft fee table when its version equals ifMatch (rights.manage).
func (s *Service) UpdateFeeTable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in FeeTablePatch) (*FeeTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out FeeTable
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, cur, err := s.feeTableTx(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if t.Version != ifMatch {
			return ErrVersionMismatch
		}
		if t.Status != FeeTableDraft {
			return ErrInvalidTransition
		}
		v := &auth.ValidationError{}
		if in.Name != nil {
			n := strings.TrimSpace(*in.Name)
			if n == "" || len(n) > 120 {
				v.Add("name", "invalid")
			}
			t.Name = n
		}
		if in.IstatBP != nil {
			if *in.IstatBP < -5000 || *in.IstatBP > 5000 {
				v.Add("istat_bp", "out_of_range")
			}
			t.IstatBP = *in.IstatBP
		}
		rows := cur.Rows
		if in.Rows != nil {
			rows = *in.Rows
			checkFeeRows(ctx, tx, scope, t.Basis, rows, v)
		}
		if err := v.Err(); err != nil {
			return err
		}
		now := s.now()
		t.UpdatedAt = now
		if err := store.UpdateFeeTable(ctx, tx, scope, t, ifMatch); err != nil {
			return err
		}
		srows := feeRowsOf(rows, now)
		if in.Rows != nil {
			if err := store.ReplaceFeeTableRows(ctx, tx, scope, t.ID, srows); err != nil {
				return err
			}
		}
		out = feeTableOf(t, srows)
		return s.write(ctx, tx, actorOf(p), scope, "fee_table.update", "fee_table", t.ID, map[string]any{"istat_bp": t.IstatBP, "rows": len(rows)},
			changed("fee_table", t.ID, "updated"))
	})
	return &out, err
}

// DeleteFeeTable deletes a draft fee table without runs (rights.manage).
func (s *Service) DeleteFeeTable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.GetFeeTable(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if t.Status != FeeTableDraft {
			return ErrInvalidTransition
		}
		if err := store.DeleteFeeTable(ctx, tx, scope, id, ifMatch); err != nil {
			return err
		}
		return s.write(ctx, tx, actorOf(p), scope, "fee_table.delete", "fee_table", id, nil, changed("fee_table", id, "deleted"))
	})
}

// ActivateFeeTable makes a draft table usable by runs (it needs at least one row) (rights.manage).
func (s *Service) ActivateFeeTable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*FeeTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out FeeTable
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, cur, err := s.feeTableTx(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if t.Status != FeeTableDraft {
			return ErrInvalidTransition
		}
		if t.Version != ifMatch {
			return ErrVersionMismatch
		}
		if len(cur.Rows) == 0 {
			return invalidField("rows", "required")
		}
		t.Status, t.UpdatedAt = FeeTableActive, s.now()
		if err := store.UpdateFeeTable(ctx, tx, scope, t, ifMatch); err != nil {
			return err
		}
		cur.Status, cur.Version = t.Status, t.Version
		out = cur
		return s.write(ctx, tx, actorOf(p), scope, "fee_table.activate", "fee_table", t.ID, map[string]any{"status": t.Status},
			changed("fee_table", t.ID, event.ChangeActivated))
	})
	return &out, err
}

// ---- runs ----

// FeeCategoryTotal is the total of a category in a simulation.
type FeeCategoryTotal struct {
	Category      string `json:"category"`
	Rights        int    `json:"rights"`
	AmountCents   int64  `json:"amount_cents"`
	PreviousCents *int64 `json:"previous_cents,omitempty"`
}

// FeeExcluded is a right excluded from a run with its reason.
type FeeExcluded struct {
	RightID string `json:"right_id"`
	Reason  string `json:"reason"`
}

// FeeTotals is the simulation of a run (stored as management_fee_runs.totals).
type FeeTotals struct {
	TotalCents         int64              `json:"total_cents"`
	PreviousTotalCents *int64             `json:"previous_total_cents,omitempty"`
	TotalsByCategory   []FeeCategoryTotal `json:"totals_by_category"`
	DiffPreviousCents  *int64             `json:"diff_previous_cents,omitempty"`
	Excluded           []FeeExcluded      `json:"excluded"`
}

// FeeRun is a fee run.
type FeeRun struct {
	store.ManagementFeeRun
	Simulation *FeeTotals
}

// LineHolder is the share of a holder in a fee line (stored as management_fee_lines.holders).
type LineHolder struct {
	CustomerID  string `json:"customer_id"`
	ShareMillis int64  `json:"share_millis"`
	AmountCents int64  `json:"amount_cents"`
	DisplayName string `json:"-"`
}

// FeeLine is a line of a run.
type FeeLine struct {
	store.ManagementFeeLine
	HolderShares []LineHolder
}

func feeRunOf(r *store.ManagementFeeRun) FeeRun {
	out := FeeRun{ManagementFeeRun: *r}
	if len(r.Totals) > 0 && string(r.Totals) != "{}" {
		var t FeeTotals
		if json.Unmarshal(r.Totals, &t) == nil {
			out.Simulation = &t
		}
	}
	return out
}

// ListFeeRuns returns the fee runs (year 0 = all) (rights.manage).
func (s *Service) ListFeeRuns(ctx context.Context, p *auth.Principal, marinaID string, year int) ([]FeeRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListFeeRuns(ctx, s.d.DB.DB, scope, year)
	if err != nil {
		return nil, err
	}
	out := make([]FeeRun, len(rows))
	for i := range rows {
		out[i] = feeRunOf(&rows[i])
	}
	return out, nil
}

// GetFeeRun returns a fee run (rights.manage).
func (s *Service) GetFeeRun(ctx context.Context, p *auth.Principal, marinaID, id string) (*FeeRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	r, err := store.GetFeeRun(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	out := feeRunOf(r)
	return &out, nil
}

// CreateFeeRun creates a draft run of a year on an active table with its holder plan (rights.manage).
func (s *Service) CreateFeeRun(ctx context.Context, p *auth.Principal, marinaID string, year int, tableID string, plan []byte) (*FeeRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	if year < 2000 || year > 9999 {
		return nil, invalidField("year", "out_of_range")
	}
	planJSON, _, err := ParsePlan(plan)
	if err != nil {
		return nil, err
	}
	var out FeeRun
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.GetFeeTable(ctx, tx, scope, tableID)
		if err != nil {
			return err
		}
		if t.Status != FeeTableActive {
			return ErrInvalidTransition
		}
		now := s.now()
		r := &store.ManagementFeeRun{
			ID: store.NewID(), Year: year, TableID: t.ID, Status: FeeRunDraft, Plan: planJSON, Totals: store.JSON(`{}`),
			CreatedByUserID: userOf(p), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertFeeRun(ctx, tx, scope, r); err != nil {
			return err
		}
		out = feeRunOf(r)
		return s.write(ctx, tx, actorOf(p), scope, "fee_run.create", "fee_run", r.ID, map[string]any{"year": year, "table_id": t.ID},
			changed("fee_run", r.ID, "created"))
	})
	return &out, err
}

// SimulateFeeRun computes (or recomputes) the lines of a draft or simulated run: every right covering a day of
// the year gets its fee (pro rata by days), split among its holders; rights without a rate are excluded with
// the reason (rights.manage).
func (s *Service) SimulateFeeRun(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*FeeTotals, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *FeeTotals
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := lockFeeRun(ctx, tx, scope, id); err != nil {
			return err
		}
		r, err := store.GetFeeRun(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		next, ok := Next(FeeRunTransitions, r.Status, "simulate")
		if !ok {
			return ErrInvalidTransition
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		lines, totals, err := s.simulate(ctx, tx, scope, r)
		if err != nil {
			return err
		}
		if err := store.ReplaceFeeLines(ctx, tx, scope, r.ID, lines); err != nil {
			return err
		}
		now := s.now()
		r.Status, r.Totals, r.SimulatedAt, r.UpdatedAt = next, store.MustJSON(totals), now, now
		if err := store.UpdateFeeRun(ctx, tx, scope, r, ifMatch); err != nil {
			return err
		}
		out = totals
		return s.write(ctx, tx, actorOf(p), scope, "fee_run.simulate", "fee_run", r.ID,
			map[string]any{"lines": len(lines), "total_cents": totals.TotalCents, "excluded": len(totals.Excluded)},
			changed("fee_run", r.ID, event.ChangePreviewed))
	})
	return out, err
}

func lockFeeRun(ctx context.Context, tx bun.IDB, scope store.MarinaScope, id string) error {
	return store.LockFeeRun(ctx, tx, scope, id)
}

// simulate computes the lines and totals of a run.
func (s *Service) simulate(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.ManagementFeeRun) ([]store.ManagementFeeLine, *FeeTotals, error) {
	t, err := store.GetFeeTable(ctx, tx, scope, r.TableID)
	if err != nil {
		return nil, nil, err
	}
	rowsBy, err := store.RowsOfFeeTables(ctx, tx, scope, []string{t.ID})
	if err != nil {
		return nil, nil, err
	}
	var frows []allot.FeeRow
	for _, x := range rowsBy[t.ID] {
		fr := allot.FeeRow{CategoryID: x.CategoryID}
		if x.FromCm != nil {
			fr.FromCm = *x.FromCm
		}
		if x.ToCm != nil {
			fr.ToCm = *x.ToCm
		}
		if x.AmountCents != nil {
			fr.Amount = money.Cents(*x.AmountCents)
		}
		if x.RateCentsPerM2 != nil {
			fr.RateCentsPerM2 = money.Cents(*x.RateCentsPerM2)
		}
		frows = append(frows, fr)
	}
	cats, err := store.ListBerthCategories(ctx, tx, scope, true)
	if err != nil {
		return nil, nil, err
	}
	catByCode := map[string]*store.BerthCategory{}
	for i := range cats {
		catByCode[cats[i].Code] = &cats[i]
	}
	from, to := calendar.New(r.Year, 1, 1), calendar.New(r.Year, 12, 31)
	var rights []store.BerthRight
	if err := tx.NewSelect().Model(&rights).Where("marina_id = ?", scope.ID()).Where("status <> 'cancelled'").
		Where("from_on <= ? AND to_on >= ?", to, from).OrderExpr("id").Scan(ctx); err != nil {
		return nil, nil, err
	}
	ids := make([]string, len(rights))
	for i := range rights {
		ids[i] = rights[i].ID
	}
	hs, err := store.HoldersOfRights(ctx, tx, scope, ids)
	if err != nil {
		return nil, nil, err
	}
	holders := map[string][]allot.Share{}
	for _, h := range hs {
		holders[h.RightID] = append(holders[h.RightID], allot.Share{CustomerID: h.CustomerID, Millis: h.ShareMillis})
	}
	prev := map[string]int64{}
	if pr, err := store.LatestIssuedFeeRun(ctx, tx, scope, r.Year-1); err == nil {
		pl, err := store.FeeLinesOfRun(ctx, tx, scope, pr.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, l := range pl {
			if !l.Excluded {
				prev[l.RightID] += l.AmountCents
			}
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}
	now := s.now()
	days := allot.DaysInYear(r.Year)
	totals := &FeeTotals{TotalsByCategory: []FeeCategoryTotal{}, Excluded: []FeeExcluded{}}
	byCat := map[string]*FeeCategoryTotal{}
	var lines []store.ManagementFeeLine
	berths := map[string]*store.Berth{}
	var prevTotal int64
	hasPrev := len(prev) > 0
	for i := range rights {
		rt := &rights[i]
		b := berths[rt.BerthID]
		if b == nil {
			if b, err = store.BerthByID(ctx, tx, scope, rt.BerthID); err != nil {
				return nil, nil, err
			}
			berths[b.ID] = b
		}
		catID := ""
		if c := catByCode[b.PriceClass]; c != nil {
			catID = c.ID
		}
		covered := allot.DaysCovered(rt.FromOn, rt.ToOn, r.Year)
		res, err := allot.FeeAmount(allot.FeeInput{
			Basis: t.Basis, Rows: frows, IstatBP: t.IstatBP, BerthCategoryID: catID, BerthLengthCm: b.LengthCm, BerthWidthCm: b.WidthCm,
			DaysCovered: covered, DaysInYear: days,
		})
		if err != nil {
			return nil, nil, err
		}
		line := store.ManagementFeeLine{
			ID: store.NewID(), RightID: rt.ID, BerthID: b.ID, Category: b.PriceClass, BaseCents: int64(res.Base), Days: covered,
			AmountCents: int64(res.Amount), Holders: store.JSON(`[]`), CreatedAt: now, UpdatedAt: now,
		}
		if pv, ok := prev[rt.ID]; ok {
			line.PreviousCents = &pv
			prevTotal += pv
		}
		if res.Excluded == "" && len(holders[rt.ID]) == 0 {
			res.Excluded = allot.ExcludedNoHolders
		}
		if res.Excluded != "" {
			line.Excluded, line.ExclusionReason, line.AmountCents = true, res.Excluded, 0
			totals.Excluded = append(totals.Excluded, FeeExcluded{RightID: rt.ID, Reason: res.Excluded})
			lines = append(lines, line)
			continue
		}
		parts, err := allot.Holders(res.Amount, holders[rt.ID])
		if err != nil {
			return nil, nil, &SumError{Err: ErrSharesInvalid}
		}
		lh := make([]LineHolder, len(parts))
		for j, ph := range parts {
			lh[j] = LineHolder{CustomerID: ph.CustomerID, ShareMillis: ph.Millis, AmountCents: int64(ph.Amount)}
		}
		line.Holders = store.MustJSON(lh)
		lines = append(lines, line)
		totals.TotalCents += line.AmountCents
		ct := byCat[line.Category]
		if ct == nil {
			ct = &FeeCategoryTotal{Category: line.Category}
			byCat[line.Category] = ct
		}
		ct.Rights++
		ct.AmountCents += line.AmountCents
		if line.PreviousCents != nil {
			pv := *line.PreviousCents
			if ct.PreviousCents != nil {
				pv += *ct.PreviousCents
			}
			ct.PreviousCents = &pv
		}
	}
	keys := make([]string, 0, len(byCat))
	for k := range byCat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		totals.TotalsByCategory = append(totals.TotalsByCategory, *byCat[k])
	}
	if hasPrev {
		diff := totals.TotalCents - prevTotal
		totals.PreviousTotalCents, totals.DiffPreviousCents = &prevTotal, &diff
	}
	return lines, totals, nil
}

// IssueFeeRun issues a simulated run: one issued run per year and table (a concurrent issue of another run is
// 409 conflict), holder instalments planned for every holder of every line (Σ = holder amount, INV-31)
// (rights.manage).
func (s *Service) IssueFeeRun(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*FeeRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out FeeRun
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := lockFeeRun(ctx, tx, scope, id); err != nil {
			return err
		}
		r, err := store.GetFeeRun(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		next, ok := Next(FeeRunTransitions, r.Status, "issue")
		if !ok {
			return ErrInvalidTransition
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		_, pol, err := ParsePlan(r.Plan)
		if err != nil {
			return err
		}
		lines, err := store.FeeLinesOfRun(ctx, tx, scope, r.ID)
		if err != nil {
			return err
		}
		now := s.now()
		today := s.today(ctx, tx, scope)
		from, to := calendar.New(r.Year, 1, 1), calendar.New(r.Year+1, 1, 1)
		var rows []store.HolderInstallment
		holders := map[string]bool{}
		var total int64
		for _, l := range lines {
			if l.Excluded {
				continue
			}
			var lh []LineHolder
			_ = json.Unmarshal(l.Holders, &lh)
			for _, h := range lh {
				items, err := planHolder(money.Cents(h.AmountCents), pol, from, to, today)
				if err != nil {
					return invalidField("plan", "invalid")
				}
				rows = append(rows, holderRowsOf(items, SourceManagementFee, l.ID, r.ID, l.RightID, h.CustomerID, 1, now)...)
				holders[h.CustomerID] = true
				total += h.AmountCents
			}
		}
		r.Status, r.IssuedAt, r.UpdatedAt = next, now, now
		if err := store.UpdateFeeRun(ctx, tx, scope, r, ifMatch); err != nil {
			return err
		}
		if err := store.InsertHolderInstallments(ctx, tx, scope, rows); err != nil {
			return err
		}
		out = feeRunOf(r)
		return s.write(ctx, tx, actorOf(p), scope, "fee_run.issue", "fee_run", r.ID,
			map[string]any{"year": r.Year, "total_cents": total, "installments": len(rows)},
			ev{event.ManagementFeeIssued, event.RunIssuedData{RunID: r.ID, Year: r.Year, TotalCents: total, Holders: len(holders)}})
	})
	return &out, err
}

// DiscardFeeRun discards a draft or simulated run (rights.manage).
func (s *Service) DiscardFeeRun(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*FeeRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out FeeRun
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := lockFeeRun(ctx, tx, scope, id); err != nil {
			return err
		}
		r, err := store.GetFeeRun(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		next, ok := Next(FeeRunTransitions, r.Status, "discard")
		if !ok {
			return ErrInvalidTransition
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		r.Status, r.UpdatedAt = next, s.now()
		if err := store.UpdateFeeRun(ctx, tx, scope, r, ifMatch); err != nil {
			return err
		}
		out = feeRunOf(r)
		return s.write(ctx, tx, actorOf(p), scope, "fee_run.discard", "fee_run", r.ID, map[string]any{"status": r.Status},
			changed("fee_run", r.ID, event.ChangeDiscarded))
	})
	return &out, err
}

func (s *Service) feeLineViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.ManagementFeeLine) ([]FeeLine, error) {
	out := make([]FeeLine, len(rows))
	var cids []string
	for i, r := range rows {
		out[i] = FeeLine{ManagementFeeLine: r, HolderShares: []LineHolder{}}
		_ = json.Unmarshal(r.Holders, &out[i].HolderShares)
		for _, h := range out[i].HolderShares {
			cids = append(cids, h.CustomerID)
		}
	}
	nm, err := names(ctx, db, scope, can(p, scope, auth.PermCustomersRead), cids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		for j := range out[i].HolderShares {
			out[i].HolderShares[j].DisplayName = nm[out[i].HolderShares[j].CustomerID]
		}
	}
	return out, nil
}

// ListFeeLines returns a page of the lines of a run (rights.manage).
func (s *Service) ListFeeLines(ctx context.Context, p *auth.Principal, marinaID, runID, cursor string, limit int) (store.Page[FeeLine], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return store.Page[FeeLine]{}, err
	}
	var out store.Page[FeeLine]
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetFeeRun(ctx, tx, scope, runID); err != nil {
			return err
		}
		page, err := store.PageFeeLines(ctx, tx, scope, runID, cursor, limit)
		if err != nil {
			return err
		}
		items, err := s.feeLineViews(ctx, tx, scope, p, page.Items)
		out = store.Page[FeeLine]{Items: items, NextCursor: page.NextCursor}
		return err
	})
	return out, err
}

// AllFeeLines returns every line of a run (CSV, rights.manage).
func (s *Service) AllFeeLines(ctx context.Context, p *auth.Principal, marinaID, runID string) ([]FeeLine, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out []FeeLine
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetFeeRun(ctx, tx, scope, runID); err != nil {
			return err
		}
		rows, err := store.FeeLinesOfRun(ctx, tx, scope, runID)
		if err != nil {
			return err
		}
		out, err = s.feeLineViews(ctx, tx, scope, p, rows)
		return err
	})
	return out, err
}
