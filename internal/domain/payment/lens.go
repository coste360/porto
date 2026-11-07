package payment

import (
	"context"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Payment lens of the operational map (spec 9.2; m5-plan §4.5, §16 D24): for every resource allocated to a
// contract or transit on a night, the worst payment state of that contract. States go to every map.view user
// (the dock is warned on departure, never blocked); amounts only with charges.manage.

// Lens states.
const (
	LensOK        = "ok"
	LensDueSoon   = "due_soon"
	LensOverdue   = "overdue"
	LensOverdue60 = "overdue_60"
	LensDisputed  = "disputed"
)

// DueSoonDays is the horizon of due_soon.
const DueSoonDays = 15

// LensResource is the lens state of one allocated resource.
type LensResource struct {
	ResourceType, ResourceID string
	ContractID               string
	State                    string
	Suspended                bool
	Residual                 *money.Cents // nil without charges.manage
}

// lensDue is an amount owed by a contract with its due date.
type lensDue struct {
	ChargeID string
	DueOn    calendar.Date
	Residual money.Cents
}

// LensState returns the state of a contract from what it owes (pure): disputed wins, then overdue by more than 60
// days, overdue, due within DueSoonDays, else ok.
func LensState(dues []lensDue, disputed bool, today calendar.Date) string {
	if disputed {
		return LensDisputed
	}
	state := LensOK
	rank := map[string]int{LensOK: 0, LensDueSoon: 1, LensOverdue: 2, LensOverdue60: 3}
	for _, d := range dues {
		if d.Residual <= 0 {
			continue
		}
		s := LensOK
		late := d.DueOn.DaysUntil(today)
		switch {
		case late > 60:
			s = LensOverdue60
		case late > 0:
			s = LensOverdue
		case -late <= DueSoonDays:
			s = LensDueSoon
		}
		if rank[s] > rank[state] {
			state = s
		}
	}
	return state
}

// MapPaymentStatus returns the payment lens of a map on a night (map.view; zero date = today).
func (s *Service) MapPaymentStatus(ctx context.Context, p *auth.Principal, marinaID, mapID string, night calendar.Date) (calendar.Date, []LensResource, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermMapView)
	if err != nil {
		return calendar.Date{}, nil, err
	}
	db := s.d.DB.DB
	if _, err := store.GetMap(ctx, db, scope, mapID); err != nil {
		return calendar.Date{}, nil, err
	}
	today := s.today(ctx, db, scope)
	if night.IsZero() {
		night = today
	}
	allocs, err := store.LensAllocations(ctx, db, scope, mapID, night)
	if err != nil {
		return calendar.Date{}, nil, err
	}
	ids := make([]string, 0, len(allocs))
	for _, a := range allocs {
		ids = append(ids, a.ContractID)
	}
	charges, err := store.OpenPositiveCharges(ctx, db, scope, store.OpenChargesFilter{ContractIDs: ids})
	if err != nil {
		return calendar.Date{}, nil, err
	}
	insts, err := store.IssuedInstallmentsWithoutCharge(ctx, db, scope, ids)
	if err != nil {
		return calendar.Date{}, nil, err
	}
	excl, err := store.ActiveExclusions(ctx, db, scope, today)
	if err != nil {
		return calendar.Date{}, nil, err
	}
	dues := map[string][]lensDue{}
	for _, c := range charges {
		dues[c.ContractID] = append(dues[c.ContractID], lensDue{ChargeID: c.ID, DueOn: c.DueOn, Residual: money.Cents(c.ResidualCents())})
	}
	for _, i := range insts {
		dues[i.ContractID] = append(dues[i.ContractID], lensDue{DueOn: i.DueOn, Residual: money.Cents(i.AmountCents)})
	}
	disputedCust, disputedCharge := map[string]bool{}, map[string]bool{}
	for _, e := range excl {
		if e.Kind != "disputed" {
			continue
		}
		if e.CustomerID != "" {
			disputedCust[e.CustomerID] = true
		}
		if e.ChargeID != "" {
			disputedCharge[e.ChargeID] = true
		}
	}
	amounts := can(p, scope, auth.PermChargesManage)
	out := make([]LensResource, 0, len(allocs))
	for _, a := range allocs {
		ds := dues[a.ContractID]
		disputed := disputedCust[a.CustomerID]
		var residual money.Cents
		for _, d := range ds {
			residual += d.Residual
			if d.ChargeID != "" && disputedCharge[d.ChargeID] {
				disputed = true
			}
		}
		r := LensResource{
			ResourceType: a.ResourceType, ResourceID: a.ResourceID, ContractID: a.ContractID,
			State: LensState(ds, disputed, today), Suspended: a.ContractStatus == "suspended",
		}
		if amounts {
			v := residual
			r.Residual = &v
		}
		out = append(out, r)
	}
	return night, out, nil
}
