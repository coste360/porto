package charge

import (
	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// CreateInput is a charge to create in a transaction (CreateTx). OriginID empty = the charge itself (manual and
// counter sales). Lines carry their final amounts (computed by chargeplan or priced from the catalogue).
type CreateInput struct {
	OriginType       string
	OriginID         string
	CustomerID       string // billing customer; empty only for walk-in receipts
	ContractID       string
	VesselID         string
	RightID          string
	IssuedOn         calendar.Date
	DueOn            calendar.Date
	FiscalMode       string // invoice (default), receipt
	CorrectsChargeID string
	CorrectionKind   string // delta, replace
	Description      string
	Lines            []LineInput
	// Invoiced creates the charge already invoiced (opening balances, m5-plan §3.8) with the external reference.
	Invoiced      bool
	InvoiceNumber string
	InvoiceDate   calendar.Date
	// NoCredit leaves the remainder of a negative charge unapplied (the caller grants the credit itself).
	NoCredit bool
}

// LineInput is one charge line (net + vat = gross, signed like the charge).
type LineInput struct {
	ItemID           string
	ItemCode         string
	Description      string
	Qty              qty.Milli
	Unit             money.Cents
	Discount         money.Cents
	Net, VAT, Gross  money.Cents
	VATRateBP        int64
	VATNature        string
	PriceIncludesVAT bool
	RevenueClassCode string
	ExternalCode     string
	ServiceFrom      calendar.Date
	ServiceTo        calendar.Date
	PlanKey          string
}

// ExportFilter selects the charges of a "da fatturare" export (m5-plan §3.6).
type ExportFilter struct {
	PeriodFrom   calendar.Date
	PeriodTo     calendar.Date
	CustomerID   string
	OriginTypes  []string
	RevenueClass string
}

// qtyOf converts a stored quantity.
func qtyOf(v int64) qty.Milli { return qty.Milli(v) }

// Aliases of the store rows, so that the HTTP layer converts them without importing the store.
type (
	// Charge is a charge row.
	Charge = store.Charge
	// Line is a charge line row.
	Line = store.ChargeLine
	// Export is an export row.
	Export = store.ChargeExport
	// Reconciliation is an invoice reconciliation row.
	Reconciliation = store.InvoiceReconciliation
	// Import is an opening balance import row.
	Import = store.ChargeImport
	// Page is a page of a list.
	Page[T any] = store.Page[T]
)

// OriginItemKeys are the origin item keys in display order.
var OriginItemKeys = store.OriginItemKeys
