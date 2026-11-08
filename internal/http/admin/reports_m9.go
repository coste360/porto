package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/report/calc"
	"github.com/coste360/porto/internal/domain/report/export"
	"github.com/coste360/porto/internal/http/api"
)

// Storage, maintenance and utilities reports (M9, m9-plan §11, M7 machinery): the same serveReport body as the M7
// reports (parse, authorise in the service, JSON or CSV/XLSX from the same result). Owned by PLANNER.

// GetStorageReport implements api.ServerInterface (GET .../reports/storage).
func (h *Handler) GetStorageReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetStorageReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyStorage, h.Reports.Storage, resultView(
		func(x *calc.StorageResult) *calc.StorageResult { return x },
		func(x *calc.StorageResult) any { return storageReportJSON(x) }, export.Storage))
}

// GetMaintenanceReport implements api.ServerInterface (GET .../reports/maintenance).
func (h *Handler) GetMaintenanceReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetMaintenanceReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyMaintenance, h.Reports.Maintenance, resultView(
		func(x *calc.MaintenanceResult) *calc.MaintenanceResult { return x },
		func(x *calc.MaintenanceResult) any { return maintenanceReportJSON(x) }, export.Maintenance))
}

// GetUtilitiesReport implements api.ServerInterface (GET .../reports/utilities).
func (h *Handler) GetUtilitiesReport(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.GetUtilitiesReportParams) {
	serveReport(h, w, r, marinaID, calc.KeyUtilities, h.Reports.Utilities, resultView(
		func(x *calc.UtilitiesResult) *calc.UtilitiesResult { return x },
		func(x *calc.UtilitiesResult) any { return utilitiesReportJSON(x) }, export.Utilities))
}

func storageReportJSON(x *calc.StorageResult) api.StorageReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.StorageTotals) api.StorageReportTotals {
		hull := t.HullM2Milli
		return api.StorageReportTotals{
			AvailableM2NightsMilli: t.AvailableM2Milli, OccupiedM2NightsMilli: t.OccupiedM2Milli,
			HullM2NightsMilli: &hull, OccupancyBp: t.OccupancyBP.Ptr(), Boats: t.Boats, ExitsBlocked: t.ExitsBlocked,
			ProductionCents: t.Production.Ptr(),
		}
	}
	return api.StorageReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.StorageTotals]) api.StorageReportGroup {
			return api.StorageReportGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.StorageRecord) api.StorageReportRecord {
			return api.StorageReportRecord{
				AllocationId: r.AllocationID, AreaId: r.AreaID, AreaCode: r.AreaCode, MapKind: api.MapKind(r.MapKind),
				VesselId: rpS(r.VesselID), LoaCm: rpI(int64(r.LOA)), BeamCm: rpI(int64(r.Beam)),
				ContractKind: rpE[api.ContractKind](r.ContractKind), ContractCode: rpS(r.ContractCode), Nights: r.Nights,
				HullM2NightsMilli: r.HullM2Milli, FootprintM2NightsMilli: r.FootprintM2Milli,
				ExitOn: rpD(r.ExitOn), ExitBlocked: r.ExitBlocked, ProductionCents: r.Production.Ptr(), Ref: refJSON(r.Ref),
			}
		}),
	}
}

func maintenanceReportJSON(x *calc.MaintenanceResult) api.MaintenanceReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.MaintenanceTotals) api.MaintenanceReportTotals {
		return api.MaintenanceReportTotals{
			Tasks: t.Tasks, Open: t.Open, Scheduled: t.Scheduled, InProgress: t.InProgress, Done: t.Done, Cancelled: t.Cancelled,
			Overdue: t.Overdue, InternalMinutes: t.InternalMinutes, ExternalCostCents: t.ExternalCost.Ptr(),
		}
	}
	return api.MaintenanceReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.MaintenanceTotals]) api.MaintenanceReportGroup {
			return api.MaintenanceReportGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.MaintenanceRecord) api.MaintenanceReportRecord {
			out := api.MaintenanceReportRecord{
				TaskId: r.TaskID, Intervention: rpE[api.MaintenanceIntervention](r.Intervention),
				Source: api.MaintenanceSource(r.Source), Priority: api.MaintenancePriority(r.Priority),
				Status: api.MaintenanceTaskStatus(r.Status), DueOn: rpD(r.DueOn), DoneOn: rpD(r.DoneOn),
				OverdueDays: r.OverdueDays, InternalMinutes: rpI(r.InternalMinutes), ExternalCostCents: r.ExternalCost.Ptr(),
				Ref: refJSON(r.Ref),
			}
			if r.ElementType != "" {
				out.Element = &api.MaintenanceElementRef{
					ElementType: api.MaintenanceElementType(r.ElementType), ElementId: r.ElementID, Code: rpS(r.ElementCode),
				}
			}
			return out
		}),
	}
}

func utilitiesReportJSON(x *calc.UtilitiesResult) api.UtilitiesReport {
	e := envJSON(&x.Envelope)
	tot := func(t calc.UtilitiesTotals) api.UtilitiesReportTotals {
		return api.UtilitiesReportTotals{
			PowerMilli: t.PowerMilli, WaterMilli: t.WaterMilli, HoursMilli: t.HoursMilli, CyclesMilli: t.CyclesMilli,
			PendingMilli: t.PendingMilli, UnassignedMilli: t.UnassignedMilli, ChargedMilli: t.ChargedMilli,
			ChargedCents: t.Charged.Ptr(), PendingValueCents: t.PendingValue.Ptr(),
		}
	}
	return api.UtilitiesReport{
		Filters: e.filters, GroupBy: e.groupBy, IgnoredFilters: e.ignored, Notes: e.notes, Page: e.page, Period: e.period,
		Report: e.report, Section: e.section, Today: e.today, Totals: tot(x.Totals),
		Groups: groupsJSON(x.Groups, func(g calc.Group[calc.UtilitiesTotals]) api.UtilitiesReportGroup {
			return api.UtilitiesReportGroup{Key: g.Key, Label: g.Label, Totals: tot(g.Totals)}
		}),
		Records: recordsJSON(x.Records, func(r *calc.UtilitiesRecord) api.UtilitiesReportRecord {
			return api.UtilitiesReportRecord{
				ConsumptionId: r.ConsumptionID, MeterId: r.MeterID, MeterCode: r.MeterCode, Kind: api.MeterKind(r.Kind),
				ResourceType: rpE[api.ResourceType](r.ResourceType), ResourceId: rpS(r.ResourceID), ContractId: rpS(r.ContractID),
				ContractCode: rpS(r.ContractCode), FirstNight: r.First.String(), LastNight: r.Last.String(), Nights: r.Nights,
				QtyMilli: r.QtyMilli, Status: api.UtilityConsumptionStatus(r.Status), ChargedCents: r.Charged.Ptr(),
				Ref: refJSON(r.Ref),
			}
		}),
	}
}
