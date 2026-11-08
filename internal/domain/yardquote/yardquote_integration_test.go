//go:build integration

package yardquote

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/contract/contracttest"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/formula"
	"github.com/coste360/porto/internal/domain/magiclink"
	"github.com/coste360/porto/internal/domain/pricelist"
	"github.com/coste360/porto/internal/domain/pricing"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/testutil/fixture"
	"github.com/coste360/porto/internal/testutil/testdb"
)

// fakeOrders is the work order side of the tests: it records the acceptances that committed.
type fakeOrders struct {
	committed atomic.Int32
	fail      error
	calls     atomic.Int32
}

func (f *fakeOrders) create(ctx context.Context, q AcceptedQuote) (OrderRef, error) {
	f.calls.Add(1)
	if f.fail != nil {
		return OrderRef{}, f.fail
	}
	if q.Quote.Status != store.YardQuoteAccepted || len(q.Lines) == 0 || len(q.Snapshot.Lines) == 0 {
		return OrderRef{}, errors.New("fake: quote not accepted or without lines")
	}
	store.AfterCommit(ctx, func() { f.committed.Add(1) })
	return OrderRef{WorkOrderID: store.NewID(), Number: "C2027-000001"}, nil
}

func (f *fakeOrders) CreateFromQuoteTx(ctx context.Context, _ bun.Tx, _ store.MarinaScope, _ audit.Actor, q AcceptedQuote) (OrderRef, error) {
	return f.create(ctx, q)
}

func (f *fakeOrders) ApplyVariationTx(ctx context.Context, _ bun.Tx, _ store.MarinaScope, _ audit.Actor, q AcceptedQuote) (OrderRef, error) {
	return f.create(ctx, q)
}

// yenv is the backend of the yard quote tests: contracttest plus the §4.4 catalogue and a 12 m sailing boat.
type yenv struct {
	*contracttest.Env
	s       *Service
	orders  *fakeOrders
	scope   store.MarinaScope
	items   map[string]string
	wt      string
	pkg     string
	vessel  string
	cust    string
	noEmail string
}

func ctxT() context.Context { return fixture.Ctx() }

func newYenv(t *testing.T, db *store.DB) *yenv {
	t.Helper()
	e := contracttest.New(t, db)
	links := magiclink.NewService(magiclink.Deps{DB: db, Clock: e.Clock, BaseURL: "https://porto.test", Log: e.Log})
	dsvc := docs.NewService(docs.Deps{DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Keys: e.Keys, Files: e.Files, Notify: e.Notify, Log: e.Log})
	y := &yenv{Env: e, scope: e.Scope(t), orders: &fakeOrders{}, items: map[string]string{}}
	y.s = NewService(Deps{
		DB: db, Clock: e.Clock, Audit: e.Audit, Outbox: e.Outbox, Log: e.Log, Keys: e.Keys, Prices: e.Prices, Docs: dsvc, Links: links,
		Notify: e.Notify, Orders: y.orders,
	})
	y.catalogue(t)
	y.vessel = e.AddVessel(t, "Maestrale", 1200, 400, "monohull_sail")
	if _, err := db.NewUpdate().Table("vessels").Set("draft_cm = 150").Where("id = ?", y.vessel).Exec(ctxT()); err != nil {
		t.Fatal(err)
	}
	now := store.UTC(e.Clock.Now())
	c := &store.Customer{
		ID: store.NewID(), MarinaID: e.MarinaID, Kind: "person", LastName: "Bianchi", FirstName: "Anna", BillingAddress: store.JSON(`{}`),
		Locale: "it", Emails: store.JSON(`[{"label":"home","value":"anna@example.test"}]`), Phones: store.JSON(`[]`), Tags: store.Strings{},
		SearchText: "bianchi", Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	e.Insert(t, c)
	y.cust, y.noEmail = c.ID, e.Customer
	return y
}

func ptr[T any](v T) *T { return &v }

func m(s string) qty.Milli { return qty.MustParse(s) }

// catalogue creates the §4.4 catalogue through the services: items and their 2027 prices (a storage list, the
// contracttest list serves the other contract types), qualifications, hull_area, layers and ANTIVEG v1.
func (y *yenv) catalogue(t *testing.T) {
	t.Helper()
	ctx, p, mid := ctxT(), y.Owner, y.MarinaID
	wt, err := y.s.CreateWorkType(ctx, p, mid, WorkTypeInput{Code: "ANTIFOULING", Name: I18n{It: "Antivegetativa"}})
	if err != nil {
		t.Fatal(err)
	}
	y.wt = wt.ID
	vat, excl := int64(2200), false
	classes, err := y.Catalog.ListRevenueClasses(ctx, p, mid, false)
	if err != nil {
		t.Fatal(err)
	}
	rc := ""
	for _, c := range classes {
		if c.Code == "services" {
			rc = c.ID
		}
	}
	prices := map[string]int64{"SRV-ALAG": 1800, "SRV-LAV": 400, "LAB-HAND": 3800, "LAB-PAINT": 4500, "MAT-AVEG": 9500, "SRV-VARO": 1800}
	for _, it := range []struct{ code, typ, unit string }{
		{"SRV-ALAG", "service", "m"},
		{"SRV-LAV", "service", "m2"},
		{"LAB-HAND", "labour", "hour"},
		{"LAB-PAINT", "labour", "hour"},
		{"MAT-AVEG", "material", "piece"},
		{"SRV-VARO", "service", "m"},
		{"SRV-NOPRICE", "service", "lump"},
	} {
		row, err := y.Catalog.CreateItem(ctx, p, mid, catalog.ItemInput{
			Code: it.code, Name: catalog.I18nText{It: it.code}, ItemType: it.typ, Unit: it.unit, VATRateBP: &vat, PriceIncludesVAT: &excl,
			RevenueClassID: rc,
		})
		if err != nil {
			t.Fatalf("item %s: %v", it.code, err)
		}
		y.items[it.code] = row.ID
	}
	types := []pricing.ContractType{pricing.ContractStorage}
	set := pricing.DefaultSettings()
	set.PrimaryItems = map[pricing.ContractType]string{pricing.ContractStorage: y.items["SRV-ALAG"]}
	doc := pricelist.SaveRequest{
		Code: "CANT27", Name: catalog.I18nText{It: "Cantiere 2027"}, ValidFrom: calendar.MustParse("2027-01-01"),
		ValidTo: calendar.MustParse("2027-12-31"), ContractTypes: types, Settings: set,
	}
	for code, c := range prices {
		cc := money.Cents(c)
		doc.Rules = append(doc.Rules, pricelist.Rule{Key: code, Rule: pricing.Rule{
			Stage: pricing.StageBase, Name: code, ItemID: y.items[code], Basis: pricing.BasisFlat, Unit: pricing.UnitStay, AmountCents: &cc,
		}})
	}
	l, err := y.Prices.Create(ctx, p, mid, pricelist.CreateInput{Code: doc.Code, Name: doc.Name, ValidFrom: doc.ValidFrom, ValidTo: doc.ValidTo, ContractTypes: types})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := y.Prices.Save(ctx, p, mid, l.ID, l.Version, doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := y.Prices.Activate(ctx, p, mid, l.ID, saved.Document.Version, calendar.Date{}); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := y.s.CreateQualification(ctx, p, mid, QualificationInput{Code: "hand", Name: I18n{It: "Operaio"}, LabourItemID: y.items["LAB-HAND"], DefaultCostCents: ptr(int64(2200))}); err != nil {
		t.Fatal(err)
	}
	if _, err := y.s.CreateQualification(ctx, p, mid, QualificationInput{Code: "painter", Name: I18n{It: "Verniciatore"}, LabourItemID: y.items["LAB-PAINT"], DefaultCostCents: ptr(int64(2600))}); err != nil {
		t.Fatal(err)
	}
	for code, cost := range map[string]int64{"SRV-LAV": 100, "MAT-AVEG": 5800, "SRV-NOPRICE": 0} {
		c := cost
		if _, err := y.s.PutItemSettings(ctx, p, mid, y.items[code], 0, ItemSettingsInput{CostCents: &c}); err != nil {
			t.Fatalf("settings %s: %v", code, err)
		}
	}
	if _, err := y.s.CreateVariable(ctx, p, mid, VariableInput{
		Name: "hull_area", Source: store.FormulaSourceDerived, Kind: store.FormulaKindNumber, Unit: "m2", Expression: "loa * (beam + draft) * 0.85",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := y.s.CreateVariable(ctx, p, mid, VariableInput{
		Name: "layers", Source: store.FormulaSourceJob, Kind: store.FormulaKindNumber, Unit: "pcs", Min: ptr(m("1")), Max: ptr(m("4")), Default: ptr(m("2")),
	}); err != nil {
		t.Fatal(err)
	}
	pkg, err := y.s.CreatePackage(ctx, p, mid, PackageInput{
		Code: "ANTIVEG", Name: I18n{It: "Antivegetativa"}, WorkTypeID: y.wt, JobVars: []string{"layers"}, Lines: antivegLines(y.items),
	})
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	y.pkg = pkg.Row.ID
}

func antivegLines(items map[string]string) []PackageLine {
	return []PackageLine{
		{Seq: 1, ItemID: items["SRV-ALAG"], QtyFormula: "loa", PriceFormula: `if(hull_type == "catamaran", list_price * 1.5, list_price)`},
		{Seq: 2, ItemID: items["SRV-LAV"], QtyFormula: "round(hull_area, 1)"},
		{Seq: 3, ItemID: items["LAB-HAND"], QtyFormula: "round(hull_area * 0.15, 1)", Task: &PackageTask{Title: I18n{It: "Carteggiatura"}}},
		{Seq: 4, ItemID: items["LAB-PAINT"], QtyFormula: "round(hull_area * layers * 0.05, 1)", Task: &PackageTask{Title: I18n{It: "Antivegetativa"}}},
		{Seq: 5, ItemID: items["MAT-AVEG"], QtyFormula: "ceil(hull_area * layers / 25)"},
		{Seq: 6, ItemID: items["SRV-VARO"], QtyFormula: "loa"},
	}
}

func (y *yenv) quote(t *testing.T, mutate ...func(*QuoteInput)) *QuoteView {
	t.Helper()
	in := QuoteInput{CustomerID: y.cust, VesselID: y.vessel, Selections: []Selection{{PackageID: y.pkg}}}
	for _, f := range mutate {
		f(&in)
	}
	q, err := y.s.Create(ctxT(), y.Owner, y.MarinaID, in)
	if err != nil {
		t.Fatalf("create quote: %v", err)
	}
	return q
}

// storedEqualsRecompute checks INV-28 on a stored quote: the line rows equal Recompute(snapshot).
func (y *yenv) storedEqualsRecompute(t *testing.T, id string) {
	t.Helper()
	r, err := store.GetYardQuote(ctxT(), y.DB.DB, y.scope, id, false)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.YardQuoteLines(ctxT(), y.DB.DB, y.scope, id)
	if err != nil {
		t.Fatal(err)
	}
	lines, totals, err := price.Recompute(snapshotOf(r.Snapshot))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(lines) || totals.Gross != money.Cents(r.GrossCents) || totals.Net != money.Cents(r.NetCents) {
		t.Fatalf("recompute differs: %d rows, %d lines, gross %d vs %d", len(rows), len(lines), r.GrossCents, totals.Gross)
	}
	for i, l := range lines {
		row := rows[i]
		if row.QtyMilli != int64(l.Qty) || row.UnitCents != int64(l.Unit) || row.NetCents != int64(l.Net) || row.VATCents != int64(l.VAT) ||
			row.QtyFormula != l.Spec.QtyFormula || row.PriceFormula != l.Spec.PriceFormula {
			t.Fatalf("line %d differs: %+v vs %+v", i, row, l)
		}
	}
}

func TestYardQuote_Antifouling12m(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		y := newYenv(t, db)
		q := y.quote(t)
		if q.Totals.Net != 170260 || q.Totals.VAT != 37457 || q.Totals.Gross != 207717 || q.Totals.Deposit != 62315 || q.Blocking {
			t.Fatalf("totals %+v blocking %v", q.Totals, q.Blocking)
		}
		if q.Number != "PC2027-000001" || q.Row.Status != store.YardQuoteDraft || q.Row.ValidUntil != calendar.MustParse("2027-07-02") {
			t.Fatalf("row %+v %s", q.Row, q.Number)
		}
		for _, l := range q.Lines {
			if l.Manual() {
				t.Fatalf("manual line %+v", l)
			}
		}
		if len(q.Hours) != 2 || q.Hours[0].Hours+q.Hours[1].Hours != m("14") {
			t.Fatalf("hours %+v", q.Hours)
		}
		if q.Lines[2].Spec.Task == nil || q.Lines[2].Spec.Task.Title["it"] != "Carteggiatura" || q.Lines[2].QualificationID == "" {
			t.Fatalf("labour line %+v", q.Lines[2])
		}
		y.storedEqualsRecompute(t, q.Row.ID)
		if n := len(y.Events(t, event.YardQuoteChanged)); n != 1 {
			t.Fatalf("%d events", n)
		}
		// Preview is the same computation without writing.
		pv, err := y.s.Preview(ctxT(), y.Owner, y.MarinaID, QuoteInput{
			CustomerID: y.cust, VesselID: y.vessel, Selections: []Selection{{PackageID: y.pkg}}, JobInputs: map[string]Value{"layers": numVal(m("3"))},
		})
		if err != nil {
			t.Fatal(err)
		}
		if pv.Lines[3].Net != 37800 || pv.Lines[4].Net != 66500 {
			t.Fatalf("layers 3: %+v", pv.Lines)
		}
	})
}

func TestYardQuote_OldQuoteKeepsFormula(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		y := newYenv(t, db)
		ctx := ctxT()
		sent := y.quote(t)
		if _, err := y.s.Send(ctx, y.Owner, y.MarinaID, sent.Row.ID, sent.Row.Version, true, ""); err != nil {
			t.Fatal(err)
		}
		draft := y.quote(t)
		before, _ := y.s.Get(ctx, y.Owner, y.MarinaID, sent.Row.ID)
		// Package v2 with other formulas, a new hull_area and new prices.
		pk, _ := y.s.GetPackage(ctx, y.Owner, y.MarinaID, y.pkg, 0)
		lines := antivegLines(y.items)
		lines[1].QtyFormula = "round(hull_area * 2, 1)"
		if _, err := y.s.SaveVersion(ctx, y.Owner, y.MarinaID, y.pkg, pk.Row.Version, VersionInput{Lines: lines}); err != nil {
			t.Fatal(err)
		}
		vars, _ := y.s.ListVariables(ctx, y.Owner, y.MarinaID)
		for _, v := range vars {
			if v.Name == "hull_area" {
				expr := "loa * (beam + draft)"
				if _, err := y.s.UpdateVariable(ctx, y.Owner, y.MarinaID, v.Row.ID, v.Row.Version, VariablePatch{Expression: &expr}); err != nil {
					t.Fatal(err)
				}
			}
		}
		after, _ := y.s.Get(ctx, y.Owner, y.MarinaID, sent.Row.ID)
		if !reflect.DeepEqual(before.Lines, after.Lines) || before.Totals.Gross != after.Totals.Gross {
			t.Fatal("an old quote changed after the catalogue changed")
		}
		y.storedEqualsRecompute(t, sent.Row.ID)
		// Ricalcola on the draft picks the new formulas and is audited.
		rq, err := y.s.Recompute(ctx, y.Owner, y.MarinaID, draft.Row.ID, draft.Row.Version)
		if err != nil {
			t.Fatal(err)
		}
		if rq.Totals.Gross == draft.Totals.Gross || rq.Lines[1].Spec.PackageVersion != 2 {
			t.Fatalf("recompute kept the old catalogue: %+v", rq.Lines[1].Spec)
		}
		if y.AuditCount(t, "yard_quote.recompute") != 1 {
			t.Fatal("recompute not audited")
		}
		if _, err := y.s.Recompute(ctx, y.Owner, y.MarinaID, sent.Row.ID, after.Row.Version); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("recompute of a sent quote: %v", err)
		}
		// The version 1 is still readable.
		v1, err := y.s.GetPackage(ctx, y.Owner, y.MarinaID, y.pkg, 1)
		if err != nil || v1.Lines[1].QtyFormula != "round(hull_area, 1)" || len(v1.Versions) != 2 {
			t.Fatalf("v1 %+v %v", v1, err)
		}
	})
}

func TestYardQuote_CatalogueValidation(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		y := newYenv(t, db)
		ctx, p, mid := ctxT(), y.Owner, y.MarinaID
		var fe *FormulaError
		_, err := y.s.CreatePackage(ctx, p, mid, PackageInput{Code: "BAD", Name: I18n{It: "x"}, WorkTypeID: y.wt, Lines: []PackageLine{
			{Seq: 1, ItemID: y.items["SRV-LAV"], QtyFormula: "hull_area * nope"},
			{Seq: 2, ItemID: y.items["SRV-LAV"], QtyFormula: "1 +"},
			{Seq: 3, ItemID: y.items["SRV-LAV"], QtyFormula: "1 / (loa - 12)"},
			{Seq: 4, ItemID: y.items["SRV-LAV"], QtyFormula: "loa", TestCase: &TestCase{ExpectQty: ptr(m("13"))}},
		}})
		if !errors.As(err, &fe) || len(fe.Issues) != 4 {
			t.Fatalf("package: %v", err)
		}
		codes := map[string]bool{}
		for _, is := range fe.Issues {
			codes[is.Code] = true
		}
		for _, c := range []string{formula.CodeUnknownVariable, formula.CodeSyntax, formula.CodeDivByZero, IssueTestCaseMismatch} {
			if !codes[c] {
				t.Fatalf("missing %s in %+v", c, fe.Issues)
			}
		}
		// Changing hull_area to a boolean breaks ANTIVEG: the save is refused and names the package.
		vars, _ := y.s.ListVariables(ctx, p, mid)
		var hull, layers *VariableView
		for i := range vars {
			switch vars[i].Name {
			case "hull_area":
				hull = &vars[i]
			case "layers":
				layers = &vars[i]
			}
		}
		expr := "loa > 10"
		_, err = y.s.UpdateVariable(ctx, p, mid, hull.Row.ID, hull.Row.Version, VariablePatch{Expression: &expr})
		if !errors.As(err, &fe) {
			t.Fatalf("variable: %v", err)
		}
		cycle := "hull_area2 * 2"
		if _, err := y.s.CreateVariable(ctx, p, mid, VariableInput{Name: "hull_area2", Source: "derived", Kind: "number", Expression: cycle}); !errors.As(err, &fe) {
			t.Fatalf("cycle: %v", err)
		}
		if err := y.s.DeleteVariable(ctx, p, mid, layers.Row.ID, layers.Row.Version); !errors.Is(err, ErrInUse) {
			t.Fatalf("delete used variable: %v", err)
		}
		if _, err := y.s.CreateVariable(ctx, p, mid, VariableInput{Name: "loa", Source: "job", Kind: "number"}); err == nil {
			t.Fatal("built-in name accepted")
		}
		if _, err := y.s.CreateWorkType(ctx, p, mid, WorkTypeInput{Code: "ANTIFOULING", Name: I18n{It: "x"}}); !errors.Is(err, ErrCodeTaken) {
			t.Fatalf("duplicate code: %v", err)
		}
		// A labour item needs its qualification; a non-labour item cannot have one.
		if _, err := y.s.PutItemSettings(ctx, p, mid, y.items["SRV-VARO"], 0, ItemSettingsInput{QualificationID: ptr("x")}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("qualification on a service: %v", err)
		}
		// Tables: ascending rows and band() in a package.
		if _, err := y.s.CreateTable(ctx, p, mid, TableInput{Name: "lift", Rows: []formula.Row{{UpTo: ptr(m("10")), Value: m("1")}, {UpTo: ptr(m("5")), Value: m("2")}}}); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("table rows: %v", err)
		}
		tv, err := y.s.CreateTable(ctx, p, mid, TableInput{Name: "lift", Rows: []formula.Row{{UpTo: ptr(m("10")), Value: m("100")}, {Value: m("200")}}})
		if err != nil {
			t.Fatal(err)
		}
		res, err := y.s.ValidateFormula(ctx, p, mid, ValidateInput{Formula: "band(loa, lift)"})
		if err != nil || !res.Valid || res.Qty == nil || *res.Qty != m("200") {
			t.Fatalf("validate %+v %v", res, err)
		}
		res, _ = y.s.ValidateFormula(ctx, p, mid, ValidateInput{Formula: "list_price * 1.5", Purpose: PurposePrice, ItemID: y.items["SRV-ALAG"]})
		if !res.Valid || res.Price == nil || *res.Price != 2700 {
			t.Fatalf("validate price %+v", res)
		}
		res, _ = y.s.ValidateFormula(ctx, p, mid, ValidateInput{Formula: "loa +"})
		if res.Valid || len(res.Issues) != 1 || res.Issues[0].Code != formula.CodeSyntax {
			t.Fatalf("validate syntax %+v", res)
		}
		_ = tv
	})
}

func TestYardQuote_Lifecycle(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		y := newYenv(t, db)
		ctx, p, mid := ctxT(), y.Owner, y.MarinaID
		// No email: 422 unless manual.
		q := y.quote(t, func(in *QuoteInput) { in.CustomerID = y.noEmail })
		if _, err := y.s.Send(ctx, p, mid, q.Row.ID, q.Row.Version, false, ""); !errors.Is(err, ErrCustomerEmailMissing) {
			t.Fatalf("send without email: %v", err)
		}
		// Send with email: document, link and message.
		q = y.quote(t)
		res, err := y.s.Send(ctx, p, mid, q.Row.ID, q.Row.Version, false, "")
		if err != nil {
			t.Fatal(err)
		}
		if res.Quote.Row.Status != store.YardQuoteSent || res.DocumentID == "" || res.LinkURL == "" || res.MessageID == "" {
			t.Fatalf("send %+v", res)
		}
		if n := len(y.Events(t, event.YardQuoteSent)); n != 1 {
			t.Fatalf("%d sent events", n)
		}
		// Blocking: no price.
		np := y.quote(t, func(in *QuoteInput) { in.Selections = []Selection{{ItemID: y.items["SRV-NOPRICE"]}} })
		if !np.Blocking {
			t.Fatal("no price not blocking")
		}
		var be *BlockingError
		if _, err := y.s.Send(ctx, p, mid, np.Row.ID, np.Row.Version, true, ""); !errors.As(err, &be) || !errors.Is(err, ErrNoPrice) {
			t.Fatalf("send blocked: %v", err)
		}
		// A marked manual price unblocks it.
		upd, err := y.s.Update(ctx, p, mid, np.Row.ID, np.Row.Version, QuotePatch{Overrides: &[]price.Override{{Seq: 1, Unit: ptr(money.Cents(5000))}}})
		if err != nil || upd.Blocking || !upd.Lines[0].PriceOverridden || !upd.Lines[0].Manual() {
			t.Fatalf("override %+v %v", upd, err)
		}
		// Accept from the panel: the order side is called once; a second accept is an invalid transition.
		acc, err := y.s.Accept(ctx, p, mid, q.Row.ID, res.Quote.Row.Version)
		if err != nil {
			t.Fatal(err)
		}
		if acc.Quote.Row.Status != store.YardQuoteAccepted || acc.Quote.Row.AcceptedVia != store.AcceptedViaPanel || acc.Order.WorkOrderID == "" {
			t.Fatalf("accept %+v", acc)
		}
		if _, err := y.s.Accept(ctx, p, mid, q.Row.ID, acc.Quote.Row.Version); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("accept twice: %v", err)
		}
		if y.orders.committed.Load() != 1 {
			t.Fatalf("orders %d", y.orders.committed.Load())
		}
		// A failing order side rolls the acceptance back.
		q2 := y.quote(t)
		y.orders.fail = errors.New("boom")
		if _, err := y.s.Accept(ctx, p, mid, q2.Row.ID, q2.Row.Version); err == nil {
			t.Fatal("accept with a failing order side")
		}
		y.orders.fail = nil
		if again, _ := y.s.Get(ctx, p, mid, q2.Row.ID); again.Row.Status != store.YardQuoteDraft {
			t.Fatalf("not rolled back: %s", again.Row.Status)
		}
		// Decline, then expiry.
		if d, err := y.s.Decline(ctx, p, mid, q2.Row.ID, q2.Row.Version, "troppo caro"); err != nil || d.Row.Status != store.YardQuoteDeclined {
			t.Fatalf("decline %v", err)
		}
		q3 := y.quote(t)
		y.Clock.Set(contracttest.T0.Add(40 * 24 * time.Hour))
		if _, err := y.s.Accept(ctx, p, mid, q3.Row.ID, q3.Row.Version); !errors.Is(err, ErrQuoteExpired) {
			t.Fatalf("accept expired: %v", err)
		}
		if err := y.s.RunExpire(ctx); err != nil {
			t.Fatal(err)
		}
		if err := y.s.RunExpire(ctx); err != nil {
			t.Fatal(err)
		}
		if e3, _ := y.s.Get(ctx, p, mid, q3.Row.ID); e3.Row.Status != store.YardQuoteExpired {
			t.Fatalf("not expired: %s", e3.Row.Status)
		}
		if n := len(y.Events(t, event.YardQuoteExpired)); n != 3 { // q3, the email-less draft and np
			t.Fatalf("%d expired events", n)
		}
	})
}

func TestYardQuote_PublicLink(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		y := newYenv(t, db)
		ctx := ctxT()
		q := y.quote(t, func(in *QuoteInput) { in.Conditions = "Saldo alla consegna." })
		res, err := y.s.Send(ctx, y.Owner, y.MarinaID, q.Row.ID, q.Row.Version, true, "")
		if err != nil {
			t.Fatal(err)
		}
		m, _ := store.GetMarina(ctx, db.DB, y.scope)
		token := res.LinkURL[len(res.LinkURL)-magiclink.TokenChars:]
		if !y.s.IsYardQuoteLink(ctx, m.Slug, token) {
			t.Fatal("not a yard quote link")
		}
		pq, _, err := y.s.PublicLinkQuote(ctx, m.Slug, token)
		if err != nil || !pq.CanAccept || pq.Gross != 207717 || pq.VesselName != "Maestrale" || len(pq.Lines) != 6 {
			t.Fatalf("public %+v %v", pq, err)
		}
		if _, err := y.s.AcceptPublic(ctx, m.Slug, token, false); !errors.Is(err, auth.ErrInvalidInput) {
			t.Fatalf("conditions not accepted: %v", err)
		}
		pa, err := y.s.AcceptPublic(ctx, m.Slug, token, true)
		if err != nil || pa.QuoteNumber != q.Number || pa.WorkOrderNumber == "" {
			t.Fatalf("accept via link %+v %v", pa, err)
		}
		got, _ := y.s.Get(ctx, y.Owner, y.MarinaID, q.Row.ID)
		if got.Row.AcceptedVia != store.AcceptedViaLink {
			t.Fatalf("via %s", got.Row.AcceptedVia)
		}
		// The link is revoked by the acceptance; a foreign token is invalid.
		if _, err := y.s.AcceptPublic(ctx, m.Slug, token, true); !errors.Is(err, magiclink.ErrInvalid) {
			t.Fatalf("reuse: %v", err)
		}
		if _, _, err := y.s.PublicLinkQuote(ctx, m.Slug, "x"); !errors.Is(err, magiclink.ErrInvalid) {
			t.Fatalf("bad token: %v", err)
		}
	})
}

func TestYardQuote_CrossMarinaAndPermissions(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		y := newYenv(t, db)
		ctx := ctxT()
		q := y.quote(t)
		_, otherMarina, otherOwner := y.OtherOrganization(t)
		if _, err := y.s.Get(ctx, otherOwner, y.MarinaID, q.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("other organization: %v", err)
		}
		if _, err := y.s.Get(ctx, otherOwner, otherMarina, q.Row.ID); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("id of another marina: %v", err)
		}
		yard := y.AddUser(t, "yard@x.test", auth.RoleYard, y.OrgID, y.MarinaID)
		if _, err := y.s.Get(ctx, yard, y.MarinaID, q.Row.ID); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("yard role reads quotes: %v", err)
		}
		mgr := y.AddUser(t, "mgr@x.test", auth.RoleYardMgr, y.OrgID, y.MarinaID)
		if _, err := y.s.Get(ctx, mgr, y.MarinaID, q.Row.ID); err != nil {
			t.Fatalf("yard_mgr: %v", err)
		}
		raw, _ := json.Marshal(y.Events(t, event.YardQuoteChanged))
		if len(raw) == 0 || json.Valid(raw) == false {
			t.Fatal("events")
		}
	})
}

// TestConcurrency_YardQuoteAccept: 100 concurrent accepts of one quote create one work order (INV-96); the others
// are 409 invalid_transition.
func TestConcurrency_YardQuoteAccept(t *testing.T) {
	testdb.Run(t, func(t *testing.T, db *store.DB) {
		y := newYenv(t, db)
		q := y.quote(t)
		var wg sync.WaitGroup
		var ok, conflict atomic.Int32
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := y.s.Accept(ctxT(), y.Owner, y.MarinaID, q.Row.ID, 0)
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrInvalidTransition):
					conflict.Add(1)
				case store.IsSerializationFailure(err) || errors.Is(err, context.DeadlineExceeded):
					conflict.Add(1)
				default:
					t.Errorf("accept: %v", err)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 || conflict.Load() != 99 || y.orders.committed.Load() != 1 {
			t.Fatalf("ok %d conflict %d orders %d", ok.Load(), conflict.Load(), y.orders.committed.Load())
		}
	})
}
