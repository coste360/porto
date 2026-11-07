//go:build integration

package admin_test

import (
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestImportHandlers_Scenario(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	body := map[string]any{
		"entity": "customers_vessels", "csv": "cognome;nome;barca;loa;beam\nRossi;Mario;Libeccio;1134;380\n", "delimiter": "semicolon",
		"has_header": true, "mapping": map[string]int{"last_name": 0, "first_name": 1, "vessel_name": 2, "vessel_loa_cm": 3, "vessel_beam_cm": 4},
		"dry_run": true,
	}
	ps.other.expect(t, 404, "not_found", "POST", M+"/imports", body, nil)
	ps.yard.expect(t, 403, "forbidden", "POST", M+"/imports", body, nil)
	var rep api.ImportReport
	c.expect(t, 200, "", "POST", M+"/imports", body, &rep)
	if rep.Rows != 1 || rep.Valid != 1 || !rep.DryRun {
		t.Fatalf("dry run %+v", rep)
	}
	c.expect(t, 400, "csv_invalid", "POST", M+"/imports", map[string]any{
		"entity": "customers_vessels", "csv": "a;b\n1;2\n", "has_header": true, "mapping": map[string]int{"last_name": 9}, "dry_run": true,
	}, nil)
	body["dry_run"] = false
	c.expect(t, 200, "", "POST", M+"/imports", body, &rep)
	if rep.Created == nil || (*rep.Created)["vessel"] != 1 {
		t.Fatalf("apply %+v", rep)
	}
	var run api.ImportRun
	c.expect(t, 200, "", "GET", M+"/imports/"+*rep.RunId, nil, &run)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/imports/"+*rep.RunId, nil, nil)
	var page api.ImportRunPage
	c.expect(t, 200, "", "GET", M+"/imports?entity=customers_vessels", nil, &page)
	if len(page.Items) != 2 {
		t.Fatalf("runs %+v", page.Items)
	}
	c.expect(t, 428, "precondition_required", "POST", M+"/imports/"+*rep.RunId+"/undo", nil, nil)
	c.expect(t, 200, "", "POST", M+"/imports/"+*rep.RunId+"/undo", nil, &run, "If-Match", etag(run.Version))
	if run.Status != "undone" {
		t.Fatalf("undo %+v", run)
	}
	c.expect(t, 409, "import_not_undoable", "POST", M+"/imports/"+*rep.RunId+"/undo", nil, nil, "If-Match", etag(run.Version))

	// Deadlines: the engine on demand and the policy channels.
	var sync api.DeadlineSyncResult
	c.expect(t, 200, "", "POST", M+"/deadlines/sync", nil, &sync)
	ps.dock.expect(t, 403, "forbidden", "POST", M+"/deadlines/sync", nil, nil)
	ps.other.expect(t, 404, "not_found", "POST", M+"/deadlines/sync", nil, nil)
	var pols api.DeadlinePolicyList
	c.expect(t, 200, "", "PUT", M+"/deadline-policies", map[string]any{"items": []map[string]any{
		{"kind": "contract_end", "lead_days": []int{60}, "channels": []string{"panel", "customer_email"}},
	}}, &pols)
	for _, p := range pols.Items {
		if p.Kind == "contract_end" && (p.Channels == nil || len(*p.Channels) != 2) {
			t.Fatalf("channels %+v", p)
		}
	}
}
