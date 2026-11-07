package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of renewal_campaigns and renewal_proposals (spec 13.8; m4-plan §3.4). One open proposal per source
// contract is renewal_proposals_open_uidx (INV-33): a second insert is ErrConflict.

// OpenProposalStatuses are the statuses under renewal_proposals_open_uidx.
var OpenProposalStatuses = []string{"previewed", "proposed", "sent", "accepted", "signed"}

// InsertRenewalProposal inserts a proposal; another open proposal of the same source (or the same source in the
// same campaign) is ErrConflict.
func InsertRenewalProposal(ctx context.Context, db bun.IDB, p *RenewalProposal) error {
	_, err := db.NewInsert().Model(p).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetRenewalProposal returns one proposal of a marina.
func GetRenewalProposal(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RenewalProposal, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	p := new(RenewalProposal)
	if err := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// LockRenewalProposal is GetRenewalProposal locked for update on PostgreSQL.
func LockRenewalProposal(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RenewalProposal, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	p := new(RenewalProposal)
	q := db.NewSelect().Model(p).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return p, nil
}

// UpdateRenewalProposal writes p when its stored version equals expected and bumps the version; another change
// in between is ErrVersionMismatch. A unique violation (an open proposal of the same source) is ErrConflict.
func UpdateRenewalProposal(ctx context.Context, db bun.IDB, s MarinaScope, expected int, p *RenewalProposal, now time.Time) error {
	p.UpdatedAt = UTC(now)
	res, err := db.NewUpdate().Model(p).ExcludeColumn("id", "marina_id", "created_at", "version").Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", p.ID).Where("version = ?", expected).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	p.Version = expected + 1
	return nil
}

// DeleteRenewalProposal deletes a proposal (draft campaign discard).
func DeleteRenewalProposal(ctx context.Context, db bun.IDB, s MarinaScope, id string) error {
	_, err := db.NewDelete().Model((*RenewalProposal)(nil)).Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return err
}

// OpenProposalOfSource returns the open proposal of a source contract (nil when none).
func OpenProposalOfSource(ctx context.Context, db bun.IDB, s MarinaScope, sourceID string) (*RenewalProposal, error) {
	return oneProposal(ctx, db.NewSelect().Where("marina_id = ?", s.ID()).Where("source_contract_id = ?", sourceID).
		Where("status IN (?)", bun.List(OpenProposalStatuses)))
}

// OpenProposalOfSuccessor returns the open proposal whose successor is contractID (nil when none).
func OpenProposalOfSuccessor(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (*RenewalProposal, error) {
	return oneProposal(ctx, db.NewSelect().Where("marina_id = ?", s.ID()).Where("new_contract_id = ?", contractID).
		Where("status IN (?)", bun.List(OpenProposalStatuses)))
}

// ProposalOfSuccessor returns the latest proposal whose successor is contractID, any status (nil when none).
func ProposalOfSuccessor(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (*RenewalProposal, error) {
	return oneProposal(ctx, db.NewSelect().Where("marina_id = ?", s.ID()).Where("new_contract_id = ?", contractID))
}

// SignedSuccessorOf returns the active (or later) successor contract of a source through a signed or completed
// proposal (nil when none): the expiry job marks the source renewed only then (INV-35).
func SignedSuccessorOf(ctx context.Context, db bun.IDB, s MarinaScope, sourceID string) (*Contract, error) {
	c := new(Contract)
	err := db.NewSelect().Model(c).Where("contract.marina_id = ?", s.ID()).
		Where("contract.status IN (?)", bun.List([]string{"active", "suspended", "renewed", "expired", "terminated"})).
		Where("EXISTS (SELECT 1 FROM renewal_proposals AS p WHERE p.new_contract_id = contract.id AND p.marina_id = contract.marina_id AND p.source_contract_id = ? AND p.status IN ('signed','completed'))", sourceID).
		Limit(1).Scan(ctx)
	if err != nil {
		if notFound(err) == ErrNotFound { //nolint:errorlint // notFound returns the sentinel itself
			return nil, nil
		}
		return nil, err
	}
	return c, nil
}

func oneProposal(ctx context.Context, q *bun.SelectQuery) (*RenewalProposal, error) {
	p := new(RenewalProposal)
	err := q.Model(p).OrderExpr("id DESC").Limit(1).Scan(ctx)
	if err != nil {
		if notFound(err) == ErrNotFound { //nolint:errorlint // notFound returns the sentinel itself
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}

// ProposalsOfCampaign returns the proposals of a campaign with the given statuses (all when empty), by id, at
// most limit (0 = all).
func ProposalsOfCampaign(ctx context.Context, db bun.IDB, s MarinaScope, campaignID string, statuses []string, limit int) ([]RenewalProposal, error) {
	out := []RenewalProposal{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("campaign_id = ?", campaignID)
	if len(statuses) > 0 {
		q = q.Where("status IN (?)", bun.List(statuses))
	}
	if limit > 0 {
		q = q.Limit(int64(limit))
	}
	err := q.OrderExpr("id").Scan(ctx)
	return out, err
}

// ListRenewalProposals returns a page of the proposals of a campaign, by id.
func ListRenewalProposals(ctx context.Context, db bun.IDB, s MarinaScope, campaignID, status, cursor string, limit int) (Page[RenewalProposal], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[RenewalProposal]{}, err
	}
	rows := []RenewalProposal{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("campaign_id = ?", campaignID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if after != "" {
		q = q.Where("id > ?", after)
	}
	if err := q.OrderExpr("id").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[RenewalProposal]{}, err
	}
	return pageOf(rows, limit, func(p RenewalProposal) string { return p.ID }), nil
}

// CountProposalsByStatus returns the number of proposals of a campaign per status.
func CountProposalsByStatus(ctx context.Context, db bun.IDB, s MarinaScope, campaignID string) (map[string]int, error) {
	var rows []struct {
		Status string `bun:"status"`
		N      int    `bun:"n"`
	}
	err := db.NewSelect().TableExpr("renewal_proposals").ColumnExpr("status, COUNT(*) AS n").
		Where("marina_id = ?", s.ID()).Where("campaign_id = ?", campaignID).GroupExpr("status").Scan(ctx, &rows)
	out := map[string]int{}
	for _, r := range rows {
		out[r.Status] = r.N
	}
	if err != nil || out["excluded"] == 0 {
		return out, err
	}
	// Exclusions per reason (excluded_<reason>), as the RenewalCounts schema documents.
	var reasons []struct {
		Reason string `bun:"exclusion_reason"`
		N      int    `bun:"n"`
	}
	err = db.NewSelect().TableExpr("renewal_proposals").ColumnExpr("exclusion_reason, COUNT(*) AS n").
		Where("marina_id = ?", s.ID()).Where("campaign_id = ?", campaignID).Where("status = ?", "excluded").
		Where("exclusion_reason IS NOT NULL").GroupExpr("exclusion_reason").Scan(ctx, &reasons)
	for _, r := range reasons {
		out["excluded_"+r.Reason] = r.N
	}
	return out, err
}

// ProposalsPastDeadline returns up to limit open proposals (before signature) whose deadline is before today.
func ProposalsPastDeadline(ctx context.Context, db bun.IDB, s MarinaScope, today calendar.Date, limit int) ([]RenewalProposal, error) {
	out := []RenewalProposal{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		Where("status IN (?)", bun.List([]string{"previewed", "proposed", "sent", "accepted"})).
		Where("deadline_on < ?", today).OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// ProposalsInStatus returns up to limit proposals of the marina in a status (any campaign), by id.
func ProposalsInStatus(ctx context.Context, db bun.IDB, s MarinaScope, status string, limit int) ([]RenewalProposal, error) {
	out := []RenewalProposal{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status = ?", status).
		OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// InsertRenewalCampaign inserts a campaign.
func InsertRenewalCampaign(ctx context.Context, db bun.IDB, c *RenewalCampaign) error {
	_, err := db.NewInsert().Model(c).Exec(ctx)
	return err
}

// GetRenewalCampaign returns one campaign of a marina.
func GetRenewalCampaign(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RenewalCampaign, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(RenewalCampaign)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// LockRenewalCampaign is GetRenewalCampaign locked for update on PostgreSQL.
func LockRenewalCampaign(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*RenewalCampaign, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(RenewalCampaign)
	q := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// UpdateRenewalCampaign writes c when its stored version equals expected and bumps the version.
func UpdateRenewalCampaign(ctx context.Context, db bun.IDB, s MarinaScope, expected int, c *RenewalCampaign, now time.Time) error {
	c.UpdatedAt = UTC(now)
	res, err := db.NewUpdate().Model(c).ExcludeColumn("id", "marina_id", "created_at", "version").Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrVersionMismatch
	}
	c.Version = expected + 1
	return nil
}

// ListRenewalCampaigns returns a page of campaigns, newest first.
func ListRenewalCampaigns(ctx context.Context, db bun.IDB, s MarinaScope, status, cursor string, limit int) (Page[RenewalCampaign], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[RenewalCampaign]{}, err
	}
	rows := []RenewalCampaign{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[RenewalCampaign]{}, err
	}
	return pageOf(rows, limit, func(c RenewalCampaign) string { return c.ID }), nil
}

// CampaignsInStatus returns the campaigns of a marina in the given statuses.
func CampaignsInStatus(ctx context.Context, db bun.IDB, s MarinaScope, statuses []string) ([]RenewalCampaign, error) {
	out := []RenewalCampaign{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status IN (?)", bun.List(statuses)).
		OrderExpr("id").Scan(ctx)
	return out, err
}

// CampaignCandidates returns the active mooring contracts of the campaign filter, by id.
type CampaignCandidates struct {
	Terms       []string
	EndsFrom    *calendar.Date // ends_on ≥
	EndsTo      *calendar.Date // ends_on ≤
	GroupIDs    []string       // a berth of the contract in one of the groups
	ContractIDs []string
	CustomerCat []string // customers.category
}

// ActiveContractsFor returns the active mooring contracts matching f (at most limit).
func ActiveContractsFor(ctx context.Context, db bun.IDB, s MarinaScope, f CampaignCandidates, limit int) ([]Contract, error) {
	out := []Contract{}
	q := db.NewSelect().Model(&out).Where("contract.marina_id = ?", s.ID()).Where("contract.kind = 'mooring'").
		Where("contract.status = 'active'")
	if len(f.Terms) > 0 {
		q = q.Where("contract.term IN (?)", bun.List(f.Terms))
	}
	if f.EndsFrom != nil {
		q = q.Where("contract.ends_on >= ?", *f.EndsFrom)
	}
	if f.EndsTo != nil {
		q = q.Where("contract.ends_on <= ?", *f.EndsTo)
	}
	if ids := validIDs(f.ContractIDs); len(f.ContractIDs) > 0 {
		if len(ids) == 0 {
			return out, nil
		}
		q = q.Where("contract.id IN (?)", bun.List(ids))
	}
	if ids := validIDs(f.GroupIDs); len(f.GroupIDs) > 0 {
		if len(ids) == 0 {
			return out, nil
		}
		q = q.Where(`EXISTS (SELECT 1 FROM contract_lines AS l
			JOIN berth_group_members AS g ON g.resource_id = l.resource_id AND g.marina_id = l.marina_id
			WHERE l.contract_id = contract.id AND l.marina_id = contract.marina_id AND g.group_id IN (?))`, bun.List(ids))
	}
	if len(f.CustomerCat) > 0 {
		q = q.Where("EXISTS (SELECT 1 FROM customers AS cu WHERE cu.id = contract.customer_id AND cu.marina_id = contract.marina_id AND cu.category IN (?))", bun.List(f.CustomerCat))
	}
	err := q.OrderExpr("contract.id").Limit(int64(limit)).Scan(ctx)
	return out, err
}
