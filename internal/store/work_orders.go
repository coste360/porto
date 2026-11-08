package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of work orders, budget lines and variations (M10, m10-plan §5). Owned by ORDERS. One order per accepted
// job quote (work_orders_quote_uidx) and one variation per accepted variation quote (work_order_variations_quote_uidx),
// INV-96.

// WorkOrderOpenStatuses are the statuses of an order that still takes work.
var WorkOrderOpenStatuses = []string{WorkOrderPlanned, WorkOrderInProgress, WorkOrderOnHold, WorkOrderCompleted}

func normaliseWorkOrder(o *WorkOrder) {
	if len(o.RateSnapshot) == 0 {
		o.RateSnapshot = JSON("{}")
	}
	o.StartedAt, o.CompletedAt, o.DeliveredAt = UTC(o.StartedAt), UTC(o.CompletedAt), UTC(o.DeliveredAt)
	o.InvoicedAt, o.CancelledAt = UTC(o.InvoicedAt), UTC(o.CancelledAt)
	o.CreatedAt, o.UpdatedAt = UTC(o.CreatedAt), UTC(o.UpdatedAt)
}

// InsertWorkOrder inserts an order of the marina (a second order of the same quote is a unique violation).
func InsertWorkOrder(ctx context.Context, tx bun.IDB, s MarinaScope, o *WorkOrder) error {
	o.MarinaID = s.ID()
	if o.Version == 0 {
		o.Version = 1
	}
	normaliseWorkOrder(o)
	_, err := tx.NewInsert().Model(o).Exec(ctx)
	return err
}

// GetWorkOrder returns an order of the marina.
func GetWorkOrder(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*WorkOrder, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	o := new(WorkOrder)
	if err := db.NewSelect().Model(o).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return o, nil
}

// LockWorkOrder reads an order FOR UPDATE on PostgreSQL (SQLite write transactions are serialised).
func LockWorkOrder(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*WorkOrder, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	o := new(WorkOrder)
	q := tx.NewSelect().Model(o).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return o, nil
}

// WorkOrderByQuote returns the order created by a quote, ErrNotFound when none.
func WorkOrderByQuote(ctx context.Context, db bun.IDB, s MarinaScope, quoteID string) (*WorkOrder, error) {
	if !ValidID(quoteID) {
		return nil, ErrNotFound
	}
	o := new(WorkOrder)
	if err := db.NewSelect().Model(o).Where("marina_id = ?", s.ID()).Where("quote_id = ?", quoteID).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return o, nil
}

// UpdateWorkOrder writes the given columns (plus updated_at) when the version equals expected and bumps it; the
// order must be locked by the caller. A version mismatch is ErrVersionMismatch, a missing row ErrNotFound.
func UpdateWorkOrder(ctx context.Context, tx bun.IDB, s MarinaScope, o *WorkOrder, expected int, now time.Time, cols ...string) error {
	o.UpdatedAt = UTC(now)
	normaliseWorkOrder(o)
	res, err := tx.NewUpdate().Model(o).Column(append(cols, "updated_at")...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", o.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetWorkOrder(ctx, tx, s, o.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	o.Version = expected + 1
	return nil
}

// WorkOrderFilter filters the order list.
type WorkOrderFilter struct {
	Statuses   []string
	CustomerID string
	VesselID   string
	Q          string // number (C2026-000034 or 34) or vessel name prefix
}

// ListWorkOrders returns a page of orders, newest first (id descending: UUIDv7).
func ListWorkOrders(ctx context.Context, db bun.IDB, s MarinaScope, f WorkOrderFilter, cursor string, limit int) (Page[WorkOrder], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[WorkOrder]{}, err
	}
	limit = ClampLimit(limit)
	var rows []WorkOrder
	q := db.NewSelect().Model(&rows).Where("work_order.marina_id = ?", s.ID())
	if len(f.Statuses) > 0 {
		q = q.Where("work_order.status IN (?)", bun.List(f.Statuses))
	}
	if f.CustomerID != "" {
		if !ValidID(f.CustomerID) {
			return Page[WorkOrder]{Items: []WorkOrder{}}, nil
		}
		q = q.Where("work_order.customer_id = ?", f.CustomerID)
	}
	if f.VesselID != "" {
		if !ValidID(f.VesselID) {
			return Page[WorkOrder]{Items: []WorkOrder{}}, nil
		}
		q = q.Where("work_order.vessel_id = ?", f.VesselID)
	}
	if qs := strings.TrimSpace(f.Q); qs != "" {
		num := strings.TrimLeft(qs[strings.LastIndex(qs, "-")+1:], "0")
		q = q.WhereGroup(" AND ", func(g *bun.SelectQuery) *bun.SelectQuery {
			g = g.Where("EXISTS (SELECT 1 FROM vessels AS v WHERE v.id = work_order.vessel_id AND LOWER(v.name) LIKE ?)", likePrefix(strings.ToLower(qs)))
			if num != "" && strings.Trim(num, "0123456789") == "" && len(num) <= 9 {
				g = g.WhereOr("CAST(work_order.number AS TEXT) = ?", num)
			}
			return g
		})
	}
	if after != "" {
		q = q.Where("work_order.id < ?", after)
	}
	if err := q.OrderExpr("work_order.id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[WorkOrder]{}, err
	}
	return pageOf(rows, limit, func(o WorkOrder) string { return o.ID }), nil
}

// WorkOrdersByStatus returns the orders of the marina in the statuses (board), by planned delivery then number.
func WorkOrdersByStatus(ctx context.Context, db bun.IDB, s MarinaScope, statuses []string) ([]WorkOrder, error) {
	out := []WorkOrder{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status IN (?)", bun.List(statuses)).
		OrderExpr("planned_delivery_on IS NULL, planned_delivery_on, year, number").Scan(ctx)
	return out, err
}

// WorkOrdersByIDs returns orders of the marina by id.
func WorkOrdersByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]WorkOrder, error) {
	out := []WorkOrder{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx)
	return out, err
}

// ---- budget lines ----

// InsertWorkOrderLines inserts budget lines of an order.
func InsertWorkOrderLines(ctx context.Context, tx bun.IDB, s MarinaScope, lines []WorkOrderLine) error {
	if len(lines) == 0 {
		return nil
	}
	for i := range lines {
		lines[i].MarinaID = s.ID()
		if lines[i].Version == 0 {
			lines[i].Version = 1
		}
		lines[i].CreatedAt, lines[i].UpdatedAt = UTC(lines[i].CreatedAt), UTC(lines[i].UpdatedAt)
	}
	_, err := tx.NewInsert().Model(&lines).Exec(ctx)
	return err
}

// WorkOrderLines returns the budget lines of orders ordered by order and seq.
func WorkOrderLines(ctx context.Context, db bun.IDB, s MarinaScope, orderIDs []string) ([]WorkOrderLine, error) {
	out := []WorkOrderLine{}
	ids := validIDs(orderIDs)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("work_order_id IN (?)", bun.List(ids)).
		OrderExpr("work_order_id, seq").Scan(ctx)
	return out, err
}

// MaxWorkOrderLineSeq returns the highest budget line seq of an order (0 when none).
func MaxWorkOrderLineSeq(ctx context.Context, db bun.IDB, s MarinaScope, orderID string) (int, error) {
	var n int
	err := db.NewSelect().TableExpr("work_order_lines").ColumnExpr("COALESCE(MAX(seq), 0)").Where("marina_id = ?", s.ID()).
		Where("work_order_id = ?", orderID).Scan(ctx, &n)
	return n, err
}

// SetWorkOrderLineConsumed sets the consumed quantity of a budget line of the order (nil = the budget quantity).
func SetWorkOrderLineConsumed(ctx context.Context, tx bun.IDB, s MarinaScope, orderID, lineID string, consumed *int64, now time.Time) error {
	if !ValidID(lineID) {
		return ErrNotFound
	}
	res, err := tx.NewUpdate().TableExpr("work_order_lines").Set("consumed_qty_milli = ?", consumed).Set("updated_at = ?", UTC(now)).
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("work_order_id = ?", orderID).Where("id = ?", lineID).Exec(ctx)
	return expectOne(res, err)
}

// ---- variations ----

// InsertWorkOrderVariation inserts the variation of an accepted variation quote (unique per quote).
func InsertWorkOrderVariation(ctx context.Context, tx bun.IDB, s MarinaScope, v *WorkOrderVariation) error {
	v.MarinaID = s.ID()
	v.AcceptedAt, v.CreatedAt = UTC(v.AcceptedAt), UTC(v.CreatedAt)
	_, err := tx.NewInsert().Model(v).Exec(ctx)
	return err
}

// ---- reads of other tables the order side needs ----

// WOVesselInfo is what an order card shows of its vessel (name is not personal data).
type WOVesselInfo struct {
	ID    string `bun:"id"`
	Name  string `bun:"name"`
	LOACm *int64 `bun:"loa_cm"`
}

// WOVessels returns name and LOA of vessels of the marina.
func WOVessels(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]WOVesselInfo, error) {
	out := map[string]WOVesselInfo{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []WOVesselInfo
	if err := db.NewSelect().TableExpr("vessels").Column("id", "name", "loa_cm").Where("marina_id = ?", s.ID()).
		Where("id IN (?)", bun.List(ids)).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// WOCustomerVesselExist reports whether the customer and the vessel belong to the marina.
func WOCustomerVesselExist(ctx context.Context, db bun.IDB, s MarinaScope, customerID, vesselID string) (bool, error) {
	if !ValidID(customerID) || !ValidID(vesselID) {
		return false, nil
	}
	n, err := db.NewSelect().TableExpr("customers").Where("marina_id = ?", s.ID()).Where("id = ?", customerID).Count(ctx)
	if err != nil || n == 0 {
		return false, err
	}
	n, err = db.NewSelect().TableExpr("vessels").Where("marina_id = ?", s.ID()).Where("id = ?", vesselID).Count(ctx)
	return n > 0, err
}

// WOAllocationOfVessel reports whether an allocation of the marina holds the vessel (the order position).
func WOAllocationOfVessel(ctx context.Context, db bun.IDB, s MarinaScope, allocationID, vesselID string) (bool, error) {
	if !ValidID(allocationID) {
		return false, nil
	}
	n, err := db.NewSelect().TableExpr("allocations").Where("marina_id = ?", s.ID()).Where("id = ?", allocationID).
		Where("vessel_id = ?", vesselID).Count(ctx)
	return n > 0, err
}

// WOAllocationLabel is the resource an allocation sits on (the bay of the order).
type WOAllocationLabel struct {
	ID           string `bun:"id"`
	ResourceType string `bun:"resource_type"`
	ResourceID   string `bun:"resource_id"`
	Code         string `bun:"code"`
}

// WOAllocationLabels returns the resource and its code of allocations (berth, dynamic area or land resource code).
func WOAllocationLabels(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]WOAllocationLabel, error) {
	out := map[string]WOAllocationLabel{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []WOAllocationLabel
	if err := db.NewSelect().TableExpr("allocations AS a").ColumnExpr("a.id, a.resource_type, a.resource_id").
		ColumnExpr(`COALESCE(b.code, da.code, lr.code, '') AS code`).
		Join("LEFT JOIN berths AS b ON a.resource_type = 'berth' AND b.id = a.resource_id").
		Join("LEFT JOIN dynamic_areas AS da ON a.resource_type = 'dynamic_area' AND da.id = a.resource_id").
		Join("LEFT JOIN land_resources AS lr ON a.resource_type = 'land_resource' AND lr.id = a.resource_id").
		Where("a.marina_id = ?", s.ID()).Where("a.id IN (?)", bun.List(ids)).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// WOWorkTypesExist reports whether every non-empty work type id belongs to the marina.
func WOWorkTypesExist(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (bool, error) {
	return woRowsExist(ctx, db, s, "work_types", ids)
}

// WOQualificationsExist reports whether every non-empty qualification id belongs to the marina.
func WOQualificationsExist(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (bool, error) {
	return woRowsExist(ctx, db, s, "yard_qualifications", ids)
}

// WOUserInMarina reports whether a user is an active member of the scope's marina (a marina-level grant or an
// organization-wide grant of its organization): the responsible of an order (INV-73).
func WOUserInMarina(ctx context.Context, db bun.IDB, s MarinaScope, userID string) (bool, error) {
	_, err := MarinaMemberUser(ctx, db, s, userID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func woRowsExist(ctx context.Context, db bun.IDB, s MarinaScope, table string, ids []string) (bool, error) {
	seen := map[string]bool{}
	var list []string
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		if !ValidID(id) {
			return false, nil
		}
		seen[id] = true
		list = append(list, id)
	}
	if len(list) == 0 {
		return true, nil
	}
	n, err := db.NewSelect().TableExpr(table).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(list)).Count(ctx)
	return int(n) == len(list), err
}

// WOActiveQualifications returns the active qualifications of the marina (labour items of the rate snapshot).
func WOActiveQualifications(ctx context.Context, db bun.IDB, s MarinaScope) ([]YardQualification, error) {
	out := []YardQualification{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("active = ?", true).OrderExpr("sort_order, code").Scan(ctx)
	return out, err
}

// WOQualifications returns qualifications of the marina by id.
func WOQualifications(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]YardQualification, error) {
	out := []YardQualification{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx)
	return out, err
}

// WOItemCosts returns the yard cost of items (yard_item_settings.cost_cents; missing = 0).
func WOItemCosts(ctx context.Context, db bun.IDB, s MarinaScope, itemIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	ids := validIDs(itemIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []YardItemSettings
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("item_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ItemID] = r.CostCents
	}
	return out, nil
}

// WORevenueClassCodes returns the code of revenue classes by id.
func WORevenueClassCodes(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]string, error) {
	out := map[string]string{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []RevenueClass
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Code
	}
	return out, nil
}

// WOOrderHours is a sum of hours of an order.
type WOOrderHours struct {
	WorkOrderID string `bun:"work_order_id"`
	Hours       int64  `bun:"hours"`
}

// WOApprovedHours returns Σ approved, non-voided hours per order.
func WOApprovedHours(ctx context.Context, db bun.IDB, s MarinaScope, orderIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	ids := validIDs(orderIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []WOOrderHours
	if err := db.NewSelect().TableExpr("timesheets").ColumnExpr("work_order_id, SUM(hours_milli) AS hours").
		Where("marina_id = ?", s.ID()).Where("status = ?", TimesheetApproved).Where("work_order_id IN (?)", bun.List(ids)).
		GroupExpr("work_order_id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkOrderID] = r.Hours
	}
	return out, nil
}

// WOEstimatedHours returns Σ estimated hours of the non-cancelled tasks per order.
func WOEstimatedHours(ctx context.Context, db bun.IDB, s MarinaScope, orderIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	ids := validIDs(orderIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []WOOrderHours
	if err := db.NewSelect().TableExpr("work_tasks").ColumnExpr("work_order_id, SUM(estimated_hours_milli) AS hours").
		Where("marina_id = ?", s.ID()).Where("status <> ?", WorkTaskCancelled).Where("work_order_id IN (?)", bun.List(ids)).
		GroupExpr("work_order_id").Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkOrderID] = r.Hours
	}
	return out, nil
}

// WOCatalogRules returns the active base rules of an item on a day (the order of ActiveBaseRulesOfItem); the
// caller decodes the first unit price or amount.
func WOCatalogRules(ctx context.Context, db bun.IDB, s MarinaScope, itemID string, day calendar.Date) ([]PriceRule, error) {
	return ActiveBaseRulesOfItem(ctx, db, s, itemID, day)
}
