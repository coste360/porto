// Package ingest implements /api/ingest/v1 (M12, spec 24.3, 27.1; m12-plan §4): the readings of occupancy sensors,
// authenticated by the device or gateway key of docs/sensor-protocol.md and verified by sensor.Ingest (the router
// treats the operation as public; per-IP limits are applied by the router, per-key limits by the service). The
// raw body is passed unchanged because the signature covers its bytes. Keys, signatures and external ids are never
// logged (INV-105). Owned by INGEST.
package ingest

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/coste360/porto/internal/domain/sensor"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/problem"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/reqctx"
)

// Handler serves the ingestion API. A nil Sensors answers 404 (instances without the sensor service).
type Handler struct {
	Sensors *sensor.Service
}

// IngestSensorReadings implements api.ServerInterface.
func (h *Handler) IngestSensorReadings(w http.ResponseWriter, r *http.Request, params api.IngestSensorReadingsParams) {
	if h == nil || h.Sensors == nil {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			respond.Problem(w, r, problem.BadRequest, problem.Status(http.StatusRequestEntityTooLarge), problem.Detail("request body too large"))
			return
		}
		respond.Problem(w, r, problem.BadRequest)
		return
	}
	req := sensor.IngestRequest{
		Method: r.Method, Path: r.URL.Path, Body: body, Authorization: r.Header.Get("Authorization"),
		Secure: r.TLS != nil || reqctx.From(r.Context()).Scheme == "https" || reqctx.ListenerFrom(r.Context()) == reqctx.ListenerLAN,
	}
	if params.PortoKey != nil {
		req.PortoKey = *params.PortoKey
	}
	if params.PortoSignature != nil {
		req.PortoSignature = *params.PortoSignature
	}
	res, err := h.Sensors.Ingest(r.Context(), req)
	if err != nil {
		if te, ok := errors.AsType[*sensor.TimestampError](err); ok {
			w.Header().Set("Porto-Server-Time", strconv.FormatInt(te.ServerTime.Unix(), 10))
		}
		respond.Error(w, r, err)
		return
	}
	rejected := make([]api.SensorIngestRejected, 0, len(res.Rejected))
	for _, it := range res.Rejected {
		item := api.SensorIngestRejected{Index: it.Index, Code: api.SensorIngestItemCode(it.Code)}
		if it.DeviceID != "" {
			id := it.DeviceID
			item.DeviceId = &id
		}
		rejected = append(rejected, item)
	}
	w.Header().Set("Cache-Control", "no-store")
	respond.JSON(w, http.StatusOK, api.SensorIngestResult{
		Accepted: res.Accepted, Duplicates: res.Duplicates, Rejected: rejected, ServerTime: res.ServerTime.UTC(),
	})
}
