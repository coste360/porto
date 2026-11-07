package store

import (
	"context"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of inquiries (spec 13.2). Every function is scoped by the marina.

// InquiryFilter narrows ListInquiries (arrival between From and To, inclusive).
type InquiryFilter struct {
	Outcome  string
	From, To *calendar.Date
}

// InsertInquiry inserts an inquiry.
func InsertInquiry(ctx context.Context, db bun.IDB, in *Inquiry) error {
	_, err := db.NewInsert().Model(in).Exec(ctx)
	return err
}

// GetInquiry returns one inquiry of a marina.
func GetInquiry(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Inquiry, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	in := new(Inquiry)
	if err := db.NewSelect().Model(in).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return in, nil
}

// UpdateInquiry writes outcome, links and notes when the stored version equals expected and bumps the version.
func UpdateInquiry(ctx context.Context, db bun.IDB, s MarinaScope, expected int, in *Inquiry) error {
	res, err := db.NewUpdate().Model(in).Column("outcome", "quote_id", "contract_id", "notes", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", in.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetInquiry(ctx, db, s, in.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	in.Version = expected + 1
	return nil
}

// ListInquiries returns a page of inquiries, newest first.
func ListInquiries(ctx context.Context, db bun.IDB, s MarinaScope, f InquiryFilter, cursor string, limit int) (Page[Inquiry], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Inquiry]{}, err
	}
	rows := []Inquiry{}
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID())
	if f.Outcome != "" {
		q = q.Where("outcome = ?", f.Outcome)
	}
	if f.From != nil {
		q = q.Where("arrival >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("arrival <= ?", *f.To)
	}
	if after != "" {
		q = q.Where("id < ?", after)
	}
	if err := q.OrderExpr("id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Inquiry]{}, err
	}
	return pageOf(rows, limit, func(i Inquiry) string { return i.ID }), nil
}
