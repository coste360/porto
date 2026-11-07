//go:build integration

package acceptance

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M4_RenewalCampaign300 (spec 32.3 M4, m4-plan §9.4): 310 annual contracts, a renewal campaign
// previewed (300 proposals holding their berths, 10 excluded for arrears or insurance), launched (300 emails
// with the PDF and one link each), 280 accepted and signed by OTP through the public API, 10 declined, 10
// silent; deposits settled in bulk; after the deadline the silent ones expire and free their berths; at the
// year end the sources are renewed or expired. Only the API, the fake clock and the jobs; the database is read
// for the invariant checks.
func TestAcceptance_M4_RenewalCampaign300(t *testing.T) {
	if testing.Short() {
		t.Skip("long acceptance")
	}
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		const (
			nContracts = 310
			nArrears   = 5
			nInsurance = 5
			nAccept    = 280
			nEarly     = 20
			nDecline   = 10
		)
		ctx := context.Background()
		rome, _ := time.LoadLocation("Europe/Rome")
		w := newDeskWorld(t, db, 340)
		o, M, b := w.owner, w.M, w.harbour.Berths
		office := w.clients["office"]

		// 1. Set-up: settings, the Y+1 list (+5 %), customers, vessels with insurance, 310 annual contracts.
		w.contractSettings(t, map[string]any{
			"signature_driver": "otp", "insurance_policy": "warn", "default_policy": m4Policy, "required_fields": []string{},
			"bank_details": map[string]any{"iban": "IT60X0542811101000000123456", "holder": "Marina Banchina"},
		})
		lists := o.expect(t, 200, "GET", M+"/price-lists", nil)
		l2027 := str(jsonList(lists, "items")[0].(map[string]any), "id")
		next := o.expect(t, 201, "POST", M+"/price-lists/"+l2027+"/renewal", map[string]any{
			"dry_run": false, "code": "L2028", "name": map[string]string{"it": "L2028"}, "shift_years": 1, "amount_rounding": "none",
			"adjustments": []map[string]any{{"scope": "all", "percent_bp": 500}},
		})
		o.expect(t, 200, "POST", M+"/price-lists/"+str(next, "id")+"/activate", map[string]any{}, "If-Match", etagOf(next["version"]))

		emails := make([]string, nContracts)
		contracts := make([]string, nContracts)
		create := func(i int) {
			locale := "it"
			if i%3 == 0 {
				locale = "en"
			}
			emails[i] = fmt.Sprintf("cliente%03d@example.test", i)
			cust := w.mailCustomer(t, fmt.Sprintf("Cliente%03d", i), emails[i], locale)
			v := o.expect(t, 201, "POST", M+"/vessels", map[string]any{"name": fmt.Sprintf("Barca %03d", i), "loa_cm": 1000, "beam_cm": 350, "hull_type": "monohull_sail"})
			expires := "2029-06-30"
			if i >= nArrears && i < nArrears+nInsurance {
				expires = "2027-12-15" // expires before the new start
			}
			o.expect(t, 201, "POST", M+"/documents", map[string]any{"owner_type": "vessel", "vessel_id": str(v, "id"), "type": "insurance_policy", "expires_on": expires})
			ct := office.expect(t, 201, "POST", M+"/contracts", map[string]any{
				"term": "annual", "customer_id": cust, "vessel_id": str(v, "id"), "starts_on": "2027-01-01", "ends_on": "2028-01-01",
				"placements": []any{berthPlacement(b[i], "2027-01-01", "2027-12-31")}, "activate": true,
			})
			if str(ct, "status") != "active" {
				t.Fatalf("contract %d %s", i, str(ct, "status"))
			}
			contracts[i] = str(ct, "id")
		}
		// The arrears contracts are created in June: their deposit is issued and never settled.
		for i := range nArrears {
			create(i)
		}
		w.setClock(t, time.Date(2027, 10, 1, 9, 0, 0, 0, rome))
		for i := nArrears; i < nContracts; i++ {
			create(i)
		}

		// 2. Campaign preview: 300 proposals holding their berth until the deadline, 10 excluded.
		camp := office.expect(t, 201, "POST", M+"/renewal-campaigns", map[string]any{
			"name": "Rinnovi 2028", "deadline_on": "2027-11-15", "price_list_id": str(next, "id"),
			"filter": map[string]any{"terms": []string{"annual"}, "ends_from": "2028-01-01", "ends_to": "2028-01-01"},
		})
		C := M + "/renewal-campaigns/" + str(camp, "id")
		// Preview prices and holds 310 candidates synchronously (one transaction each): give the call more than
		// the default 10 s client timeout.
		office.http.Timeout = 5 * time.Minute
		started := time.Now()
		prev := office.expect(t, 200, "POST", C+"/preview", map[string]any{}, "If-Match", etagOf(camp["version"]))
		t.Logf("preview of %d contracts took %s", nContracts, time.Since(started).Round(time.Millisecond))
		office.http.Timeout = 10 * time.Second
		counts := prev["counts"].(map[string]any)
		if counts["previewed"] != float64(300) || counts["excluded_arrears"] != float64(nArrears) || counts["excluded_insurance_expired"] != float64(nInsurance) {
			t.Fatalf("preview counts %v", counts)
		}
		sim := o.expect(t, 200, "POST", M+"/pricing/simulate", map[string]any{
			"contract_type": "annual", "arrival": "2028-01-01", "departure": "2029-01-01", "vessel": map[string]any{"loa_cm": 1000, "beam_cm": 350},
		})
		engine, _ := num(sim, "total_cents")
		proposals := func(status string) []map[string]any {
			var out []map[string]any
			cursor := ""
			for {
				q := url.Values{"limit": {"200"}}
				if status != "" {
					q.Set("status", status)
				}
				if cursor != "" {
					q.Set("cursor", cursor)
				}
				page := o.expect(t, 200, "GET", C+"/proposals?"+q.Encode(), nil)
				for _, it := range jsonList(page, "items") {
					out = append(out, it.(map[string]any))
				}
				nc, _ := page["next_cursor"].(string)
				if nc == "" {
					return out
				}
				cursor = nc
			}
		}
		previewed := proposals("previewed")
		if len(previewed) != 300 {
			t.Fatalf("previewed %d", len(previewed))
		}
		for _, p := range previewed {
			nt, _ := num(p, "new_total_cents")
			ot, _ := num(p, "old_total_cents")
			if nt != engine || ot != 480_000 || str(p, "hold_expires_at") == "" || p["new_contract"] == nil {
				t.Fatalf("proposal %v (engine %d)", p, engine)
			}
		}
		invariants := func(when string) {
			t.Helper()
			vs, err := store.CheckOccupancyInvariants(ctx, db.DB, w.marina, calendar.In(w.clock.Now(), rome))
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range vs {
				t.Errorf("%s: occupancy invariant %s %v %s", when, v.Query, v.IDs, v.Detail)
			}
			cvs, err := store.CheckContractInvariants(ctx, db.DB, w.marina)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range cvs {
				t.Errorf("%s: contract invariant %s %v %s", when, v.Query, v.IDs, v.Detail)
			}
		}
		invariants("preview")

		// 3. Launch; renewals.sync twice, once concurrently ⇒ exactly 300 emails with the PDF, one link each.
		camp = o.expect(t, 200, "GET", C, nil)
		office.expect(t, 200, "POST", C+"/launch", map[string]any{}, "If-Match", etagOf(camp["version"]))
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// The job runner refuses a second run of the same job, so both call the job function: two
				// instances (or a crash and a restart) must not send twice.
				errs[i] = w.h.app.Contracts.RunRenewalsSync(ctx)
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		w.h.runJobs("renewals.sync")
		w.h.drainMail()
		byEmail := map[string]string{}
		sentCount := 0
		for _, m := range w.h.mail.Messages() {
			if m.Template != "renewal_proposal" {
				continue
			}
			sentCount++
			if len(m.Attachments) == 0 || !bytes.HasPrefix(m.Attachments[0].Content, []byte("%PDF-")) {
				t.Fatalf("proposal email without the PDF")
			}
			byEmail[m.To] = w.h.latestLink(t, m.To, "renewal_proposal", "renewal")
		}
		if sentCount != 300 || len(byEmail) != 300 {
			t.Fatalf("renewal emails %d to %d customers", sentCount, len(byEmail))
		}
		if camp = o.expect(t, 200, "GET", C, nil); str(camp, "status") != "open" {
			t.Fatalf("campaign %s", str(camp, "status"))
		}

		// 4. 280 accept and sign by OTP (20 with early payment, 10 with a wrong code first), 10 decline, 10 silent.
		pub := w.h.cookieClient()
		var renewable []int
		for i := nArrears + nInsurance; i < nContracts; i++ {
			renewable = append(renewable, i)
		}
		tick := func() { w.clock.Set(w.clock.Now().Add(time.Minute)) } // the link limiter counts per minute
		accepted := map[int]bool{}
		for k, i := range renewable[:nAccept] {
			tick()
			L := "/api/public/v1/" + linkSlug + "/links/" + byEmail[emails[i]]
			page := pub.expect(t, 200, "GET", L, nil)
			if page["renewal"] == nil {
				t.Fatalf("renewal page %v", page)
			}
			if doc := pub.do(t, "GET", L+"/document", nil); doc.status != 200 || !bytes.HasPrefix(doc.body, []byte("%PDF-")) {
				t.Fatalf("proposal PDF %d", doc.status)
			}
			plan := "standard"
			if k < nEarly {
				plan = "early_payment"
			}
			pub.expect(t, 200, "POST", L+"/renewal/accept", map[string]any{"plan": plan})
			res := signByOTP(t, w.h, pub, emails[i], k >= nEarly && k < nEarly+10)
			if str(res, "request_status") != "signed" {
				t.Fatalf("signature of %d: %v", i, res)
			}
			accepted[i] = true
		}
		for _, i := range renewable[nAccept : nAccept+nDecline] {
			tick()
			L := "/api/public/v1/" + linkSlug + "/links/" + byEmail[emails[i]]
			pub.expect(t, 200, "POST", L+"/renewal/decline", map[string]any{"reason": "vendo la barca"})
		}
		w.setClock(t, w.clock.Now().Add(time.Minute))
		w.h.drainMail()
		signed := proposals("signed")
		if len(signed) != nAccept {
			t.Fatalf("signed proposals %d", len(signed))
		}
		if n := len(proposals("declined")); n != nDecline {
			t.Fatalf("declined %d", n)
		}
		payReq := 0
		for i := range accepted {
			if len(w.h.mailTo(emails[i], "installment_payment_request")) >= 2 { // the source deposit, then the successor's
				payReq++
			}
		}
		if payReq != nAccept {
			t.Fatalf("payment request emails for %d successors", payReq)
		}
		invariants("signatures")

		// 5. Accounting settles the 280 deposits in three bulk calls (one sent twice with the same key).
		var items []map[string]any
		successors := make([]string, 0, nAccept)
		for _, p := range signed {
			sid := str(p["new_contract"].(map[string]any), "id")
			successors = append(successors, sid)
			c := o.expect(t, 200, "GET", M+"/contracts/"+sid, nil)
			if str(c, "status") != "active" || str(c, "signed_document_id") == "" {
				t.Fatalf("successor %s %s", sid, str(c, "status"))
			}
			live := liveInstallments(planOf(t, o, M, sid))
			if str(live[0], "status") != "issued" {
				t.Fatalf("successor deposit %v", live[0])
			}
			if str(p, "accepted_plan") == "early_payment" && len(live) != 1 {
				t.Fatalf("early payment plan with %d instalments", len(live))
			}
			items = append(items, map[string]any{"id": live[0]["id"], "version": live[0]["version"]})
		}
		for k := 0; k < len(items); k += 100 {
			batch := map[string]any{"items": items[k:min(k+100, len(items))], "method": "bank_transfer", "reference": fmt.Sprintf("CRO-%d", k)}
			key := fmt.Sprintf("acc-settle-%04d", k)
			r := office.expect(t, 200, "POST", M+"/installments/settle", batch, "Idempotency-Key", key)
			if n, _ := num(r, "settled"); int(n) != len(batch["items"].([]map[string]any)) {
				t.Fatalf("settled %v", r)
			}
			if k == 0 {
				again := office.expect(t, 200, "POST", M+"/installments/settle", batch, "Idempotency-Key", key)
				if again["settled"] != r["settled"] {
					t.Fatalf("replay %v", again)
				}
			}
		}
		if n := len(proposals("completed")); n != nAccept {
			t.Fatalf("completed %d", n)
		}

		// 6. After the deadline the silent proposals expire, their drafts are cancelled and the berths are free.
		w.setClock(t, time.Date(2027, 11, 16, 0, 5, 0, 0, rome))
		w.h.runJobs("renewals.sync", "contracts.expire")
		expired := proposals("expired")
		if len(expired) != 10 {
			t.Fatalf("expired %d", len(expired))
		}
		var freed []any
		for _, p := range expired {
			nc := o.expect(t, 200, "GET", M+"/contracts/"+str(p["new_contract"].(map[string]any), "id"), nil)
			if str(nc, "status") != "cancelled" {
				t.Fatalf("draft of an expired proposal %s", str(nc, "status"))
			}
			for _, l := range jsonList(o.expect(t, 200, "GET", M+"/contracts/"+str(p["source_contract"].(map[string]any), "id"), nil), "lines") {
				if lm := l.(map[string]any); str(lm, "resource_type") == "berth" {
					freed = append(freed, str(lm, "resource_id"))
				}
			}
		}
		if n, err := db.NewSelect().TableExpr("berth_occupancies").Where("berth_id IN (?)", bun.List(freed)).Where("night = ?", "2028-01-10").Count(ctx); err != nil || n != 0 || len(freed) != 10 {
			t.Fatalf("freed berths %d still occupied %d (%v)", len(freed), n, err)
		}
		probe := w.vessel(t, "Sonda", 1000, 350)
		search := o.expect(t, 200, "POST", M+"/occupancies/search", map[string]any{
			"purpose": "transit", "vessel_id": probe, "arrival": "2028-01-10", "departure": "2028-01-11", "resource_types": []string{"berth"}, "limit": 50,
		})
		if len(jsonList(search, "solutions")) == 0 {
			t.Fatal("no free berth found for 2028-01-10")
		}
		if camp = o.expect(t, 200, "GET", C, nil); str(camp, "status") != "closed" {
			t.Fatalf("campaign after the deadline %s", str(camp, "status"))
		}
		for _, st := range []string{"previewed", "proposed", "sent", "accepted", "signed"} {
			if n := len(proposals(st)); n != 0 {
				t.Fatalf("%d proposals still %s", n, st)
			}
		}

		// 7. Year end: sources renewed or expired; the quarterly instalments are issued on their dates.
		w.setClock(t, time.Date(2028, 1, 1, 0, 10, 0, 0, rome))
		w.h.runJobs("contracts.expire")
		statuses := map[string]int{}
		for _, id := range contracts {
			statuses[str(o.expect(t, 200, "GET", M+"/contracts/"+id, nil), "status")]++
		}
		if statuses["renewed"] != nAccept || statuses["expired"] != nContracts-nAccept {
			t.Fatalf("source statuses %v", statuses)
		}
		w.setClock(t, time.Date(2028, 3, 20, 9, 0, 0, 0, rome))
		w.h.runJobs("installments.issue")
		issuedLater := 0
		for _, sid := range successors {
			for _, it := range liveInstallments(planOf(t, o, M, sid)) {
				if seq, _ := num(it, "seq"); seq > 1 && str(it, "status") == "issued" {
					issuedLater++
					break
				}
			}
		}
		if issuedLater == 0 {
			t.Fatal("no quarterly instalment issued")
		}

		// 8. Events, personal data, invariants.
		for typ, want := range map[string]int{"contract.signed": nAccept, "contract.renewed": nAccept} {
			if n := len(eventsOf(t, db, typ)); n != want {
				t.Errorf("%s events %d, want %d", typ, n, want)
			}
		}
		var events []store.OutboxEvent
		if err := db.NewSelect().Model(&events).Column("type", "payload").Scan(ctx); err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			p := string(e.Payload)
			if strings.Contains(p, "@") || strings.Contains(p, "Cliente") || strings.Contains(p, byEmail[emails[nArrears+nInsurance]]) {
				t.Fatalf("event %s carries personal data", e.Type)
			}
		}
		assertNoPII(t, w.h.logs.String(), emails[nArrears+nInsurance], byEmail[emails[nArrears+nInsurance]], "Cliente010")
		invariants("year end")
		t.Logf("contracts %d, proposals %d (signed %d, declined %d, expired %d), excluded %d, sources %v, later instalments issued on %d successors",
			nContracts, 300, nAccept, nDecline, len(expired), nArrears+nInsurance, statuses, issuedLater)
	})
}
