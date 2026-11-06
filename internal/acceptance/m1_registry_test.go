//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
	"github.com/coste360/porto/pkg/ext"
)

// newRegistryHarness is newHarness with a fake clock (signed links expire, deadlines depend on today).
func newRegistryHarness(t *testing.T, db *store.DB, c *clock.Fake) *harness {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vars := map[string]string{
		"PORTO_SECRET_KEY": base64.StdEncoding.EncodeToString(key), "PORTO_BASE_URL": "http://localhost:8080",
		"PORTO_DATA_DIR": t.TempDir(), "PORTO_DEV": "true", "PORTO_LOG_LEVEL": "debug",
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mail := &fixture.Mailbox{}
	a, err := app.NewWithOptions(context.Background(), cfg, "test", ext.Extensions{}, log, app.Options{
		DB: db, Clock: c, Argon2: auth.TestArgon2Params, Transport: mail,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Router)
	t.Cleanup(srv.Close)
	return &harness{t: t, app: a, srv: srv, logs: logs, mail: mail}
}

// raw sends a binary body.
func (c *client) raw(t *testing.T, method, path, contentType string, body []byte, headers ...string) response {
	t.Helper()
	req, err := http.NewRequest(method, c.h.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	if tok := c.csrf(); tok != "" {
		req.Header.Set("X-CSRF-Token", tok)
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
	return response{status: res.StatusCode, header: res.Header, body: b}
}

func (h *harness) member(t *testing.T, owner *client, marinaID, email, role string) *client {
	t.Helper()
	m := owner.expect(t, 201, "POST", A+"/marinas/"+marinaID+"/members", map[string]any{"email": email, "name": "Staff " + role, "role": role})
	if err := h.app.Auth.SetPassword(context.Background(), str(m, "user_id"), password); err != nil {
		t.Fatal(err)
	}
	c := h.cookieClient()
	c.login(t, email)
	return c
}

func countBlobs(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func rowText(t *testing.T, db *store.DB, query string, args ...any) string {
	t.Helper()
	var rows []map[string]any
	if err := db.NewRaw(query, args...).Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, r := range rows {
		for k, v := range r {
			if bs, ok := v.([]byte); ok {
				v = string(bs)
			}
			fmt.Fprintf(&b, "%s=%v\n", k, v)
		}
	}
	return strings.ToLower(b.String())
}

// TestAcceptance_M1_Registry is the registry scenario of m1-plan §8.3.
func TestAcceptance_M1_Registry(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		c := clock.NewFake(time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC))
		h := newRegistryHarness(t, db, c)
		ctx := context.Background()
		own, err := h.app.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
			Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Prova SRL",
			MarinaName: "Marina Registro", MarinaSlug: "registro",
		})
		if err != nil {
			t.Fatal(err)
		}
		M := A + "/marinas/" + own.MarinaID
		o := h.cookieClient()
		o.login(t, "owner@porto.test")
		pdf, err := os.ReadFile(filepath.Join("testdata", "registry", "sample.pdf"))
		if err != nil {
			t.Fatal(err)
		}

		// 1. A person, a duplicate by fiscal code, a company.
		p := o.expect(t, 201, "POST", M+"/customers", map[string]any{
			"kind": "person", "first_name": "Ottavio", "last_name": "Quintavalle", "fiscal_code": "RSSMRA85T10A562S",
			"emails":          []map[string]string{{"label": "casa", "value": "ottavio.quintavalle@example.com"}},
			"phones":          []map[string]string{{"label": "cell", "value": "+39 347 555 0101"}},
			"iban":            "IT60X0542811101000000123456",
			"billing_address": map[string]string{"line1": "Via dei Gabbiani 12", "city": "Gaeta", "country": "IT"},
			"notes":           "cliente storico",
		}, "Idempotency-Key", "acc-customer-1")
		person := p["customer"].(map[string]any)
		pid := str(person, "id")
		d := o.expect(t, 201, "POST", M+"/customers", map[string]any{"kind": "person", "first_name": "O.", "last_name": "Quintavalle", "fiscal_code": "RSSMRA85T10A562S"})
		dupID := str(d["customer"].(map[string]any), "id")
		if dups := d["possible_duplicates"].([]any); len(dups) != 1 || str(dups[0].(map[string]any), "id") != pid {
			t.Fatalf("possible duplicates %v", d["possible_duplicates"])
		}
		co := o.expect(t, 201, "POST", M+"/customers", map[string]any{"kind": "company", "company_name": "Charter Blu SRL", "vat_number": "IT01234567897", "fiscal_code": "01234567897"})
		coID := str(co["customer"].(map[string]any), "id")
		second := o.expect(t, 201, "POST", M+"/customers", map[string]any{"kind": "person", "first_name": "Livia", "last_name": "Serafini"})
		secondID := str(second["customer"].(map[string]any), "id")
		third := o.expect(t, 201, "POST", M+"/customers", map[string]any{"kind": "person", "last_name": "Terzi"})

		// 2. A vessel with owners 600 + 400 and billing by the company; a third owner share is refused.
		v := o.expect(t, 201, "POST", M+"/vessels", map[string]any{
			"name": "Libeccio Secondo", "registration": "LT 4321", "loa_cm": 1190, "beam_cm": 395, "hull_type": "monohull_sail",
			"parties": []map[string]any{
				{"customer_id": pid, "role": "owner", "share_millis": 600},
				{"customer_id": secondID, "role": "owner", "share_millis": 400},
				{"customer_id": coID, "role": "billing"},
			},
		})
		vid := str(v, "id")
		r := o.do(t, "PUT", M+"/vessels/"+vid+"/parties", map[string]any{"parties": []map[string]any{
			{"customer_id": pid, "role": "owner", "share_millis": 600},
			{"customer_id": secondID, "role": "owner", "share_millis": 400},
			{"customer_id": str(third["customer"].(map[string]any), "id"), "role": "owner", "share_millis": 100},
		}}, "If-Match", `"1"`)
		if r.status != 400 || r.problemType() != "validation_failed" || !strings.Contains(string(r.body), "owner_shares_exceed") {
			t.Fatalf("third owner: %d %s", r.status, r.body)
		}

		// 3. The dock records a measurement.
		dock := h.member(t, o, own.MarinaID, "dock@porto.test", "dock")
		m := dock.expect(t, 200, "POST", M+"/vessels/"+vid+"/measurements", map[string]any{"loa_cm": 1240, "beam_cm": 401, "draft_cm": 180})
		if str(m, "dimensions_source") != "measured" || m["effective_loa_cm"].(float64) != 1240 || str(m, "unit_class") != "imbarcazione" {
			t.Fatalf("measurement %v", m)
		}

		// 4. An insurance policy expiring in 10 days, with its PDF: a due_soon deadline appears.
		rome, err := calendar.LoadLocation("Europe/Rome")
		if err != nil {
			t.Fatal(err)
		}
		due := calendar.In(c.Now(), rome).AddDays(10).String()
		ins := o.expect(t, 201, "POST", M+"/documents", map[string]any{"owner_type": "vessel", "vessel_id": vid, "type": "insurance_policy", "number": "POL-778899", "expires_on": due})
		insID := str(ins, "id")
		if r := o.raw(t, "PUT", M+"/documents/"+insID+"/file", "application/pdf", pdf, "X-File-Name", "polizza.pdf"); r.status != 200 {
			t.Fatalf("upload: %d %s", r.status, r.body)
		}
		idDoc := o.expect(t, 201, "POST", M+"/documents", map[string]any{"owner_type": "customer", "customer_id": pid, "type": "identity_card", "number": "CA00112233", "expires_on": "2030-01-01"})
		if r := o.raw(t, "PUT", M+"/documents/"+str(idDoc, "id")+"/file", "application/pdf", pdf, "X-File-Name", "carta.pdf"); r.status != 200 {
			t.Fatalf("upload id: %d %s", r.status, r.body)
		}
		dl := o.expect(t, 200, "GET", M+"/deadlines?urgency=due_soon", nil)
		items := dl["items"].([]any)
		if len(items) != 1 || str(items[0].(map[string]any), "source_id") != insID || str(items[0].(map[string]any), "due_on") != due {
			t.Fatalf("deadlines %v", dl)
		}

		// 5. Signed download: works, then expires.
		link := o.expect(t, 200, "POST", M+"/documents/"+insID+"/file-url", nil)
		if r := o.do(t, "GET", str(link, "url"), nil); r.status != 200 || !bytes.Equal(r.body, pdf) {
			t.Fatalf("download: %d", r.status)
		}
		c.Advance(6 * time.Minute)
		if r := o.do(t, "GET", str(link, "url"), nil); r.status != 403 || r.problemType() != "download_link_expired" {
			t.Fatalf("expired download: %d %s", r.status, r.body)
		}

		// 6. Searching the vessel name finds both owners (and the billing company).
		found := o.expect(t, 200, "GET", M+"/customers?q=libeccio", nil)
		ids := map[string]bool{}
		for _, it := range found["items"].([]any) {
			ids[str(it.(map[string]any), "id")] = true
		}
		if !ids[pid] || !ids[secondID] || !ids[coID] || len(ids) != 3 {
			t.Fatalf("search by vessel %v", found)
		}

		// 7. Merge the duplicate into the person.
		target := o.do(t, "GET", M+"/customers/"+pid, nil)
		merged := o.expect(t, 200, "POST", M+"/customers/"+pid+"/merge", map[string]any{"source_ids": []string{dupID}}, "If-Match", target.header.Get("ETag"))
		if merged["version"].(float64) != 2 {
			t.Fatalf("merge %v", merged)
		}
		if src := o.expect(t, 200, "GET", M+"/customers/"+dupID, nil); str(src, "merged_into_id") != pid || src["archived_at"] == nil {
			t.Fatalf("merged source %v", src)
		}

		// 8. GDPR export.
		ex := o.do(t, "GET", M+"/customers/"+pid+"/export", nil)
		if ex.status != 200 || !strings.HasPrefix(ex.header.Get("Content-Disposition"), "attachment") {
			t.Fatalf("export: %d", ex.status)
		}
		exj := ex.json(t)
		if str(exj, "format") != "porto-customer/1" || len(exj["documents"].([]any)) != 1 || len(exj["vessels"].([]any)) != 1 ||
			!strings.Contains(string(ex.body), "CA00112233") {
			t.Fatalf("export %s", ex.body)
		}

		// 9. Anonymise: documents and blobs gone, no PII left in the row or the audit log, event in the outbox.
		blobsBefore := countBlobs(t, h.app.Files.Dir())
		o.expect(t, 204, "POST", M+"/customers/"+pid+"/anonymize", map[string]any{"confirm": true})
		if n := countBlobs(t, h.app.Files.Dir()); n != blobsBefore-1 {
			t.Fatalf("blobs %d -> %d", blobsBefore, n)
		}
		if r := o.do(t, "GET", M+"/documents/"+str(idDoc, "id"), nil); r.status != 404 {
			t.Fatalf("identity document still there: %d", r.status)
		}
		row := rowText(t, db, "SELECT * FROM customers WHERE id = ?", pid)
		audits := rowText(t, db, "SELECT * FROM audit_log")
		for _, s := range []string{
			"ottavio", "quintavalle", "rssmra85t10a562s", "ottavio.quintavalle@example.com", "3475550101",
			"it60x0542811101000000123456", "via dei gabbiani", "cliente storico", "ca00112233", "pol-778899",
		} {
			if strings.Contains(row, s) {
				t.Errorf("customer row holds %q", s)
			}
			if strings.Contains(audits, s) {
				t.Errorf("audit log holds %q", s)
			}
		}
		var anonymized int
		if err := db.NewSelect().Table("outbox_events").ColumnExpr("COUNT(*)").Where("type = ?", event.CustomerAnonymized).Scan(ctx, &anonymized); err != nil || anonymized != 1 {
			t.Fatalf("customer.anonymized events: %d %v", anonymized, err)
		}

		// 10. A viewer of the marina has no customers.read.
		viewer := h.member(t, o, own.MarinaID, "viewer@porto.test", "viewer")
		if r := viewer.do(t, "GET", M+"/customers", nil); r.status != 403 || r.problemType() != "forbidden" {
			t.Fatalf("viewer: %d %s", r.status, r.body)
		}

		// Nothing personal reached the logs (TestLogs_NoPII extended to the M1 registry run).
		h.runJobs("outbox.dispatch")
		logs := h.logs.String()
		for _, s := range []string{"Ottavio", "Quintavalle", "Livia", "Serafini", "RSSMRA85T10A562S", "IT60X", "CA00112233", "POL-778899", "+39347"} {
			if strings.Contains(logs, s) {
				t.Errorf("logs contain %q", s)
			}
		}
		assertNoPII(t, logs)
	})
}
