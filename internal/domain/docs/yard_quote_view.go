package docs

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/docs/views"
	"github.com/coste360/porto/internal/money"
)

// The yard quote document (M10, kind yard_quote, m10-plan §4.3; owned by QUOTE). The data source is the yardquote
// service (registered at wiring); the view lives here so the field catalogue, the demo data and the built-in
// templates (defaults/{it,en}/yard_quote.md) stay with the docs package.

// SubjectYardQuote is generated_documents.subject_type of yard quotes.
const SubjectYardQuote SubjectType = "yard_quote"

// YardQuoteInfo are the data of a yard quote.
type YardQuoteInfo struct {
	Number            string        `json:"number" doc:"number,fields.yard_quote.number"`
	Kind              string        `json:"kind" doc:"kind,fields.yard_quote.kind"` // job, variation
	IssuedOn          calendar.Date `json:"issued_on" doc:"issued_on,fields.yard_quote.issued_on"`
	ValidUntil        calendar.Date `json:"valid_until" doc:"valid_until,fields.yard_quote.valid_until"`
	BillingMode       string        `json:"billing_mode" doc:"billing_mode,fields.yard_quote.billing_mode"` // actual, fixed, progress
	PlannedStartOn    calendar.Date `json:"planned_start_on" doc:"planned_start_on,fields.yard_quote.planned_start_on"`
	PlannedDeliveryOn calendar.Date `json:"planned_delivery_on" doc:"planned_delivery_on,fields.yard_quote.planned_delivery_on"`
	WorkOrderNumber   string        `json:"work_order_number" doc:"work_order_number,fields.yard_quote.work_order_number"`
	Conditions        views.Notes   `json:"conditions" doc:"conditions,fields.yard_quote.conditions"`
	Notes             views.Notes   `json:"notes" doc:"notes,fields.yard_quote.notes"`
}

// YardQuoteLine is a priced line of a yard quote.
type YardQuoteLine struct {
	Seq         int         `json:"seq" doc:"seq,fields.yard_quote.line_seq"`
	Description string      `json:"description" doc:"description,fields.yard_quote.line_description"`
	Unit        string      `json:"unit" doc:"unit,fields.yard_quote.line_unit"`
	Qty         string      `json:"qty" doc:"qty,fields.yard_quote.line_qty"` // formatted quantity (e.g. 56,1)
	UnitPrice   money.Cents `json:"unit_price_cents" doc:"unit_price,fields.yard_quote.line_unit_price"`
	Discount    money.Cents `json:"discount_cents" doc:"discount,fields.yard_quote.line_discount"`
	Net         money.Cents `json:"net_cents" doc:"net,fields.yard_quote.line_net"`
	VATRate     views.BP    `json:"vat_rate_bp" doc:"vat_rate,fields.yard_quote.line_vat_rate"`
}

// YardQuoteHours are the estimated hours of a qualification.
type YardQuoteHours struct {
	Qualification string `json:"qualification" doc:"qualification,fields.yard_quote.hours_qualification"`
	Hours         string `json:"hours" doc:"hours,fields.yard_quote.hours_hours"` // formatted hours
}

// YardQuoteView is the data of kind yard_quote.
type YardQuoteView struct {
	Marina   views.Marina     `json:"marina" doc:"marina,fields.groups.marina"`
	Customer views.Party      `json:"customer" doc:"customer,fields.customer"`
	Vessel   views.Vessel     `json:"vessel" doc:"vessel,fields.groups.vessel"`
	Quote    YardQuoteInfo    `json:"quote" doc:"quote,fields.groups.yard_quote"`
	Lines    []YardQuoteLine  `json:"lines" doc:"lines,fields.yard_quote.lines"`
	VAT      []views.VATRow   `json:"vat" doc:"vat,fields.yard_quote.vat"`
	Hours    []YardQuoteHours `json:"hours" doc:"hours,fields.yard_quote.hours"`
	Net      money.Cents      `json:"net_cents" doc:"net,fields.yard_quote.net"`
	VATTotal money.Cents      `json:"vat_cents" doc:"vat_total,fields.yard_quote.vat_total"`
	Total    money.Cents      `json:"gross_cents" doc:"total,fields.yard_quote.total"`
	Deposit  money.Cents      `json:"deposit_cents" doc:"deposit,fields.yard_quote.deposit"`
	Link     string           `json:"link" doc:"link,fields.yard_quote.link"`
}

func init() {
	kindTitles["it"][KindYardQuote] = "Preventivo lavori" //nolint:misspell // Italian title
	kindTitles["en"][KindYardQuote] = "Yard quote"
}

// yardQuoteDemo is the demo data of kind yard_quote: the antifouling job of m10-plan §4.4.
func yardQuoteDemo() YardQuoteView {
	lines := []YardQuoteLine{
		{Seq: 1, Description: "Alaggio", Unit: "m", Qty: "12", UnitPrice: 1800, Net: 21600, VATRate: 2200},
		{Seq: 2, Description: "Lavaggio carena", Unit: "m2", Qty: "56,1", UnitPrice: 400, Net: 22440, VATRate: 2200},
		{Seq: 3, Description: "Ora operaio", Unit: "hour", Qty: "8,4", UnitPrice: 3800, Net: 31920, VATRate: 2200},
		{Seq: 4, Description: "Ora verniciatore", Unit: "hour", Qty: "5,6", UnitPrice: 4500, Net: 25200, VATRate: 2200},
		{Seq: 5, Description: "Antivegetativa 2,5 l", Unit: "piece", Qty: "5", UnitPrice: 9500, Net: 47500, VATRate: 2200},
		{Seq: 6, Description: "Varo", Unit: "m", Qty: "12", UnitPrice: 1800, Net: 21600, VATRate: 2200},
	}
	return YardQuoteView{
		Marina: demoMarina, Customer: demoCustomer, Vessel: demoVessel,
		Quote: YardQuoteInfo{
			Number: "PC2026-000012", Kind: "job", IssuedOn: calendar.MustParse("2026-11-07"),
			ValidUntil: calendar.MustParse("2026-12-07"), BillingMode: "actual",
			PlannedStartOn: calendar.MustParse("2027-02-01"), PlannedDeliveryOn: calendar.MustParse("2027-02-12"),
			Conditions: "Acconto del 30% all'accettazione.\nSaldo alla consegna.",
		},
		Lines: lines,
		VAT:   []views.VATRow{{RateBP: 2200, Net: 170260, VAT: 37457, Gross: 207717}},
		Hours: []YardQuoteHours{{Qualification: "Operaio", Hours: "8,4"}, {Qualification: "Verniciatore", Hours: "5,6"}},
		Net:   170260, VATTotal: 37457, Total: 207717, Deposit: 62315,
		Link: "https://porto-demo.example/b/demo/m/quote/DEMO",
	}
}
