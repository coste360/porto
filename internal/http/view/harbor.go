package view

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/coste360/porto/internal/domain/harbor"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/http/api"
)

// Harbour mappers (m1-plan §4.3). The harbour document types carry the JSON names of the contract, so the
// large document schemas convert with a strict JSON round trip: an unknown field on either side fails, which
// TestHarborView_RoundTrip (internal/http/admin) catches when the contract drifts.

// ErrHarborShape reports a document that does not fit the harbour types.
var ErrHarborShape = errors.New("view: map document shape")

// convert copies src into dst through JSON, refusing unknown fields.
func convert(src, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return errors.Join(ErrHarborShape, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.Join(ErrHarborShape, err)
	}
	return nil
}

func mustConvert[T any](src any) T {
	var out T
	if err := convert(src, &out); err != nil {
		panic(err) // harbour output types are a subset of the contract: a failure is a programming error
	}
	return out
}

// SaveRequestIn converts a MapSaveRequest.
func SaveRequestIn(in api.MapSaveRequest) (harbor.SaveRequest, error) {
	var out harbor.SaveRequest
	err := convert(in, &out)
	return out, err
}

// MapDocument converts a map document.
func MapDocument(d *harbor.Document) api.MapDocument { return mustConvert[api.MapDocument](d) }

// MapIssues converts validation issues (MapIssue).
func MapIssues(list []layout.Issue) []api.MapIssue {
	out := make([]api.MapIssue, 0, len(list))
	for _, is := range list {
		refs := make([]string, len(is.Refs))
		for i, r := range is.Refs {
			refs[i] = string(r)
		}
		mi := api.MapIssue{Code: is.Code, Severity: api.MapIssueSeverity(is.Severity), ElementType: is.Kind, Refs: refs}
		if is.Field != "" {
			mi.Field = String(is.Field)
		}
		if len(is.Params) > 0 {
			p := is.Params
			mi.Params = &p
		}
		out = append(out, mi)
	}
	return out
}

// MapValidationReport converts a validation report.
func MapValidationReport(r layout.Report) api.MapValidationReport {
	return api.MapValidationReport{Errors: MapIssues(r.Errors), Warnings: MapIssues(r.Warnings)}
}

// MapSaveResult converts a save result.
func MapSaveResult(r *harbor.SaveResult) api.MapSaveResult {
	idMap := r.IDMap
	if idMap == nil {
		idMap = map[string]string{}
	}
	return api.MapSaveResult{Document: MapDocument(&r.Document), IdMap: idMap, Warnings: MapIssues(r.Warnings)}
}

// MapSummaries converts the map list.
func MapSummaries(list []harbor.Summary) api.MapSummaryList {
	out := api.MapSummaryList{Items: make([]api.MapSummary, 0, len(list))}
	for i := range list {
		out.Items = append(out.Items, MapSummary(&list[i]))
	}
	return out
}

// MapSummary converts a map summary.
func MapSummary(s *harbor.Summary) api.MapSummary { return mustConvert[api.MapSummary](s) }

// BerthGroup converts a group.
func BerthGroup(g *harbor.Group) api.BerthGroup {
	return api.BerthGroup{
		Id: g.ID, Code: g.Code, Name: api.I18nText{It: g.Name.It, En: g.Name.En}, Kind: api.BerthGroupKind(g.Kind), SortOrder: g.SortOrder,
		Version: g.Version, MemberCounts: api.BerthGroupMemberCounts{
			Berth: g.MemberCounts["berth"], DynamicArea: g.MemberCounts["dynamic_area"], LandResource: g.MemberCounts["land_resource"],
		},
	}
}

// HarborSettings converts the harbour settings.
func HarborSettings(s *harbor.Settings) api.HarborSettings {
	out := api.HarborSettings{
		LoaToleranceCm: s.LOAToleranceCm, DefaultGridCm: s.DefaultGridCm, Version: s.Version,
		PriceClassRule: api.PriceClassRule{
			Basis: api.PriceClassRuleBasis(s.PriceClassRule.Basis), Fallback: s.PriceClassRule.Fallback,
			Bands: make([]api.PriceBand, 0, len(s.PriceClassRule.Bands)),
		},
	}
	for _, b := range s.PriceClassRule.Bands {
		out.PriceClassRule.Bands = append(out.PriceClassRule.Bands, api.PriceBand{MaxCm: int64(b.MaxCm), Class: b.Class})
	}
	return out
}

// PriceClassRuleIn converts a price-class rule.
func PriceClassRuleIn(r api.PriceClassRule) layout.PriceClassRule {
	out := layout.PriceClassRule{Basis: string(r.Basis), Fallback: r.Fallback}
	for _, b := range r.Bands {
		out.Bands = append(out.Bands, layout.PriceBand{MaxCm: geo.Cm(b.MaxCm), Class: b.Class})
	}
	return out
}

// BerthSummary converts a berth of the list.
func BerthSummary(b *harbor.BerthSummary) api.BerthSummary {
	out := api.BerthSummary{
		Id: b.ID, MapId: b.MapID, Code: b.Code, WidthCm: b.WidthCm, LengthCm: b.LengthCm, MaxLoaCm: b.MaxLOACm, MaxBeamCm: b.MaxBeamCm,
		MooringType: api.MooringType(b.MooringType), Active: b.Active,
	}
	if b.DepthCm > 0 {
		out.DepthCm = &b.DepthCm
	}
	if b.PriceClass != "" {
		out.PriceClass = String(b.PriceClass)
	}
	if b.StructureCode != "" {
		out.StructureCode = String(b.StructureCode)
	}
	return out
}

// GeneratorSpecIn converts the generator union.
func GeneratorSpecIn(g api.GeneratorParams) (harbor.GeneratorSpec, error) {
	var out harbor.GeneratorSpec
	b, err := g.MarshalJSON()
	if err != nil || len(b) == 0 || string(b) == "null" {
		return out, nil // validated by the service after authorising
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, errors.Join(ErrHarborShape, err)
	}
	return out, nil
}

// GenerateResult converts a generator preview.
func GenerateResult(r *harbor.GenerateResult) api.GenerateResult {
	out := api.GenerateResult{Elements: mustConvert[api.MapElements](r.Elements), Counts: mustConvert[api.MapCounts](r.Counts)}
	if r.Report != nil {
		rep := MapValidationReport(*r.Report)
		out.Report = &rep
	}
	return out
}

// MapExport converts a JSON export.
func MapExport(e *harbor.Export) api.MapExport { return mustConvert[api.MapExport](e) }

// MapExportIn converts an export to import.
func MapExportIn(in api.MapExport) (harbor.Export, error) {
	var out harbor.Export
	err := convert(in, &out)
	return out, err
}

// BerthImportReport converts an import report.
func BerthImportReport(r *harbor.ImportReport) api.BerthImportReport {
	out := api.BerthImportReport{
		RowsTotal: r.RowsTotal, Valid: r.Valid, Rejected: []api.BerthImportRejected{},
		Duplicates: []api.BerthImportDuplicate{}, Planned: []api.BerthImportPlanned{},
	}
	for _, x := range r.Rejected {
		errs := make([]api.FieldError, 0, len(x.Errors))
		for _, e := range x.Errors {
			errs = append(errs, api.FieldError{Field: e.Field, Code: e.Code})
		}
		out.Rejected = append(out.Rejected, api.BerthImportRejected{Row: x.Row, Errors: errs})
	}
	for _, x := range r.Duplicates {
		out.Duplicates = append(out.Duplicates, api.BerthImportDuplicate{Row: x.Row, Code: x.Code, Reason: api.BerthImportDuplicateReason(x.Reason)})
	}
	for _, x := range r.Planned {
		out.Planned = append(out.Planned, api.BerthImportPlanned{Row: x.Row, Code: x.Code, Pier: x.Pier, Side: api.Side(x.Side)})
	}
	if r.Document != nil {
		d := MapSaveResult(r.Document)
		out.Document = &d
	}
	return out
}
