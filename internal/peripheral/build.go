package peripheral

import (
	"github.com/coste360/porto/internal/desktopport"
)

// BuildOptions are the environment of Build.
type BuildOptions struct {
	// FakeDir is where the fake drivers write (<data>/peripherals).
	FakeDir string
	// Reader is the fake QR reader to use (shared across reloads so pushed scans are not lost); nil = a new one.
	Reader *FakeReader
	Target TargetOptions
}

// Validate reports the invalid fields of peripheral settings (names only, never values) as a
// *desktopport.SettingsError, or nil.
func Validate(s desktopport.PeripheralSettings, o TargetOptions) error {
	var bad []string
	switch s.Receipt.Driver {
	case "", desktopport.DriverNone, desktopport.DriverFake:
	case desktopport.DriverESCPOS:
		if _, err := ParseTarget(s.Receipt.Target, o); err != nil {
			bad = append(bad, "receipt.target")
		}
	default:
		bad = append(bad, "receipt.driver")
	}
	if w := s.Receipt.Width; w != 0 && w != desktopport.ReceiptWidth80 && w != desktopport.ReceiptWidth58 {
		bad = append(bad, "receipt.width")
	}
	switch s.Label.Driver {
	case "", desktopport.DriverNone, desktopport.DriverFake:
	case desktopport.DriverZPL:
		if _, err := ParseTarget(s.Label.Target, o); err != nil {
			bad = append(bad, "label.target")
		}
	default:
		bad = append(bad, "label.driver")
	}
	switch s.Reader.Driver {
	case "", desktopport.DriverKeyboard, desktopport.DriverFake:
	default:
		bad = append(bad, "reader.driver")
	}
	switch s.Fiscal.Driver {
	case "", desktopport.DriverNone:
	case desktopport.DriverFake:
		switch s.Fiscal.FakeMode {
		case "", FakeFiscalOK, FakeFiscalOffline, FakeFiscalFail:
		default:
			bad = append(bad, "fiscal.fake_mode")
		}
	default:
		bad = append(bad, "fiscal.driver")
	}
	if len(bad) > 0 {
		return &desktopport.SettingsError{Fields: bad}
	}
	return nil
}

// Normalize fills the defaults of peripheral settings (none, keyboard, 80 mm, fake mode ok) and drops the targets
// of drivers that do not use one.
func Normalize(s desktopport.PeripheralSettings) desktopport.PeripheralSettings {
	if s.Receipt.Driver == "" {
		s.Receipt.Driver = desktopport.DriverNone
	}
	if s.Receipt.Driver != desktopport.DriverESCPOS {
		s.Receipt.Target = ""
	}
	if s.Receipt.Width == 0 {
		s.Receipt.Width = desktopport.ReceiptWidth80
	}
	if s.Label.Driver == "" {
		s.Label.Driver = desktopport.DriverNone
	}
	if s.Label.Driver != desktopport.DriverZPL {
		s.Label.Target = ""
	}
	if s.Reader.Driver == "" {
		s.Reader.Driver = desktopport.DriverKeyboard
	}
	if s.Fiscal.Driver == "" {
		s.Fiscal.Driver = desktopport.DriverNone
	}
	if s.Fiscal.Driver == desktopport.DriverFake && s.Fiscal.FakeMode == "" {
		s.Fiscal.FakeMode = FakeFiscalOK
	}
	if s.Fiscal.Driver != desktopport.DriverFake {
		s.Fiscal.FakeMode = ""
	}
	return s
}

// Build returns the drivers of valid settings (call Validate first; an invalid target yields no driver).
func Build(s desktopport.PeripheralSettings, o BuildOptions) desktopport.Peripherals {
	s = Normalize(s)
	var p desktopport.Peripherals
	switch s.Receipt.Driver {
	case desktopport.DriverFake:
		p.Receipt = FakeReceipt{Dir: o.FakeDir, Width: s.Receipt.Width}
	case desktopport.DriverESCPOS:
		if t, err := ParseTarget(s.Receipt.Target, o.Target); err == nil {
			p.Receipt = ESCPOSPrinter{Transport: t, Width: s.Receipt.Width}
		}
	}
	switch s.Label.Driver {
	case desktopport.DriverFake:
		p.Label = FakeLabel{Dir: o.FakeDir}
	case desktopport.DriverZPL:
		if t, err := ParseTarget(s.Label.Target, o.Target); err == nil {
			p.Label = ZPLPrinter{Transport: t}
		}
	}
	if s.Reader.Driver == desktopport.DriverFake {
		r := o.Reader
		if r == nil {
			r = NewFakeReader()
		}
		p.Reader = r
	}
	if s.Fiscal.Driver == desktopport.DriverFake {
		p.Fiscal = FakeFiscal{Dir: o.FakeDir, Mode: s.Fiscal.FakeMode}
	}
	return p
}

// TestReceipt is the receipt printed by the bridge test (no customer data).
func TestReceipt() desktopport.ReceiptDoc {
	return desktopport.ReceiptDoc{
		MarinaName: "Porto", Number: "TEST", Date: "", Method: "cash", AmountCents: 0, NotFiscal: true,
		Lines: []desktopport.ReceiptLine{{Description: "Stampa di prova", QtyMilli: 1000}},
	}
}

// TestLabel is the label printed by the bridge test.
func TestLabel() desktopport.LabelDoc {
	return desktopport.LabelDoc{Lines: []string{"Porto", "Etichetta di prova"}, Copies: 1}
}
