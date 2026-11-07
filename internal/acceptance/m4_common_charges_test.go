//go:build integration

package acceptance

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/csvimport"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// TestAcceptance_M4_CommonCharges400 is the common expenses acceptance of spec 32.3 M4 (m4-plan §9.4): 400
// berths, 380 rights (370 by CSV import, 10 by API; single, double and triple holders), 20 berths of the marina;
// three millesimal tables by CSV (Σ 999 refused, then fixed); a budget with 7 items with odd cents; an advance run
// whose allocations sum exactly to round_half_even(item × 0.5) per item; actuals by CSV with one item below its
// advance; a balance run equal to Σ actual − Σ advances exactly, with credits; a transfer and a mandate during the
// year that do not change the sums; two concurrent issues of the balance run; invariants and no personal data in
// events.
func TestAcceptance_M4_CommonCharges400(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		w := newDeskWorld(t, db, 400)
		o, M := w.owner, w.M
		ctx := context.Background()
		scope, err := store.ResolveMarinaScope(ctx, db.DB, w.marina)
		if err != nil {
			t.Fatal(err)
		}
		berths := w.harbour.Berths // codes A1..A400
		code := func(i int) string { return "A" + strconv.Itoa(i+1) }

		// 1. concession expiring Y+40; holders inserted through the store with a tax id for the import
		o.expect(t, 200, "PUT", M+"/concession", map[string]any{"authority": "Autorità portuale", "expires_on": "2067-12-31"}, "If-Match", `"0"`)
		const nCustomers = 120
		tax := make([]string, nCustomers)
		ids := make([]string, nCustomers)
		now := store.UTC(w.clock.Now())
		for i := range nCustomers {
			tax[i] = fmt.Sprintf("TAX%05d", i)
			c := &store.Customer{
				ID: store.NewID(), MarinaID: w.marina, Kind: "person", LastName: "Socio", FirstName: strconv.Itoa(i),
				FiscalCode: tax[i], BillingAddress: store.JSON(`{}`), Locale: "it", Emails: store.JSON(`[]`), Phones: store.JSON(`[]`),
				Tags: store.Strings{}, SearchText: "socio", Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			if _, err := db.NewInsert().Model(c).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			ids[i] = c.ID
		}
		// 300 single (berths 0..299), 60 double (300..359: 500/500 or 333/667), 20 triple (360..379); 380..399 the marina's
		type holder struct {
			c     int
			share int64
		}
		plan := make([][]holder, 380)
		for i := range 380 {
			c := i % nCustomers
			switch {
			case i < 300:
				plan[i] = []holder{{c, 1000}}
			case i < 360 && i%2 == 0:
				plan[i] = []holder{{c, 500}, {(c + 1) % nCustomers, 500}}
			case i < 360:
				plan[i] = []holder{{c, 333}, {(c + 1) % nCustomers, 667}}
			default:
				plan[i] = []holder{{c, 333}, {(c + 1) % nCustomers, 333}, {(c + 2) % nCustomers, 334}}
			}
		}
		var csv strings.Builder
		csv.WriteString("posto;tipo;dal;al;cf;millesimi\n")
		for i := range 370 {
			for _, h := range plan[i] {
				fmt.Fprintf(&csv, "%s;ownership_share;2027-01-01;2027-12-31;%s;%d\n", code(i), tax[h.c], h.share)
			}
		}
		imp := map[string]any{
			"entity": "rights", "csv": csv.String(), "has_header": true, "dry_run": true,
			"mapping": map[string]int{"berth_code": 0, "kind": 1, "from_on": 2, "to_on": 3, "holder_tax_id": 4, "share_millis": 5},
		}
		if r := o.do(t, "POST", M+"/imports", imp); r.status == http.StatusNotImplemented {
			// the generic import endpoint is DOCUMENTS'; until it lands the rights handler is driven directly
			t.Log("imports endpoint not implemented yet: applying the rights import handler directly")
			tab, err := csvimport.Parse(csv.String(), ';', true)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := tab.Mapped(csvimport.Mapping{"berth_code": 0, "kind": 1, "from_on": 2, "to_on": 3, "holder_tax_id": 4, "share_millis": 5})
			if err != nil {
				t.Fatal(err)
			}
			h := w.h.app.Rights.ImportHandler()
			rep := &csvimport.Report{Entity: csvimport.EntityRights}
			if err := h.DryRun(ctx, db.DB, scope, rows, rep); err != nil || len(rep.Rejected) != 0 {
				t.Fatalf("dry run %v %+v", err, rep.Rejected)
			}
			rep = &csvimport.Report{Entity: csvimport.EntityRights}
			if err := db.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
				_, err := h.Apply(ctx, tx, scope, rows, rep)
				return err
			}); err != nil || len(rep.Rejected) != 0 {
				t.Fatalf("apply %v %+v", err, rep.Rejected)
			}
		} else {
			rep := r.json(t)
			if r.status != 200 || len(jsonList(rep, "rejected")) != 0 {
				t.Fatalf("dry run %d %s", r.status, r.body)
			}
			imp["dry_run"] = false
			rep = o.expect(t, 200, "POST", M+"/imports", imp)
			if len(jsonList(rep, "rejected")) != 0 {
				t.Fatalf("import rejected %v", rep["rejected"])
			}
		}
		rightOf := map[int]string{}
		for i := 370; i < 380; i++ {
			var hs []map[string]any
			for _, h := range plan[i] {
				hs = append(hs, map[string]any{"customer_id": ids[h.c], "share_millis": h.share})
			}
			r := o.expect(t, 201, "POST", M+"/rights", map[string]any{
				"berth_id": berths[i], "kind": "membership", "from_on": "2027-01-01",
				"to_on": "2027-12-31", "title": map[string]any{}, "holders": hs,
			})
			rightOf[i] = str(r, "id")
		}
		list := o.expect(t, 200, "GET", M+"/rights?limit=200", nil)
		total := len(jsonList(list, "items"))
		for next := str(list, "next_cursor"); next != ""; next = str(list, "next_cursor") {
			list = o.expect(t, 200, "GET", M+"/rights?limit=200&cursor="+next, nil)
			total += len(jsonList(list, "items"))
		}
		if total != 380 {
			t.Fatalf("rights %d", total)
		}

		// 2. three millesimal tables by CSV: general (400, uneven with zeros), pier B (A81..A160), services (400)
		rng := rand.New(rand.NewPCG(7, 11))
		weights := func(n int) []int64 {
			w := make([]int64, n)
			for i := range w {
				w[i] = int64(rng.IntN(6)) // zeros included
			}
			w[0]++
			return w
		}
		tableCSV := func(from, n int, millis []money.Cents) string {
			var b strings.Builder
			b.WriteString("posto;millesimi\n")
			for i := range n {
				fmt.Fprintf(&b, "%s;%d\n", code(from+i), millis[i])
			}
			return b.String()
		}
		makeTable := func(name, tcode string, from, n int) string {
			millis, err := money.Allocate(1000, weights(n))
			if err != nil {
				t.Fatal(err)
			}
			tb := o.expect(t, 201, "POST", M+"/millesimal-tables", map[string]any{"name": name, "code": tcode})
			id := str(tb, "id")
			// first with one milli missing: Σ 999 is refused at activation
			broken := append([]money.Cents(nil), millis...)
			for i := range broken {
				if broken[i] > 0 {
					broken[i]--
					break
				}
			}
			r := o.do(t, "PUT", M+"/millesimal-tables/"+id+"/entries", map[string]any{"csv": tableCSV(from, n, broken)}, "If-Match", `"1"`)
			if r.status != 200 {
				t.Fatalf("entries: %d %s", r.status, r.body)
			}
			if a := o.do(t, "POST", M+"/millesimal-tables/"+id+"/activate", nil, "If-Match", r.header.Get("ETag")); a.status != 422 || a.problemType() != "table_incomplete" {
				t.Fatalf("Σ 999 activation: %d %s", a.status, a.body)
			}
			r = o.do(t, "PUT", M+"/millesimal-tables/"+id+"/entries", map[string]any{"csv": tableCSV(from, n, millis)}, "If-Match", r.header.Get("ETag"))
			if r.status != 200 || int(r.json(t)["sum_millis"].(float64)) != 1000 {
				t.Fatalf("entries fixed: %d %s", r.status, r.body)
			}
			o.expect(t, 200, "POST", M+"/millesimal-tables/"+id+"/activate", nil, "If-Match", r.header.Get("ETag"))
			return id
		}
		general := makeTable("Generale", "general", 0, 400)
		pierB := makeTable("Pontile B", "pier_B", 80, 80)
		services := makeTable("Servizi", "services", 0, 400)

		// 3. budget with 7 items, odd cents
		bud := o.expect(t, 201, "POST", M+"/common-charge-budgets", map[string]any{"year": 2027, "name": "Spese comuni 2027"})
		budgetID := str(bud, "id")
		items := []struct {
			name   string
			table  string
			amount int64
			actual int64
		}{
			{"Pulizia", general, 1_234_567, 1_300_001},
			{"Luce", general, 9_876_543, 4_000_000},
			{"Acqua", services, 1, 3},
			{"Vigilanza", general, 4_500_011, 4_500_011},
			{"Manutenzione B", pierB, 777_777, 1_000_001},
			{"Rifiuti", services, 333_333, 400_000},
			{"Assicurazione", services, 123_457, 99_999},
		}
		itemID := map[string]string{}
		var sumItems int64
		for _, it := range items {
			r := o.expect(t, 201, "POST", M+"/common-charge-budgets/"+budgetID+"/items", map[string]any{"name": it.name, "table_id": it.table, "budget_cents": it.amount})
			itemID[it.name] = str(r, "id")
		}
		o.expect(t, 200, "POST", M+"/common-charge-budgets/"+budgetID+"/approve", nil, "If-Match", `"1"`)
		adv := o.expect(t, 201, "POST", M+"/common-charge-budgets/"+budgetID+"/runs", map[string]any{"kind": "advance", "share_bp": 5000})
		advID := str(adv, "id")
		allocs, err := store.AllocationsOfRun(ctx, db.DB, scope, advID, store.AllocationFilterCommon{})
		if err != nil {
			t.Fatal(err)
		}
		perItem := map[string]int64{}
		var runTotal, marina int64
		for _, a := range allocs {
			perItem[a.ItemID] += a.AmountCents
			runTotal += a.AmountCents
			if a.CustomerID == "" {
				marina += a.AmountCents
			}
		}
		for _, it := range items {
			want := int64(money.Cents(it.amount).PercentBP(5000))
			if perItem[itemID[it.name]] != want {
				t.Fatalf("advance %s: Σ %d, want %d", it.name, perItem[itemID[it.name]], want)
			}
			sumItems += want
		}
		if runTotal != sumItems || int64(adv["total_cents"].(float64)) != sumItems || marina == 0 {
			t.Fatalf("advance run %d, items %d, marina %d", runTotal, sumItems, marina)
		}
		o.expect(t, 200, "POST", M+"/common-charge-runs/"+advID+"/issue", nil, "If-Match", `"1"`)
		inst, err := store.AllHolderInstallments(ctx, db.DB, scope, store.HolderInstallmentFilter{RunID: advID})
		if err != nil {
			t.Fatal(err)
		}
		var instSum int64
		for _, x := range inst {
			instSum += x.AmountCents
		}
		if instSum != runTotal-marina {
			t.Fatalf("advance instalments Σ %d, holders %d", instSum, runTotal-marina)
		}

		// 5. a transfer of a co-held right and a mandate on another, during the year
		w.setClock(t, w.clock.Now().AddDate(0, 1, 0)) // July
		o = w.owner
		coHeld := rightOf[372] // triple holders
		tr := o.expect(t, 201, "POST", M+"/right-transfers", map[string]any{
			"right_id": coHeld, "effective_on": "2027-09-01",
			"to_holders": []map[string]any{{"customer_id": ids[0], "share_millis": 1000}}, "fees_policy": "reassign",
		})
		o.expect(t, 200, "POST", M+"/right-transfers/"+str(tr, "id")+"/complete", nil, "If-Match", `"1"`)
		o.expect(t, 201, "POST", M+"/mandates", map[string]any{
			"right_id": rightOf[375], "from_on": "2027-08-01", "to_on": "2027-08-31",
			"owner_share_bp": 6000, "channels": []string{"transit"}, "settlement": "credit",
		})

		// 4. actuals by CSV, balance run = Σ actual − Σ advances, credits for the lower item
		var acsv strings.Builder
		acsv.WriteString("voce;importo\n")
		var sumActual int64
		for _, it := range items {
			fmt.Fprintf(&acsv, "%s;%s\n", it.name, strings.Replace(money.Cents(it.actual).String(), ".", ",", 1))
			sumActual += it.actual
		}
		b := o.expect(t, 200, "GET", M+"/common-charge-budgets/"+budgetID, nil)
		o.expect(t, 200, "PUT", M+"/common-charge-budgets/"+budgetID+"/actuals", map[string]any{"csv": acsv.String()}, "If-Match", fmt.Sprintf(`"%d"`, int(b["version"].(float64))))
		bal := o.expect(t, 201, "POST", M+"/common-charge-budgets/"+budgetID+"/runs", map[string]any{"kind": "balance"})
		balID := str(bal, "id")
		if got := int64(bal["total_cents"].(float64)); got != sumActual-sumItems {
			t.Fatalf("balance %d, want Σ actual %d − Σ advances %d", got, sumActual, sumItems)
		}
		ballocs, err := store.AllocationsOfRun(ctx, db.DB, scope, balID, store.AllocationFilterCommon{ItemID: itemID["Luce"]})
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range ballocs {
			if a.CustomerID != "" && a.AmountCents > 0 {
				t.Fatalf("Luce balance allocation should be a credit: %+v", a)
			}
		}
		// the transferred right's berth: the balance goes to the holder of 31 December (§15 D17)
		bOf := map[string]bool{}
		for _, a := range ballocs {
			if a.BerthID == berths[372] {
				bOf[a.CustomerID] = true
			}
		}
		if !bOf[ids[0]] || len(bOf) != 1 {
			t.Fatalf("balance holders of the transferred berth %v", bOf)
		}
		// statements per holder and per item
		if r := o.do(t, "GET", M+"/common-charge-runs/"+balID+"/statements?format=csv&by=item", nil); r.status != 200 {
			t.Fatalf("items csv %d", r.status)
		}
		if r := o.do(t, "GET", M+"/common-charge-runs/"+advID+"/statements?format=csv", nil); r.status != 200 || !strings.Contains(string(r.body), "Pulizia") {
			t.Fatalf("holders csv %d", r.status)
		}
		for _, q := range []string{"?by=item", "?customer_id=" + ids[3]} {
			r := o.do(t, "GET", M+"/common-charge-runs/"+advID+"/statements"+q, nil)
			if r.status != 200 || r.header.Get("Content-Type") != "application/pdf" || !strings.HasPrefix(string(r.body), "%PDF") {
				t.Fatalf("statement PDF %s: %d %s", q, r.status, r.header.Get("Content-Type"))
			}
		}
		// concurrency: two issues of the balance run, one wins
		var wg sync.WaitGroup
		statuses := make([]int, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses[i] = o.do(t, "POST", M+"/common-charge-runs/"+balID+"/issue", nil, "If-Match", `"1"`).status
			}()
		}
		wg.Wait()
		oneWins := (statuses[0] == 200) != (statuses[1] == 200)
		loserRefused := statuses[0] == 409 || statuses[1] == 409 || statuses[0] == 412 || statuses[1] == 412
		if !oneWins || !loserRefused {
			t.Fatalf("concurrent issues %v", statuses)
		}

		// 6. invariants: occupancy (owner rows of every active right, Q17), INV-40, no personal data in events
		vs, err := store.CheckOccupancyInvariants(ctx, db.DB, w.marina, calendar.MustParse("2027-07-01"))
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			t.Errorf("invariant %s %v %s", v.Query, v.IDs, v.Detail)
		}
		var bad []string
		if err := db.NewRaw(`SELECT right_id FROM berth_right_holders WHERE marina_id = ? GROUP BY right_id HAVING sum(share_millis) <> 1000`, w.marina).Scan(ctx, &bad); err != nil {
			t.Fatal(err)
		}
		if len(bad) > 0 {
			t.Fatalf("INV-40 broken on %v", bad)
		}
		var events []store.OutboxEvent
		if err := db.NewSelect().Model(&events).Where("marina_id = ?", w.marina).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			s := string(e.Payload)
			for _, secret := range []string{"TAX000", "Socio", "@porto.test"} {
				if strings.Contains(s, secret) {
					t.Fatalf("event %s carries personal data", e.Type)
				}
			}
		}
		t.Logf("400 berths, 380 rights, advance %d (marina %d), balance %d, %d events", runTotal, marina, sumActual-sumItems, len(events))
	})
}
