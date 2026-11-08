package store

import (
	"context"
	"maps"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// ReportContractRow is a contract or transit with the price class of its first resource line.
type ReportContractRow struct {
	ID         string        `bun:"id"`
	Kind       string        `bun:"kind"`
	Term       string        `bun:"term"`
	Status     string        `bun:"status"`
	Year       int           `bun:"year"`
	Number     int64         `bun:"number"`
	Channel    string        `bun:"channel"`
	CustomerID string        `bun:"customer_id"`
	VesselID   string        `bun:"vessel_id"`
	StartsOn   calendar.Date `bun:"starts_on"`
	EndsOn     calendar.Date `bun:"ends_on"`
	TotalCents int64         `bun:"total_cents"`
	Category   string        `bun:"category"`
}

// ReportContractFilter selects contracts by status and dates: Statuses maps a kind to its statuses (required);
// StartsBy keeps starts_on ≤ StartsBy, EndsAfter keeps ends_on > EndsAfter (zero = no bound).
type ReportContractFilter struct {
	Statuses  map[string][]string
	StartsBy  calendar.Date
	EndsAfter calendar.Date
}

// ReportContracts returns the contracts of the filter, oldest first.
func ReportContracts(ctx context.Context, db bun.IDB, s MarinaScope, f ReportContractFilter) ([]ReportContractRow, error) {
	out := []ReportContractRow{}
	if len(f.Statuses) == 0 {
		return out, nil
	}
	q := db.NewSelect().TableExpr("contracts AS c").
		ColumnExpr("c.id, c.kind, COALESCE(c.term, '') AS term, c.status, c.year, c.number, c.channel").
		ColumnExpr("c.customer_id AS customer_id, c.vessel_id, c.starts_on, c.ends_on, c.total_cents").
		ColumnExpr(`COALESCE((SELECT COALESCE(b.price_class, da.price_class, lr.price_class) FROM contract_lines AS cl
			LEFT JOIN berths AS b ON cl.resource_type = 'berth' AND b.id = cl.resource_id
			LEFT JOIN dynamic_areas AS da ON cl.resource_type = 'dynamic_area' AND da.id = cl.resource_id
			LEFT JOIN land_resources AS lr ON cl.resource_type = 'land_resource' AND lr.id = cl.resource_id
			WHERE cl.contract_id = c.id AND cl.line_kind = 'resource' ORDER BY cl.seq LIMIT 1), '') AS category`).
		Where("c.marina_id = ?", s.ID()).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			kinds := slices.Sorted(maps.Keys(f.Statuses))
			for _, kind := range kinds {
				q = q.WhereOr("(c.kind = ? AND c.status IN (?))", kind, bun.List(f.Statuses[kind]))
			}
			return q
		})
	if !f.StartsBy.IsZero() {
		q = q.Where("c.starts_on <= ?", f.StartsBy)
	}
	if !f.EndsAfter.IsZero() {
		q = q.Where("c.ends_on > ?", f.EndsAfter)
	}
	err := q.OrderExpr("c.starts_on, c.kind, c.year, c.number").Scan(ctx, &out)
	return out, err
}

// ReportRenewalRow links a renewed contract to its new contract (proposals with a new contract).
type ReportRenewalRow struct {
	SourceContractID string `bun:"source_contract_id"`
	NewContractID    string `bun:"new_contract_id"`
}

// ReportRenewals returns the renewal proposals that produced a new contract.
func ReportRenewals(ctx context.Context, db bun.IDB, s MarinaScope) ([]ReportRenewalRow, error) {
	out := []ReportRenewalRow{}
	err := db.NewSelect().TableExpr("renewal_proposals").ColumnExpr("source_contract_id, new_contract_id").
		Where("marina_id = ?", s.ID()).Where("new_contract_id IS NOT NULL").OrderExpr("source_contract_id, id").Scan(ctx, &out)
	return out, err
}

// LostOutcomes are the inquiry outcomes of the lost demand (§3.4).
var LostOutcomes = []string{"declined_no_space", "declined_size", "declined_other", "lost"}

// ReportInquiries returns the unserved inquiries with at least one night in the period.
func ReportInquiries(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]Inquiry, error) {
	out := []Inquiry{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("outcome IN (?)", bun.List(LostOutcomes)).
		Where("arrival <= ?", f.To).Where("departure > ?", f.From).OrderExpr("arrival, id").Scan(ctx)
	return out, err
}

// ReportPresenceRow is an arrival or departure with its vessel, contract and berth labels.
type ReportPresenceRow struct {
	ID             string        `bun:"id"`
	VesselID       string        `bun:"vessel_id"`
	VesselName     string        `bun:"vessel_name"`
	Event          string        `bun:"event"`
	Night          calendar.Date `bun:"night"`
	ContractID     string        `bun:"contract_id"`
	ContractKind   string        `bun:"contract_kind"`
	ContractYear   int           `bun:"contract_year"`
	ContractNumber int64         `bun:"contract_number"`
	BerthCode      string        `bun:"berth_code"`
}

// ReportPresences returns the arrivals and departures with night in [From, To], in time order.
func ReportPresences(ctx context.Context, db bun.IDB, s MarinaScope, f ReportFilter) ([]ReportPresenceRow, error) {
	out := []ReportPresenceRow{}
	err := db.NewSelect().TableExpr("presences AS p").
		ColumnExpr("p.id, p.vessel_id, COALESCE(v.name, '') AS vessel_name, p.event, p.night").
		ColumnExpr("p.contract_id AS contract_id, COALESCE(c.kind, '') AS contract_kind").
		ColumnExpr("COALESCE(c.year, 0) AS contract_year, COALESCE(c.number, 0) AS contract_number").
		ColumnExpr("COALESCE(b.code, COALESCE(da.code, COALESCE(lr.code, ''))) AS berth_code").
		Join("LEFT JOIN vessels AS v ON v.id = p.vessel_id").
		Join("LEFT JOIN contracts AS c ON c.id = p.contract_id").
		Join("LEFT JOIN allocations AS a ON a.id = p.allocation_id").
		Join("LEFT JOIN berths AS b ON a.resource_type = 'berth' AND b.id = a.resource_id").
		Join("LEFT JOIN dynamic_areas AS da ON a.resource_type = 'dynamic_area' AND da.id = a.resource_id").
		Join("LEFT JOIN land_resources AS lr ON a.resource_type = 'land_resource' AND lr.id = a.resource_id").
		Where("p.marina_id = ?", s.ID()).Where("p.event IN (?)", bun.List([]string{"arrived", "departed"})).
		Where("p.night >= ?", f.From).Where("p.night <= ?", f.To).
		OrderExpr("p.at, p.id").Scan(ctx, &out)
	return out, err
}

// ReportLastPresencesBefore returns, for each of the given vessels, its last arrival or departure with night
// before the given date (latest night, then time, then id): the state the vessel was in when a period starts,
// however long ago the event happened (R13 absence nights).
func ReportLastPresencesBefore(ctx context.Context, db bun.IDB, s MarinaScope, vesselIDs []string, before calendar.Date) ([]ReportPresenceRow, error) {
	out := []ReportPresenceRow{}
	if len(vesselIDs) == 0 {
		return out, nil
	}
	events := bun.List([]string{"arrived", "departed"})
	err := db.NewSelect().TableExpr("presences AS p").
		ColumnExpr("p.id, p.vessel_id, COALESCE(v.name, '') AS vessel_name, p.event, p.night").
		ColumnExpr("p.contract_id AS contract_id, COALESCE(c.kind, '') AS contract_kind").
		ColumnExpr("COALESCE(c.year, 0) AS contract_year, COALESCE(c.number, 0) AS contract_number").
		ColumnExpr("'' AS berth_code").
		Join("LEFT JOIN vessels AS v ON v.id = p.vessel_id").
		Join("LEFT JOIN contracts AS c ON c.id = p.contract_id").
		Where("p.marina_id = ?", s.ID()).Where("p.event IN (?)", events).
		Where("p.vessel_id IN (?)", bun.List(vesselIDs)).Where("p.night < ?", before).
		Where(`NOT EXISTS (SELECT 1 FROM presences AS q WHERE q.marina_id = p.marina_id AND q.vessel_id = p.vessel_id
			AND q.event IN (?) AND q.night < ? AND (q.night > p.night OR (q.night = p.night AND (q.at > p.at
			OR (q.at = p.at AND q.id > p.id)))))`, events, before).
		OrderExpr("p.vessel_id").Scan(ctx, &out)
	return out, err
}

// ReportVesselRow is the name of a vessel.
type ReportVesselRow struct {
	ID   string `bun:"id"`
	Name string `bun:"name"`
}

// ReportVessels returns the names of the given vessels.
func ReportVessels(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]ReportVesselRow, error) {
	out := []ReportVesselRow{}
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("vessels").ColumnExpr("id, name").Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(ids)).OrderExpr("id").Scan(ctx, &out)
	return out, err
}

// ReportCustomerRow is the profile of a customer used by the reports (names for the display name, no contacts).
type ReportCustomerRow struct {
	ID           string `bun:"id"`
	Kind         string `bun:"kind"`
	FirstName    string `bun:"first_name"`
	LastName     string `bun:"last_name"`
	CompanyName  string `bun:"company_name"`
	Nationality  string `bun:"nationality"`
	Category     string `bun:"category"`
	IsAnonymized bool   `bun:"is_anonymized"`
}

// ReportCustomers returns the profiles of the given customers.
func ReportCustomers(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]ReportCustomerRow, error) {
	out := []ReportCustomerRow{}
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().TableExpr("customers").
		ColumnExpr("id, kind, first_name, last_name, company_name, COALESCE(nationality, '') AS nationality").
		ColumnExpr("COALESCE(category, '') AS category, (anonymized_at IS NOT NULL) AS is_anonymized").
		Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).OrderExpr("id").Scan(ctx, &out)
	return out, err
}
