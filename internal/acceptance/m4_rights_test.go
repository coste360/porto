//go:build integration

package acceptance

import (
	"context"
	"testing"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M4_RightsLifecycle (m4-plan §9.4): a right with 2 holders writes its owner rows; a July–August
// mandate releases them; a transit sold inside the mandate is accrued by mandates.accrue into the statement; a fee
// run is simulated (one right excluded with its reason) and issued into holder instalments; a transfer of the
// right moves the owner rows and reassigns the future instalments; the rights lens returns the statuses; roles
// and isolation hold. The transfer is signed by OTP through the customers' links, one signer after the other.
func TestAcceptance_M4_RightsLifecycle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 6)
		o, M := w.owner, w.M
		ctx := context.Background()
		b := w.harbour.Berths
		rossi := w.mailCustomer(t, "Rossi", "rossi@example.test", "it")
		bianchi := w.mailCustomer(t, "Bianchi", "bianchi@example.test", "it")
		verdi := w.mailCustomer(t, "Verdi", "verdi@example.test", "en")
		vela := w.vessel(t, "Vela", 1100, 380)
		ownerRows := func(berth string) int {
			n, err := db.NewSelect().TableExpr("berth_occupancies").Where("berth_id = ?", berth).Where("kind = 'owner'").Count(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return int(n)
		}
		// right with two holders: owner rows from today (2027-06-01) to 31 December
		r := o.expect(t, 201, "POST", M+"/rights", map[string]any{
			"berth_id": b[0], "kind": "ownership_share", "from_on": "2027-01-01", "to_on": "2027-12-31",
			"title":   map[string]any{"deed": "Atto", "number": "7/2020", "notary": "Notaio"},
			"holders": []map[string]any{{"customer_id": rossi, "share_millis": 600}, {"customer_id": bianchi, "share_millis": 400}},
			"vessels": []map[string]any{{"vessel_id": vela, "primary": true}},
		})
		rightID := str(r, "id")
		if n := ownerRows(b[0]); n != 214 {
			t.Fatalf("owner rows %d", n)
		}
		o.expect(t, 201, "POST", M+"/rights", map[string]any{
			"berth_id": b[1], "kind": "long_term_use", "from_on": "2027-01-01", "to_on": "2027-12-31",
			"title": map[string]any{}, "holders": []map[string]any{{"customer_id": verdi, "share_millis": 1000}},
		})
		// a third right on a long berth that the band table does not cover
		if _, err := db.NewUpdate().Table("berths").Set("length_cm = 2000").Where("id = ?", b[2]).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		r3 := o.expect(t, 201, "POST", M+"/rights", map[string]any{
			"berth_id": b[2], "kind": "membership", "from_on": "2027-01-01", "to_on": "2027-12-31",
			"title": map[string]any{}, "holders": []map[string]any{{"customer_id": verdi, "share_millis": 1000}},
		})

		// roles: the dock cannot manage rights; another marina sees nothing
		w.clients["dock"].expect(t, 403, "GET", M+"/rights", nil)
		w.clients["viewer"].expect(t, 403, "POST", M+"/mandates", map[string]any{
			"right_id": rightID, "from_on": "2027-07-01", "to_on": "2027-08-31",
			"owner_share_bp": 6000, "channels": []string{"transit"}, "settlement": "credit",
		})

		// mandate July–August: the nights are released, a transit is sold inside it
		mand := o.expect(t, 201, "POST", M+"/mandates", map[string]any{
			"right_id": rightID, "from_on": "2027-07-01", "to_on": "2027-08-31",
			"owner_share_bp": 6000, "min_guaranteed_cents": 50_000, "channels": []string{"transit"}, "settlement": "credit",
		})
		if n := ownerRows(b[0]); n != 214-62 {
			t.Fatalf("owner rows after mandate %d", n)
		}
		tr := o.expect(t, 201, "POST", M+"/transits", map[string]any{
			"vessel_id": vela, "customer_id": verdi, "arrival": "2027-07-10", "departure": "2027-07-17",
			"placements": []any{berthPlacement(b[0], "2027-07-10", "2027-07-16")}, "confirm": map[string]any{},
		})
		w.h.runJobs("mandates.accrue")
		st := o.expect(t, 200, "GET", M+"/mandates/"+str(mand, "id")+"/statement", nil)
		total, _ := num(tr, "total_cents")
		if rev, _ := num(st, "revenue_cents"); rev != total || len(jsonList(st, "lines")) != 1 {
			t.Fatalf("statement %v, transit total %d", st, total)
		}
		if n := len(eventsOf(t, db, event.MandateRevenueAccrued)); n != 1 {
			t.Fatalf("revenue events %d", n)
		}
		w.h.runJobs("mandates.accrue") // once per marina day
		if n := len(eventsOf(t, db, event.MandateRevenueAccrued)); n != 1 {
			t.Fatalf("revenue events after a second run %d", n)
		}

		// fee run: band table [10 m, 15 m) at 800 €; the 20 m berth is excluded with no_rate
		ft := o.expect(t, 201, "POST", M+"/fee-tables", map[string]any{
			"year": 2027, "name": "Oneri 2027", "basis": "per_length_band",
			"rows": []map[string]any{{"from_cm": 1000, "to_cm": 1500, "amount_cents": 80_000}},
		})
		o.expect(t, 200, "POST", M+"/fee-tables/"+str(ft, "id")+"/activate", nil, "If-Match", `"1"`)
		run := o.expect(t, 201, "POST", M+"/fee-runs", map[string]any{
			"year": 2027, "table_id": str(ft, "id"),
			"plan": map[string]any{"schedule": map[string]any{"mode": "fixed_dates", "dates": []map[string]any{{"due_on": "06-30", "bp": 5000}, {"due_on": "12-15", "bp": 5000}}}},
		})
		sim := o.do(t, "POST", M+"/fee-runs/"+str(run, "id")+"/simulate", nil, "If-Match", `"1"`)
		if sim.status != 200 {
			t.Fatalf("simulate %d %s", sim.status, sim.body)
		}
		simj := sim.json(t)
		excl := jsonList(simj, "excluded")
		if len(excl) != 1 || str(excl[0].(map[string]any), "right_id") != str(r3, "id") || str(excl[0].(map[string]any), "reason") != "no_rate" {
			t.Fatalf("excluded %v", excl)
		}
		if tot, _ := num(simj, "total_cents"); tot != 160_000 {
			t.Fatalf("fee total %d", tot)
		}
		o.expect(t, 200, "POST", M+"/fee-runs/"+str(run, "id")+"/issue", nil, "If-Match", sim.header.Get("ETag"))
		hi := o.expect(t, 200, "GET", M+"/holder-installments?right_id="+rightID, nil)
		if n := len(jsonList(hi, "items")); n != 4 { // two holders × two semesters
			t.Fatalf("holder instalments %d", n)
		}

		// transfer of the right on 1 September to Verdi, signed by OTP in sequence: the two outgoing holders, the
		// incoming holder, then the marina; the last signature completes the transfer.
		trf := o.expect(t, 201, "POST", M+"/right-transfers", map[string]any{
			"right_id": rightID, "effective_on": "2027-09-01",
			"to_holders": []map[string]any{{"customer_id": verdi, "share_millis": 1000}}, "fees_policy": "reassign",
		})
		o.expect(t, 200, "POST", M+"/right-transfers/"+str(trf, "id")+"/request-signature", map[string]any{"driver": "otp"}, "If-Match", `"1"`)
		pub := w.h.cookieClient()
		signers := []string{"rossi@example.test", "bianchi@example.test", "verdi@example.test", "owner@porto.test"}
		for k, email := range signers {
			res := signByOTP(t, w.h, pub, email, false)
			want := "signed"
			if k < len(signers)-1 {
				want = "open"
				if st := str(res, "request_status"); st == want || st == "in_progress" || st == "partially_signed" {
					continue
				}
				t.Fatalf("signer %d: %v", k, res)
			}
			if str(res, "request_status") != want {
				t.Fatalf("last signer: %v", res)
			}
		}
		done := o.expect(t, 200, "GET", M+"/right-transfers/"+str(trf, "id"), nil)
		if str(done, "status") != "completed" {
			t.Fatalf("transfer after the signatures %v", done)
		}
		newRight := str(done, "new_right_id")
		if newRight == "" || ownerRows(b[0]) != 214-62 {
			t.Fatalf("transfer %v rows %d", done, ownerRows(b[0]))
		}
		hi = o.expect(t, 200, "GET", M+"/holder-installments?status=scheduled&right_id="+newRight, nil)
		items := jsonList(hi, "items")
		if len(items) != 1 {
			t.Fatalf("reassigned instalments %v", items)
		}
		if amt, _ := num(items[0].(map[string]any), "amount_cents"); amt != 40_000 {
			t.Fatalf("reassigned amount %d", amt)
		}
		hist := o.expect(t, 200, "GET", M+"/berths/"+b[0]+"/right-history", nil)
		if n := len(jsonList(hist, "items")); n != 2 {
			t.Fatalf("history %d", n)
		}

		// the rights lens on 15 July: mandate on b0, holder on b1 and b2, marina elsewhere; no names
		lens := o.expect(t, 200, "GET", M+"/maps/"+w.harbour.MapID+"/rights?date=2027-07-15", nil)
		status := map[string]string{}
		for _, it := range jsonList(lens, "items") {
			m := it.(map[string]any)
			status[str(m, "berth_id")] = str(m, "status")
			if _, ok := m["display_name"]; ok {
				t.Fatal("lens carries names")
			}
		}
		if status[b[0]] != "mandate" || status[b[1]] != "holder" || status[b[2]] != "holder" || status[b[3]] != "marina" {
			t.Fatalf("lens %v", status)
		}
		w.clients["dock"].expect(t, 200, "GET", M+"/maps/"+w.harbour.MapID+"/rights", nil)

		vs, err := store.CheckOccupancyInvariants(ctx, db.DB, w.marina, calendar.MustParse("2027-06-01"))
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			t.Errorf("invariant %s %v %s", v.Query, v.IDs, v.Detail)
		}
	})
}
