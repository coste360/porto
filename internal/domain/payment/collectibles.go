package payment

import (
	"context"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Collectible is an open charge the dock can collect on a date (m6b-plan §5.4, D19). CustomerName is empty
// without customers.read.
type Collectible struct {
	ChargeID, Number, ContractID, ContractCode string
	VesselID, VesselName                       string
	CustomerID, CustomerName                   string
	Description                                string
	DueOn                                      calendar.Date
	Residual                                   money.Cents
	FiscalMode                                 string
}

// CollectiblePage is a page of collectibles.
type CollectiblePage struct {
	Items      []Collectible
	NextCursor string
}

// Collectibles lists the open charges of the contracts in port, arriving or departing on date (default today):
// payments.collect or payments.collect_cash. It is the narrow read of the dock, which cannot list charges.
func (s *Service) Collectibles(ctx context.Context, p *auth.Principal, marinaID string, date *calendar.Date, cursor string, limit int) (*CollectiblePage, error) {
	scope, _, err := authorizeAny(ctx, s.d.DB.DB, p, marinaID, auth.PermPaymentsCollect, auth.PermPaymentsCollectCash)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	night := s.today(ctx, db, scope)
	if date != nil {
		night = *date
	}
	page, err := store.CollectibleCharges(ctx, db, scope, night, cursor, limit)
	if err != nil {
		return nil, err
	}
	var cids, vids, custIDs []string
	for _, c := range page.Items {
		cids, vids, custIDs = append(cids, c.ContractID), append(vids, c.VesselID), append(custIDs, c.CustomerID)
	}
	contracts, err := store.ContractsByIDs(ctx, db, scope, cids)
	if err != nil {
		return nil, err
	}
	cm := map[string]*store.Contract{}
	for i := range contracts {
		cm[contracts[i].ID] = &contracts[i]
		vids = append(vids, contracts[i].VesselID)
	}
	vessels, err := store.VesselsByIDs(ctx, db, scope, vids)
	if err != nil {
		return nil, err
	}
	vm := map[string]string{}
	for i := range vessels {
		vm[vessels[i].ID] = vessels[i].Name
	}
	names, err := s.customerNames(ctx, db, scope, p, custIDs)
	if err != nil {
		return nil, err
	}
	out := &CollectiblePage{Items: make([]Collectible, 0, len(page.Items)), NextCursor: page.NextCursor}
	for i := range page.Items {
		c := &page.Items[i]
		it := Collectible{
			ChargeID: c.ID, Number: c.Number, ContractID: c.ContractID, VesselID: c.VesselID, CustomerID: c.CustomerID,
			CustomerName: names[c.CustomerID], Description: c.Description, DueOn: c.DueOn,
			Residual: money.Cents(c.ResidualCents()), FiscalMode: c.FiscalMode,
		}
		if k := cm[c.ContractID]; k != nil {
			it.ContractCode = contractCode(k.Kind, k.Year, k.Number)
			if it.VesselID == "" {
				it.VesselID = k.VesselID
			}
		}
		it.VesselName = vm[it.VesselID]
		out.Items = append(out.Items, it)
	}
	return out, nil
}
