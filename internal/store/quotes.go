package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Queries of quotes, quote_options and quote_option_placements (spec 13.2; m3-plan §4.3). Every function is
// scoped by the marina.

// QuoteFilter narrows ListQuotes.
type QuoteFilter struct {
	Status     string
	Kind       string
	CustomerID string
	Search     string // vessel name (folded)
	Number     int64  // exact number when the search is numeric
}

// InsertQuote inserts a quote; a number already taken is ErrConflict.
func InsertQuote(ctx context.Context, db bun.IDB, q *Quote) error {
	_, err := db.NewInsert().Model(q).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// GetQuote returns one quote of a marina.
func GetQuote(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Quote, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	q := new(Quote)
	if err := db.NewSelect().Model(q).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return q, nil
}

// LockQuote returns one quote locked for update on PostgreSQL (see LockContract).
func LockQuote(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Quote, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	q := new(Quote)
	sel := db.NewSelect().Model(q).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(db) {
		sel = sel.For("UPDATE")
	}
	if err := sel.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return q, nil
}

var quoteColumns = []string{
	"status", "term", "customer_id", "vessel_id", "starts_on", "ends_on", "valid_until", "expires_at", "channel",
	"policy", "notes", "inquiry_id", "accepted_option_id", "contract_id", "sent_at", "decided_at", "decline_reason",
	"updated_at",
}

// UpdateQuote writes the mutable columns when the stored version equals expected and bumps the version.
func UpdateQuote(ctx context.Context, db bun.IDB, s MarinaScope, expected int, q *Quote) error {
	res, err := db.NewUpdate().Model(q).Column(quoteColumns...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", q.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetQuote(ctx, db, s, q.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	q.Version = expected + 1
	return nil
}

// ListQuotes returns a page of quotes, newest first.
func ListQuotes(ctx context.Context, db bun.IDB, s MarinaScope, f QuoteFilter, cursor string, limit int) (Page[Quote], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Quote]{}, err
	}
	rows := []Quote{}
	q := db.NewSelect().Model(&rows).Where("quote.marina_id = ?", s.ID())
	if f.Status != "" {
		q = q.Where("quote.status = ?", f.Status)
	}
	if f.Kind != "" {
		q = q.Where("quote.kind = ?", f.Kind)
	}
	if f.CustomerID != "" {
		if !ValidID(f.CustomerID) {
			return Page[Quote]{Items: []Quote{}}, nil
		}
		q = q.Where("quote.customer_id = ?", f.CustomerID)
	}
	if f.Search != "" {
		sub := "EXISTS (SELECT 1 FROM vessels AS v WHERE v.id = quote.vessel_id AND v.marina_id = quote.marina_id AND v.search_text LIKE ? ESCAPE '!')"
		if f.Number > 0 {
			q = q.Where("("+sub+" OR quote.number = ?)", likePattern(f.Search), f.Number)
		} else {
			q = q.Where(sub, likePattern(f.Search))
		}
	}
	if after != "" {
		q = q.Where("quote.id < ?", after)
	}
	if err := q.OrderExpr("quote.id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Quote]{}, err
	}
	return pageOf(rows, limit, func(q Quote) string { return q.ID }), nil
}

// InsertQuoteOptions inserts options of a quote.
func InsertQuoteOptions(ctx context.Context, db bun.IDB, opts []QuoteOption) error {
	if len(opts) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&opts).Exec(ctx)
	return err
}

// InsertQuotePlacements inserts placements of quote options.
func InsertQuotePlacements(ctx context.Context, db bun.IDB, ps []QuoteOptionPlacement) error {
	if len(ps) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&ps).Exec(ctx)
	return err
}

// DeleteQuoteOptions deletes the options of a quote (their placements cascade).
func DeleteQuoteOptions(ctx context.Context, db bun.IDB, s MarinaScope, quoteID string) error {
	_, err := db.NewDelete().Model((*QuoteOption)(nil)).Where("marina_id = ?", s.ID()).Where("quote_id = ?", quoteID).Exec(ctx)
	return err
}

// OptionsOfQuotes returns the options of the given quotes, ordered by quote and seq.
func OptionsOfQuotes(ctx context.Context, db bun.IDB, s MarinaScope, quoteIDs []string) ([]QuoteOption, error) {
	out := []QuoteOption{}
	quoteIDs = validIDs(quoteIDs)
	if len(quoteIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("quote_id IN (?)", bun.List(quoteIDs)).
		OrderExpr("quote_id, seq").Scan(ctx)
	return out, err
}

// PlacementsOfOptions returns the placements of the given options, ordered by option and seq.
func PlacementsOfOptions(ctx context.Context, db bun.IDB, s MarinaScope, optionIDs []string) ([]QuoteOptionPlacement, error) {
	out := []QuoteOptionPlacement{}
	optionIDs = validIDs(optionIDs)
	if len(optionIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("option_id IN (?)", bun.List(optionIDs)).
		OrderExpr("option_id, seq").Scan(ctx)
	return out, err
}

// QuotesToExpire returns the draft and sent quotes of a marina past their expiry (at most limit).
func QuotesToExpire(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time, limit int) ([]Quote, error) {
	out := []Quote{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("status IN ('draft','sent')").
		Where("expires_at <= ?", UTC(now)).OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// QuoteOfAllocation returns the quote id whose option placement holds the allocation ("" when none).
func QuoteOfAllocation(ctx context.Context, db bun.IDB, s MarinaScope, allocationID string) (string, error) {
	if !ValidID(allocationID) {
		return "", nil
	}
	var ids []string
	err := db.NewRaw(`SELECT o.quote_id FROM quote_option_placements AS p JOIN quote_options AS o ON o.id = p.option_id
		WHERE p.marina_id = ? AND p.allocation_id = ?`, s.ID(), allocationID).Scan(ctx, &ids)
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

// RelinkQuotePlacement points the quote option placement holding allocation fromID to allocation to, copying its
// resource, position and nights (a moved quote hold stays the option's hold, m3-plan §3.5). It returns whether a
// placement was linked to fromID.
func RelinkQuotePlacement(ctx context.Context, db bun.IDB, s MarinaScope, fromID string, to *Allocation, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*QuoteOptionPlacement)(nil)).
		Set("allocation_id = ?", to.ID).Set("resource_type = ?", to.ResourceType).Set("resource_id = ?", to.ResourceID).
		Set("x_cm = ?", to.XCm).Set("y_cm = ?", to.YCm).Set("rotation_dd = ?", to.RotationDd).
		Set("first_night = ?", to.FirstNight).Set("last_night = ?", to.LastNight).Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("allocation_id = ?", fromID).Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
