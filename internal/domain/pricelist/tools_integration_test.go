//go:build integration

package pricelist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

func insertVessel(t testing.TB, e *env, loa, beam, mLOA, mBeam int64, source, hull string) string {
	t.Helper()
	id := store.NewID()
	now := store.UTC(e.Clock.Now())
	var ml, mb any
	if mLOA > 0 {
		ml, mb = mLOA, mBeam
	}
	_, err := e.DB.NewRaw(`INSERT INTO vessels (id, marina_id, name, hull_type, loa_cm, beam_cm, measured_loa_cm, measured_beam_cm, dimensions_source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, e.MarinaID, "Vento", hull, loa, beam, ml, mb, source, now, now).Exec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertCustomer(t testing.TB, e *env, category string) string {
	t.Helper()
	id := store.NewID()
	now := store.UTC(e.Clock.Now())
	_, err := e.DB.NewRaw(`INSERT INTO customers (id, marina_id, kind, first_name, last_name, category, created_at, updated_at)
		VALUES (?, ?, 'person', 'Mario', 'Rossi', ?, ?, ?)`, id, e.MarinaID, category, now, now).Exec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPricing_SimulateResolvesInputs(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		doc := e.transitDoc("T-2027", 2027)
		// A member discount and a berth-group modifier make the customer and the groups visible in the price.
		doc.Rules = append(doc.Rules,
			Rule{Key: "soci", Rule: pricing.Rule{
				Stage: pricing.StageDiscount, Name: "Soci", DiscountKind: pricing.DiscountCustomer,
				Scope: pricing.Scope{CustomerCategories: []string{"member"}}, Effect: pricing.Effect{PercentBP: ptr(int64(1000))},
			}},
		)
		e.activate(t, e.createList(t, doc), calendar.Date{})
		var mapID string
		berth := e.addBerth(t, &mapID, "A-1", "L12", 1200, 400, 1200, 350)
		g := &store.BerthGroup{
			ID: store.NewID(), Code: "PONT-A", Name: store.MustJSON(catalog.I18nText{It: "A"}), Kind: "structure", Version: 1,
			CreatedAt: e.Clock.Now(), UpdatedAt: e.Clock.Now(),
		}
		if err := store.InsertBerthGroup(context.Background(), db.DB, e.scope(t), g); err != nil {
			t.Fatal(err)
		}
		if _, err := db.NewRaw(`INSERT INTO berth_group_members (id, marina_id, group_id, map_id, resource_type, resource_id, created_at) VALUES (?, ?, ?, ?, 'berth', ?, ?)`,
			store.NewID(), e.MarinaID, g.ID, mapID, berth, e.Clock.Now()).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		measured := insertVessel(t, e, 1100, 380, 1134, 390, "measured", "catamaran")
		declared := insertVessel(t, e, 1100, 380, 0, 0, "declared", "monohull_sail")
		member := insertCustomer(t, e, "member")
		// 23:30 UTC on 1 March is already 2 March in Rome.
		e.Clock.Set(time.Date(2027, 3, 1, 23, 30, 0, 0, time.UTC))

		b, res, err := e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"), Departure: d("2027-07-13"), VesselID: measured,
			Resource: &SimResource{Type: "berth", ID: berth}, CustomerID: member,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.DimensionOrigin != OriginMeasured || res.LOA != 1134 || res.Beam != 390 || res.HullType != "catamaran" {
			t.Fatalf("vessel: %+v", res)
		}
		if res.Category != "L12" || res.BerthLength != 1200 || res.BerthWidth != 400 || len(res.GroupIDs) != 1 || res.GroupIDs[0] != g.ID {
			t.Fatalf("resource: %+v", res)
		}
		if res.CustomerCategory != "member" || res.Channel != pricing.ChannelDesk || res.QuotedOn != d("2027-03-02") {
			t.Fatalf("customer/channel/quoted_on: %+v", res)
		}
		if res.PriceListCode != "T-2027" || res.ItemID != e.trn {
			t.Fatalf("list/item: %+v", res)
		}
		// 11,34 m rounded up to 11,40 m × 7,50 €/m = 85,50 € per night × 3 = 256,50 €, then −10 % member = 230,85 €.
		if b.Subtotal != 25650 || b.Total != 23085 {
			t.Fatalf("subtotal %d total %d", b.Subtotal, b.Total)
		}
		// Declared dimensions; explicit fields override the resolved ones.
		_, res, err = e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"), Departure: d("2027-07-11"), VesselID: declared,
			Resource: &SimResource{Type: "berth", ID: berth}, Category: "L10", BerthLengthCm: 1000, CustomerID: member, CustomerCategory: "resident",
			QuotedOn: ptr(d("2027-01-15")), Channel: pricing.ChannelOnline,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.DimensionOrigin != OriginDeclared || res.LOA != 1100 || res.Category != "L10" || res.BerthLength != 1000 || res.BerthWidth != 400 ||
			res.CustomerCategory != "resident" || res.QuotedOn != d("2027-01-15") || res.Channel != pricing.ChannelOnline {
			t.Fatalf("overrides: %+v", res)
		}
		_, res, err = e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"), Departure: d("2027-07-11"), Vessel: &SimVessel{LOA: 900, Beam: 300},
		})
		if err != nil || res.DimensionOrigin != OriginInput {
			t.Fatalf("input dims: %+v %v", res, err)
		}
		// Ids of another marina are 404; no price is a typed error.
		_, otherMarina, otherOwner := e.OtherOrganization(t)
		_ = otherMarina
		if _, _, err := e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"),
			Departure: d("2027-07-11"), VesselID: store.NewID(),
		}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unknown vessel: %v", err)
		}
		if _, _, err := e.pl.Quote(ctx, otherOwner, otherMarina, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"),
			Departure: d("2027-07-11"), VesselID: measured,
		}); !errors.Is(err, ErrNoActiveList) {
			t.Fatalf("other marina: %v", err)
		}
		if _, _, err := e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractMonthly, Arrival: d("2027-07-10"),
			Departure: d("2027-08-10"),
		}); !errors.Is(err, ErrNoActiveList) {
			t.Fatalf("no list for monthly: %v", err)
		}
		_, _, err = e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"),
			Departure: d("2027-07-11"),
		})
		if !errors.Is(err, pricing.ErrNoPrice) && !errors.Is(err, pricing.ErrInvalidInput) {
			t.Fatalf("no dimensions: %v", err)
		}
		if _, _, err := e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"),
			Departure: d("2027-07-11"), Vessel: &SimVessel{LOA: 900, Beam: 300}, PromoCode: "NONE",
		}); !errors.Is(err, pricing.ErrPromoNotApplicable) {
			t.Fatalf("promo: %v", err)
		}
	})
}

// demoLikeDoc is a list close to the demo seed: four contract types, events, modifiers and discounts.
func (e *env) demoLikeDoc(t testing.TB, code string, y int) SaveRequest {
	doc := e.transitDoc(code, y)
	stg := e.item(t, "ORM-STG", "berth_fee", "season", false, "mooring_annual")
	mes := e.item(t, "ORM-MES", "berth_fee", "month", false, "mooring_annual")
	doc.ContractTypes = append(doc.ContractTypes, pricing.ContractSeasonal, pricing.ContractMonthly)
	doc.Settings.PrimaryItems[pricing.ContractSeasonal] = stg
	doc.Settings.PrimaryItems[pricing.ContractMonthly] = mes
	doc.Settings.SeasonWindow = &pricing.DateRange{From: calendar.New(y, 4, 1), To: calendar.New(y, 9, 30)}
	doc.Settings.Split = map[pricing.ContractType]pricing.SplitRule{pricing.AnyContractType: pricing.SplitProRataDaily, pricing.ContractTransit: pricing.SplitBestCombination}
	doc.Settings.Deposits = map[pricing.ContractType]pricing.DepositPolicy{pricing.ContractAnnual: {PercentBP: ptr(int64(3000))}}
	doc.Periods = append(doc.Periods, Period{
		Key: "p-salone", Code: "SALONE", Name: catalog.I18nText{It: "Salone"}, Kind: pricing.PeriodEvent,
		Ranges: []pricing.DateRange{{From: calendar.New(y, 9, 17), To: calendar.New(y, 9, 22)}},
	})
	doc.Rules = append(doc.Rules,
		Rule{Key: "r-salone", Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: "Passaggio salone", ItemID: e.trn, Basis: pricing.BasisPerMeter,
			Unit: pricing.UnitNight, UnitCents: cents(900), Scope: pricing.Scope{Periods: []string{"SALONE"}},
		}},
		Rule{Key: "r-week", Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: "Settimana alta", ItemID: e.trn, Basis: pricing.BasisPerMeter,
			Unit: pricing.UnitWeek, UnitCents: cents(4500), Scope: pricing.Scope{Periods: []string{"ALTA"}},
		}},
		Rule{Key: "r-stg", Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: "Stagionale", ItemID: stg, Basis: pricing.BasisLengthBand,
			Unit: pricing.UnitSeason, Amounts: map[string]money.Cents{"L10": 245000, "L12": 320000, "L14": 405000},
		}},
		Rule{Key: "r-mes", Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: "Mensile", ItemID: mes, Basis: pricing.BasisLengthBand,
			Unit: pricing.UnitMonth, AmountCents: cents(60000), Amounts: map[string]money.Cents{"L10": 49000},
		}},
		Rule{Key: "m-multi", Rule: pricing.Rule{
			Stage: pricing.StageModifier, Name: "Multiscafo", Conditions: pricing.Conditions{HullTypes: []string{"catamaran", "trimaran"}},
			Effect: pricing.Effect{PercentBP: ptr(int64(5000))},
		}},
		Rule{Key: "m-online", Rule: pricing.Rule{
			Stage: pricing.StageModifier, Name: "Online", Stackable: true, Scope: pricing.Scope{Channels: []pricing.Channel{pricing.ChannelOnline}},
			Effect: pricing.Effect{PercentBP: ptr(int64(-300))},
		}},
		Rule{Key: "d-soci", Rule: pricing.Rule{
			Stage: pricing.StageDiscount, Name: "Soci", DiscountKind: pricing.DiscountCustomer,
			Scope:  pricing.Scope{CustomerCategories: []string{"member"}, ContractTypes: []pricing.ContractType{pricing.ContractAnnual, pricing.ContractSeasonal}},
			Effect: pricing.Effect{PercentBP: ptr(int64(1000))},
		}},
	)
	return doc
}

func TestPricing_MatrixEqualsSimulate(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		doc := e.createList(t, e.demoLikeDoc(t, "DEMO-2027", 2027))
		if hasErrors(doc.Issues) {
			t.Fatalf("demo-like issues: %+v", doc.Issues)
		}
		e.activate(t, doc, calendar.Date{})
		e.addCategory(t, "L12")
		var mapID string
		e.addBerth(t, &mapID, "A-1", "L12", 1200, 400, 1180, 380)
		checked := 0
		for _, ct := range doc.ContractTypes {
			for _, rows := range []string{pricing.RowsBands, pricing.RowsCategories} {
				for _, mp := range []MatrixParams{
					{ContractType: ct, Rows: rows},
					{ContractType: ct, Rows: rows, Channel: pricing.ChannelOnline, CustomerCategory: "member"},
				} {
					m, err := e.pl.Matrix(ctx, e.Owner, e.MarinaID, doc.ID, mp)
					if err != nil {
						t.Fatalf("%s/%s: %v", ct, rows, err)
					}
					if len(m.Cells) == 0 {
						t.Fatalf("%s/%s: empty matrix", ct, rows)
					}
					for i, c := range m.Cells {
						b, _, err := e.pl.Quote(ctx, e.Owner, e.MarinaID, c.Probe)
						if c.Error != "" {
							if err == nil {
								t.Fatalf("%s cell %d: matrix error %s, simulate ok", ct, i, c.Error)
							}
							continue
						}
						if err != nil {
							t.Fatalf("%s cell %d: simulate: %v", ct, i, err)
						}
						want, _ := json.Marshal(m.Breakdowns[i])
						got, _ := json.Marshal(b)
						if !bytes.Equal(want, got) {
							t.Fatalf("%s cell %d differs (INV-23):\nmatrix   %s\nsimulate %s", ct, i, want, got)
						}
						if *c.TotalCents != b.Total {
							t.Fatalf("total %d vs %d", *c.TotalCents, b.Total)
						}
						checked++
					}
				}
			}
		}
		if checked < 20 {
			t.Fatalf("only %d cells checked", checked)
		}
	})
}

func TestPricing_CoverageFindsGaps(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		ctx := fixture.Ctx()
		e.addCategory(t, "L12")
		var mapID string
		e.addBerth(t, &mapID, "A-1", "L12", 1200, 400, 1180, 380)
		e.addBerth(t, &mapID, "A-2", "", 1200, 400, 1180, 380)
		e.addBerth(t, &mapID, "A-3", "XL", 2000, 600, 2000, 550)
		full := e.activate(t, e.createList(t, e.transitDoc("T-2027", 2027)), calendar.Date{}).Document
		cov, err := e.pl.Coverage(ctx, e.Owner, e.MarinaID, full.ID, calendar.Date{})
		if err != nil {
			t.Fatal(err)
		}
		if len(cov.Gaps) != 0 || cov.Checked == 0 {
			t.Fatalf("full list: %+v", cov)
		}
		if cov.ElementsWithoutCategory.Berths != 1 || len(cov.UnknownCategories) != 1 || cov.UnknownCategories[0].Code != "XL" {
			t.Fatalf("elements: %+v", cov)
		}
		// A copy without the BASSA rule has a transit gap in BASSA.
		_, copyDoc, err := e.pl.Renew(ctx, e.Owner, e.MarinaID, full.ID, RenewalRequest{Code: "T-2027-B", Name: full.Name, AmountRounding: pricing.AmountRoundNone})
		if err != nil {
			t.Fatal(err)
		}
		req := docToSave(copyDoc)
		req.Rules = req.Rules[1:]
		res, err := e.pl.Save(ctx, e.Owner, e.MarinaID, copyDoc.ID, copyDoc.Version, req)
		if err != nil {
			t.Fatal(err)
		}
		cov, err = e.pl.Coverage(ctx, e.Owner, e.MarinaID, res.Document.ID, calendar.Date{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, g := range cov.Gaps {
			if g.ContractType != pricing.ContractTransit || g.Period != "BASSA" {
				t.Fatalf("unexpected gap %+v", g)
			}
			found = true
		}
		if !found {
			t.Fatalf("no BASSA gap: %+v", cov.Gaps)
		}
	})
}

func TestPricing_NoPIIInLogs(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		var buf bytes.Buffer
		var mu sync.Mutex
		log := slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug}))
		e.pl = NewService(Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: log})
		doc := e.transitDoc("T-2027", 2027)
		doc.Rules = append(doc.Rules, Rule{Key: "promo", Rule: pricing.Rule{
			Stage: pricing.StageDiscount, Name: "Benvenuto",
			DiscountKind: pricing.DiscountPromo, PromoCode: "SEGRETO42", Effect: pricing.Effect{PercentBP: ptr(int64(1000))},
		}})
		e.activate(t, e.createList(t, doc), calendar.Date{})
		customer := insertCustomer(t, e, "member")
		ctx := fixture.Ctx()
		if _, _, err := e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"),
			Departure: d("2027-07-11"), Vessel: &SimVessel{LOA: 1000, Beam: 300}, CustomerID: customer, PromoCode: "segreto42",
		}); err != nil {
			t.Fatal(err)
		}
		_, _, _ = e.pl.Quote(ctx, e.Owner, e.MarinaID, QuoteRequest{
			ContractType: pricing.ContractTransit, Arrival: d("2027-07-10"),
			Departure: d("2027-07-11"), Vessel: &SimVessel{LOA: 1000, Beam: 300}, CustomerID: customer, PromoCode: "SBAGLIATO99",
		})
		mu.Lock()
		out := buf.String()
		mu.Unlock()
		for _, s := range []string{"SEGRETO42", "segreto42", "SBAGLIATO99", customer, "Mario", "Rossi"} {
			if strings.Contains(out, s) {
				t.Fatalf("log contains %q: %s", s, out)
			}
		}
		// Events and audit rows never carry the promo code either.
		var payloads []string
		if err := db.NewSelect().Table("outbox_events").Column("payload").Scan(context.Background(), &payloads); err != nil {
			t.Fatal(err)
		}
		var diffs []string
		if err := db.NewRaw("SELECT CAST(diff AS TEXT) FROM audit_log").Scan(context.Background(), &diffs); err != nil {
			t.Fatal(err)
		}
		for _, p := range append(payloads, diffs...) {
			if strings.Contains(strings.ToUpper(p), "SEGRETO42") {
				t.Fatalf("promo code stored in %s", p)
			}
		}
	})
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestConcurrency_PriceListActivation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		const n = 100
		docs := make([]*Document, n)
		for i := range n {
			doc := e.transitDoc("C-"+strconv.Itoa(i), 2027)
			docs[i] = e.createList(t, doc)
		}
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Half start later, so they truncate an earlier winner instead of overlapping it.
				eff := calendar.Date{}
				if i%2 == 1 {
					eff = d("2027-03-01").AddDays(i)
				}
				_, errs[i] = e.pl.Activate(fixture.Ctx(), e.Owner, e.MarinaID, docs[i].ID, docs[i].Version, eff)
			}()
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrOverlap):
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if ok == 0 {
			t.Fatal("no activation succeeded")
		}
		assertOneActivePerNight(t, e)
	})
}

func TestConcurrency_PriceListSaveSingleWinner(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		e := newEnv(t, db)
		doc := e.createList(t, e.transitDoc("T-2027", 2027))
		const n = 100
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := docToSave(doc)
				req.Rules[0].UnitCents = cents(int64(300 + i))
				_, errs[i] = e.pl.Save(fixture.Ctx(), e.Owner, e.MarinaID, doc.ID, doc.Version, req)
			}()
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrVersionMismatch):
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if ok != 1 {
			t.Fatalf("%d saves won", ok)
		}
		got, err := e.pl.Get(fixture.Ctx(), e.Owner, e.MarinaID, doc.ID)
		if err != nil || got.Version != doc.Version+1 {
			t.Fatalf("version %d, %v", got.Version, err)
		}
	})
}
