package store

import (
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Row types of the M5 charges tables (internal/migrations/*/00010_charges.sql). Conventions as in
// contract_models.go. Owned by the CHARGES-EXPORT builder after the contract phase (fields may be added only if
// they exist in a migration).

// Charge origin types (charges.origin_type, m5-plan §3.3). utility, work_order and rental are later origins.
const (
	ChargeOriginInstallment       = "installment"
	ChargeOriginHolderInstallment = "holder_installment"
	ChargeOriginTransit           = "transit"
	ChargeOriginContractTrueUp    = "contract_trueup"
	ChargeOriginCounterSale       = "counter_sale"
	ChargeOriginTermination       = "termination"
	ChargeOriginDepositRetention  = "deposit_retention"
	ChargeOriginManual            = "manual"
	ChargeOriginAdjustment        = "adjustment"
	ChargeOriginOpeningBalance    = "opening_balance"
	ChargeOriginUtility           = "utility"
	ChargeOriginWorkOrder         = "work_order"
	ChargeOriginRental            = "rental"
)

// Charge payment statuses (m5-plan §3.2).
const (
	ChargeOpen          = "open"
	ChargePartiallyPaid = "partially_paid"
	ChargePaid          = "paid"
	ChargeCancelled     = "cancelled"
)

// Charge invoicing statuses (m5-plan §3.2).
const (
	ChargeToInvoice = "to_invoice"
	ChargeExported  = "exported"
	ChargeInvoiced  = "invoiced"
)

// Trigger messages of the M5 immutability triggers (both dialects).
const (
	ChargeImmutableMessage         = "charge_immutable"
	InsertOnlyMessage              = "insert_only"
	CashSessionClosedMessage       = "cash_session_closed"
	FiscalDocumentImmutableMessage = "fiscal_document_immutable"
)

// BillingSettings is the row of billing_settings of a marina (lazily created with the defaults).
type BillingSettings struct {
	bun.BaseModel `bun:"table:billing_settings"`

	MarinaID           string    `bun:"marina_id,pk"`
	DefaultDueDays     int       `bun:"default_due_days"`
	AutoPaymentRequest bool      `bun:"auto_payment_request"`
	ReminderPolicy     JSON      `bun:"reminder_policy"`
	ReminderHour       int       `bun:"reminder_hour"`
	ArrearsPolicy      JSON      `bun:"arrears_policy"`
	ExportReminderDays int       `bun:"export_reminder_days"`
	FiscalDriver       string    `bun:"fiscal_driver"`
	ReceiptFooter      string    `bun:"receipt_footer"`
	SEPALeadDays       int       `bun:"sepa_lead_days"`
	Version            int       `bun:"version"`
	CreatedAt          time.Time `bun:"created_at"`
	UpdatedAt          time.Time `bun:"updated_at"`
}

// ChargeOriginItem is a row of charge_origin_items: the catalogue item used for charges without a price snapshot.
type ChargeOriginItem struct {
	bun.BaseModel `bun:"table:charge_origin_items"`

	ID        string    `bun:"id,pk"`
	MarinaID  string    `bun:"marina_id"`
	OriginKey string    `bun:"origin_key"` // management_fee, common_advance, common_balance, transfer_fee, ...
	ItemID    string    `bun:"item_id"`
	CreatedAt time.Time `bun:"created_at"`
	UpdatedAt time.Time `bun:"updated_at"`
}

// BillingSequence is a row of billing_sequences (charge, payment, refund, export and request numbers per year).
type BillingSequence struct {
	bun.BaseModel `bun:"table:billing_sequences"`

	MarinaID   string `bun:"marina_id,pk"`
	Series     string `bun:"series,pk"` // charge, payment, refund, export, request
	Period     string `bun:"period,pk"` // YYYY
	NextNumber int64  `bun:"next_number"`
}

// Charge is a row of charges (spec 16.1, m5-plan §3.1). Amounts are signed (adjustments may be negative).
type Charge struct {
	bun.BaseModel `bun:"table:charges"`

	ID                    string        `bun:"id,pk"`
	MarinaID              string        `bun:"marina_id"`
	Number                string        `bun:"number"`
	CustomerID            string        `bun:"customer_id,nullzero"`
	OriginType            string        `bun:"origin_type"`
	OriginID              string        `bun:"origin_id"`
	ContractID            string        `bun:"contract_id,nullzero"`
	VesselID              string        `bun:"vessel_id,nullzero"`
	RightID               string        `bun:"right_id,nullzero"`
	IssuedOn              calendar.Date `bun:"issued_on"`
	DueOn                 calendar.Date `bun:"due_on"`
	ServiceFrom           calendar.Date `bun:"service_from,nullzero"`
	ServiceTo             calendar.Date `bun:"service_to,nullzero"`
	TotalCents            int64         `bun:"total_cents"`
	NetCents              int64         `bun:"net_cents"`
	VATCents              int64         `bun:"vat_cents"`
	PaidAmountCents       int64         `bun:"paid_amount_cents"`
	CreditedCents         int64         `bun:"credited_cents"`
	PaymentStatus         string        `bun:"payment_status"`
	InvoicingStatus       string        `bun:"invoicing_status"`
	FiscalMode            string        `bun:"fiscal_mode"` // invoice, receipt
	CorrectsChargeID      string        `bun:"corrects_charge_id,nullzero"`
	CorrectionKind        string        `bun:"correction_kind,nullzero"` // delta, replace
	Description           string        `bun:"description"`
	FiscalSnapshotEnc     string        `bun:"fiscal_snapshot_enc,nullzero"` // KeyRing, AAD charge-fiscal:<id>
	ExternalInvoiceNumber string        `bun:"external_invoice_number,nullzero"`
	ExternalInvoiceDate   calendar.Date `bun:"external_invoice_date,nullzero"`
	CancelReason          string        `bun:"cancel_reason,nullzero"`
	FirstExportID         string        `bun:"first_export_id,nullzero"`
	ExportedAt            time.Time     `bun:"exported_at,nullzero"`
	InvoicedAt            time.Time     `bun:"invoiced_at,nullzero"`
	CancelledAt           time.Time     `bun:"cancelled_at,nullzero"`
	CreatedByUserID       string        `bun:"created_by_user_id,nullzero"`
	CreatedByActor        string        `bun:"created_by_actor"`
	Version               int           `bun:"version"`
	CreatedAt             time.Time     `bun:"created_at"`
	UpdatedAt             time.Time     `bun:"updated_at"`
}

// ResidualCents is what is still owed on a positive charge (0 for negative or cancelled charges).
func (c *Charge) ResidualCents() int64 {
	if c.TotalCents <= 0 || c.PaymentStatus == ChargeCancelled {
		return 0
	}
	return c.TotalCents - c.PaidAmountCents - c.CreditedCents
}

// ChargeLine is a row of charge_lines (net + vat = gross, signed like the charge).
type ChargeLine struct {
	bun.BaseModel `bun:"table:charge_lines"`

	ID               string        `bun:"id,pk"`
	MarinaID         string        `bun:"marina_id"`
	ChargeID         string        `bun:"charge_id"`
	Seq              int           `bun:"seq"`
	ItemID           string        `bun:"item_id,nullzero"`
	ItemCode         string        `bun:"item_code"`
	Description      string        `bun:"description"`
	QtyMilli         int64         `bun:"qty_milli"`
	UnitCents        int64         `bun:"unit_cents"`
	DiscountCents    int64         `bun:"discount_cents"`
	NetCents         int64         `bun:"net_cents"`
	VATCents         int64         `bun:"vat_cents"`
	GrossCents       int64         `bun:"gross_cents"`
	VATRateBP        int64         `bun:"vat_rate_bp"`
	VATNature        string        `bun:"vat_nature,nullzero"` // a FatturaPA nature (N1..N7 with subcodes) when vat_rate_bp = 0
	PriceIncludesVAT bool          `bun:"price_includes_vat"`
	RevenueClassCode string        `bun:"revenue_class_code"`
	ExternalCode     string        `bun:"external_code"`
	ServiceFrom      calendar.Date `bun:"service_from,nullzero"`
	ServiceTo        calendar.Date `bun:"service_to,nullzero"`
	PlanKey          string        `bun:"plan_key"`
	CreatedAt        time.Time     `bun:"created_at"`
	UpdatedAt        time.Time     `bun:"updated_at"`
}

// ChargeOffset is a row of charge_offsets (insert-only): part of a negative charge applied to the charge it
// corrects.
type ChargeOffset struct {
	bun.BaseModel `bun:"table:charge_offsets"`

	ID               string    `bun:"id,pk"`
	MarinaID         string    `bun:"marina_id"`
	NegativeChargeID string    `bun:"negative_charge_id"`
	TargetChargeID   string    `bun:"target_charge_id"`
	AmountCents      int64     `bun:"amount_cents"`
	CreatedAt        time.Time `bun:"created_at"`
}

// ChargeExport is a row of charge_exports (spec 16.5).
type ChargeExport struct {
	bun.BaseModel `bun:"table:charge_exports"`

	ID              string        `bun:"id,pk"`
	MarinaID        string        `bun:"marina_id"`
	Number          string        `bun:"number"`
	Kind            string        `bun:"kind"` // export, reexport
	ReexportOf      string        `bun:"reexport_of,nullzero"`
	PeriodFrom      calendar.Date `bun:"period_from,nullzero"`
	PeriodTo        calendar.Date `bun:"period_to,nullzero"`
	Filters         JSON          `bun:"filters"`
	Delivery        string        `bun:"delivery"` // file, exporter
	ExporterCode    string        `bun:"exporter_code,nullzero"`
	DeliveryStatus  string        `bun:"delivery_status"` // none, pending, sent, failed
	DeliveryError   string        `bun:"delivery_error,nullzero"`
	DeliveryRef     string        `bun:"delivery_ref,nullzero"`
	ChargeCount     int           `bun:"charge_count"`
	LineCount       int           `bun:"line_count"`
	Totals          JSON          `bun:"totals"`
	CSVFileID       string        `bun:"csv_file_id,nullzero"`
	XLSXFileID      string        `bun:"xlsx_file_id,nullzero"`
	PDFFileID       string        `bun:"pdf_file_id,nullzero"`
	CSVSHA256       string        `bun:"csv_sha256,nullzero"`
	XLSXSHA256      string        `bun:"xlsx_sha256,nullzero"`
	PDFSHA256       string        `bun:"pdf_sha256,nullzero"`
	CreatedByUserID string        `bun:"created_by_user_id,nullzero"`
	Version         int           `bun:"version"`
	CreatedAt       time.Time     `bun:"created_at"`
	UpdatedAt       time.Time     `bun:"updated_at"`
}

// ChargeExportItem is a row of charge_export_items (insert-only). IsFirst marks the first export of the charge
// (INV-53: at most one per charge).
type ChargeExportItem struct {
	bun.BaseModel `bun:"table:charge_export_items"`

	ID       string `bun:"id,pk"`
	MarinaID string `bun:"marina_id"`
	ExportID string `bun:"export_id"`
	ChargeID string `bun:"charge_id"`
	IsFirst  bool   `bun:"is_first"`
	// RowSnapshotEnc freezes, encrypted, what the export rows show beyond the immutable charge (payment status,
	// paid amount, vessel, references) so that the files are rebuilt byte for byte (m5-plan §16 D27).
	RowSnapshotEnc string    `bun:"row_snapshot_enc,nullzero"`
	CreatedAt      time.Time `bun:"created_at"`
}

// InvoiceReconciliation is a row of invoice_reconciliations (report with charge numbers and outcomes only).
type InvoiceReconciliation struct {
	bun.BaseModel `bun:"table:invoice_reconciliations"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Source          string    `bun:"source"` // csv, manual, exporter
	ExportID        string    `bun:"export_id,nullzero"`
	FileSHA256      string    `bun:"file_sha256,nullzero"`
	DryRun          bool      `bun:"dry_run"`
	RowCount        int       `bun:"row_count"`
	MatchedCount    int       `bun:"matched_count"`
	Report          JSON      `bun:"report"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}

// ChargeImport is a row of charge_imports (opening balances, spec 11.6).
type ChargeImport struct {
	bun.BaseModel `bun:"table:charge_imports"`

	ID              string    `bun:"id,pk"`
	MarinaID        string    `bun:"marina_id"`
	Status          string    `bun:"status"` // dry_run, applied, undone, failed
	FileSHA256      string    `bun:"file_sha256"`
	Format          string    `bun:"format"` // csv, xlsx
	Mapping         JSON      `bun:"mapping"`
	Report          JSON      `bun:"report"`
	ChargeCount     int       `bun:"charge_count"`
	CreatedByUserID string    `bun:"created_by_user_id,nullzero"`
	UndoneAt        time.Time `bun:"undone_at,nullzero"`
	Version         int       `bun:"version"`
	CreatedAt       time.Time `bun:"created_at"`
	UpdatedAt       time.Time `bun:"updated_at"`
}
