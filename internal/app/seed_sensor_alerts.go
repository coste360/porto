package app

import (
	"context"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/sensor"
	"github.com/coste360/porto/internal/store"
)

// seedSensorAlerts adds the M12 demo of RECONCILE (m12-plan §17.2): the presence effect enabled in the sensor
// settings, then one reconcile run on the seeded sensors prepared for one alert of every kind and one automatic
// presence out (watch.SeedDemo). A no-op when the marina has no sensors or already has sensor alerts.
func (a *App) seedSensorAlerts(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	if a.SensorWatch == nil || a.Sensors == nil || res.MarinaID == "" {
		return nil
	}
	ok, err := a.SensorWatch.Seedable(ctx, p, res.MarinaID)
	if err != nil || !ok {
		return err
	}
	set, err := a.Sensors.GetSettings(ctx, p, res.MarinaID)
	if err != nil {
		return err
	}
	if !set.PresenceEffectEnabled {
		in := sensor.SettingsInput{
			HysteresisReadings: set.HysteresisReadings, HysteresisMinutes: set.HysteresisMinutes, OfflineAfterMinutes: set.OfflineAfterMinutes,
			SilentAlertMinutes: set.SilentAlertMinutes, BatteryLowPct: set.BatteryLowPct, UnexpectedAfterMinutes: set.UnexpectedAfterMinutes,
			DepartureCutoffMinutes: set.DepartureCutoffMinutes, PresenceEffectEnabled: true, PresenceOutAfterHours: set.PresenceOutAfterHours,
			ReleaseProposalAfterDays: set.ReleaseProposalAfterDays, ReleaseProposalNights: set.ReleaseProposalNights,
		}
		if _, err := a.Sensors.PutSettings(ctx, p, res.MarinaID, set.Version, in); err != nil {
			return err
		}
	}
	if err := a.seedSensorArrival(ctx, p, res.MarinaID); err != nil {
		return err
	}
	if _, err = a.SensorWatch.SeedDemo(ctx, p, res.MarinaID); err != nil {
		return err
	}
	// The demo readings stop at the seed (the fake source is in memory): with the default thresholds the whole pier
	// would turn offline after half an hour and raise a silent alert per device after three. The demo keeps the
	// devices online for a day (a demo instance is reseeded nightly); the alerts of the seed stay open.
	if set, err = a.Sensors.GetSettings(ctx, p, res.MarinaID); err != nil {
		return err
	}
	in := sensor.SettingsInput{
		HysteresisReadings: set.HysteresisReadings, HysteresisMinutes: set.HysteresisMinutes, OfflineAfterMinutes: seedSensorQuietMinutes,
		SilentAlertMinutes: max(set.SilentAlertMinutes, seedSensorQuietMinutes), BatteryLowPct: set.BatteryLowPct,
		UnexpectedAfterMinutes: set.UnexpectedAfterMinutes, DepartureCutoffMinutes: set.DepartureCutoffMinutes,
		PresenceEffectEnabled: set.PresenceEffectEnabled, PresenceOutAfterHours: set.PresenceOutAfterHours,
		ReleaseProposalAfterDays: set.ReleaseProposalAfterDays, ReleaseProposalNights: set.ReleaseProposalNights,
	}
	_, err = a.Sensors.PutSettings(ctx, p, res.MarinaID, set.Version, in)
	return err
}

// seedSensorQuietMinutes is the demo's offline and silent-alert threshold (one day, the maximum offline_after).
const seedSensorQuietMinutes = 1440

// seedSensorArrival books a confirmed transit arriving today on a free berth with a sensor (the transit arrival
// proposal of the demo), unless one already arrives today or yesterday on such a berth. Taken berths are skipped.
func (a *App) seedSensorArrival(ctx context.Context, p *auth.Principal, marinaID string) error {
	db := a.DB.DB
	scope, err := store.ResolveMarinaScope(ctx, db, marinaID)
	if err != nil {
		return err
	}
	devs, err := store.SensorWatchDevices(ctx, db, scope, store.SensorWatchDeviceFilter{ActiveOnly: true, ResourceType: store.SensorResourceBerth})
	if err != nil || len(devs) == 0 {
		return err
	}
	tz, err := store.MarinaTimezone(ctx, db, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := calendar.In(a.Clock.Now(), loc)
	ids := make([]string, 0, len(devs))
	for _, d := range devs {
		ids = append(ids, d.ResourceID)
	}
	stays, err := store.SensorStays(ctx, db, scope, ids, today.AddDays(-1), today.AddDays(2))
	if err != nil {
		return err
	}
	busy := map[string]bool{}
	for _, st := range stays {
		if st.ContractKind == contract.KindTransit && st.ContractStatus == string(contract.StatusConfirmed) &&
			(st.FirstNight.Equal(today) || st.FirstNight.Equal(today.AddDays(-1))) {
			return nil // the demo already has an arrival on a sensor berth
		}
		busy[st.ResourceID] = true
	}
	var customers []string
	if err := db.NewSelect().Table("customers").Column("id").Where("marina_id = ?", marinaID).OrderExpr("id").Limit(1).
		Scan(ctx, &customers); err != nil || len(customers) == 0 {
		return err
	}
	for _, d := range devs {
		if busy[d.ResourceID] {
			continue
		}
		_, err := a.Contracts.CreateTransit(ctx, p, marinaID, contract.TransitInput{
			NewVessel:  &contract.NewVessel{Name: "Ospite sensore", LOACm: 800, BeamCm: 280, HullType: "monohull_sail"},
			CustomerID: customers[0], Arrival: today, Departure: today.AddDays(2), Confirm: &contract.ManualConfirmation{Reference: "demo"},
			Placements: []contract.Segment{{ResourceType: store.ResourceBerth, ResourceID: d.ResourceID, First: today, Last: today.AddDays(1)}},
		})
		if err == nil {
			return nil
		}
	}
	return nil
}
