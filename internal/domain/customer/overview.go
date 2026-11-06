package customer

import (
	"context"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// VesselLink is the relation of a customer with one vessel: the roles it holds and its owner share.
type VesselLink struct {
	VesselID    string
	Roles       []string
	ShareMillis int64 // owner share, 0 when not an owner
}

// PartyRow is a stored party link (GDPR export).
type PartyRow struct {
	VesselID, CustomerID, Role string
	ShareMillis                int64
}

// Overview is the customer part of the customer page (m1-plan §4.4 getCustomerOverview). Vessels, documents
// and deadlines come from their own services.
type Overview struct {
	Customer   Customer
	Vessels    []VesselLink
	Consents   []Consent // current consent per kind
	Duplicates []Summary
}

func vesselLinks(parties []store.VesselParty) []VesselLink {
	out := []VesselLink{}
	for _, pr := range parties {
		i := slices.IndexFunc(out, func(l VesselLink) bool { return l.VesselID == pr.VesselID })
		if i < 0 {
			out = append(out, VesselLink{VesselID: pr.VesselID, Roles: []string{}})
			i = len(out) - 1
		}
		out[i].Roles = append(out[i].Roles, pr.Role)
		if pr.Role == "owner" {
			out[i].ShareMillis = pr.ShareMillis
		}
	}
	return out
}

// Overview returns the customer, its vessel links, current consents and suspected duplicates
// (customers.read).
func (s *Service) Overview(ctx context.Context, p *auth.Principal, marinaID, id string) (*Overview, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	var out *Overview
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetCustomer(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		parties, err := store.PartiesOfCustomers(ctx, tx, scope, []string{row.ID})
		if err != nil {
			return err
		}
		history, err := store.ListConsents(ctx, tx, scope, row.ID)
		if err != nil {
			return err
		}
		consents, err := toConsents(ctx, tx, history)
		if err != nil {
			return err
		}
		dups, err := s.duplicates(ctx, tx, scope, row)
		if err != nil {
			return err
		}
		out = &Overview{Customer: s.toCustomer(row), Vessels: vesselLinks(parties), Consents: CurrentConsents(consents), Duplicates: dups}
		return nil
	})
	return out, err
}

// Export is the customer part of the GDPR export (format porto-customer/1): the customer, the full consent
// history and the party links. Vessels, documents and deadlines are added by their services.
type Export struct {
	Customer Customer
	Consents []Consent
	Parties  []PartyRow
}

// Export returns the customer's data for a GDPR export and records the export in the audit log
// (customers.read).
func (s *Service) Export(ctx context.Context, p *auth.Principal, marinaID, id string) (*Export, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersRead)
	if err != nil {
		return nil, err
	}
	var out *Export
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		row, err := store.GetCustomer(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		parties, err := store.PartiesOfCustomers(ctx, tx, scope, []string{row.ID})
		if err != nil {
			return err
		}
		history, err := store.ListConsents(ctx, tx, scope, row.ID)
		if err != nil {
			return err
		}
		consents, err := toConsents(ctx, tx, history)
		if err != nil {
			return err
		}
		rows := make([]PartyRow, 0, len(parties))
		for _, pr := range parties {
			rows = append(rows, PartyRow{VesselID: pr.VesselID, CustomerID: pr.CustomerID, Role: pr.Role, ShareMillis: pr.ShareMillis})
		}
		if err := s.writeAudit(ctx, tx, scope, p, "customer.export", row.ID, nil); err != nil {
			return err
		}
		out = &Export{Customer: s.toCustomer(row), Consents: consents, Parties: rows}
		return nil
	})
	return out, err
}
