package sensor

import (
	"time"

	"github.com/coste360/porto/internal/store"
)

// health is the device health of m12-plan §9.2 (silent > offline > battery_low > ok): silent when the last contact
// (or the creation, never contacted) is older than silent_alert_minutes, offline when the last observed reading
// (or the creation) is older than offline_after_minutes, battery_low under battery_low_pct.
func health(d *store.SensorDevice, st *store.SensorDeviceState, set *store.SensorSettings, now time.Time) string {
	contact, observed := d.CreatedAt, d.CreatedAt
	if st != nil {
		if st.LastContactAt != nil {
			contact = *st.LastContactAt
		}
		if st.LastObservedAt != nil {
			observed = *st.LastObservedAt
		}
	}
	switch {
	case now.Sub(contact) >= time.Duration(set.SilentAlertMinutes)*time.Minute:
		return HealthSilent
	case now.Sub(observed) >= time.Duration(set.OfflineAfterMinutes)*time.Minute:
		return HealthOffline
	case st != nil && st.BatteryPct != nil && *st.BatteryPct < set.BatteryLowPct:
		return HealthBatteryLow
	}
	return HealthOK
}

// viewCtx carries what device views need besides the rows.
type viewCtx struct {
	settings  *store.SensorSettings
	codes     map[string]string
	pedestals map[string]int
	now       time.Time
}

// informational reports whether a pedestal device maps to no single berth (map and history only, no rules).
func informational(d *store.SensorDevice, pedestals map[string]int) bool {
	if d.ResourceType != store.SensorResourcePedestal {
		return false
	}
	if d.PedestalSocket != "" {
		return pedestals[d.ResourceID+"|"+d.PedestalSocket] != 1
	}
	return pedestals[d.ResourceID] != 1
}

func (s *Service) deviceView(d *store.SensorDevice, st *store.SensorDeviceState, vc viewCtx) DeviceView {
	return DeviceView{
		SensorDevice: *d, State: st, ResourceCode: vc.codes[d.ResourceID], Informational: informational(d, vc.pedestals),
		Health: health(d, st, vc.settings, vc.now), KeyStatus: s.keyStatus(deviceKeyCols(d), aadDevice+d.ID, vc.now),
	}
}
