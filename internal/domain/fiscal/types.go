package fiscal

import (
	"context"

	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/pkg/ext"
)

// Core driver codes (m5-plan §5.2, §16 D10).
const (
	DriverNone   = "none"
	DriverExport = "export"
	// DriverBridge (M6b, m6b-plan §8.4): documents are issued by the desktop set in billing settings
	// (fiscal_bridge_device_id), in remote mode, through the device claim/outcome API; the fiscal.issue job leaves
	// them pending.
	DriverBridge = "bridge"
)

// inlineDriver is a core driver with no I/O: the document is issued with the internal number (tracking only; the
// operator uses their own registered device). "export" also offers the daily corrispettivi file.
type inlineDriver struct{ code string }

func (d inlineDriver) Code() string { return d.code }

func (inlineDriver) Issue(_ context.Context, _ ext.FiscalDocumentRequest) (*ext.FiscalDocumentResult, error) {
	return &ext.FiscalDocumentResult{}, nil
}

func (inlineDriver) Void(_ context.Context, _ string) error { return nil }

func (inlineDriver) Status(_ context.Context) (ext.DeviceStatus, error) {
	return ext.DeviceStatus{Online: true}, nil
}

// NoneDriver returns the core driver "none".
func NoneDriver() ext.FiscalDriver { return inlineDriver{code: DriverNone} }

// ExportDriver returns the core driver "export".
func ExportDriver() ext.FiscalDriver { return inlineDriver{code: DriverExport} }

// Deferrer is implemented by driver errors that keep a document pending (device unreachable).
type Deferrer interface{ Deferred() bool }

// Store types re-exported for the HTTP layer (which never imports internal/store).
type (
	// Document is a fiscal document.
	Document = store.FiscalDocument
	// DocumentPage is a page of fiscal documents.
	DocumentPage = store.Page[store.FiscalDocument]
)
