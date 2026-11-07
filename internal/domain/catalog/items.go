package catalog

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Items and services (spec 12.1).

func itemOf(r *store.Item) Item {
	return Item{
		ID: r.ID, Code: r.Code, Name: unmarshal[I18nText](r.Name), Description: unmarshal[I18nTextOptional](r.Description),
		ItemType: r.ItemType, Unit: r.Unit, VATRateBP: r.VATRateBP, VATNature: strPtr(r.VATNature), PriceIncludesVAT: r.PriceIncludesVAT,
		RevenueClassID: r.RevenueClassID, ExternalCode: r.ExternalCode, Active: r.Active, OnlineVisible: r.OnlineVisible,
		SortOrder: r.SortOrder, Version: r.Version, UpdatedAt: r.UpdatedAt,
	}
}

// ItemFilter narrows ListItems.
type ItemFilter struct {
	Type   string
	Active *bool
	Q      string
}

// ListItems returns a page of items by sort order and code (pricing.read).
func (s *Service) ListItems(ctx context.Context, p *auth.Principal, marinaID string, f ItemFilter, cursor string, limit int) (store.Page[Item], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return store.Page[Item]{}, err
	}
	if f.Type != "" && !slices.Contains(itemTypes(), f.Type) {
		v := &auth.ValidationError{}
		v.Add("type", "invalid")
		return store.Page[Item]{}, v
	}
	page, err := store.ListItems(ctx, s.d.DB.DB, scope, store.ItemFilter{Type: f.Type, Active: f.Active, Q: strings.TrimSpace(f.Q)}, cursor, limit)
	if err != nil {
		return store.Page[Item]{}, err
	}
	out := store.Page[Item]{Items: make([]Item, 0, len(page.Items)), NextCursor: page.NextCursor}
	for i := range page.Items {
		out.Items = append(out.Items, itemOf(&page.Items[i]))
	}
	return out, nil
}

// GetItem returns an item with its usage (pricing.read).
func (s *Service) GetItem(ctx context.Context, p *auth.Principal, marinaID, id string) (*Item, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingRead)
	if err != nil {
		return nil, err
	}
	var out Item
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetItem(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		refs, err := store.ItemPriceListRefs(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		out = itemOf(r)
		out.Usage = &ItemUsage{PriceLists: len(refs)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ItemInput are the fields of a new item; nil pointers take the defaults (VAT from the pricing settings).
type ItemInput struct {
	Code             string
	Name             I18nText
	Description      I18nTextOptional
	ItemType         string
	Unit             string
	VATRateBP        *int64
	VATNature        string
	PriceIncludesVAT *bool
	RevenueClassID   string
	ExternalCode     string
	Active           *bool
	OnlineVisible    bool
	SortOrder        *int
}

func checkItem(c checker, r *store.Item) {
	c.pattern("code", r.Code, itemCodePattern)
	name := unmarshal[I18nText](r.Name)
	c.i18n("name", name)
	desc := unmarshal[I18nTextOptional](r.Description)
	c.text("description.it", desc.It, 2000, false)
	c.text("description.en", desc.En, 2000, false)
	c.enum("item_type", r.ItemType, itemTypes())
	c.enum("unit", r.Unit, itemUnits)
	c.rangeI("vat_rate_bp", r.VATRateBP, 0, 10000)
	switch {
	case r.VATRateBP == 0 && r.VATNature == "":
		c.add("vat_nature", "required")
	case r.VATRateBP == 0:
		c.enum("vat_nature", r.VATNature, VATNatures)
	case r.VATNature != "":
		c.add("vat_nature", "not_allowed")
	}
	c.text("external_code", r.ExternalCode, 64, false)
	c.rangeI("sort_order", int64(r.SortOrder), 0, 100000)
	if !store.ValidID(r.RevenueClassID) {
		c.add("revenue_class_id", "invalid")
	}
}

// CreateItem creates an item (pricing.configure). A revenue class of another marina is 404.
func (s *Service) CreateItem(ctx context.Context, p *auth.Principal, marinaID string, in ItemInput) (*Item, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	var out Item
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		set, err := s.readSettings(ctx, tx, scope)
		if err != nil {
			return err
		}
		now := s.now()
		r := &store.Item{
			ID: store.NewID(), Code: strings.ToUpper(strings.TrimSpace(in.Code)), Name: store.MustJSON(in.Name),
			Description: store.MustJSON(in.Description), ItemType: in.ItemType, Unit: in.Unit, VATRateBP: set.DefaultVATRateBP,
			VATNature: in.VATNature, PriceIncludesVAT: set.DefaultPriceIncludesVAT, RevenueClassID: in.RevenueClassID,
			ExternalCode: strings.TrimSpace(in.ExternalCode), Active: true, OnlineVisible: in.OnlineVisible, Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if in.VATRateBP != nil {
			r.VATRateBP = *in.VATRateBP
		}
		if in.PriceIncludesVAT != nil {
			r.PriceIncludesVAT = *in.PriceIncludesVAT
		}
		if in.Active != nil {
			r.Active = *in.Active
		}
		if in.SortOrder != nil {
			r.SortOrder = *in.SortOrder
		}
		c := newChecker()
		checkItem(c, r)
		if err := c.err(); err != nil {
			return err
		}
		if _, err := store.GetRevenueClass(ctx, tx, scope, r.RevenueClassID); err != nil {
			return err
		}
		if err := store.InsertItem(ctx, tx, scope, r); err != nil {
			return err
		}
		out = itemOf(r)
		out.Usage = &ItemUsage{}
		return s.write(ctx, tx, p, scope, "item.create", "item", r.ID, map[string]any{"item_type": r.ItemType},
			event.PricingChangedData{ItemID: r.ID, Version: r.Version, Change: event.ChangeItem})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ItemPatch lists the fields to change (nil = unchanged). With a non-zero vat_rate_bp and no vat_nature, the
// nature is cleared.
type ItemPatch struct {
	Code             *string
	Name             *I18nText
	Description      *I18nTextOptional
	ItemType         *string
	Unit             *string
	VATRateBP        *int64
	VATNature        *string
	PriceIncludesVAT *bool
	RevenueClassID   *string
	ExternalCode     *string
	Active           *bool
	OnlineVisible    *bool
	SortOrder        *int
}

// UpdateItem changes an item when its version equals ifMatch (pricing.configure). Every field stays editable:
// draft lists price with the live item, while published (active or archived) lists price with the snapshot of
// its engine fields taken at activation, so their prices and content hash do not change (INV-26).
func (s *Service) UpdateItem(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in ItemPatch) (*Item, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	var out Item
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.GetItem(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		var fields []string
		set := func(name string, apply func()) {
			apply()
			fields = append(fields, name)
		}
		if in.Code != nil {
			code := strings.ToUpper(strings.TrimSpace(*in.Code))
			if code != r.Code {
				set("code", func() { r.Code = code })
			}
		}
		if in.Name != nil {
			set("name", func() { r.Name = store.MustJSON(*in.Name) })
		}
		if in.Description != nil {
			set("description", func() { r.Description = store.MustJSON(*in.Description) })
		}
		if in.ItemType != nil {
			set("item_type", func() { r.ItemType = *in.ItemType })
		}
		if in.Unit != nil {
			set("unit", func() { r.Unit = *in.Unit })
		}
		if in.VATRateBP != nil {
			set("vat_rate_bp", func() { r.VATRateBP = *in.VATRateBP })
			if *in.VATRateBP != 0 && in.VATNature == nil {
				r.VATNature = ""
			}
		}
		if in.VATNature != nil {
			set("vat_nature", func() { r.VATNature = *in.VATNature })
		}
		if in.PriceIncludesVAT != nil {
			set("price_includes_vat", func() { r.PriceIncludesVAT = *in.PriceIncludesVAT })
		}
		if in.RevenueClassID != nil {
			set("revenue_class_id", func() { r.RevenueClassID = *in.RevenueClassID })
		}
		if in.ExternalCode != nil {
			set("external_code", func() { r.ExternalCode = strings.TrimSpace(*in.ExternalCode) })
		}
		if in.Active != nil {
			set("active", func() { r.Active = *in.Active })
		}
		if in.OnlineVisible != nil {
			set("online_visible", func() { r.OnlineVisible = *in.OnlineVisible })
		}
		if in.SortOrder != nil {
			set("sort_order", func() { r.SortOrder = *in.SortOrder })
		}
		c := newChecker()
		checkItem(c, r)
		if err := c.err(); err != nil {
			return err
		}
		if in.RevenueClassID != nil {
			if _, err := store.GetRevenueClass(ctx, tx, scope, r.RevenueClassID); err != nil {
				return err
			}
		}
		r.UpdatedAt = s.now()
		if err := store.UpdateItem(ctx, tx, scope, r, ifMatch); err != nil {
			if errors.Is(err, store.ErrVersionMismatch) {
				return ErrVersionMismatch
			}
			return err
		}
		refs, err := store.ItemPriceListRefs(ctx, tx, scope, r.ID)
		if err != nil {
			return err
		}
		out = itemOf(r)
		out.Usage = &ItemUsage{PriceLists: len(refs)}
		return s.write(ctx, tx, p, scope, "item.update", "item", r.ID, map[string]any{"fields": fields, "version": r.Version},
			event.PricingChangedData{ItemID: r.ID, Version: r.Version, Change: event.ChangeItem})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteItem deletes an item no price list references (pricing.configure); otherwise in_use (deactivate it).
func (s *Service) DeleteItem(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) error {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := store.GetItem(ctx, tx, scope, id); err != nil {
			return err
		}
		refs, err := store.ItemPriceListRefs(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if len(refs) > 0 {
			return &InUseError{References: len(refs)}
		}
		if err := store.DeleteItem(ctx, tx, scope, id, ifMatch); err != nil {
			switch {
			case errors.Is(err, store.ErrVersionMismatch):
				return ErrVersionMismatch
			case errors.Is(err, store.ErrConflict):
				return &InUseError{References: 1}
			}
			return err
		}
		return s.write(ctx, tx, p, scope, "item.delete", "item", id, nil,
			event.PricingChangedData{ItemID: id, Change: event.ChangeItem})
	})
}
