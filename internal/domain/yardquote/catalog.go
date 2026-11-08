package yardquote

import (
	"context"
	"errors"
	"regexp"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Work types, qualifications and item yard settings (m10-plan §3.1). Prices stay in the M2 price lists.

var (
	workTypeCode      = regexp.MustCompile(`^[A-Z0-9_-]{1,16}$`)
	qualificationCode = regexp.MustCompile(`^[a-z0-9_-]{1,16}$`)
	packageCode       = workTypeCode
)

// yardItemTypes are the item types usable in yard quotes.
var yardItemTypes = []string{"service", "labour", "material", "product", "fee"}

// ---- work types ----

// ListWorkTypes lists the work types (yard.read).
func (s *Service) ListWorkTypes(ctx context.Context, p *auth.Principal, marinaID string, active *bool) ([]store.WorkType, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return store.ListWorkTypes(ctx, s.d.DB.DB, scope, active)
}

func (s *Service) checkRevenueClass(ctx context.Context, db bun.IDB, scope store.MarinaScope, v *auth.ValidationError, id string) {
	if id == "" {
		return
	}
	if _, err := store.GetRevenueClass(ctx, db, scope, id); err != nil {
		v.Add("revenue_class_id", "not_found")
	}
}

// CreateWorkType creates a work type (yard.manage).
func (s *Service) CreateWorkType(ctx context.Context, p *auth.Principal, marinaID string, in WorkTypeInput) (*store.WorkType, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !workTypeCode.MatchString(in.Code) {
		v.Add("code", "invalid")
	}
	name := checkI18n(v, "name", in.Name, 100)
	s.checkRevenueClass(ctx, s.d.DB.DB, scope, v, in.RevenueClassID)
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	row := &store.WorkType{
		ID: store.NewID(), Code: in.Code, Name: i18nJSON(name), RevenueClassID: in.RevenueClassID, Active: in.Active == nil || *in.Active,
		CreatedAt: now, UpdatedAt: now,
	}
	if in.SortOrder != nil {
		row.SortOrder = *in.SortOrder
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertWorkType(ctx, tx, scope, row); err != nil {
			return writeErr(err)
		}
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "work_type.create", "work_type", row.ID, map[string]any{"code": row.Code})
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

// UpdateWorkType changes a work type (yard.manage, If-Match).
func (s *Service) UpdateWorkType(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in WorkTypePatch) (*store.WorkType, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var row *store.WorkType
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetWorkType(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		if in.Code != nil {
			if !workTypeCode.MatchString(*in.Code) {
				v.Add("code", "invalid")
			}
			r.Code = *in.Code
		}
		if in.Name != nil {
			r.Name = i18nJSON(checkI18n(v, "name", *in.Name, 100))
		}
		if in.RevenueClassID != nil {
			s.checkRevenueClass(ctx, tx, scope, v, *in.RevenueClassID)
			r.RevenueClassID = *in.RevenueClassID
		}
		if in.Active != nil {
			r.Active = *in.Active
		}
		if in.SortOrder != nil {
			r.SortOrder = *in.SortOrder
		}
		if err := v.Err(); err != nil {
			return err
		}
		if err := store.UpdateWorkType(ctx, tx, scope, r, ifMatch, s.now()); err != nil {
			return writeErr(err)
		}
		row = r
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "work_type.update", "work_type", r.ID, map[string]any{"code": r.Code, "active": r.Active})
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

// ---- qualifications ----

// ListQualifications lists the qualifications (yard.read).
func (s *Service) ListQualifications(ctx context.Context, p *auth.Principal, marinaID string, active *bool) ([]store.YardQualification, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return store.ListYardQualifications(ctx, s.d.DB.DB, scope, active)
}

// checkLabourItem verifies that the item is a labour item priced by the hour.
func checkLabourItem(ctx context.Context, db bun.IDB, scope store.MarinaScope, v *auth.ValidationError, id string) {
	it, err := store.GetItem(ctx, db, scope, id)
	if err != nil {
		v.Add("labour_item_id", "not_found")
		return
	}
	if it.ItemType != "labour" || it.Unit != "hour" {
		v.Add("labour_item_id", "not_labour_hour")
	}
}

// linkLabourItem points the yard settings of the labour item to the qualification (one item per qualification).
func (s *Service) linkLabourItem(ctx context.Context, tx bun.IDB, scope store.MarinaScope, q *store.YardQualification) error {
	st, err := store.GetYardItemSettings(ctx, tx, scope, q.LabourItemID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		row := &store.YardItemSettings{ItemID: q.LabourItemID, YardEnabled: true, QualificationID: q.ID}
		return store.SaveYardItemSettings(ctx, tx, scope, row, 0, s.now())
	case err != nil:
		return err
	}
	if st.QualificationID == q.ID {
		return nil
	}
	st.QualificationID = q.ID
	return store.SaveYardItemSettings(ctx, tx, scope, st, st.Version, s.now())
}

// CreateQualification creates a qualification with its labour item (yard.manage).
func (s *Service) CreateQualification(ctx context.Context, p *auth.Principal, marinaID string, in QualificationInput) (*store.YardQualification, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !qualificationCode.MatchString(in.Code) {
		v.Add("code", "invalid")
	}
	name := checkI18n(v, "name", in.Name, 100)
	checkLabourItem(ctx, s.d.DB.DB, scope, v, in.LabourItemID)
	if in.DefaultCostCents != nil && *in.DefaultCostCents < 0 {
		v.Add("default_cost_cents", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	row := &store.YardQualification{
		ID: store.NewID(), Code: in.Code, Name: i18nJSON(name), LabourItemID: in.LabourItemID,
		LiftOperator: in.LiftOperator != nil && *in.LiftOperator, Active: in.Active == nil || *in.Active, CreatedAt: now, UpdatedAt: now,
	}
	if in.DefaultCostCents != nil {
		row.DefaultCostCents = *in.DefaultCostCents
	}
	if in.SortOrder != nil {
		row.SortOrder = *in.SortOrder
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := store.InsertYardQualification(ctx, tx, scope, row); err != nil {
			return writeErr(err)
		}
		if err := s.linkLabourItem(ctx, tx, scope, row); err != nil {
			return writeErr(err)
		}
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "yard_qualification.create", "qualification", row.ID, map[string]any{
			"code": row.Code, "labour_item_id": row.LabourItemID, "default_cost_cents": row.DefaultCostCents,
		})
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

// UpdateQualification changes a qualification (yard.manage, If-Match).
func (s *Service) UpdateQualification(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in QualificationPatch) (*store.YardQualification, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	var row *store.YardQualification
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetYardQualification(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if r.Version != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		oldItem := r.LabourItemID
		if in.Code != nil {
			if !qualificationCode.MatchString(*in.Code) {
				v.Add("code", "invalid")
			}
			r.Code = *in.Code
		}
		if in.Name != nil {
			r.Name = i18nJSON(checkI18n(v, "name", *in.Name, 100))
		}
		if in.LabourItemID != nil && *in.LabourItemID != r.LabourItemID {
			checkLabourItem(ctx, tx, scope, v, *in.LabourItemID)
			r.LabourItemID = *in.LabourItemID
		}
		if in.DefaultCostCents != nil {
			if *in.DefaultCostCents < 0 {
				v.Add("default_cost_cents", "invalid")
			}
			r.DefaultCostCents = *in.DefaultCostCents
		}
		if in.LiftOperator != nil {
			r.LiftOperator = *in.LiftOperator
		}
		if in.Active != nil {
			r.Active = *in.Active
		}
		if in.SortOrder != nil {
			r.SortOrder = *in.SortOrder
		}
		if err := v.Err(); err != nil {
			return err
		}
		if oldItem != r.LabourItemID {
			if st, err := store.GetYardItemSettings(ctx, tx, scope, oldItem); err == nil && st.QualificationID == r.ID {
				st.QualificationID = ""
				if err := store.SaveYardItemSettings(ctx, tx, scope, st, st.Version, s.now()); err != nil {
					return writeErr(err)
				}
			}
		}
		if err := store.UpdateYardQualification(ctx, tx, scope, r, ifMatch, s.now()); err != nil {
			return writeErr(err)
		}
		if err := s.linkLabourItem(ctx, tx, scope, r); err != nil {
			return writeErr(err)
		}
		row = r
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "yard_qualification.update", "qualification", r.ID, map[string]any{
			"code": r.Code, "labour_item_id": r.LabourItemID, "default_cost_cents": r.DefaultCostCents, "active": r.Active,
		})
	})
	if err != nil {
		return nil, err
	}
	return row, nil
}

// ---- item yard settings ----

func yardItemOf(r store.YardItemRow) YardItem {
	out := YardItem{Item: r.Item, Settings: r.Settings}
	if r.Settings != nil {
		out.TestCase = decodeTestCase(r.Settings.TestCase)
	}
	return out
}

// ListItems lists the catalogue items usable in the yard with their yard settings (yard.read).
func (s *Service) ListItems(ctx context.Context, p *auth.Principal, marinaID string, f ItemFilter, cursor string, limit int) ([]YardItem, string, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, "", err
	}
	page, err := store.ListYardItems(ctx, s.d.DB.DB, scope, store.YardItemFilter{
		Query: f.Query, Category: f.Category, ItemType: f.ItemType, YardEnabled: f.YardEnabled, ItemTypes: yardItemTypes,
	}, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]YardItem, 0, len(page.Items))
	for _, r := range page.Items {
		out = append(out, yardItemOf(r))
	}
	return out, page.NextCursor, nil
}

// GetItem returns an item with its yard settings (yard.read).
func (s *Service) GetItem(ctx context.Context, p *auth.Principal, marinaID, itemID string) (*YardItem, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	return s.getItem(ctx, s.d.DB.DB, scope, itemID)
}

func (s *Service) getItem(ctx context.Context, db bun.IDB, scope store.MarinaScope, itemID string) (*YardItem, error) {
	it, err := store.GetItem(ctx, db, scope, itemID)
	if err != nil {
		return nil, notFound(err)
	}
	r := store.YardItemRow{Item: *it}
	st, err := store.GetYardItemSettings(ctx, db, scope, itemID)
	switch {
	case err == nil:
		r.Settings = st
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}
	out := yardItemOf(r)
	return &out, nil
}

// PutItemSettings sets the yard settings of an item (yard.manage). ifMatch is the settings version (0 when the
// item is not configured yet); the formulas are validated on the test vessel (422 formula_invalid).
func (s *Service) PutItemSettings(ctx context.Context, p *auth.Principal, marinaID, itemID string, ifMatch int, in ItemSettingsInput) (*YardItem, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		it, err := store.GetItem(ctx, tx, scope, itemID)
		if err != nil {
			return notFound(err)
		}
		row, err := store.GetYardItemSettings(ctx, tx, scope, itemID)
		expected := 0
		switch {
		case errors.Is(err, store.ErrNotFound):
			row = &store.YardItemSettings{ItemID: itemID, YardEnabled: true}
		case err != nil:
			return err
		default:
			expected = row.Version
		}
		if expected != ifMatch {
			return ErrVersionMismatch
		}
		v := &auth.ValidationError{}
		if !contains(yardItemTypes, it.ItemType) {
			v.Add("item_id", "not_yard_item")
		}
		if in.YardEnabled != nil {
			row.YardEnabled = *in.YardEnabled
		}
		if in.CostCents != nil {
			if *in.CostCents < 0 {
				v.Add("cost_cents", "invalid")
			}
			row.CostCents = *in.CostCents
		}
		if in.Category != nil {
			c, ok := cleanText(*in.Category, 40)
			if !ok {
				v.Add("category", "invalid")
			}
			row.Category = c
		}
		if in.QtyFormula != nil {
			row.QtyFormula = *in.QtyFormula
		}
		if in.PriceFormula != nil {
			row.PriceFormula = *in.PriceFormula
		}
		if in.ClearTestCase {
			row.TestCase = nil
		} else if in.TestCase != nil {
			row.TestCase = store.MustJSON(in.TestCase)
		}
		if in.QualificationID != nil {
			row.QualificationID = *in.QualificationID
		}
		if it.ItemType == "labour" {
			if row.QualificationID == "" {
				v.Add("qualification_id", "required")
			} else if q, err := store.GetYardQualification(ctx, tx, scope, row.QualificationID); err != nil {
				v.Add("qualification_id", "not_found")
			} else if q.LabourItemID != itemID {
				v.Add("qualification_id", "labour_item_mismatch")
			}
		} else if row.QualificationID != "" {
			v.Add("qualification_id", "not_labour")
		}
		if err := v.Err(); err != nil {
			return err
		}
		ce, err := loadCatalogEnv(ctx, tx, scope)
		if err != nil {
			return err
		}
		val, err := s.newValidator(ctx, tx, scope, ce)
		if err != nil {
			return err
		}
		val.lineFormulas("", row.QtyFormula, row.PriceFormula, decodeTestCase(row.TestCase), centsPtr(s.listPrices(ctx, tx, scope, []string{itemID}), itemID))
		if err := val.err(); err != nil {
			return err
		}
		if err := store.SaveYardItemSettings(ctx, tx, scope, row, expected, s.now()); err != nil {
			return writeErr(err)
		}
		return s.catalogChanged(ctx, tx, scope, p.Actor(), "yard_item_settings.save", "item_settings", itemID, map[string]any{
			"yard_enabled": row.YardEnabled, "cost_cents": row.CostCents, "qualification_id": row.QualificationID,
			"qty_formula": row.QtyFormula, "price_formula": row.PriceFormula,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.getItem(ctx, s.d.DB.DB, scope, itemID)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
