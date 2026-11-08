package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of the M10 yard quotes (m10-plan §4; owned by QUOTE). The quote row is locked (FOR UPDATE on
// PostgreSQL; SQLite has one writer) before every state change, then state and version are re-checked.

// GetYardQuote returns a yard quote of the marina; lock takes a row lock on PostgreSQL.
func GetYardQuote(ctx context.Context, db bun.IDB, s MarinaScope, id string, lock bool) (*YardQuote, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	row := new(YardQuote)
	q := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if lock && IsPostgres(db) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return row, nil
}

// InsertYardQuote inserts a quote (number and year already taken from the yard_quote series).
func InsertYardQuote(ctx context.Context, tx bun.IDB, s MarinaScope, row *YardQuote) error {
	row.MarinaID = s.ID()
	if row.Version == 0 {
		row.Version = 1
	}
	row.CreatedAt, row.UpdatedAt, row.ExpiresAt = UTC(row.CreatedAt), UTC(row.UpdatedAt), UTC(row.ExpiresAt)
	_, err := tx.NewInsert().Model(row).Exec(ctx)
	return err
}

// UpdateYardQuote writes every mutable column of a locked quote and bumps its version (expected = the version
// read under the lock).
func UpdateYardQuote(ctx context.Context, tx bun.IDB, s MarinaScope, row *YardQuote, expected int, now time.Time) error {
	row.UpdatedAt = UTC(now)
	row.ExpiresAt = UTC(row.ExpiresAt)
	if !row.SentAt.IsZero() {
		row.SentAt = UTC(row.SentAt)
	}
	if !row.DecidedAt.IsZero() {
		row.DecidedAt = UTC(row.DecidedAt)
	}
	if err := yardVersionedUpdate(ctx, tx, s, row, "id", row.ID, expected,
		"status", "quoted_on", "valid_until", "expires_at", "billing_mode", "deposit_bp", "deposit_cents", "conditions", "notes",
		"planned_start_on", "planned_delivery_on", "job_inputs", "vessel_overrides", "snapshot", "net_cents", "vat_cents",
		"gross_cents", "deposit_amount_cents", "estimated_hours", "sent_at", "decided_at", "accepted_via", "decline_reason",
		"updated_at"); err != nil {
		return err
	}
	row.Version = expected + 1
	return nil
}

// YardQuoteFilter filters ListYardQuotes.
type YardQuoteFilter struct {
	Status      string
	Kind        string
	CustomerID  string
	VesselID    string
	WorkOrderID string
}

// ListYardQuotes returns a page of quotes, newest first (cursor = id, ids are time ordered).
func ListYardQuotes(ctx context.Context, db bun.IDB, s MarinaScope, f YardQuoteFilter, cursor string, limit int) (Page[YardQuote], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[YardQuote]{}, err
	}
	limit = ClampLimit(limit)
	var rows []YardQuote
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	for col, id := range map[string]string{"customer_id": f.CustomerID, "vessel_id": f.VesselID, "work_order_id": f.WorkOrderID} {
		if id == "" {
			continue
		}
		if !ValidID(id) {
			return Page[YardQuote]{Items: []YardQuote{}}, nil
		}
		q = q.Where("? = ?", bun.Ident(col), id)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[YardQuote]{}, err
	}
	return pageOf(rows, limit, func(r YardQuote) string { return r.ID }), nil
}

// YardQuoteLines returns the lines of a quote by seq.
func YardQuoteLines(ctx context.Context, db bun.IDB, s MarinaScope, quoteID string) ([]YardQuoteLine, error) {
	out := []YardQuoteLine{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("quote_id = ?", quoteID).OrderExpr("seq").Scan(ctx)
	return out, err
}

// YardQuoteLinesOf returns the lines of several quotes, by quote id.
func YardQuoteLinesOf(ctx context.Context, db bun.IDB, s MarinaScope, quoteIDs []string) (map[string][]YardQuoteLine, error) {
	out := map[string][]YardQuoteLine{}
	if len(quoteIDs) == 0 {
		return out, nil
	}
	var rows []YardQuoteLine
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("quote_id IN (?)", bun.List(quoteIDs)).
		OrderExpr("quote_id, seq").Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.QuoteID] = append(out[r.QuoteID], r)
	}
	return out, nil
}

// ReplaceYardQuoteLines deletes the lines of a quote and inserts the given ones.
func ReplaceYardQuoteLines(ctx context.Context, tx bun.IDB, s MarinaScope, quoteID string, lines []YardQuoteLine) error {
	if _, err := tx.NewDelete().Model((*YardQuoteLine)(nil)).Where("marina_id = ?", s.ID()).Where("quote_id = ?", quoteID).Exec(ctx); err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	for i := range lines {
		lines[i].MarinaID, lines[i].QuoteID = s.ID(), quoteID
		lines[i].CreatedAt, lines[i].UpdatedAt = UTC(lines[i].CreatedAt), UTC(lines[i].UpdatedAt)
	}
	_, err := tx.NewInsert().Model(&lines).Exec(ctx)
	return err
}

// YardQuoteRef is a quote to expire (any marina).
type YardQuoteRef struct {
	ID       string `bun:"id"`
	MarinaID string `bun:"marina_id"`
}

// ExpiringYardQuotes returns draft and sent quotes whose expires_at is not after now, oldest first (job
// yard.quotes_expire; cross-marina by design, each is then handled in its marina scope).
func ExpiringYardQuotes(ctx context.Context, db bun.IDB, now time.Time, limit int) ([]YardQuoteRef, error) {
	out := []YardQuoteRef{}
	err := db.NewSelect().TableExpr("yard_quotes").Column("id", "marina_id").
		Where("status IN (?)", bun.List([]string{YardQuoteDraft, YardQuoteSent})).Where("expires_at <= ?", UTC(now)).
		OrderExpr("expires_at, id").Limit(int64(limit)).Scan(ctx, &out)
	return out, err
}

// YardWorkOrderNumber returns the year and number of a work order of the marina (read by the yard quote documents
// and the link page of variation quotes; work_orders belongs to ORDERS, this is a read-only lookup).
func YardWorkOrderNumber(ctx context.Context, db bun.IDB, s MarinaScope, id string) (int, int64, error) {
	if !ValidID(id) {
		return 0, 0, ErrNotFound
	}
	var row struct {
		Year   int   `bun:"year"`
		Number int64 `bun:"number"`
	}
	err := db.NewSelect().TableExpr("work_orders").Column("year", "number").Where("marina_id = ?", s.ID()).Where("id = ?", id).
		Limit(1).Scan(ctx, &row)
	if err != nil {
		return 0, 0, notFound(err)
	}
	return row.Year, row.Number, nil
}
