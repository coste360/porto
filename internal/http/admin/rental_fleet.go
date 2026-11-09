package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/rental/fleet"
	"github.com/coste360/porto/internal/domain/rental/slots"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Rental fleet operations (m11-plan §13.1): units, skippers and hours, formulas, extras, settings and the stateless
// quote. Owned by FLEET. The helpers fleetPoint and fleetPriceLines are reusable by the other rental handlers.

// ---- conversions ----

func fleetI18n(t api.I18nText) map[string]string {
	m := map[string]string{"it": t.It}
	if t.En != nil && *t.En != "" {
		m["en"] = *t.En
	}
	return m
}

func fleetI18nOut(m map[string]string) api.I18nText {
	return api.I18nText{It: m["it"], En: view.String(m["en"])}
}

func fleetI18nJSON(j []byte) api.I18nText {
	m := map[string]string{}
	_ = json.Unmarshal(j, &m)
	return fleetI18nOut(m)
}

func fleetCategoriesIn(p *[]api.RentalCategory) []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(*p))
	for _, c := range *p {
		out = append(out, string(c))
	}
	return out
}

func fleetCategoriesOut(list []string) []api.RentalCategory {
	out := make([]api.RentalCategory, 0, len(list))
	for _, c := range list {
		out = append(out, api.RentalCategory(c))
	}
	return out
}

func fleetDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

// fleetPoint converts an API slot point; a malformed date is ok=false (400).
func fleetPoint(p api.RentalSlotPoint) (slots.Point, bool) {
	d, err := calendar.Parse(p.Date)
	if err != nil {
		return slots.Point{}, false
	}
	return slots.Point{Date: d, Slot: p.Slot}, true
}

// fleetPriceLines converts the lines of a rental breakdown.
func fleetPriceLines(b pricing.RentalBreakdown) []api.RentalPriceLine {
	out := make([]api.RentalPriceLine, 0, len(b.Lines))
	for _, l := range b.Lines {
		line := api.RentalPriceLine{
			Seq: l.Seq, Kind: api.RentalPriceLineKind(l.Kind), ItemId: l.ItemID, ItemCode: l.ItemCode, QtyMilli: int64(l.Qty),
			UnitCents: int64(l.UnitCents), AmountCents: int64(l.Amount), PriceIncludesVat: l.PriceIncludesVAT,
		}
		if len(l.RuleIDs) > 0 {
			ids := append([]string(nil), l.RuleIDs...)
			line.RuleIds = &ids
		}
		out = append(out, line)
	}
	return out
}

func fleetUnit(u *fleet.UnitView) api.RentalUnit {
	r := u.RentalUnit
	return api.RentalUnit{
		Id: r.ID, VesselId: r.VesselID, VesselName: u.VesselName, Code: r.Code, Category: api.RentalCategory(r.Category),
		Seats: r.Seats, Licence: api.RentalLicenceMode(r.Licence), LicenceRequired: u.LicenceRequired, EngineKwMilli: u.EngineKWMilli,
		FuelType: api.RentalFuelType(r.FuelType), TankMilli: r.TankMilli, DepositCents: r.DepositCents,
		Status: api.RentalUnitStatus(r.Status), HomeBerthId: view.String(r.HomeBerthID), HomeBerthCode: view.String(u.HomeBerthCode),
		ItemId: r.ItemID, Ownership: api.RentalOwnership(r.Ownership), OwnerCustomerId: view.String(r.OwnerCustomerID),
		EngineHoursMilli: r.EngineHoursMilli, Note: r.Note, Sort: r.Sort, Version: int64(r.Version),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func fleetSkipper(k *fleet.SkipperView) api.RentalSkipper {
	r := k.RentalSkipper
	hours := make([]api.RentalSkipperHoursRow, 0, len(k.Hours))
	for _, h := range k.Hours {
		hours = append(hours, api.RentalSkipperHoursRow{Weekday: h.Weekday, FromSlot: h.FromSlot, ToSlot: h.ToSlot})
	}
	return api.RentalSkipper{
		Id: r.ID, Name: r.Name, Phone: view.String(k.Phone), Email: view.String(k.Email), LicenceExpiresOn: fleetDate(r.LicenceExpiresOn),
		UserId: view.String(r.UserID), Active: r.Active, Sort: r.Sort, Hours: hours, Version: int64(r.Version),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func fleetFormula(f *fleet.Formula) api.RentalFormula {
	return api.RentalFormula{
		Id: f.ID, Code: f.Code, Name: fleetI18nOut(fleet.FormulaName(f)), Kind: api.RentalFormulaKind(f.Kind),
		BandCode: view.String(f.BandCode), MinSlots: f.MinSlots, MaxSlots: f.MaxSlots, MinDays: f.MinDays, MaxDays: f.MaxDays,
		Skipper: api.RentalSkipperMode(f.Skipper), Fuel: api.RentalFuelMode(f.Fuel), FuelItemId: view.String(f.FuelItemID),
		LateItemId: view.String(f.LateItemID), GraceMinutes: f.GraceMinutes, LateStepMinutes: api.RentalFormulaLateStepMinutes(f.LateStepMinutes),
		Categories: fleetCategoriesOut(fleet.FormulaCategories(f)), Active: f.Active, Sort: f.Sort, Version: int64(f.Version),
		CreatedAt: f.CreatedAt.UTC(), UpdatedAt: f.UpdatedAt.UTC(),
	}
}

func fleetExtra(e *fleet.ExtraView) api.RentalExtra {
	cats := []string{}
	_ = json.Unmarshal(e.Categories, &cats)
	return api.RentalExtra{
		Id: e.ID, ItemId: e.ItemID, ItemCode: e.ItemCode, Name: fleetI18nJSON(e.Name), ChargeMode: api.RentalExtraChargeMode(e.ChargeMode),
		DefaultQtyMilli: e.DefaultQtyMilli, MaxQtyMilli: e.MaxQtyMilli, Categories: fleetCategoriesOut(cats), Active: e.Active,
		Sort: e.Sort, Version: int64(e.Version),
	}
}

func fleetSettings(s *fleet.Settings) api.RentalSettings {
	r := s.Row
	bands := make([]api.RentalBand, 0, len(s.Bands))
	for _, b := range s.Bands {
		bands = append(bands, api.RentalBand{Code: b.Code, FromSlot: b.FromSlot, ToSlot: b.ToSlot, Label: fleetI18nOut(b.Label)})
	}
	checklist := make([]api.RentalChecklistTemplateItem, 0, len(s.Checklist))
	for _, c := range s.Checklist {
		checklist = append(checklist, api.RentalChecklistTemplateItem{Key: c.Key, Label: fleetI18nOut(c.Label), Categories: fleetCategoriesOut(c.Categories)})
	}
	modes := make([]api.RentalSettingsDepositModes, 0, len(s.DepositModes))
	for _, m := range s.DepositModes {
		modes = append(modes, api.RentalSettingsDepositModes(m))
	}
	return api.RentalSettings{
		OpenFromSlot: r.OpenFromSlot, OpenToSlot: r.OpenToSlot, Bands: bands, BufferSlots: r.BufferSlots,
		DeskHoldMinutes: r.DeskHoldMinutes, NoShowAfterMinutes: r.NoShowAfterMinutes, LicenceThresholdKwMilli: r.LicenceThresholdKWMilli,
		SkipperItemId: view.String(r.SkipperItemID), DamageItemId: view.String(r.DamageItemID), Checklist: checklist,
		CheckoutMinPhotos: r.CheckoutMinPhotos, CheckinMinPhotos: r.CheckinMinPhotos, DepositModes: modes,
		IdentityRetentionDays: r.IdentityRetentionDays, DefaultRegisterId: view.String(r.DefaultRegisterID),
		PreauthAvailable: s.PreauthAvailable, Version: int64(r.Version),
	}
}

func fleetWrite[T any](w http.ResponseWriter, version, status int, body T) {
	respond.SetETag(w, version)
	respond.JSON(w, status, body)
}

func fleetStrPtr[T ~string](p *T) *string {
	if p == nil {
		return nil
	}
	s := string(*p)
	return &s
}

func fleetStr[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

func fleetIntPtr[T ~int](p *T) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

func fleetMilli(p *int64) *qty.Milli {
	if p == nil {
		return nil
	}
	v := qty.Milli(*p)
	return &v
}

func fleetCents(p *int64) *money.Cents {
	if p == nil {
		return nil
	}
	v := money.Cents(*p)
	return &v
}

// ---- units ----

// ListRentalUnits implements api.ServerInterface (GET .../rental/units).
func (h *Handler) ListRentalUnits(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRentalUnitsParams) {
	items, next, err := h.RentalFleet.ListUnits(r.Context(), principal(r), marinaID, fleet.UnitFilter{
		Status: fleetStr(params.Status), Category: fleetStr(params.Category), Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.RentalUnit, 0, len(items))
	for i := range items {
		out = append(out, fleetUnit(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.RentalUnitList{Items: out, NextCursor: nextCursor(next)})
}

// CreateRentalUnit implements api.ServerInterface (POST .../rental/units).
func (h *Handler) CreateRentalUnit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRentalUnitParams) {
	var in api.RentalUnitInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	u, err := h.RentalFleet.CreateUnit(r.Context(), principal(r), marinaID, fleet.UnitInput{
		VesselID: in.VesselId, Code: in.Code, Category: string(in.Category), Seats: in.Seats, Licence: fleetStr(in.Licence),
		FuelType: string(in.FuelType), Tank: qty.Milli(int64Or(in.TankMilli)), Deposit: money.Cents(int64Or(in.DepositCents)),
		Status: fleetStr(in.Status), HomeBerthID: str(in.HomeBerthId), ItemID: in.ItemId, Ownership: fleetStr(in.Ownership),
		OwnerCustomerID: str(in.OwnerCustomerId), Note: str(in.Note), Sort: intOr(in.Sort, 0),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, u.Version, http.StatusCreated, fleetUnit(u))
}

// GetRentalUnit implements api.ServerInterface (GET .../rental/units/{rentalUnitId}).
func (h *Handler) GetRentalUnit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, unitID api.RentalUnitId) {
	u, err := h.RentalFleet.GetUnit(r.Context(), principal(r), marinaID, unitID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, u.Version, http.StatusOK, fleetUnit(u))
}

// UpdateRentalUnit implements api.ServerInterface (PATCH .../rental/units/{rentalUnitId}).
func (h *Handler) UpdateRentalUnit(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, unitID api.RentalUnitId, params api.UpdateRentalUnitParams) {
	var in api.RentalUnitPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := fleet.UnitPatch{
		Code: in.Code, Category: fleetStrPtr(in.Category), Seats: in.Seats, Licence: fleetStrPtr(in.Licence), FuelType: fleetStrPtr(in.FuelType),
		Tank: fleetMilli(in.TankMilli), Deposit: fleetCents(in.DepositCents), Status: fleetStrPtr(in.Status), HomeBerthID: in.HomeBerthId,
		ItemID: in.ItemId, Ownership: fleetStrPtr(in.Ownership), OwnerCustomerID: in.OwnerCustomerId, Note: in.Note, Sort: in.Sort,
	}
	empty := ""
	if respond.IsNull(fields, "home_berth_id") {
		patch.HomeBerthID = &empty
	}
	if respond.IsNull(fields, "owner_customer_id") {
		patch.OwnerCustomerID = &empty
	}
	u, err := h.RentalFleet.UpdateUnit(r.Context(), principal(r), marinaID, unitID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, u.Version, http.StatusOK, fleetUnit(u))
}

// ---- skippers ----

// ListRentalSkippers implements api.ServerInterface (GET .../rental/skippers).
func (h *Handler) ListRentalSkippers(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRentalSkippersParams) {
	items, next, err := h.RentalFleet.ListSkippers(r.Context(), principal(r), marinaID, fleet.SkipperFilter{
		Active: params.Active, Cursor: str(params.Cursor), Limit: intOr(params.Limit, 0),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.RentalSkipper, 0, len(items))
	for i := range items {
		out = append(out, fleetSkipper(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.RentalSkipperList{Items: out, NextCursor: nextCursor(next)})
}

// CreateRentalSkipper implements api.ServerInterface (POST .../rental/skippers).
func (h *Handler) CreateRentalSkipper(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRentalSkipperParams) {
	var in api.RentalSkipperInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	exp, err := calendar.Parse(str(in.LicenceExpiresOn))
	if in.LicenceExpiresOn != nil && err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	if in.LicenceExpiresOn == nil {
		exp = calendar.Date{}
	}
	k, err := h.RentalFleet.CreateSkipper(r.Context(), principal(r), marinaID, fleet.SkipperInput{
		Name: in.Name, Phone: str(in.Phone), Email: str(in.Email), LicenceExpiresOn: exp, UserID: str(in.UserId),
		Active: in.Active, Sort: intOr(in.Sort, 0),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, k.Version, http.StatusCreated, fleetSkipper(k))
}

// UpdateRentalSkipper implements api.ServerInterface (PATCH .../rental/skippers/{rentalSkipperId}).
func (h *Handler) UpdateRentalSkipper(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, skipperID api.RentalSkipperId, params api.UpdateRentalSkipperParams) {
	var in api.RentalSkipperPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := fleet.SkipperPatch{Name: in.Name, Phone: in.Phone, Email: in.Email, UserID: in.UserId, Active: in.Active, Sort: in.Sort}
	if in.LicenceExpiresOn != nil {
		d := calendar.New(in.LicenceExpiresOn.Year(), in.LicenceExpiresOn.Month(), in.LicenceExpiresOn.Day())
		patch.LicenceExpiresOn = &d
	}
	patch.ClearLicence = respond.IsNull(fields, "licence_expires_on")
	empty := ""
	for key, dst := range map[string]**string{"phone": &patch.Phone, "email": &patch.Email, "user_id": &patch.UserID} {
		if respond.IsNull(fields, key) {
			*dst = &empty
		}
	}
	k, err := h.RentalFleet.UpdateSkipper(r.Context(), principal(r), marinaID, skipperID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, k.Version, http.StatusOK, fleetSkipper(k))
}

// PutRentalSkipperHours implements api.ServerInterface (PUT .../rental/skippers/{rentalSkipperId}/hours).
func (h *Handler) PutRentalSkipperHours(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, skipperID api.RentalSkipperId, params api.PutRentalSkipperHoursParams) {
	var in api.RentalSkipperHoursInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	rows := make([]fleet.HoursRow, 0, len(in.Hours))
	for _, x := range in.Hours {
		rows = append(rows, fleet.HoursRow{Weekday: x.Weekday, FromSlot: x.FromSlot, ToSlot: x.ToSlot})
	}
	k, err := h.RentalFleet.PutSkipperHours(r.Context(), principal(r), marinaID, skipperID, version, rows)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, k.Version, http.StatusOK, fleetSkipper(k))
}

// ---- formulas ----

// ListRentalFormulas implements api.ServerInterface (GET .../rental/formulas).
func (h *Handler) ListRentalFormulas(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRentalFormulasParams) {
	items, err := h.RentalFleet.ListFormulas(r.Context(), principal(r), marinaID, params.Active)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.RentalFormula, 0, len(items))
	for i := range items {
		out = append(out, fleetFormula(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.RentalFormulaList{Items: out})
}

// CreateRentalFormula implements api.ServerInterface (POST .../rental/formulas).
func (h *Handler) CreateRentalFormula(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRentalFormulaParams) {
	var in api.RentalFormulaInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	f, err := h.RentalFleet.CreateFormula(r.Context(), principal(r), marinaID, fleet.FormulaInput{
		Code: in.Code, Name: fleetI18n(in.Name), Kind: string(in.Kind), BandCode: str(in.BandCode), MinSlots: intOr(in.MinSlots, 0),
		MaxSlots: intOr(in.MaxSlots, 0), MinDays: intOr(in.MinDays, 0), MaxDays: intOr(in.MaxDays, 0), Skipper: fleetStr(in.Skipper),
		Fuel: fleetStr(in.Fuel), FuelItemID: str(in.FuelItemId), LateItemID: str(in.LateItemId), GraceMinutes: in.GraceMinutes,
		LateStepMinutes: intOr(fleetIntPtr(in.LateStepMinutes), 0), Categories: fleetCategoriesIn(in.Categories), Active: in.Active,
		Sort: intOr(in.Sort, 0),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, f.Version, http.StatusCreated, fleetFormula(f))
}

// UpdateRentalFormula implements api.ServerInterface (PATCH .../rental/formulas/{rentalFormulaId}).
func (h *Handler) UpdateRentalFormula(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, formulaID api.RentalFormulaId, params api.UpdateRentalFormulaParams) {
	var in api.RentalFormulaPatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := fleet.FormulaPatch{
		Code: in.Code, BandCode: in.BandCode, MinSlots: in.MinSlots, MaxSlots: in.MaxSlots, MinDays: in.MinDays, MaxDays: in.MaxDays,
		Skipper: fleetStrPtr(in.Skipper), Fuel: fleetStrPtr(in.Fuel), FuelItemID: in.FuelItemId, LateItemID: in.LateItemId,
		GraceMinutes: in.GraceMinutes, LateStepMinutes: fleetIntPtr(in.LateStepMinutes), Categories: fleetCategoriesIn(in.Categories),
		Active: in.Active, Sort: in.Sort,
	}
	if in.Name != nil {
		patch.Name = fleetI18n(*in.Name)
	}
	empty, zero := "", 0
	if respond.IsNull(fields, "late_item_id") {
		patch.LateItemID = &empty
	}
	if respond.IsNull(fields, "fuel_item_id") {
		patch.FuelItemID = &empty
	}
	if respond.IsNull(fields, "max_slots") {
		patch.MaxSlots = &zero
	}
	f, err := h.RentalFleet.UpdateFormula(r.Context(), principal(r), marinaID, formulaID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, f.Version, http.StatusOK, fleetFormula(f))
}

// ---- extras ----

// ListRentalExtras implements api.ServerInterface (GET .../rental/extras).
func (h *Handler) ListRentalExtras(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRentalExtrasParams) {
	items, err := h.RentalFleet.ListExtras(r.Context(), principal(r), marinaID, params.Active)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.RentalExtra, 0, len(items))
	for i := range items {
		out = append(out, fleetExtra(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.RentalExtraList{Items: out})
}

// CreateRentalExtra implements api.ServerInterface (POST .../rental/extras).
func (h *Handler) CreateRentalExtra(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRentalExtraParams) {
	var in api.RentalExtraInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	def := int64(1000)
	if in.DefaultQtyMilli != nil {
		def = *in.DefaultQtyMilli
	}
	e, err := h.RentalFleet.CreateExtra(r.Context(), principal(r), marinaID, fleet.ExtraInput{
		ItemID: in.ItemId, ChargeMode: fleetStr(in.ChargeMode), DefaultQtyMilli: qty.Milli(def), MaxQtyMilli: qty.Milli(int64Or(in.MaxQtyMilli)),
		Categories: fleetCategoriesIn(in.Categories), Active: in.Active, Sort: intOr(in.Sort, 0),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, e.Version, http.StatusCreated, fleetExtra(e))
}

// UpdateRentalExtra implements api.ServerInterface (PATCH .../rental/extras/{rentalExtraId}).
func (h *Handler) UpdateRentalExtra(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, extraID api.RentalExtraId, params api.UpdateRentalExtraParams) {
	var in api.RentalExtraPatch
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	e, err := h.RentalFleet.UpdateExtra(r.Context(), principal(r), marinaID, extraID, version, fleet.ExtraPatch{
		ChargeMode: fleetStrPtr(in.ChargeMode), DefaultQtyMilli: fleetMilli(in.DefaultQtyMilli), MaxQtyMilli: fleetMilli(in.MaxQtyMilli),
		Categories: fleetCategoriesIn(in.Categories), Active: in.Active, Sort: in.Sort,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, e.Version, http.StatusOK, fleetExtra(e))
}

// ---- settings ----

// GetRentalSettings implements api.ServerInterface (GET .../rental/settings).
func (h *Handler) GetRentalSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.RentalFleet.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, s.Row.Version, http.StatusOK, fleetSettings(s))
}

// PutRentalSettings implements api.ServerInterface (PUT .../rental/settings; If-Match "0" on the first save).
func (h *Handler) PutRentalSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.PutRentalSettingsParams) {
	var in api.RentalSettingsInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := ifMatchZero(w, r, params.IfMatch)
	if !ok {
		return
	}
	bands := make([]fleet.Band, 0, len(in.Bands))
	for _, b := range in.Bands {
		bands = append(bands, fleet.Band{Code: b.Code, FromSlot: b.FromSlot, ToSlot: b.ToSlot, Label: fleetI18n(b.Label)})
	}
	checklist := make([]fleet.ChecklistItem, 0, len(in.Checklist))
	for _, c := range in.Checklist {
		cats := fleetCategoriesIn(&c.Categories)
		checklist = append(checklist, fleet.ChecklistItem{Key: c.Key, Label: fleetI18n(c.Label), Categories: cats})
	}
	modes := make([]string, 0, len(in.DepositModes))
	for _, m := range in.DepositModes {
		modes = append(modes, string(m))
	}
	s, err := h.RentalFleet.PutSettings(r.Context(), principal(r), marinaID, version, fleet.SettingsInput{
		OpenFromSlot: in.OpenFromSlot, OpenToSlot: in.OpenToSlot, Bands: bands, BufferSlots: in.BufferSlots,
		DeskHoldMinutes: in.DeskHoldMinutes, NoShowAfterMinutes: in.NoShowAfterMinutes, LicenceThresholdKWMilli: in.LicenceThresholdKwMilli,
		SkipperItemID: str(in.SkipperItemId), DamageItemID: str(in.DamageItemId), Checklist: checklist,
		CheckoutMinPhotos: in.CheckoutMinPhotos, CheckinMinPhotos: in.CheckinMinPhotos, DepositModes: modes,
		IdentityRetentionDays: in.IdentityRetentionDays, DefaultRegisterID: str(in.DefaultRegisterId),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	fleetWrite(w, s.Row.Version, http.StatusOK, fleetSettings(s))
}

// ---- quote ----

// QuoteRental implements api.ServerInterface (POST .../rental/quote).
func (h *Handler) QuoteRental(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.RentalQuoteInput
	if !h.mntDecode(w, r, marinaID, &in, false) {
		return
	}
	start, ok1 := fleetPoint(in.Start)
	end, ok2 := fleetPoint(in.End)
	if !ok1 || !ok2 {
		h.badPricingBody(w, r, marinaID)
		return
	}
	q := fleet.QuoteInput{
		UnitID: in.UnitId, Range: slots.Range{Start: start, End: end}, FormulaID: str(in.FormulaId), SkipperID: str(in.SkipperId),
		WithSkipper: in.WithSkipper != nil && *in.WithSkipper, CustomerID: str(in.CustomerId),
	}
	if in.Extras != nil {
		for _, x := range *in.Extras {
			q.Extras = append(q.Extras, fleet.ExtraQty{ExtraID: x.ExtraId, Qty: qty.Milli(x.QtyMilli)})
		}
	}
	opts, err := h.RentalFleet.Quote(r.Context(), principal(r), marinaID, q)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.RentalQuoteOption, 0, len(opts))
	for _, o := range opts {
		item := api.RentalQuoteOption{
			FormulaId: o.FormulaID, FormulaCode: o.FormulaCode, Kind: api.RentalFormulaKind(o.Kind), Ok: o.OK,
			Lines: []api.RentalPriceLine{}, Problem: view.String(o.Problem),
		}
		if o.OK {
			ev := o.Snapshot.EngineVersion
			item.Lines, item.TotalCents, item.LicenceRequired, item.EngineVersion = fleetPriceLines(o.Snapshot.Breakdown), int64(o.Snapshot.Total()), o.Snapshot.LicenceRequired, &ev
		}
		out = append(out, item)
	}
	respond.JSON(w, http.StatusOK, api.RentalQuoteResult{Items: out})
}

// ifMatchZero parses an If-Match that may be "0" (first save of the rental settings, never saved before): missing
// is 428, a non-number 412.
func ifMatchZero(w http.ResponseWriter, r *http.Request, header *string) (int, bool) {
	if header == nil {
		return respond.IfMatch(w, r, header)
	}
	v := strings.Trim(strings.TrimPrefix(strings.TrimSpace(*header), "W/"), `"`)
	if v == "0" {
		return 0, true
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n, true
	}
	return respond.IfMatch(w, r, header)
}
