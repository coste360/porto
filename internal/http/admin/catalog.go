package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Pricing catalogue handlers (m2-plan §4.3): pricing settings, revenue classes, items, berth categories.

func catText(t api.I18nText) catalog.I18nText { return catalog.I18nText{It: t.It, En: t.En} }

func catTextOptional(t *api.I18nTextOptional) catalog.I18nTextOptional {
	if t == nil {
		return catalog.I18nTextOptional{}
	}
	return catalog.I18nTextOptional{It: str(t.It), En: str(t.En)}
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func targets(in []api.CategoryTarget) []string {
	out := make([]string, len(in))
	for i, t := range in {
		out[i] = string(t)
	}
	return out
}

// GetPricingSettings implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/pricing-settings).
func (h *Handler) GetPricingSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.Catalog.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, view.PricingSettings(s))
}

// UpdatePricingSettings implements api.ServerInterface (PUT /api/admin/v1/marinas/{marinaId}/pricing-settings).
func (h *Handler) UpdatePricingSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.UpdatePricingSettingsParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.PricingSettingsUpdate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	s, err := h.Catalog.UpdateSettings(r.Context(), principal(r), marinaID, version,
		catalog.SettingsInput{DefaultVATRateBP: in.DefaultVatRateBp, DefaultPriceIncludesVAT: in.DefaultPriceIncludesVat})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, s.Version)
	respond.JSON(w, http.StatusOK, view.PricingSettings(s))
}

// ListRevenueClasses implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/revenue-classes).
func (h *Handler) ListRevenueClasses(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRevenueClassesParams) {
	list, err := h.Catalog.ListRevenueClasses(r.Context(), principal(r), marinaID, boolOr(params.IncludeArchived, false))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.RevenueClasses(list))
}

// CreateRevenueClass implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/revenue-classes).
func (h *Handler) CreateRevenueClass(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRevenueClassParams) {
	var in api.RevenueClassCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	c, err := h.Catalog.CreateRevenueClass(r.Context(), principal(r), marinaID, catalog.RevenueClassInput{
		Code: in.Code, Name: catText(in.Name), ParentID: str(in.ParentId), SortOrder: in.SortOrder,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Version)
	respond.JSON(w, http.StatusCreated, view.RevenueClass(c))
}

// UpdateRevenueClass implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/revenue-classes/{revenueClassId}).
func (h *Handler) UpdateRevenueClass(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.RevenueClassId, params api.UpdateRevenueClassParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.RevenueClassPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	patch := catalog.RevenueClassPatch{Code: in.Code, ParentID: in.ParentId, SortOrder: in.SortOrder, Archived: in.Archived}
	if respond.IsNull(fields, "parent_id") {
		top := ""
		patch.ParentID = &top
	}
	if in.Name != nil {
		n := catText(*in.Name)
		patch.Name = &n
	}
	c, err := h.Catalog.UpdateRevenueClass(r.Context(), principal(r), marinaID, id, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Version)
	respond.JSON(w, http.StatusOK, view.RevenueClass(c))
}

// DeleteRevenueClass implements api.ServerInterface (DELETE /api/admin/v1/marinas/{marinaId}/revenue-classes/{revenueClassId}).
func (h *Handler) DeleteRevenueClass(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.RevenueClassId, params api.DeleteRevenueClassParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Catalog.DeleteRevenueClass(r.Context(), principal(r), marinaID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ListItems implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/items).
func (h *Handler) ListItems(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListItemsParams) {
	f := catalog.ItemFilter{Active: params.Active, Q: str(params.Q)}
	if params.Type != nil {
		f.Type = string(*params.Type)
	}
	page, err := h.Catalog.ListItems(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.ItemPage(page.Items, page.NextCursor))
}

// CreateItem implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/items).
func (h *Handler) CreateItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateItemParams) {
	var in api.ItemCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	ci := catalog.ItemInput{
		Code: in.Code, Name: catText(in.Name), Description: catTextOptional(in.Description), ItemType: string(in.ItemType),
		Unit: string(in.Unit), VATRateBP: in.VatRateBp, PriceIncludesVAT: in.PriceIncludesVat, RevenueClassID: in.RevenueClassId,
		ExternalCode: str(in.ExternalCode), Active: in.Active, OnlineVisible: boolOr(in.OnlineVisible, false), SortOrder: in.SortOrder,
	}
	if in.VatNature != nil {
		ci.VATNature = string(*in.VatNature)
	}
	it, err := h.Catalog.CreateItem(r.Context(), principal(r), marinaID, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, it.Version)
	respond.JSON(w, http.StatusCreated, view.Item(it))
}

// GetItem implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/items/{itemId}).
func (h *Handler) GetItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ItemId) {
	it, err := h.Catalog.GetItem(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, it.Version)
	respond.JSON(w, http.StatusOK, view.Item(it))
}

// UpdateItem implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/items/{itemId}).
func (h *Handler) UpdateItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ItemId, params api.UpdateItemParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ItemPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	patch := catalog.ItemPatch{
		Code: in.Code, VATRateBP: in.VatRateBp, PriceIncludesVAT: in.PriceIncludesVat, RevenueClassID: in.RevenueClassId,
		ExternalCode: in.ExternalCode, Active: in.Active, OnlineVisible: in.OnlineVisible, SortOrder: in.SortOrder,
	}
	if in.Name != nil {
		n := catText(*in.Name)
		patch.Name = &n
	}
	if in.Description != nil {
		d := catTextOptional(in.Description)
		patch.Description = &d
	}
	if in.ItemType != nil {
		t := string(*in.ItemType)
		patch.ItemType = &t
	}
	if in.Unit != nil {
		u := string(*in.Unit)
		patch.Unit = &u
	}
	switch {
	case in.VatNature != nil:
		n := string(*in.VatNature)
		patch.VATNature = &n
	case respond.IsNull(fields, "vat_nature"):
		none := ""
		patch.VATNature = &none
	}
	it, err := h.Catalog.UpdateItem(r.Context(), principal(r), marinaID, id, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, it.Version)
	respond.JSON(w, http.StatusOK, view.Item(it))
}

// DeleteItem implements api.ServerInterface (DELETE /api/admin/v1/marinas/{marinaId}/items/{itemId}).
func (h *Handler) DeleteItem(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ItemId, params api.DeleteItemParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Catalog.DeleteItem(r.Context(), principal(r), marinaID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ListBerthCategories implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/berth-categories).
func (h *Handler) ListBerthCategories(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListBerthCategoriesParams) {
	l, err := h.Catalog.ListCategories(r.Context(), principal(r), marinaID, boolOr(params.IncludeArchived, false))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.BerthCategories(l))
}

// ListCustomerCategoryCounts implements api.ServerInterface (GET /api/admin/v1/marinas/{marinaId}/customer-categories).
func (h *Handler) ListCustomerCategoryCounts(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	c, err := h.Catalog.CustomerCategoryCounts(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.CustomerCategoryCounts(c))
}

// CreateBerthCategory implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/berth-categories).
func (h *Handler) CreateBerthCategory(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateBerthCategoryParams) {
	var in api.BerthCategoryCreate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	c, err := h.Catalog.CreateCategory(r.Context(), principal(r), marinaID, catalog.CategoryInput{
		Code: in.Code, Name: catText(in.Name), AppliesTo: targets(in.AppliesTo), SortOrder: in.SortOrder,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Version)
	respond.JSON(w, http.StatusCreated, view.BerthCategory(c))
}

// UpdateBerthCategory implements api.ServerInterface (PATCH /api/admin/v1/marinas/{marinaId}/berth-categories/{categoryId}).
func (h *Handler) UpdateBerthCategory(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CategoryId, params api.UpdateBerthCategoryParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.BerthCategoryPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	patch := catalog.CategoryPatch{SortOrder: in.SortOrder, Archived: in.Archived}
	if in.Name != nil {
		n := catText(*in.Name)
		patch.Name = &n
	}
	if in.AppliesTo != nil {
		t := targets(*in.AppliesTo)
		patch.AppliesTo = &t
	}
	c, err := h.Catalog.UpdateCategory(r.Context(), principal(r), marinaID, id, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, c.Version)
	respond.JSON(w, http.StatusOK, view.BerthCategory(c))
}

// DeleteBerthCategory implements api.ServerInterface (DELETE /api/admin/v1/marinas/{marinaId}/berth-categories/{categoryId}).
func (h *Handler) DeleteBerthCategory(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CategoryId, params api.DeleteBerthCategoryParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.Catalog.DeleteCategory(r.Context(), principal(r), marinaID, id, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}

// SyncBerthCategories implements api.ServerInterface (POST /api/admin/v1/marinas/{marinaId}/berth-categories/sync).
func (h *Handler) SyncBerthCategories(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.SyncBerthCategoriesParams) {
	list, err := h.Catalog.SyncCategories(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.BerthCategorySync(list))
}
