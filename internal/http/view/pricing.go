package view

import (
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/http/api"
)

// Pricing mappers (m2-plan §7.4). The catalogue, price list and engine types carry the JSON names of the
// contract, so they convert with the strict JSON round trip of harbor.go; nothing is recomputed here.
// TestPricingView_RoundTrip (internal/http/admin) catches drift between the contract and the domain types.

// PricingSettings converts the pricing settings.
func PricingSettings(s *catalog.Settings) api.PricingSettings {
	return mustConvert[api.PricingSettings](s)
}

// RevenueClass converts a revenue class.
func RevenueClass(c *catalog.RevenueClass) api.RevenueClass { return mustConvert[api.RevenueClass](c) }

// RevenueClasses converts a list of revenue classes.
func RevenueClasses(list []catalog.RevenueClass) api.RevenueClassList {
	out := api.RevenueClassList{Items: make([]api.RevenueClass, 0, len(list))}
	for i := range list {
		out.Items = append(out.Items, RevenueClass(&list[i]))
	}
	return out
}

// Item converts an item.
func Item(it *catalog.Item) api.Item { return mustConvert[api.Item](it) }

// ItemPage converts a page of items.
func ItemPage(items []catalog.Item, next string) api.ItemPage {
	out := api.ItemPage{Items: make([]api.Item, 0, len(items)), NextCursor: String(next)}
	for i := range items {
		out.Items = append(out.Items, Item(&items[i]))
	}
	return out
}

// BerthCategory converts a berth category.
func BerthCategory(c *catalog.BerthCategory) api.BerthCategory {
	return mustConvert[api.BerthCategory](c)
}

// BerthCategories converts the category list.
func BerthCategories(l *catalog.CategoryList) api.BerthCategoryList {
	return mustConvert[api.BerthCategoryList](l)
}

// CustomerCategoryCounts converts the customer category counts.
func CustomerCategoryCounts(c *catalog.CustomerCategoryCounts) api.CustomerCategoryCountList {
	return mustConvert[api.CustomerCategoryCountList](c)
}

// BerthCategorySync converts the categories created by a sync.
func BerthCategorySync(list []catalog.BerthCategory) api.BerthCategorySyncResult {
	out := api.BerthCategorySyncResult{Created: make([]api.BerthCategory, 0, len(list))}
	for i := range list {
		out.Created = append(out.Created, BerthCategory(&list[i]))
	}
	return out
}

// PriceListSaveIn converts a save request; a shape the domain does not accept (e.g. an invalid date) is an
// error.
func PriceListSaveIn(in api.PriceListSave) (pricelist.SaveRequest, error) {
	var out pricelist.SaveRequest
	err := convert(in, &out)
	return out, err
}

// PriceListDocument converts a document.
func PriceListDocument(d *pricelist.Document) api.PriceListDocument {
	return mustConvert[api.PriceListDocument](d)
}

// PriceListSummaries converts the list of price lists.
func PriceListSummaries(list []pricelist.Summary) api.PriceListSummaryList {
	out := api.PriceListSummaryList{Items: make([]api.PriceListSummary, 0, len(list))}
	for i := range list {
		out.Items = append(out.Items, mustConvert[api.PriceListSummary](&list[i]))
	}
	return out
}

// PriceListSaveResult converts the answer to a save.
func PriceListSaveResult(r *pricelist.SaveResult) api.PriceListSaveResult {
	idMap := r.IDMap
	if idMap == nil {
		idMap = map[string]string{}
	}
	return api.PriceListSaveResult{Document: PriceListDocument(r.Document), IdMap: idMap, Issues: PriceListIssues(r.Issues)}
}

// PriceListIssues converts validation issues.
func PriceListIssues(list []pricing.Issue) []api.PriceListIssue {
	out := make([]api.PriceListIssue, 0, len(list))
	for i := range list {
		out = append(out, mustConvert[api.PriceListIssue](&list[i]))
	}
	return out
}

// PriceListActivateResult converts the answer to an activation.
func PriceListActivateResult(r *pricelist.ActivateResult) api.PriceListActivateResult {
	return mustConvert[api.PriceListActivateResult](r)
}

// RenewalRequestIn converts a renewal request.
func RenewalRequestIn(in api.RenewalRequest) (pricelist.RenewalRequest, error) {
	var out pricelist.RenewalRequest
	err := convert(in, &out)
	return out, err
}

// RenewalPreview converts a renewal preview.
func RenewalPreview(p *pricelist.RenewalPreview) api.RenewalPreview {
	return mustConvert[api.RenewalPreview](p)
}

// SimulateRequestIn converts a simulate request.
func SimulateRequestIn(in api.PricingSimulateRequest) (pricelist.QuoteRequest, error) {
	var out pricelist.QuoteRequest
	err := convert(in, &out)
	return out, err
}

// SimulateRequest converts a quote request (a matrix probe) to the wire form.
func SimulateRequest(q *pricelist.QuoteRequest) api.PricingSimulateRequest {
	return mustConvert[api.PricingSimulateRequest](q)
}

// PriceBreakdown converts an engine breakdown and what the quote resolved. Absent lists become empty arrays.
func PriceBreakdown(b pricing.Breakdown, res *pricelist.Resolved) api.PriceBreakdown {
	b = normalizeBreakdown(b)
	out := mustConvert[api.PriceBreakdown](b)
	if res != nil {
		r := mustConvert[api.PriceResolved](res)
		out.Resolved = &r
	}
	return out
}

// normalizeBreakdown replaces nil slices with empty ones (the contract marks them required arrays).
func normalizeBreakdown(b pricing.Breakdown) pricing.Breakdown {
	if b.Lines == nil {
		b.Lines = []pricing.Line{}
	}
	lines := make([]pricing.Line, len(b.Lines))
	for i, l := range b.Lines {
		if l.Blocks == nil {
			l.Blocks = []pricing.Block{}
		}
		blocks := make([]pricing.Block, len(l.Blocks))
		for j, bl := range l.Blocks {
			if bl.Modifiers == nil {
				bl.Modifiers = []pricing.Applied{}
			}
			blocks[j] = bl
		}
		l.Blocks = blocks
		if l.Discounts == nil {
			l.Discounts = []pricing.Applied{}
		}
		lines[i] = l
	}
	b.Lines = lines
	if b.VATSummary == nil {
		b.VATSummary = []pricing.VATBucket{}
	}
	if b.Trace == nil {
		b.Trace = []pricing.TraceStep{}
	}
	if b.Warnings == nil {
		b.Warnings = []string{}
	}
	return b
}

// PriceMatrix converts a matrix.
func PriceMatrix(m *pricelist.Matrix) api.PriceMatrix { return mustConvert[api.PriceMatrix](m) }

// PriceCoverage converts a coverage report.
func PriceCoverage(c *pricelist.Coverage) api.PriceCoverage { return mustConvert[api.PriceCoverage](c) }
