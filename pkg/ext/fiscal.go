package ext

import (
	"context"
	"time"
)

// FiscalDriver issues commercial documents (documento commerciale) on a fiscal device or service (spec 16.3).
// Porto ships the drivers "none" (tracking only, the operator uses their own registered device) and "export"
// (daily corrispettivi file); device drivers are extensions. A driver whose device is temporarily unreachable
// returns an error implementing Deferred() bool = true: the document stays pending and is retried.
type FiscalDriver interface {
	Code() string
	Issue(ctx context.Context, doc FiscalDocumentRequest) (*FiscalDocumentResult, error)
	Void(ctx context.Context, ref string) error
	Status(ctx context.Context) (DeviceStatus, error)
}

// FiscalLine is one line of a commercial document: gross amount per VAT rate or nature.
type FiscalLine struct {
	Description string
	QtyMilli    int64
	GrossCents  int64
	VATRateBP   int64
	VATNature   string // a FatturaPA nature (N1..N7 with subcodes) when the rate is 0
}

// FiscalDocumentRequest is a commercial document to issue.
type FiscalDocumentRequest struct {
	MarinaID     string
	DocumentID   string
	Number       string // Porto's internal daily number YYYYMMDD-NNNN
	BusinessDate string // YYYY-MM-DD in the marina time zone
	Lines        []FiscalLine
	TotalCents   int64
	Method       string // payment method
	Currency     string
}

// FiscalDocumentResult is the driver's answer.
type FiscalDocumentResult struct {
	ExternalRef string
	IssuedAt    time.Time
}

// DeviceStatus is the state of a fiscal device.
type DeviceStatus struct {
	Online  bool
	Message string // a code, never personal data
}
