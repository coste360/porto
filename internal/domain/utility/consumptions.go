package utility

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Consumption is a row of utility_consumptions.
type Consumption = store.UtilityConsumption

// ConsumptionView is a consumption with its meter and contract.
type ConsumptionView struct {
	Row       Consumption
	MeterCode string
	Kind      string
	Contract  *store.Contract
}

var consumptionStatuses = []string{
	store.ConsumptionPending, store.ConsumptionAnomaly, store.ConsumptionUnassigned, store.ConsumptionCharged, store.ConsumptionExcluded,
}

func (s *Service) consumptionViews(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []Consumption) ([]ConsumptionView, error) {
	meters := map[string]*store.UtilityMeter{}
	var cids []string
	for _, r := range rows {
		if meters[r.MeterID] == nil {
			m, err := store.GetUtilityMeter(ctx, db, scope, r.MeterID)
			if err != nil {
				return nil, err
			}
			meters[r.MeterID] = m
		}
		if r.ContractID != "" {
			cids = append(cids, r.ContractID)
		}
	}
	contracts := map[string]*store.Contract{}
	if len(cids) > 0 {
		cs, err := store.ContractsByIDs(ctx, db, scope, cids)
		if err != nil {
			return nil, err
		}
		for i := range cs {
			contracts[cs[i].ID] = &cs[i]
		}
	}
	out := make([]ConsumptionView, 0, len(rows))
	for _, r := range rows {
		m := meters[r.MeterID]
		out = append(out, ConsumptionView{Row: r, MeterCode: m.Code, Kind: m.Kind, Contract: contracts[r.ContractID]})
	}
	return out, nil
}

func (s *Service) consumptionView(ctx context.Context, scope store.MarinaScope, id string) (*ConsumptionView, error) {
	c, err := store.GetUtilityConsumption(ctx, s.d.DB.DB, scope, id)
	if err != nil {
		return nil, notFound(err)
	}
	vs, err := s.consumptionViews(ctx, s.d.DB.DB, scope, []Consumption{*c})
	if err != nil {
		return nil, err
	}
	return &vs[0], nil
}

// ListConsumptions lists consumptions (charges.manage).
func (s *Service) ListConsumptions(ctx context.Context, p *auth.Principal, marinaID string, f ConsumptionFilter) ([]ConsumptionView, string, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, "", err
	}
	v := &auth.ValidationError{}
	if f.Status != "" && !contains(consumptionStatuses, f.Status) {
		v.Add("status", "invalid")
	}
	if f.Kind != "" && unitOf[f.Kind] == "" {
		v.Add("kind", "invalid")
	}
	if err := v.Err(); err != nil {
		return nil, "", err
	}
	sf := store.UtilityConsumptionFilter{Status: f.Status, ContractID: f.ContractID, MeterID: f.MeterID, Kind: f.Kind}
	if f.From != nil {
		sf.From = *f.From
	}
	if f.To != nil {
		sf.To = *f.To
	}
	page, err := store.ListUtilityConsumptions(ctx, s.d.DB.DB, scope, sf, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	items, err := s.consumptionViews(ctx, s.d.DB.DB, scope, page.Items)
	return items, page.NextCursor, err
}

// updateConsumption locks the meter, checks the version and the status, applies fn and writes the row.
func (s *Service) updateConsumption(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, action string,
	fn func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *Consumption) error,
) (*ConsumptionView, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesManage)
	if err != nil {
		return nil, err
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.GetUtilityConsumption(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		if _, err := store.LockUtilityMeter(ctx, tx, scope, c.MeterID); err != nil {
			return err
		}
		if c, err = store.GetUtilityConsumption(ctx, tx, scope, id); err != nil {
			return err
		}
		if int64(c.Version) != version {
			return ErrVersionMismatch
		}
		if c.Status == store.ConsumptionCharged || c.Status == store.ConsumptionExcluded {
			return ErrConsumptionFrozen
		}
		if err := fn(ctx, tx, scope, c); err != nil {
			return err
		}
		if err := store.UpdateUtilityConsumption(ctx, tx, scope, c, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), action, "utility_consumption", c.ID,
			map[string]any{"status": c.Status, "contract_id": c.ContractID}); err != nil {
			return err
		}
		return s.changed(ctx, tx, scope, "consumption", c.ID, c.MeterID, c.Status)
	})
	if err != nil {
		return nil, err
	}
	return s.consumptionView(ctx, scope, id)
}

// ExcludeConsumption excludes a consumption from charging (charges.manage, If-Match). Its interval is frozen.
func (s *Service) ExcludeConsumption(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, reason string) (*ConsumptionView, error) {
	return s.updateConsumption(ctx, p, marinaID, id, version, "utility_consumption.exclude",
		func(_ context.Context, _ bun.IDB, _ store.MarinaScope, c *Consumption) error {
			reason, ok := cleanText(reason, maxNoteRunes)
			if !ok || reason == "" {
				v := &auth.ValidationError{}
				v.Add("reason", "invalid")
				return v
			}
			c.Status, c.ExcludeReason = store.ConsumptionExcluded, reason
			return nil
		})
}

// AssignConsumption assigns an unassigned consumption to a contract, which then charges it (charges.manage,
// If-Match; D20, D29).
func (s *Service) AssignConsumption(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, contractID string) (*ConsumptionView, error) {
	return s.updateConsumption(ctx, p, marinaID, id, version, "utility_consumption.assign",
		func(ctx context.Context, tx bun.IDB, scope store.MarinaScope, c *Consumption) error {
			if c.Status != store.ConsumptionUnassigned {
				return ErrInvalidTransition
			}
			ct, err := store.GetContract(ctx, tx, scope, contractID)
			if err != nil {
				return ErrElementInvalid
			}
			if ct.Status == "cancelled" || ct.Status == "draft" {
				return ErrElementInvalid
			}
			c.ContractID, c.Status = ct.ID, store.ConsumptionPending
			c.CustomerID = ct.BillingCustomerID
			if c.CustomerID == "" {
				c.CustomerID = ct.CustomerID
			}
			return nil
		})
}
