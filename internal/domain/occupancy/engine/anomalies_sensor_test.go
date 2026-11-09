package engine

import (
	"testing"

	"github.com/coste360/porto/internal/calendar"
)

func TestAnomalies_SensorDiscrepancy(t *testing.T) {
	in := AnomalyInput{
		Date: calendar.MustParse("2027-06-12"), SensorUnexpectedMinutes: 15,
		Sensors: map[string]SensorFact{
			"b-occ-free": {Physical: "occupied", StableMinutes: 15, Expectation: "free"},
			"b-free-occ": {Physical: "free", StableMinutes: 60, Expectation: "occupied"},
			"b-early":    {Physical: "occupied", StableMinutes: 14, Expectation: "free"},
			"b-either":   {Physical: "occupied", StableMinutes: 60, Expectation: "either"},
			"b-unknown":  {Physical: "unknown", StableMinutes: 60, Expectation: "occupied"},
			"b-agree":    {Physical: "occupied", StableMinutes: 60, Expectation: "occupied"},
			"b-none":     {Physical: "occupied", StableMinutes: 60, Expectation: "none"},
			"b-pedestal": {Physical: "occupied", StableMinutes: 60, Expectation: "free", Informational: true},
		},
	}
	got := Anomalies(in)
	if len(got) != 2 || got[0].ResourceID != "b-free-occ" || got[1].ResourceID != "b-occ-free" {
		t.Fatalf("got %+v", got)
	}
	for _, a := range got {
		if a.Code != AnomalySensorDiscrepancy || a.AllocationID != "" {
			t.Fatalf("got %+v", a)
		}
	}
	if Anomalies(AnomalyInput{}) != nil {
		t.Fatal("no sensors: no anomalies")
	}
}
