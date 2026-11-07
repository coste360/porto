//go:build integration

package admin_test

import (
	"bytes"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

func TestTemplateHandlers_Scenario(t *testing.T) {
	ps := newPricingSetup(t)
	c, M := ps.owner, ps.M
	var kinds api.TemplateKindList
	c.expect(t, 200, "", "GET", M+"/template-kinds", nil, &kinds)
	if len(kinds.Items) < 20 || len(kinds.Items[0].Fields) == 0 {
		t.Fatalf("kinds %d", len(kinds.Items))
	}
	ps.other.expect(t, 404, "not_found", "GET", M+"/template-kinds", nil, nil)

	body := map[string]any{"kind": "contract_annual", "code": "annual", "name": "Annuale", "is_default": true}
	ps.dock.expect(t, 403, "forbidden", "POST", M+"/templates", body, nil)
	ps.other.expect(t, 404, "not_found", "POST", M+"/templates", body, nil)
	var tp api.Template
	r := c.expect(t, 201, "", "POST", M+"/templates", body, &tp)
	if r.header.Get("ETag") != etag(tp.Version) || !tp.IsDefault {
		t.Fatalf("template %+v", tp)
	}
	c.expect(t, 409, "code_taken", "POST", M+"/templates", body, nil)
	c.expect(t, 400, "validation_failed", "POST", M+"/templates", map[string]any{"kind": "yard_work_order", "code": "r", "name": "R"}, nil)
	T := M + "/templates/" + tp.Id
	c.expect(t, 200, "", "GET", T, nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/templates/"+tp.Id, nil, nil)
	c.expect(t, 428, "precondition_required", "PATCH", T, map[string]any{"name": "Nuovo"}, nil)
	c.expect(t, 412, "version_conflict", "PATCH", T, map[string]any{"name": "Nuovo"}, nil, "If-Match", etag(tp.Version+5))
	c.expect(t, 200, "", "PATCH", T, map[string]any{"name": "Nuovo"}, &tp, "If-Match", etag(tp.Version))

	bad := c.expect(t, 422, "template_invalid", "POST", T+"/versions", map[string]any{"locale": "it", "body_md": "riga\n{{customer.nope}}"}, nil)
	if !bytes.Contains(bad.body, []byte(`"line":2`)) || !bytes.Contains(bad.body, []byte(`"code":"unknown_path"`)) {
		t.Fatalf("issues %s", bad.body)
	}
	var v api.TemplateVersion
	c.expect(t, 201, "", "POST", T+"/versions", map[string]any{"locale": "it", "body_md": "# {{contract.code}}\n\n{{signature \"customer\"}}", "page": map[string]int{"margins_mm": 15}}, &v)
	if v.Status != "draft" || v.Version != 1 || v.Page.MarginsMm == nil {
		t.Fatalf("version %+v", v)
	}
	pdf := c.expect(t, 200, "", "POST", T+"/preview", map[string]any{"locale": "it", "version": 1, "sample": "demo"}, nil)
	if !bytes.HasPrefix(pdf.body, []byte("%PDF-")) || pdf.header.Get("X-Porto-Pages") != "1" {
		t.Fatalf("preview %q", pdf.header)
	}
	c.expect(t, 422, "template_invalid", "POST", T+"/preview", map[string]any{"locale": "it", "body_md": "{{#each customer}}", "sample": "demo"}, nil)
	ps.other.expect(t, 404, "not_found", "POST", ps.otherM+"/templates/"+tp.Id+"/preview", map[string]any{"locale": "it", "sample": "demo"}, nil)
	c.expect(t, 200, "", "POST", T+"/versions/1/publish", map[string]any{"locale": "it"}, &v)
	if v.Status != "published" || v.Sha256 == nil {
		t.Fatalf("published %+v", v)
	}
	c.expect(t, 409, "conflict", "POST", T+"/versions/1/publish", map[string]any{"locale": "it"}, nil)
	c.expect(t, 404, "not_found", "POST", T+"/versions/9/publish", map[string]any{"locale": "it"}, nil)
	c.expect(t, 400, "validation_failed", "POST", T+"/versions/x/publish", map[string]any{"locale": "it"}, nil)
	var vs api.TemplateVersionList
	c.expect(t, 200, "", "GET", T+"/versions?locale=it", nil, &vs)
	if len(vs.Items) != 1 {
		t.Fatalf("versions %+v", vs.Items)
	}
	var list api.TemplateList
	c.expect(t, 200, "", "GET", M+"/templates?kind=contract_annual", nil, &list)
	if len(list.Items) != 1 || len(list.Items[0].Locales) != 1 {
		t.Fatalf("list %+v", list.Items)
	}
}
