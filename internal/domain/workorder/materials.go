package workorder

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/workorder/account"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Material issues and returns (stock_movements kinds issue and return only, no stock levels in M10, D13) and
// external costs of an order (m10-plan §8).

// materialTarget locks the order of a movement or cost: it must take work; after the final billing only yard.manage
// may record and the row is late.
func materialTarget(ctx context.Context, tx bun.IDB, scope store.MarinaScope, perm auth.Permission, orderID string) (*store.WorkOrder, bool, error) {
	o, err := store.LockWorkOrder(ctx, tx, scope, orderID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, invalid("work_order_id", "invalid")
		}
		return nil, false, err
	}
	switch {
	case Open(o.Status):
		return o, false, nil
	case o.Status == store.WorkOrderInvoiced && perm == auth.PermYardManage:
		return o, true, nil
	}
	return nil, false, ErrClosed
}

// CreateMovement issues or returns material on an order with frozen cost and price (yard.manage; yard.timesheets on
// an own assigned task when yard_settings.workers_issue_materials). A return never exceeds the net issued quantity
// of the item on the order (422 return_exceeds_issued), checked under the order lock.
func (s *Service) CreateMovement(ctx context.Context, p *auth.Principal, marinaID string, in MovementInput) (*MovementView, error) {
	scope, perm, own, err := s.workerScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if in.Kind != store.StockIssue && in.Kind != store.StockReturn {
		return nil, invalid("kind", "invalid")
	}
	if in.Qty <= 0 {
		return nil, invalid("qty_milli", "invalid")
	}
	if (in.UnitCost != nil && *in.UnitCost < 0) || (in.UnitPrice != nil && *in.UnitPrice < 0) {
		return nil, invalid("unit_price_cents", "negative")
	}
	note, ok := cleanText(in.Note, maxNoteRunes)
	if !ok {
		return nil, invalid("note", "too_long")
	}
	if perm == auth.PermYardTimesheets {
		set, err := s.settings(ctx, s.d.DB.DB, scope)
		if err != nil {
			return nil, err
		}
		if !set.WorkersIssueMaterials || in.TaskID == "" {
			return nil, auth.ErrForbidden
		}
		if err := s.assignedTask(ctx, s.d.DB.DB, scope, perm, own, in.TaskID); err != nil {
			return nil, err
		}
	}
	var out store.StockMovement
	var code string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		o, late, err := materialTarget(ctx, tx, scope, perm, in.WorkOrderID)
		if err != nil {
			return err
		}
		if in.TaskID != "" {
			t, err := store.GetWorkTask(ctx, tx, scope, in.TaskID)
			if err != nil || t.WorkOrderID != o.ID {
				return invalid("task_id", "invalid")
			}
		}
		it, err := store.GetItem(ctx, tx, scope, in.ItemID)
		if err != nil || !account.IsMaterial(it.ItemType) {
			return invalid("item_id", "invalid")
		}
		code = it.Code
		if in.Kind == store.StockReturn {
			net, err := store.NetIssuedQty(ctx, tx, scope, o.ID, it.ID)
			if err != nil {
				return err
			}
			if int64(in.Qty) > net {
				return ErrReturnExceedsIssued
			}
		}
		day := in.MovedOn
		if day.IsZero() {
			day = s.today(ctx, tx, scope)
		}
		var unitCost money.Cents
		if in.UnitCost != nil {
			unitCost = *in.UnitCost
		} else {
			costs, err := store.WOItemCosts(ctx, tx, scope, []string{it.ID})
			if err != nil {
				return err
			}
			unitCost = money.Cents(costs[it.ID])
		}
		var unitPrice money.Cents
		overridden := in.UnitPrice != nil
		if overridden {
			unitPrice = *in.UnitPrice
		} else {
			r := rateSnapshotOf(o.RateSnapshot)
			p, ok := r.Units[it.ID]
			if !ok {
				if p, ok, err = s.catalogPrice(ctx, tx, scope, it.ID, day); err != nil {
					return err
				}
			}
			if !ok {
				return ErrNoPrice
			}
			unitPrice = p
		}
		out = store.StockMovement{
			ID: store.NewID(), WorkOrderID: o.ID, TaskID: in.TaskID, ItemID: it.ID, Kind: in.Kind, QtyMilli: int64(in.Qty),
			UnitCostCents: int64(unitCost), UnitPriceCents: int64(unitPrice), PriceOverridden: overridden, MovedOn: day,
			Note: note, Late: late, CreatedByUserID: p.UserID, CreatedAt: s.now(),
		}
		if err := store.InsertStockMovement(ctx, tx, scope, &out); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "stock_movement.create", "stock_movement", out.ID, map[string]any{
			"work_order_id": o.ID, "item_id": it.ID, "kind": in.Kind, "qty_milli": out.QtyMilli, "unit_cost_cents": out.UnitCostCents,
			"unit_price_cents": out.UnitPriceCents, "price_overridden": overridden, "late": late,
		}); err != nil {
			return err
		}
		return s.orderChanged(ctx, tx, scope, o.ID, "stock_movement", out.ID)
	})
	if err != nil {
		return nil, err
	}
	return &MovementView{Row: out, ItemCode: code}, nil
}

// ListMovements lists material movements, newest first (yard.read or yard.manage).
func (s *Service) ListMovements(ctx context.Context, p *auth.Principal, marinaID string, f MovementFilter) ([]MovementView, string, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, "", err
	}
	page, err := store.ListStockMovements(ctx, s.d.DB.DB, scope, store.StockMovementFilter{
		WorkOrderID: f.WorkOrderID, ItemID: f.ItemID, From: f.From, To: f.To,
	}, f.Cursor, f.Limit)
	if err != nil {
		return nil, "", err
	}
	var ids []string
	for _, m := range page.Items {
		ids = append(ids, m.ItemID)
	}
	items, err := store.ItemsByIDs(ctx, s.d.DB.DB, scope, ids)
	if err != nil {
		return nil, "", err
	}
	codes := map[string]string{}
	for _, it := range items {
		codes[it.ID] = it.Code
	}
	out := make([]MovementView, 0, len(page.Items))
	for _, m := range page.Items {
		out = append(out, MovementView{Row: m, ItemCode: codes[m.ItemID]})
	}
	return out, page.NextCursor, nil
}

// ---- external costs ----

// validateExternal checks and completes an external cost: price default cost × (10000 + markup) / 10000
// half-even, item default from the settings (required when billable).
func (s *Service) validateExternal(ctx context.Context, db bun.IDB, scope store.MarinaScope, c *store.WorkOrderExternalCost, priceGiven bool) error {
	v := &auth.ValidationError{}
	var ok bool
	if c.SupplierName, ok = cleanText(c.SupplierName, 100); !ok || c.SupplierName == "" {
		v.Add("supplier_name", "invalid")
	}
	if c.Reference, ok = cleanText(c.Reference, 60); !ok {
		v.Add("reference", "too_long")
	}
	if c.Description, ok = cleanText(c.Description, maxNoteRunes); !ok {
		v.Add("description", "too_long")
	}
	if c.CostCents <= 0 {
		v.Add("cost_cents", "invalid")
	}
	if c.PriceCents < 0 {
		v.Add("price_cents", "negative")
	}
	if err := v.Err(); err != nil {
		return err
	}
	set, err := s.settings(ctx, db, scope)
	if err != nil {
		return err
	}
	if !priceGiven {
		c.PriceCents = int64(money.Cents(c.CostCents).MulRat(10000+set.ExternalMarkupBP, 10000))
	}
	if c.ItemID == "" {
		c.ItemID = set.ExternalItemID
	}
	if c.Billable && c.ItemID == "" {
		return invalid("item_id", "required")
	}
	if c.ItemID != "" {
		if _, err := store.GetItem(ctx, db, scope, c.ItemID); err != nil {
			return invalid("item_id", "invalid")
		}
	}
	if ok, err := store.WOWorkTypesExist(ctx, db, scope, []string{c.WorkTypeID}); err != nil {
		return err
	} else if !ok {
		return invalid("work_type_id", "invalid")
	}
	return nil
}

// CreateExternal adds an external cost to an order (yard.manage).
func (s *Service) CreateExternal(ctx context.Context, p *auth.Principal, marinaID, orderID string, in ExternalInput) (*store.WorkOrderExternalCost, error) {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetWorkOrder(ctx, s.d.DB.DB, scope, orderID); err != nil {
		return nil, notFound(err)
	}
	c := &store.WorkOrderExternalCost{
		ID: store.NewID(), WorkOrderID: orderID, SupplierName: in.SupplierName, Reference: in.Reference, IncurredOn: in.IncurredOn,
		Description: in.Description, WorkTypeID: in.WorkTypeID, CostCents: int64(in.Cost), Billable: in.Billable == nil || *in.Billable,
		ItemID: in.ItemID, CreatedByUserID: p.UserID,
	}
	if in.Price != nil {
		c.PriceCents = int64(*in.Price)
	}
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, late, err := materialTarget(ctx, tx, scope, auth.PermYardManage, orderID)
		if err != nil {
			return err
		}
		if c.IncurredOn.IsZero() {
			c.IncurredOn = s.today(ctx, tx, scope)
		}
		if err := s.validateExternal(ctx, tx, scope, c, in.Price != nil); err != nil {
			return err
		}
		c.Late = late
		now := s.now()
		c.CreatedAt, c.UpdatedAt = now, now
		if err := store.InsertExternalCost(ctx, tx, scope, c); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "external_cost.create", "external_cost", c.ID, map[string]any{
			"work_order_id": orderID, "cost_cents": c.CostCents, "price_cents": c.PriceCents, "billable": c.Billable, "late": late,
		}); err != nil {
			return err
		}
		return s.orderChanged(ctx, tx, scope, orderID, "external_cost", c.ID)
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListExternal lists the external costs of an order (yard.read or yard.manage).
func (s *Service) ListExternal(ctx context.Context, p *auth.Principal, marinaID, orderID string) ([]store.WorkOrderExternalCost, error) {
	scope, err := s.readScope(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetWorkOrder(ctx, s.d.DB.DB, scope, orderID); err != nil {
		return nil, notFound(err)
	}
	return store.ExternalCostsOfOrders(ctx, s.d.DB.DB, scope, []string{orderID})
}

// externalChange locks the order of a cost and refuses changes once it is invoiced or cancelled (the cost is
// billed: corrections are new late costs billed as a conguaglio).
func (s *Service) externalChange(ctx context.Context, p *auth.Principal, marinaID, id string, version int64,
	fn func(tx bun.Tx, scope store.MarinaScope, c *store.WorkOrderExternalCost) error,
) error {
	scope, err := s.manageScope(ctx, p, marinaID)
	if err != nil {
		return err
	}
	return s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := store.GetExternalCost(ctx, tx, scope, id)
		if err != nil {
			return notFound(err)
		}
		o, err := store.LockWorkOrder(ctx, tx, scope, c.WorkOrderID)
		if err != nil {
			return notFound(err)
		}
		if int64(c.Version) != version {
			return ErrVersionMismatch
		}
		if !Open(o.Status) {
			return ErrClosed
		}
		return fn(tx, scope, c)
	})
}

// UpdateExternal changes an external cost of an open order (yard.manage, If-Match).
func (s *Service) UpdateExternal(ctx context.Context, p *auth.Principal, marinaID, id string, version int64, patch ExternalPatch) (*store.WorkOrderExternalCost, error) {
	var out *store.WorkOrderExternalCost
	err := s.externalChange(ctx, p, marinaID, id, version, func(tx bun.Tx, scope store.MarinaScope, c *store.WorkOrderExternalCost) error {
		if patch.SupplierName != nil {
			c.SupplierName = *patch.SupplierName
		}
		if patch.Reference != nil {
			c.Reference = *patch.Reference
		}
		if patch.IncurredOn != nil {
			c.IncurredOn = *patch.IncurredOn
		}
		if patch.Description != nil {
			c.Description = *patch.Description
		}
		if patch.WorkTypeID != nil {
			c.WorkTypeID = *patch.WorkTypeID
		}
		if patch.Cost != nil {
			c.CostCents = int64(*patch.Cost)
		}
		if patch.Billable != nil {
			c.Billable = *patch.Billable
		}
		if patch.ItemID != nil {
			c.ItemID = *patch.ItemID
		}
		priceGiven := true
		if patch.Price != nil {
			c.PriceCents = int64(*patch.Price)
		} else if patch.Cost != nil {
			priceGiven = false // the price follows the new cost with the markup
		}
		if err := s.validateExternal(ctx, tx, scope, c, priceGiven); err != nil {
			return err
		}
		if err := store.UpdateExternalCost(ctx, tx, scope, c, int(version), s.now()); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "external_cost.update", "external_cost", c.ID, map[string]any{
			"cost_cents": c.CostCents, "price_cents": c.PriceCents, "billable": c.Billable,
		}); err != nil {
			return err
		}
		out = c
		return s.orderChanged(ctx, tx, scope, c.WorkOrderID, "external_cost", c.ID)
	})
	return out, err
}

// DeleteExternal deletes an external cost of an open order (yard.manage, If-Match).
func (s *Service) DeleteExternal(ctx context.Context, p *auth.Principal, marinaID, id string, version int64) error {
	return s.externalChange(ctx, p, marinaID, id, version, func(tx bun.Tx, scope store.MarinaScope, c *store.WorkOrderExternalCost) error {
		if err := store.DeleteExternalCost(ctx, tx, scope, id, int(version)); err != nil {
			return versionErr(err)
		}
		if err := s.audit(ctx, tx, scope, p.Actor(), "external_cost.delete", "external_cost", id, map[string]any{"work_order_id": c.WorkOrderID}); err != nil {
			return err
		}
		return s.orderChanged(ctx, tx, scope, c.WorkOrderID, "external_cost", id)
	})
}
