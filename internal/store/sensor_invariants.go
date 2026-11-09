package store

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// CheckSensorInvariants runs the M12 ingestion invariant queries on a marina (m12-plan §15, INV-101) and returns
// every violation (none on a coherent database). Q42 and Q43 (alerts, sensor presences) are RECONCILE's.
//
//	Q40 each device state's last_observed_at is at or after every stored reading of the device, and physical equals
//	    the to_state of its latest state change (unknown with none).
//	Q41 the state changes of a device alternate: from_state equals the previous to_state (unknown before the first)
//	    and at is non-decreasing.
func CheckSensorInvariants(ctx context.Context, db bun.IDB, marinaID string) ([]Violation, error) {
	var out []Violation
	var late []string
	if err := db.NewSelect().TableExpr("sensor_device_states AS st").Column("st.device_id").
		Where("st.marina_id = ?", marinaID).
		Where("EXISTS (SELECT 1 FROM sensor_readings AS r WHERE r.device_id = st.device_id AND (st.last_observed_at IS NULL OR r.observed_at > st.last_observed_at))").
		OrderExpr("st.device_id").Scan(ctx, &late); err != nil {
		return nil, fmt.Errorf("Q40: %w", err)
	}
	for _, id := range late {
		out = append(out, Violation{Query: "Q40", IDs: []string{id}, Detail: "reading newer than last_observed_at"})
	}
	var states []SensorDeviceState
	if err := db.NewSelect().Model(&states).Where("marina_id = ?", marinaID).OrderExpr("device_id").Scan(ctx); err != nil {
		return nil, fmt.Errorf("Q40: %w", err)
	}
	var changes []SensorStateChange
	if err := db.NewSelect().Model(&changes).Where("marina_id = ?", marinaID).OrderExpr("device_id, at, created_at, id").Scan(ctx); err != nil {
		return nil, fmt.Errorf("Q41: %w", err)
	}
	last := map[string]SensorStateChange{}
	for _, c := range changes {
		prev, ok := last[c.DeviceID]
		from := SensorUnknown
		if ok {
			from = prev.ToState
		}
		if c.FromState != from {
			out = append(out, Violation{Query: "Q41", IDs: []string{c.DeviceID, c.ID}, Detail: fmt.Sprintf("from %s after %s", c.FromState, from)})
		}
		if ok && c.At.Before(prev.At) {
			out = append(out, Violation{Query: "Q41", IDs: []string{c.DeviceID, c.ID}, Detail: "instant before the previous change"})
		}
		last[c.DeviceID] = c
	}
	for _, st := range states {
		want := SensorUnknown
		if c, ok := last[st.DeviceID]; ok {
			want = c.ToState
		}
		if st.Physical != want {
			out = append(out, Violation{Query: "Q40", IDs: []string{st.DeviceID}, Detail: fmt.Sprintf("physical %s, latest change to %s", st.Physical, want)})
		}
	}
	return out, nil
}
