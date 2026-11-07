package csvimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Env are the dependencies of the import runs (database, clock, audit).
type Env struct {
	DB    *store.DB
	Clock clock.Clock
	Audit *audit.Writer
}

// Input is one import request.
type Input struct {
	Entity    string
	CSV       string
	Delimiter string // semicolon, comma, tab ("" = detect)
	HasHeader bool
	Mapping   Mapping
	DryRun    bool
}

// errRejected aborts the transaction of an apply whose report has rejected rows.
var errRejected = errors.New("csvimport: rows rejected")

// Permission returns the permission an entity needs.
func Permission(entity string) auth.Permission {
	switch entity {
	case EntityContracts:
		return auth.PermContractsWrite
	case EntityRights:
		return auth.PermRightsManage
	default:
		return auth.PermCustomersWrite
	}
}

func readPermission(entity string) auth.Permission {
	switch entity {
	case EntityContracts:
		return auth.PermContractsRead
	case EntityRights:
		return auth.PermRightsManage
	default:
		return auth.PermCustomersRead
	}
}

// Run imports a file: the dry run validates every row and stores only the run with its report; the apply writes
// the valid rows in one transaction and stores what it created for the undo; a file with rejected rows writes
// nothing and records a failed run.
func (r *Registry) Run(ctx context.Context, env Env, p *auth.Principal, marinaID string, in Input) (*Report, error) {
	// The marina first: another marina's id is 404 whatever the body says.
	if _, err := p.Authorize(ctx, env.DB.DB, marinaID, auth.PermMarinaRead); err != nil {
		return nil, err
	}
	h, err := r.Handler(in.Entity)
	if err != nil {
		return nil, err
	}
	scope, err := p.Authorize(ctx, env.DB.DB, marinaID, Permission(in.Entity))
	if err != nil {
		return nil, err
	}
	var delim rune
	if in.Delimiter != "" {
		d, ok := Delimiters[in.Delimiter]
		if !ok {
			return nil, fmt.Errorf("%w: delimiter", ErrCSVInvalid)
		}
		delim = d
	}
	t, err := Parse(in.CSV, delim, in.HasHeader)
	if err != nil {
		return nil, err
	}
	for f := range in.Mapping {
		if !contains(h.Fields(), f) {
			return nil, fmt.Errorf("%w: unknown field %s", ErrCSVInvalid, f)
		}
	}
	rows, err := t.Mapped(in.Mapping)
	if err != nil {
		return nil, err
	}
	rep := &Report{Entity: in.Entity, DryRun: in.DryRun, Rows: len(rows), Rejected: []Issue{}, Duplicates: []Issue{}}
	now := store.UTC(env.Clock.Now())
	run := &store.ImportRun{
		ID: store.NewID(), MarinaID: scope.ID(), Entity: in.Entity, Mapping: store.MustJSON(in.Mapping),
		FileSHA256: crypto.SHA256Hex(in.CSV), RowCount: len(rows), CreatedByUserID: p.UserID, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	rep.RunID = run.ID
	if in.DryRun {
		if err := h.DryRun(ctx, env.DB.DB, scope, rows, rep); err != nil {
			return nil, err
		}
		rep.Valid = len(rows) - rejectedRows(rep)
		run.Status = store.ImportDryRun
		return rep, r.saveRun(ctx, env, scope, p, run, rep, nil, "import.dry_run")
	}
	var created []Created
	err = env.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		created, err = h.Apply(ctx, tx, scope, rows, rep)
		if err != nil {
			return err
		}
		if len(rep.Rejected) > 0 {
			return errRejected
		}
		rep.Valid = len(rows)
		rep.Created = map[string]int{}
		for _, c := range created {
			rep.Created[c.EntityType]++
		}
		run.Status, run.AppliedAt = store.ImportApplied, now
		return r.saveRunTx(ctx, tx, env, scope, p, run, rep, created, "import.apply")
	})
	if errors.Is(err, errRejected) {
		rep.Valid = len(rows) - rejectedRows(rep)
		rep.Created = nil
		run.Status = store.ImportFailed
		return rep, r.saveRun(ctx, env, scope, p, run, rep, nil, "import.apply")
	}
	if err != nil {
		return nil, err
	}
	return rep, nil
}

func rejectedRows(rep *Report) int {
	seen := map[int]bool{}
	for _, i := range rep.Rejected {
		seen[i.Row] = true
	}
	return len(seen)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func (r *Registry) saveRun(ctx context.Context, env Env, scope store.MarinaScope, p *auth.Principal, run *store.ImportRun, rep *Report, created []Created, action string) error {
	return env.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		return r.saveRunTx(ctx, tx, env, scope, p, run, rep, created, action)
	})
}

func (r *Registry) saveRunTx(ctx context.Context, tx bun.IDB, env Env, scope store.MarinaScope, p *auth.Principal, run *store.ImportRun, rep *Report, created []Created, action string) error {
	run.Report = store.MustJSON(rep)
	if err := store.InsertImportRun(ctx, tx, run); err != nil {
		return err
	}
	items := make([]store.ImportRunItem, 0, len(created))
	now := store.UTC(env.Clock.Now())
	for _, c := range created {
		items = append(items, store.ImportRunItem{
			ID: store.NewID(), MarinaID: scope.ID(), RunID: run.ID, EntityType: c.EntityType, EntityID: c.EntityID, CreatedAt: now,
		})
	}
	if err := store.InsertImportRunItems(ctx, tx, items); err != nil {
		return err
	}
	return env.Audit.Write(ctx, tx, audit.Entry{
		OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: action, EntityType: "import_run",
		EntityID: run.ID, Diff: map[string]any{
			"entity": run.Entity, "status": run.Status, "rows": run.RowCount, "rejected": len(rep.Rejected),
			"file_sha256": run.FileSHA256,
		},
	})
}

// RunView is a run with its decoded report.
type RunView struct {
	store.ImportRun
	Decoded  Report
	MapOf    Mapping
	Undoable bool
}

func view(row store.ImportRun) RunView {
	v := RunView{ImportRun: row}
	_ = json.Unmarshal(row.Report, &v.Decoded)
	_ = json.Unmarshal(row.Mapping, &v.MapOf)
	v.Undoable = row.Status == store.ImportApplied
	return v
}

// List returns the runs of the marina (customers.read; contracts and rights runs need their read permission).
func (r *Registry) List(ctx context.Context, env Env, p *auth.Principal, marinaID, entity, cursor string, limit int) (store.Page[RunView], error) {
	perm := auth.PermCustomersRead
	if entity != "" {
		perm = readPermission(entity)
	}
	scope, err := p.Authorize(ctx, env.DB.DB, marinaID, perm)
	if err != nil {
		return store.Page[RunView]{}, err
	}
	page, err := store.ListImportRuns(ctx, env.DB.DB, scope, entity, cursor, limit)
	if err != nil {
		return store.Page[RunView]{}, err
	}
	out := store.Page[RunView]{Items: make([]RunView, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, row := range page.Items {
		if p.Can(scope.OrgID(), scope.ID(), readPermission(row.Entity)) {
			out.Items = append(out.Items, view(row))
		}
	}
	return out, nil
}

// Get returns a run (the read permission of its entity).
func (r *Registry) Get(ctx context.Context, env Env, p *auth.Principal, marinaID, id string) (*RunView, error) {
	scope, err := p.Authorize(ctx, env.DB.DB, marinaID, auth.PermMarinaRead)
	if err != nil {
		return nil, err
	}
	row, err := store.GetImportRun(ctx, env.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	if !p.Can(scope.OrgID(), scope.ID(), readPermission(row.Entity)) {
		return nil, auth.ErrForbidden
	}
	v := view(*row)
	return &v, nil
}

// Undo removes the records of an applied run in one transaction while they are untouched (ErrNotUndoable
// otherwise; the permission of its entity).
func (r *Registry) Undo(ctx context.Context, env Env, p *auth.Principal, marinaID, id string, ifMatch int) (*RunView, error) {
	scope, err := p.Authorize(ctx, env.DB.DB, marinaID, auth.PermMarinaRead)
	if err != nil {
		return nil, err
	}
	var out *RunView
	err = env.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetImportRun(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if !p.Can(scope.OrgID(), scope.ID(), Permission(row.Entity)) {
			return auth.ErrForbidden
		}
		if ifMatch > 0 && row.Version != ifMatch {
			return store.ErrVersionMismatch
		}
		if row.Status != store.ImportApplied {
			return ErrNotUndoable
		}
		h, err := r.Handler(row.Entity)
		if err != nil {
			return err
		}
		items, err := store.ImportRunItems(ctx, tx, scope, row.ID)
		if err != nil {
			return err
		}
		created := make([]Created, len(items))
		for i, it := range items {
			created[i] = Created{EntityType: it.EntityType, EntityID: it.EntityID}
		}
		if err := h.Undo(ctx, tx, scope, created); err != nil {
			return err
		}
		ok, err := store.MarkImportUndone(ctx, tx, scope, row.ID, row.Version, env.Clock.Now())
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotUndoable
		}
		if err := env.Audit.Write(ctx, tx, audit.Entry{
			OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "import.undo",
			EntityType: "import_run", EntityID: row.ID, Diff: map[string]any{"entity": row.Entity, "records": len(items)},
		}); err != nil {
			return err
		}
		fresh, err := store.GetImportRun(ctx, tx, scope, row.ID)
		if err != nil {
			return err
		}
		v := view(*fresh)
		out = &v
		return nil
	})
	return out, err
}

func notFound(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}
