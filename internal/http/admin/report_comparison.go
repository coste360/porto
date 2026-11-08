package admin

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// Period comparison of M7 (spec 21.3 "Comparazione delle statistiche", m7-plan §5): JSON, or CSV/XLSX with one row
// per bucket of the current period (previous values aligned by date or weekday by the service) and a last
// "Totale" row with the period values. Formats are always Italian; labels follow the request language.

// GetReportComparison implements api.ServerInterface (GET .../reports/comparison).
func (h *Handler) GetReportComparison(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetReportComparisonParams) {
	start := h.dashNow()
	p := principal(r)
	q, err := report.ParseComparisonParams(r.URL.Query())
	if err != nil {
		h.dashParseError(w, r, marinaID, err)
		return
	}
	if q.Format == "" && strings.Contains(r.Header.Get("Accept"), "text/csv") {
		q.Format = report.FormatCSV
	}
	c, err := h.Reports.Compare(r.Context(), p, marinaID, q)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.Logger(r.Context()).Info("report served",
		"report", "comparison", "period_days", dashPeriodDays(c.Current.From, c.Current.To),
		"records", len(c.Series), "format", string(q.Format), "duration_ms", h.dashSince(start))
	switch q.Format {
	case report.FormatCSV, report.FormatXLSX:
		lang := q.Lang
		if lang == "" && p != nil {
			lang = p.Locale
		}
		b, ext, ctype, err := comparisonExport(c, lang, q.Format)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeDownload(w, export.Comparison(c, lang).Filename(ext), ctype, b)
	default:
		respond.JSON(w, http.StatusOK, comparisonOf(c))
	}
}

// comparisonExport writes the comparison as CSV or XLSX through the export package, from the same table the
// INV-63 tests check against the JSON series.
func comparisonExport(c *calc.Comparison, lang string, f report.Format) (b []byte, ext, ctype string, err error) {
	t := export.Comparison(c, lang)
	var buf bytes.Buffer
	ext, ctype = "csv", reportCSVType
	if f == report.FormatXLSX {
		ext, ctype = "xlsx", reportXLSXType
		err = export.WriteXLSX(&buf, t)
	} else {
		err = export.WriteCSV(&buf, t)
	}
	if err != nil {
		return nil, "", "", err
	}
	return buf.Bytes(), ext, ctype, nil
}

func comparisonOf(c *calc.Comparison) api.ComparisonResult {
	out := api.ComparisonResult{
		Indicator:   api.ComparisonIndicator(c.Indicator),
		Unit:        api.ReportUnit(c.Unit),
		Align:       api.ReportAlign(c.Align),
		Granularity: api.ReportGranularity(c.Granularity),
		YearsBack:   c.YearsBack,
		Filters:     dashMap(c.Filters),
		Current:     comparisonPeriodOf(c.Current),
		Previous:    comparisonPeriodOf(c.Previous),
		Delta:       c.Delta.Ptr(),
		DeltaBp:     c.DeltaBP.Ptr(),
		Series:      make([]api.ComparisonPoint, 0, len(c.Series)),
	}
	for _, s := range c.Series {
		pt := api.ComparisonPoint{Bucket: dashDate(s.Bucket), Current: s.Current.Ptr(), Previous: s.Previous.Ptr()}
		if s.HasFraction {
			pt.CurrentNumerator, pt.CurrentDenominator = dashInt(s.CurrentNumerator), dashInt(s.CurrentDenominator)
			pt.PreviousNumerator, pt.PreviousDenominator = dashInt(s.PreviousNumerator), dashInt(s.PreviousDenominator)
		}
		out.Series = append(out.Series, pt)
	}
	return out
}

func comparisonPeriodOf(v calc.ComparisonPeriod) api.ComparisonPeriodValue {
	out := api.ComparisonPeriodValue{From: dashDate(v.From), To: dashDate(v.To), Value: v.Value.Ptr()}
	if v.HasFraction {
		out.Numerator, out.Denominator = dashInt(v.Numerator), dashInt(v.Denominator)
	}
	return out
}
