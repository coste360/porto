package desktop

import (
	"net/http"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
)

// M6b operations of /api/desktop/v1 (m6b-plan §7) served by the in-process server in local mode. Remote mode runs
// no Porto server in the app: the remote proxy (internal/desktop/remote) serves the same handlers over the remote
// runtime, which checks the app PIN of the Porto-App-Pin header (the local runtime ignores it), and answers the
// operations whose only security is appPin / remoteCookie itself, so they are always 404 here.

// AppPinHeader carries the app PIN of the remote mode (security scheme appPin).
const AppPinHeader = "Porto-App-Pin"

func remoteStatusView(s desktopport.RemoteStatus) *api.RemoteStatus {
	return &api.RemoteStatus{
		Url: s.URL, Marina: api.MarinaRef{Id: s.MarinaID, Name: s.MarinaName}, Reachable: s.Reachable,
		PairedAt: s.PairedAt, VersionMismatch: s.VersionMismatch,
	}
}

func (h *Handler) adminAuth(r *http.Request) desktopport.AdminAuth {
	return desktopport.AdminAuth{Principal: principal(r), AppPin: r.Header.Get(AppPinHeader)}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PeripheralSettingsView converts desktopport settings to the wire form (shared with the remote proxy).
func PeripheralSettingsView(s desktopport.PeripheralSettings) api.PeripheralSettings {
	out := api.PeripheralSettings{
		Receipt: api.ReceiptPrinterSettings{Driver: api.ReceiptPrinterSettingsDriver(orNone(s.Receipt.Driver)), Target: strPtr(s.Receipt.Target)},
		Label:   api.LabelPrinterSettings{Driver: api.LabelPrinterSettingsDriver(orNone(s.Label.Driver)), Target: strPtr(s.Label.Target)},
		Reader:  api.QrReaderSettings{Driver: api.QrReaderSettingsDriver(s.Reader.Driver)},
		Fiscal:  api.LocalFiscalSettings{Driver: api.LocalFiscalSettingsDriver(orNone(s.Fiscal.Driver))},
	}
	if out.Reader.Driver == "" {
		out.Reader.Driver = api.QrReaderSettingsDriver(desktopport.DriverKeyboard)
	}
	if s.Receipt.Width != 0 {
		wd := api.ReceiptPrinterSettingsWidth(s.Receipt.Width)
		out.Receipt.Width = &wd
	}
	if s.Fiscal.FakeMode != "" {
		m := api.LocalFiscalSettingsFakeMode(s.Fiscal.FakeMode)
		out.Fiscal.FakeMode = &m
	}
	return out
}

// PeripheralSettingsInput converts the wire form to desktopport settings (validated by the runtime).
func PeripheralSettingsInput(in api.PeripheralSettings) desktopport.PeripheralSettings {
	out := desktopport.PeripheralSettings{
		Receipt: desktopport.ReceiptPrinterSettings{Driver: string(in.Receipt.Driver), Target: deref(in.Receipt.Target)},
		Label:   desktopport.LabelPrinterSettings{Driver: string(in.Label.Driver), Target: deref(in.Label.Target)},
		Reader:  desktopport.QRReaderSettings{Driver: string(in.Reader.Driver)},
		Fiscal:  desktopport.LocalFiscalSettings{Driver: string(in.Fiscal.Driver)},
	}
	if in.Receipt.Width != nil {
		out.Receipt.Width = int(*in.Receipt.Width)
	}
	if in.Fiscal.FakeMode != nil {
		out.Fiscal.FakeMode = string(*in.Fiscal.FakeMode)
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return desktopport.DriverNone
	}
	return s
}

// PairDesktopRemote implements api.ServerInterface (first run, first-run token in the body).
func (h *Handler) PairDesktopRemote(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.DesktopRemotePairRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	err := h.Runtime.PairRemote(r.Context(), desktopport.RemotePairInput{
		FirstRunToken: in.FirstRunToken, Link: deref(in.Link), URL: deref(in.Url), Code: deref(in.Code),
		Fingerprint: deref(in.Fp), Pin: in.Pin, Name: deref(in.Name),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// UnpairDesktopRemote implements api.ServerInterface: remote mode only, answered by the remote proxy.
func (h *Handler) UnpairDesktopRemote(w http.ResponseWriter, r *http.Request) {
	problem.Write(w, r, problem.NotFound)
}

// SetDesktopRemoteOffline implements api.ServerInterface: remote mode only (dev), answered by the remote proxy.
func (h *Handler) SetDesktopRemoteOffline(w http.ResponseWriter, r *http.Request) {
	problem.Write(w, r, problem.NotFound)
}

// GetDesktopPeripherals implements api.ServerInterface.
func (h *Handler) GetDesktopPeripherals(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	s, err := h.Runtime.PeripheralSettings(r.Context(), h.adminAuth(r))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, PeripheralSettingsView(s))
}

// UpdateDesktopPeripherals implements api.ServerInterface.
func (h *Handler) UpdateDesktopPeripherals(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.PeripheralSettings
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	s, err := h.Runtime.UpdatePeripheralSettings(r.Context(), h.adminAuth(r), PeripheralSettingsInput(in))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, PeripheralSettingsView(s))
}

// SetDesktopKiosk implements api.ServerInterface.
func (h *Handler) SetDesktopKiosk(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.DesktopKioskUpdate
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	k, err := h.Runtime.SetKiosk(r.Context(), h.adminAuth(r), in.Enabled)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.DesktopKioskUpdate{Enabled: k.Enabled})
}

// ExitDesktopKiosk implements api.ServerInterface (local mode: owner credentials in the body).
func (h *Handler) ExitDesktopKiosk(w http.ResponseWriter, r *http.Request) {
	if !h.available(w, r) {
		return
	}
	var in api.DesktopKioskExit
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	cred := desktopport.KioskExitCredentials{Email: deref(in.Email), Password: deref(in.Password)}
	if err := h.Runtime.ExitKiosk(r.Context(), h.adminAuth(r), cred); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
