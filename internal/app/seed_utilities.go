package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/maintenance"
	"github.com/coste360/porto/internal/domain/maintenance/due"
	"github.com/coste360/porto/internal/domain/utility"
	"github.com/coste360/porto/internal/store"
)

// seedUtilities adds the M9 maintenance and utilities demo (m9-plan §15.2): the utility settings (UTL-KWH and
// UTL-H2O of the pricing seed), equipment (a travel lift with its hour meter, a crane, a forklift), power meters on
// the first twelve berths with a pedestal socket and two readings each (30 days apart), maintenance plans
// (underwater inspection of the berths, RCD test of the pedestals, travel lift service every 250 hours), the tasks
// the schedule job generates from them and one field report. Everything goes through the services as the demo
// owner; a no-op when the marina already has meters.
func (a *App) seedUtilities(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := a.DB.NewSelect().TableExpr("utility_meters").Where("marina_id = ?", scope.ID()).Count(ctx); err != nil || n > 0 {
		return err
	}
	m := res.MarinaID
	var items []struct {
		ID   string `bun:"id"`
		Code string `bun:"code"`
	}
	if err := a.DB.NewSelect().TableExpr("items").ColumnExpr("CAST(id AS TEXT) AS id, code").Where("marina_id = ?", scope.ID()).
		Where("code IN (?)", []any{"UTL-KWH", "UTL-H2O"}).Scan(ctx, &items); err != nil {
		return err
	}
	set, err := a.Utilities.GetSettings(ctx, p, m)
	if err != nil {
		return err
	}
	in := utility.SettingsInput{Mode: store.UtilityModeWithInstallment}
	for i := range items {
		switch items[i].Code {
		case "UTL-KWH":
			in.PowerItemID = &items[i].ID
		case "UTL-H2O":
			in.WaterItemID = &items[i].ID
		}
	}
	if _, err := a.Utilities.UpdateSettings(ctx, p, m, int64(set.Version), in); err != nil {
		return fmt.Errorf("settings: %w", err)
	}

	// Equipment and the travel lift hour meter.
	var lift string
	for _, e := range []maintenance.EquipmentInput{
		{Code: "TL-70", Name: "Travel lift 70 t", Kind: "travel_lift", CapacityKg: utlPtrInt64(70_000)},
		{Code: "GR-1", Name: "Gru a bandiera", Kind: "crane", CapacityKg: utlPtrInt64(3_000)},
		{Code: "MUL-1", Name: "Muletto", Kind: "forklift", CapacityKg: utlPtrInt64(2_500)},
	} {
		v, err := a.Maintenance.CreateEquipment(ctx, p, m, e)
		if err != nil {
			return fmt.Errorf("equipment %s: %w", e.Code, err)
		}
		if e.Kind == "travel_lift" {
			lift = v.Row.ID
		}
	}
	hours, err := a.Utilities.CreateMeter(ctx, p, m, utility.MeterInput{Code: "TL-70-H", Kind: store.MeterHours, Unit: "h", EquipmentID: lift})
	if err != nil {
		return fmt.Errorf("hour meter: %w", err)
	}
	now := a.Clock.Now().UTC().Truncate(time.Minute)
	monthAgo := now.Add(-30 * 24 * time.Hour)
	for i, v := range []string{"1180", "1436,5"} {
		at := monthAgo
		if i == 1 {
			at = now.Add(-time.Hour)
		}
		if _, err := a.Utilities.RecordReading(ctx, p, m, utility.ReadingInput{MeterID: hours.Row.ID, Value: v, ReadAt: &at}); err != nil {
			return fmt.Errorf("hour reading: %w", err)
		}
	}

	// Power meters on the berths fed by a pedestal socket, with a reading a month ago and one today.
	var berths []store.Berth
	if err := a.DB.NewSelect().Model(&berths).Where("marina_id = ?", scope.ID()).Where("pedestal_id IS NOT NULL").
		Where("pedestal_socket IS NOT NULL").OrderExpr("code").Limit(12).Scan(ctx); err != nil {
		return err
	}
	var berthTargets []maintenance.Element
	pedestals := map[string]bool{}
	var pedestalTargets []maintenance.Element
	for i, b := range berths {
		mt, err := a.Utilities.CreateMeter(ctx, p, m, utility.MeterInput{
			Code: b.Code + "-E", Kind: store.MeterPower, Unit: "kwh", PedestalID: b.PedestalID, SocketCode: b.PedestalSocket,
			ResourceType: store.ResourceBerth, ResourceID: b.ID, Serial: "EM-" + strconv.Itoa(1000+i),
		})
		if err != nil {
			return fmt.Errorf("meter %s: %w", b.Code, err)
		}
		start := 1000 + 137*i
		values := []string{strconv.Itoa(start), strconv.Itoa(start+40+11*i) + ",250"}
		for j, v := range values {
			at := monthAgo.Add(time.Duration(i) * time.Minute)
			if j == 1 {
				at = now.Add(-time.Duration(30+i) * time.Minute)
			}
			if _, err := a.Utilities.RecordReading(ctx, p, m, utility.ReadingInput{
				MeterID: mt.Row.ID, Value: v, ReadAt: &at, ClientRef: "seed-" + b.Code + "-" + strconv.Itoa(j),
			}); err != nil {
				return fmt.Errorf("reading %s: %w", b.Code, err)
			}
		}
		berthTargets = append(berthTargets, maintenance.Element{Type: store.ElementBerth, ID: b.ID})
		if !pedestals[b.PedestalID] {
			pedestals[b.PedestalID] = true
			pedestalTargets = append(pedestalTargets, maintenance.Element{Type: store.ElementPedestal, ID: b.PedestalID})
		}
	}

	// Plans: the inspection is due within the lead days, so the schedule job generates its tasks now.
	loc, err := calendar.LoadLocation(utlTZ(ctx, a, scope))
	if err != nil {
		return err
	}
	today := calendar.In(now, loc)
	inspectionStart := today.AddDays(-170)
	plans := []maintenance.PlanInput{
		{
			Name: "Ispezione subacquea corpi morti", Intervention: "underwater_inspection", Targets: berthTargets, LeadDays: 30,
			Periodicity: due.Periodicity{Kind: due.KindMonths, Every: 6, StartsOn: &inspectionStart}, SupplierName: "Sub Service",
			Checklist: []maintenance.ChecklistEntry{
				{Key: "corpo_morto", Label: "Corpo morto"}, {Key: "catenaria", Label: "Catenaria"}, {Key: "grillo", Label: "Grilli e redance"},
			},
			RequiresFree: true, Nights: 1,
		},
		{
			Name: "Prova differenziali colonnine", Intervention: "rcd_test", Targets: pedestalTargets, LeadDays: 14,
			Periodicity: due.Periodicity{Kind: due.KindFixedDate, MonthDay: utlMonthDay(today.AddDays(10))},
			Checklist:   []maintenance.ChecklistEntry{{Key: "rcd", Label: "Test differenziale"}, {Key: "prese", Label: "Prese e coperchi"}},
		},
		{
			Name: "Tagliando travel lift", Intervention: "general", Targets: []maintenance.Element{{Type: store.ElementEquipment, ID: lift}},
			Periodicity: due.Periodicity{Kind: due.KindCycles, Every: 250_000}, MeterID: hours.Row.ID,
			Checklist: []maintenance.ChecklistEntry{{Key: "olio", Label: "Olio idraulico"}, {Key: "funi", Label: "Fasce e funi"}},
		},
	}
	for _, pl := range plans {
		if len(pl.Targets) == 0 {
			continue
		}
		if _, err := a.Maintenance.CreatePlan(ctx, p, m, pl); err != nil {
			return fmt.Errorf("plan %s: %w", pl.Name, err)
		}
	}
	if _, err := a.Maintenance.ScheduleMarina(ctx, scope); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	if len(berths) > 0 {
		if _, err := a.Maintenance.Report(ctx, p, m, maintenance.ReportInput{
			ClientRef: "seed-report-1", Element: &maintenance.Element{Type: store.ElementPedestal, ID: berths[0].PedestalID},
			Title: "Presa 16 A non eroga", Description: "Segnalato dal cliente in banchina", Priority: "high",
		}); err != nil {
			return fmt.Errorf("report: %w", err)
		}
	}
	return nil
}

func utlPtrInt64(v int64) *int64 { return &v }

func utlMonthDay(d calendar.Date) string { return d.String()[5:] }

func utlTZ(ctx context.Context, a *App, scope store.MarinaScope) string {
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil || tz == "" {
		return "UTC"
	}
	return tz
}
