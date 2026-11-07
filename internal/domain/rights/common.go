package rights

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Millesimal tables, common charge budgets, items, actuals, advance and balance runs, allocations and statements
// (spec 14.5; m4-plan §4.5, §15 D17–D18). Allocation is the pure allot.Common: Σ per item = item amount and Σ run
// = Σ items exactly (INV-42). A table is usable only at Σ = 1000 (INV-41), re-checked in every allocation
// transaction; entries of an active table and allocations of an issued run are frozen (triggers, INV-44).

// ---- millesimal tables ----

// MillesimalTable is a table with its entry count and Σ millis.
type MillesimalTable struct {
	store.MillesimalTable
	EntriesCount int
	SumMillis    int64
}

// MillesimalEntry is the weight of a berth.
type MillesimalEntry struct {
	BerthID   string
	BerthCode string
	Millis    int64
}

// MillesimalEntries are the entries of a table; Rejected lists the CSV rows that could not be read.
type MillesimalEntries struct {
	TableID   string
	Entries   []MillesimalEntry
	SumMillis int64
	Rejected  []csvimport.Issue
}

func (s *Service) tableViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, ts []store.MillesimalTable) ([]MillesimalTable, error) {
	ids := make([]string, len(ts))
	for i := range ts {
		ids[i] = ts[i].ID
	}
	st, err := store.MillesimalStats(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := make([]MillesimalTable, len(ts))
	for i := range ts {
		out[i] = MillesimalTable{MillesimalTable: ts[i], EntriesCount: st[ts[i].ID].Count, SumMillis: st[ts[i].ID].Sum}
	}
	return out, nil
}

func (s *Service) tableView(ctx context.Context, db bun.IDB, scope store.MarinaScope, t *store.MillesimalTable) (*MillesimalTable, error) {
	out, err := s.tableViews(ctx, db, scope, []store.MillesimalTable{*t})
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

// ListMillesimalTables returns the tables by code (rights.manage).
func (s *Service) ListMillesimalTables(ctx context.Context, p *auth.Principal, marinaID string) ([]MillesimalTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out []MillesimalTable
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		ts, err := store.ListMillesimalTables(ctx, tx, scope)
		if err != nil {
			return err
		}
		out, err = s.tableViews(ctx, tx, scope, ts)
		return err
	})
	return out, err
}

// GetMillesimalTable returns a table (rights.manage).
func (s *Service) GetMillesimalTable(ctx context.Context, p *auth.Principal, marinaID, id string) (*MillesimalTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	t, err := store.GetMillesimalTable(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	return s.tableView(ctx, s.d.DB.DB, scope, t)
}

// CreateMillesimalTable creates a draft table; a code already taken is 409 (rights.manage).
func (s *Service) CreateMillesimalTable(ctx context.Context, p *auth.Principal, marinaID, name, code string) (*MillesimalTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	name, code = strings.TrimSpace(name), strings.TrimSpace(code)
	v := &auth.ValidationError{}
	if name == "" || len(name) > 120 {
		v.Add("name", "invalid")
	}
	if code == "" || len(code) > 32 {
		v.Add("code", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	var out *MillesimalTable
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		t := &store.MillesimalTable{ID: store.NewID(), Name: name, Code: code, Status: TableDraft, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := store.InsertMillesimalTable(ctx, tx, scope, t); err != nil {
			return err
		}
		out = &MillesimalTable{MillesimalTable: *t}
		return s.write(ctx, tx, actorOf(p), scope, "millesimal_table.create", "millesimal_table", t.ID, map[string]any{"code": code},
			changed("millesimal_table", t.ID, "created"))
	})
	return out, err
}

// UpdateMillesimalTable renames a draft table or archives an active one (rights.manage).
func (s *Service) UpdateMillesimalTable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, name *string, archive bool) (*MillesimalTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *MillesimalTable
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMillesimalTable(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if t.Version != ifMatch {
			return ErrVersionMismatch
		}
		now := s.now()
		change := "updated"
		if name != nil {
			n := strings.TrimSpace(*name)
			if n == "" || len(n) > 120 {
				return invalidField("name", "invalid")
			}
			if t.Status != TableDraft && n != t.Name {
				return ErrTableActive
			}
			t.Name = n
		}
		if archive {
			if t.Status != TableActive {
				return ErrInvalidTransition
			}
			t.Status, t.ArchivedAt, change = TableArchived, now, "archived"
		}
		t.UpdatedAt = now
		if err := store.UpdateMillesimalTable(ctx, tx, scope, t, ifMatch); err != nil {
			return err
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "millesimal_table.update", "millesimal_table", t.ID, map[string]any{"status": t.Status},
			changed("millesimal_table", t.ID, change)); err != nil {
			return err
		}
		out, err = s.tableView(ctx, tx, scope, t)
		return err
	})
	return out, err
}

// DeleteMillesimalTable deletes a draft table not used by any item (rights.manage).
func (s *Service) DeleteMillesimalTable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMillesimalTable(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if t.Status != TableDraft {
			return ErrTableActive
		}
		if err := store.DeleteMillesimalTable(ctx, tx, scope, id, ifMatch); err != nil {
			return err
		}
		return s.write(ctx, tx, actorOf(p), scope, "millesimal_table.delete", "millesimal_table", id, nil, changed("millesimal_table", id, "deleted"))
	})
}

func (s *Service) entriesView(ctx context.Context, db bun.IDB, scope store.MarinaScope, tableID string) (*MillesimalEntries, error) {
	es, err := store.EntriesOfTables(ctx, db, scope, []string{tableID})
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(es[tableID]))
	for i, e := range es[tableID] {
		ids[i] = e.BerthID
	}
	bs, err := store.RightsBerthsByIDs(ctx, db, scope, ids)
	if err != nil {
		return nil, err
	}
	out := &MillesimalEntries{TableID: tableID, Entries: []MillesimalEntry{}, Rejected: []csvimport.Issue{}}
	for _, e := range es[tableID] {
		out.Entries = append(out.Entries, MillesimalEntry{BerthID: e.BerthID, BerthCode: bs[e.BerthID].Code, Millis: e.Millis})
		out.SumMillis += e.Millis
	}
	slices.SortFunc(out.Entries, func(a, b MillesimalEntry) int { return strings.Compare(a.BerthCode, b.BerthCode) })
	return out, nil
}

// GetMillesimalEntries returns the entries of a table by berth code (rights.manage).
func (s *Service) GetMillesimalEntries(ctx context.Context, p *auth.Principal, marinaID, id string) (*MillesimalEntries, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *MillesimalEntries
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetMillesimalTable(ctx, tx, scope, id); err != nil {
			return err
		}
		out, err = s.entriesView(ctx, tx, scope, id)
		return err
	})
	return out, err
}

// EntriesInput is the full set of entries of a table: JSON entries or CSV text (berth code;millis).
type EntriesInput struct {
	Entries []MillesimalEntry
	CSV     *string
}

// ReplaceMillesimalEntries replaces the entries of a draft table (an active table is 409 table_active) when its
// version equals ifMatch. CSV rows that cannot be read are reported and nothing is written (rights.manage).
func (s *Service) ReplaceMillesimalEntries(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in EntriesInput) (*MillesimalEntries, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *MillesimalEntries
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMillesimalTable(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if t.Status != TableDraft {
			return ErrTableActive
		}
		if t.Version != ifMatch {
			return ErrVersionMismatch
		}
		entries := in.Entries
		var rejected []csvimport.Issue
		if in.CSV != nil {
			entries, rejected, err = s.parseEntriesCSV(ctx, tx, scope, *in.CSV)
			if err != nil {
				return err
			}
			if len(rejected) > 0 {
				out = &MillesimalEntries{TableID: id, Entries: entries, Rejected: rejected}
				for _, e := range entries {
					out.SumMillis += e.Millis
				}
				return nil
			}
		}
		if len(entries) > csvimport.MaxRows {
			return invalidField("entries", "too_many")
		}
		now := s.now()
		seen := map[string]bool{}
		ids := make([]string, 0, len(entries))
		rows := make([]store.MillesimalEntry, 0, len(entries))
		for _, e := range entries {
			if e.Millis < 0 || e.Millis > allot.Whole {
				return invalidField("entries", "out_of_range")
			}
			if seen[e.BerthID] {
				return invalidField("entries", "duplicate")
			}
			seen[e.BerthID] = true
			ids = append(ids, e.BerthID)
			rows = append(rows, store.MillesimalEntry{ID: store.NewID(), BerthID: e.BerthID, Millis: e.Millis, CreatedAt: now, UpdatedAt: now})
		}
		bs, err := store.RightsBerthsByIDs(ctx, tx, scope, ids)
		if err != nil {
			return err
		}
		if len(bs) != len(ids) {
			return store.ErrNotFound
		}
		if err := store.ReplaceMillesimalEntries(ctx, tx, scope, id, rows); err != nil {
			return mapImmutable(err)
		}
		t.UpdatedAt = now
		if err := store.UpdateMillesimalTable(ctx, tx, scope, t, ifMatch); err != nil {
			return err
		}
		if out, err = s.entriesView(ctx, tx, scope, id); err != nil {
			return err
		}
		return s.write(ctx, tx, actorOf(p), scope, "millesimal_table.entries", "millesimal_table", id,
			map[string]any{"entries": len(rows), "sum_millis": out.SumMillis}, changed("millesimal_table", id, "entries"))
	})
	return out, err
}

// mapImmutable turns a frozen-row trigger error into ErrTableActive (millesimal) or ErrInvalidTransition (runs).
func mapImmutable(err error) error {
	if errors.Is(err, store.ErrImmutable) {
		return ErrTableActive
	}
	return err
}

// parseEntriesCSV reads "berth code;millis" rows (a first row that is not numeric is a header).
func (s *Service) parseEntriesCSV(ctx context.Context, db bun.IDB, scope store.MarinaScope, text string) ([]MillesimalEntry, []csvimport.Issue, error) {
	t, err := csvimport.Parse(text, 0, false)
	if err != nil {
		return nil, nil, err
	}
	codes, err := store.RightsBerthCodes(ctx, db, scope)
	if err != nil {
		return nil, nil, err
	}
	var out []MillesimalEntry
	rejected := []csvimport.Issue{}
	seen := map[string]bool{}
	for i, rec := range t.Rows {
		line := i + 1
		if len(rec) < 2 {
			rejected = append(rejected, csvimport.Issue{Row: line, Code: "columns"})
			continue
		}
		code, val := strings.TrimSpace(rec[0]), strings.TrimSpace(rec[1])
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			if i == 0 {
				continue // header
			}
			rejected = append(rejected, csvimport.Issue{Row: line, Code: "invalid", Field: "millis", Ref: code})
			continue
		}
		b, ok := codes[code]
		switch {
		case !ok:
			rejected = append(rejected, csvimport.Issue{Row: line, Code: "unknown_berth", Field: "berth_code", Ref: code})
		case n < 0 || n > allot.Whole:
			rejected = append(rejected, csvimport.Issue{Row: line, Code: "out_of_range", Field: "millis", Ref: code})
		case seen[b.ID]:
			rejected = append(rejected, csvimport.Issue{Row: line, Code: "duplicate", Field: "berth_code", Ref: code})
		default:
			seen[b.ID] = true
			out = append(out, MillesimalEntry{BerthID: b.ID, BerthCode: code, Millis: n})
		}
	}
	return out, rejected, nil
}

// ActivateMillesimalTable activates a draft table whose entries sum to exactly 1000 (else 422 table_incomplete,
// INV-41); its entries are frozen from now on (rights.manage).
func (s *Service) ActivateMillesimalTable(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*MillesimalTable, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *MillesimalTable
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.LockMillesimalTable(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if t.Status != TableDraft {
			return ErrTableActive
		}
		if t.Version != ifMatch {
			return ErrVersionMismatch
		}
		es, err := store.EntriesOfTables(ctx, tx, scope, []string{id})
		if err != nil {
			return err
		}
		if sum, err := allot.SumMillis(entriesOf(es[id])); err != nil {
			return &SumError{Err: ErrTableIncomplete, SumMillis: sum}
		}
		now := s.now()
		t.Status, t.ActivatedAt, t.UpdatedAt = TableActive, now, now
		if err := store.UpdateMillesimalTable(ctx, tx, scope, t, ifMatch); err != nil {
			return err
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "millesimal_table.activate", "millesimal_table", t.ID, map[string]any{"status": t.Status},
			changed("millesimal_table", t.ID, event.ChangeActivated)); err != nil {
			return err
		}
		out, err = s.tableView(ctx, tx, scope, t)
		return err
	})
	return out, err
}

func entriesOf(es []store.MillesimalEntry) []allot.Entry {
	out := make([]allot.Entry, len(es))
	for i, e := range es {
		out[i] = allot.Entry{BerthID: e.BerthID, Millis: e.Millis}
	}
	return out
}

// ---- budgets and items ----

// Budget is a budget with its totals.
type Budget struct {
	store.CommonChargeBudget
	BudgetCents int64
	ActualCents *int64
	ItemsCount  int
}

// BudgetItem is an item with the Σ of its issued advances.
type BudgetItem struct {
	store.CommonChargeItem
	AdvancesCents int64
}

func (s *Service) budgetView(ctx context.Context, db bun.IDB, scope store.MarinaScope, b *store.CommonChargeBudget) (*Budget, error) {
	items, err := store.ItemsOfBudget(ctx, db, scope, b.ID)
	if err != nil {
		return nil, err
	}
	out := &Budget{CommonChargeBudget: *b, ItemsCount: len(items)}
	var actual int64
	allActual := len(items) > 0
	for _, it := range items {
		out.BudgetCents += it.BudgetCents
		if it.ActualCents == nil {
			allActual = false
		} else {
			actual += *it.ActualCents
		}
	}
	if allActual {
		out.ActualCents = &actual
	}
	return out, nil
}

// ListBudgets returns the budgets, newest year first (rights.manage).
func (s *Service) ListBudgets(ctx context.Context, p *auth.Principal, marinaID string) ([]Budget, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out []Budget
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		bs, err := store.ListBudgets(ctx, tx, scope)
		if err != nil {
			return err
		}
		out = make([]Budget, 0, len(bs))
		for i := range bs {
			v, err := s.budgetView(ctx, tx, scope, &bs[i])
			if err != nil {
				return err
			}
			out = append(out, *v)
		}
		return nil
	})
	return out, err
}

// GetBudget returns a budget (rights.manage).
func (s *Service) GetBudget(ctx context.Context, p *auth.Principal, marinaID, id string) (*Budget, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	b, err := store.GetBudget(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	return s.budgetView(ctx, s.d.DB.DB, scope, b)
}

// CreateBudget creates the draft budget of a year (one per year: 409) (rights.manage).
func (s *Service) CreateBudget(ctx context.Context, p *auth.Principal, marinaID string, year int, name string) (*Budget, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if year < 2000 || year > 9999 {
		return nil, invalidField("year", "out_of_range")
	}
	if len(name) > 120 {
		return nil, invalidField("name", "too_long")
	}
	var out *Budget
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := s.now()
		b := &store.CommonChargeBudget{ID: store.NewID(), Year: year, Name: name, Status: BudgetDraft, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := store.InsertBudget(ctx, tx, scope, b); err != nil {
			return err
		}
		out = &Budget{CommonChargeBudget: *b}
		return s.write(ctx, tx, actorOf(p), scope, "budget.create", "budget", b.ID, map[string]any{"year": year}, changed("budget", b.ID, "created"))
	})
	return out, err
}

// budgetAction changes the status or name of a budget under its version.
func (s *Service) budgetAction(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, action string, fn func(tx bun.IDB, scope store.MarinaScope, b *store.CommonChargeBudget) error) (*Budget, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *Budget
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockBudget(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if b.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := fn(tx, scope, b); err != nil {
			return err
		}
		b.UpdatedAt = s.now()
		if err := store.UpdateBudget(ctx, tx, scope, b, ifMatch); err != nil {
			return err
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "budget."+action, "budget", b.ID, map[string]any{"status": b.Status},
			changed("budget", b.ID, action)); err != nil {
			return err
		}
		out, err = s.budgetView(ctx, tx, scope, b)
		return err
	})
	return out, err
}

// UpdateBudget renames a budget that is not closed (rights.manage).
func (s *Service) UpdateBudget(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, name *string) (*Budget, error) {
	return s.budgetAction(ctx, p, marinaID, id, ifMatch, "update", func(_ bun.IDB, _ store.MarinaScope, b *store.CommonChargeBudget) error {
		if b.Status == BudgetClosed {
			return ErrInvalidTransition
		}
		if name != nil {
			n := strings.TrimSpace(*name)
			if len(n) > 120 {
				return invalidField("name", "too_long")
			}
			b.Name = n
		}
		return nil
	})
}

// ApproveBudget approves a draft budget with at least one item (rights.manage).
func (s *Service) ApproveBudget(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Budget, error) {
	return s.budgetAction(ctx, p, marinaID, id, ifMatch, "approve", func(tx bun.IDB, scope store.MarinaScope, b *store.CommonChargeBudget) error {
		if b.Status != BudgetDraft {
			return ErrInvalidTransition
		}
		items, err := store.ItemsOfBudget(ctx, tx, scope, b.ID)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return invalidField("items", "required")
		}
		b.Status, b.ApprovedAt = BudgetApproved, s.now()
		return nil
	})
}

// CloseBudget closes an approved budget (rights.manage).
func (s *Service) CloseBudget(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Budget, error) {
	return s.budgetAction(ctx, p, marinaID, id, ifMatch, "close", func(_ bun.IDB, _ store.MarinaScope, b *store.CommonChargeBudget) error {
		if b.Status != BudgetApproved {
			return ErrInvalidTransition
		}
		b.Status, b.ClosedAt = BudgetClosed, s.now()
		return nil
	})
}

// DeleteBudget deletes a draft budget without runs (rights.manage).
func (s *Service) DeleteBudget(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockBudget(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if b.Status != BudgetDraft {
			return ErrInvalidTransition
		}
		if err := store.DeleteBudget(ctx, tx, scope, id, ifMatch); err != nil {
			return err
		}
		return s.write(ctx, tx, actorOf(p), scope, "budget.delete", "budget", id, nil, changed("budget", id, "deleted"))
	})
}

func (s *Service) itemViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, budgetID string, items []store.CommonChargeItem) ([]BudgetItem, error) {
	adv, err := store.IssuedSumsByItem(ctx, db, scope, budgetID, "advance")
	if err != nil {
		return nil, err
	}
	out := make([]BudgetItem, len(items))
	for i := range items {
		out[i] = BudgetItem{CommonChargeItem: items[i], AdvancesCents: adv[items[i].ID]}
	}
	return out, nil
}

// ListBudgetItems returns the items of a budget (rights.manage).
func (s *Service) ListBudgetItems(ctx context.Context, p *auth.Principal, marinaID, budgetID string) ([]BudgetItem, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out []BudgetItem
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetBudget(ctx, tx, scope, budgetID); err != nil {
			return err
		}
		items, err := store.ItemsOfBudget(ctx, tx, scope, budgetID)
		if err != nil {
			return err
		}
		out, err = s.itemViews(ctx, tx, scope, budgetID, items)
		return err
	})
	return out, err
}

// ItemInput are the fields of a budget item (nil = unchanged on update).
type ItemInput struct {
	Name        *string
	TableID     *string
	BudgetCents *int64
	Sort        *int
}

// CreateBudgetItem adds an item to a draft budget (rights.manage).
func (s *Service) CreateBudgetItem(ctx context.Context, p *auth.Principal, marinaID, budgetID string, in ItemInput) (*BudgetItem, error) {
	return s.saveItem(ctx, p, marinaID, budgetID, "", 0, in)
}

// UpdateBudgetItem changes an item of a draft budget when its version equals ifMatch (rights.manage).
func (s *Service) UpdateBudgetItem(ctx context.Context, p *auth.Principal, marinaID, budgetID, id string, ifMatch int, in ItemInput) (*BudgetItem, error) {
	return s.saveItem(ctx, p, marinaID, budgetID, id, ifMatch, in)
}

func (s *Service) saveItem(ctx context.Context, p *auth.Principal, marinaID, budgetID, id string, ifMatch int, in ItemInput) (*BudgetItem, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *BudgetItem
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockBudget(ctx, tx, scope, budgetID)
		if err != nil {
			return err
		}
		if b.Status != BudgetDraft {
			return ErrInvalidTransition
		}
		now := s.now()
		it := &store.CommonChargeItem{ID: store.NewID(), BudgetID: b.ID, Version: 1, CreatedAt: now}
		if id != "" {
			if it, err = store.GetBudgetItem(ctx, tx, scope, b.ID, id); err != nil {
				return err
			}
			if it.Version != ifMatch {
				return ErrVersionMismatch
			}
		} else if in.Name == nil || in.TableID == nil || in.BudgetCents == nil {
			return invalidField("item", "required")
		}
		v := &auth.ValidationError{}
		if in.Name != nil {
			n := strings.TrimSpace(*in.Name)
			if n == "" || len(n) > 120 {
				v.Add("name", "invalid")
			}
			it.Name = n
		}
		if in.TableID != nil {
			if _, err := store.GetMillesimalTable(ctx, tx, scope, *in.TableID); err != nil {
				return err
			}
			it.TableID = *in.TableID
		}
		if in.BudgetCents != nil {
			if *in.BudgetCents < 0 {
				v.Add("budget_cents", "out_of_range")
			}
			it.BudgetCents = *in.BudgetCents
		}
		if in.Sort != nil {
			it.Sort = *in.Sort
		}
		if err := v.Err(); err != nil {
			return err
		}
		it.UpdatedAt = now
		action := "budget.item_create"
		if id == "" {
			err = store.InsertBudgetItem(ctx, tx, scope, it)
		} else {
			action = "budget.item_update"
			err = store.UpdateBudgetItem(ctx, tx, scope, it, ifMatch)
		}
		if err != nil {
			return err
		}
		out = &BudgetItem{CommonChargeItem: *it}
		return s.write(ctx, tx, actorOf(p), scope, action, "budget_item", it.ID, map[string]any{"budget_cents": it.BudgetCents, "table_id": it.TableID},
			changed("budget", b.ID, "items"))
	})
	return out, err
}

// DeleteBudgetItem deletes an item of a draft budget (rights.manage).
func (s *Service) DeleteBudgetItem(ctx context.Context, p *auth.Principal, marinaID, budgetID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockBudget(ctx, tx, scope, budgetID)
		if err != nil {
			return err
		}
		if b.Status != BudgetDraft {
			return ErrInvalidTransition
		}
		if err := store.DeleteBudgetItem(ctx, tx, scope, b.ID, id, ifMatch); err != nil {
			return err
		}
		return s.write(ctx, tx, actorOf(p), scope, "budget.item_delete", "budget_item", id, nil, changed("budget", b.ID, "items"))
	})
}

// Actual is the actual amount of an item.
type Actual struct {
	ItemID      string
	ActualCents int64
}

// SetBudgetActuals records the actual amounts of the items of an approved budget, from JSON or CSV (item
// name;amount in euros) under the budget version (rights.manage). Unknown items are 404 (JSON) or reported as
// a validation error (CSV).
func (s *Service) SetBudgetActuals(ctx context.Context, p *auth.Principal, marinaID, budgetID string, ifMatch int, actuals []Actual, csv *string) ([]BudgetItem, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out []BudgetItem
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockBudget(ctx, tx, scope, budgetID)
		if err != nil {
			return err
		}
		if b.Version != ifMatch {
			return ErrVersionMismatch
		}
		if b.Status != BudgetApproved {
			return ErrInvalidTransition
		}
		items, err := store.ItemsOfBudget(ctx, tx, scope, b.ID)
		if err != nil {
			return err
		}
		byID := map[string]*store.CommonChargeItem{}
		byName := map[string]*store.CommonChargeItem{}
		for i := range items {
			byID[items[i].ID] = &items[i]
			byName[strings.ToLower(items[i].Name)] = &items[i]
		}
		if csv != nil {
			t, err := csvimport.Parse(*csv, 0, false)
			if err != nil {
				return err
			}
			for i, rec := range t.Rows {
				if len(rec) < 2 {
					return invalidField("csv", "columns")
				}
				amount, err := money.ParseCents(rec[1])
				if err != nil {
					if i == 0 {
						continue // header
					}
					return invalidField("csv", "amount_row_"+strconv.Itoa(i+1))
				}
				it := byName[strings.ToLower(strings.TrimSpace(rec[0]))]
				if it == nil {
					return invalidField("csv", "item_row_"+strconv.Itoa(i+1))
				}
				actuals = append(actuals, Actual{ItemID: it.ID, ActualCents: int64(amount)})
			}
		}
		now := s.now()
		for _, a := range actuals {
			it := byID[a.ItemID]
			if it == nil {
				return store.ErrNotFound
			}
			if a.ActualCents < 0 {
				return invalidField("actual_cents", "out_of_range")
			}
			v := a.ActualCents
			it.ActualCents, it.UpdatedAt = &v, now
			if err := store.SetItemActual(ctx, tx, scope, it); err != nil {
				return err
			}
			it.Version++
		}
		b.UpdatedAt = now
		if err := store.UpdateBudget(ctx, tx, scope, b, ifMatch); err != nil {
			return err
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "budget.actuals", "budget", b.ID, map[string]any{"items": len(actuals)},
			changed("budget", b.ID, "actuals")); err != nil {
			return err
		}
		out, err = s.itemViews(ctx, tx, scope, b.ID, items)
		return err
	})
	return out, err
}

// ---- runs ----

// Common run kinds.
const (
	RunAdvance = "advance"
	RunBalance = "balance"
)

// ItemTotal is the total of an item in a run.
type ItemTotal struct {
	ItemID           string `json:"item_id"`
	Name             string `json:"name"`
	AmountCents      int64  `json:"amount_cents"`
	AllocatedCents   int64  `json:"allocated_cents"`
	MarinaShareCents int64  `json:"marina_share_cents"`
}

// RunTotals are the totals of a run (stored as common_charge_runs.totals).
type RunTotals struct {
	Items      []ItemTotal `json:"totals_by_item"`
	TotalCents int64       `json:"total_cents"`
	Holders    int         `json:"holders"`
}

// CommonRun is a run with its totals.
type CommonRun struct {
	store.CommonChargeRun
	Totals RunTotals
}

func commonRunOf(r *store.CommonChargeRun) CommonRun {
	out := CommonRun{CommonChargeRun: *r, Totals: RunTotals{Items: []ItemTotal{}}}
	if len(r.Totals) > 0 {
		_ = json.Unmarshal(r.Totals, &out.Totals)
	}
	if out.Totals.Items == nil {
		out.Totals.Items = []ItemTotal{}
	}
	return out
}

// ListCommonRuns returns the runs of a budget (rights.manage).
func (s *Service) ListCommonRuns(ctx context.Context, p *auth.Principal, marinaID, budgetID string) ([]CommonRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetBudget(ctx, s.d.DB.DB, scope, budgetID); err != nil {
		return nil, err
	}
	rows, err := store.RunsOfBudget(ctx, s.d.DB.DB, scope, budgetID)
	if err != nil {
		return nil, err
	}
	out := make([]CommonRun, len(rows))
	for i := range rows {
		out[i] = commonRunOf(&rows[i])
	}
	return out, nil
}

// GetCommonRun returns a run (rights.manage).
func (s *Service) GetCommonRun(ctx context.Context, p *auth.Principal, marinaID, id string) (*CommonRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	r, err := store.GetCommonRun(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, err
	}
	out := commonRunOf(r)
	return &out, nil
}

// CommonRunInput is a new run: kind advance (share_bp of the budget) or balance (actual − issued advances).
type CommonRunInput struct {
	Kind    string
	ShareBP *int64
	Plan    []byte
}

// CreateCommonRun creates a draft run of an approved budget and allocates it (allot.Common): advances on the
// holders of year-01-01, the balance on the holders of year-12-31 (§15 D17). Every item's table must be active
// with Σ = 1000 (422 table_incomplete) (rights.manage).
func (s *Service) CreateCommonRun(ctx context.Context, p *auth.Principal, marinaID, budgetID string, in CommonRunInput) (*CommonRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	switch in.Kind {
	case RunAdvance:
		if in.ShareBP == nil || *in.ShareBP < 0 || *in.ShareBP > 10_000 {
			v.Add("share_bp", "required")
		}
	case RunBalance:
		if in.ShareBP != nil {
			v.Add("share_bp", "not_allowed")
		}
	default:
		v.Add("kind", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	planJSON, _, err := ParsePlan(in.Plan)
	if err != nil {
		return nil, err
	}
	var out CommonRun
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		b, err := store.LockBudget(ctx, tx, scope, budgetID)
		if err != nil {
			return err
		}
		if b.Status != BudgetApproved {
			return ErrInvalidTransition
		}
		items, err := store.ItemsOfBudget(ctx, tx, scope, b.ID)
		if err != nil {
			return err
		}
		ref := calendar.New(b.Year, 1, 1)
		amounts := map[string]money.Cents{}
		if in.Kind == RunAdvance {
			for _, it := range items {
				amounts[it.ID] = allot.AdvanceAmount(money.Cents(it.BudgetCents), *in.ShareBP)
			}
		} else {
			ref = calendar.New(b.Year, 12, 31)
			adv, err := store.IssuedSumsByItem(ctx, tx, scope, b.ID, RunAdvance)
			if err != nil {
				return err
			}
			for _, it := range items {
				if it.ActualCents == nil {
					return invalidField("actual_cents", "required")
				}
				amounts[it.ID] = money.Cents(*it.ActualCents - adv[it.ID])
			}
		}
		allocs, totals, err := s.allocate(ctx, tx, scope, items, amounts, ref)
		if err != nil {
			return err
		}
		seq, err := store.NextCommonRunSeq(ctx, tx, scope, b.ID, in.Kind)
		if err != nil {
			return err
		}
		now := s.now()
		r := &store.CommonChargeRun{
			ID: store.NewID(), BudgetID: b.ID, Kind: in.Kind, Seq: seq, ShareBP: in.ShareBP, ReferenceOn: ref, Status: CommonRunDraft,
			Totals: store.MustJSON(totals), Plan: planJSON, CreatedByUserID: userOf(p), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertCommonRun(ctx, tx, scope, r); err != nil {
			return err
		}
		rows := make([]store.CommonChargeAllocation, len(allocs))
		for i, a := range allocs {
			rows[i] = store.CommonChargeAllocation{
				ID: store.NewID(), RunID: r.ID, ItemID: a.ItemID, BerthID: a.BerthID, RightID: a.RightID, CustomerID: a.CustomerID,
				BerthShareCents: int64(a.BerthShare), AmountCents: int64(a.Amount), CreatedAt: now,
			}
		}
		if err := store.InsertCommonAllocations(ctx, tx, scope, rows); err != nil {
			return err
		}
		out = commonRunOf(r)
		return s.write(ctx, tx, actorOf(p), scope, "common_run.create", "common_run", r.ID,
			map[string]any{"kind": r.Kind, "seq": r.Seq, "total_cents": totals.TotalCents, "allocations": len(rows)},
			changed("common_run", r.ID, "created"))
	})
	return &out, err
}

// allocate runs allot.Common for the items with their amounts on the holders of ref and returns the rows and the
// totals per item (allocated = Σ rows, marina share = rows without customer).
func (s *Service) allocate(ctx context.Context, tx bun.IDB, scope store.MarinaScope, items []store.CommonChargeItem, amounts map[string]money.Cents, ref calendar.Date) ([]allot.Allocation, *RunTotals, error) {
	tableIDs := make([]string, 0, len(items))
	for _, it := range items {
		tableIDs = append(tableIDs, it.TableID)
	}
	tables := map[string]*store.MillesimalTable{}
	for _, id := range tableIDs {
		if _, ok := tables[id]; ok {
			continue
		}
		t, err := store.GetMillesimalTable(ctx, tx, scope, id)
		if err != nil {
			return nil, nil, err
		}
		tables[id] = t
	}
	entries, err := store.EntriesOfTables(ctx, tx, scope, tableIDs)
	if err != nil {
		return nil, nil, err
	}
	in := make([]allot.Item, 0, len(items))
	for _, it := range items {
		es := entriesOf(entries[it.TableID])
		sum, err := allot.SumMillis(es)
		if err != nil || tables[it.TableID].Status != TableActive {
			return nil, nil, &SumError{Err: ErrTableIncomplete, SumMillis: sum}
		}
		in = append(in, allot.Item{ID: it.ID, Amount: amounts[it.ID], Entries: es})
	}
	holders, err := s.holdersOn(ctx, tx, scope, ref)
	if err != nil {
		return nil, nil, err
	}
	allocs, err := allot.Common(in, holders)
	if err != nil {
		if errors.Is(err, allot.ErrTableIncomplete) {
			return nil, nil, &SumError{Err: ErrTableIncomplete}
		}
		return nil, nil, &SumError{Err: ErrSharesInvalid}
	}
	totals := &RunTotals{Items: make([]ItemTotal, 0, len(items))}
	idx := map[string]int{}
	for i, it := range items {
		idx[it.ID] = i
		totals.Items = append(totals.Items, ItemTotal{ItemID: it.ID, Name: it.Name, AmountCents: int64(amounts[it.ID])})
	}
	custs := map[string]bool{}
	for _, a := range allocs {
		t := &totals.Items[idx[a.ItemID]]
		t.AllocatedCents += int64(a.Amount)
		if a.CustomerID == "" {
			t.MarinaShareCents += int64(a.Amount)
		} else {
			custs[a.CustomerID] = true
		}
		totals.TotalCents += int64(a.Amount)
	}
	totals.Holders = len(custs)
	return allocs, totals, nil
}

// holdersOn returns the holders of the rights active on a date by berth (rights transferred or ended later count
// for the nights they still covered on that date).
func (s *Service) holdersOn(ctx context.Context, tx bun.IDB, scope store.MarinaScope, on calendar.Date) (map[string]allot.BerthHolders, error) {
	rs, err := store.RightsActiveOn(ctx, tx, scope, on, nil)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rs))
	for i := range rs {
		ids[i] = rs[i].ID
	}
	hs, err := store.HoldersOfRights(ctx, tx, scope, ids)
	if err != nil {
		return nil, err
	}
	byRight := map[string][]allot.Share{}
	for _, h := range hs {
		byRight[h.RightID] = append(byRight[h.RightID], allot.Share{CustomerID: h.CustomerID, Millis: h.ShareMillis})
	}
	out := map[string]allot.BerthHolders{}
	for _, r := range rs {
		out[r.BerthID] = allot.BerthHolders{BerthID: r.BerthID, RightID: r.ID, Shares: byRight[r.ID]}
	}
	return out, nil
}

// IssueCommonRun issues a draft run: its allocations are frozen (INV-44), at most one issued balance run per
// budget (a concurrent issue is 409), and the holders get their instalments: an advance is planned with the run
// plan, a balance is one instalment per holder (a credit when negative) (rights.manage).
func (s *Service) IssueCommonRun(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*CommonRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out CommonRun
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.LockCommonRun(ctx, tx, scope, id); err != nil {
			return err
		}
		r, err := store.GetCommonRun(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		next, ok := Next(CommonRunTransitions, r.Status, "issue")
		if !ok {
			return ErrInvalidTransition
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		b, err := store.LockBudget(ctx, tx, scope, r.BudgetID)
		if err != nil {
			return err
		}
		if b.Status != BudgetApproved {
			return ErrInvalidTransition
		}
		// INV-41 re-checked in the issue transaction: the tables are still active (they cannot change once active).
		items, err := store.ItemsOfBudget(ctx, tx, scope, b.ID)
		if err != nil {
			return err
		}
		for _, it := range items {
			t, err := store.GetMillesimalTable(ctx, tx, scope, it.TableID)
			if err != nil {
				return err
			}
			if t.Status != TableActive {
				return &SumError{Err: ErrTableIncomplete}
			}
		}
		allocs, err := store.AllocationsOfRun(ctx, tx, scope, r.ID, store.AllocationFilterCommon{})
		if err != nil {
			return err
		}
		perCustomer := map[string]int64{}
		rightsOf := map[string]map[string]bool{}
		var order []string
		var total int64
		for _, a := range allocs {
			total += a.AmountCents
			if a.CustomerID == "" {
				continue
			}
			if _, ok := perCustomer[a.CustomerID]; !ok {
				order = append(order, a.CustomerID)
				rightsOf[a.CustomerID] = map[string]bool{}
			}
			perCustomer[a.CustomerID] += a.AmountCents
			rightsOf[a.CustomerID][a.RightID] = true
		}
		_, pol, err := ParsePlan(r.Plan)
		if err != nil {
			return err
		}
		now := s.now()
		today := s.today(ctx, tx, scope)
		from, to := calendar.New(b.Year, 1, 1), calendar.New(b.Year+1, 1, 1)
		source := SourceCommonAdvance
		if r.Kind == RunBalance {
			source = SourceCommonBalance
			from, to = today, today.AddDays(31)
		}
		var rows []store.HolderInstallment
		for _, cid := range order {
			amount := money.Cents(perCustomer[cid])
			if amount == 0 {
				continue
			}
			items, err := planHolder(amount, pol, from, to, today)
			if err != nil {
				return invalidField("plan", "invalid")
			}
			rightID := ""
			if len(rightsOf[cid]) == 1 {
				for k := range rightsOf[cid] {
					rightID = k
				}
			}
			rows = append(rows, holderRowsOf(items, source, r.ID, r.ID, rightID, cid, 1, now)...)
		}
		r.Status, r.IssuedAt, r.UpdatedAt = next, now, now
		if err := store.UpdateCommonRun(ctx, tx, scope, r, ifMatch); err != nil {
			return err
		}
		if err := store.InsertHolderInstallments(ctx, tx, scope, rows); err != nil {
			return err
		}
		out = commonRunOf(r)
		return s.write(ctx, tx, actorOf(p), scope, "common_run.issue", "common_run", r.ID,
			map[string]any{"kind": r.Kind, "total_cents": total, "installments": len(rows)},
			ev{event.CommonChargesIssued, event.RunIssuedData{RunID: r.ID, Year: b.Year, Kind: r.Kind, TotalCents: total, Holders: len(order)}})
	})
	return &out, err
}

// DiscardCommonRun discards a draft run (rights.manage).
func (s *Service) DiscardCommonRun(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*CommonRun, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out CommonRun
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.LockCommonRun(ctx, tx, scope, id); err != nil {
			return err
		}
		r, err := store.GetCommonRun(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		next, ok := Next(CommonRunTransitions, r.Status, "discard")
		if !ok {
			return ErrInvalidTransition
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		r.Status, r.UpdatedAt = next, s.now()
		if err := store.UpdateCommonRun(ctx, tx, scope, r, ifMatch); err != nil {
			return err
		}
		out = commonRunOf(r)
		return s.write(ctx, tx, actorOf(p), scope, "common_run.discard", "common_run", r.ID, map[string]any{"status": r.Status},
			changed("common_run", r.ID, event.ChangeDiscarded))
	})
	return &out, err
}

// CommonAllocation is an allocation with the berth code and the holder name (with customers.read).
type CommonAllocation struct {
	store.CommonChargeAllocation
	BerthCode   string
	DisplayName string
}

func (s *Service) allocationViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, rows []store.CommonChargeAllocation) ([]CommonAllocation, error) {
	var bids, cids []string
	for _, r := range rows {
		bids = append(bids, r.BerthID)
		if r.CustomerID != "" {
			cids = append(cids, r.CustomerID)
		}
	}
	bs, err := store.RightsBerthsByIDs(ctx, db, scope, bids)
	if err != nil {
		return nil, err
	}
	nm, err := names(ctx, db, scope, can(p, scope, auth.PermCustomersRead), cids)
	if err != nil {
		return nil, err
	}
	out := make([]CommonAllocation, len(rows))
	for i, r := range rows {
		out[i] = CommonAllocation{CommonChargeAllocation: r, BerthCode: bs[r.BerthID].Code, DisplayName: nm[r.CustomerID]}
	}
	return out, nil
}

// ListCommonAllocations returns a page of the allocations of a run (rights.manage).
func (s *Service) ListCommonAllocations(ctx context.Context, p *auth.Principal, marinaID, runID string, f store.AllocationFilterCommon, cursor string, limit int) (store.Page[CommonAllocation], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return store.Page[CommonAllocation]{}, err
	}
	var out store.Page[CommonAllocation]
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetCommonRun(ctx, tx, scope, runID); err != nil {
			return err
		}
		page, err := store.PageAllocationsOfRun(ctx, tx, scope, runID, f, cursor, limit)
		if err != nil {
			return err
		}
		items, err := s.allocationViews(ctx, tx, scope, p, page.Items)
		out = store.Page[CommonAllocation]{Items: items, NextCursor: page.NextCursor}
		return err
	})
	return out, err
}

// AllCommonAllocations returns every allocation of a run (CSV, rights.manage).
func (s *Service) AllCommonAllocations(ctx context.Context, p *auth.Principal, marinaID, runID string, f store.AllocationFilterCommon) ([]CommonAllocation, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out []CommonAllocation
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetCommonRun(ctx, tx, scope, runID); err != nil {
			return err
		}
		rows, err := store.AllocationsOfRun(ctx, tx, scope, runID, f)
		if err != nil {
			return err
		}
		out, err = s.allocationViews(ctx, tx, scope, p, rows)
		return err
	})
	return out, err
}
