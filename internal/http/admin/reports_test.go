//go:build integration

package admin_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/xlsx"
)

// reportKeys are the report routes of M7 served by REPORTS (comparison is DASHBOARD's).
var reportKeys = []string{
	"occupancy", "efficiency", "allocation-waste", "lost-demand", "customers", "production", "accruals", "charges",
	"payments", "revenue-classes", "receivables", "presences", "rights", "cash", "vessels", "berth-revenue",
}

const utf8BOM = "\xef\xbb\xbf"

// skipUntilEngines skips while the report service is still the CONTRACT stub (501): the handlers are complete,
// the service and ParseParams belong to ENGINES.
func skipUntilEngines(t *testing.T, r harborResp) {
	t.Helper()
	if r.status == 501 && r.problem() == "not_implemented" {
		t.Skip("report service not implemented yet (ENGINES)")
	}
}

// TestReportsAPI_Formats (m7-plan §7, §11.2): every report answers JSON, Italian CSV and Excel from the same
// request; content types, attachment names and the BOM; Accept: text/csv; parse errors after authorisation.
func TestReportsAPI_Formats(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	period := "from=2026-01-01&to=2026-03-31"

	skipUntilEngines(t, c.do(t, "GET", M+"/reports/charges?"+period, nil))

	for _, key := range reportKeys {
		base := M + "/reports/" + key + "?" + period

		// JSON: the envelope with the never-null arrays.
		var env struct {
			Report  string `json:"report"`
			Period  struct{ From, To string }
			Records []map[string]any `json:"records"`
			Groups  []map[string]any `json:"groups"`
			Filters map[string]any   `json:"filters"`
			Page    struct {
				TotalRecords *int64 `json:"total_records"`
			} `json:"page"`
		}
		r := c.expect(t, 200, "", "GET", base, nil, &env)
		if ct := r.header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: json content type %q", key, ct)
		}
		if env.Report != key || env.Period.From != "2026-01-01" || env.Period.To != "2026-03-31" ||
			env.Records == nil || env.Groups == nil || env.Filters == nil || env.Page.TotalRecords == nil {
			t.Errorf("%s: envelope %s", key, r.body)
		}

		// CSV: BOM, ';' header, CRLF, attachment name, Totale as last row.
		r = c.expect(t, 200, "", "GET", base+"&format=csv", nil, nil)
		if ct := r.header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
			t.Errorf("%s: csv content type %q", key, ct)
		}
		wantName := `attachment; filename="porto-` + key + `-2026-01-01-2026-03-31.csv"`
		if cd := r.header.Get("Content-Disposition"); cd != wantName {
			t.Errorf("%s: disposition %q, want %q", key, cd, wantName)
		}
		body := string(r.body)
		if !strings.HasPrefix(body, utf8BOM) || !strings.Contains(body, ";") || !strings.HasSuffix(body, "\r\n") {
			t.Errorf("%s: csv %q", key, body)
		}
		lines := strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n")
		if last := lines[len(lines)-1]; !strings.HasPrefix(last, "Totale;") {
			t.Errorf("%s: last csv line %q", key, last)
		}

		// Excel: a workbook whose first sheet has the CSV header.
		r = c.expect(t, 200, "", "GET", base+"&format=xlsx", nil, nil)
		if ct := r.header.Get("Content-Type"); ct != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
			t.Errorf("%s: xlsx content type %q", key, ct)
		}
		if cd := r.header.Get("Content-Disposition"); !strings.HasSuffix(cd, `.xlsx"`) {
			t.Errorf("%s: xlsx disposition %q", key, cd)
		}
		rows, err := xlsx.ReadFirstSheet(bytes.NewReader(r.body), int64(len(r.body)), xlsx.DefaultLimits)
		if err != nil {
			t.Fatalf("%s: xlsx: %v", key, err)
		}
		if got, want := strings.Join(rows[0], ";"), strings.TrimPrefix(lines[0], utf8BOM); got != want {
			t.Errorf("%s: xlsx header %q, csv header %q", key, got, want)
		}
		if len(rows) != len(lines) {
			t.Errorf("%s: xlsx rows %d, csv lines %d", key, len(rows), len(lines))
		}
	}

	// Accept: text/csv without format, English labels with Italian formats.
	r := c.expect(t, 200, "", "GET", M+"/reports/charges?"+period+"&lang=en", nil, nil, "Accept", "text/csv")
	if !strings.HasPrefix(string(r.body), utf8BOM+"Issued on;Number;") {
		t.Errorf("english csv %q", r.body)
	}
	// An explicit format wins over Accept.
	r = c.expect(t, 200, "", "GET", M+"/reports/charges?"+period+"&format=json", nil, nil, "Accept", "text/csv")
	if !json.Valid(r.body) {
		t.Errorf("format=json must answer JSON: %q", r.body)
	}

	// Errors: authorisation first, then validation.
	bad := M + "/reports/charges?from=2026-03-01&to=2026-01-01"
	ps.other.expect(t, 404, "not_found", "GET", bad, nil, nil)
	ps.dock.expect(t, 403, "forbidden", "GET", bad, nil, nil)
	c.expect(t, 422, "invalid_period", "GET", bad, nil, nil)
	c.expect(t, 422, "invalid_period", "GET", M+"/reports/charges?from=2025-01-01&to=2026-03-31", nil, nil)
	c.expect(t, 422, "invalid_report_filter", "GET", M+"/reports/charges?"+period+"&group_by=nope", nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", M+"/reports/charges?"+period+"&format=csv", nil, nil)
	ps.dock.expect(t, 403, "forbidden", "GET", M+"/reports/occupancy?"+period+"&format=xlsx", nil, nil)
}
