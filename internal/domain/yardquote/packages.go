package yardquote

import (
	"context"
	"fmt"
	"sort"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/store"
)

// Work packages with insert-only versions (m10-plan §3.1, §3.6): saving inserts version N+1, nothing is updated
// in place, so an old quote keeps the formulas of the version it was priced with (INV-28).

const maxPackageLines = 100

// ListPackages lists the packages with their current version (yard.read).
func (s *Service) ListPackages(ctx context.Context, p *auth.Principal, marinaID string, f PackageFilter, cursor string, limit int) ([]PackageView, string, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, "", err
	}
	db := s.d.DB.DB
	page, err := store.ListWorkPackages(ctx, db, scope, store.WorkPackageFilter{Active: f.Active, WorkTypeID: f.WorkTypeID}, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]PackageView, 0, len(page.Items))
	for _, pk := range page.Items {
		pv := PackageView{Row: pk}
		if pk.CurrentVersion > 0 {
			ver, err := store.GetWorkPackageVersion(ctx, db, scope, pk.ID, pk.CurrentVersion)
			if err != nil {
				return nil, "", err
			}
			pv.Content, pv.Lines = ver, decodeLines(ver.Lines)
		}
		out = append(out, pv)
	}
	return out, page.NextCursor, nil
}

// GetPackage returns a package with a version (0 = current) and its history (yard.read).
func (s *Service) GetPackage(ctx context.Context, p *auth.Principal, marinaID, id string, version int) (*PackageView, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return s.getPackage(ctx, s.d.DB.DB, scope, id, version)
}

func (s *Service) getPackage(ctx context.Context, db bun.IDB, scope store.MarinaScope, id string, version int) (*PackageView, error) {
	pk, err := store.GetWorkPackage(ctx, db, scope, id, false)
	if err != nil {
		return nil, notFound(err)
	}
	if version == 0 {
		version = pk.CurrentVersion
	}
	out := &PackageView{Row: *pk}
	if version > 0 {
		ver, err := store.GetWorkPackageVersion(ctx, db, scope, id, version)
		if err != nil {
			return nil, notFound(err)
		}
		out.Content, out.Lines = ver, decodeLines(ver.Lines)
	}
	if out.Versions, err = store.ListWorkPackageVersions(ctx, db, scope, id); err != nil {
		return nil, err
	}
	return out, nil
}

// checkPackageLines validates the lines and job variables of a version: references, formulas on the test vessel
// and the per-line test cases. It returns the normalised lines (by seq) and job variables (declared plus every job
// variable the formulas read).
func (s *Service) checkPackageLines(ctx context.Context, tx bun.IDB, scope store.MarinaScope, lines []PackageLine, jobVars []string) ([]PackageLine, []string, error) {
	v := &auth.ValidationError{}
	if len(lines) == 0 || len(lines) > maxPackageLines {
		v.Add("lines", "invalid")
		return nil, nil, v
	}
	out := append([]PackageLine(nil), lines...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	seen := map[int]bool{}
	ids := make([]string, 0, len(out))
	for i := range out {
		l := &out[i]
		f := fmt.Sprintf("lines[%d]", i)
		if l.Seq < 1 || seen[l.Seq] {
			v.Add(f+".seq", "invalid")
		}
		seen[l.Seq] = true
		if l.QtyFormula == "" {
			v.Add(f+".qty_formula", "required")
		}
		if n, ok := cleanText(l.Notes, 500); !ok {
			v.Add(f+".notes", "invalid")
		} else {
			l.Notes = n
		}
		if l.Task != nil {
			l.Task.Title = checkI18n(v, f+".task.title", l.Task.Title, 200)
			if len(l.Task.Checklist) > 40 {
				v.Add(f+".task.checklist", "invalid")
			}
			for j, c := range l.Task.Checklist {
				if c2, ok := cleanText(c, 200); !ok || c2 == "" {
					v.Add(fmt.Sprintf("%s.task.checklist[%d]", f, j), "invalid")
				}
			}
		}
		ids = append(ids, l.ItemID)
	}
	items, err := store.ItemsByIDs(ctx, tx, scope, ids)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]store.Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	wts, err := store.ListWorkTypes(ctx, tx, scope, nil)
	if err != nil {
		return nil, nil, err
	}
	wtIDs := map[string]bool{}
	for _, w := range wts {
		wtIDs[w.ID] = true
	}
	for i, l := range out {
		f := fmt.Sprintf("lines[%d]", i)
		it, ok := byID[l.ItemID]
		if !ok {
			v.Add(f+".item_id", "not_found")
		} else if !contains(yardItemTypes, it.ItemType) {
			v.Add(f+".item_id", "not_yard_item")
		}
		if l.WorkTypeID != "" && !wtIDs[l.WorkTypeID] {
			v.Add(f+".work_type_id", "not_found")
		}
	}
	if err := v.Err(); err != nil {
		return nil, nil, err
	}
	ce, err := loadCatalogEnv(ctx, tx, scope)
	if err != nil {
		return nil, nil, err
	}
	jobs := map[string]bool{}
	for _, j := range jobVars {
		d, ok := ce.vars[j]
		if !ok || d.Source != store.FormulaSourceJob {
			return nil, nil, &FormulaError{Issues: []Issue{{Code: formula.CodeUnknownVariable, Field: "job_vars", Ref: j}}}
		}
		jobs[j] = true
	}
	val, err := s.newValidator(ctx, tx, scope, ce)
	if err != nil {
		return nil, nil, err
	}
	prices := s.listPrices(ctx, tx, scope, ids)
	srcs := []string{}
	for i, l := range out {
		val.lineFormulas(fmt.Sprintf("lines[%d].", i), l.QtyFormula, l.PriceFormula, l.TestCase, centsPtr(prices, l.ItemID))
		srcs = append(srcs, l.QtyFormula, l.PriceFormula)
	}
	if err := val.err(); err != nil {
		return nil, nil, err
	}
	used, _ := ce.closure(srcs)
	for name := range used {
		if d, ok := ce.vars[name]; ok && d.Source == store.FormulaSourceJob {
			jobs[name] = true
		}
	}
	jv := make([]string, 0, len(jobs))
	for j := range jobs {
		jv = append(jv, j)
	}
	sort.Strings(jv)
	return out, jv, nil
}

// insertVersion inserts version n of a package.
func (s *Service) insertVersion(ctx context.Context, tx bun.IDB, scope store.MarinaScope, p *auth.Principal, pk *store.WorkPackage, n int, lines []PackageLine, jobVars []string) error {
	ver := &store.WorkPackageVersion{
		ID: store.NewID(), PackageID: pk.ID, Version: n, LanguageVersion: formula.LanguageVersion, Lines: store.MustJSON(lines),
		JobVars: store.Strings(jobVars), CreatedByUserID: p.UserID, CreatedAt: s.now(),
	}
	return store.InsertWorkPackageVersion(ctx, tx, scope, ver)
}

// CreatePackage creates a package with its version 1 (yard.manage; 422 formula_invalid).
func (s *Service) CreatePackage(ctx context.Context, p *auth.Principal, marinaID string, in PackageInput) (*PackageView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !packageCode.MatchString(in.Code) {
		v.Add("code", "invalid")
	}
	name := checkI18n(v, "name", in.Name, 100)
	if _, err := store.GetWorkType(ctx, s.d.DB.DB, scope, in.WorkTypeID); err != nil {
		v.Add("work_type_id", "not_found")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	pk := &store.WorkPackage{
		ID: store.NewID(), Code: in.Code, Name: i18nJSON(name), WorkTypeID: in.WorkTypeID, Active: in.Active == nil || *in.Active,
		CurrentVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		lines, jobs, err := s.checkPackageLines(ctx, tx, scope, in.Lines, in.JobVars)
		if err != nil {
			return err
		}
		if err := store.InsertWorkPackage(ctx, tx, scope, pk); err != nil {
			return writeErr(err)
		}
		if err := s.insertVersion(ctx, tx, scope, p, pk, 1, lines, jobs); err != nil {
			return writeErr(err)
		}
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "work_package.create", "package", pk.ID, map[string]any{
			"code": pk.Code, "version": 1, "lines": len(lines),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.getPackage(ctx, s.d.DB.DB, scope, pk.ID, 0)
}

// SaveVersion saves the lines as version N+1 (yard.manage, If-Match on the package; insert-only).
func (s *Service) SaveVersion(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in VersionInput) (*PackageView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pk, err := store.GetWorkPackage(ctx, tx, scope, id, true)
		if err != nil {
			return notFound(err)
		}
		if pk.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		if in.Name != nil {
			pk.Name = i18nJSON(checkI18n(v, "name", *in.Name, 100))
		}
		if in.WorkTypeID != nil {
			if _, err := store.GetWorkType(ctx, tx, scope, *in.WorkTypeID); err != nil {
				v.Add("work_type_id", "not_found")
			}
			pk.WorkTypeID = *in.WorkTypeID
		}
		if err := v.Err(); err != nil {
			return err
		}
		lines, jobs, err := s.checkPackageLines(ctx, tx, scope, in.Lines, in.JobVars)
		if err != nil {
			return err
		}
		n := pk.CurrentVersion + 1
		if err := s.insertVersion(ctx, tx, scope, p, pk, n, lines, jobs); err != nil {
			return writeErr(err)
		}
		pk.CurrentVersion = n
		if err := store.UpdateWorkPackage(ctx, tx, scope, pk, ifMatch, s.now()); err != nil {
			return writeErr(err)
		}
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "work_package.version", "package", pk.ID, map[string]any{
			"code": pk.Code, "version": n, "lines": len(lines),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.getPackage(ctx, s.d.DB.DB, scope, id, 0)
}

// SetPackageActive activates or deactivates a package (yard.manage, If-Match); versions stay.
func (s *Service) SetPackageActive(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, active bool) (*PackageView, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		pk, err := store.GetWorkPackage(ctx, tx, scope, id, true)
		if err != nil {
			return notFound(err)
		}
		if pk.Version != ifMatch {
			return ErrVersionMismatch
		}
		pk.Active = active
		if err := store.UpdateWorkPackage(ctx, tx, scope, pk, ifMatch, s.now()); err != nil {
			return writeErr(err)
		}
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "work_package.active", "package", pk.ID, map[string]any{"active": active})
	})
	if err != nil {
		return nil, err
	}
	return s.getPackage(ctx, s.d.DB.DB, scope, id, 0)
}
