//go:build integration

package admin_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: documents composed through the domain transaction API
)

func TestGeneratedDocumentHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	a := ds.s.app
	ctx := context.Background()
	scope, err := store.SystemMarinaScope(ctx, a.DB.DB, ds.marinaID)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := a.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		id, err = a.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{Kind: docs.KindLetter, SubjectType: docs.SubjectLetter, SubjectID: ds.customer})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	D := M + "/generated-documents/" + id
	var d api.GeneratedDocument
	c.expect(t, 200, "", "GET", D, nil, &d)
	if d.Status != "pending" && d.Status != "ready" {
		t.Fatalf("status %s", d.Status)
	}
	if err := a.Docs.RenderPending(ctx, id); err != nil {
		t.Fatal(err)
	}
	c.expect(t, 200, "", "GET", D, nil, &d)
	if d.Status != "ready" || d.Sha256 == nil || d.TemplateVersion == nil || d.TemplateVersion.BuiltinRef == nil {
		t.Fatalf("ready %+v", d)
	}
	ps := ds.pricingSetup
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/generated-documents/"+id, nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", D, nil, nil)
	var page api.GeneratedDocumentPage
	c.expect(t, 200, "", "GET", M+"/generated-documents?subject_type=letter&subject_id="+ds.customer, nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("page %+v", page.Items)
	}
	var link api.FileLink
	c.expect(t, 200, "", "GET", D+"/link", nil, &link)
	file := c.expect(t, 200, "", "GET", strings.TrimPrefix(link.Url, ""), nil, nil)
	if !bytes.HasPrefix(file.body, []byte("%PDF-")) || file.header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("download %q", file.header)
	}
	ds.yard.expect(t, 403, "forbidden", "GET", D+"/link", nil, nil)
	c.expect(t, 403, "download_link_expired", "GET", link.Url+"x", nil, nil)
	c.expect(t, 409, "conflict", "POST", D+"/render", nil, nil)
	ps.other.expect(t, 404, "not_found", "POST", ps.otherM+"/generated-documents/"+id+"/render", nil, nil)

	// Map print.
	var maps api.MapSummaryList
	c.expect(t, 200, "", "GET", M+"/maps", nil, &maps)
	if len(maps.Items) == 0 {
		t.Fatal("no map")
	}
	pdf := c.expect(t, 200, "", "GET", M+"/maps/"+maps.Items[0].Id+"/print?lens=vessel_type&date=2027-03-01", nil, nil)
	if !bytes.HasPrefix(pdf.body, []byte("%PDF-")) {
		t.Fatal("map print is not a PDF")
	}
	c.expect(t, 400, "validation_failed", "GET", M+"/maps/"+maps.Items[0].Id+"/print?lens=nope", nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", M+"/maps/"+maps.Items[0].Id+"/print", nil, nil)
}
