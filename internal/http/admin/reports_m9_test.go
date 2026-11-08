//go:build integration

package admin_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/xlsx"
)

// TestReportsAPI_M9 (m9-plan §11): the storage, maintenance and utilities reports answer JSON, CSV and Excel from
// the same request; authorisation before validation (another marina 404, dock 403); the report-specific filter
// values are validated (422 invalid_report_filter).
func TestReportsAPI_M9(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	period := "from=2026-01-01&to=2026-03-31"
	for _, key := range []string{"storage", "maintenance", "utilities"} {
		base := M + "/reports/" + key + "?" + period
		var env struct {
			Report  string           `json:"report"`
			Records []map[string]any `json:"records"`
			Groups  []map[string]any `json:"groups"`
			Totals  map[string]any   `json:"totals"`
		}
		r := c.expect(t, 200, "", "GET", base, nil, &env)
		if env.Report != key || env.Records == nil || env.Groups == nil || env.Totals == nil {
			t.Errorf("%s: envelope %s", key, r.body)
		}
		r = c.expect(t, 200, "", "GET", base+"&format=csv", nil, nil)
		body := string(r.body)
		lines := strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n")
		if !strings.HasPrefix(body, utf8BOM) || !strings.HasPrefix(lines[len(lines)-1], "Totale;") {
			t.Errorf("%s: csv %q", key, body)
		}
		r = c.expect(t, 200, "", "GET", base+"&format=xlsx", nil, nil)
		rows, err := xlsx.ReadFirstSheet(bytes.NewReader(r.body), int64(len(r.body)), xlsx.DefaultLimits)
		if err != nil || len(rows) != len(lines) {
			t.Errorf("%s: xlsx %d rows, csv %d lines (%v)", key, len(rows), len(lines), err)
		}
		ps.other.expect(t, 404, "not_found", "GET", base, nil, nil)
		ps.dock.expect(t, 403, "forbidden", "GET", base, nil, nil)
	}
	for _, q := range []string{
		"/reports/storage?group_by=resource",
		"/reports/maintenance?status=pending", "/reports/maintenance?element_type=boat",
		"/reports/utilities?kind=mooring", "/reports/utilities?status=open",
	} {
		c.expect(t, 422, "invalid_report_filter", "GET", M+q, nil, nil)
		ps.other.expect(t, 404, "not_found", "GET", M+q, nil, nil)
	}
	// A boolean is bound by the generated wrapper before the handler (400).
	c.expect(t, 400, "bad_request", "GET", M+"/reports/storage?exits_blocked_only=maybe", nil, nil)
	// Grouped storage report: every yard and shed area is a group, also without boats.
	var grouped struct {
		Groups []struct {
			Key    string         `json:"key"`
			Totals map[string]any `json:"totals"`
		} `json:"groups"`
	}
	c.expect(t, 200, "", "GET", M+"/reports/storage?"+period+"&group_by=map_kind", nil, &grouped)
	for _, g := range grouped.Groups {
		if g.Key != "yard" && g.Key != "shed" {
			t.Errorf("storage group %q", g.Key)
		}
	}
}
