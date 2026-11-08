package desktop

import (
	"errors"
	"net/url"
	"time"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/peripheral"
)

// desktop.json parts added by M6b (m6b-plan §7.1, §7.6, §8.2).

// KioskFile is desktop.json kiosk.
type KioskFile struct {
	Enabled bool `json:"enabled"`
}

// RemoteFile is desktop.json remote: the online instance of the remote mode (no secret: the device token is in
// the SecretStore).
type RemoteFile struct {
	URL        string    `json:"url"`
	DeviceID   string    `json:"device_id"`
	MarinaID   string    `json:"marina_id"`
	MarinaName string    `json:"marina_name"`
	LeafFP     string    `json:"leaf_fp,omitempty"`
	CAFP       string    `json:"ca_fp,omitempty"`
	PairedAt   time.Time `json:"paired_at"`
}

func (r *RemoteFile) invalid() []string {
	var bad []string
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		bad = append(bad, "remote.url")
	}
	if r.DeviceID == "" || len(r.DeviceID) > 64 {
		bad = append(bad, "remote.device_id")
	}
	if r.MarinaID == "" || len(r.MarinaID) > 64 {
		bad = append(bad, "remote.marina_id")
	}
	if len(r.MarinaName) > 200 {
		bad = append(bad, "remote.marina_name")
	}
	if !hexFP(r.LeafFP) {
		bad = append(bad, "remote.leaf_fp")
	}
	if !hexFP(r.CAFP) {
		bad = append(bad, "remote.ca_fp")
	}
	return bad
}

func hexFP(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Port converts to the desktopport view (nil stays nil).
func (r *RemoteFile) Port() *desktopport.RemoteSettings {
	if r == nil {
		return nil
	}
	return &desktopport.RemoteSettings{
		URL: r.URL, DeviceID: r.DeviceID, MarinaID: r.MarinaID, MarinaName: r.MarinaName, LeafFP: r.LeafFP,
		CAFP: r.CAFP, PairedAt: r.PairedAt,
	}
}

// PeripheralsFile is desktop.json peripherals.
type PeripheralsFile struct {
	Receipt struct {
		Driver string `json:"driver,omitempty"`
		Target string `json:"target,omitempty"`
		Width  int    `json:"width,omitempty"`
	} `json:"receipt"`
	Label struct {
		Driver string `json:"driver,omitempty"`
		Target string `json:"target,omitempty"`
	} `json:"label"`
	Reader struct {
		Driver string `json:"driver,omitempty"`
	} `json:"reader"`
	Fiscal struct {
		Driver   string `json:"driver,omitempty"`
		FakeMode string `json:"fake_mode,omitempty"`
	} `json:"fiscal"`
}

// Port converts to the desktopport settings, with the defaults filled in.
func (p PeripheralsFile) Port() desktopport.PeripheralSettings {
	return peripheral.Normalize(desktopport.PeripheralSettings{
		Receipt: desktopport.ReceiptPrinterSettings{Driver: p.Receipt.Driver, Target: p.Receipt.Target, Width: p.Receipt.Width},
		Label:   desktopport.LabelPrinterSettings{Driver: p.Label.Driver, Target: p.Label.Target},
		Reader:  desktopport.QRReaderSettings{Driver: p.Reader.Driver},
		Fiscal:  desktopport.LocalFiscalSettings{Driver: p.Fiscal.Driver, FakeMode: p.Fiscal.FakeMode},
	})
}

// peripheralsFileOf converts normalized desktopport settings to the file form.
func peripheralsFileOf(s desktopport.PeripheralSettings) PeripheralsFile {
	s = peripheral.Normalize(s)
	var p PeripheralsFile
	p.Receipt.Driver, p.Receipt.Target, p.Receipt.Width = s.Receipt.Driver, s.Receipt.Target, s.Receipt.Width
	p.Label.Driver, p.Label.Target = s.Label.Driver, s.Label.Target
	p.Reader.Driver = s.Reader.Driver
	p.Fiscal.Driver, p.Fiscal.FakeMode = s.Fiscal.Driver, s.Fiscal.FakeMode
	return p
}

// invalid checks the driver names and the shape of the targets (the transport rules of the running platform are
// applied again when the settings are changed and when the drivers are built).
func (p PeripheralsFile) invalid() []string {
	err := peripheral.Validate(p.Port(), peripheral.TargetOptions{Dev: true})
	var se *desktopport.SettingsError
	if errors.As(err, &se) {
		out := make([]string, 0, len(se.Fields))
		for _, f := range se.Fields {
			out = append(out, "peripherals."+f)
		}
		return out
	}
	return nil
}
