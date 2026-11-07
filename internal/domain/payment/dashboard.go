package payment

import (
	"context"
	"slices"
	"time"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Customer dashboard (spec 11.4; m5-plan §4.5): computed by read queries, no table of its own. The billing
// sections (arrears, balance, credit, future instalments, payments) are present only with charges.manage; work
// orders and rentals stay empty until M10/M11.

// DashboardExpiringDays is the horizon of expiring documents.
const DashboardExpiringDays = 60

// DashboardContract is a contract of the customer.
type DashboardContract struct {
	ID, Code, Kind, Status string
	StartsOn, EndsOn       calendar.Date
	Total                  money.Cents
}

// DashboardDocument is an expiring (or expired) document.
type DashboardDocument struct {
	ID, Type  string
	ExpiresOn calendar.Date
}

// DashboardMessage is a communication sent to the customer.
type DashboardMessage struct {
	ID, Template, Status string
	CreatedAt            time.Time
}

// DashboardVessel is a vessel of the customer.
type DashboardVessel struct{ ID, Name string }

// DashboardResource is a resource the customer occupies today.
type DashboardResource struct{ ResourceType, ResourceID string }

// Dashboard is the customer dashboard.
type Dashboard struct {
	CustomerID, CustomerName string
	Vessels                  []DashboardVessel
	Contracts                []DashboardContract
	BerthsToday              []DashboardResource
	Documents                []DashboardDocument
	Communications           []DashboardMessage
	Notes                    string
	Billing                  bool // the billing sections are present
	Arrears                  Ageing
	Balance, Credit          money.Cents
	FutureInstallments       []Receivable
	RecentPayments           []Detail
}

// CustomerDashboard returns the dashboard of a customer (customers.read; billing sections with charges.manage).
func (s *Service) CustomerDashboard(ctx context.Context, p *auth.Principal, marinaID, customerID string) (*Dashboard, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	c, err := store.GetCustomer(ctx, db, scope, customerID)
	if err != nil {
		return nil, err
	}
	names, err := s.customerNames(ctx, db, scope, p, []string{customerID})
	if err != nil {
		return nil, err
	}
	today := s.today(ctx, db, scope)
	d := &Dashboard{
		CustomerID: c.ID, CustomerName: names[c.ID], Notes: c.Notes, Vessels: []DashboardVessel{}, Contracts: []DashboardContract{},
		BerthsToday: []DashboardResource{}, Documents: []DashboardDocument{}, Communications: []DashboardMessage{},
	}
	parties, err := store.PartiesOfCustomers(ctx, db, scope, []string{c.ID})
	if err != nil {
		return nil, err
	}
	var vesselIDs []string
	for _, vp := range parties {
		if !slices.Contains(vesselIDs, vp.VesselID) {
			vesselIDs = append(vesselIDs, vp.VesselID)
		}
	}
	vessels, err := store.VesselsByIDs(ctx, db, scope, vesselIDs)
	if err != nil {
		return nil, err
	}
	for _, v := range vessels {
		d.Vessels = append(d.Vessels, DashboardVessel{ID: v.ID, Name: v.Name})
	}
	if can(p, scope, auth.PermContractsRead) {
		page, err := store.ListContracts(ctx, db, scope, store.ContractFilter{CustomerID: c.ID}, "", store.MaxLimit)
		if err != nil {
			return nil, err
		}
		var ids []string
		for _, k := range page.Items {
			d.Contracts = append(d.Contracts, DashboardContract{
				ID: k.ID, Code: contractCode(k.Kind, k.Year, k.Number), Kind: k.Kind, Status: k.Status, StartsOn: k.StartsOn,
				EndsOn: k.EndsOn, Total: money.Cents(k.TotalCents),
			})
			ids = append(ids, k.ID)
		}
		allocs, err := store.AllocationsOfContracts(ctx, db, scope, ids)
		if err != nil {
			return nil, err
		}
		for _, a := range allocs {
			if a.Status == store.AllocActive && !a.FirstNight.After(today) && !a.LastNight.Before(today) {
				d.BerthsToday = append(d.BerthsToday, DashboardResource{ResourceType: a.ResourceType, ResourceID: a.ResourceID})
			}
		}
	}
	docs, err := store.DocumentsOfCustomer(ctx, db, scope, c.ID)
	if err != nil {
		return nil, err
	}
	for _, vid := range vesselIDs {
		vd, err := store.DocumentsOfVessel(ctx, db, scope, vid)
		if err != nil {
			return nil, err
		}
		docs = append(docs, vd...)
	}
	horizon := today.AddDays(DashboardExpiringDays)
	for _, doc := range docs {
		if !doc.ExpiresOn.IsZero() && !doc.ExpiresOn.After(horizon) {
			d.Documents = append(d.Documents, DashboardDocument{ID: doc.ID, Type: doc.Type, ExpiresOn: doc.ExpiresOn})
		}
	}
	slices.SortFunc(d.Documents, func(a, b DashboardDocument) int { return a.ExpiresOn.Compare(b.ExpiresOn) })
	msgs, err := store.ListCustomerMessages(ctx, db, scope, c.ID, "", 20)
	if err != nil {
		return nil, err
	}
	for _, m := range msgs.Items {
		d.Communications = append(d.Communications, DashboardMessage{ID: m.ID, Template: m.Template, Status: m.Status, CreatedAt: m.CreatedAt})
	}
	if !can(p, scope, auth.PermChargesManage) {
		return d, nil
	}
	d.Billing = true
	open, err := store.OpenPositiveCharges(ctx, db, scope, store.OpenChargesFilter{CustomerID: c.ID})
	if err != nil {
		return nil, err
	}
	for _, ch := range open {
		d.Arrears.Add(BucketOf(ch.DueOn, today), money.Cents(ch.ResidualCents()))
	}
	if d.Balance, d.Credit, err = BalanceTx(ctx, db, scope, c.ID); err != nil {
		return nil, err
	}
	future, _, err := s.receivables(ctx, db, scope, p, ReceivableFilter{CustomerID: c.ID, Status: "scheduled"})
	if err != nil {
		return nil, err
	}
	d.FutureInstallments = future
	pays, err := store.ListPayments(ctx, db, scope, store.PaymentFilter{CustomerID: c.ID}, "", 10)
	if err != nil {
		return nil, err
	}
	if d.RecentPayments, err = s.details(ctx, db, scope, p, pays.Items, false); err != nil {
		return nil, err
	}
	return d, nil
}
