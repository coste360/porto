package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Yard catalogue (M10, m10-plan §3, §13): work types, qualifications, item yard settings, formula variables, band
// tables, work packages, formula validation, yard settings. Owned by QUOTE.

// errYardBadValue is a formula value that is neither an integer (milli), a boolean nor a string.
var errYardBadValue = errors.New("admin: invalid formula value")

// yardDecode decodes a body; a malformed one is 400 after the marina check (another marina stays 404).
func (h *Handler) yardDecode(w http.ResponseWriter, r *http.Request, marinaID string, v any, optional bool) bool {
	if err := respond.Decode(r, v, optional); err != nil {
		h.badPricingBody(w, r, marinaID)
		return false
	}
	return true
}

// yardOptionalIfMatch reads an optional If-Match header: 0 when absent.
func yardOptionalIfMatch(w http.ResponseWriter, r *http.Request, header *string) (int, bool) {
	if header == nil || strings.TrimSpace(*header) == "" {
		return 0, true
	}
	return respond.IfMatch(w, r, header)
}

// yardRespondOptional answers a version mismatch without If-Match as 428 (the row exists, If-Match is required).
func yardRespondOptional(w http.ResponseWriter, r *http.Request, header *string, err error) {
	if errors.Is(err, yardquote.ErrVersionMismatch) && (header == nil || strings.TrimSpace(*header) == "") {
		respond.Problem(w, r, problem.PreconditionRequired, problem.Detail("If-Match header required"))
		return
	}
	respond.Error(w, r, err)
}

func yardText(t api.I18nText) yardquote.I18n { return yardquote.I18n{It: t.It, En: t.En} }

func apiYardText(t yardquote.I18n) api.I18nText { return api.I18nText{It: t.It, En: t.En} }

func yardTextJSON(j []byte) api.I18nText {
	var t yardquote.I18n
	if len(j) > 0 {
		_ = json.Unmarshal(j, &t)
	}
	return apiYardText(t)
}

// yardValueOf decodes a FormulaValue: an integer is a number in milli, a boolean, a string an enum value.
func yardValueOf(fv api.FormulaValue) (price.VariableValue, error) {
	raw, err := fv.MarshalJSON()
	if err != nil {
		return price.VariableValue{}, errYardBadValue
	}
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) == 0:
		return price.VariableValue{}, errYardBadValue
	case raw[0] == '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return price.VariableValue{}, errYardBadValue
		}
		return price.VariableValue{Kind: price.KindEnum, Enum: s}, nil
	case string(raw) == "true" || string(raw) == "false":
		return price.VariableValue{Kind: price.KindBool, Bool: string(raw) == "true"}, nil
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return price.VariableValue{}, errYardBadValue
	}
	return price.VariableValue{Kind: price.KindNumber, Num: qty.Milli(n)}, nil
}

func yardValuesOf(m *map[string]api.FormulaValue) (map[string]price.VariableValue, error) {
	if m == nil {
		return nil, nil
	}
	out := make(map[string]price.VariableValue, len(*m))
	for k, fv := range *m {
		v, err := yardValueOf(fv)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func apiYardValue(v price.VariableValue) api.FormulaValue {
	var fv api.FormulaValue
	switch v.Kind {
	case price.KindBool:
		_ = fv.FromFormulaValue1(v.Bool)
	case price.KindEnum:
		_ = fv.FromFormulaValue2(v.Enum)
	default:
		_ = fv.FromFormulaValue0(int64(v.Num))
	}
	return fv
}

func apiYardValues(m map[string]price.VariableValue) *map[string]api.FormulaValue {
	out := make(map[string]api.FormulaValue, len(m))
	for k, v := range m {
		out[k] = apiYardValue(v)
	}
	return &out
}

func yardMilli(p *int64) *qty.Milli {
	if p == nil {
		return nil
	}
	v := qty.Milli(*p)
	return &v
}

func yardInt64(p *qty.Milli) *int64 {
	if p == nil {
		return nil
	}
	v := int64(*p)
	return &v
}

func yardIssues(is []yardquote.Issue) []api.FormulaIssue {
	out := make([]api.FormulaIssue, 0, len(is))
	for _, i := range is {
		fi := api.FormulaIssue{Code: i.Code, Field: view.String(i.Field), Ref: view.String(i.Ref)}
		if i.Pos != 0 {
			pos := i.Pos
			fi.Pos = &pos
		}
		if len(i.Params) > 0 {
			p := i.Params
			fi.Params = &p
		}
		out = append(out, fi)
	}
	return out
}

func yardTestCaseOf(tc *api.FormulaTestCase) (*yardquote.TestCase, error) {
	if tc == nil {
		return nil, nil
	}
	vars, err := yardValuesOf(tc.Vars)
	if err != nil {
		return nil, err
	}
	out := &yardquote.TestCase{Vars: vars, ExpectQty: yardMilli(tc.ExpectQtyMilli)}
	if tc.ExpectPriceCents != nil {
		c := money.Cents(*tc.ExpectPriceCents)
		out.ExpectPrice = &c
	}
	return out, nil
}

func apiYardTestCase(tc *yardquote.TestCase) *api.FormulaTestCase {
	if tc == nil {
		return nil
	}
	out := &api.FormulaTestCase{ExpectQtyMilli: yardInt64(tc.ExpectQty)}
	if len(tc.Vars) > 0 {
		out.Vars = apiYardValues(tc.Vars)
	}
	if tc.ExpectPrice != nil {
		c := int64(*tc.ExpectPrice)
		out.ExpectPriceCents = &c
	}
	return out
}

// ---- work types ----

func apiYardWorkType(r *yardquote.WorkType) api.WorkType {
	return api.WorkType{
		Id: r.ID, Code: r.Code, Name: yardTextJSON(r.Name), RevenueClassId: view.String(r.RevenueClassID), Active: r.Active,
		SortOrder: r.SortOrder, Version: int64(r.Version), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func writeYardWorkType(w http.ResponseWriter, r *yardquote.WorkType, status int) {
	respond.SetETag(w, r.Version)
	respond.JSON(w, status, apiYardWorkType(r))
}

// ListWorkTypes implements api.ServerInterface.
func (h *Handler) ListWorkTypes(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListWorkTypesParams) {
	rows, err := h.YardQuotes.ListWorkTypes(r.Context(), principal(r), marinaID, params.Active)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.WorkType, 0, len(rows))
	for i := range rows {
		out = append(out, apiYardWorkType(&rows[i]))
	}
	respond.JSON(w, http.StatusOK, api.WorkTypeList{Items: out})
}

// CreateWorkType implements api.ServerInterface.
func (h *Handler) CreateWorkType(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateWorkTypeParams) {
	var in api.WorkTypeInput
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	row, err := h.YardQuotes.CreateWorkType(r.Context(), principal(r), marinaID, yardquote.WorkTypeInput{
		Code: in.Code, Name: yardText(in.Name), RevenueClassID: str(in.RevenueClassId), Active: in.Active, SortOrder: in.SortOrder,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardWorkType(w, row, http.StatusCreated)
}

// UpdateWorkType implements api.ServerInterface.
func (h *Handler) UpdateWorkType(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, workTypeID api.WorkTypeId, params api.UpdateWorkTypeParams) {
	var in api.WorkTypePatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := yardquote.WorkTypePatch{Code: in.Code, RevenueClassID: in.RevenueClassId, Active: in.Active, SortOrder: in.SortOrder}
	if in.Name != nil {
		t := yardText(*in.Name)
		patch.Name = &t
	}
	if respond.IsNull(fields, "revenue_class_id") {
		empty := ""
		patch.RevenueClassID = &empty
	}
	row, err := h.YardQuotes.UpdateWorkType(r.Context(), principal(r), marinaID, workTypeID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardWorkType(w, row, http.StatusOK)
}

// ---- qualifications ----

func apiYardQualification(q *yardquote.Qualification) api.YardQualification {
	return api.YardQualification{
		Id: q.ID, Code: q.Code, Name: yardTextJSON(q.Name), LabourItemId: q.LabourItemID, DefaultCostCents: q.DefaultCostCents,
		LiftOperator: q.LiftOperator, Active: q.Active, SortOrder: q.SortOrder, Version: int64(q.Version),
		CreatedAt: q.CreatedAt.UTC(), UpdatedAt: q.UpdatedAt.UTC(),
	}
}

func writeYardQualification(w http.ResponseWriter, q *yardquote.Qualification, status int) {
	respond.SetETag(w, q.Version)
	respond.JSON(w, status, apiYardQualification(q))
}

// ListYardQualifications implements api.ServerInterface.
func (h *Handler) ListYardQualifications(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListYardQualificationsParams) {
	rows, err := h.YardQuotes.ListQualifications(r.Context(), principal(r), marinaID, params.Active)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.YardQualification, 0, len(rows))
	for i := range rows {
		out = append(out, apiYardQualification(&rows[i]))
	}
	respond.JSON(w, http.StatusOK, api.YardQualificationList{Items: out})
}

// CreateYardQualification implements api.ServerInterface.
func (h *Handler) CreateYardQualification(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateYardQualificationParams) {
	var in api.YardQualificationInput
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	row, err := h.YardQuotes.CreateQualification(r.Context(), principal(r), marinaID, yardquote.QualificationInput{
		Code: in.Code, Name: yardText(in.Name), LabourItemID: in.LabourItemId, DefaultCostCents: in.DefaultCostCents,
		LiftOperator: in.LiftOperator, Active: in.Active, SortOrder: in.SortOrder,
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardQualification(w, row, http.StatusCreated)
}

// UpdateYardQualification implements api.ServerInterface.
func (h *Handler) UpdateYardQualification(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, qualificationID api.QualificationId, params api.UpdateYardQualificationParams) {
	var in api.YardQualificationPatch
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := yardquote.QualificationPatch{
		Code: in.Code, LabourItemID: in.LabourItemId, DefaultCostCents: in.DefaultCostCents, LiftOperator: in.LiftOperator,
		Active: in.Active, SortOrder: in.SortOrder,
	}
	if in.Name != nil {
		t := yardText(*in.Name)
		patch.Name = &t
	}
	row, err := h.YardQuotes.UpdateQualification(r.Context(), principal(r), marinaID, qualificationID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardQualification(w, row, http.StatusOK)
}

// ---- items ----

func apiYardItem(it *yardquote.YardItem) api.YardItem {
	out := api.YardItem{
		ItemId: it.Item.ID, Code: it.Item.Code, Name: yardTextJSON(it.Item.Name), ItemType: api.ItemType(it.Item.ItemType),
		Unit: api.ItemUnit(it.Item.Unit), VatRateBp: it.Item.VATRateBP, PriceIncludesVat: it.Item.PriceIncludesVAT, Active: it.Item.Active,
	}
	if st := it.Settings; st != nil {
		out.Configured, out.YardEnabled, out.CostCents = true, st.YardEnabled, st.CostCents
		out.QualificationId, out.QtyFormula, out.PriceFormula = view.String(st.QualificationID), st.QtyFormula, st.PriceFormula
		out.Category, out.Version, out.TestCase = st.Category, int64(st.Version), apiYardTestCase(it.TestCase)
	}
	return out
}

func writeYardItem(w http.ResponseWriter, it *yardquote.YardItem) {
	if it.Settings != nil {
		respond.SetETag(w, it.Settings.Version)
	}
	respond.JSON(w, http.StatusOK, apiYardItem(it))
}

// ListYardItems implements api.ServerInterface.
func (h *Handler) ListYardItems(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListYardItemsParams) {
	f := yardquote.ItemFilter{Query: str(params.Q), Category: str(params.Category), YardEnabled: params.YardEnabled}
	if params.ItemType != nil {
		f.ItemType = string(*params.ItemType)
	}
	items, next, err := h.YardQuotes.ListItems(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.YardItem, 0, len(items))
	for i := range items {
		out = append(out, apiYardItem(&items[i]))
	}
	respond.JSON(w, http.StatusOK, api.YardItemPage{Items: out, NextCursor: nextCursor(next)})
}

// GetYardItemSettings implements api.ServerInterface.
func (h *Handler) GetYardItemSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, itemID api.ItemId) {
	it, err := h.YardQuotes.GetItem(r.Context(), principal(r), marinaID, itemID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardItem(w, it)
}

// PutYardItemSettings implements api.ServerInterface.
func (h *Handler) PutYardItemSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, itemID api.ItemId, params api.PutYardItemSettingsParams) {
	var in api.YardItemSettingsInput
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := yardOptionalIfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	tc, err := yardTestCaseOf(in.TestCase)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	si := yardquote.ItemSettingsInput{
		YardEnabled: in.YardEnabled, CostCents: in.CostCents, QualificationID: in.QualificationId, QtyFormula: in.QtyFormula,
		PriceFormula: in.PriceFormula, Category: in.Category, TestCase: tc, ClearTestCase: in.ClearTestCase != nil && *in.ClearTestCase,
	}
	if respond.IsNull(fields, "qualification_id") {
		empty := ""
		si.QualificationID = &empty
	}
	it, err := h.YardQuotes.PutItemSettings(r.Context(), principal(r), marinaID, itemID, version, si)
	if err != nil {
		yardRespondOptional(w, r, params.IfMatch, err)
		return
	}
	writeYardItem(w, it)
}

// ---- variables ----

func apiYardVariable(v *yardquote.VariableView) api.FormulaVariable {
	out := api.FormulaVariable{
		Name: v.Name, Source: api.FormulaVariableSource(v.Source), Kind: api.FormulaVariableKind(v.Kind), Unit: api.FormulaUnit(v.Unit),
		Label: apiYardText(v.Label), Builtin: v.Builtin,
	}
	if len(v.Enum) > 0 {
		e := append([]string{}, v.Enum...)
		out.EnumValues = &e
	}
	if v.TestValue != nil {
		tv := apiYardValue(*v.TestValue)
		out.TestValue = &tv
	}
	if r := v.Row; r != nil {
		ver := int64(r.Version)
		out.Id, out.Version = &r.ID, &ver
		out.Expression = view.String(r.Expression)
		out.MinMilli, out.MaxMilli, out.DefaultMilli = r.MinMilli, r.MaxMilli, r.DefaultMilli
		out.DefaultEnum = view.String(r.DefaultEnum)
	}
	return out
}

func writeYardVariable(w http.ResponseWriter, v *yardquote.VariableView, status int) {
	if v.Row != nil {
		respond.SetETag(w, v.Row.Version)
	}
	respond.JSON(w, status, apiYardVariable(v))
}

// ListFormulaVariables implements api.ServerInterface.
func (h *Handler) ListFormulaVariables(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	vars, err := h.YardQuotes.ListVariables(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.FormulaVariable, 0, len(vars))
	for i := range vars {
		out = append(out, apiYardVariable(&vars[i]))
	}
	respond.JSON(w, http.StatusOK, api.FormulaVariableList{Items: out})
}

// CreateFormulaVariable implements api.ServerInterface.
func (h *Handler) CreateFormulaVariable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateFormulaVariableParams) {
	var in api.FormulaVariableInput
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	vi := yardquote.VariableInput{
		Name: in.Name, Source: string(in.Source), Kind: string(in.Kind), Expression: str(in.Expression), Min: yardMilli(in.MinMilli),
		Max: yardMilli(in.MaxMilli), Default: yardMilli(in.DefaultMilli), DefaultEnum: str(in.DefaultEnum),
	}
	if in.Unit != nil {
		vi.Unit = string(*in.Unit)
	}
	if in.EnumValues != nil {
		vi.EnumValues = *in.EnumValues
	}
	if in.Label != nil {
		t := yardText(*in.Label)
		vi.Label = &t
	}
	v, err := h.YardQuotes.CreateVariable(r.Context(), principal(r), marinaID, vi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardVariable(w, v, http.StatusCreated)
}

// UpdateFormulaVariable implements api.ServerInterface.
func (h *Handler) UpdateFormulaVariable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, variableID api.VariableId, params api.UpdateFormulaVariableParams) {
	var in api.FormulaVariablePatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := yardquote.VariablePatch{Expression: in.Expression, EnumValues: in.EnumValues}
	if in.Unit != nil {
		u := string(*in.Unit)
		patch.Unit = &u
	}
	if in.Label != nil {
		t := yardText(*in.Label)
		patch.Label = &t
	}
	opt := func(key string, v *int64) yardquote.OptMilli {
		_, present := fields[key]
		return yardquote.OptMilli{Set: present, V: yardMilli(v)}
	}
	patch.Min, patch.Max, patch.Default = opt("min_milli", in.MinMilli), opt("max_milli", in.MaxMilli), opt("default_milli", in.DefaultMilli)
	if _, present := fields["default_enum"]; present {
		patch.DefaultEnum = yardquote.OptString{Set: true, V: str(in.DefaultEnum)}
	}
	v, err := h.YardQuotes.UpdateVariable(r.Context(), principal(r), marinaID, variableID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardVariable(w, v, http.StatusOK)
}

// DeleteFormulaVariable implements api.ServerInterface.
func (h *Handler) DeleteFormulaVariable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, variableID api.VariableId, params api.DeleteFormulaVariableParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	if err := h.YardQuotes.DeleteVariable(r.Context(), principal(r), marinaID, variableID, version); err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- tables ----

func yardFormulaRows(rows []api.FormulaTableRow) []formula.Row {
	out := make([]formula.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, formula.Row{UpTo: yardMilli(r.UpToMilli), Value: qty.Milli(r.ValueMilli)})
	}
	return out
}

func apiYardTable(t *yardquote.TableView) api.FormulaTable {
	rows := make([]api.FormulaTableRow, 0, len(t.Rows))
	for _, r := range t.Rows {
		rows = append(rows, api.FormulaTableRow{UpToMilli: yardInt64(r.UpTo), ValueMilli: int64(r.Value)})
	}
	return api.FormulaTable{
		Id: t.Row.ID, Name: t.Row.Name, Label: yardTextJSON(t.Row.Label), Rows: rows, Version: int64(t.Row.Version),
		CreatedAt: t.Row.CreatedAt.UTC(), UpdatedAt: t.Row.UpdatedAt.UTC(),
	}
}

func writeYardTable(w http.ResponseWriter, t *yardquote.TableView, status int) {
	respond.SetETag(w, t.Row.Version)
	respond.JSON(w, status, apiYardTable(t))
}

// ListFormulaTables implements api.ServerInterface.
func (h *Handler) ListFormulaTables(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	tables, err := h.YardQuotes.ListTables(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.FormulaTable, 0, len(tables))
	for i := range tables {
		out = append(out, apiYardTable(&tables[i]))
	}
	respond.JSON(w, http.StatusOK, api.FormulaTableList{Items: out})
}

// CreateFormulaTable implements api.ServerInterface.
func (h *Handler) CreateFormulaTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateFormulaTableParams) {
	var in api.FormulaTableInput
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	ti := yardquote.TableInput{Name: in.Name, Rows: yardFormulaRows(in.Rows)}
	if in.Label != nil {
		t := yardText(*in.Label)
		ti.Label = &t
	}
	t, err := h.YardQuotes.CreateTable(r.Context(), principal(r), marinaID, ti)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardTable(w, t, http.StatusCreated)
}

// UpdateFormulaTable implements api.ServerInterface.
func (h *Handler) UpdateFormulaTable(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, tableID api.TableId, params api.UpdateFormulaTableParams) {
	var in api.FormulaTablePatch
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var patch yardquote.TablePatch
	if in.Rows != nil {
		rows := yardFormulaRows(*in.Rows)
		patch.Rows = &rows
	}
	if in.Label != nil {
		t := yardText(*in.Label)
		patch.Label = &t
	}
	t, err := h.YardQuotes.UpdateTable(r.Context(), principal(r), marinaID, tableID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardTable(w, t, http.StatusOK)
}

// ---- packages ----

func yardPackageLines(in []api.WorkPackageLine) ([]yardquote.PackageLine, error) {
	out := make([]yardquote.PackageLine, 0, len(in))
	for _, l := range in {
		tc, err := yardTestCaseOf(l.TestCase)
		if err != nil {
			return nil, err
		}
		pl := yardquote.PackageLine{
			Seq: l.Seq, ItemID: l.ItemId, WorkTypeID: str(l.WorkTypeId), QtyFormula: l.QtyFormula, PriceFormula: str(l.PriceFormula),
			Notes: str(l.Notes), TestCase: tc,
		}
		if l.Task != nil {
			pl.Task = &yardquote.PackageTask{Title: yardText(l.Task.Title)}
			if l.Task.Checklist != nil {
				pl.Task.Checklist = *l.Task.Checklist
			}
		}
		out = append(out, pl)
	}
	return out, nil
}

func apiYardPackageLines(in []yardquote.PackageLine) []api.WorkPackageLine {
	out := make([]api.WorkPackageLine, 0, len(in))
	for _, l := range in {
		al := api.WorkPackageLine{
			Seq: l.Seq, ItemId: l.ItemID, WorkTypeId: view.String(l.WorkTypeID), QtyFormula: l.QtyFormula,
			PriceFormula: view.String(l.PriceFormula), Notes: view.String(l.Notes), TestCase: apiYardTestCase(l.TestCase),
		}
		if l.Task != nil {
			al.Task = &api.WorkPackageTask{Title: apiYardText(l.Task.Title)}
			if len(l.Task.Checklist) > 0 {
				c := append([]string{}, l.Task.Checklist...)
				al.Task.Checklist = &c
			}
		}
		out = append(out, al)
	}
	return out
}

func apiYardPackage(p *yardquote.PackageView) api.WorkPackage {
	out := api.WorkPackage{
		Id: p.Row.ID, Code: p.Row.Code, Name: yardTextJSON(p.Row.Name), WorkTypeId: p.Row.WorkTypeID, Active: p.Row.Active,
		CurrentVersion: p.Row.CurrentVersion, Version: int64(p.Row.Version), CreatedAt: p.Row.CreatedAt.UTC(), UpdatedAt: p.Row.UpdatedAt.UTC(),
	}
	if c := p.Content; c != nil {
		jv := append([]string{}, c.JobVars...)
		out.Content = &api.WorkPackageVersion{
			Version: c.Version, LanguageVersion: c.LanguageVersion, Lines: apiYardPackageLines(p.Lines), JobVars: jv,
			CreatedByUserId: view.String(c.CreatedByUserID), CreatedAt: c.CreatedAt.UTC(),
		}
	}
	if p.Versions != nil {
		vs := make([]api.WorkPackageVersionBrief, 0, len(p.Versions))
		for _, v := range p.Versions {
			vs = append(vs, api.WorkPackageVersionBrief{Version: v.Version, CreatedAt: v.CreatedAt.UTC()})
		}
		out.Versions = &vs
	}
	return out
}

func writeYardPackage(w http.ResponseWriter, p *yardquote.PackageView, status int) {
	respond.SetETag(w, p.Row.Version)
	respond.JSON(w, status, apiYardPackage(p))
}

// ListWorkPackages implements api.ServerInterface.
func (h *Handler) ListWorkPackages(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListWorkPackagesParams) {
	pkgs, next, err := h.YardQuotes.ListPackages(r.Context(), principal(r), marinaID, yardquote.PackageFilter{
		Active: params.Active, WorkTypeID: str(params.WorkTypeId),
	}, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.WorkPackage, 0, len(pkgs))
	for i := range pkgs {
		out = append(out, apiYardPackage(&pkgs[i]))
	}
	respond.JSON(w, http.StatusOK, api.WorkPackagePage{Items: out, NextCursor: nextCursor(next)})
}

// CreateWorkPackage implements api.ServerInterface.
func (h *Handler) CreateWorkPackage(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateWorkPackageParams) {
	var in api.WorkPackageInput
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	lines, err := yardPackageLines(in.Lines)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	pi := yardquote.PackageInput{Code: in.Code, Name: yardText(in.Name), WorkTypeID: in.WorkTypeId, Active: in.Active, Lines: lines}
	if in.JobVars != nil {
		pi.JobVars = *in.JobVars
	}
	p, err := h.YardQuotes.CreatePackage(r.Context(), principal(r), marinaID, pi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardPackage(w, p, http.StatusCreated)
}

// GetWorkPackage implements api.ServerInterface.
func (h *Handler) GetWorkPackage(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, packageID api.PackageId, params api.GetWorkPackageParams) {
	p, err := h.YardQuotes.GetPackage(r.Context(), principal(r), marinaID, packageID, intOr(params.Version, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardPackage(w, p, http.StatusOK)
}

// SaveWorkPackageVersion implements api.ServerInterface.
func (h *Handler) SaveWorkPackageVersion(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, packageID api.PackageId, params api.SaveWorkPackageVersionParams) {
	var in api.WorkPackageVersionInput
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	lines, err := yardPackageLines(in.Lines)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	vi := yardquote.VersionInput{WorkTypeID: in.WorkTypeId, Lines: lines}
	if in.Name != nil {
		t := yardText(*in.Name)
		vi.Name = &t
	}
	if in.JobVars != nil {
		vi.JobVars = *in.JobVars
	}
	p, err := h.YardQuotes.SaveVersion(r.Context(), principal(r), marinaID, packageID, version, vi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardPackage(w, p, http.StatusCreated)
}

// SetWorkPackageActive implements api.ServerInterface.
func (h *Handler) SetWorkPackageActive(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, packageID api.PackageId, params api.SetWorkPackageActiveParams) {
	var in api.ActiveFlag
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	p, err := h.YardQuotes.SetPackageActive(r.Context(), principal(r), marinaID, packageID, version, in.Active)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardPackage(w, p, http.StatusOK)
}

// ---- formula test and settings ----

// ValidateFormula implements api.ServerInterface.
func (h *Handler) ValidateFormula(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var in api.FormulaValidateInput
	if !h.yardDecode(w, r, marinaID, &in, false) {
		return
	}
	vars, err := yardValuesOf(in.Vars)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	vi := yardquote.ValidateInput{Formula: in.Formula, VesselID: str(in.VesselId), Vars: vars, ItemID: str(in.ItemId), Qty: yardMilli(in.QtyMilli)}
	if in.Purpose != nil {
		vi.Purpose = string(*in.Purpose)
	}
	res, err := h.YardQuotes.ValidateFormula(r.Context(), principal(r), marinaID, vi)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.FormulaValidateResult{Valid: res.Valid, Issues: yardIssues(res.Issues), Idents: res.Idents, QtyMilli: yardInt64(res.Qty)}
	if res.Value != nil {
		v := apiYardValue(*res.Value)
		out.Value = &v
	}
	if res.Price != nil {
		c := int64(*res.Price)
		out.PriceCents = &c
	}
	respond.JSON(w, http.StatusOK, out)
}

func apiYardSettings(s *yardquote.SettingsView) api.YardSettings {
	tv := s.TestVessel
	atv := api.YardTestVessel{LoaCm: tv.LOACm, BeamCm: tv.BeamCm, HullType: api.YardTestVesselHullType(tv.HullType)}
	opt := func(v int64) *int64 {
		if v == 0 {
			return nil
		}
		return &v
	}
	atv.DraftCm, atv.AirDraftCm, atv.LwlCm, atv.DisplacementKg = opt(tv.DraftCm), opt(tv.AirDraftCm), opt(tv.LWLCm), opt(tv.DisplacementKg)
	atv.HullMaterial = view.String(tv.HullMaterial)
	if len(tv.EnginesKW) > 0 {
		e := append([]int64{}, tv.EnginesKW...)
		atv.EnginesKw = &e
	}
	if tv.YearBuilt > 0 {
		y := tv.YearBuilt
		atv.YearBuilt = &y
	}
	row := s.Row
	return api.YardSettings{
		TestVessel: atv, QuoteValidityDays: row.QuoteValidityDays, DepositBp: int64(row.DepositBP), AdvanceOnAccept: row.AdvanceOnAccept,
		ExternalMarkupBp: int64(row.ExternalMarkupBP), ExternalItemId: view.String(row.ExternalItemID),
		FixedIncludesExternal: row.FixedIncludesExternal, WorkersIssueMaterials: row.WorkersIssueMaterials, LiftFromSlot: row.LiftFromSlot,
		LiftToSlot: row.LiftToSlot, LiftRequestOnMove: row.LiftRequestOnMove, Version: int64(row.Version),
	}
}

func writeYardSettings(w http.ResponseWriter, s *yardquote.SettingsView) {
	respond.SetETag(w, s.Row.Version)
	respond.JSON(w, http.StatusOK, apiYardSettings(s))
}

// GetYardSettings implements api.ServerInterface.
func (h *Handler) GetYardSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	s, err := h.YardQuotes.GetSettings(r.Context(), principal(r), marinaID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardSettings(w, s)
}

// PutYardSettings implements api.ServerInterface.
func (h *Handler) PutYardSettings(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.PutYardSettingsParams) {
	var in api.YardSettingsInput
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := yardOptionalIfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	si := yardquote.SettingsInput{
		QuoteValidityDays: in.QuoteValidityDays, AdvanceOnAccept: in.AdvanceOnAccept, ExternalItemID: in.ExternalItemId,
		ClearExternalItem: respond.IsNull(fields, "external_item_id"), FixedIncludesExternal: in.FixedIncludesExternal,
		WorkersIssueMaterials: in.WorkersIssueMaterials, LiftFromSlot: in.LiftFromSlot, LiftToSlot: in.LiftToSlot,
		LiftRequestOnMove: in.LiftRequestOnMove,
	}
	toInt := func(p *int64) *int {
		if p == nil {
			return nil
		}
		v := int(*p)
		return &v
	}
	si.DepositBP, si.ExternalMarkupBP = toInt(in.DepositBp), toInt(in.ExternalMarkupBp)
	if tv := in.TestVessel; tv != nil {
		val := func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		}
		t := yardquote.TestVessel{
			LOACm: tv.LoaCm, BeamCm: tv.BeamCm, DraftCm: val(tv.DraftCm), AirDraftCm: val(tv.AirDraftCm), LWLCm: val(tv.LwlCm),
			DisplacementKg: val(tv.DisplacementKg), HullType: string(tv.HullType), HullMaterial: str(tv.HullMaterial),
		}
		if tv.EnginesKw != nil {
			t.EnginesKW = *tv.EnginesKw
		}
		if tv.YearBuilt != nil {
			t.YearBuilt = *tv.YearBuilt
		}
		si.TestVessel = &t
	}
	s, err := h.YardQuotes.PutSettings(r.Context(), principal(r), marinaID, version, si)
	if err != nil {
		yardRespondOptional(w, r, params.IfMatch, err)
		return
	}
	writeYardSettings(w, s)
}
