//go:build integration

package admin_test

import (
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestRenewalHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	ct := ds.activeAnnual(t, ds.berths[0], "2027-06-02", "2028-06-01")
	body := map[string]any{"name": "Rinnovi 2028", "filter": map[string]any{"contract_ids": []string{ct.Id}}, "deadline_on": "2027-05-15"}
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/renewal-campaigns", body, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/renewal-campaigns", map[string]any{"name": "", "filter": map[string]any{}, "deadline_on": "2020-01-01"}, nil)
	var camp api.RenewalCampaign
	c.expect(t, 201, "", "POST", M+"/renewal-campaigns", body, &camp)
	R := M + "/renewal-campaigns/" + camp.Id
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/renewal-campaigns/"+camp.Id, nil, nil)
	var prev api.RenewalCampaignPreview
	c.expect(t, 200, "", "POST", R+"/preview", nil, &prev, "If-Match", etag(camp.Version))
	// No 2028 list: the contract is excluded with reason no_price (or insurance, checked first).
	if len(prev.Proposals.Items) != 1 || prev.Proposals.Items[0].Status != "excluded" || prev.Proposals.Items[0].ExclusionReason == nil {
		t.Fatalf("preview %+v", prev.Proposals.Items)
	}
	pr := prev.Proposals.Items[0]
	// The campaign table shows the customer and the boat of each proposal.
	if pr.Customer == nil || pr.Customer.DisplayName == nil || *pr.Customer.DisplayName == "" || pr.Vessel == nil || pr.Vessel.Id != ds.vessel || pr.Vessel.Name == "" {
		t.Fatalf("preview proposal without customer or vessel: %+v %+v", pr.Customer, pr.Vessel)
	}
	var one api.RenewalProposal
	c.expect(t, 200, "", "GET", M+"/renewal-proposals/"+pr.Id, nil, &one)
	if one.Customer == nil || one.Customer.DisplayName == nil || one.Vessel == nil || one.Vessel.Id != ds.vessel {
		t.Fatalf("proposal without customer or vessel: %+v %+v", one.Customer, one.Vessel)
	}
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/renewal-proposals/"+pr.Id, nil, nil)
	c.expect(t, 409, "invalid_transition", "POST", M+"/renewal-proposals/"+pr.Id+"/accept", map[string]any{}, nil, "If-Match", etag(pr.Version))
	c.expect(t, 200, "", "GET", R+"/proposals?format=csv", nil, nil)
	var page api.RenewalCampaignPage
	c.expect(t, 200, "", "GET", M+"/renewal-campaigns", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("campaigns %+v", page)
	}
	c.expect(t, 200, "", "POST", R+"/discard", nil, &camp, "If-Match", etag(prev.Campaign.Version))
	if camp.Status != "discarded" {
		t.Fatalf("discarded %s", camp.Status)
	}
	c.expect(t, 409, "invalid_transition", "POST", R+"/launch", nil, nil, "If-Match", etag(camp.Version))
}
