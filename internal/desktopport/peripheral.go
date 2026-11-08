package desktopport

import (
	"context"
	"time"

	"github.com/coste360/porto/pkg/ext"
)

// Local peripherals of the desktop app (m6b-plan §8.1). Drivers live in internal/peripheral (pure Go, no new
// dependency); the bridge API (/api/bridge/v1, loopback only) and the remote-mode fiscal agent use them through
// these interfaces.

// Peripheral driver codes (desktop.json peripherals.*.driver).
const (
	DriverNone     = "none"
	DriverFake     = "fake"
	DriverESCPOS   = "escpos"
	DriverZPL      = "zpl"
	DriverKeyboard = "keyboard"
)

// Peripheral kinds (bridge test, fake driver files).
const (
	PeripheralReceipt = "receipt"
	PeripheralLabel   = "label"
	PeripheralReader  = "reader"
	PeripheralFiscal  = "fiscal"
)

// Receipt paper widths in millimetres.
const (
	ReceiptWidth80 = 80
	ReceiptWidth58 = 58
)

// PeripheralStatus is what the bridge reports for a device. Message is a status code (e.g. "ready",
// "unreachable", "not_configured"), never personal data.
type PeripheralStatus struct {
	Driver  string
	Ready   bool
	Message string
}

// ReceiptPrinter prints courtesy receipts.
type ReceiptPrinter interface {
	Code() string
	Status(ctx context.Context) PeripheralStatus
	Print(ctx context.Context, r ReceiptDoc) error
}

// LabelPrinter prints labels (a test label in M6b).
type LabelPrinter interface {
	Code() string
	Status(ctx context.Context) PeripheralStatus
	Print(ctx context.Context, l LabelDoc) error
}

// QRReader yields scans of a reader that does not type into the page. Next blocks until a scan or ctx is done
// (then it returns ctx.Err()).
type QRReader interface {
	Code() string
	Next(ctx context.Context) (string, error)
}

// Peripherals are the configured drivers; a nil field means the driver is "none" (or "keyboard" for the reader,
// which has no Go driver). Fiscal is the local fiscal device: the existing ext.FiscalDriver, whose Deferred()
// errors keep a document pending.
type Peripherals struct {
	Receipt ReceiptPrinter
	Label   LabelPrinter
	Reader  QRReader
	Fiscal  ext.FiscalDriver
}

// Transport sends raw bytes to a device: tcp://host:9100, cups://queue (macOS, Linux), file:// (development).
type Transport interface {
	Write(ctx context.Context, b []byte) error
}

// ReceiptDoc is a courtesy receipt ready for a printer driver, built from the ReceiptData of getPaymentReceipt
// (format=json), so no driver parses HTML. Amounts are cents, quantities thousandths, rates basis points.
type ReceiptDoc struct {
	MarinaName   string
	LegalName    string
	Address      string
	VATNumber    string
	Number       string
	Date         string // YYYY-MM-DD
	Method       string
	AmountCents  int64
	Lines        []ReceiptLine
	VAT          []ReceiptVAT
	FiscalNumber string
	Footer       string
	// NotFiscal is always true: the courtesy receipt is "Non valido ai fini fiscali".
	NotFiscal bool
	// Width is ReceiptWidth80 or ReceiptWidth58 (0 = the configured width).
	Width int
}

// ReceiptLine is a line of a ReceiptDoc.
type ReceiptLine struct {
	Description string
	QtyMilli    int64
	GrossCents  int64
	VATRateBP   int64
}

// ReceiptVAT is a VAT summary row of a ReceiptDoc.
type ReceiptVAT struct {
	RateBP     int64
	Nature     string
	NetCents   int64
	VATCents   int64
	GrossCents int64
}

// LabelDoc is a text label (M6b: the test label only).
type LabelDoc struct {
	Lines []string
	// Copies defaults to 1.
	Copies int
}

// PeripheralSettings is desktop.json peripherals (m6b-plan §8.2).
type PeripheralSettings struct {
	Receipt ReceiptPrinterSettings
	Label   LabelPrinterSettings
	Reader  QRReaderSettings
	Fiscal  LocalFiscalSettings
}

// ReceiptPrinterSettings: driver none|fake|escpos, target (Transport URL), width 80|58.
type ReceiptPrinterSettings struct {
	Driver string
	Target string
	Width  int
}

// LabelPrinterSettings: driver none|fake|zpl, target (Transport URL).
type LabelPrinterSettings struct {
	Driver string
	Target string
}

// QRReaderSettings: driver keyboard|fake.
type QRReaderSettings struct {
	Driver string
}

// LocalFiscalSettings: driver none|fake; FakeMode ok|offline|fail (development).
type LocalFiscalSettings struct {
	Driver   string
	FakeMode string
}

// FiscalClaimLease is the lease of a bridge claim (m6b-plan §8.4).
const FiscalClaimLease = 60 * time.Second
