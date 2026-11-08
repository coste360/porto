//go:build integration

package admin_test

// Handler tests of the registry operations (m1-plan §4.4, §8.2): every operation through the real router of a
// full app on SQLite, asserting status codes, problem types and headers. Both dialects are covered by the
// domain integration tests and by internal/acceptance.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/pkg/ext"
)

const pw = "handler-test-password-1"

type server struct {
	app   *app.App
	srv   *httptest.Server
	clock *clock.Fake
}

func newServer(t *testing.T) *server {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_BASE_URL": "http://localhost:8080",
		"PORTO_DATA_DIR": t.TempDir(), "PORTO_DEV": "true",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(fixture.T0)
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, slog.New(slog.DiscardHandler), app.Options{
		Clock: c, Argon2: auth.TestArgon2Params, Transport: &fixture.Mailbox{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)
	return &server{app: a, srv: srv, clock: c}
}

type client struct {
	s    *server
	http *http.Client
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

func (r resp) json(t *testing.T) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("decode %d %s: %v", r.status, r.body, err)
	}
	return m
}

func (r resp) problem() string {
	var p struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(r.body, &p)
	return p.Type
}

func (s *server) login(t *testing.T, email string) *client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &client{s: s, http: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	if r := c.do(t, "POST", "/api/admin/v1/auth/login", map[string]string{"email": email, "password": pw}); r.status != 200 {
		t.Fatalf("login %s: %d %s", email, r.status, r.body)
	}
	return c
}

// do sends body as JSON; a []byte body is sent raw with the Content-Type given in headers.
func (c *client) do(t *testing.T, method, path string, body any, headers ...string) resp {
	t.Helper()
	var rd io.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		rd, ct = bytes.NewReader(raw), "application/json"
	}
	req, err := http.NewRequest(method, c.s.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	u, _ := url.Parse(c.s.srv.URL)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "porto_csrf" {
			req.Header.Set("X-CSRF-Token", ck.Value)
		}
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{status: res.StatusCode, header: res.Header, body: b}
}

// expect checks the status (and, for errors, the problem type) and returns the decoded JSON object, if any.
func (c *client) expect(t *testing.T, status int, problem, method, path string, body any, headers ...string) map[string]any {
	t.Helper()
	r := c.do(t, method, path, body, headers...)
	if r.status != status || (problem != "" && r.problem() != problem) {
		t.Fatalf("%s %s: %d %s, want %d %s", method, path, r.status, r.body, status, problem)
	}
	if len(r.body) > 0 && r.body[0] == '{' {
		return r.json(t)
	}
	return nil
}

func (s *server) addMember(t *testing.T, owner *client, marinaID, email, role string) *client {
	t.Helper()
	m := owner.expect(t, 201, "", "POST", "/api/admin/v1/marinas/"+marinaID+"/members", map[string]any{"email": email, "name": "M " + role, "role": role})
	if err := s.app.Auth.SetPassword(context.Background(), m["user_id"].(string), pw); err != nil {
		t.Fatal(err)
	}
	return s.login(t, email)
}

func sstr(m map[string]any, k string) string { s, _ := m[k].(string); return s }

var testPDF = []byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n")

var testPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01,
	0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x60, 0x00, 0x02, 0x00, 0x00, 0x05, 0x00, 0x01, 0xe9, 0xfa, 0xdc, 0xd8, 0x00, 0x00, 0x00, 0x00,
	0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestRegistryHandlers_Operations(t *testing.T) {
	s := newServer(t)
	ctx := context.Background()
	own, err := s.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
		Email: "owner@porto.test", Name: "Owner", Password: pw, Organization: "Org", MarinaName: "Marina", MarinaSlug: "marina-h",
	})
	if err != nil {
		t.Fatal(err)
	}
	o := s.login(t, "owner@porto.test")
	M := "/api/admin/v1/marinas/" + own.MarinaID
	missing := "01900000-0000-7000-8000-00000000abcd"

	// ---- customers ----
	created := o.expect(t, 201, "", "POST", M+"/customers", map[string]any{
		"kind": "person", "first_name": "Mario", "last_name": "Rossi", "fiscal_code": "RSSMRA85T10A562S",
		"emails": []map[string]string{{"label": "casa", "value": "mario@example.com"}}, "iban": "IT60X0542811101000000123456",
		"payment_terms": map[string]any{"days": 30, "end_of_month": true}, "tags": []string{"vip"},
	}, "Idempotency-Key", "create-customer-1")
	cust := created["customer"].(map[string]any)
	cid := sstr(cust, "id")
	if sstr(cust, "iban_masked") != "IT•• •••• 3456" || sstr(cust, "display_name") != "Rossi Mario" || len(created["possible_duplicates"].([]any)) != 0 {
		t.Fatalf("created %v", created)
	}
	if _, ok := cust["iban"]; ok {
		t.Fatal("IBAN returned in clear")
	}
	// Idempotent replay returns the same customer.
	replay := o.expect(t, 201, "", "POST", M+"/customers", map[string]any{
		"kind": "person", "first_name": "Mario", "last_name": "Rossi", "fiscal_code": "RSSMRA85T10A562S",
		"emails": []map[string]string{{"label": "casa", "value": "mario@example.com"}}, "iban": "IT60X0542811101000000123456",
		"payment_terms": map[string]any{"days": 30, "end_of_month": true}, "tags": []string{"vip"},
	}, "Idempotency-Key", "create-customer-1")
	if sstr(replay["customer"].(map[string]any), "id") != cid {
		t.Fatal("idempotent replay created another customer")
	}
	bad := o.expect(t, 400, "validation_failed", "POST", M+"/customers", map[string]any{"kind": "person", "last_name": "X", "fiscal_code": "RSSMRA85T10A562X"})
	if !strings.Contains(string(mustJSON(bad)), "fiscal_code_invalid") {
		t.Fatalf("field code missing: %v", bad)
	}
	o.expect(t, 400, "bad_request", "POST", M+"/customers", map[string]any{"kind": "person", "unknown": 1})
	dupRes := o.expect(t, 201, "", "POST", M+"/customers", map[string]any{"kind": "person", "last_name": "Rossi", "fiscal_code": "RSSMRA85T10A562S"})
	dupID := sstr(dupRes["customer"].(map[string]any), "id")
	if len(dupRes["possible_duplicates"].([]any)) != 1 {
		t.Fatalf("duplicates %v", dupRes)
	}
	list := o.expect(t, 200, "", "GET", M+"/customers?q=rossi&limit=1", nil)
	if len(list["items"].([]any)) != 1 || list["next_cursor"] == nil {
		t.Fatalf("list %v", list)
	}
	o.expect(t, 200, "", "GET", M+"/customers?cursor="+url.QueryEscape(sstr(list, "next_cursor")), nil)
	o.expect(t, 400, "bad_request", "GET", M+"/customers?cursor=nope", nil)
	if r := o.do(t, "GET", M+"/customers/"+cid, nil); r.status != 200 || r.header.Get("ETag") != `"1"` {
		t.Fatalf("get %d %s", r.status, r.header.Get("ETag"))
	}
	o.expect(t, 404, "not_found", "GET", M+"/customers/"+missing, nil)
	o.expect(t, 404, "not_found", "GET", M+"/customers/not-a-uuid", nil)
	o.expect(t, 428, "precondition_required", "PATCH", M+"/customers/"+cid, map[string]any{"notes": "x"})
	o.expect(t, 412, "version_conflict", "PATCH", M+"/customers/"+cid, map[string]any{"notes": "x"}, "If-Match", `"9"`)
	patched := o.expect(t, 200, "", "PATCH", M+"/customers/"+cid, map[string]any{"notes": "x", "payment_terms": nil, "iban": ""}, "If-Match", `"1"`)
	if patched["payment_terms"] != nil || patched["iban_masked"] != nil || patched["version"].(float64) != 2 {
		t.Fatalf("patched %v", patched)
	}
	o.expect(t, 201, "", "POST", M+"/customers/"+cid+"/consents", map[string]any{"kind": "privacy", "policy_version": "v1", "granted": true, "source": "desk"})
	o.expect(t, 400, "validation_failed", "POST", M+"/customers/"+cid+"/consents", map[string]any{"kind": "spam", "policy_version": "v1", "granted": true, "source": "desk"})
	if cs := o.expect(t, 200, "", "GET", M+"/customers/"+cid+"/consents", nil); len(cs["items"].([]any)) != 1 {
		t.Fatalf("consents %v", cs)
	}
	o.expect(t, 204, "", "POST", M+"/customers/"+dupID+"/archive", nil)
	o.expect(t, 409, "conflict", "POST", M+"/customers/"+dupID+"/archive", nil)
	o.expect(t, 204, "", "POST", M+"/customers/"+dupID+"/unarchive", nil)
	o.expect(t, 409, "conflict", "POST", M+"/customers/"+dupID+"/unarchive", nil)

	// ---- vessels ----
	v := o.expect(t, 201, "", "POST", M+"/vessels", map[string]any{
		"name": "Aurora", "loa_cm": 1180, "beam_cm": 390, "hull_type": "monohull_sail",
		"engines": []map[string]any{{"make": "Volvo", "power_kw": 40}},
		"parties": []map[string]any{{"customer_id": cid, "role": "owner", "share_millis": 1000}},
	}, "Idempotency-Key", "create-vessel-1")
	vid := sstr(v, "id")
	if sstr(v, "unit_class") != "imbarcazione" || len(v["parties"].([]any)) != 1 || len(v["documents"].([]any)) != 0 {
		t.Fatalf("vessel %v", v)
	}
	o.expect(t, 400, "validation_failed", "POST", M+"/vessels", map[string]any{"name": "", "loa_cm": 0, "beam_cm": 390})
	if vl := o.expect(t, 200, "", "GET", M+"/vessels?q=auro&customer_id="+cid, nil); len(vl["items"].([]any)) != 1 {
		t.Fatalf("vessel list %v", vl)
	}
	o.expect(t, 200, "", "GET", M+"/vessels/"+vid, nil)
	o.expect(t, 404, "not_found", "GET", M+"/vessels/"+missing, nil)
	o.expect(t, 428, "precondition_required", "PATCH", M+"/vessels/"+vid, map[string]any{"name": "B"})
	vp := o.expect(t, 200, "", "PATCH", M+"/vessels/"+vid, map[string]any{"registration": "RM1", "draft_cm": 150}, "If-Match", `"1"`)
	if sstr(vp, "registration") != "RM1" {
		t.Fatalf("patched vessel %v", vp)
	}
	vp = o.expect(t, 200, "", "PATCH", M+"/vessels/"+vid, map[string]any{"registration": nil, "draft_cm": nil}, "If-Match", `"2"`)
	if vp["registration"] != nil || vp["draft_cm"] != nil {
		t.Fatalf("null clears %v", vp)
	}
	exceed := o.expect(t, 400, "validation_failed", "PUT", M+"/vessels/"+vid+"/parties", map[string]any{"parties": []map[string]any{
		{"customer_id": cid, "role": "owner", "share_millis": 600}, {"customer_id": dupID, "role": "owner", "share_millis": 500},
	}}, "If-Match", `"3"`)
	if !strings.Contains(string(mustJSON(exceed)), "owner_shares_exceed") {
		t.Fatalf("exceed %v", exceed)
	}
	o.expect(t, 412, "version_conflict", "PUT", M+"/vessels/"+vid+"/parties", map[string]any{"parties": []any{}}, "If-Match", `"1"`)
	o.expect(t, 200, "", "PUT", M+"/vessels/"+vid+"/parties", map[string]any{"parties": []map[string]any{
		{"customer_id": cid, "role": "owner", "share_millis": 600}, {"customer_id": dupID, "role": "owner", "share_millis": 400},
	}}, "If-Match", `"3"`)
	dock := s.addMember(t, o, own.MarinaID, "dock@porto.test", "dock")
	viewer := s.addMember(t, o, own.MarinaID, "viewer@porto.test", "viewer")
	meas := dock.expect(t, 200, "", "POST", M+"/vessels/"+vid+"/measurements", map[string]any{"loa_cm": 1250, "beam_cm": 400})
	if sstr(meas, "dimensions_source") != "measured" || meas["effective_loa_cm"].(float64) != 1250 || meas["measured_by"] == nil {
		t.Fatalf("measured %v", meas)
	}
	viewer.expect(t, 403, "forbidden", "POST", M+"/vessels/"+vid+"/measurements", map[string]any{"loa_cm": 1250, "beam_cm": 400})
	viewer.expect(t, 403, "forbidden", "GET", M+"/customers", nil)
	viewer.expect(t, 403, "forbidden", "GET", M+"/vessels/"+vid, nil)
	dock.expect(t, 403, "forbidden", "POST", M+"/customers", map[string]any{"kind": "person", "last_name": "X"})
	o.expect(t, 204, "", "POST", M+"/vessels/"+vid+"/archive", nil)
	o.expect(t, 409, "conflict", "POST", M+"/vessels/"+vid+"/archive", nil)
	o.expect(t, 204, "", "POST", M+"/vessels/"+vid+"/unarchive", nil)

	// ---- documents ----
	doc := o.expect(t, 201, "", "POST", M+"/documents", map[string]any{
		"owner_type": "vessel", "vessel_id": vid, "type": "insurance_policy", "number": "POL-1", "issuer": "Assicura",
		"issued_on": "2026-01-01", "expires_on": "2026-11-16",
	}, "Idempotency-Key", "create-document-1")
	did := sstr(doc, "id")
	if sstr(doc, "number") != "POL-1" || doc["has_file"] != false {
		t.Fatalf("document %v", doc)
	}
	o.expect(t, 400, "validation_failed", "POST", M+"/documents", map[string]any{"owner_type": "vessel", "vessel_id": vid, "type": "insurance_policy", "expires_on": "16/11/2026"})
	// A rejected create must not leave a row behind (no write before validation).
	if dl := o.expect(t, 200, "", "GET", M+"/documents?vessel_id="+vid, nil); len(dl["items"].([]any)) != 1 {
		t.Fatalf("rejected create persisted a document: %v", dl)
	}
	o.expect(t, 404, "not_found", "POST", M+"/documents", map[string]any{"owner_type": "vessel", "vessel_id": missing, "type": "insurance_policy"})
	o.expect(t, 200, "", "GET", M+"/documents/"+did, nil)
	if dl := o.expect(t, 200, "", "GET", M+"/documents?vessel_id="+vid+"&expires_before=2026-12-31", nil); len(dl["items"].([]any)) != 1 {
		t.Fatalf("documents %v", dl)
	}
	o.expect(t, 400, "validation_failed", "GET", M+"/documents?expires_before=tomorrow", nil)
	o.expect(t, 200, "", "PATCH", M+"/documents/"+did, map[string]any{"issuer": nil, "notes": "rinnovo"}, "If-Match", `"1"`)
	o.expect(t, 404, "not_found", "POST", M+"/documents/"+did+"/file-url", nil)
	o.expect(t, 415, "unsupported_media_type", "PUT", M+"/documents/"+did+"/file", []byte("hello"), "Content-Type", "text/plain")
	o.expect(t, 400, "invalid_file", "PUT", M+"/documents/"+did+"/file", testPNG, "Content-Type", "application/pdf")
	big := append(append([]byte{}, testPDF...), make([]byte, 15<<20)...)
	o.expect(t, 413, "file_too_large", "PUT", M+"/documents/"+did+"/file", big, "Content-Type", "application/pdf")
	up := o.expect(t, 200, "", "PUT", M+"/documents/"+did+"/file", testPDF, "Content-Type", "application/pdf", "X-File-Name", "polizza.pdf")
	if up["has_file"] != true || sstr(up, "content_type") != "application/pdf" || sstr(up, "file_name") != "polizza.pdf" {
		t.Fatalf("uploaded %v", up)
	}
	link := o.expect(t, 200, "", "POST", M+"/documents/"+did+"/file-url", nil)
	r := o.do(t, "GET", sstr(link, "url"), nil)
	if r.status != 200 || !bytes.Equal(r.body, testPDF) || r.header.Get("Content-Type") != "application/pdf" ||
		r.header.Get("Cache-Control") != "no-store" || !strings.Contains(r.header.Get("Content-Security-Policy"), "sandbox") ||
		r.header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(r.header.Get("Content-Disposition"), "inline; filename*=UTF-8''polizza.pdf") {
		t.Fatalf("download %d %v", r.status, r.header)
	}
	dock.expect(t, 403, "download_link_expired", "GET", sstr(link, "url"), nil)
	o.expect(t, 403, "download_link_expired", "GET", M+"/documents/"+did+"/file", nil)
	s.clock.Advance(6 * time.Minute)
	o.expect(t, 403, "download_link_expired", "GET", sstr(link, "url"), nil)
	dock.expect(t, 403, "forbidden", "PUT", M+"/documents/"+did+"/file", testPDF, "Content-Type", "application/pdf")
	o.expect(t, 204, "", "DELETE", M+"/documents/"+did+"/file", nil)
	o.expect(t, 404, "not_found", "DELETE", M+"/documents/"+did+"/file", nil)

	// ---- deadlines ----
	dls := o.expect(t, 200, "", "GET", M+"/deadlines", nil)
	items := dls["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("deadlines %v", dls)
	}
	dl := items[0].(map[string]any)
	if sstr(dl, "urgency") != "due_soon" || sstr(dl, "kind") != "insurance" || sstr(dl, "document_type") != "insurance_policy" ||
		sstr(dl["vessel"].(map[string]any), "name") != "Aurora" {
		t.Fatalf("deadline %v", dl)
	}
	o.expect(t, 200, "", "GET", M+"/deadlines?urgency=due_soon&kind=insurance&due_after=2026-01-01&due_before=2027-01-01", nil)
	o.expect(t, 400, "validation_failed", "GET", M+"/deadlines?due_before=x", nil)
	o.expect(t, 428, "precondition_required", "PATCH", M+"/deadlines/"+sstr(dl, "id"), map[string]any{"status": "done"})
	if d := o.expect(t, 200, "", "PATCH", M+"/deadlines/"+sstr(dl, "id"), map[string]any{"status": "done"}, "If-Match", `"1"`); sstr(d, "status") != "done" {
		t.Fatalf("deadline done %v", d)
	}
	o.expect(t, 412, "version_conflict", "PATCH", M+"/deadlines/"+sstr(dl, "id"), map[string]any{"status": "open"}, "If-Match", `"1"`)
	o.expect(t, 404, "not_found", "PATCH", M+"/deadlines/"+missing, map[string]any{"status": "open"}, "If-Match", `"1"`)
	if ps := o.expect(t, 200, "", "GET", M+"/deadline-policies", nil); len(ps["items"].([]any)) != 21 { // M1 kinds, the M4 engine kinds and M9 maintenance_due
		t.Fatalf("policies %v", ps)
	}
	o.expect(t, 200, "", "PUT", M+"/deadline-policies", map[string]any{"items": []map[string]any{{"kind": "insurance", "lead_days": []int{30, 90}}}})
	o.expect(t, 400, "validation_failed", "PUT", M+"/deadline-policies", map[string]any{"items": []map[string]any{{"kind": "insurance", "lead_days": []int{0}}}})
	dock.expect(t, 403, "forbidden", "PUT", M+"/deadline-policies", map[string]any{"items": []any{}})
	o.expect(t, 412, "version_conflict", "DELETE", M+"/documents/"+did, nil, "If-Match", `"1"`)
	o.expect(t, 428, "precondition_required", "DELETE", M+"/documents/"+did, nil)

	// ---- overview, export, merge, anonymise ----
	ov := o.expect(t, 200, "", "GET", M+"/customers/"+cid+"/overview", nil)
	if len(ov["vessels"].([]any)) != 1 || len(ov["duplicates"].([]any)) != 1 || len(ov["consents"].([]any)) != 1 || len(ov["deadlines"].([]any)) != 0 {
		t.Fatalf("overview %v", ov)
	}
	er := o.do(t, "GET", M+"/customers/"+cid+"/export", nil)
	if er.status != 200 || !strings.HasPrefix(er.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export %d %v", er.status, er.header)
	}
	ex := er.json(t)
	if sstr(ex, "format") != "porto-customer/1" || len(ex["vessels"].([]any)) != 1 || len(ex["parties"].([]any)) != 1 {
		t.Fatalf("export %v", ex)
	}
	o.expect(t, 428, "precondition_required", "POST", M+"/customers/"+cid+"/merge", map[string]any{"source_ids": []string{dupID}})
	o.expect(t, 400, "validation_failed", "POST", M+"/customers/"+cid+"/merge", map[string]any{"source_ids": []string{}}, "If-Match", `"2"`)
	merged := o.expect(t, 200, "", "POST", M+"/customers/"+cid+"/merge", map[string]any{"source_ids": []string{dupID}}, "If-Match", `"2"`)
	if merged["version"].(float64) != 3 {
		t.Fatalf("merged %v", merged)
	}
	o.expect(t, 409, "customer_merged", "PATCH", M+"/customers/"+dupID, map[string]any{"notes": "x"}, "If-Match", `"3"`)
	o.expect(t, 400, "validation_failed", "POST", M+"/customers/"+cid+"/anonymize", map[string]any{"confirm": false})
	o.expect(t, 204, "", "POST", M+"/customers/"+cid+"/anonymize", map[string]any{"confirm": true})
	o.expect(t, 409, "customer_anonymized", "POST", M+"/customers/"+cid+"/anonymize", map[string]any{"confirm": true})
	o.expect(t, 409, "customer_anonymized", "POST", M+"/customers/"+cid+"/consents", map[string]any{"kind": "privacy", "policy_version": "v2", "granted": true, "source": "desk"})
	o.expect(t, 409, "customer_anonymized", "POST", M+"/documents", map[string]any{"owner_type": "customer", "customer_id": cid, "type": "passport"})
	o.expect(t, 204, "", "DELETE", M+"/documents/"+did, nil, "If-Match", `"4"`)
	o.expect(t, 404, "not_found", "GET", M+"/documents/"+did, nil)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
