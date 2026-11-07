package catalog

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/store"
)

// Berth categories (spec 07 price_class, 12.2; m2-plan §12 D16): the registry of the codes that berths, dynamic
// areas and land resources carry in price_class. Elements keep free text (no FK); codes in use without a category
// are reported and a sync creates them.

func categoryOf(r *store.BerthCategory, counts map[string]map[string]int, rules int) BerthCategory {
	applies := []string(r.AppliesTo)
	if applies == nil {
		applies = []string{}
	}
	return BerthCategory{
		ID: r.ID, Code: r.Code, Name: unmarshal[I18nText](r.Name), AppliesTo: applies, SortOrder: r.SortOrder,
		Archived: !r.ArchivedAt.IsZero(), Version: r.Version,
		Usage: CategoryUsage{
			Berths: counts["berth"][r.Code], DynamicAreas: counts["dynamic_area"][r.Code], LandResources: counts["land_resource"][r.Code],
			Rules: rules,
		},
	}
}

func (s *Service) categoryView(ctx context.Context, tx bun.IDB, scope store.MarinaScope, r *store.BerthCategory) (BerthCategory, error) {
	counts, err := store.PriceClassCounts(ctx, tx, scope)
	if err != nil {
		return BerthCategory{}, err
	}
	rules, err := store.CategoryRuleRefs(ctx, tx, scope, r.Code)
	if err != nil {
		return BerthCategory{}, err
	}
	return categoryOf(r, counts, rules), nil
}

// ListCategories returns the berth categories with usage and the price_class codes in use without a category
// (pricing.read).
func (s *Service) ListCategories(ctx context.Context, p *auth.Principal, marinaID string, includeArchived bool) (*CategoryList, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	out := &CategoryList{Items: []BerthCategory{}, UnknownInUse: []CodeCount{}}
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		rows, err := store.ListBerthCategories(ctx, tx, scope, true)
		if err != nil {
			return err
		}
		counts, err := store.PriceClassCounts(ctx, tx, scope)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for i := range rows {
			known[rows[i].Code] = true
			if !includeArchived && !rows[i].ArchivedAt.IsZero() {
				continue
			}
			rules, err := store.CategoryRuleRefs(ctx, tx, scope, rows[i].Code)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, categoryOf(&rows[i], counts, rules))
		}
		out.UnknownInUse = unknownCodes(counts, known)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// unknownCodes sums the element counts of the codes without a category, sorted by code.
func unknownCodes(counts map[string]map[string]int, known map[string]bool) []CodeCount {
	sum := map[string]int{}
	for _, m := range counts {
		for code, n := range m {
			if !known[code] {
				sum[code] += n
			}
		}
	}
	out := make([]CodeCount, 0, len(sum))
	for code, n := range sum {
		out = append(out, CodeCount{Code: code, Count: n})
	}
	slices.SortFunc(out, func(a, b CodeCount) int { return strings.Compare(a.Code, b.Code) })
	return out
}

// CategoryInput are the fields of a new berth category.
type CategoryInput struct {
	Code      string
	Name      I18nText
	AppliesTo []string
	SortOrder *int
}

func checkAppliesTo(c checker, applies []string) {
	if len(applies) == 0 || len(applies) > 3 {
		c.add("applies_to", "invalid")
		return
	}
	seen := map[string]bool{}
	for _, a := range applies {
		if seen[a] || !slices.Contains(categoryTarget, a) {
			c.add("applies_to", "invalid")
			return
		}
		seen[a] = true
	}
}

func sortedTargets(applies []string) []string {
	out := slices.Clone(applies)
	slices.SortFunc(out, func(a, b string) int { return slices.Index(categoryTarget, a) - slices.Index(categoryTarget, b) })
	return out
}

// CreateCategory creates a berth category (pricing.configure).
func (s *Service) CreateCategory(ctx context.Context, p *auth.Principal, marinaID string, in CategoryInput) (*BerthCategory, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	in.Code = strings.TrimSpace(in.Code)
	c := newChecker()
	c.pattern("code", in.Code, catCodePattern)
	c.i18n("name", in.Name)
	checkAppliesTo(c, in.AppliesTo)
	if in.SortOrder != nil {
		c.rangeI("sort_order", int64(*in.SortOrder), 0, 100000)
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	var out BerthCategory
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := s.insertCategory(ctx, tx, scope, in)
		if err != nil {
			return err
		}
		if out, err = s.categoryView(ctx, tx, scope, r); err != nil {
			return err
		}
		return s.write(ctx, tx, p, scope, "berth_category.create", "berth_category", r.ID, map[string]any{"applies_to": r.AppliesTo},
			event.PricingChangedData{CategoryID: r.ID, Version: r.Version, Change: event.ChangeCategory})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) insertCategory(ctx context.Context, tx bun.IDB, scope store.MarinaScope, in CategoryInput) (*store.BerthCategory, error) {
	order := 0
	if in.SortOrder != nil {
		order = *in.SortOrder
	} else {
		rows, err := store.ListBerthCategories(ctx, tx, scope, true)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			order = max(order, r.SortOrder+1)
		}
	}
	now := s.now()
	r := &store.BerthCategory{
		ID: store.NewID(), Code: in.Code, Name: store.MustJSON(in.Name), AppliesTo: store.Strings(sortedTargets(in.AppliesTo)),
		SortOrder: order, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertBerthCategory(ctx, tx, scope, r); err != nil {
		return nil, err
	}
	return r, nil
}

// CategoryPatch lists the fields to change (nil = unchanged); the code never changes.
type CategoryPatch struct {
	Name      *I18nText
	AppliesTo *[]string
	SortOrder *int
	Archived  *bool
}

// UpdateCategory changes a berth category when its version equals ifMatch (pricing.configure).
func (s *Service) UpdateCategory(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in CategoryPatch) (*BerthCategory, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	c := newChecker()
	if in.Name != nil {
		c.i18n("name", *in.Name)
	}
	if in.AppliesTo != nil {
		checkAppliesTo(c, *in.AppliesTo)
	}
	if in.SortOrder != nil {
		c.rangeI("sort_order", int64(*in.SortOrder), 0, 100000)
	}
	if err := c.err(); err != nil {
		return nil, err
	}
	var out BerthCategory
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetBerthCategory(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		var fields []string
		if in.Name != nil {
			r.Name = store.MustJSON(*in.Name)
			fields = append(fields, "name")
		}
		if in.AppliesTo != nil {
			r.AppliesTo = store.Strings(sortedTargets(*in.AppliesTo))
			fields = append(fields, "applies_to")
		}
		if in.SortOrder != nil {
			r.SortOrder = *in.SortOrder
			fields = append(fields, "sort_order")
		}
		if in.Archived != nil && *in.Archived != !r.ArchivedAt.IsZero() {
			if *in.Archived {
				r.ArchivedAt = s.now()
			} else {
				r.ArchivedAt = time.Time{}
			}
			fields = append(fields, "archived")
		}
		r.UpdatedAt = s.now()
		if err := store.UpdateBerthCategory(ctx, tx, scope, r, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		if out, err = s.categoryView(ctx, tx, scope, r); err != nil {
			return err
		}
		return s.write(ctx, tx, p, scope, "berth_category.update", "berth_category", r.ID, map[string]any{"fields": fields, "version": r.Version},
			event.PricingChangedData{CategoryID: r.ID, Version: r.Version, Change: event.ChangeCategory})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCategory deletes a category that no element and no rule uses (pricing.configure); otherwise in_use.
func (s *Service) DeleteCategory(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetBerthCategory(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		v, err := s.categoryView(ctx, tx, scope, r)
		if err != nil {
			return err
		}
		if refs := v.Usage.Berths + v.Usage.DynamicAreas + v.Usage.LandResources + v.Usage.Rules; refs > 0 {
			return &InUseError{References: refs}
		}
		if err := store.DeleteBerthCategory(ctx, tx, scope, id, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		return s.write(ctx, tx, p, scope, "berth_category.delete", "berth_category", id, nil,
			event.PricingChangedData{CategoryID: id, Change: event.ChangeCategory})
	})
}

// SyncCategories creates the categories missing for the classes of the harbour price-class rule (applies to
// berths and dynamic areas) and for the codes in use by elements (applies to the element types using them)
// (pricing.configure). Names default to the code.
func (s *Service) SyncCategories(ctx context.Context, p *auth.Principal, marinaID string) ([]BerthCategory, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	out := []BerthCategory{}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		rule := layout.DefaultPriceClassRule()
		if hs, err := store.GetHarborSettings(ctx, tx, scope); err == nil {
			if r := unmarshal[layout.PriceClassRule](hs.PriceClassRule); len(r.Bands) > 0 || r.Fallback != "" {
				rule = r
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		rows, err := store.ListBerthCategories(ctx, tx, scope, true)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, r := range rows {
			known[r.Code] = true
		}
		var codes []string
		applies := map[string][]string{}
		add := func(code, target string) {
			code = strings.TrimSpace(code)
			if code == "" || known[code] || !catCodePattern.MatchString(code) {
				return
			}
			if _, ok := applies[code]; !ok {
				codes = append(codes, code)
			}
			if !slices.Contains(applies[code], target) {
				applies[code] = append(applies[code], target)
			}
		}
		for _, b := range rule.Bands {
			add(b.Class, "berth")
			add(b.Class, "dynamic_area")
		}
		if rule.Fallback != "" {
			add(rule.Fallback, "berth")
			add(rule.Fallback, "dynamic_area")
		}
		counts, err := store.PriceClassCounts(ctx, tx, scope)
		if err != nil {
			return err
		}
		var inUse []string
		for _, kind := range categoryTarget {
			for code := range counts[kind] {
				inUse = append(inUse, kind+"\x00"+code)
			}
		}
		slices.Sort(inUse)
		for _, k := range inUse {
			kind, code, _ := strings.Cut(k, "\x00")
			add(code, kind)
		}
		for _, code := range codes {
			r, err := s.insertCategory(ctx, tx, scope, CategoryInput{Code: code, Name: I18nText{It: code}, AppliesTo: applies[code]})
			if err != nil {
				return err
			}
			out = append(out, categoryOf(r, counts, 0))
		}
		return s.write(ctx, tx, p, scope, "berth_category.sync", "berth_category", scope.ID(), map[string]any{"created": len(out)},
			event.PricingChangedData{Change: event.ChangeCategory})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CategoryCodes returns the codes of the non-archived berth categories of a marina.
func CategoryCodes(ctx context.Context, db bun.IDB, scope store.MarinaScope) ([]string, error) {
	rows, err := store.ListBerthCategories(ctx, db, scope, false)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Code
	}
	return out, nil
}

// customerCategories is the fixed customer category enum of spec 11.1, in display order.
var customerCategories = []string{"member", "resident", "affiliated", "charter", "yard"}

// CustomerCategoryCounts is the number of non-archived customers per category (every category of the enum,
// in enum order) and without one. Counts only: no customer data leaves the registry.
type CustomerCategoryCounts struct {
	Items         []CodeCount `json:"items"`
	Uncategorised int         `json:"uncategorised"`
}

// CustomerCategoryCounts returns the customer categories with their counts (pricing.read), for the read-only
// customer categories tab of the classifications page (m2-plan §7.2).
func (s *Service) CustomerCategoryCounts(ctx context.Context, p *auth.Principal, marinaID string) (*CustomerCategoryCounts, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	counts, err := store.CustomerCategoryCounts(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	out := &CustomerCategoryCounts{Items: make([]CodeCount, 0, len(customerCategories))}
	known := map[string]bool{}
	for _, c := range customerCategories {
		out.Items = append(out.Items, CodeCount{Code: c, Count: counts[c]})
		known[c] = true
	}
	for c, n := range counts {
		if !known[c] {
			out.Uncategorised += n // "" and any value outside the enum
		}
	}
	return out, nil
}
