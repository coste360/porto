package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of the "da fatturare" exports, their items and the invoice reconciliations (spec 16.5; m5-plan §3.6).
// INV-53: a charge leaves to_invoice only through MarkExported (conditional UPDATE inside the export transaction)
// and charge_export_items_first_uidx keeps one first item per charge.

// ExportSelection selects the to_invoice charges of an export.
type ExportSelection struct {
	PeriodFrom   calendar.Date
	PeriodTo     calendar.Date
	CustomerID   string
	OriginTypes  []string
	RevenueClass string
}

func (f ExportSelection) apply(q *bun.SelectQuery, s MarinaScope) *bun.SelectQuery {
	q = q.Where("c.marina_id = ?", s.ID()).Where("c.invoicing_status = ?", ChargeToInvoice).
		Where("c.payment_status <> ?", ChargeCancelled).
		Where("c.issued_on >= ?", f.PeriodFrom).Where("c.issued_on <= ?", f.PeriodTo)
	if f.CustomerID != "" {
		q = q.Where("c.customer_id = ?", f.CustomerID)
	}
	if len(f.OriginTypes) > 0 {
		q = q.Where("c.origin_type IN (?)", bun.List(f.OriginTypes))
	}
	if f.RevenueClass != "" {
		q = q.Where("EXISTS (SELECT 1 FROM charge_lines AS l WHERE l.charge_id = c.id AND l.revenue_class_code = ?)", f.RevenueClass)
	}
	return q
}

// CountExportable counts the charges of a selection.
func CountExportable(ctx context.Context, db bun.IDB, s MarinaScope, f ExportSelection) (int, error) {
	return toInt(f.apply(db.NewSelect().TableExpr("charges AS c"), s).Count(ctx))
}

// ExportableCharges returns up to limit charges of a selection ordered by number; with lock, the rows are locked
// (FOR UPDATE on PostgreSQL) in id order first so two exports never deadlock.
func ExportableCharges(ctx context.Context, db bun.IDB, s MarinaScope, f ExportSelection, limit int, lock bool) ([]Charge, error) {
	var out []Charge
	q := f.apply(db.NewSelect().Model(&out).ModelTableExpr("charges AS c").ColumnExpr("c.*"), s).OrderExpr("c.id").Limit(int64(limit))
	if lock && IsPostgres(db) {
		q = q.For("UPDATE OF c")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Charge{}
	}
	sortCharges(out)
	return out, nil
}

// MarkExported moves the given charges from to_invoice to exported for an export (conditional per row, in chunks
// of 500) and returns the ids actually updated: rows exported meanwhile by another export are skipped (INV-53).
func MarkExported(ctx context.Context, db bun.IDB, s MarinaScope, ids []string, exportID string, now time.Time) ([]string, error) {
	var done []string
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		var got []string
		err := db.NewRaw(
			"UPDATE charges SET invoicing_status = ?, first_export_id = ?, exported_at = ?, updated_at = ?, version = version + 1 "+
				"WHERE marina_id = ? AND id IN (?) AND invoicing_status = ? AND payment_status <> ? RETURNING id",
			ChargeExported, exportID, UTC(now), UTC(now), s.ID(), bun.List(ids[start:end]), ChargeToInvoice, ChargeCancelled,
		).Scan(ctx, &got)
		if err != nil {
			return nil, mapChargeErr(err)
		}
		done = append(done, got...)
	}
	return done, nil
}

// RefreshFiscalSnapshot replaces the fiscal snapshot of a charge (allowed on exported charges).
func RefreshFiscalSnapshot(ctx context.Context, db bun.IDB, s MarinaScope, id, enc string) error {
	_, err := db.NewUpdate().Model((*Charge)(nil)).Set("fiscal_snapshot_enc = ?", nullStr(enc)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return mapChargeErr(err)
}

// InsertChargeExport inserts an export with its items.
func InsertChargeExport(ctx context.Context, db bun.IDB, e *ChargeExport, items []ChargeExportItem) error {
	if _, err := db.NewInsert().Model(e).Exec(ctx); err != nil {
		return err
	}
	for start := 0; start < len(items); start += 500 {
		end := min(start+500, len(items))
		part := items[start:end]
		if _, err := db.NewInsert().Model(&part).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
	}
	return nil
}

// GetChargeExport returns an export of the marina.
func GetChargeExport(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*ChargeExport, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	e := new(ChargeExport)
	if err := db.NewSelect().Model(e).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return e, nil
}

// ListChargeExports returns a page of exports, newest first.
func ListChargeExports(ctx context.Context, db bun.IDB, s MarinaScope, cursor string, limit int) (Page[ChargeExport], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[ChargeExport]{}, err
	}
	var rows []ChargeExport
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[ChargeExport]{}, err
	}
	return pageOf(rows, limit, func(e ChargeExport) string { return e.ID }), nil
}

// ExportItemRow is an item of an export with its charge.
type ExportItemRow struct {
	ChargeID   string `bun:"charge_id"`
	Number     string `bun:"number"`
	IsFirst    bool   `bun:"is_first"`
	TotalCents int64  `bun:"total_cents"`
	CustomerID string `bun:"customer_id"`
	ItemID     string `bun:"item_id"`
}

// ExportItems returns a page of the items of an export ordered by charge number (cursor = item id).
func ExportItems(ctx context.Context, db bun.IDB, s MarinaScope, exportID, cursor string, limit int) (Page[ExportItemRow], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[ExportItemRow]{}, err
	}
	var rows []ExportItemRow
	q := db.NewSelect().TableExpr("charge_export_items AS i").Join("JOIN charges AS c ON c.id = i.charge_id").
		ColumnExpr("i.charge_id, c.number, i.is_first, c.total_cents, COALESCE(CAST(c.customer_id AS TEXT), '') AS customer_id, i.id AS item_id").
		Where("i.marina_id = ?", s.ID()).Where("i.export_id = ?", exportID)
	if after != "" {
		q = q.Where("c.number > (SELECT c2.number FROM charge_export_items AS i2 JOIN charges AS c2 ON c2.id = i2.charge_id WHERE i2.id = ?)", after)
	}
	if err := q.OrderExpr("c.number").Limit(int64(limit+1)).Scan(ctx, &rows); err != nil {
		return Page[ExportItemRow]{}, err
	}
	return pageOf(rows, limit, func(r ExportItemRow) string { return r.ItemID }), nil
}

// ExportChargeIDs returns the charge ids of an export.
func ExportChargeIDs(ctx context.Context, db bun.IDB, s MarinaScope, exportID string) ([]string, error) {
	var ids []string
	err := db.NewSelect().Model((*ChargeExportItem)(nil)).Column("charge_id").Where("marina_id = ?", s.ID()).
		Where("export_id = ?", exportID).OrderExpr("charge_id").Scan(ctx, &ids)
	return ids, err
}

// ExportItemSnapshots returns the frozen row snapshots of an export by charge id (rows without one are absent).
func ExportItemSnapshots(ctx context.Context, db bun.IDB, s MarinaScope, exportID string) (map[string]string, error) {
	var rows []ChargeExportItem
	if err := db.NewSelect().Model(&rows).Column("charge_id", "row_snapshot_enc").Where("marina_id = ?", s.ID()).
		Where("export_id = ?", exportID).Where("row_snapshot_enc IS NOT NULL").Scan(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.ChargeID] = r.RowSnapshotEnc
	}
	return out, nil
}

// SetChargeExportFiles records the files of an export (built after commit, or lazily).
func SetChargeExportFiles(ctx context.Context, db bun.IDB, s MarinaScope, e *ChargeExport, now time.Time) error {
	_, err := db.NewUpdate().Model(e).Column("csv_file_id", "xlsx_file_id", "pdf_file_id", "csv_sha256", "xlsx_sha256", "pdf_sha256").
		Set("updated_at = ?", UTC(now)).Where("marina_id = ?", s.ID()).Where("id = ?", e.ID).Exec(ctx)
	return err
}

// SetChargeExportDelivery records the delivery status of an exporter export.
func SetChargeExportDelivery(ctx context.Context, db bun.IDB, s MarinaScope, id, status, errCode, ref string, now time.Time) error {
	_, err := db.NewUpdate().Model((*ChargeExport)(nil)).Set("delivery_status = ?", status).
		Set("delivery_error = ?", nullStr(errCode)).Set("delivery_ref = ?", nullStr(ref)).
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return err
}

// ExporterExports returns the exporter exports of the marina in a delivery status (oldest first).
func ExporterExports(ctx context.Context, db bun.IDB, s MarinaScope, status string, limit int) ([]ChargeExport, error) {
	out := []ChargeExport{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("delivery = 'exporter'").
		Where("delivery_status = ?", status).OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// InsertInvoiceReconciliation inserts a reconciliation report.
func InsertInvoiceReconciliation(ctx context.Context, db bun.IDB, r *InvoiceReconciliation) error {
	_, err := db.NewInsert().Model(r).Exec(ctx)
	return err
}

// ListInvoiceReconciliations returns a page of reconciliations, newest first.
func ListInvoiceReconciliations(ctx context.Context, db bun.IDB, s MarinaScope, cursor string, limit int) (Page[InvoiceReconciliation], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[InvoiceReconciliation]{}, err
	}
	var rows []InvoiceReconciliation
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[InvoiceReconciliation]{}, err
	}
	return pageOf(rows, limit, func(r InvoiceReconciliation) string { return r.ID }), nil
}

// FiscalDocumentNumbers returns the number of the issued commercial document of each charge (receipt mode).
func FiscalDocumentNumbers(ctx context.Context, db bun.IDB, s MarinaScope, chargeIDs []string) (map[string]string, error) {
	out := map[string]string{}
	ids := validIDs(chargeIDs)
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		var rows []struct {
			ChargeID string `bun:"charge_id"`
			Number   string `bun:"number"`
		}
		if err := db.NewSelect().Table("fiscal_documents").ColumnExpr("charge_id, number").Where("marina_id = ?", s.ID()).
			Where("charge_id IN (?)", bun.List(ids[start:end])).Where("status = 'issued'").OrderExpr("number").
			Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if out[r.ChargeID] == "" {
				out[r.ChargeID] = r.Number
			}
		}
	}
	return out, nil
}

// InstallmentSeqs returns the seq of instalments and holder instalments by id.
func InstallmentSeqs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]int, error) {
	out := map[string]int{}
	ids = validIDs(ids)
	for _, table := range []string{"installments", "holder_installments"} {
		for start := 0; start < len(ids); start += 500 {
			end := min(start+500, len(ids))
			var rows []struct {
				ID  string `bun:"id"`
				Seq int    `bun:"seq"`
			}
			if err := db.NewSelect().Table(table).ColumnExpr("id, seq").Where("marina_id = ?", s.ID()).
				Where("id IN (?)", bun.List(ids[start:end])).Scan(ctx, &rows); err != nil {
				return nil, err
			}
			for _, r := range rows {
				out[r.ID] = r.Seq
			}
		}
	}
	return out, nil
}

// PaymentExportRow is a payment or refund of the payments export (m5-plan §3.6).
type PaymentExportRow struct {
	Kind         string        `bun:"kind"` // payment, refund
	ID           string        `bun:"id"`
	Number       string        `bun:"number"`
	PaymentKind  string        `bun:"payment_kind"`
	Method       string        `bun:"method"`
	Source       string        `bun:"source"`
	ProviderCode string        `bun:"provider_code"`
	ProviderRef  string        `bun:"provider_ref"`
	Day          calendar.Date `bun:"day"`
	AmountCents  int64         `bun:"amount_cents"`
	FeeCents     int64         `bun:"fee_cents"`
	CustomerID   string        `bun:"customer_id"`
	Status       string        `bun:"status"`
}

// PaymentsForExport returns the payments (not credits) and the refunds of a period, by day and number.
func PaymentsForExport(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date, method string, limit int) ([]PaymentExportRow, error) {
	var pays []PaymentExportRow
	q := db.NewSelect().Table("payments").
		ColumnExpr("'payment' AS kind, id, number, kind AS payment_kind, method, source, COALESCE(provider_code, '') AS provider_code, "+
			"COALESCE(provider_ref, '') AS provider_ref, received_on AS day, amount_cents, fee_cents, COALESCE(CAST(customer_id AS TEXT), '') AS customer_id, '' AS status").
		Where("marina_id = ?", s.ID()).Where("kind <> 'credit'").Where("received_on >= ?", from).Where("received_on <= ?", to)
	if method != "" {
		q = q.Where("method = ?", method)
	}
	if err := q.OrderExpr("received_on, number").Limit(int64(limit)).Scan(ctx, &pays); err != nil {
		return nil, err
	}
	var refs []PaymentExportRow
	q = db.NewSelect().Table("refunds").
		ColumnExpr("'refund' AS kind, id, number, kind AS payment_kind, method, source, COALESCE(provider_code, '') AS provider_code, "+
			"COALESCE(provider_ref, '') AS provider_ref, business_date AS day, amount_cents, 0 AS fee_cents, COALESCE(CAST(customer_id AS TEXT), '') AS customer_id, status").
		Where("marina_id = ?", s.ID()).Where("status <> 'failed'").Where("business_date >= ?", from).Where("business_date <= ?", to)
	if method != "" {
		q = q.Where("method = ?", method)
	}
	if err := q.OrderExpr("business_date, number").Limit(int64(limit)).Scan(ctx, &refs); err != nil {
		return nil, err
	}
	return append(pays, refs...), nil
}

// MatchedChargeNumbers returns, per payment, the numbers of the charges it is matched to (net of reversals).
func MatchedChargeNumbers(ctx context.Context, db bun.IDB, s MarinaScope, paymentIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	ids := validIDs(paymentIDs)
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		var rows []struct {
			PaymentID string `bun:"payment_id"`
			Number    string `bun:"number"`
		}
		if err := db.NewSelect().TableExpr("payment_matches AS m").Join("JOIN charges AS c ON c.id = m.charge_id").
			ColumnExpr("m.payment_id, c.number").Where("m.marina_id = ?", s.ID()).Where("m.payment_id IN (?)", bun.List(ids[start:end])).
			GroupExpr("m.payment_id, c.number").Having("SUM(m.amount_cents) <> 0").OrderExpr("m.payment_id, c.number").
			Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.PaymentID] = append(out[r.PaymentID], r.Number)
		}
	}
	return out, nil
}
