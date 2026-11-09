package engine

import (
	"cmp"
	"slices"
)

// Physical states and expectations of a SensorFact (m12-plan §8.1).
const (
	sensorOccupied = "occupied"
	sensorFree     = "free"
	expectOccupied = "occupied"
	expectFree     = "free"
)

// sensorAnomalies returns the sensor_discrepancy anomalies of in.Sensors (M12, m12-plan §8.1, §9.2): physical
// occupied stable for at least SensorUnexpectedMinutes with expectation free, or physical free with expectation
// occupied; unknown, either, none and informational devices never. One anomaly per resource (several segment
// devices of one area report one anomaly on the area element), sorted by resource id.
func sensorAnomalies(in AnomalyInput) []Anomaly {
	if len(in.Sensors) == 0 {
		return nil
	}
	var out []Anomaly
	for id, f := range in.Sensors {
		if f.Informational || f.StableMinutes < in.SensorUnexpectedMinutes {
			continue
		}
		if (f.Physical == sensorOccupied && f.Expectation == expectFree) || (f.Physical == sensorFree && f.Expectation == expectOccupied) {
			out = append(out, Anomaly{ResourceID: id, Code: AnomalySensorDiscrepancy})
		}
	}
	slices.SortFunc(out, func(a, b Anomaly) int { return cmp.Compare(a.ResourceID, b.ResourceID) })
	return out
}
