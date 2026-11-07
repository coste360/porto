//go:build integration

package admin_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/coste360/porto/internal/http/api"
)

// TestChargeHandlers_Scenario: the charges, exports, reconciliation, billing settings, opening balances and
// payments export operations through the real router (m5-plan §7.3.1): statuses, problem types, ETags,
// permissions and another marina's 404.
func TestChargeHandlers_Scenario(t *testing.T) {
	ds := newDeskSetup(t)
	c, M := ds.owner, ds.M
	var classes api.RevenueClassList
	c.expect(t, 200, "", "GET", M+"/revenue-classes", nil, &classes)
	var gru, saldo api.Item
	c.expect(t, 201, "", "POST", M+"/items", map[string]any{
		"code": "GRU", "name": map[string]string{"it": "Gru"}, "item_type": "service", "unit": "piece", "vat_rate_bp": 2200,
		"revenue_class_id": classes.Items[0].Id, "price_includes_vat": true,
	}, &gru)
	c.expect(t, 201, "", "POST", M+"/items", map[string]any{
		"code": "SALDO", "name": map[string]string{"it": "Saldo"}, "item_type": "fee", "unit": "piece", "vat_rate_bp": 0, "vat_nature": "N1",
		"revenue_class_id": classes.Items[0].Id, "price_includes_vat": true,
	}, &saldo)

	// Billing settings: version 0 until the first save.
	var bs api.BillingSettings
	r := c.expect(t, 200, "", "GET", M+"/billing-settings", nil, &bs)
	if bs.Version != 0 || bs.DefaultDueDays != 30 || len(bs.ReminderPolicy.Levels) != 5 || len(bs.FiscalDrivers) < 2 || r.header.Get("ETag") != `"0"` {
		t.Fatalf("settings %+v", bs)
	}
	settings := map[string]any{
		"default_due_days": 15, "auto_payment_request": true, "reminder_policy": bs.ReminderPolicy, "reminder_hour": 9,
		"arrears_policy": map[string]any{"enabled": false, "days_overdue": 60, "min_amount_cents": 0}, "export_reminder_days": 30,
		"fiscal_driver": "none", "receipt_footer": "", "sepa_lead_days": 0,
		"origin_items": []any{map[string]any{"origin_key": "opening_balance", "item_id": saldo.Id}},
	}
	ds.dock.expect(t, 403, "forbidden", "PUT", M+"/billing-settings", settings, nil, "If-Match", `"0"`)
	ds.other.expect(t, 404, "not_found", "PUT", M+"/billing-settings", settings, nil, "If-Match", `"0"`)
	bad := map[string]any{}
	for k, v := range settings {
		bad[k] = v
	}
	bad["fiscal_driver"] = "nope"
	c.expect(t, 400, "validation_failed", "PUT", M+"/billing-settings", bad, nil, "If-Match", `"0"`)
	c.expect(t, 200, "", "PUT", M+"/billing-settings", settings, &bs, "If-Match", `"0"`)
	if bs.Version != 1 || bs.DefaultDueDays != 15 || len(bs.OriginItems) != 1 {
		t.Fatalf("saved %+v", bs)
	}
	c.expect(t, 412, "version_conflict", "PUT", M+"/billing-settings", settings, nil, "If-Match", `"7"`)

	// Manual charges.
	body := map[string]any{"origin_type": "manual", "customer_id": ds.customer, "lines": []any{
		map[string]any{"item_id": gru.Id, "qty_milli": 2000, "unit_cents": 6100},
	}}
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/charges", body, nil)
	ds.other.expect(t, 404, "not_found", "POST", ds.otherM+"/charges", body, nil)
	c.expect(t, 422, "customer_required", "POST", M+"/charges", map[string]any{"origin_type": "manual", "lines": body["lines"]}, nil)
	c.expect(t, 422, "no_price", "POST", M+"/charges", map[string]any{"origin_type": "manual", "customer_id": ds.customer, "lines": []any{
		map[string]any{"item_id": gru.Id, "qty_milli": 1000},
	}}, nil)
	var ch api.Charge
	r = c.expect(t, 201, "", "POST", M+"/charges", body, &ch)
	if ch.TotalCents != 12200 || ch.VatCents != 2200 || ch.PaymentStatus != "open" || ch.InvoicingStatus != "to_invoice" ||
		!strings.HasPrefix(ch.Number, "A") || r.header.Get("ETag") != etag(ch.Version) || ch.Lines == nil || len(*ch.Lines) != 1 {
		t.Fatalf("charge %+v", ch)
	}
	C := M + "/charges/" + ch.Id
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/charges/"+ch.Id, nil, nil)
	c.expect(t, 404, "not_found", "GET", M+"/charges/01900000-0000-7000-8000-000000000001", nil, nil)
	var page api.ChargePage
	c.expect(t, 200, "", "GET", M+"/charges?invoicing_status=to_invoice&q=A20", nil, &page)
	if len(page.Items) != 1 {
		t.Fatalf("list %+v", page.Items)
	}
	c.expect(t, 400, "bad_request", "GET", M+"/charges?issued_from=ieri", nil, nil)
	r = c.expect(t, 200, "", "GET", M+"/charges?format=csv", nil, nil)
	if !strings.HasPrefix(r.header.Get("Content-Type"), "text/csv") || !strings.Contains(string(r.body), ch.Number) {
		t.Fatalf("csv %q", r.body)
	}
	r = c.expect(t, 200, "", "GET", M+"/charges?format=xlsx", nil, nil)
	if !strings.HasPrefix(string(r.body), "PK") {
		t.Fatal("xlsx")
	}
	// Edit in place (to_invoice): If-Match required; adjust refused.
	c.expect(t, 428, "precondition_required", "PATCH", C, map[string]any{"description": "Varo"}, nil)
	c.expect(t, 200, "", "PATCH", C, map[string]any{"description": "Varo", "due_on": "2027-07-31"}, &ch, "If-Match", etag(ch.Version))
	c.expect(t, 409, "use_edit", "POST", C+"/adjust", map[string]any{"mode": "delta", "reason": "x", "lines": []any{
		map[string]any{"item_id": gru.Id, "qty_milli": -1000, "unit_cents": 100},
	}}, nil, "If-Match", etag(ch.Version))
	c.expect(t, 409, "not_exported", "PUT", C+"/invoice", map[string]any{"number": "FT-1", "date": "2027-06-30"}, nil)

	// Export.
	filter := map[string]any{"period_from": "2020-01-01", "period_to": "2035-12-31"}
	var prev api.ExportPreview
	c.expect(t, 200, "", "POST", M+"/charge-exports/preview", filter, &prev)
	if prev.Count != 1 || prev.TotalCents != 12200 || len(prev.TotalsByVat) != 1 || len(prev.Warnings) != 1 {
		t.Fatalf("preview %+v", prev)
	}
	ds.dock.expect(t, 403, "forbidden", "POST", M+"/charge-exports", filter, nil)
	c.expect(t, 422, "provider_unknown", "POST", M+"/charge-exports", map[string]any{"period_from": "2020-01-01", "period_to": "2035-12-31", "exporter_code": "x"}, nil)
	var exp api.ChargeExport
	c.expect(t, 201, "", "POST", M+"/charge-exports", filter, &exp)
	if exp.ChargeCount != 1 || exp.TotalCents != 12200 || exp.Kind != "export" || !strings.HasPrefix(exp.Number, "E") {
		t.Fatalf("export %+v", exp)
	}
	c.expect(t, 422, "export_empty", "POST", M+"/charge-exports", filter, nil)
	E := M + "/charge-exports/" + exp.Id
	var det api.ChargeExportDetail
	c.expect(t, 200, "", "GET", E, nil, &det)
	if len(det.Items) != 1 || !det.Items[0].First || det.Items[0].Customer == nil {
		t.Fatalf("detail %+v", det)
	}
	ds.other.expect(t, 404, "not_found", "GET", ds.otherM+"/charge-exports/"+exp.Id, nil, nil)
	c.expect(t, 400, "validation_failed", "GET", E+"/file", nil, nil)
	for format, prefix := range map[string]string{"csv": "\ufeffNumero addebito", "xlsx": "PK", "pdf": "%PDF-"} {
		r = c.expect(t, 200, "", "GET", E+"/file?format="+format, nil, nil)
		if !strings.HasPrefix(string(r.body), prefix) || !strings.Contains(r.header.Get("Content-Disposition"), exp.Number) {
			t.Fatalf("%s download %q", format, r.header.Get("Content-Disposition"))
		}
	}
	var list api.ChargeExportPage
	c.expect(t, 200, "", "GET", M+"/charge-exports", nil, &list)
	if len(list.Items) != 1 {
		t.Fatalf("exports %+v", list)
	}
	var re api.ChargeExport
	c.expect(t, 201, "", "POST", M+"/charge-exports/reexport", map[string]any{"export_id": exp.Id}, &re)
	if re.Kind != "reexport" || re.ChargeCount != 1 {
		t.Fatalf("reexport %+v", re)
	}
	c.expect(t, 400, "validation_failed", "POST", M+"/charge-exports/reexport", map[string]any{}, nil)

	// Exported: immutable, adjustable, invoiceable.
	c.expect(t, 200, "", "GET", C, nil, &ch)
	c.expect(t, 409, "charge_immutable", "POST", C+"/cancel", map[string]any{"reason": "x"}, nil, "If-Match", etag(ch.Version))
	c.expect(t, 409, "charge_immutable", "PATCH", C, map[string]any{"lines": []any{map[string]any{"item_id": gru.Id, "qty_milli": 1000, "unit_cents": 1}}}, nil, "If-Match", etag(ch.Version))
	var adj api.AdjustResult
	c.expect(t, 201, "", "POST", C+"/adjust", map[string]any{"mode": "delta", "reason": "maggiorazione", "lines": []any{
		map[string]any{"item_id": gru.Id, "qty_milli": 1000, "unit_cents": 500},
	}}, &adj, "If-Match", etag(ch.Version))
	if len(adj.Adjustments) != 1 || adj.Adjustments[0].CorrectsCharge == nil || adj.Adjustments[0].TotalCents != 500 {
		t.Fatalf("adjust %+v", adj)
	}
	var rec api.InvoiceReconciliation
	c.expect(t, 200, "", "POST", M+"/invoice-reconciliations", map[string]any{"dry_run": true, "rows": []any{
		map[string]any{"charge_number": ch.Number, "invoice_number": "FT-1", "invoice_date": "30/06/2027"},
		map[string]any{"charge_number": "A1999-000001", "invoice_number": "FT-2", "invoice_date": "30/06/2027"},
	}}, &rec)
	if rec.MatchedCount != 1 || rec.RowCount != 2 || rec.Report[1].Outcome != "unknown_charge" {
		t.Fatalf("reconciliation %+v", rec)
	}
	c.expect(t, 400, "csv_invalid", "POST", M+"/invoice-reconciliations", map[string]any{"dry_run": true, "csv": "a;b\n1;2\n"}, nil)
	var recs api.InvoiceReconciliationPage
	c.expect(t, 200, "", "GET", M+"/invoice-reconciliations", nil, &recs)
	c.expect(t, 200, "", "PUT", C+"/invoice", map[string]any{"number": "FT-1", "date": "2027-06-30"}, &ch)
	if ch.InvoicingStatus != "invoiced" || ch.ExternalInvoiceNumber == nil || *ch.ExternalInvoiceNumber != "FT-1" {
		t.Fatalf("invoiced %+v", ch)
	}

	// Payments export (empty period), validated after authorisation.
	ds.other.expect(t, 404, "not_found", "GET", M+"/payments/export", nil, nil)
	c.expect(t, 400, "validation_failed", "GET", M+"/payments/export", nil, nil)
	r = c.expect(t, 200, "", "GET", M+"/payments/export?from=2020-01-01&to=2035-12-31&format=csv", nil, nil)
	if !strings.Contains(string(r.body), "Metodo") {
		t.Fatalf("payments export %q", r.body)
	}

	// Opening balances.
	file := base64.StdEncoding.EncodeToString([]byte("id;importo;scadenza\n" + ds.customer + ";100,00;31/07/2027\n"))
	ob := map[string]any{"format": "csv", "file_base64": file, "has_header": true, "dry_run": true, "mapping": map[string]any{
		"customer_match": "customer_code", "customer_column": 0, "amount_column": 1, "due_on_column": 2,
	}}
	var imp api.ChargeImport
	c.expect(t, 200, "", "POST", M+"/opening-balances/import", ob, &imp)
	if imp.Status != "dry_run" || imp.ValidRows != 1 || imp.TotalCents == nil || *imp.TotalCents != 10000 {
		t.Fatalf("dry run %+v", imp)
	}
	ob["dry_run"] = false
	c.expect(t, 200, "", "POST", M+"/opening-balances/import", ob, &imp)
	if imp.Status != "applied" || imp.ChargeCount != 1 {
		t.Fatalf("apply %+v", imp)
	}
	var imps api.ChargeImportPage
	c.expect(t, 200, "", "GET", M+"/opening-balances/imports", nil, &imps)
	if len(imps.Items) != 2 {
		t.Fatalf("imports %+v", imps)
	}
	c.expect(t, 200, "", "POST", M+"/opening-balances/imports/"+imp.Id+"/undo", nil, &imp, "If-Match", etag(imp.Version))
	c.expect(t, 409, "import_not_undoable", "POST", M+"/opening-balances/imports/"+imp.Id+"/undo", nil, nil, "If-Match", etag(imp.Version))
	c.expect(t, 400, "validation_failed", "POST", M+"/opening-balances/import", map[string]any{
		"format": "csv", "file_base64": "%%%", "has_header": true, "dry_run": true, "mapping": ob["mapping"],
	}, nil)

	// Termination charge: unknown contract 404.
	c.expect(t, 404, "not_found", "POST", M+"/contracts/01900000-0000-7000-8000-000000000001/termination-charge", nil, nil, "If-Match", `"1"`)
}
