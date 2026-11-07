package catalog

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Revenue classes (spec 12.1): a per-marina table, one optional sub-level, the nine defaults of the spec
// inserted on first use (m2-plan §12 D25).

type defaultClass struct{ code, it, en string }

var defaultClasses = []defaultClass{
	{"mooring_annual", "Ormeggi annuali", "Annual moorings"},
	{"transit", "Transiti", "Transits"},
	{"storage", "Rimessaggio", "Storage"},
	{"services", "Servizi", "Services"},
	{"yard_labour", "Cantiere manodopera", "Yard labour"},
	{"yard_materials", "Cantiere materiali", "Yard materials"},
	{"rental", "Noleggio", "Rental"},
	{"utilities", "Utenze", "Utilities"},
	{"fees", "Oneri", "Fees"},
}

// DefaultRevenueClassCodes returns the codes of the default revenue classes in order.
func DefaultRevenueClassCodes() []string {
	out := make([]string, len(defaultClasses))
	for i, d := range defaultClasses {
		out[i] = d.code
	}
	return out
}

func classOf(r *store.RevenueClass, counts map[string]int) RevenueClass {
	return RevenueClass{
		ID: r.ID, Code: r.Code, Name: unmarshal[I18nText](r.Name), ParentID: strPtr(r.ParentID), SortOrder: r.SortOrder,
		Archived: !r.ArchivedAt.IsZero(), ItemCount: counts[r.ID], Version: r.Version,
	}
}

// ensureDefaultClasses inserts the default classes when the marina has none.
func (s *Service) ensureDefaultClasses(ctx context.Context, scope store.MarinaScope) error {
	n, err := store.CountRevenueClasses(ctx, s.d.DB.DB, scope)
	if err != nil || n > 0 {
		return err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		n, err := store.CountRevenueClasses(ctx, tx, scope)
		if err != nil || n > 0 {
			return err
		}
		now := s.now()
		for i, d := range defaultClasses {
			en := d.en
			r := &store.RevenueClass{
				ID: store.NewID(), Code: d.code, Name: store.MustJSON(I18nText{It: d.it, En: &en}), SortOrder: i,
				Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			if err := store.InsertRevenueClass(ctx, tx, scope, r); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, store.ErrConflict) {
		return nil // inserted concurrently
	}
	return err
}

// ListRevenueClasses returns the revenue classes of a marina (pricing.read), creating the defaults on first use.
func (s *Service) ListRevenueClasses(ctx context.Context, p *auth.Principal, marinaID string, includeArchived bool) ([]RevenueClass, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	if err := s.ensureDefaultClasses(ctx, scope); err != nil {
		return nil, err
	}
	var out []RevenueClass
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		rows, err := store.ListRevenueClasses(ctx, tx, scope, includeArchived)
		if err != nil {
			return err
		}
		counts, err := store.RevenueClassItemCounts(ctx, tx, scope)
		if err != nil {
			return err
		}
		out = make([]RevenueClass, 0, len(rows))
		for i := range rows {
			out = append(out, classOf(&rows[i], counts))
		}
		return nil
	})
	return out, err
}

// RevenueClassInput are the fields of a new revenue class.
type RevenueClassInput struct {
	Code      string
	Name      I18nText
	ParentID  string
	SortOrder *int
}

// checkParent verifies that parentID is a top-level class of the marina other than self.
func checkParent(ctx context.Context, tx bun.IDB, scope store.MarinaScope, parentID, self string) error {
	if parentID == "" {
		return nil
	}
	if parentID == self {
		v := &auth.ValidationError{}
		v.Add("parent_id", "invalid")
		return v
	}
	parent, err := store.GetRevenueClass(ctx, tx, scope, parentID)
	if err != nil {
		return err
	}
	if parent.ParentID != "" {
		v := &auth.ValidationError{}
		v.Add("parent_id", "too_deep") // one level only
		return v
	}
	return nil
}

// CreateRevenueClass creates a revenue class or subclass (pricing.configure). A parent of another marina is 404.
func (s *Service) CreateRevenueClass(ctx context.Context, p *auth.Principal, marinaID string, in RevenueClassInput) (*RevenueClass, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	in.Code = strings.TrimSpace(in.Code)
	c := newChecker()
	c.pattern("code", in.Code, classCodePattern)
	c.i18n("name", in.Name)
	if in.SortOrder != nil {
		c.rangeI("sort_order", int64(*in.SortOrder), 0, 100000)
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	if err := s.ensureDefaultClasses(ctx, scope); err != nil {
		return nil, err
	}
	var out RevenueClass
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if err := checkParent(ctx, tx, scope, in.ParentID, ""); err != nil {
			return err
		}
		order := 0
		if in.SortOrder != nil {
			order = *in.SortOrder
		} else {
			rows, err := store.ListRevenueClasses(ctx, tx, scope, true)
			if err != nil {
				return err
			}
			for _, r := range rows {
				order = max(order, r.SortOrder+1)
			}
		}
		now := s.now()
		r := &store.RevenueClass{
			ID: store.NewID(), Code: in.Code, Name: store.MustJSON(in.Name), ParentID: in.ParentID, SortOrder: order,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertRevenueClass(ctx, tx, scope, r); err != nil {
			return err
		}
		out = classOf(r, nil)
		return s.write(ctx, tx, p, scope, "revenue_class.create", "revenue_class", r.ID, map[string]any{"parent": in.ParentID != ""},
			event.PricingChangedData{RevenueClassID: r.ID, Version: r.Version, Change: event.ChangeRevenueClass})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RevenueClassPatch lists the fields to change (nil = unchanged). ParentID "" makes the class top level.
type RevenueClassPatch struct {
	Code      *string
	Name      *I18nText
	ParentID  *string
	SortOrder *int
	Archived  *bool
}

// UpdateRevenueClass changes a revenue class when its version equals ifMatch (pricing.configure).
func (s *Service) UpdateRevenueClass(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in RevenueClassPatch) (*RevenueClass, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	c := newChecker()
	if in.Code != nil {
		*in.Code = strings.TrimSpace(*in.Code)
		c.pattern("code", *in.Code, classCodePattern)
	}
	if in.Name != nil {
		c.i18n("name", *in.Name)
	}
	if in.SortOrder != nil {
		c.rangeI("sort_order", int64(*in.SortOrder), 0, 100000)
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	var out RevenueClass
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetRevenueClass(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		var fields []string
		if in.Code != nil && *in.Code != r.Code {
			// Published lists keep the code in their item snapshot (INV-26); drafts pick up the new one.
			r.Code = *in.Code
			fields = append(fields, "code")
		}
		if in.Name != nil {
			r.Name = store.MustJSON(*in.Name)
			fields = append(fields, "name")
		}
		if in.ParentID != nil && *in.ParentID != r.ParentID {
			if err := checkParent(ctx, tx, scope, *in.ParentID, r.ID); err != nil {
				return err
			}
			if *in.ParentID != "" {
				n, err := store.CountRevenueClassChildren(ctx, tx, scope, r.ID)
				if err != nil {
					return err
				}
				if n > 0 {
					v := &auth.ValidationError{}
					v.Add("parent_id", "too_deep")
					return v
				}
			}
			r.ParentID = *in.ParentID
			fields = append(fields, "parent_id")
		}
		if in.SortOrder != nil {
			r.SortOrder = *in.SortOrder
			fields = append(fields, "sort_order")
		}
		action := "revenue_class.update"
		if in.Archived != nil && *in.Archived != !r.ArchivedAt.IsZero() {
			if *in.Archived {
				r.ArchivedAt = s.now()
				action = "revenue_class.archive"
			} else {
				r.ArchivedAt = time.Time{}
			}
			fields = append(fields, "archived")
		}
		r.UpdatedAt = s.now()
		if err := store.UpdateRevenueClass(ctx, tx, scope, r, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		counts, err := store.RevenueClassItemCounts(ctx, tx, scope)
		if err != nil {
			return err
		}
		out = classOf(r, counts)
		return s.write(ctx, tx, p, scope, action, "revenue_class", r.ID, map[string]any{"fields": fields, "version": r.Version},
			event.PricingChangedData{RevenueClassID: r.ID, Version: r.Version, Change: event.ChangeRevenueClass})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRevenueClass deletes a class used by no item and no subclass (pricing.configure); otherwise in_use.
func (s *Service) DeleteRevenueClass(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetRevenueClass(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		counts, err := store.RevenueClassItemCounts(ctx, tx, scope)
		if err != nil {
			return err
		}
		children, err := store.CountRevenueClassChildren(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if refs := counts[id] + children; refs > 0 {
			return &InUseError{References: refs}
		}
		if err := store.DeleteRevenueClass(ctx, tx, scope, id, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		return s.write(ctx, tx, p, scope, "revenue_class.delete", "revenue_class", r.ID, nil,
			event.PricingChangedData{RevenueClassID: r.ID, Change: event.ChangeRevenueClass})
	})
}

// RevenueClassCodes returns revenue class id → code of a marina (archived included), for the engine items.
func RevenueClassCodes(ctx context.Context, db bun.IDB, scope store.MarinaScope) (map[string]string, error) {
	rows, err := store.ListRevenueClasses(ctx, db, scope, true)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Code
	}
	return out, nil
}
