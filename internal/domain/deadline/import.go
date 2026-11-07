package deadline

import (
	"context"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/store"
)

// SourceImport is the source type of imported deadlines (one per row; they are not reconciled by the engine).
const SourceImport = "import"

// ImportEnv returns the environment of the import runs (the admin handler runs every entity import with it).
func (s *Service) ImportEnv() csvimport.Env {
	return csvimport.Env{DB: s.d.DB, Clock: s.d.Clock, Audit: s.d.Audit}
}

// ImportHandler returns the CSV import handler of the deadlines entity (m4-plan §3.7, §15 D27): one manual
// deadline per row with a kind, a due date and optionally the customer (fiscal code or VAT number) and the vessel
// (registration) it concerns.
func (s *Service) ImportHandler() csvimport.EntityHandler { return deadlineImport{s: s} }

type deadlineImport struct{ s *Service }

const (
	fKind         = "kind"
	fDueOn        = "due_on"
	fCustomerCode = "customer_fiscal_code"
	fVesselReg    = "vessel_registration"
	fNote         = "note"
)

func (deadlineImport) Entity() string { return csvimport.EntityDeadlines }

func (deadlineImport) Fields() []string {
	return []string{fKind, fDueOn, fCustomerCode, fVesselReg, fNote}
}

type deadlineRow struct {
	line       int
	kind       string
	due        calendar.Date
	customerID string
	vesselID   string
	note       string
}

func (h deadlineImport) parse(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) ([]deadlineRow, error) {
	var codes, regs []string
	for _, r := range rows {
		if c := strings.ToUpper(r.Get(fCustomerCode)); c != "" {
			codes = append(codes, c)
		}
		if v := strings.ToUpper(r.Get(fVesselReg)); v != "" {
			regs = append(regs, v)
		}
	}
	customers, err := store.CustomerIDsByFiscalCode(ctx, db, scope, codes)
	if err != nil {
		return nil, err
	}
	vessels, err := store.VesselIDsByRegistration(ctx, db, scope, regs)
	if err != nil {
		return nil, err
	}
	out := make([]deadlineRow, 0, len(rows))
	for _, r := range rows {
		ok := true
		d := deadlineRow{line: r.Line, kind: r.Get(fKind), note: r.Get(fNote)}
		if !slices.Contains(AllKinds(), d.kind) {
			rep.Reject(r.Line, "kind_invalid", fKind, "")
			ok = false
		}
		due, err := calendar.Parse(r.Get(fDueOn))
		if err != nil {
			rep.Reject(r.Line, "date_invalid", fDueOn, "")
			ok = false
		}
		d.due = due
		if c := strings.ToUpper(r.Get(fCustomerCode)); c != "" {
			if d.customerID = customers[c]; d.customerID == "" {
				rep.Reject(r.Line, "customer_unknown", fCustomerCode, "")
				ok = false
			}
		}
		if v := strings.ToUpper(r.Get(fVesselReg)); v != "" {
			if d.vesselID = vessels[v]; d.vesselID == "" {
				rep.Reject(r.Line, "vessel_unknown", fVesselReg, v)
				ok = false
			}
		}
		if len([]rune(d.note)) > maxNote {
			rep.Reject(r.Line, "too_long", fNote, "")
			ok = false
		}
		if ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func (h deadlineImport) DryRun(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) error {
	_, err := h.parse(ctx, db, scope, rows, rep)
	return err
}

func (h deadlineImport) Apply(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) ([]csvimport.Created, error) {
	parsed, err := h.parse(ctx, tx, scope, rows, rep)
	if err != nil || len(rep.Rejected) > 0 {
		return nil, err
	}
	now := h.s.now()
	out := make([]csvimport.Created, 0, len(parsed))
	for _, p := range parsed {
		id := store.NewID()
		d := &store.Deadline{
			ID: id, MarinaID: scope.ID(), SourceType: SourceImport, SourceID: id, Kind: p.kind, CustomerID: p.customerID,
			VesselID: p.vesselID, DueOn: p.due, Status: StatusOpen, Note: p.note, NotifiedAt: store.JSON("[]"),
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertDeadline(ctx, tx, d); err != nil {
			return nil, err
		}
		out = append(out, csvimport.Created{EntityType: "deadline", EntityID: id})
	}
	return out, nil
}

// Undo deletes the imported deadlines while they are untouched (version 1).
func (h deadlineImport) Undo(ctx context.Context, tx bun.IDB, scope store.MarinaScope, created []csvimport.Created) error {
	for _, c := range created {
		ok, err := store.DeleteRow(ctx, tx, scope, "deadlines", c.EntityID, 1)
		if err != nil {
			return err
		}
		if !ok {
			return csvimport.ErrNotUndoable
		}
	}
	return nil
}
