//go:build integration

package admin_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/store" //nolint:depguard // test setup: documents composed through the domain transaction API
)

func TestSignatureHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	a := ds.s.app
	ctx := context.Background()
	scope, err := store.SystemMarinaScope(ctx, a.DB.DB, ds.marinaID)
	if err != nil {
		t.Fatal(err)
	}
	var reqID string
	if err := a.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		docID, err := a.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{Kind: docs.KindLetter, SubjectType: docs.SubjectLetter, SubjectID: ds.customer, PDFA: true})
		if err != nil {
			return err
		}
		reqID, err = a.Signatures.OpenTx(ctx, tx, scope, signature.OpenRequest{
			SubjectType: "contract", SubjectID: store.NewID(), DocumentID: docID, Driver: signature.DriverPaper,
			Signers: []signature.SignerSpec{{Role: "customer", CustomerID: ds.customer}}, ExpiresAt: time.Now().Add(24 * time.Hour),
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = a.Docs.RunRenderJob(ctx)
	_ = a.Signatures.RunSync(ctx)
	R := M + "/signature-requests/" + reqID
	var req api.SignatureRequest
	r := c.expect(t, 200, "", "GET", R, nil, &req)
	if req.Status != "sent" || len(req.Signers) != 1 || req.Signers[0].DisplayName == nil || r.header.Get("ETag") != etag(req.Version) {
		t.Fatalf("request %+v", req)
	}
	ps := ds.pricingSetup
	ps.yard.expect(t, 403, "forbidden", "GET", R, nil, nil)
	ps.other.expect(t, 404, "not_found", "GET", ps.otherM+"/signature-requests/"+reqID, nil, nil)
	var page api.SignatureRequestPage
	c.expect(t, 200, "", "GET", M+"/signature-requests?status=sent", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("page %+v", page.Items)
	}
	c.expect(t, 428, "precondition_required", "POST", R+"/cancel", nil, nil)
	c.expect(t, 428, "precondition_required", "POST", R+"/remind", nil, nil)
	c.expect(t, 412, "version_conflict", "POST", R+"/remind", nil, nil, "If-Match", etag(req.Version+3))
	c.expect(t, 200, "", "POST", R+"/remind", nil, &req, "If-Match", etag(req.Version))

	bad := c.s.login(t, "owner@porto.test").raw(t, "POST", R+"/paper-scan", "application/pdf", bytes.NewReader([]byte("nope")))
	if bad.status != 400 || bad.problem() != "invalid_file" {
		t.Fatalf("bad scan %d %s", bad.status, bad.body)
	}
	svg := c.raw(t, "POST", R+"/paper-scan", "image/svg+xml", bytes.NewReader([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)))
	if svg.status != 415 {
		t.Fatalf("svg scan %d %s", svg.status, svg.body)
	}
	pdfBytes := []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n")
	ok := c.raw(t, "POST", R+"/paper-scan", "application/pdf", bytes.NewReader(pdfBytes))
	if ok.status != 201 {
		t.Fatalf("scan %d %s", ok.status, ok.body)
	}
	other := ps.other.raw(t, "POST", R+"/paper-scan", "application/pdf", bytes.NewReader(pdfBytes))
	if other.status != 404 {
		t.Fatalf("other marina scan %d", other.status)
	}
	c.expect(t, 422, "paper_scan_required", "POST", R+"/confirm-paper", map[string]any{}, nil, "If-Match", etag(req.Version))
	c.expect(t, 404, "not_found", "POST", R+"/confirm-paper", map[string]any{"paper_scan_file_id": store.NewID()}, nil, "If-Match", etag(req.Version))

	// Provider settings.
	var st api.SignatureSettings
	c.expect(t, 200, "", "GET", M+"/signature-settings", nil, &st)
	if st.HasCredentials || len(st.AvailableProviders) != 0 {
		t.Fatalf("settings %+v", st)
	}
	ds.dock.expect(t, 403, "forbidden", "GET", M+"/signature-settings", nil, nil)
	c.expect(t, 422, "provider_unknown", "PUT", M+"/signature-settings", map[string]any{"provider_code": "infocert"}, nil, "If-Match", `"0"`)
	c.expect(t, 200, "", "PUT", M+"/signature-settings", map[string]any{"credentials": map[string]string{"k": "v"}}, &st, "If-Match", `"0"`)
	if !st.HasCredentials || st.Version != 1 {
		t.Fatalf("saved %+v", st)
	}

	// Webhook: no provider is registered, so every callback is 404.
	wh := c.raw(t, "POST", "/webhooks/signature/infocert/"+ds.marinaID, "application/json", bytes.NewReader([]byte(`{}`)))
	if wh.status != 404 {
		t.Fatalf("webhook %d", wh.status)
	}
}
