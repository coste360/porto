package cash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/cash/summary"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Z report of a session (m5-plan §5.1): header, operators, totals per method (expected, counted, difference), VAT
// totals per rate from the payments' frozen split, the range of fiscal documents and the movements. Print-ready
// HTML or a PDF note.

// ZReport is the data of a Z report.
type ZReport struct {
	MarinaName   string
	RegisterCode string
	RegisterName string
	Session      store.CashSession
	OpenedBy     string
	ClosedBy     string
	Summary      summary.Result
	VAT          []summary.VATRow
	FiscalFirst  string
	FiscalLast   string
	FiscalCount  int
	Movements    []store.CashMovement
	Locale       string
}

// ZReportOf builds the Z report of a session (payments.collect or payments.collect_cash).
func (s *Service) ZReportOf(ctx context.Context, p *auth.Principal, marinaID, sessionID string) (*ZReport, error) {
	scope, _, err := s.authorizeCollect(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	sess, err := store.GetCashSession(ctx, db, scope, sessionID)
	if err != nil {
		return nil, err
	}
	v, err := s.sessionView(ctx, db, scope, sess, "")
	if err != nil {
		return nil, err
	}
	z := &ZReport{Session: *sess, Summary: v.Summary, Movements: v.Movements, RegisterCode: v.RegisterCode, Locale: "it"}
	if m, err := store.GetMarina(ctx, db, scope); err == nil {
		z.MarinaName = m.Name
		if m.DefaultLocale != "" {
			z.Locale = m.DefaultLocale
		}
	}
	if r, err := store.GetCashRegister(ctx, db, scope, sess.RegisterID); err == nil {
		z.RegisterName = r.Name
	}
	names, err := store.UserNames(ctx, db, []string{sess.OpenedByUserID, sess.ClosedByUserID})
	if err != nil {
		return nil, err
	}
	z.OpenedBy, z.ClosedBy = names[sess.OpenedByUserID], names[sess.ClosedByUserID]
	pays, err := store.SessionPayments(ctx, db, scope, sess.ID)
	if err != nil {
		return nil, err
	}
	var rows []summary.VATRow
	for _, pay := range pays {
		var split []struct {
			RateBP int64       `json:"vat_rate_bp"`
			Nature string      `json:"vat_nature"`
			Net    money.Cents `json:"net_cents"`
			VAT    money.Cents `json:"vat_cents"`
			Gross  money.Cents `json:"gross_cents"`
		}
		_ = json.Unmarshal(pay.VATSplit, &split)
		for _, b := range split {
			rows = append(rows, summary.VATRow{RateBP: b.RateBP, Nature: b.Nature, Gross: b.Gross, Net: b.Net, VAT: b.VAT})
		}
	}
	z.VAT = summary.VATTotals(rows)
	docsPage, err := store.ListFiscalDocuments(ctx, db, scope, store.FiscalDocumentFilter{SessionID: sess.ID}, "", 200)
	if err != nil {
		return nil, err
	}
	for _, d := range docsPage.Items {
		if d.Status == store.FiscalVoid {
			continue
		}
		z.FiscalCount++
		if z.FiscalFirst == "" || d.Number < z.FiscalFirst {
			z.FiscalFirst = d.Number
		}
		if d.Number > z.FiscalLast {
			z.FiscalLast = d.Number
		}
	}
	return z, nil
}

type zLabels struct {
	Lang, Title, Register, Day, Opened, Closed, Open, Float, Method, Expected, Counted, Difference string
	VAT, Rate, Net, Tax, Gross, Fiscal, Movements, In, Out, Payments, Refunds, Total               string
}

var zText = map[string]zLabels{
	"it": {
		Lang: "it", Title: "Chiusura di cassa (Z)", Register: "Cassa", Day: "Giornata", Opened: "Aperta", Closed: "Chiusa",
		Open: "aperta", Float: "Fondo cassa", Method: "Metodo", Expected: "Atteso", Counted: "Contato", Difference: "Differenza",
		VAT: "Riepilogo IVA incassato", Rate: "Aliquota", Net: "Imponibile", Tax: "IVA", Gross: "Totale",
		Fiscal: "Documenti commerciali", Movements: "Movimenti", In: "Versamento", Out: "Prelievo", Payments: "Incassi",
		Refunds: "Rimborsi", Total: "Totale",
	},
	"en": {
		Lang: "en", Title: "Cash register close (Z)", Register: "Register", Day: "Business day", Opened: "Opened", Closed: "Closed",
		Open: "open", Float: "Float", Method: "Method", Expected: "Expected", Counted: "Counted", Difference: "Difference",
		VAT: "VAT collected", Rate: "Rate", Net: "Net", Tax: "VAT", Gross: "Gross", Fiscal: "Commercial documents",
		Movements: "Movements", In: "Cash in", Out: "Cash out", Payments: "Payments", Refunds: "Refunds", Total: "Total",
	},
}

func zLabelsFor(locale string) zLabels {
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		return zText["en"]
	}
	return zText["it"]
}

func euro(c money.Cents) string { return strings.Replace(c.String(), ".", ",", 1) }

var zTmpl = template.Must(template.New("z").Funcs(template.FuncMap{
	"money": euro,
	"rate":  rateLabel,
	"cents": func(n int64) string { return euro(money.Cents(n)) },
}).Parse(`<!doctype html>
<html lang="{{.L.Lang}}"><head><meta charset="utf-8"><title>{{.L.Title}}</title>
<style>
@page { size: 80mm auto; margin: 2mm; }
body { width: 72mm; margin: 0 auto; font-family: monospace; font-size: 9pt; color: #000; }
h1 { font-size: 1.1em; text-align: center; margin: 0 0 2mm; } table { width: 100%; border-collapse: collapse; }
td { padding: 0; } .r { text-align: right; } hr { border: 0; border-top: 1px dashed #000; margin: 2mm 0; } .b { font-weight: bold; }
</style></head><body>
<h1>{{.Z.MarinaName}}</h1>
<div class="b">{{.L.Title}}</div>
<div>{{.L.Register}}: {{.Z.RegisterCode}}{{with .Z.RegisterName}} – {{.}}{{end}}</div>
<div>{{.L.Day}}: {{.Day}}</div>
<div>{{.L.Opened}}: {{.Opened}}{{with .Z.OpenedBy}} ({{.}}){{end}}</div>
<div>{{.L.Closed}}: {{if .Closed}}{{.Closed}}{{with .Z.ClosedBy}} ({{.}}){{end}}{{else}}{{.L.Open}}{{end}}</div>
<hr>
<table><tr><td>{{.L.Float}}</td><td class="r">{{cents .Z.Session.FloatCents}}</td></tr>
<tr><td>{{.L.Payments}}</td><td class="r">{{money .Z.Summary.Payments}}</td></tr>
<tr><td>{{.L.Refunds}}</td><td class="r">{{money .Z.Summary.Refunds}}</td></tr>
<tr><td>{{.L.In}}</td><td class="r">{{money .Z.Summary.MovementsIn}}</td></tr>
<tr><td>{{.L.Out}}</td><td class="r">{{money .Z.Summary.MovementsOut}}</td></tr></table>
<hr>
<table><tr class="b"><td>{{.L.Method}}</td><td class="r">{{.L.Expected}}</td><td class="r">{{.L.Counted}}</td></tr>
{{range $i, $e := .Z.Summary.Expected}}<tr><td>{{$e.Method}}</td><td class="r">{{money $e.Amount}}</td><td class="r">{{money (index $.Z.Summary.Counted $i).Amount}}</td></tr>{{end}}
<tr class="b"><td>{{.L.Total}}</td><td class="r">{{money .Z.Summary.ExpectedTotal}}</td><td class="r">{{money .Z.Summary.CountedTotal}}</td></tr>
<tr class="b"><td>{{.L.Difference}}</td><td></td><td class="r">{{money .Z.Summary.Difference}}</td></tr></table>
{{if .Z.VAT}}<hr><div class="b">{{.L.VAT}}</div><table>{{range .Z.VAT}}<tr><td>{{if .Nature}}{{.Nature}}{{else}}{{rate .RateBP}}%{{end}}</td><td class="r">{{money .Net}}</td><td class="r">{{money .VAT}}</td><td class="r">{{money .Gross}}</td></tr>{{end}}</table>{{end}}
{{if .Z.FiscalCount}}<hr><div>{{.L.Fiscal}}: {{.Z.FiscalCount}} ({{.Z.FiscalFirst}} – {{.Z.FiscalLast}})</div>{{end}}
{{if .Z.Movements}}<hr><div class="b">{{.L.Movements}}</div><table>{{range .Z.Movements}}<tr><td>{{if eq .Kind "in"}}{{$.L.In}}{{else}}{{$.L.Out}}{{end}} {{.Reason}}</td><td class="r">{{cents .AmountCents}}</td></tr>{{end}}</table>{{end}}
</body></html>
`))

func (z *ZReport) times() (day, opened, closed string) {
	day = italianDate(z.Session.BusinessDate.String())
	opened = z.Session.OpenedAt.UTC().Format("02/01/2006 15:04 UTC")
	if !z.Session.ClosedAt.IsZero() {
		closed = z.Session.ClosedAt.UTC().Format("02/01/2006 15:04 UTC")
	}
	return day, opened, closed
}

// HTML renders the Z report as print-ready HTML (80 mm, no script, no external resource).
func (z *ZReport) HTML() ([]byte, error) {
	day, opened, closed := z.times()
	var b bytes.Buffer
	err := zTmpl.Execute(&b, map[string]any{"Z": z, "L": zLabelsFor(z.Locale), "Day": day, "Opened": opened, "Closed": closed})
	return b.Bytes(), err
}

// PDF renders the Z report as a one-page PDF note.
func (z *ZReport) PDF() ([]byte, error) {
	l := zLabelsFor(z.Locale)
	day, opened, closed := z.times()
	if closed == "" {
		closed = l.Open
	}
	paras := []string{
		z.MarinaName,
		fmt.Sprintf("%s: %s %s · %s: %s", l.Register, z.RegisterCode, z.RegisterName, l.Day, day),
		fmt.Sprintf("%s: %s · %s: %s", l.Opened, opened, l.Closed, closed),
		fmt.Sprintf("%s %s · %s %s · %s %s · %s %s · %s %s", l.Float, euro(money.Cents(z.Session.FloatCents)), l.Payments,
			euro(z.Summary.Payments), l.Refunds, euro(z.Summary.Refunds), l.In, euro(z.Summary.MovementsIn), l.Out, euro(z.Summary.MovementsOut)),
	}
	for i, e := range z.Summary.Expected {
		paras = append(paras, fmt.Sprintf("%s · %s %s · %s %s", e.Method, l.Expected, euro(e.Amount), l.Counted, euro(z.Summary.Counted[i].Amount)))
	}
	paras = append(paras, fmt.Sprintf("%s %s · %s %s · %s %s", l.Expected, euro(z.Summary.ExpectedTotal), l.Counted,
		euro(z.Summary.CountedTotal), l.Difference, euro(z.Summary.Difference)))
	for _, v := range z.VAT {
		label := v.Nature
		if label == "" {
			label = rateLabel(v.RateBP) + "%"
		}
		paras = append(paras, fmt.Sprintf("%s %s · %s %s · %s %s · %s %s", l.Rate, label, l.Net, euro(v.Net), l.Tax, euro(v.VAT), l.Gross, euro(v.Gross)))
	}
	if z.FiscalCount > 0 {
		paras = append(paras, fmt.Sprintf("%s: %d (%s – %s)", l.Fiscal, z.FiscalCount, z.FiscalFirst, z.FiscalLast))
	}
	for _, m := range z.Movements {
		k := l.Out
		if m.Kind == MovementIn {
			k = l.In
		}
		paras = append(paras, fmt.Sprintf("%s %s %s", k, euro(money.Cents(m.AmountCents)), m.Reason))
	}
	at := z.Session.OpenedAt
	if !z.Session.ClosedAt.IsZero() {
		at = z.Session.ClosedAt
	}
	return docs.RenderNote(l.Title, paras, z.Locale, at)
}
