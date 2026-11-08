package admin

import (
	"net/http"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/report"
	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
)

// Manager dashboard of M7 (spec 21.2, m7-plan §4): the service builds every tile from the totals of the report
// call its drill link makes (INV-62); the handler parses the raw query string, encodes and logs only the
// counts (no PII).

// GetDashboard implements api.ServerInterface (GET .../dashboard).
func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetDashboardParams) {
	start := h.dashNow()
	p := principal(r)
	q, err := report.ParseDashboardParams(r.URL.Query())
	if err != nil {
		h.dashParseError(w, r, marinaID, err)
		return
	}
	d, err := h.Reports.Dashboard(r.Context(), p, marinaID, q)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.Logger(r.Context()).Info("dashboard served",
		"report", "dashboard", "period_days", dashPeriodDays(d.From, d.To), "tiles", len(d.Tiles),
		"duration_ms", h.dashSince(start))
	respond.JSON(w, http.StatusOK, dashboardOf(d))
}

// dashParseError answers an invalid dashboard/comparison query: the request is authorised first, so another
// marina is still 404 (and a role without report permissions 403) before any 422.
func (h *Handler) dashParseError(w http.ResponseWriter, r *http.Request, marinaID string, err error) {
	if cerr := h.Reports.Check(r.Context(), principal(r), marinaID); cerr != nil {
		respond.Error(w, r, cerr)
		return
	}
	respond.Error(w, r, err)
}

// dashNow is the handler clock (the wall clock when none is injected); used for durations in logs only.
func (h *Handler) dashNow() time.Time {
	if h.Clock == nil {
		return time.Time{}
	}
	return h.Clock.Now()
}

// dashSince is the elapsed milliseconds from start on the handler clock.
func (h *Handler) dashSince(start time.Time) int64 {
	if h.Clock == nil || start.IsZero() {
		return 0
	}
	return h.Clock.Now().Sub(start).Milliseconds()
}

// dashPeriodDays is the number of nights of an inclusive period (0 when unset).
func dashPeriodDays(from, to calendar.Date) int {
	if from.IsZero() || to.IsZero() {
		return 0
	}
	return from.DaysUntil(to) + 1
}

func dashDate(d calendar.Date) api.IsoDate { return d.String() }

func dashboardOf(d *calc.Dashboard) api.Dashboard {
	out := api.Dashboard{
		Align:       api.ReportAlign(d.Align),
		Compare:     api.DashboardCompare(d.Compare),
		Filters:     dashMap(d.Filters),
		Granularity: api.ReportGranularity(d.Granularity),
		Period:      api.ReportPeriod{From: dashDate(d.From), To: dashDate(d.To)},
		Today:       dashDate(d.Today),
		Tiles:       make([]api.Tile, 0, len(d.Tiles)),
	}
	if !d.PrevFrom.IsZero() && !d.PrevTo.IsZero() {
		out.PreviousPeriod = &api.ReportPeriod{From: dashDate(d.PrevFrom), To: dashDate(d.PrevTo)}
	}
	for i := range d.Tiles {
		out.Tiles = append(out.Tiles, tileOf(&d.Tiles[i]))
	}
	return out
}

func tileOf(t *calc.Tile) api.Tile {
	out := api.Tile{
		Key:         api.TileKey(t.Key),
		Unit:        api.ReportUnit(t.Unit),
		Available:   t.Available,
		DimsApplied: t.DimsApplied,
		Value:       t.Current.Value.Ptr(),
		Delta:       t.Delta.Ptr(),
		DeltaBp:     t.DeltaBP.Ptr(),
		Trend:       api.TileTrend(t.Trend),
		Series:      make([]api.TileSeriesPoint, 0, len(t.Series)),
	}
	if out.Trend == "" {
		out.Trend = api.TileTrendNone
	}
	if t.Current.HasFraction {
		out.Numerator, out.Denominator = dashInt(t.Current.Numerator), dashInt(t.Current.Denominator)
	}
	if t.Reason != "" {
		reason := api.TileReason(t.Reason)
		out.Reason = &reason
	}
	if t.Previous != nil {
		out.Previous = &api.TileValue{Value: t.Previous.Value.Ptr()}
		if t.Previous.HasFraction {
			out.Previous.Numerator = dashInt(t.Previous.Numerator)
			out.Previous.Denominator = dashInt(t.Previous.Denominator)
		}
	}
	for _, s := range t.Series {
		pt := api.TileSeriesPoint{Bucket: dashDate(s.Bucket), Value: s.Value.Ptr()}
		if t.Previous != nil {
			pt.Previous = s.Previous.Ptr()
		}
		out.Series = append(out.Series, pt)
	}
	if len(t.Breakdown) > 0 {
		bd := make([]api.TileBreakdown, 0, len(t.Breakdown))
		for _, b := range t.Breakdown {
			e := api.TileBreakdown{Key: b.Key, Value: b.Value}
			if b.Label != "" {
				label := b.Label
				e.Label = &label
			}
			bd = append(bd, e)
		}
		out.Breakdown = &bd
	}
	if t.Drill != nil {
		out.Drill = &api.TileDrill{Report: api.ReportKey(t.Drill.Report), Params: dashMap(t.Drill.Params)}
	}
	return out
}

func dashInt(v int64) *int64 { return &v }

func dashMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
