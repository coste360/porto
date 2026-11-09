package engine

import (
	"cmp"
	"slices"
)

// Contract kinds and statuses read by Anomalies (values of contracts.kind and contracts.status).
const (
	contractMooring = "mooring"
	contractTransit = "transit"
	statusActive    = "active"
	statusDraft     = "draft"
	statusConfirmed = "confirmed"
	statusArrived   = "arrived"
)

// Anomalies returns the anomalies of the day-view elements (m3-plan §8.4), sorted by resource, allocation and
// code: presence_without_allocation, not_arrived, departure_overdue, vessel_exceeds_limits,
// insurance_expired_active, hold_lost_draft and (M12) sensor_discrepancy.
func Anomalies(in AnomalyInput) []Anomaly {
	var out []Anomaly
	for _, e := range in.Elements {
		add := func(code string) {
			out = append(out, Anomaly{ResourceID: e.ResourceID, AllocationID: e.AllocationID, Code: code})
		}
		transit := e.ContractKind == contractTransit
		overdue := transit && e.ContractStatus == statusArrived && e.Departure.Before(in.Date)
		if e.PresenceInPort && !e.HasAllocation && !overdue {
			add(AnomalyPresenceWithoutAllocation)
		}
		if transit && e.ContractStatus == statusConfirmed && !e.ArrivedPresence {
			late := e.Arrival.Before(in.Date) ||
				(e.Arrival.Equal(in.Date) && in.NowMinutes > e.ExpectedArrivalMins+in.GraceMinutes)
			if late && !e.Departure.Before(in.Date) {
				add(AnomalyNotArrived)
			}
		}
		if overdue && e.PresenceInPort {
			add(AnomalyDepartureOverdue)
		}
		if e.FitBlocking {
			add(AnomalyVesselExceedsLimits)
		}
		live := (e.ContractKind == contractMooring && e.ContractStatus == statusActive) ||
			(transit && (e.ContractStatus == statusConfirmed || e.ContractStatus == statusArrived))
		if live && e.HasAllocation && !e.InsuranceOK {
			add(AnomalyInsuranceExpiredActive)
		}
		if e.ContractKind == contractMooring && e.ContractStatus == statusDraft && e.HoldLost {
			add(AnomalyHoldLostDraft)
		}
	}
	out = append(out, sensorAnomalies(in)...)
	slices.SortStableFunc(out, func(a, b Anomaly) int {
		if c := cmp.Compare(a.ResourceID, b.ResourceID); c != 0 {
			return c
		}
		if c := cmp.Compare(a.AllocationID, b.AllocationID); c != 0 {
			return c
		}
		return cmp.Compare(a.Code, b.Code)
	})
	return out
}
