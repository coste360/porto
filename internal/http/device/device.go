// Package device implements the /api/device/v1 operations: pairing (code or demo) and the device's own record.
package device

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/device"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Handler serves the device API.
type Handler struct {
	Auth    *auth.Service
	Devices *device.Service
	BaseURL string
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// writePairResult answers a successful pairing: the token is shown once, with the device, its user and marina.
func (h *Handler) writePairResult(w http.ResponseWriter, r *http.Request, res *device.PairResult) {
	p, err := h.Auth.AuthenticateDevice(r.Context(), res.Token)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	self, err := h.Devices.Self(r.Context(), p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	sv := view.DeviceSelf(self)
	respond.JSON(w, http.StatusCreated, api.PairResult{
		Token: res.Token, Device: sv.Device, Marina: sv.Marina, User: sv.User, BaseUrl: h.BaseURL,
	})
}

// PairDevice implements api.ServerInterface.
func (h *Handler) PairDevice(w http.ResponseWriter, r *http.Request, _ api.PairDeviceParams) {
	var in api.PairRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Devices.Pair(r.Context(), device.PairInput{
		Code: in.Code, Name: in.Name, Platform: string(in.Platform), Kind: string(in.Kind), AppVersion: str(in.AppVersion),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writePairResult(w, r, res)
}

// DemoPairDevice implements api.ServerInterface.
func (h *Handler) DemoPairDevice(w http.ResponseWriter, r *http.Request) {
	var in api.DemoPairRequest
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Devices.DemoPair(r.Context(), device.PairInput{
		Name: in.Name, Platform: string(in.Platform), Kind: string(in.Kind), AppVersion: str(in.AppVersion),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	h.writePairResult(w, r, res)
}

// GetDeviceSelf implements api.ServerInterface.
func (h *Handler) GetDeviceSelf(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	self, err := h.Devices.Self(r.Context(), p)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.DeviceSelf(self))
}

// UnpairDeviceSelf implements api.ServerInterface.
func (h *Handler) UnpairDeviceSelf(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	if err := h.Devices.RevokeSelf(r.Context(), p); err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.NoContent(w)
}
