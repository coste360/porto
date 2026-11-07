package cash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Courtesy receipts (m5-plan §5.3): 80/58 mm self-contained print-ready HTML (no script, no external resource)
// or A4 PDF through the docs kind receipt (rendered ephemerally). Both always carry "Non valido ai fini fiscali".

// Receipt formats.
const (
	Receipt80 = "80"
	Receipt58 = "58"
	ReceiptA4 = "a4"
)

// NotFiscalIT is the fixed notice of every courtesy receipt.
const NotFiscalIT = "Non valido ai fini fiscali"

// Receipt renders the receipt of a payment: HTML (80, 58) or PDF (a4). Allowed with payments.collect or
// charges.manage, and with payments.collect_cash for cash payments.
func (s *Service) Receipt(ctx context.Context, p *auth.Principal, marinaID, paymentID, format string) ([]byte, string, error) {
	scope, cashOnly, err := s.authorizeCollect(ctx, p, marinaID)
	if errors.Is(err, auth.ErrForbidden) {
		scope, err = p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
		cashOnly = false
	}
	if err != nil {
		return nil, "", err
	}
	if format == "" {
		format = Receipt80
	}
	if format != Receipt80 && format != Receipt58 && format != ReceiptA4 {
		return nil, "", invalid("format", "invalid")
	}
	pay, err := store.GetPayment(ctx, s.d.DB.DB, scope, paymentID)
	if err != nil {
		return nil, "", err
	}
	if cashOnly && pay.Method != store.PaymentMethodCash {
		return nil, "", ErrNotFound
	}
	v, locale, err := s.ReceiptViewOf(ctx, s.d.DB.DB, scope, pay)
	if err != nil {
		return nil, "", err
	}
	if format == ReceiptA4 {
		if s.d.Docs == nil {
			return nil, "", ErrNotFound
		}
		b, err := s.d.Docs.RenderEphemeral(ctx, scope, docs.KindReceipt, locale, v)
		return b, "application/pdf", err
	}
	b, err := ReceiptHTML(v, format, locale)
	return b, "text/html; charset=utf-8", err
}

// ReceiptViewOf builds the receipt data of a payment (also the source of the docs kind receipt): the sold lines
// of a counter sale, otherwise the matched charges; the VAT split frozen on the payment; the fiscal number when a
// commercial document exists; the footer of the billing settings.
func (s *Service) ReceiptViewOf(ctx context.Context, db bun.IDB, scope store.MarinaScope, pay *store.Payment) (views.ReceiptView, string, error) {
	var v views.ReceiptView
	header, err := docs.MarinaHeader(ctx, db, scope, nil)
	if err != nil {
		return v, "", err
	}
	locale := "it"
	if m, err := store.GetMarina(ctx, db, scope); err == nil && m.DefaultLocale != "" {
		locale = m.DefaultLocale
	}
	v = views.ReceiptView{
		Marina: header, Number: pay.Number, Date: pay.ReceivedOn, Method: pay.Method, AmountCents: money.Cents(pay.AmountCents),
		Lines: []views.ReceiptLine{}, VAT: []views.VATRow{},
	}
	if pay.CustomerID != "" {
		if cs, err := store.CustomersByIDs(ctx, db, scope, []string{pay.CustomerID}); err == nil && len(cs) == 1 {
			party := docs.PartyOf(&cs[0])
			v.Customer = &party
			if cs[0].Locale != "" {
				locale = cs[0].Locale
			}
		}
	}
	var matches []struct {
		ChargeID string `bun:"charge_id"`
		Amount   int64  `bun:"amount"`
	}
	if err := db.NewSelect().TableExpr("payment_matches").ColumnExpr("charge_id, SUM(amount_cents) AS amount").
		Where("marina_id = ?", scope.ID()).Where("payment_id = ?", pay.ID).GroupExpr("charge_id").
		Having("SUM(amount_cents) <> 0").OrderExpr("charge_id").Scan(ctx, &matches); err != nil {
		return v, "", err
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, m.ChargeID)
	}
	charges, err := store.CollectChargesByIDs(ctx, db, scope, ids)
	if err != nil {
		return v, "", err
	}
	byID := map[string]store.Charge{}
	for _, c := range charges {
		byID[c.ID] = c
	}
	for _, m := range matches {
		ch := byID[m.ChargeID]
		if ch.OriginType == store.ChargeOriginCounterSale && m.Amount == ch.TotalCents {
			lines, err := store.CollectChargeLines(ctx, db, scope, ch.ID)
			if err != nil {
				return v, "", err
			}
			for _, l := range lines {
				v.Lines = append(v.Lines, views.ReceiptLine{Description: l.Description, Qty: views.Millis(l.QtyMilli), Gross: money.Cents(l.GrossCents), VATRate: views.BP(l.VATRateBP)})
			}
			continue
		}
		desc := strings.TrimSpace(ch.Number + " " + ch.Description)
		v.Lines = append(v.Lines, views.ReceiptLine{Description: desc, Qty: 1000, Gross: money.Cents(m.Amount)})
	}
	var split []struct {
		RateBP int64       `json:"vat_rate_bp"`
		Nature string      `json:"vat_nature"`
		Net    money.Cents `json:"net_cents"`
		VAT    money.Cents `json:"vat_cents"`
		Gross  money.Cents `json:"gross_cents"`
	}
	_ = json.Unmarshal(pay.VATSplit, &split)
	for _, b := range split {
		if b.Gross == 0 {
			continue
		}
		v.VAT = append(v.VAT, views.VATRow{RateBP: views.BP(b.RateBP), Nature: b.Nature, Net: b.Net, VAT: b.VAT, Gross: b.Gross})
	}
	if docsOf, err := store.FiscalDocumentsOfPayment(ctx, db, scope, pay.ID); err == nil {
		for _, d := range docsOf {
			if d.Status != store.FiscalVoid && d.RefundID == "" {
				v.FiscalNumber = d.Number
			}
		}
	}
	if bs, err := store.GetBillingSettings(ctx, db, scope, s.now()); err == nil {
		v.Footer = views.Notes(bs.ReceiptFooter)
	}
	return v, locale, nil
}

type receiptLabels struct {
	Lang, Title, Number, Date, Method, Total, VAT, Rate, Net, Tax, Fiscal, NotFiscal, Customer string
	Methods                                                                                    map[string]string
}

var labels = map[string]receiptLabels{
	"it": {
		Lang: "it", Title: "Ricevuta di cortesia", Number: "Pagamento", Date: "Data", Method: "Metodo", Total: "Totale",
		VAT: "Riepilogo IVA", Rate: "Aliquota", Net: "Imponibile", Tax: "IVA", Fiscal: "Documento commerciale n.",
		NotFiscal: NotFiscalIT, Customer: "Cliente",
		Methods: map[string]string{"cash": "Contanti", //nolint:misspell // Italian
			"card_pos": "Carta (POS)", "bank_transfer": "Bonifico", "cheque": "Assegno", "other": "Altro", "card_online": "Carta online", "wallet": "Wallet", "sepa_debit": "Addebito SEPA", "credit_note": "Credito"},
	},
	"en": {
		Lang: "en", Title: "Courtesy receipt", Number: "Payment", Date: "Date", Method: "Method", Total: "Total",
		VAT: "VAT summary", Rate: "Rate", Net: "Net", Tax: "VAT", Fiscal: "Commercial document no.",
		NotFiscal: NotFiscalIT + " (not valid for tax purposes)", Customer: "Customer",
		Methods: map[string]string{"cash": "Cash", "card_pos": "Card (POS)", "bank_transfer": "Bank transfer", "cheque": "Cheque", "other": "Other", "card_online": "Online card", "wallet": "Wallet", "sepa_debit": "SEPA debit", "credit_note": "Credit"},
	},
}

func labelsFor(locale string) receiptLabels {
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		return labels["en"]
	}
	return labels["it"]
}

var receiptTmpl = template.Must(template.New("receipt").Funcs(template.FuncMap{
	"money": func(c money.Cents) string { return strings.Replace(c.String(), ".", ",", 1) },
	"rate":  func(bp views.BP) string { return rateLabel(int64(bp)) },
	"qty":   func(m views.Millis) string { return qtyLabel(int64(m)) },
	"date":  func(v views.ReceiptView) string { return italianDate(v.Date.String()) },
	"lines": func(n views.Notes) []string { return strings.Split(string(n), "\n") },
}).Parse(`<!doctype html>
<html lang="{{.L.Lang}}"><head><meta charset="utf-8"><title>{{.L.Title}} {{.V.Number}}</title>
<style>
@page { size: {{.Width}}mm auto; margin: 2mm; }
body { width: {{.Body}}mm; margin: 0 auto; font-family: monospace; font-size: {{.Font}}pt; color: #000; }
h1 { font-size: 1.15em; text-align: center; margin: 0 0 2mm; }
.c { text-align: center; } .r { text-align: right; } table { width: 100%; border-collapse: collapse; }
hr { border: 0; border-top: 1px dashed #000; margin: 2mm 0; } .b { font-weight: bold; }
</style></head><body>
<h1>{{.V.Marina.Name}}</h1>
{{with .V.Marina.LegalName}}<div class="c">{{.}}</div>{{end}}
{{with .V.Marina.Address}}<div class="c">{{.}}</div>{{end}}
{{with .V.Marina.VATNumber}}<div class="c">P. IVA {{.}}</div>{{end}}
<hr>
<div class="c b">{{.L.Title}}</div>
<div>{{.L.Number}}: {{.V.Number}}</div>
<div>{{.L.Date}}: {{date .V}}</div>
{{with .V.Customer}}<div>{{$.L.Customer}}: {{.DisplayName}}</div>{{end}}
<hr>
<table>{{range .V.Lines}}<tr><td>{{.Description}}{{if ne .Qty 1000}} x{{qty .Qty}}{{end}}</td><td class="r">{{money .Gross}}</td></tr>{{end}}</table>
<hr>
<table><tr class="b"><td>{{.L.Total}}</td><td class="r">{{money .V.AmountCents}}</td></tr>
<tr><td>{{.L.Method}}</td><td class="r">{{.Method}}</td></tr></table>
{{if .V.VAT}}<hr><div>{{.L.VAT}}</div><table>{{range .V.VAT}}<tr><td>{{if .Nature}}{{.Nature}}{{else}}{{rate .RateBP}}%{{end}}</td><td class="r">{{money .Net}}</td><td class="r">{{money .VAT}}</td></tr>{{end}}</table>{{end}}
{{with .V.FiscalNumber}}<hr><div>{{$.L.Fiscal}} {{.}}</div>{{end}}
{{with .V.Footer}}<hr><div class="c">{{range lines .}}{{.}}<br>{{end}}</div>{{end}}
<hr><div class="c b">{{.L.NotFiscal}}</div>
</body></html>
`))

// ReceiptHTML renders a receipt view as print-ready HTML for an 80 or 58 mm roll.
func ReceiptHTML(v views.ReceiptView, format, locale string) ([]byte, error) {
	l := labelsFor(locale)
	width, body, font := 80, 72, 9
	if format == Receipt58 {
		width, body, font = 58, 50, 8
	}
	method := l.Methods[v.Method]
	if method == "" {
		method = v.Method
	}
	var b bytes.Buffer
	err := receiptTmpl.Execute(&b, map[string]any{"V": v, "L": l, "Width": width, "Body": body, "Font": font, "Method": method})
	return b.Bytes(), err
}

func rateLabel(bp int64) string {
	s := money.Cents(bp).String() // bp has two decimals like cents: 2200 → "22.00"
	s = strings.TrimSuffix(strings.TrimSuffix(s, "0"), "0")
	s = strings.TrimSuffix(s, ".")
	return strings.Replace(s, ".", ",", 1)
}

func qtyLabel(m int64) string { return strings.Replace(qty.Milli(m).String(), ".", ",", 1) }

func italianDate(iso string) string {
	if len(iso) != 10 {
		return iso
	}
	return iso[8:10] + "/" + iso[5:7] + "/" + iso[0:4]
}
