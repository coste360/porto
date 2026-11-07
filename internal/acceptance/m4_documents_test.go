//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M4_DocumentsDeadlinesComms (m4-plan §9.4): a template version with a field error is refused with
// its line and column, fixed, previewed on the demo data and on a real contract and published; a generated
// document records the version and the hash; the deadline sync run twice gives the same rows; lead-day
// notifications go out once per lead day and channel across clock advances; the insurance policy block stops a
// contract without a valid policy; a promotional communication skips the customers without the marketing
// consent; the unsubscribe link removes a customer from the next promotional send but not from a service send;
// the map print returns a PDF.
func TestAcceptance_M4_DocumentsDeadlinesComms(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 4)
		o, M := w.owner, w.M
		ctx := context.Background()
		b := w.harbour.Berths

		// Templates: error with line and column, fix, previews, publish.
		tp := o.expect(t, 201, "POST", M+"/templates", map[string]any{"kind": "contract_annual", "code": "annuale", "name": "Annuale", "is_default": true})
		T := M + "/templates/" + str(tp, "id")
		bad := o.do(t, "POST", T+"/versions", map[string]any{"locale": "it", "body_md": "# Contratto\n\nCliente: {{customer.nome}}"})
		if bad.status != 422 || bad.problemType() != "template_invalid" || !bytes.Contains(bad.body, []byte(`"line":3`)) || !bytes.Contains(bad.body, []byte(`"col":10`)) {
			t.Fatalf("invalid version %d %s", bad.status, bad.body)
		}
		body := "# Contratto {{contract.code}}\n\nCliente: **{{customer.display_name}}**, unità {{vessel.name}} ({{vessel.loa | meters}}).\n\n" +
			"| Rata | Scadenza | Importo |\n|---:|---|---:|\n{{#each installments}}| {{this.seq}} | {{this.due_on | date}} | {{this.amount | money}} |\n{{/each}}\n\n{{signature \"customer\"}}\n"
		v := o.expect(t, 201, "POST", T+"/versions", map[string]any{"locale": "it", "body_md": body})
		if pdf := o.do(t, "POST", T+"/preview", map[string]any{"locale": "it", "version": v["version"], "sample": "demo"}); pdf.status != 200 || !bytes.HasPrefix(pdf.body, []byte("%PDF-")) {
			t.Fatalf("demo preview %d", pdf.status)
		}
		rossi := w.customer(t, "Rossi")
		vela := w.vessel(t, "Vela", 1100, 380)
		ct := o.expect(t, 201, "POST", M+"/contracts", map[string]any{
			"term": "annual", "customer_id": rossi, "vessel_id": vela, "starts_on": "2027-07-01",
			"placements": []any{berthPlacement(b[0], "2027-07-01", "2028-06-30")},
		})
		onRecord := o.do(t, "POST", T+"/preview", map[string]any{"locale": "it", "version": v["version"], "sample": map[string]any{"subject_type": "contract", "subject_id": str(ct, "id")}})
		if onRecord.status != 200 || !bytes.HasPrefix(onRecord.body, []byte("%PDF-")) {
			t.Fatalf("real preview %d %s", onRecord.status, onRecord.body)
		}
		pub := o.expect(t, 200, "POST", T+"/versions/1/publish", map[string]any{"locale": "it"})
		if str(pub, "status") != "published" || str(pub, "sha256") == "" {
			t.Fatalf("publish %v", pub)
		}

		// A generated document records the version and the hash.
		scope, err := store.SystemMarinaScope(ctx, db.DB, w.marina)
		if err != nil {
			t.Fatal(err)
		}
		var docID string
		if err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			docID, err = w.h.app.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
				Kind: docs.KindContractAnnual, SubjectType: docs.SubjectContract, SubjectID: str(ct, "id"), Locale: "it", PDFA: true,
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		w.h.runJobs("documents.render")
		gd := o.expect(t, 200, "GET", M+"/generated-documents/"+docID, nil)
		tv, _ := gd["template_version"].(map[string]any)
		if str(gd, "status") != "ready" || str(gd, "sha256") == "" || tv == nil || str(tv, "template_id") != str(tp, "id") || tv["version"] != float64(1) {
			t.Fatalf("generated document %v", gd)
		}

		// Deadlines: an obligation in 20 days, office email notifications at 10 and 3 days.
		set := o.expect(t, 200, "GET", M+"/contract-settings", nil)
		o.expect(t, 200, "PUT", M+"/contract-settings", map[string]any{"office_email": "ufficio@porto.test", "insurance_policy": "block"}, "If-Match", etagOf(set["version"]))
		now := store.UTC(w.clock.Now())
		today := calendar.In(now, time.UTC)
		conc := &store.Concession{ID: store.NewID(), MarinaID: w.marina, ExpiresOn: today.AddDays(900), Surfaces: store.JSON("[]"), Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now}
		obl := &store.ConcessionObligation{ID: store.NewID(), MarinaID: w.marina, ConcessionID: conc.ID, Kind: "periodic_report", DueOn: today.AddDays(20), Recurrence: "none", Version: 1, CreatedAt: now, UpdatedAt: now}
		for _, m := range []any{conc, obl} {
			if _, err := db.NewInsert().Model(m).Exec(ctx); err != nil {
				t.Fatal(err)
			}
		}
		o.expect(t, 200, "PUT", M+"/deadline-policies", map[string]any{"items": []map[string]any{
			{"kind": "concession_obligation", "lead_days": []int{10, 3}, "channels": []string{"panel", "office_email"}},
		}})
		first := o.expect(t, 200, "POST", M+"/deadlines/sync", nil)
		second := o.expect(t, 200, "POST", M+"/deadlines/sync", nil)
		if first["created"] == float64(0) || second["created"] != float64(0) || second["updated"] != float64(0) || second["closed"] != float64(0) {
			t.Fatalf("sync %v then %v", first, second)
		}
		notices := func() int {
			n, err := db.NewSelect().Table("message_log").Where("template = ?", "deadline_notice_office").Count(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		steps := []struct{ days, want int }{{0, 0}, {11, 1}, {1, 1}, {6, 2}, {5, 2}}
		if !w.h.app.Deadlines.EmailEnabled() {
			t.Fatal("deadline emails not wired in internal/app")
		}
		for _, step := range steps {
			w.setClock(t, w.clock.Now().Add(time.Duration(step.days)*24*time.Hour))
			w.h.runJobs("deadlines.notify", "deadlines.notify")
			if got := notices(); got != step.want {
				t.Fatalf("after %d days: %d office notices, want %d", step.days, got, step.want)
			}
		}

		// Communications: consent rules and unsubscribe.
		mkCustomer := func(last, email string, marketing bool) string {
			c := o.expect(t, 201, "POST", M+"/customers", map[string]any{
				"kind": "person", "first_name": "Anna", "last_name": last,
				"emails": []map[string]string{{"label": "home", "value": email}},
			})
			id := str(c["customer"].(map[string]any), "id")
			if marketing {
				o.expect(t, 201, "POST", M+"/customers/"+id+"/consents", map[string]any{"kind": "marketing", "granted": true, "policy_version": "v1", "source": "desk"})
			}
			return id
		}
		yes, no := mkCustomer("Consenso", "si@example.test", true), mkCustomer("Nessuno", "no@example.test", false)
		send := func(category string) map[string]any {
			c := o.expect(t, 201, "POST", M+"/communications", map[string]any{
				"name": "Offerta", "category": category, "audience": map[string]any{"customer_ids": []string{yes, no}},
				"subject": "Per {{customer.first_name}}", "body_md": "Offerta del porto {{marina.name}}.",
			})
			o.expect(t, 200, "POST", M+"/communications/"+str(c, "id")+"/send", nil, "If-Match", etagOf(c["version"]))
			w.h.runJobs("comms.send", "comms.send")
			return o.expect(t, 200, "GET", M+"/communications/"+str(c, "id"), nil)
		}
		promo := send("promotional")
		counts := promo["counts"].(map[string]any)
		if str(promo, "status") != "sent" || counts["queued"] != float64(1) || counts["skipped_no_consent"] != float64(1) {
			t.Fatalf("promotional %v", promo)
		}
		w.h.runJobs("email.deliver")
		var token string
		for _, m := range w.h.mail.Messages() {
			if m.To == "si@example.test" && m.Template == "communication" {
				if mm := regexp.MustCompile(`/m/unsubscribe/([0-9A-Z]{52})`).FindStringSubmatch(m.Text); mm != nil {
					token = mm[1]
				}
			}
		}
		if token == "" {
			t.Fatal("no unsubscribe link in the promotional email")
		}
		pub2 := w.h.cookieClient()
		link := pub2.expect(t, 200, "GET", "/api/public/v1/banchina/links/"+token, nil)
		if u, _ := link["unsubscribe"].(map[string]any); u == nil || u["subscribed"] != true || str(link, "purpose") != magiclink.PurposeUnsubscribe {
			t.Fatalf("public link %v", link)
		}
		req, _ := http.NewRequest(http.MethodPost, w.h.srv.URL+"/api/public/v1/banchina/links/"+token+"/unsubscribe", strings.NewReader("List-Unsubscribe=One-Click"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 204 {
			t.Fatalf("one-click unsubscribe %d", resp.StatusCode)
		}
		if again := send("promotional")["counts"].(map[string]any); again["queued"] != nil && again["queued"] != float64(0) {
			t.Fatalf("unsubscribed customer still reached: %v", again)
		}
		pub2.expect(t, 404, "GET", "/api/public/v1/marina-altra/links/"+token, nil)

		// Map print.
		maps := o.expect(t, 200, "GET", M+"/maps", nil)
		mapID := str(jsonList(maps, "items")[0].(map[string]any), "id")
		if pdf := o.do(t, "GET", M+"/maps/"+mapID+"/print?date=2027-07-02", nil); pdf.status != 200 || !bytes.HasPrefix(pdf.body, []byte("%PDF-")) {
			t.Fatalf("map print %d", pdf.status)
		}
		// Insurance policy block (set above): no valid policy on the start, no new contract (contract calls
		// deadline.InsuranceValid).
		res := o.do(t, "POST", M+"/contracts", map[string]any{
			"term": "annual", "customer_id": rossi, "vessel_id": vela, "starts_on": "2027-08-01",
			"placements": []any{berthPlacement(b[1], "2027-08-01", "2028-07-31")},
		})
		if res.status != 422 || res.problemType() != "insurance_required" {
			t.Fatalf("contract without insurance %d %s", res.status, res.body)
		}
	})
}

// TestPublicLinks_CrossMarina: a token of marina A used with the slug of marina B is 404 link_invalid, the same
// answer as an unknown or expired token (INV-75).
func TestPublicLinks_CrossMarina(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 1)
		ctx := context.Background()
		other := w.h.app.Auth
		if _, err := other.CreateOwner(ctx, authOwner("altra")); err != nil {
			t.Fatal(err)
		}
		scope, err := store.SystemMarinaScope(ctx, db.DB, w.marina)
		if err != nil {
			t.Fatal(err)
		}
		cust := w.customer(t, "Rossi")
		var token string
		if err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			token, _, err = w.h.app.Links.CreateTx(ctx, tx, scope, magiclink.PurposeUnsubscribe, "customer", cust, cust, w.clock.Now().Add(time.Hour))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		c := w.h.cookieClient()
		c.expect(t, 200, "GET", "/api/public/v1/banchina/links/"+token, nil)
		for _, path := range []string{
			"/api/public/v1/altra/links/" + token,
			"/api/public/v1/banchina/links/" + strings.Repeat("A", 52),
			"/api/public/v1/banchina/links/short",
		} {
			if r := c.do(t, "GET", path, nil); r.status != 404 || r.problemType() != "link_invalid" {
				t.Fatalf("%s: %d %s", path, r.status, r.body)
			}
		}
		if r := c.do(t, "POST", "/api/public/v1/altra/links/"+token+"/unsubscribe", nil); r.status != 404 {
			t.Fatalf("cross-marina unsubscribe %d", r.status)
		}
		w.setClock(t, w.clock.Now().Add(2*time.Hour))
		if r := c.do(t, "GET", "/api/public/v1/banchina/links/"+token, nil); r.status != 404 || r.problemType() != "link_invalid" {
			t.Fatalf("expired: %d", r.status)
		}
	})
}

func authOwner(slug string) auth.CreateOwnerInput {
	return auth.CreateOwnerInput{
		Email: "owner-" + slug + "@porto.test", Name: "Owner", Password: password, Organization: "Org " + slug,
		MarinaName: "Marina " + slug, MarinaSlug: slug,
	}
}
