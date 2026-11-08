package remote

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/bridge"
	httpdesktop "github.com/coste360/porto/internal/http/desktop"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
)

// PinChecker is implemented by the remote runtime: it checks the app PIN (rate limited, never logged).
type PinChecker interface {
	CheckAppPin(pin string) error
}

// LocalAPI serves the remote subset of /api/desktop/v1 and the bridge API on the proxy, with the handlers of the
// in-process server over the remote runtime (which checks the app PIN header).
type LocalAPI struct {
	Runtime desktopport.Runtime
	// Proxy is set after the proxy is built (receipts and the dev offline switch go through it).
	Proxy    *Proxy
	MarinaID string
	Dev      bool

	desk   *httpdesktop.Handler
	bridge *bridge.Handler
}

// NewLocalAPI returns the local API of the proxy.
func NewLocalAPI(rt desktopport.Runtime, marinaID string, dev bool) *LocalAPI {
	l := &LocalAPI{Runtime: rt, MarinaID: marinaID, Dev: dev}
	l.desk = &httpdesktop.Handler{Runtime: rt}
	l.bridge = &bridge.Handler{Backend: remoteBackend{l}}
	return l
}

// ServeHTTP implements http.Handler.
func (l *LocalAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route := r.Method + " " + r.URL.Path
	switch route {
	case "POST /api/desktop/v1/activate":
		l.desk.ActivateDesktop(w, r)
	case "GET /api/desktop/v1/status":
		l.desk.GetDesktopStatus(w, r)
	case "GET /api/desktop/v1/peripherals":
		l.desk.GetDesktopPeripherals(w, r)
	case "PUT /api/desktop/v1/peripherals":
		l.desk.UpdateDesktopPeripherals(w, r)
	case "PUT /api/desktop/v1/kiosk":
		l.desk.SetDesktopKiosk(w, r)
	case "POST /api/desktop/v1/kiosk/exit":
		l.desk.ExitDesktopKiosk(w, r)
	case "DELETE /api/desktop/v1/remote":
		if err := l.Runtime.UnpairRemote(r.Context(), desktopport.AdminAuth{AppPin: r.Header.Get(AppPinHeader)}); err != nil {
			respond.Error(w, r, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	case "POST /api/desktop/v1/dev/remote-offline":
		if !l.Dev || l.Proxy == nil {
			problem.Write(w, r, problem.NotFound)
			return
		}
		var in api.DesktopRemoteOffline
		if !respond.DecodeOrFail(w, r, &in, false) {
			return
		}
		l.Proxy.SetOffline(in.Offline)
		respond.NoContent(w)
	case "GET /api/bridge/v1/status":
		l.bridge.GetBridgeStatus(w, r)
	case "POST /api/bridge/v1/receipts":
		l.bridge.PrintBridgeReceipt(w, r)
	case "POST /api/bridge/v1/test":
		l.bridge.TestBridgePeripheral(w, r)
	case "GET /api/bridge/v1/scanner/next":
		var params api.NextBridgeScanParams
		if v := r.URL.Query().Get("timeout"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				problem.Write(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "timeout", Code: "invalid"}))
				return
			}
			params.Timeout = &n
		}
		l.bridge.NextBridgeScan(w, r, params)
	default:
		problem.Write(w, r, problem.NotFound)
	}
}

// remoteBackend is the bridge Backend of the remote mode: receipts come from the online instance through the
// device token (its permissions decide), the test needs the app PIN.
type remoteBackend struct{ l *LocalAPI }

func (b remoteBackend) Peripherals() desktopport.Peripherals { return b.l.Runtime.Peripherals() }

func (b remoteBackend) Receipt(r *http.Request, paymentID string) (desktopport.ReceiptDoc, error) {
	if b.l.Proxy == nil || b.l.Proxy.offline.Load() {
		return desktopport.ReceiptDoc{}, desktopport.ErrRemoteUnreachable
	}
	var d api.ReceiptData
	path := "/api/admin/v1/marinas/" + url.PathEscape(b.l.MarinaID) + "/payments/" + url.PathEscape(paymentID) + "/receipt?format=json"
	code, err := b.l.Proxy.Upstream().JSON(r.Context(), http.MethodGet, path, nil, nil, &d)
	switch {
	case err != nil:
		return desktopport.ReceiptDoc{}, err
	case code == http.StatusOK:
		return bridge.ReceiptDocOf(d), nil
	case code == http.StatusNotFound || code == http.StatusForbidden || code == http.StatusUnauthorized:
		return desktopport.ReceiptDoc{}, desktopport.ErrNotFound
	default:
		return desktopport.ReceiptDoc{}, desktopport.ErrRemoteUnreachable
	}
}

func (b remoteBackend) AuthorizeTest(r *http.Request) error {
	pc, ok := b.l.Runtime.(PinChecker)
	if !ok {
		return desktopport.ErrAppPinInvalid
	}
	return pc.CheckAppPin(r.Header.Get(AppPinHeader))
}
