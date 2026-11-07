//go:build integration

package admin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/http/api"
)

func TestCommunicationHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	ps := ds.pricingSetup
	body := map[string]any{
		"name": "Avviso", "category": "service", "audience": map[string]any{"customer_ids": []string{ds.customer}},
		"subject": "Ciao {{customer.first_name}}", "body_md": "Il porto **{{marina.name}}** informa.",
	}
	ps.yard.expect(t, 403, "forbidden", "POST", M+"/communications", body, nil)
	ps.other.expect(t, 404, "not_found", "POST", M+"/communications", body, nil)
	c.expect(t, 422, "template_invalid", "POST", M+"/communications", map[string]any{
		"name": "x", "category": "service", "audience": map[string]any{}, "body_md": "{{customer.nope}}",
	}, nil)
	var cm api.Communication
	c.expect(t, 201, "", "POST", M+"/communications", body, &cm)
	C := M + "/communications/" + cm.Id
	c.expect(t, 200, "", "GET", C, nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/communications/"+cm.Id, nil, nil)
	c.expect(t, 428, "precondition_required", "PATCH", C, map[string]any{"name": "Nuovo"}, nil)
	c.expect(t, 200, "", "PATCH", C, map[string]any{"name": "Nuovo"}, &cm, "If-Match", etag(cm.Version))
	var pv api.CommunicationPreview
	c.expect(t, 200, "", "POST", C+"/preview", map[string]any{}, &pv)
	if pv.Recipients.Count != 1 || pv.Recipients.SkippedNoEmail+pv.Recipients.SkippedNoConsent != 1 {
		t.Fatalf("preview %+v", pv)
	}
	c.expect(t, 200, "", "POST", C+"/preview", map[string]any{"customer_id": ds.customer}, &pv)
	if pv.Sample == nil || pv.Sample.Subject != "Ciao Mario" || !strings.Contains(pv.Sample.Html, "<strong>") {
		t.Fatalf("sample %+v", pv.Sample)
	}
	c.expect(t, 400, "validation_failed", "POST", C+"/schedule", map[string]any{"scheduled_at": time.Now().Add(-time.Hour)}, nil, "If-Match", etag(cm.Version))
	c.expect(t, 200, "", "POST", C+"/schedule", map[string]any{"scheduled_at": time.Now().Add(time.Hour)}, &cm, "If-Match", etag(cm.Version))
	if cm.Status != "scheduled" {
		t.Fatalf("scheduled %+v", cm)
	}
	c.expect(t, 200, "", "POST", C+"/send", nil, &cm, "If-Match", etag(cm.Version))
	c.expect(t, 412, "version_conflict", "POST", C+"/cancel", nil, nil, "If-Match", etag(cm.Version+1))
	c.expect(t, 200, "", "POST", C+"/cancel", nil, &cm, "If-Match", etag(cm.Version))
	c.expect(t, 409, "conflict", "POST", C+"/send", nil, nil, "If-Match", etag(cm.Version))
	var page api.CommunicationPage
	c.expect(t, 200, "", "GET", M+"/communications?status=cancelled", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("list %+v", page.Items)
	}
	csv := c.expect(t, 200, "", "GET", C+"/recipients?format=csv", nil, nil)
	if !strings.HasPrefix(string(csv.body), "customer_id,display_name,status") {
		t.Fatalf("csv %s", csv.body)
	}
	c.expect(t, 200, "", "GET", C+"/recipients", nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/communications/"+cm.Id+"/recipients", nil, nil)
	var msgs api.CustomerMessagePage
	c.expect(t, 200, "", "GET", M+"/customers/"+ds.customer+"/messages", nil, &msgs)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/customers/"+ds.customer+"/messages", nil, nil)
}
