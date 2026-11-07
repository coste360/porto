//go:build integration

package admin_test

import (
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestContractSettingsHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	var s api.ContractSettings
	r := c.expect(t, 200, "", "GET", M+"/contract-settings", nil, &s)
	if s.SignatureDriver != "paper" || !s.RequirePaperScan || r.header.Get("ETag") != etag(s.Version) {
		t.Fatalf("defaults %+v", s)
	}
	body := map[string]any{
		"signature_driver": "otp", "insurance_policy": "block", "require_paper_scan": false,
		"bank_details": map[string]any{"iban": "IT60 X054 2811 1010 0000 0123 456", "holder": "Marina"},
		"default_policy": map[string]any{"payment": map[string]any{
			"deposit": map[string]any{"bp": 3000}, "deposit_due_days": 7, "issue_days_before_due": 15,
			"schedule": map[string]any{"mode": "interval", "every_months": 3, "count": 3},
		}},
	}
	c.expect(t, 428, "precondition_required", "PUT", M+"/contract-settings", body, nil)
	c.expect(t, 412, "version_conflict", "PUT", M+"/contract-settings", body, nil, "If-Match", etag(s.Version+5))
	ds.dock.expect(t, 403, "forbidden", "PUT", M+"/contract-settings", body, nil, "If-Match", etag(s.Version))
	ds.other.expect(t, 404, "not_found", "GET", M+"/contract-settings", nil, nil)
	bad := map[string]any{"signature_driver": "telepathy"}
	c.expect(t, 400, "validation_failed", "PUT", M+"/contract-settings", bad, nil, "If-Match", etag(s.Version))
	c.expect(t, 200, "", "PUT", M+"/contract-settings", body, &s, "If-Match", etag(s.Version))
	if s.SignatureDriver != "otp" || s.InsurancePolicy != "block" || s.BankDetails == nil || s.BankDetails.Iban == nil ||
		*s.BankDetails.Iban != "IT60X0542811101000000123456" || s.DefaultPolicy.Payment == nil {
		t.Fatalf("updated %+v", s)
	}
}
