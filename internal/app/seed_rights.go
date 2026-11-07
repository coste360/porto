package app

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// seedRights seeds the M4 demo data owned by RIGHTS-CHARGES (m4-plan §10), through the services as the demo
// owner: the concession (expiry T+30 years, 4 obligations), 60 rights on free berths (12 co-held; the demo harbour
// of M1 has no pier F, so the free berths of the last piers are used), 4 mandates (one current, with a transit sold
// inside it and its revenue accrued), 1 completed transfer, a per-m² fee table with an issued run for the year,
// 2 millesimal tables (general and one pier), a budget with 6 items and an issued advance run. Idempotent: it runs
// only when the demo marina has no rights. Rights end on 31 December of T+4 years to keep the demo database small
// (the concession expiry stays their default end through the API).
func (a *App) seedRights(ctx context.Context, p *auth.Principal, res *SeedResult) error {
	scope, err := store.ResolveMarinaScope(ctx, a.DB.DB, res.MarinaID)
	if err != nil {
		return err
	}
	if n, err := store.CountBerthRights(ctx, a.DB.DB, scope); err != nil || n > 0 {
		return err
	}
	tz, err := store.MarinaTimezone(ctx, a.DB.DB, scope)
	if err != nil {
		return err
	}
	loc, err := calendar.LoadLocation(tz)
	if err != nil {
		return err
	}
	today := occupancy.Today(a.Clock, loc)
	m := res.MarinaID
	rng := rand.New(rand.NewPCG(41, 5)) //nolint:gosec // demo data: deterministic by design
	year := today.Year()

	// concession
	expires := calendar.New(year+30, 12, 31)
	fee := int64(4_850_000)
	auth0, deed, number := "Autorità di sistema portuale", "Concessione demaniale marittima", "12/2015"
	if _, err := a.Rights.UpdateConcession(ctx, p, m, 0, rights.ConcessionInput{
		Authority: &auth0, DeedKind: &deed, DeedNumber: &number, GrantedOn: ptrDate(calendar.New(2015, 3, 1)), ExpiresOn: &expires,
		AnnualFeeCents: &fee, Surfaces: &[]rights.Surface{{Kind: "water", AreaCm: 1_250_000_000}, {Kind: "land", AreaCm: 180_000_000}},
	}); err != nil {
		return fmt.Errorf("concession: %w", err)
	}
	for _, o := range []rights.ObligationInput{
		{Kind: "fee_instalment", Description: "Canone demaniale, prima rata", DueOn: calendar.New(year, 6, 30), Recurrence: "yearly", AmountCents: &fee},
		{Kind: "deposit", Description: "Rinnovo cauzione", DueOn: calendar.New(year+1, 1, 31), Recurrence: "none"},
		{Kind: "insurance", Description: "Polizza RC della concessione", DueOn: calendar.New(year, 12, 31), Recurrence: "yearly"},
		{Kind: "periodic_report", Description: "Relazione annuale all'autorità", DueOn: calendar.New(year, 3, 31), Recurrence: "yearly"},
	} {
		if _, err := a.Rights.CreateObligation(ctx, p, m, o); err != nil {
			return fmt.Errorf("obligation: %w", err)
		}
	}

	// free berths (no active allocation from today to the end of the rights), from the last piers
	last := calendar.New(year+4, 12, 31)
	berths, err := seedFreeBerths(ctx, a.DB.DB, scope, today, last, 60)
	if err != nil {
		return err
	}
	cs, err := store.ListCustomers(ctx, a.DB.DB, scope, store.CustomerFilter{}, "", store.MaxLimit)
	if err != nil {
		return err
	}
	if len(cs.Items) < 2 || len(berths) == 0 {
		return nil
	}
	var made []*rights.Right
	for i, b := range berths {
		c1 := cs.Items[(i*7)%len(cs.Items)].ID
		holders := []allot.Share{{CustomerID: c1, Millis: 1000}}
		if i%5 == 0 { // 12 co-held of 60
			c2 := cs.Items[(i*7+3)%len(cs.Items)].ID
			if c2 != c1 {
				holders = []allot.Share{{CustomerID: c1, Millis: 500}, {CustomerID: c2, Millis: 500}}
				if i%10 == 0 {
					holders = []allot.Share{{CustomerID: c1, Millis: 333}, {CustomerID: c2, Millis: 667}}
				}
			}
		}
		kind := []string{rights.KindOwnershipShare, rights.KindLongTermUse, rights.KindMembership}[rng.IntN(3)]
		r, err := a.Rights.CreateRight(ctx, p, m, rights.RightInput{
			BerthID: b.ID, Kind: kind, FromOn: calendar.New(year-rng.IntN(5), 1, 1), ToOn: last, Holders: holders,
			Title: rights.Title{Deed: "Atto di cessione", Number: fmt.Sprintf("%d/%d", 100+i, year-3), Notary: "Notaio demo"},
		})
		if err != nil {
			if seedSkippable(err) || errors.Is(err, rights.ErrSharesInvalid) {
				continue
			}
			return fmt.Errorf("right %s: %w", b.Code, err)
		}
		made = append(made, r)
	}
	res.Rights = len(made)
	if len(made) < 6 {
		return nil
	}

	// mandates: one current (with a transit sold inside and its revenue accrued), three in the summer
	mandates := []struct{ from, to calendar.Date }{
		{today, today.AddDays(30)},
		{maxD(calendar.New(year, 7, 1), today.AddDays(40)), maxD(calendar.New(year, 8, 31), today.AddDays(100))},
		{maxD(calendar.New(year, 7, 1), today.AddDays(40)), maxD(calendar.New(year, 8, 31), today.AddDays(100))},
		{calendar.New(year+1, 6, 1), calendar.New(year+1, 9, 30)},
	}
	for i, md := range mandates {
		if _, err := a.Rights.CreateMandate(ctx, p, m, rights.MandateInput{
			RightID: made[i].ID, FromOn: md.from, ToOn: md.to, OwnerShareBP: 6000 + int64(i)*500, MinGuaranteedCents: 150_000,
			Channels: []string{"transit", "period"}, Settlement: "credit",
		}); err != nil {
			return fmt.Errorf("mandate: %w", err)
		}
		res.Mandates++
	}
	if vs, err := store.ListVessels(ctx, a.DB.DB, scope, store.VesselFilter{}, "", 50); err == nil {
		for _, v := range vs.Items {
			_, err := a.Contracts.CreateTransit(ctx, p, m, contract.TransitInput{
				VesselID: v.ID, CustomerID: cs.Items[0].ID, Arrival: today.AddDays(3), Departure: today.AddDays(8),
				Placements: []contract.Segment{{ResourceType: store.ResourceBerth, ResourceID: made[0].BerthID, First: today.AddDays(3), Last: today.AddDays(7)}},
				Confirm:    &contract.ManualConfirmation{Reference: "demo"},
			})
			if err == nil {
				break
			}
			if !seedSkippable(err) {
				return fmt.Errorf("mandate transit: %w", err)
			}
		}
	}
	if _, err := a.Rights.AccrueMandates(ctx, p, m); err != nil {
		return fmt.Errorf("accrue: %w", err)
	}

	// management fees: per m² with ISTAT, issued for the year in two semesters
	rate := int64(1_250)
	ft, err := a.Rights.CreateFeeTable(ctx, p, m, rights.FeeTableInput{
		Year: year, Name: "Oneri di gestione " + fmt.Sprint(year),
		Basis: allot.BasisPerM2Berth, IstatBP: 170, Rows: []rights.FeeRow{{RateCentsPerM2: &rate}},
	})
	if err != nil {
		return fmt.Errorf("fee table: %w", err)
	}
	if ft, err = a.Rights.ActivateFeeTable(ctx, p, m, ft.ID, ft.Version); err != nil {
		return err
	}
	plan := []byte(`{"schedule":{"mode":"fixed_dates","dates":[{"due_on":"06-30","bp":5000},{"due_on":"12-15","bp":5000}]}}`)
	run, err := a.Rights.CreateFeeRun(ctx, p, m, year, ft.ID, plan)
	if err != nil {
		return fmt.Errorf("fee run: %w", err)
	}
	if _, err := a.Rights.SimulateFeeRun(ctx, p, m, run.ID, run.Version); err != nil {
		return fmt.Errorf("fee simulate: %w", err)
	}
	cur, err := a.Rights.GetFeeRun(ctx, p, m, run.ID)
	if err != nil {
		return err
	}
	if _, err := a.Rights.IssueFeeRun(ctx, p, m, run.ID, cur.Version); err != nil {
		return fmt.Errorf("fee issue: %w", err)
	}
	res.FeeRuns = 1

	// transfer: the last right goes to another customer on 1 September (or in 30 days)
	tr := made[len(made)-1]
	eff := maxD(calendar.New(year, 9, 1), today.AddDays(30))
	to := cs.Items[len(cs.Items)-1].ID
	if to == tr.Holders[0].CustomerID {
		to = cs.Items[len(cs.Items)-2].ID
	}
	t, err := a.Rights.CreateTransfer(ctx, p, m, rights.TransferInput{
		RightID: tr.ID, EffectiveOn: eff,
		ToHolders: []allot.Share{{CustomerID: to, Millis: 1000}}, FeesPolicy: rights.FeesReassign,
	})
	if err != nil {
		return fmt.Errorf("transfer: %w", err)
	}
	if _, err := a.Rights.CompleteTransfer(ctx, p, m, t.ID, t.Version); err != nil {
		return fmt.Errorf("transfer complete: %w", err)
	}
	res.Transfers = 1

	// millesimal tables: general over the berths with rights, one over the berths of their first pier
	general := make([]string, len(made))
	for i, r := range made {
		general[i] = r.BerthID
	}
	gt, err := seedMillesimal(ctx, a, p, m, "Generale", "GEN", berths, general)
	if err != nil {
		return err
	}
	prefix := berths[0].Code[:strings.IndexByte(berths[0].Code+"-", '-')]
	var pier []string
	for _, b := range berths {
		if strings.HasPrefix(b.Code, prefix+"-") {
			pier = append(pier, b.ID)
		}
	}
	pt, err := seedMillesimal(ctx, a, p, m, "Pontile "+prefix, "PONT-"+prefix, berths, pier)
	if err != nil {
		return err
	}
	res.MillesimalTables = 2

	// budget with 6 items and an issued advance (50 %)
	bud, err := a.Rights.CreateBudget(ctx, p, m, year, "Spese comuni "+fmt.Sprint(year))
	if err != nil {
		return fmt.Errorf("budget: %w", err)
	}
	for i, it := range []struct {
		name   string
		table  string
		amount int64
	}{
		{"Pulizia pontili", gt, 1_234_567},
		{"Illuminazione", gt, 987_643},
		{"Vigilanza", gt, 2_450_001},
		{"Manutenzione pontile", pt, 1_100_000},
		{"Acqua", pt, 355_533},
		{"Assicurazione parti comuni", gt, 640_009},
	} {
		name, table, amount, sort := it.name, it.table, it.amount, i
		if _, err := a.Rights.CreateBudgetItem(ctx, p, m, bud.ID, rights.ItemInput{Name: &name, TableID: &table, BudgetCents: &amount, Sort: &sort}); err != nil {
			return fmt.Errorf("budget item: %w", err)
		}
	}
	if bud, err = a.Rights.ApproveBudget(ctx, p, m, bud.ID, bud.Version); err != nil {
		return fmt.Errorf("budget approve: %w", err)
	}
	share := int64(5000)
	adv, err := a.Rights.CreateCommonRun(ctx, p, m, bud.ID, rights.CommonRunInput{Kind: rights.RunAdvance, ShareBP: &share})
	if err != nil {
		return fmt.Errorf("advance run: %w", err)
	}
	if _, err := a.Rights.IssueCommonRun(ctx, p, m, adv.ID, adv.Version); err != nil {
		return fmt.Errorf("advance issue: %w", err)
	}
	res.CommonChargeRuns = 1
	n, err := store.AllHolderInstallments(ctx, a.DB.DB, scope, store.HolderInstallmentFilter{})
	if err != nil {
		return err
	}
	res.HolderInstallments = len(n)
	return nil
}

func ptrDate(d calendar.Date) *calendar.Date { return &d }

func maxD(a, b calendar.Date) calendar.Date {
	if a.After(b) {
		return a
	}
	return b
}

// seedFreeBerths returns up to n active berths without an active allocation in [from, to], from the last piers
// (codes in descending pier order, ascending number).
func seedFreeBerths(ctx context.Context, db bun.IDB, scope store.MarinaScope, from, to calendar.Date, n int) ([]store.Berth, error) {
	codes, err := store.RightsBerthCodes(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	busy := map[string]bool{}
	as, err := store.ListAllocations(ctx, db, scope, store.AllocationFilter{ResourceType: store.ResourceBerth, Status: store.AllocActive, From: from, To: to})
	if err != nil {
		return nil, err
	}
	for _, al := range as {
		busy[al.ResourceID] = true
	}
	var free []store.Berth
	for _, b := range codes {
		if b.Active && !busy[b.ID] && strings.Contains(b.Code, "-") {
			free = append(free, b)
		}
	}
	slices.SortFunc(free, func(x, y store.Berth) int {
		px, nx, _ := strings.Cut(x.Code, "-")
		py, ny, _ := strings.Cut(y.Code, "-")
		if px != py {
			return strings.Compare(py, px)
		}
		if len(nx) != len(ny) {
			return len(nx) - len(ny)
		}
		return strings.Compare(nx, ny)
	})
	if len(free) > n {
		free = free[:n]
	}
	return free, nil
}

// seedMillesimal creates and activates a table over berths (ids) with millis proportional to the berth areas,
// summing to exactly 1000 (money.Allocate, largest remainder).
func seedMillesimal(ctx context.Context, a *App, p *auth.Principal, m, name, code string, all []store.Berth, ids []string) (string, error) {
	area := map[string]int64{}
	for _, b := range all {
		area[b.ID] = b.LengthCm * b.WidthCm / 100
	}
	w := make([]int64, len(ids))
	for i, id := range ids {
		w[i] = max(area[id], 1)
	}
	parts, err := money.Allocate(1000, w)
	if err != nil {
		return "", err
	}
	t, err := a.Rights.CreateMillesimalTable(ctx, p, m, name, code)
	if err != nil {
		return "", fmt.Errorf("millesimal table: %w", err)
	}
	es := make([]rights.MillesimalEntry, len(ids))
	for i, id := range ids {
		es[i] = rights.MillesimalEntry{BerthID: id, Millis: int64(parts[i])}
	}
	if _, err := a.Rights.ReplaceMillesimalEntries(ctx, p, m, t.ID, t.Version, rights.EntriesInput{Entries: es}); err != nil {
		return "", fmt.Errorf("millesimal entries: %w", err)
	}
	cur, err := a.Rights.GetMillesimalTable(ctx, p, m, t.ID)
	if err != nil {
		return "", err
	}
	if _, err := a.Rights.ActivateMillesimalTable(ctx, p, m, t.ID, cur.Version); err != nil {
		return "", fmt.Errorf("millesimal activate: %w", err)
	}
	return t.ID, nil
}
