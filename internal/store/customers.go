package store

import (
	"context"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// Queries of customers (m1-plan §3.1, §4.4). Every function is scoped by the marina.

// CustomerFilter narrows ListCustomers. Search is a normalised term (customer.SearchTerm): it matches the
// customer's search_text or the search_text of a vessel linked to the customer.
type CustomerFilter struct {
	Search   string
	Category string
	Tag      string
	Archived bool // true = only archived customers, false = only active ones
}

// likePattern returns a LIKE pattern matching s anywhere, with % _ and ! escaped by "!" (ESCAPE '!').
func likePattern(s string) string {
	r := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return "%" + r.Replace(s) + "%"
}

// InsertCustomer inserts a customer.
func InsertCustomer(ctx context.Context, db bun.IDB, c *Customer) error {
	_, err := db.NewInsert().Model(c).Exec(ctx)
	return err
}

// GetCustomer returns one customer of a marina.
func GetCustomer(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Customer, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(Customer)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// CustomersByIDs returns the customers of a marina with the given ids (unknown ids are skipped).
func CustomersByIDs(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) ([]Customer, error) {
	out := []Customer{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("id IN (?)", bun.List(ids)).
		OrderExpr("id").Scan(ctx)
	return out, err
}

// customerColumns are the columns an update writes (everything but id, marina_id, version and created_at).
var customerColumns = []string{
	"kind", "first_name", "last_name", "company_name", "fiscal_code", "vat_number", "sdi_code", "pec",
	"billing_address", "nationality", "locale", "emails", "phones", "category", "tags", "payment_terms",
	"iban_enc", "iban_last4", "notes", "search_text", "merged_into_id", "anonymized_at", "archived_at", "updated_at",
}

// UpdateCustomer writes every editable column when the stored version equals ifMatch and bumps the version.
// A stale version is ErrVersionMismatch, an unknown customer ErrNotFound.
func UpdateCustomer(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, c *Customer) error {
	res, err := db.NewUpdate().Model(c).Column(customerColumns...).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("version = ?", ifMatch).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetCustomer(ctx, db, s, c.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	c.Version = ifMatch + 1
	return nil
}

// ListCustomers returns a page of customers, newest first.
func ListCustomers(ctx context.Context, db bun.IDB, s MarinaScope, f CustomerFilter, cursor string, limit int) (Page[Customer], error) {
	limit = ClampLimit(limit)
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[Customer]{}, err
	}
	rows := []Customer{}
	q := db.NewSelect().Model(&rows).Where("customer.marina_id = ?", s.ID())
	if f.Archived {
		q = q.Where("customer.archived_at IS NOT NULL")
	} else {
		q = q.Where("customer.archived_at IS NULL")
	}
	if f.Category != "" {
		q = q.Where("customer.category = ?", f.Category)
	}
	if f.Tag != "" {
		if IsPostgres(db) {
			q = q.Where("jsonb_exists(customer.tags, ?)", f.Tag)
		} else {
			q = q.Where("EXISTS (SELECT 1 FROM json_each(customer.tags) AS t WHERE t.value = ?)", f.Tag)
		}
	}
	if f.Search != "" {
		p := likePattern(f.Search)
		q = q.Where(`(customer.search_text LIKE ? ESCAPE '!' OR EXISTS (SELECT 1 FROM vessel_parties AS vp
			JOIN vessels AS v ON v.id = vp.vessel_id
			WHERE vp.customer_id = customer.id AND vp.marina_id = ? AND v.search_text LIKE ? ESCAPE '!'))`, p, s.ID(), p)
	}
	if after != "" {
		q = q.Where("customer.id < ?", after)
	}
	if err := q.OrderExpr("customer.id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[Customer]{}, err
	}
	return pageOf(rows, limit, func(c Customer) string { return c.ID }), nil
}

// CustomerDuplicateCandidates returns the live (not merged, not anonymised) customers of a marina, other than
// excludeID, sharing the fiscal code, the VAT number or one of the emails. Email matches are candidates by
// search_text only; the caller confirms them on the decoded emails.
func CustomerDuplicateCandidates(ctx context.Context, db bun.IDB, s MarinaScope, excludeID, fiscalCode, vat string, emails []string) ([]Customer, error) {
	out := []Customer{}
	var conds []string
	var args []any
	if fiscalCode != "" {
		conds = append(conds, "fiscal_code = ?")
		args = append(args, fiscalCode)
	}
	if vat != "" {
		conds = append(conds, "vat_number = ?")
		args = append(args, vat)
	}
	for _, e := range emails {
		if e != "" {
			conds = append(conds, "search_text LIKE ? ESCAPE '!'")
			args = append(args, likePattern(e))
		}
	}
	if len(conds) == 0 {
		return out, nil
	}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("merged_into_id IS NULL").
		Where("anonymized_at IS NULL").Where("("+strings.Join(conds, " OR ")+")", args...)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.OrderExpr("id").Limit(50).Scan(ctx)
	return out, err
}

// VesselCountsOfCustomers returns, per customer id, the number of distinct vessels linked to it.
func VesselCountsOfCustomers(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]int, error) {
	out := map[string]int{}
	ids = validIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		CustomerID string `bun:"customer_id"`
		N          int    `bun:"n"`
	}
	err := db.NewSelect().Table("vessel_parties").ColumnExpr("customer_id, COUNT(DISTINCT vessel_id) AS n").
		Where("marina_id = ?", s.ID()).Where("customer_id IN (?)", bun.List(ids)).Group("customer_id").Scan(ctx, &rows)
	for _, r := range rows {
		out[r.CustomerID] = r.N
	}
	return out, err
}

// MarkCustomerMerged sets merged_into_id and archived_at on a source customer and bumps its version.
func MarkCustomerMerged(ctx context.Context, db bun.IDB, s MarinaScope, id, targetID string, at time.Time) error {
	res, err := db.NewUpdate().Table("customers").Set("merged_into_id = ?", targetID).Set("archived_at = ?", at).
		Set("updated_at = ?", at).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
	return expectOne(res, err)
}

// MoveCustomerRecords re-points the documents, deadlines and consents of the source customers to the target
// (merge). Vessel parties are moved by the caller, which resolves the duplicates first.
func MoveCustomerRecords(ctx context.Context, db bun.IDB, s MarinaScope, sourceIDs []string, targetID string, at time.Time) error {
	sourceIDs = validIDs(sourceIDs)
	if len(sourceIDs) == 0 {
		return nil
	}
	if _, err := db.NewUpdate().Table("documents").Set("customer_id = ?", targetID).Set("updated_at = ?", at).
		Where("marina_id = ?", s.ID()).Where("customer_id IN (?)", bun.List(sourceIDs)).Exec(ctx); err != nil {
		return err
	}
	if _, err := db.NewUpdate().Table("deadlines").Set("customer_id = ?", targetID).Set("updated_at = ?", at).
		Where("marina_id = ?", s.ID()).Where("customer_id IN (?)", bun.List(sourceIDs)).Exec(ctx); err != nil {
		return err
	}
	_, err := db.NewUpdate().Table("consents").Set("customer_id = ?", targetID).
		Where("marina_id = ?", s.ID()).Where("customer_id IN (?)", bun.List(sourceIDs)).Exec(ctx)
	return err
}

// CountCustomers returns the number of customers of a marina (seed idempotency).
func CountCustomers(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Table("customers").Where("marina_id = ?", s.ID()).Count(ctx))
}
