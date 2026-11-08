//go:build integration

package admin_test

import (
	"testing"
)

// TestReportStubs_Authorise (M7 CONTRACT): every report operation, the dashboard and the comparison authorise
// before anything else: another marina 404, dock (no report permission) 403, owner 200.
func TestReportStubs_Authorise(t *testing.T) {
	ps := newPricingSetup(t)
	paths := []string{
		"/dashboard", "/reports/comparison?indicator=production", "/reports/occupancy", "/reports/efficiency",
		"/reports/allocation-waste", "/reports/lost-demand", "/reports/customers", "/reports/production",
		"/reports/accruals", "/reports/charges", "/reports/payments", "/reports/revenue-classes",
		"/reports/receivables", "/reports/presences", "/reports/rights", "/reports/cash", "/reports/vessels",
		"/reports/berth-revenue",
	}
	for _, p := range paths {
		ps.other.expect(t, 404, "not_found", "GET", ps.M+p, nil, nil)
		ps.dock.expect(t, 403, "forbidden", "GET", ps.M+p, nil, nil)
		ps.owner.expect(t, 200, "", "GET", ps.M+p, nil, nil)
	}
}
