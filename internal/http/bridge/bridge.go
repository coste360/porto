// Package bridge implements /api/bridge/v1 (m6b-plan §8.3): the local peripherals of the desktop app (receipt and
// label printers, QR reader, local fiscal device) as thin handlers over a Backend. The router serves the prefix
// only on the desktop loopback listener from a loopback peer (middleware.LoopbackOnly, INV-85); with a nil
// Runtime (server edition) every operation answers 404. In remote mode the same handlers are served by the app's
// remote proxy (internal/desktop/remote) with its own Backend, never by the online instance.
package bridge

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/peripheral"
)

// Prefix is the path prefix of the bridge API.
const Prefix = "/api/bridge/v1/"

// Scan long-poll bounds (seconds).
const (
	scanTimeoutDefault = 25
	scanTimeoutMax     = 25
)

// Backend is what the handlers need from the app: the configured drivers, the receipt data of a payment as seen
// by the caller (its permissions decide: payments.collect or payments.collect_cash, 404 otherwise) and the
// authorisation of the peripheral test (owner in local mode, app PIN in remote mode).
type Backend interface {
	Peripherals() desktopport.Peripherals
	Receipt(r *http.Request, paymentID string) (desktopport.ReceiptDoc, error)
	AuthorizeTest(r *http.Request) error
}

// ReceiptSource is implemented by the local-mode runtime (internal/desktop): the receipt data of a payment of any
// marina the principal can collect for.
type ReceiptSource interface {
	BridgeReceipt(ctx context.Context, p *auth.Principal, paymentID string) (desktopport.ReceiptDoc, error)
}

// Handler serves the bridge API.
type Handler struct {
	// Runtime is nil in the server edition: every operation is then 404. Local mode.
	Runtime desktopport.Runtime
	// Backend replaces Runtime (remote proxy).
	Backend Backend
}

func (h *Handler) backend(w http.ResponseWriter, r *http.Request) (Backend, bool) {
	switch {
	case h == nil:
	case h.Backend != nil:
		return h.Backend, true
	case h.Runtime != nil:
		return localBackend{h.Runtime}, true
	}
	problem.Write(w, r, problem.NotFound)
	return nil, false
}

// localBackend is the Backend of the local mode: the session principal of the request.
type localBackend struct{ rt desktopport.Runtime }

func (b localBackend) Peripherals() desktopport.Peripherals { return b.rt.Peripherals() }

func (b localBackend) Receipt(r *http.Request, paymentID string) (desktopport.ReceiptDoc, error) {
	src, ok := b.rt.(ReceiptSource)
	if !ok {
		return desktopport.ReceiptDoc{}, desktopport.ErrNotFound
	}
	p, _ := auth.PrincipalFrom(r.Context())
	return src.BridgeReceipt(r.Context(), p, paymentID)
}

func (b localBackend) AuthorizeTest(r *http.Request) error {
	p, _ := auth.PrincipalFrom(r.Context())
	if !p.IsOwnerAnywhere() {
		return auth.ErrForbidden
	}
	return nil
}

// StatusView reports the status of the configured drivers (shared with the remote proxy).
func StatusView(r *http.Request, p desktopport.Peripherals) api.BridgeStatus {
	ctx := r.Context()
	none := func(driver string) api.PeripheralStatus {
		return api.PeripheralStatus{Driver: driver, Ready: false, Message: "not_configured"}
	}
	out := api.BridgeStatus{
		Receipt: none(desktopport.DriverNone), Label: none(desktopport.DriverNone),
		Reader: api.PeripheralStatus{Driver: desktopport.DriverKeyboard, Ready: true, Message: "ready"},
		Fiscal: none(desktopport.DriverNone),
	}
	if p.Receipt != nil {
		out.Receipt = statusOf(p.Receipt.Status(ctx))
	}
	if p.Label != nil {
		out.Label = statusOf(p.Label.Status(ctx))
	}
	if p.Reader != nil {
		out.Reader = api.PeripheralStatus{Driver: p.Reader.Code(), Ready: true, Message: "ready"}
	}
	if p.Fiscal != nil {
		st, err := p.Fiscal.Status(ctx)
		out.Fiscal = api.PeripheralStatus{Driver: p.Fiscal.Code(), Ready: err == nil && st.Online, Message: "ready"}
		if err != nil || !st.Online {
			out.Fiscal.Message = "unreachable"
		}
	}
	return out
}

// ReceiptDocOf converts the receipt data of getPaymentReceipt (format=json) for a printer driver.
func ReceiptDocOf(d api.ReceiptData) desktopport.ReceiptDoc {
	out := desktopport.ReceiptDoc{
		MarinaName: d.Marina.Name, LegalName: d.Marina.LegalName, Address: d.Marina.Address,
		VATNumber: d.Marina.VatNumber, Number: d.Number, Date: string(d.Date), Method: d.Method,
		AmountCents: int64(d.AmountCents), FiscalNumber: d.FiscalNumber, Footer: d.Footer, NotFiscal: true,
	}
	for _, l := range d.Lines {
		out.Lines = append(out.Lines, desktopport.ReceiptLine{
			Description: l.Description, QtyMilli: l.QtyMilli, GrossCents: int64(l.GrossCents), VATRateBP: int64(l.VatRateBp),
		})
	}
	for _, v := range d.Vat {
		out.VAT = append(out.VAT, desktopport.ReceiptVAT{
			RateBP: int64(v.VatRateBp), Nature: v.VatNature, NetCents: int64(v.NetCents), VATCents: int64(v.VatCents),
			GrossCents: int64(v.GrossCents),
		})
	}
	return out
}

func statusOf(s desktopport.PeripheralStatus) api.PeripheralStatus {
	return api.PeripheralStatus{Driver: s.Driver, Ready: s.Ready, Message: s.Message}
}

// GetBridgeStatus implements api.ServerInterface.
func (h *Handler) GetBridgeStatus(w http.ResponseWriter, r *http.Request) {
	b, ok := h.backend(w, r)
	if !ok {
		return
	}
	respond.JSON(w, http.StatusOK, StatusView(r, b.Peripherals()))
}

// PrintBridgeReceipt implements api.ServerInterface: the receipt data of the payment (getPaymentReceipt
// format=json: in process in local mode, through the proxy client in remote mode) rendered by the configured
// receipt driver.
func (h *Handler) PrintBridgeReceipt(w http.ResponseWriter, r *http.Request) {
	b, ok := h.backend(w, r)
	if !ok {
		return
	}
	var in api.BridgeReceiptRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if in.PaymentId == "" || len(in.PaymentId) > 36 {
		problem.Write(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "payment_id", Code: "invalid"}))
		return
	}
	printer := b.Peripherals().Receipt
	if printer == nil {
		respond.Error(w, r, desktopport.ErrPeripheralUnavailable)
		return
	}
	doc, err := b.Receipt(r, in.PaymentId)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	if in.Width != nil {
		doc.Width = int(*in.Width)
	}
	doc.NotFiscal = true
	if err := printer.Print(r.Context(), doc); err != nil {
		respond.Error(w, r, unavailable(err))
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// unavailable maps a driver error to ErrPeripheralUnavailable without its detail (it may carry an address).
func unavailable(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	return desktopport.ErrPeripheralUnavailable
}

// TestBridgePeripheral implements api.ServerInterface: a test receipt or label, or the fiscal device status.
func (h *Handler) TestBridgePeripheral(w http.ResponseWriter, r *http.Request) {
	b, ok := h.backend(w, r)
	if !ok {
		return
	}
	var in api.BridgeTestRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	if err := b.AuthorizeTest(r); err != nil {
		respond.Error(w, r, err)
		return
	}
	p := b.Peripherals()
	var err error
	switch string(in.Device) {
	case desktopport.PeripheralReceipt:
		if p.Receipt == nil {
			err = desktopport.ErrPeripheralUnavailable
		} else {
			err = p.Receipt.Print(r.Context(), peripheral.TestReceipt())
		}
	case desktopport.PeripheralLabel:
		if p.Label == nil {
			err = desktopport.ErrPeripheralUnavailable
		} else {
			err = p.Label.Print(r.Context(), peripheral.TestLabel())
		}
	case desktopport.PeripheralFiscal:
		if p.Fiscal == nil {
			err = desktopport.ErrPeripheralUnavailable
		} else if st, serr := p.Fiscal.Status(r.Context()); serr != nil || !st.Online {
			err = desktopport.ErrPeripheralUnavailable
		}
	default:
		problem.Write(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "device", Code: "invalid"}))
		return
	}
	if err != nil {
		respond.Error(w, r, unavailable(err))
		return
	}
	respond.NoContent(w)
}

// NextBridgeScan implements api.ServerInterface: long poll on the reader driver (the keyboard reader types into
// the page and never answers here).
func (h *Handler) NextBridgeScan(w http.ResponseWriter, r *http.Request, params api.NextBridgeScanParams) {
	b, ok := h.backend(w, r)
	if !ok {
		return
	}
	secs := scanTimeoutDefault
	if params.Timeout != nil {
		secs = *params.Timeout
	}
	if secs < 1 || secs > scanTimeoutMax {
		problem.Write(w, r, problem.ValidationFailed, problem.Fields(problem.FieldError{Field: "timeout", Code: "range"}))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(secs)*time.Second)
	defer cancel()
	reader := b.Peripherals().Reader
	if reader == nil {
		<-ctx.Done()
		if r.Context().Err() == nil {
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}
	text, err := reader.Next(ctx)
	if err != nil {
		if r.Context().Err() == nil {
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}
	respond.JSON(w, http.StatusOK, api.BridgeScan{Text: text})
}
