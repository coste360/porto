//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// Helpers of the M4 contract acceptance tests: the customer reads their emails (fixture.Mailbox) and acts
// through the public magic-link API only.

const linkSlug = "banchina" // slug of the desk world marina

var (
	otpCodeRE = regexp.MustCompile(`(?:Codice|Code): (\d{6})`)
	linkRE    = map[string]*regexp.Regexp{}
)

// drainMail renders the pending documents and delivers every queued email (both jobs work in batches).
func (h *harness) drainMail() {
	h.t.Helper()
	ctx := context.Background()
	for range 200 {
		h.runJobs("documents.render")
		n, err := h.app.Notify.DeliverDue(ctx, 500)
		if err != nil {
			h.t.Fatalf("deliver: %v", err)
		}
		pending, err := h.app.DB.NewSelect().TableExpr("generated_documents").Where("status IN (?)", bun.List([]string{"pending", "rendering"})).Count(ctx)
		if err != nil {
			h.t.Fatal(err)
		}
		if n == 0 && pending == 0 {
			return
		}
	}
	h.t.Fatal("mail queue not drained")
}

// mailTo returns the delivered messages of a template to an address, oldest first.
func (h *harness) mailTo(to, template string) []string {
	var out []string
	for _, m := range h.mail.Messages() {
		if m.To == to && m.Template == template {
			out = append(out, m.Text)
		}
	}
	return out
}

// latestLink returns the token of the newest link of a purpose in the emails of a template to an address.
func (h *harness) latestLink(t *testing.T, to, template, purpose string) string {
	t.Helper()
	re, ok := linkRE[purpose]
	if !ok {
		re = regexp.MustCompile(`/m/` + purpose + `/([0-9A-Za-z_-]{20,})`)
		linkRE[purpose] = re
	}
	msgs := h.mailTo(to, template)
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := re.FindStringSubmatch(msgs[i]); m != nil {
			return m[1]
		}
	}
	t.Fatalf("no %s link in the %s emails to the customer (%d emails)", purpose, template, len(msgs))
	return ""
}

// latestOTP returns the newest signature code emailed to an address.
func (h *harness) latestOTP(t *testing.T, to string) string {
	t.Helper()
	msgs := h.mailTo(to, "signature_otp")
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := otpCodeRE.FindStringSubmatch(msgs[i]); m != nil {
			return m[1]
		}
	}
	t.Fatalf("no OTP email to the customer")
	return ""
}

// signByOTP signs through a signature link: identity confirmation, code by email, verification. wrongFirst
// sends a wrong code once and checks the attempts left.
func signByOTP(t *testing.T, h *harness, pub *client, to string, wrongFirst bool) map[string]any {
	t.Helper()
	h.drainMail()
	tok := h.latestLink(t, to, "signature_request", "signature")
	L := "/api/public/v1/" + linkSlug + "/links/" + tok
	page := pub.expect(t, 200, "GET", L, nil)
	if sig, _ := page["signature"].(map[string]any); sig == nil || str(sig, "signature_kind") != "firma_elettronica_semplice" {
		t.Fatalf("signature page %v", page)
	}
	if doc := pub.do(t, "GET", L+"/document", nil); doc.status != 200 || !bytes.HasPrefix(doc.body, []byte("%PDF-")) {
		t.Fatalf("document to sign %d", doc.status)
	}
	pub.expect(t, 200, "POST", L+"/signature/otp", map[string]any{"identity_confirmed": true})
	h.drainMail()
	code := h.latestOTP(t, to)
	if wrongFirst {
		wrong := "000000"
		if code == wrong {
			wrong = "111111"
		}
		r := pub.do(t, "POST", L+"/signature/verify", map[string]any{"code": wrong})
		if r.status != 422 || r.problemType() != "otp_invalid" || !strings.Contains(string(r.body), `"attempts_left":4`) {
			t.Fatalf("wrong code %d %s", r.status, r.body)
		}
	}
	return pub.expect(t, 200, "POST", L+"/signature/verify", map[string]any{"code": code})
}

// contractSettings updates the contract settings through the API.
func (w *deskWorld) contractSettings(t *testing.T, body map[string]any) {
	t.Helper()
	set := w.owner.expect(t, 200, "GET", w.M+"/contract-settings", nil)
	w.owner.expect(t, 200, "PUT", w.M+"/contract-settings", body, "If-Match", etagOf(set["version"]))
}

// mailCustomer creates a person with an email and a locale.
func (w *deskWorld) mailCustomer(t *testing.T, last, email, locale string) string {
	t.Helper()
	r := w.owner.expect(t, 201, "POST", w.M+"/customers", map[string]any{
		"kind": "person", "first_name": "Mario", "last_name": last, "locale": locale,
		"emails": []map[string]string{{"label": "home", "value": email}},
	})
	return str(r["customer"].(map[string]any), "id")
}

// m4Policy is the payment policy of the M4 acceptance: deposit 30 % due in 7 days, then 3 quarterly instalments.
var m4Policy = map[string]any{
	"version": 2,
	"payment": map[string]any{
		"deposit": map[string]any{"bp": 3000}, "deposit_due_days": 7,
		"schedule":              map[string]any{"mode": "interval", "every_months": 3, "count": 3},
		"issue_days_before_due": 15, "rounding_unit_cents": 1,
		"early_payment": map[string]any{"discount_bp": 300, "pay_by_offset_days": 10},
	},
}

// planOf returns the instalment plan of a contract and checks Σ = total (INV-31).
func planOf(t *testing.T, c *client, marinaPath, contractID string) map[string]any {
	t.Helper()
	p := c.expect(t, 200, "GET", marinaPath+"/contracts/"+contractID+"/installments", nil)
	total, _ := num(p, "total_cents")
	sum, _ := num(p, "sum_cents")
	var items int64
	for _, it := range jsonList(p, "items") {
		m := it.(map[string]any)
		if str(m, "status") == "cancelled" {
			continue
		}
		a, _ := num(m, "amount_cents")
		items += a
	}
	if total == 0 || sum != total || items != total {
		t.Fatalf("plan of %s: total %d sum %d items %d", contractID, total, sum, items)
	}
	return p
}

// liveInstallments are the instalments of a plan that are not cancelled.
func liveInstallments(p map[string]any) []map[string]any {
	var out []map[string]any
	for _, it := range jsonList(p, "items") {
		if m := it.(map[string]any); str(m, "status") != "cancelled" {
			out = append(out, m)
		}
	}
	return out
}

// TestAcceptance_M4_QuoteToContract (m4-plan §9.4): a quote with two options sent by email; the customer uploads
// the insurance policy and accepts option 2 through the link completing the missing address; the contract awaits
// the signature with its plan and PDF/A document; OTP signature activates it and issues the first instalment;
// the office settles it. A second quote accepted from the panel is signed on paper (scan required). An addendum
// signed by OTP replans the future instalments; a termination shows its refund. Roles and isolation.
func TestAcceptance_M4_QuoteToContract(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 6)
		o, M, b := w.owner, w.M, w.harbour.Berths
		office, dock, viewer := w.clients["office"], w.clients["dock"], w.clients["viewer"]
		ctx := context.Background()
		w.contractSettings(t, map[string]any{
			"signature_driver": "otp", "require_paper_scan": true, "insurance_policy": "warn", "required_fields": []string{"address"},
			"default_policy": m4Policy, "office_email": "ufficio@porto.test",
			"bank_details": map[string]any{"iban": "IT60X0542811101000000123456", "holder": "Marina Banchina", "bank": "Banca"},
		})
		const email = "bianchi@example.test"
		bianchi := w.mailCustomer(t, "Bianchi", email, "it")
		vela := w.vessel(t, "Vela", 1100, 380)

		// 1. A quote with two options, sent by email with the PDF attached and a link.
		q := office.expect(t, 201, "POST", M+"/quotes", map[string]any{
			"kind": "mooring", "term": "annual", "customer_id": bianchi, "vessel_id": vela, "starts_on": "2027-07-01", "ends_on": "2028-07-01",
			"options": []any{
				map[string]any{"label": "A1", "hold": true, "placements": []any{berthPlacement(b[0], "2027-07-01", "2028-06-30")}},
				map[string]any{"label": "A2", "hold": true, "placements": []any{berthPlacement(b[1], "2027-07-01", "2028-06-30")}},
			},
		})
		qid := str(q, "id")
		sent := office.expect(t, 200, "POST", M+"/quotes/"+qid+"/send", map[string]any{}, "If-Match", etagOf(q["version"]))
		if str(sent["quote"].(map[string]any), "status") != "sent" || str(sent, "document_id") == "" {
			t.Fatalf("send %v", sent)
		}
		w.h.drainMail()
		var attached bool
		for _, m := range w.h.mail.Messages() {
			if m.To == email && m.Template == "quote" {
				attached = len(m.Attachments) > 0 && bytes.HasPrefix(m.Attachments[0].Content, []byte("%PDF-"))
			}
		}
		if !attached {
			t.Fatal("quote email without the PDF")
		}
		tok := w.h.latestLink(t, email, "quote", "quote")
		pub := w.h.cookieClient()
		L := "/api/public/v1/" + linkSlug + "/links/" + tok
		page := pub.expect(t, 200, "GET", L, nil)
		pq, _ := page["quote"].(map[string]any)
		if pq == nil || len(jsonList(pq, "options")) != 2 || !strings.Contains(string(mustJSON(t, pq["missing_fields"])), "address") {
			t.Fatalf("quote page %v", page)
		}
		if doc := pub.do(t, "GET", L+"/document", nil); doc.status != 200 || !bytes.HasPrefix(doc.body, []byte("%PDF-")) {
			t.Fatalf("quote document %d", doc.status)
		}
		opt2 := str(jsonList(pq, "options")[1].(map[string]any), "id")

		// 2. The customer uploads the insurance policy and accepts option 2, completing the address.
		up := pub.raw(t, "POST", L+"/documents?type=insurance_policy&expires_on=2029-06-30", "application/pdf", testPDF)
		if up.status != 201 {
			t.Fatalf("upload %d %s", up.status, up.body)
		}
		if r := pub.do(t, "POST", L+"/quote/accept", map[string]any{"option_id": opt2}); r.status != 400 || !strings.Contains(string(r.body), "missing_fields.address") {
			t.Fatalf("accept without the address %d %s", r.status, r.body)
		}
		acc := pub.expect(t, 200, "POST", L+"/quote/accept", map[string]any{
			"option_id": opt2, "customer": map[string]any{"address": map[string]any{"line1": "Via Roma 1", "postal_code": "16100", "city": "Genova", "province": "GE", "country": "IT"}},
		})
		if str(acc, "contract_code") == "" {
			t.Fatalf("accept %v", acc)
		}
		cust := o.expect(t, 200, "GET", M+"/customers/"+bianchi, nil)
		if !strings.Contains(string(mustJSON(t, cust)), "Via Roma 1") {
			t.Fatal("address not saved on the customer")
		}
		q = o.expect(t, 200, "GET", M+"/quotes/"+qid, nil)
		cid := str(q, "contract_id")
		ct := o.expect(t, 200, "GET", M+"/contracts/"+cid, nil)
		if str(q, "status") != "accepted" || str(ct, "status") != "awaiting_signature" {
			t.Fatalf("after acceptance quote %s contract %s", str(q, "status"), str(ct, "status"))
		}
		planOf(t, o, M, cid)
		w.h.runJobs("documents.render")
		gds := o.expect(t, 200, "GET", M+"/generated-documents?subject_type=contract&subject_id="+cid, nil)
		var pdfa bool
		for _, g := range jsonList(gds, "items") {
			gm := g.(map[string]any)
			pdfa = pdfa || (gm["pdfa"] == true && str(gm, "status") == "ready")
		}
		if !pdfa {
			t.Fatalf("no ready PDF/A contract document: %v", gds)
		}

		// 3. OTP signature ⇒ active, first instalment issued with the payment request email.
		signed := signByOTP(t, w.h, pub, email, true)
		if dep, _ := signed["deposit"].(map[string]any); dep == nil || str(dep, "due_on") == "" {
			t.Fatalf("signature result without the deposit instructions: %v", signed)
		}
		ct = o.expect(t, 200, "GET", M+"/contracts/"+cid, nil)
		if str(ct, "status") != "active" || str(ct, "signed_document_id") == "" {
			t.Fatalf("after signature %s %v", str(ct, "status"), ct["signed_document_id"])
		}
		plan := planOf(t, o, M, cid)
		first := liveInstallments(plan)[0]
		if str(first, "status") != "issued" {
			t.Fatalf("first instalment %v", first)
		}
		w.h.drainMail()
		if len(w.h.mailTo(email, "payment_request")) == 0 {
			t.Fatal("no payment request email")
		}
		if len(eventsOf(t, db, "contract.signed")) != 1 {
			t.Fatal("contract.signed not emitted once")
		}

		// 4. Roles on settlement, then the office settles.
		settle := map[string]any{"method": "bank_transfer", "reference": "CRO 0001"}
		for _, c := range []*client{dock, viewer} {
			if r := c.do(t, "POST", M+"/installments/"+str(first, "id")+"/settle", settle, "If-Match", etagOf(first["version"])); r.status != 403 {
				t.Fatalf("settle by a role without payments.collect: %d", r.status)
			}
		}
		paid := office.expect(t, 200, "POST", M+"/installments/"+str(first, "id")+"/settle", settle, "If-Match", etagOf(first["version"]))
		if str(paid, "status") != "paid" || str(paid, "settlement_source") != "payment" { // M5 (D7): a settlement records a payment
			t.Fatalf("settled %v", paid)
		}

		// 5. A second quote accepted from the panel, signed on paper: the scan is required.
		w.contractSettings(t, map[string]any{"signature_driver": "paper"})
		rossi := w.mailCustomer(t, "Rossi", "rossi@example.test", "en")
		maestrale := w.vessel(t, "Maestrale", 1000, 350)
		q2 := office.expect(t, 201, "POST", M+"/quotes", map[string]any{
			"kind": "mooring", "term": "annual", "customer_id": rossi, "vessel_id": maestrale, "starts_on": "2027-07-01", "ends_on": "2028-07-01",
			"options": []any{map[string]any{"hold": true, "placements": []any{berthPlacement(b[2], "2027-07-01", "2028-06-30")}}},
		})
		acc2 := office.expect(t, 200, "POST", M+"/quotes/"+str(q2, "id")+"/accept", map[string]any{"option_id": str(jsonList(q2, "options")[0].(map[string]any), "id")}, "If-Match", etagOf(q2["version"]))
		c2 := acc2["contract"].(map[string]any)
		c2id := str(c2, "id")
		if r := office.do(t, "POST", M+"/contracts/"+c2id+"/activate", map[string]any{}, "If-Match", etagOf(c2["version"])); r.status != 422 || r.problemType() != "paper_scan_required" {
			t.Fatalf("paper activation without scan %d %s", r.status, r.body)
		}
		rs := office.expect(t, 200, "POST", M+"/contracts/"+c2id+"/request-signature", map[string]any{"driver": "paper"}, "If-Match", etagOf(c2["version"]))
		reqID := str(rs, "signature_request_id")
		if reqID == "" {
			c2 = o.expect(t, 200, "GET", M+"/contracts/"+c2id, nil)
			reqID = str(c2, "signature_request_id")
		}
		scan := office.raw(t, "POST", M+"/signature-requests/"+reqID+"/paper-scan", "application/pdf", testPDF, "X-File-Name", "scan.pdf")
		if scan.status != 201 {
			t.Fatalf("scan upload %d %s", scan.status, scan.body)
		}
		sr := office.expect(t, 200, "GET", M+"/signature-requests/"+reqID, nil)
		office.expect(t, 200, "POST", M+"/signature-requests/"+reqID+"/confirm-paper", map[string]any{"paper_scan_file_id": str(scan.json(t), "file_id")}, "If-Match", etagOf(sr["version"]))
		if c := o.expect(t, 200, "GET", M+"/contracts/"+c2id, nil); str(c, "status") != "active" {
			t.Fatalf("paper contract %s", str(c, "status"))
		}
		planOf(t, o, M, c2id)

		// 6. An addendum signed by OTP replans the future instalments (price change: new total).
		w.contractSettings(t, map[string]any{"signature_driver": "otp"})
		ct = o.expect(t, 200, "GET", M+"/contracts/"+cid, nil)
		oldTotal, _ := num(ct, "total_cents")
		ad := office.expect(t, 201, "POST", M+"/contracts/"+cid+"/addenda", map[string]any{
			"kind": "price_change", "effective_on": "2027-09-01", "payload": map[string]any{"new_total_cents": oldTotal + 30_000, "reason": "posto auto aggiunto"},
		})
		office.expect(t, 200, "POST", M+"/contracts/"+cid+"/addenda/"+str(ad, "id")+"/request-signature", map[string]any{}, "If-Match", etagOf(ad["version"]))
		signByOTP(t, w.h, pub, email, false)
		plan2 := planOf(t, o, M, cid)
		if v1, v2 := plan["plan_version"].(float64), plan2["plan_version"].(float64); v2 <= v1 {
			t.Fatalf("addendum did not replan: %v → %v", v1, v2)
		}
		if tot, _ := num(plan2, "total_cents"); tot != oldTotal+30_000 {
			t.Fatalf("replanned total %d", tot)
		}
		ads := o.expect(t, 200, "GET", M+"/contracts/"+cid+"/addenda", nil)
		if a := jsonList(ads, "items")[0].(map[string]any); str(a, "status") != "active" {
			t.Fatalf("addendum %v", a)
		}

		// 7. A termination stores the refund amount.
		ct = o.expect(t, 200, "GET", M+"/contracts/"+cid, nil)
		term := office.expect(t, 200, "POST", M+"/contracts/"+cid+"/terminate", map[string]any{"effective_on": "2028-01-01", "reason": "vendita barca"}, "If-Match", etagOf(ct["version"]))
		if _, ok := term["termination_amount_cents"]; !ok {
			t.Fatalf("termination without amount %v", term)
		}

		// 8. Another marina: every id of this test is 404.
		if _, err := w.h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "altra@porto.test", Name: "Altra", Password: password, Organization: "Altra SRL", MarinaName: "Marina Altra", MarinaSlug: "altra",
		}); err != nil {
			t.Fatal(err)
		}
		other := w.h.cookieClient()
		other.login(t, "altra@porto.test")
		for _, p := range []string{"/contracts/" + cid, "/contracts/" + cid + "/installments", "/quotes/" + qid, "/signature-requests/" + reqID} {
			if r := other.do(t, "GET", M+p, nil); r.status != 404 {
				t.Fatalf("other marina %s: %d", p, r.status)
			}
		}
		vs, err := store.CheckOccupancyInvariants(ctx, db.DB, w.marina, calendar.In(w.clock.Now(), time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			t.Errorf("invariant %s %v %s", v.Query, v.IDs, v.Detail)
		}
		cvs, err := store.CheckContractInvariants(ctx, db.DB, w.marina)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range cvs {
			t.Errorf("contract invariant %s %v %s", v.Query, v.IDs, v.Detail)
		}
		assertNoPII(t, w.h.logs.String(), email, "Bianchi", tok)
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// testPDF is a minimal PDF for uploads (scans, policies).
var testPDF = []byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n")
