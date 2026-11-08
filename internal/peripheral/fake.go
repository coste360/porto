package peripheral

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/pkg/ext"
)

// Fake drivers (development, tests, e2e): every output is appended as a JSON line to
// <dir>/<kind>.jsonl, so a test reads back what was "printed" or "issued".

// Fake fiscal modes.
const (
	FakeFiscalOK      = "ok"
	FakeFiscalOffline = "offline"
	FakeFiscalFail    = "fail"
)

var fakeMu sync.Mutex

func appendJSONLine(dir, kind string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fakeMu.Lock()
	defer fakeMu.Unlock()
	return FileTransport{Path: filepath.Join(dir, kind+".jsonl")}.Write(context.Background(), append(b, '\n'))
}

// FakeFile is the JSON-lines file of a fake peripheral kind.
func FakeFile(dir, kind string) string { return filepath.Join(dir, kind+".jsonl") }

// ReadFake returns the lines written by a fake peripheral (tests).
func ReadFake(dir, kind string) ([]json.RawMessage, error) {
	b, err := os.ReadFile(FakeFile(dir, kind))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []json.RawMessage
	for _, l := range splitLines(b) {
		out = append(out, json.RawMessage(l))
	}
	return out, nil
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// FakeReceipt prints receipts as JSON lines (the ReceiptDoc plus the ESC/POS text rendering size).
type FakeReceipt struct {
	Dir   string
	Width int
}

// Code implements desktopport.ReceiptPrinter.
func (FakeReceipt) Code() string { return desktopport.DriverFake }

// Status implements desktopport.ReceiptPrinter.
func (FakeReceipt) Status(context.Context) desktopport.PeripheralStatus {
	return desktopport.PeripheralStatus{Driver: desktopport.DriverFake, Ready: true, Message: "ready"}
}

// FakeReceiptLine is one line of receipt.jsonl.
type FakeReceiptLine struct {
	Number      string `json:"number"`
	AmountCents int64  `json:"amount_cents"`
	Width       int    `json:"width"`
	Lines       int    `json:"lines"`
	Bytes       int    `json:"bytes"`
}

// Print implements desktopport.ReceiptPrinter.
func (f FakeReceipt) Print(_ context.Context, r desktopport.ReceiptDoc) error {
	width := f.Width
	if r.Width != 0 {
		width = r.Width
	}
	if width == 0 {
		width = desktopport.ReceiptWidth80
	}
	return appendJSONLine(f.Dir, desktopport.PeripheralReceipt, FakeReceiptLine{
		Number: r.Number, AmountCents: r.AmountCents, Width: width, Lines: len(r.Lines), Bytes: len(RenderESCPOS(r, width)),
	})
}

// FakeLabel prints labels as JSON lines.
type FakeLabel struct{ Dir string }

// Code implements desktopport.LabelPrinter.
func (FakeLabel) Code() string { return desktopport.DriverFake }

// Status implements desktopport.LabelPrinter.
func (FakeLabel) Status(context.Context) desktopport.PeripheralStatus {
	return desktopport.PeripheralStatus{Driver: desktopport.DriverFake, Ready: true, Message: "ready"}
}

// Print implements desktopport.LabelPrinter.
func (f FakeLabel) Print(_ context.Context, l desktopport.LabelDoc) error {
	return appendJSONLine(f.Dir, desktopport.PeripheralLabel, l)
}

// FakeReader is a QR reader fed by Push.
type FakeReader struct {
	ch chan string
}

// NewFakeReader returns a reader with a small buffer.
func NewFakeReader() *FakeReader { return &FakeReader{ch: make(chan string, 16)} }

// Code implements desktopport.QRReader.
func (*FakeReader) Code() string { return desktopport.DriverFake }

// Push queues a scan; it is dropped when the buffer is full.
func (r *FakeReader) Push(text string) {
	select {
	case r.ch <- text:
	default:
	}
}

// Next implements desktopport.QRReader.
func (r *FakeReader) Next(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case t := <-r.ch:
		return t, nil
	}
}

// FakeFiscal is a local fiscal device: ok issues with a reference FAKE-<number>, offline defers (the document
// stays pending), fail refuses.
type FakeFiscal struct {
	Dir  string
	Mode string
}

type deferredError struct{}

func (deferredError) Error() string  { return "peripheral: fake fiscal device offline" }
func (deferredError) Deferred() bool { return true }

// ErrFakeFiscalFailed is the refusal of a fake fiscal device in mode fail.
var ErrFakeFiscalFailed = errors.New("peripheral: fake fiscal device refused the document")

// Code implements ext.FiscalDriver.
func (FakeFiscal) Code() string { return desktopport.DriverFake }

// FakeFiscalLine is one line of fiscal.jsonl.
type FakeFiscalLine struct {
	Op          string `json:"op"`
	DocumentID  string `json:"document_id,omitempty"`
	Number      string `json:"number,omitempty"`
	TotalCents  int64  `json:"total_cents,omitempty"`
	ExternalRef string `json:"external_ref,omitempty"`
}

// Issue implements ext.FiscalDriver.
func (f FakeFiscal) Issue(_ context.Context, doc ext.FiscalDocumentRequest) (*ext.FiscalDocumentResult, error) {
	switch f.Mode {
	case FakeFiscalOffline:
		return nil, deferredError{}
	case FakeFiscalFail:
		return nil, ErrFakeFiscalFailed
	}
	ref := "FAKE-" + doc.Number
	if err := appendJSONLine(f.Dir, desktopport.PeripheralFiscal, FakeFiscalLine{
		Op: "issue", DocumentID: doc.DocumentID, Number: doc.Number, TotalCents: doc.TotalCents, ExternalRef: ref,
	}); err != nil {
		return nil, err
	}
	return &ext.FiscalDocumentResult{ExternalRef: ref}, nil
}

// Void implements ext.FiscalDriver.
func (f FakeFiscal) Void(_ context.Context, ref string) error {
	if f.Mode == FakeFiscalOffline {
		return deferredError{}
	}
	return appendJSONLine(f.Dir, desktopport.PeripheralFiscal, FakeFiscalLine{Op: "void", ExternalRef: ref})
}

// Status implements ext.FiscalDriver.
func (f FakeFiscal) Status(context.Context) (ext.DeviceStatus, error) {
	if f.Mode == FakeFiscalOffline {
		return ext.DeviceStatus{Online: false, Message: "offline"}, nil
	}
	return ext.DeviceStatus{Online: true, Message: "ready"}, nil
}
