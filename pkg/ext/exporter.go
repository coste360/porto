package ext

import (
	"context"
	"time"
)

// ChargeExporter sends exported charges to an external invoicing software and receives invoice numbers and dates
// back (spec 16.5). Porto ships none: the target software is an open decision (spec 33 D4) and the core export is
// a generic CSV/Excel file. Fiscal data are decrypted in memory only for the call and must never be logged.
type ChargeExporter interface {
	Code() string
	Export(ctx context.Context, batch ChargeBatch) (*ExportReceipt, error)
	Poll(ctx context.Context, receipt ExportReceipt) ([]InvoiceRef, error)
}

// ChargeBatch is one export of charges.
type ChargeBatch struct {
	MarinaID string
	ExportID string
	Number   string
	Currency string
	Charges  []ExportedCharge
}

// ExportedCharge is a charge with its customer's fiscal data and lines.
type ExportedCharge struct {
	ChargeID           string
	Number             string
	IssuedOn           string // YYYY-MM-DD
	DueOn              string
	OriginType         string
	CorrectsNumber     string
	CommercialDocument bool
	Customer           FiscalParty
	TotalCents         int64
	NetCents           int64
	VATCents           int64
	Lines              []ExportedLine
}

// FiscalParty is the billing data of a customer (personal data).
type FiscalParty struct {
	Name, FiscalCode, VATNumber, SDICode, PEC   string
	Street, PostalCode, City, Province, Country string
	Company                                     bool
}

// ExportedLine is a charge line.
type ExportedLine struct {
	Seq           int
	ItemCode      string
	ExternalCode  string
	Description   string
	QtyMilli      int64
	UnitCents     int64
	DiscountCents int64
	NetCents      int64
	VATCents      int64
	GrossCents    int64
	VATRateBP     int64
	VATNature     string
	RevenueClass  string
	ServiceFrom   string
	ServiceTo     string
}

// ExportReceipt identifies a batch at the external software.
type ExportReceipt struct {
	Ref    string
	SentAt time.Time
}

// InvoiceRef is the invoice the external software issued for a charge.
type InvoiceRef struct {
	ChargeID string
	Number   string
	Date     string // YYYY-MM-DD
}
