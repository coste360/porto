//go:build integration

package acceptance

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/pkg/ext"
)

// The M5 simulated year (m5-plan §11.4, CHARGES-EXPORT): a deterministic scenario generator (PCG(51, 5)) over a
// fresh marina, driven through the API one day at a time by TestAcceptance_M5_SimulatedYear. PAYMENTS and
// CASH-DRIVERS reuse the generator read-only (newSimYear, simYear.run) and add their own actions through the
// hooks (simYear.OnDay).

// Sizes of the scenario.
const (
	simMoorings     = 24 // 8 annual, 8 seasonal, 8 monthly on berths 0..23
	simTransitBerth = 24 // first berth of the transits
	simBerths       = 60
	simTransits     = 150
	simManual       = 10
	simCounterSales = 60
	simAdjustments  = 15
)

var simStart = calendar.MustParse("2027-01-01")

// simItems are the catalogue items of the scenario (code → VAT rate and nature, flat price of a stay service).
var simItems = []struct {
	code    string
	bp      int64
	nature  string
	service int64 // > 0: flat price per stay, offered as a contract service
}{
	{"ELETTRICITA", 1000, "", 4500},
	{"PULIZIA", 0, "N4", 2500},
	{"BOA", 400, "", 1800},
	{"GASOLIO", 2200, "", 0},
	{"ACQUA", 1000, "", 0},
	{"LAVANDERIA", 2200, "", 0},
	{"BOLLO", 0, "N1", 0},
}

// simTransitPlan is a generated transit.
type simTransitPlan struct {
	berth              int
	created            calendar.Date
	arrival, departure calendar.Date // departure exclusive
	services           []string
	shortenOn          calendar.Date // zero = never
	cancelOn           calendar.Date // zero = never
}

// simMooringPlan is a generated mooring contract.
type simMooringPlan struct {
	berth    int
	term     string
	starts   calendar.Date
	services []string
}

// simSale is a generated manual charge or counter sale.
type simSale struct {
	day     calendar.Date
	counter bool
	item    string
	qty     int64
	unit    int64
}

// simAdjust is a generated adjustment of an exported charge.
type simAdjust struct {
	day     calendar.Date
	replace bool
	delta   int64 // cents, signed (delta mode)
}

// simScenario is the whole generated year.
type simScenario struct {
	moorings    []simMooringPlan
	transits    []simTransitPlan
	sales       []simSale
	adjustments []simAdjust
	reexportOn  calendar.Date
	filterWeeks map[calendar.Date][]string // export day → origin filter
	addenda     []simAddendum
	terminate   map[int]calendar.Date // annual mooring index → termination day
}

// simAddendum is a price change of an annual mooring.
type simAddendum struct {
	day     calendar.Date
	mooring int
	delta   int64
}

// generateScenario draws the year deterministically.
func generateScenario() simScenario {
	rng := mrand.New(mrand.NewPCG(51, 5)) //nolint:gosec // deterministic scenario
	sc := simScenario{filterWeeks: map[calendar.Date][]string{}}
	services := []string{"ELETTRICITA", "PULIZIA", "BOA"}
	pickServices := func() []string {
		var out []string
		for _, s := range services {
			if rng.IntN(3) == 0 {
				out = append(out, s)
			}
		}
		return out
	}
	for i := range simMoorings {
		m := simMooringPlan{berth: i, services: pickServices()}
		switch {
		case i < 8:
			m.term, m.starts = "annual", simStart.AddDays(rng.IntN(90))
		case i < 16:
			m.term, m.starts = "seasonal", calendar.MustParse("2027-05-01")
		default:
			m.term, m.starts = "monthly", simStart.AddDays(rng.IntN(300))
		}
		sc.moorings = append(sc.moorings, m)
	}
	free := make([]calendar.Date, simBerths) // next free night per transit berth
	for b := range free {
		free[b] = simStart.AddDays(5)
	}
	for range simTransits {
		b := simTransitBerth + rng.IntN(simBerths-simTransitBerth)
		arrival := free[b].AddDays(rng.IntN(12))
		nights := 1 + rng.IntN(5)
		departure := arrival.AddDays(nights)
		if departure.Year() > 2027 || departure.After(calendar.MustParse("2027-12-28")) {
			continue
		}
		free[b] = departure
		created := arrival.AddDays(-rng.IntN(5))
		if created.Before(simStart) {
			created = simStart
		}
		tp := simTransitPlan{berth: b, created: created, arrival: arrival, departure: departure, services: pickServices()}
		switch x := rng.IntN(20); {
		case x < 2 && nights > 1:
			tp.shortenOn = created.AddDays(rng.IntN(3)) // before or after the weekly export
		case x == 2:
			tp.cancelOn = arrival.AddDays(-1)
			if tp.cancelOn.Before(created) {
				tp.cancelOn = created
			}
		}
		sc.transits = append(sc.transits, tp)
	}
	items := []string{"GASOLIO", "ACQUA", "LAVANDERIA", "BOLLO", "ELETTRICITA", "BOA"}
	for i := range simManual + simCounterSales {
		sc.sales = append(sc.sales, simSale{
			day: simStart.AddDays(rng.IntN(355)), counter: i >= simManual, item: items[rng.IntN(len(items))],
			qty: int64(1+rng.IntN(5)) * 1000, unit: int64(100 + rng.IntN(20000)),
		})
	}
	for range simAdjustments {
		a := simAdjust{day: calendar.MustParse("2027-02-01").AddDays(rng.IntN(320)), replace: rng.IntN(3) == 0}
		a.delta = int64(rng.IntN(3000)) - 2000
		if a.delta == 0 {
			a.delta = 100
		}
		sc.adjustments = append(sc.adjustments, a)
	}
	sort.Slice(sc.sales, func(i, j int) bool { return sc.sales[i].day.Before(sc.sales[j].day) })
	sort.Slice(sc.adjustments, func(i, j int) bool { return sc.adjustments[i].day.Before(sc.adjustments[j].day) })
	// Mondays with an origin filter, and the re-export of one week.
	for d := simStart; d.Year() == 2027; d = d.AddDays(1) {
		if d.ISOWeekday() == 1 && rng.IntN(6) == 0 {
			sc.filterWeeks[d] = []string{"transit", "counter_sale"}
		}
	}
	sc.reexportOn = calendar.MustParse("2027-06-14")
	sc.terminate = map[int]calendar.Date{}
	for i := range 6 {
		m := sc.moorings[i]
		sc.addenda = append(sc.addenda, simAddendum{day: m.starts.AddDays(20 + rng.IntN(250)), mooring: i, delta: 1000 + int64(rng.IntN(40000))})
	}
	for _, i := range []int{6, 7} {
		sc.terminate[i] = sc.moorings[i].starts.AddDays(150 + rng.IntN(60))
	}
	return sc
}

// simYear is the world of the simulation.
type simYear struct {
	t         *testing.T
	h         *harness
	db        *store.DB
	clock     *clock.Fake
	owner     *client
	marina    string
	M         string
	harbour   contracttest.Harbour
	items     map[string]string // code → id
	customers []string
	sc        simScenario
	today     calendar.Date
	transits  map[int]string // scenario index → contract id
	moorings  map[int]string // scenario index → contract id
	exports   []string       // first export ids
	reexports []string
	// OnDay lets other builders' acceptance tests add their actions after the scenario of each day.
	OnDay func(y *simYear)
}

// newSimHarness is the acceptance harness with a fake clock and quiet logs (a year of debug logs is large).
func newSimHarness(t *testing.T, db *store.DB, c *clock.Fake) *harness {
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
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
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

// newSimYear builds the marina: harbour, price list (transit, annual, seasonal, monthly and the stay services),
// items, customers and the billing settings.
func newSimYear(t *testing.T, db *store.DB) *simYear {
	t.Helper()
	c := clock.NewFake(time.Date(2027, 1, 1, 7, 0, 0, 0, time.UTC))
	h := newSimHarness(t, db, c)
	own, err := h.app.Auth.CreateOwner(context.Background(), auth.CreateOwnerInput{
		Email: "owner@porto.test", Name: "Owner", Password: password, Organization: "Porto Anno SRL", MarinaName: "Marina Anno", MarinaSlug: "anno",
	})
	if err != nil {
		t.Fatal(err)
	}
	y := &simYear{
		t: t, h: h, db: db, clock: c, marina: own.MarinaID, M: A + "/marinas/" + own.MarinaID, owner: h.cookieClient(),
		items: map[string]string{}, sc: generateScenario(), today: simStart, transits: map[int]string{}, moorings: map[int]string{},
	}
	y.owner.login(t, "owner@porto.test")
	y.harbour = contracttest.InsertHarbour(t, db, own.MarinaID, store.UTC(c.Now()), simBerths)
	y.prices(t)
	for i := range 30 {
		r := y.owner.expect(t, 201, "POST", y.M+"/customers", map[string]any{
			"kind": "person", "first_name": "Cliente", "last_name": fmt.Sprintf("Anno%02d", i),
		})
		y.customers = append(y.customers, str(r["customer"].(map[string]any), "id"))
	}
	bs := y.owner.expect(t, 200, "GET", y.M+"/billing-settings", nil)
	y.owner.expect(t, 200, "PUT", y.M+"/billing-settings", map[string]any{
		"default_due_days": 30, "auto_payment_request": false, "reminder_policy": bs["reminder_policy"], "reminder_hour": 9,
		"arrears_policy": map[string]any{"enabled": false, "days_overdue": 60, "min_amount_cents": 0}, "export_reminder_days": 30,
		"fiscal_driver": "none", "receipt_footer": "", "sepa_lead_days": 0,
		"origin_items": []any{map[string]any{"origin_key": "contract_trueup_fallback", "item_id": y.items["GASOLIO"]}},
	}, "If-Match", `"0"`)
	return y
}

// prices creates the items and the 2027 list through the API.
func (y *simYear) prices(t *testing.T) {
	t.Helper()
	o, M := y.owner, y.M
	classes := o.expect(t, 200, "GET", M+"/revenue-classes", nil)
	classID := map[string]string{}
	var anyClass string
	for _, rc := range jsonList(classes, "items") {
		classID[str(rc.(map[string]any), "code")] = str(rc.(map[string]any), "id")
		anyClass = str(rc.(map[string]any), "id")
	}
	or := func(code string) string {
		if id := classID[code]; id != "" {
			return id
		}
		return anyClass
	}
	trn := o.expect(t, 201, "POST", M+"/items", map[string]any{
		"code": "TRN-NTT", "name": map[string]string{"it": "Passaggio"}, "item_type": "berth_fee", "unit": "night",
		"revenue_class_id": or("transit"), "price_includes_vat": true, "vat_rate_bp": 2200,
	})
	orm := o.expect(t, 201, "POST", M+"/items", map[string]any{
		"code": "ORM-ANN", "name": map[string]string{"it": "Ormeggio"}, "item_type": "berth_fee", "unit": "year",
		"revenue_class_id": or("mooring_annual"), "price_includes_vat": true, "vat_rate_bp": 2200,
	})
	rules := []map[string]any{
		{"key": "r1", "stage": "base", "name": "Transito", "item_id": str(trn, "id"), "basis": "per_meter", "unit": "night", "unit_cents": 311, "scope": map[string]any{"contract_types": []string{"transit"}}},
		{"key": "r2", "stage": "base", "name": "Annuale", "item_id": str(orm, "id"), "basis": "flat", "unit": "year", "amount_cents": 487391, "scope": map[string]any{"contract_types": []string{"annual"}}},
		{"key": "r3", "stage": "base", "name": "Stagionale", "item_id": str(orm, "id"), "basis": "flat", "unit": "season", "amount_cents": 301117, "scope": map[string]any{"contract_types": []string{"seasonal"}}},
		{"key": "r4", "stage": "base", "name": "Mensile", "item_id": str(orm, "id"), "basis": "flat", "unit": "month", "amount_cents": 60013, "scope": map[string]any{"contract_types": []string{"monthly"}}},
	}
	for i, it := range simItems {
		body := map[string]any{
			"code": it.code, "name": map[string]string{"it": it.code}, "item_type": "service", "unit": "piece",
			"revenue_class_id": anyClass, "price_includes_vat": true, "vat_rate_bp": it.bp,
		}
		if it.nature != "" {
			body["vat_nature"] = it.nature
		}
		created := o.expect(t, 201, "POST", M+"/items", body)
		y.items[it.code] = str(created, "id")
		if it.service > 0 {
			rules = append(rules, map[string]any{
				"key": fmt.Sprintf("s%d", i), "stage": "base", "name": it.code, "item_id": str(created, "id"), "basis": "flat", "unit": "stay",
				"amount_cents": it.service, "scope": map[string]any{},
			})
		}
	}
	types := []string{"transit", "annual", "seasonal", "monthly"}
	settings := map[string]any{
		"dimension_source": "vessel", "length_rounding": map[string]any{"step_cm": 10, "mode": "up"},
		"primary_items": map[string]string{"transit": str(trn, "id"), "annual": str(orm, "id"), "seasonal": str(orm, "id"), "monthly": str(orm, "id")},
		"season_window": map[string]string{"from": "2027-05-01", "to": "2027-09-30"},
	}
	doc := o.expect(t, 201, "POST", M+"/price-lists", map[string]any{
		"code": "L2027", "name": map[string]string{"it": "L2027"}, "valid_from": "2027-01-01", "valid_to": "2028-12-31", "contract_types": types,
	})
	body := map[string]any{
		"code": "L2027", "name": map[string]string{"it": "L2027"}, "valid_from": "2027-01-01", "valid_to": "2028-12-31",
		"contract_types": types, "settings": settings, "periods": []any{}, "bands": []any{}, "rules": rules,
	}
	saved := o.expect(t, 200, "PUT", M+"/price-lists/"+str(doc, "id"), body, "If-Match", etagOf(doc["version"]))
	sd := saved["document"].(map[string]any)
	o.expect(t, 200, "POST", M+"/price-lists/"+str(doc, "id")+"/activate", map[string]any{}, "If-Match", etagOf(sd["version"]))
}

// vessel creates a vessel that fits the 12 × 4 m berths.
func (y *simYear) vessel(t *testing.T, i int) string {
	t.Helper()
	v := y.owner.expect(t, 201, "POST", y.M+"/vessels", map[string]any{
		"name": fmt.Sprintf("Barca %03d", i), "loa_cm": 900 + (i%25)*10, "beam_cm": 320, "hull_type": "monohull_sail",
	})
	return str(v, "id")
}

func (y *simYear) customer(i int) string { return y.customers[i%len(y.customers)] }

func servicesBody(y *simYear, codes []string) []any {
	out := []any{}
	for _, c := range codes {
		out = append(out, map[string]any{"item_id": y.items[c]})
	}
	return out
}

// run drives the scenario from 2027-01-01 to 2027-12-31, then exports what is left.
func (y *simYear) run(t *testing.T, checkEvery func(day calendar.Date)) {
	t.Helper()
	for d := simStart; d.Year() == 2027; d = d.AddDays(1) {
		y.today = d
		y.clock.Set(time.Date(d.Year(), d.Month(), d.Day(), 7, 0, 0, 0, time.UTC))
		y.owner.login(t, "owner@porto.test")
		y.h.runJobs("installments.issue", "charges.backfill")
		y.day(t, d)
		if y.OnDay != nil {
			y.OnDay(y)
		}
		if checkEvery != nil {
			checkEvery(d)
		}
	}
	// Export everything still to invoice (charges left out by the filtered weeks).
	r := y.owner.do(t, "POST", y.M+"/charge-exports", map[string]any{"period_from": "2027-01-01", "period_to": "2027-12-31"})
	if r.status == 201 {
		y.exports = append(y.exports, str(r.json(t), "id"))
	} else if r.problemType() != "export_empty" {
		t.Fatalf("final export: %d %s", r.status, r.body)
	}
}

// day runs the actions of one day.
func (y *simYear) day(t *testing.T, d calendar.Date) {
	t.Helper()
	o, M := y.owner, y.M
	for i, m := range y.sc.moorings {
		if !m.starts.Equal(d) {
			continue
		}
		body := map[string]any{
			"term": m.term, "customer_id": y.customer(i), "vessel_id": y.vessel(t, i), "starts_on": d.String(),
			"placements": []any{berthPlacement(y.harbour.Berths[m.berth], d.String(), mooringLast(m).String())},
			"services":   servicesBody(y, m.services), "activate": true,
		}
		ct := o.expect(t, 201, "POST", M+"/contracts", body)
		y.moorings[i] = str(ct, "id")
	}
	for _, a := range y.sc.addenda {
		id := y.moorings[a.mooring]
		if !a.day.Equal(d) || id == "" {
			continue
		}
		ct := o.expect(t, 200, "GET", M+"/contracts/"+id, nil)
		total := int64(ct["total_cents"].(float64))
		ad := o.expect(t, 201, "POST", M+"/contracts/"+id+"/addenda", map[string]any{
			"kind": "price_change", "effective_on": d.String(), "payload": map[string]any{"new_total_cents": total + a.delta, "reason": "adeguamento"},
		})
		o.expect(t, 200, "POST", M+"/contracts/"+id+"/addenda/"+str(ad, "id")+"/activate", map[string]any{}, "If-Match", etagOf(ad["version"]))
	}
	for i, day := range y.sc.terminate {
		id := y.moorings[i]
		if !day.Equal(d) || id == "" {
			continue
		}
		ct := o.expect(t, 200, "GET", M+"/contracts/"+id, nil)
		term := o.expect(t, 200, "POST", M+"/contracts/"+id+"/terminate", map[string]any{"effective_on": d.AddDays(30).String(), "reason": "vendita"}, "If-Match", etagOf(ct["version"]))
		if amt, ok := term["termination_amount_cents"].(float64); ok && amt != 0 {
			o.expect(t, 201, "POST", M+"/contracts/"+id+"/termination-charge", nil, "If-Match", etagOf(term["version"]))
		}
	}
	for i, tp := range y.sc.transits {
		if tp.created.Equal(d) {
			tr := o.expect(t, 201, "POST", M+"/transits", map[string]any{
				"vessel_id": y.vessel(t, 100+i), "customer_id": y.customer(i), "arrival": tp.arrival.String(), "departure": tp.departure.String(),
				"placements": []any{berthPlacement(y.harbour.Berths[tp.berth], tp.arrival.String(), tp.departure.AddDays(-1).String())},
				"services":   servicesBody(y, tp.services), "confirm": map[string]any{"reference": "banco"},
			})
			y.transits[i] = str(tr, "id")
		}
		if id := y.transits[i]; id != "" && tp.shortenOn.Equal(d) {
			c := o.expect(t, 200, "GET", M+"/contracts/"+id, nil)
			o.expect(t, 200, "POST", M+"/transits/"+id+"/stay", map[string]any{"departure": tp.departure.AddDays(-1).String()}, "If-Match", etagOf(c["version"]))
		}
		if id := y.transits[i]; id != "" && tp.cancelOn.Equal(d) {
			c := o.expect(t, 200, "GET", M+"/contracts/"+id, nil)
			o.expect(t, 200, "POST", M+"/transits/"+id+"/cancel", map[string]any{"reason": "meteo"}, "If-Match", etagOf(c["version"]))
		}
	}
	for i, s := range y.sc.sales {
		if !s.day.Equal(d) {
			continue
		}
		origin := "manual"
		if s.counter {
			origin = "counter_sale"
		}
		o.expect(t, 201, "POST", M+"/charges", map[string]any{
			"origin_type": origin, "customer_id": y.customer(i),
			"lines": []any{map[string]any{"item_id": y.items[s.item], "qty_milli": s.qty, "unit_cents": s.unit}},
		})
	}
	for i, a := range y.sc.adjustments {
		if a.day.Equal(d) {
			y.adjust(t, i, a)
		}
	}
	if d.ISOWeekday() == 1 && d.After(simStart.AddDays(6)) {
		body := map[string]any{"period_from": d.AddDays(-7).String(), "period_to": d.AddDays(-1).String()}
		if f := y.sc.filterWeeks[d]; f != nil {
			body["origin_types"] = f
		}
		r := o.do(t, "POST", M+"/charge-exports", body)
		switch {
		case r.status == 201:
			id := str(r.json(t), "id")
			y.exports = append(y.exports, id)
			if d.Equal(y.sc.reexportOn) {
				re := o.expect(t, 201, "POST", M+"/charge-exports/reexport", map[string]any{"export_id": id})
				y.reexports = append(y.reexports, str(re, "id"))
			}
		case r.problemType() != "export_empty":
			t.Fatalf("export %s: %d %s", d, r.status, r.body)
		}
	}
	if d.Day() == 1 && d.Month() > 1 {
		y.reconcile(t)
	}
}

// mooringLast is the last night of a mooring of the scenario.
func mooringLast(m simMooringPlan) calendar.Date {
	switch m.term {
	case "annual":
		return calendar.New(m.starts.Year()+1, m.starts.Month(), m.starts.Day()).AddDays(-1)
	case "seasonal":
		return calendar.MustParse("2027-09-30")
	default:
		return calendar.New(m.starts.Year(), m.starts.Month()+1, m.starts.Day()).AddDays(-1)
	}
}

// adjust corrects the oldest exported manual charge or counter sale not yet corrected.
func (y *simYear) adjust(t *testing.T, i int, a simAdjust) {
	t.Helper()
	page := y.owner.expect(t, 200, "GET", y.M+"/charges?invoicing_status=exported&limit=200", nil)
	var target map[string]any
	for _, it := range jsonList(page, "items") {
		c := it.(map[string]any)
		o := str(c, "origin_type")
		if (o == "manual" || o == "counter_sale") && c["corrects_charge"] == nil && str(c, "payment_status") == "open" {
			target = c
		}
	}
	if target == nil {
		return
	}
	full := y.owner.expect(t, 200, "GET", y.M+"/charges/"+str(target, "id"), nil)
	if corr := jsonList(full, "corrections"); len(corr) > 0 {
		return
	}
	line := jsonList(full, "lines")[0].(map[string]any)
	item := str(line, "item_id")
	body := map[string]any{"mode": "delta", "reason": fmt.Sprintf("rettifica %d", i), "lines": []any{
		map[string]any{"item_id": item, "qty_milli": 1000, "unit_cents": a.delta},
	}}
	if a.replace {
		body = map[string]any{"mode": "replace", "reason": fmt.Sprintf("sostituzione %d", i), "lines": []any{
			map[string]any{"item_id": item, "qty_milli": 1000, "unit_cents": 1234 + i},
		}}
	}
	y.owner.expect(t, 201, "POST", y.M+"/charges/"+str(target, "id")+"/adjust", body, "If-Match", etagOf(full["version"]))
}

// reconcile sends back invoice numbers for 90 % of the exported charges not yet invoiced.
func (y *simYear) reconcile(t *testing.T) {
	t.Helper()
	cursor := ""
	var rows []any
	n := 0
	for {
		q := y.M + "/charges?invoicing_status=exported&limit=200"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		page := y.owner.expect(t, 200, "GET", q, nil)
		for _, it := range jsonList(page, "items") {
			c := it.(map[string]any)
			n++
			if n%10 == 0 {
				continue
			}
			rows = append(rows, map[string]any{
				"charge_id": str(c, "id"), "invoice_number": fmt.Sprintf("FT-%s-%d", y.today, n), "invoice_date": y.today.String(),
			})
		}
		next, _ := page["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(rows) == 0 {
		return
	}
	rec := y.owner.expect(t, 200, "POST", y.M+"/invoice-reconciliations", map[string]any{"dry_run": false, "rows": rows})
	if int(rec["matched_count"].(float64)) != len(rows) {
		t.Fatalf("reconciliation matched %v of %d", rec["matched_count"], len(rows))
	}
}
