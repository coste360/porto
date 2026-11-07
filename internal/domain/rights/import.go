package rights

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// CSV import of berth rights with holders (spec 11.6; m4-plan §3.7). One row per holder: the rows of a right
// share berth_code and from_on. Customers are found by fiscal code or VAT number (holder_tax_id). The owner
// occupancy goes through the normal engine: a taken night is a rejected row, never an overlap. Undo deletes the
// rights while they are untouched (version 1, no mandates, transfers, fees or allocations).

// Import fields of the rights entity.
const (
	FieldBerthCode   = "berth_code"
	FieldKind        = "kind"
	FieldFromOn      = "from_on"
	FieldToOn        = "to_on"
	FieldHolderTaxID = "holder_tax_id"
	FieldShareMillis = "share_millis"
	FieldTitleDeed   = "title_deed"
	FieldTitleNumber = "title_number"
	FieldTitleNotary = "title_notary"
	FieldTitleDate   = "title_date"
)

// ImportHandler returns the CSV import handler of the rights entity (registered by internal/app).
func (s *Service) ImportHandler() csvimport.EntityHandler { return rightsImport{s} }

type rightsImport struct{ s *Service }

func (rightsImport) Entity() string { return csvimport.EntityRights }

func (rightsImport) Fields() []string {
	return []string{
		FieldBerthCode, FieldKind, FieldFromOn, FieldToOn, FieldHolderTaxID, FieldShareMillis, FieldTitleDeed,
		FieldTitleNumber, FieldTitleNotary, FieldTitleDate,
	}
}

// importGroup is one right of the file.
type importGroup struct {
	key   string
	rows  []int // file lines
	input RightInput
}

// parse validates the rows and groups them into rights; rejected rows go to rep.
func (ri rightsImport) parse(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) ([]*importGroup, error) {
	codes, err := store.RightsBerthCodes(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	var taxIDs []string
	for _, r := range rows {
		if t := strings.ToUpper(r.Get(FieldHolderTaxID)); t != "" {
			taxIDs = append(taxIDs, t)
		}
	}
	custs, err := store.CustomersByTaxIDs(ctx, db, scope, taxIDs)
	if err != nil {
		return nil, err
	}
	defEnd, err := defaultEnd(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	groups := map[string]*importGroup{}
	var order []*importGroup
	bad := map[string]bool{}
	for _, r := range rows {
		code := r.Get(FieldBerthCode)
		b, ok := codes[code]
		if code == "" || !ok {
			rep.Reject(r.Line, "unknown_berth", FieldBerthCode, code)
			bad[code+"|"+r.Get(FieldFromOn)] = true
			continue
		}
		key := code + "|" + r.Get(FieldFromOn)
		from, err := calendar.Parse(r.Get(FieldFromOn))
		if err != nil {
			rep.Reject(r.Line, "invalid", FieldFromOn, code)
			bad[key] = true
			continue
		}
		to := defEnd
		if v := r.Get(FieldToOn); v != "" {
			if to, err = calendar.Parse(v); err != nil {
				rep.Reject(r.Line, "invalid", FieldToOn, code)
				bad[key] = true
				continue
			}
		}
		kind := r.Get(FieldKind)
		if kind == "" {
			kind = KindOwnershipShare
		}
		if !slices.Contains(Kinds, kind) {
			rep.Reject(r.Line, "invalid", FieldKind, code)
			bad[key] = true
			continue
		}
		tax := strings.ToUpper(r.Get(FieldHolderTaxID))
		c, ok := custs[tax]
		if !ok {
			rep.Reject(r.Line, "unknown_customer", FieldHolderTaxID, code)
			bad[key] = true
			continue
		}
		share := int64(allot.Whole)
		if v := r.Get(FieldShareMillis); v != "" {
			if share, err = strconv.ParseInt(v, 10, 64); err != nil || share < 1 || share > allot.Whole {
				rep.Reject(r.Line, "invalid", FieldShareMillis, code)
				bad[key] = true
				continue
			}
		}
		g := groups[key]
		if g == nil {
			g = &importGroup{key: key, input: RightInput{BerthID: b.ID, Kind: kind, FromOn: from, ToOn: to, Title: Title{
				Deed: r.Get(FieldTitleDeed), Number: r.Get(FieldTitleNumber), Notary: r.Get(FieldTitleNotary),
			}}}
			if d := r.Get(FieldTitleDate); d != "" {
				if g.input.Title.Date, err = calendar.Parse(d); err != nil {
					rep.Reject(r.Line, "invalid", FieldTitleDate, code)
					bad[key] = true
					continue
				}
			}
			groups[key] = g
			order = append(order, g)
		}
		g.rows = append(g.rows, r.Line)
		g.input.Holders = append(g.input.Holders, allot.Share{CustomerID: c.ID, Millis: share})
	}
	var out []*importGroup
	for _, g := range order {
		if bad[g.key] {
			for _, l := range g.rows {
				rep.Reject(l, "right_incomplete", "", strings.SplitN(g.key, "|", 2)[0])
			}
			continue
		}
		if _, err := allot.CheckShares(g.input.Holders); err != nil {
			for _, l := range g.rows {
				rep.Reject(l, "shares_invalid", FieldShareMillis, strings.SplitN(g.key, "|", 2)[0])
			}
			continue
		}
		if g.input.ToOn.IsZero() || g.input.ToOn.Before(g.input.FromOn) {
			rep.Reject(g.rows[0], "invalid", FieldToOn, strings.SplitN(g.key, "|", 2)[0])
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (ri rightsImport) DryRun(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) error {
	groups, err := ri.parse(ctx, db, scope, rows, rep)
	if err != nil {
		return err
	}
	for _, g := range groups {
		rep.Valid += len(g.rows)
	}
	return nil
}

func (ri rightsImport) Apply(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rows []csvimport.Row, rep *csvimport.Report) ([]csvimport.Created, error) {
	groups, err := ri.parse(ctx, tx, scope, rows, rep)
	if err != nil {
		return nil, err
	}
	var out []csvimport.Created
	for _, g := range groups {
		r, err := ri.s.createRightTx(ctx, tx, scope, nil, g.input, "")
		if err != nil {
			code := "invalid"
			var ce *occupancy.ConflictError
			var se *SumError
			switch {
			case errors.As(err, &ce):
				code = "resource_taken"
			case errors.As(err, &se):
				code = "shares_invalid"
			case errors.As(err, new(*occupancy.FitError)):
				code = "fit_blocked"
			case isValidation(err):
			default:
				return nil, err
			}
			for _, l := range g.rows {
				rep.Reject(l, code, "", strings.SplitN(g.key, "|", 2)[0])
			}
			continue
		}
		rep.Valid += len(g.rows)
		out = append(out, csvimport.Created{EntityType: "berth_right", EntityID: r.ID})
	}
	if rep.Created == nil {
		rep.Created = map[string]int{}
	}
	rep.Created["berth_right"] += len(out)
	return out, nil
}

func (ri rightsImport) Undo(ctx context.Context, tx bun.IDB, scope store.MarinaScope, created []csvimport.Created) error {
	for _, c := range created {
		if c.EntityType != "berth_right" {
			continue
		}
		r, err := store.LockBerthRight(ctx, tx, scope, c.EntityID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		n, err := store.RightDependents(ctx, tx, scope, r.ID)
		if err != nil {
			return err
		}
		if r.Version != 1 || r.Status != RightActive || n > 0 {
			return csvimport.ErrNotUndoable
		}
		allocs, err := store.AllocationsOfRights(ctx, tx, scope, []string{r.ID}, true)
		if err != nil {
			return err
		}
		for _, a := range allocs[r.ID] {
			if err := ri.s.d.Occupancy.CancelTx(ctx, tx, scope, a.ID, occupancy.StatusCancelled); err != nil {
				return err
			}
		}
		if err := store.DeleteBerthRight(ctx, tx, scope, r.ID); err != nil {
			return err
		}
		if err := ri.s.write(ctx, tx, actorOf(nil), scope, "right.cancel", "right", r.ID, map[string]any{"import_undo": true},
			changed("right", r.ID, "deleted")); err != nil {
			return err
		}
	}
	return nil
}

func isValidation(err error) bool { return errors.Is(err, auth.ErrInvalidInput) }
