package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of customer communications and of the per-customer message registry (m4-plan §5.3, §6.7).

// Communication statuses and recipient statuses.
const (
	CommDraft     = "draft"
	CommScheduled = "scheduled"
	CommSending   = "sending"
	CommSent      = "sent"
	CommCancelled = "cancelled"

	RecipientPending          = "pending"
	RecipientQueued           = "queued"
	RecipientSkippedNoConsent = "skipped_no_consent"
	RecipientSkippedNoEmail   = "skipped_no_email"
	RecipientFailed           = "failed"
)

// InsertCustomerMessage records an email in the customer registry.
func InsertCustomerMessage(ctx context.Context, db bun.IDB, m *CustomerMessage) error {
	_, err := db.NewInsert().Model(m).Exec(ctx)
	return err
}

// MessageIDByDedupe returns the message of a dedupe key in a marina ("" marina = account emails).
func MessageIDByDedupe(ctx context.Context, db bun.IDB, marinaID, key string) (string, error) {
	var id string
	err := marinaOf(db.NewSelect().Table("message_log").Column("id"), marinaID).
		Where("dedupe_key = ?", key).Limit(1).Scan(ctx, &id)
	return id, notFound(err)
}

// marinaOf filters message_log by marina ("" = account emails without a marina).
func marinaOf(q *bun.SelectQuery, marinaID string) *bun.SelectQuery {
	if marinaID == "" {
		return q.Where("marina_id IS NULL")
	}
	return q.Where("marina_id = ?", marinaID)
}

// MessageExists reports whether a dedupe key is already used in a marina.
func MessageExists(ctx context.Context, db bun.IDB, marinaID, key string) (bool, error) {
	n, err := marinaOf(db.NewSelect().Table("message_log"), marinaID).Where("dedupe_key = ?", key).Count(ctx)
	return n > 0, err
}

// CustomerMessageRow is a registry row with the delivery status from message_log.
type CustomerMessageRow struct {
	CustomerMessage
	Status string    `bun:"status"`
	SentAt time.Time `bun:"sent_at,nullzero"`
}

// ListCustomerMessages returns the registry of a customer, newest first.
func ListCustomerMessages(ctx context.Context, db bun.IDB, s MarinaScope, customerID, cursor string, limit int) (Page[CustomerMessageRow], error) {
	limit = ClampLimit(limit)
	var rows []CustomerMessageRow
	q := db.NewSelect().TableExpr("customer_messages AS cm").
		ColumnExpr("cm.*, ml.status AS status, ml.sent_at AS sent_at").
		Join("JOIN message_log AS ml ON ml.id = cm.message_id").
		Where("cm.marina_id = ?", s.ID()).Where("cm.customer_id = ?", customerID).OrderExpr("cm.id DESC").
		Limit(int64(limit + 1))
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[CustomerMessageRow]{}, err
		}
		q = q.Where("cm.id < ?", last)
	}
	if err := q.Scan(ctx, &rows); err != nil {
		return Page[CustomerMessageRow]{}, err
	}
	return pageOf(rows, limit, func(r CustomerMessageRow) string { return r.ID }), nil
}

// MessageStatuses returns the delivery status and send time of messages by id.
func MessageStatuses(ctx context.Context, db bun.IDB, ids []string) (map[string]Message, error) {
	out := map[string]Message{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Message
	err := db.NewSelect().Model(&rows).Column("id", "status", "sent_at").Where("id IN (?)", bun.List(ids)).Scan(ctx)
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, err
}

// InsertCommCampaign inserts a communication.
func InsertCommCampaign(ctx context.Context, db bun.IDB, c *CommCampaign) error {
	_, err := db.NewInsert().Model(c).Exec(ctx)
	return err
}

// GetCommCampaign returns one communication of a marina.
func GetCommCampaign(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*CommCampaign, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	c := new(CommCampaign)
	if err := db.NewSelect().Model(c).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// ListCommCampaigns returns the communications of a marina, newest first.
func ListCommCampaigns(ctx context.Context, db bun.IDB, s MarinaScope, status, cursor string, limit int) (Page[CommCampaign], error) {
	limit = ClampLimit(limit)
	var rows []CommCampaign
	q := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).OrderExpr("id DESC").Limit(int64(limit + 1))
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[CommCampaign]{}, err
		}
		q = q.Where("id < ?", last)
	}
	if err := q.Scan(ctx); err != nil {
		return Page[CommCampaign]{}, err
	}
	return pageOf(rows, limit, func(c CommCampaign) string { return c.ID }), nil
}

// UpdateCommCampaign writes every editable column of a communication in status from when the version equals
// ifMatch (0 = the loaded version) and bumps it. ErrVersionMismatch when the version or the status differ.
func UpdateCommCampaign(ctx context.Context, db bun.IDB, s MarinaScope, ifMatch int, from []string, c *CommCampaign) error {
	want := ifMatch
	if want == 0 {
		want = c.Version
	}
	res, err := db.NewUpdate().Model(c).
		Column("name", "category", "audience", "channels", "template_id", "subject", "body_md", "attachments",
			"scheduled_at", "status", "counts", "sent_at", "cancelled_at", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", c.ID).Where("version = ?", want).
		Where("status IN (?)", bun.List(from)).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetCommCampaign(ctx, db, s, c.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	c.Version = want + 1
	return nil
}

// DueCommCampaigns returns the communications to process: scheduled and due, or sending (crash recovery).
func DueCommCampaigns(ctx context.Context, db bun.IDB, now time.Time, limit int) ([]CommCampaign, error) {
	var rows []CommCampaign
	err := db.NewSelect().Model(&rows).
		Where("(status = ? AND scheduled_at <= ?) OR status = ?", CommScheduled, UTC(now), CommSending).
		OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return rows, err
}

// InsertCommRecipients inserts recipients, ignoring the ones already present (unique campaign, customer).
func InsertCommRecipients(ctx context.Context, db bun.IDB, rows []CommRecipient) error {
	if len(rows) == 0 {
		return nil
	}
	_, err := db.NewInsert().Model(&rows).On("CONFLICT DO NOTHING").Exec(ctx)
	return err
}

// PendingRecipients returns up to limit pending recipients of a communication.
func PendingRecipients(ctx context.Context, db bun.IDB, s MarinaScope, campaignID string, limit int) ([]CommRecipient, error) {
	var rows []CommRecipient
	err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("campaign_id = ?", campaignID).
		Where("status = ?", RecipientPending).OrderExpr("id").Limit(int64(limit)).Scan(ctx)
	return rows, err
}

// SetRecipientStatus moves a pending recipient to status (conditional: false when it is no longer pending).
func SetRecipientStatus(ctx context.Context, db bun.IDB, s MarinaScope, id, status, messageID string, at time.Time) (bool, error) {
	res, err := db.NewUpdate().Table("comm_recipients").Set("status = ?", status).
		Set("message_id = ?", nullable(messageID)).Set("updated_at = ?", UTC(at)).Where("marina_id = ?", s.ID()).
		Where("id = ?", id).Where("status = ?", RecipientPending).Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// RecipientCounts returns the recipients of a communication by status.
func RecipientCounts(ctx context.Context, db bun.IDB, s MarinaScope, campaignID string) (map[string]int, error) {
	var rows []struct {
		Status string `bun:"status"`
		N      int    `bun:"n"`
	}
	err := db.NewSelect().Table("comm_recipients").ColumnExpr("status, COUNT(*) AS n").Where("marina_id = ?", s.ID()).
		Where("campaign_id = ?", campaignID).GroupExpr("status").Scan(ctx, &rows)
	out := map[string]int{}
	for _, r := range rows {
		out[r.Status] = r.N
	}
	return out, err
}

// CommRecipientRow is a recipient with its delivery status.
type CommRecipientRow struct {
	CommRecipient
	Delivery string    `bun:"delivery"`
	SentAt   time.Time `bun:"sent_at,nullzero"`
}

// ListCommRecipients returns the recipients of a communication (status "" = all), in insertion order.
func ListCommRecipients(ctx context.Context, db bun.IDB, s MarinaScope, campaignID, status, cursor string, limit int) (Page[CommRecipientRow], error) {
	limit = ClampLimit(limit)
	var rows []CommRecipientRow
	q := db.NewSelect().TableExpr("comm_recipients AS r").
		ColumnExpr("r.*, ml.status AS delivery, ml.sent_at AS sent_at").
		Join("LEFT JOIN message_log AS ml ON ml.id = r.message_id").
		Where("r.marina_id = ?", s.ID()).Where("r.campaign_id = ?", campaignID).OrderExpr("r.id").Limit(int64(limit + 1))
	if status != "" {
		q = q.Where("r.status = ?", status)
	}
	if cursor != "" {
		last, err := DecodeCursor(cursor)
		if err != nil {
			return Page[CommRecipientRow]{}, err
		}
		q = q.Where("r.id > ?", last)
	}
	if err := q.Scan(ctx, &rows); err != nil {
		return Page[CommRecipientRow]{}, err
	}
	return pageOf(rows, limit, func(r CommRecipientRow) string { return r.ID }), nil
}

// LatestConsents returns, per customer, whether the latest consent of kind is granted (absent = no consent).
func LatestConsents(ctx context.Context, db bun.IDB, s MarinaScope, kind string, customerIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(customerIDs) == 0 {
		return out, nil
	}
	var rows []Consent
	err := db.NewSelect().Model(&rows).Column("customer_id", "granted", "recorded_at", "id").
		Where("marina_id = ?", s.ID()).Where("kind = ?", kind).Where("customer_id IN (?)", bun.List(customerIDs)).
		OrderExpr("customer_id, recorded_at, id").Scan(ctx)
	for _, r := range rows {
		out[r.CustomerID] = r.Granted // ordered: the last one wins
	}
	return out, err
}

// Audience queries of communications (m4-plan §6.7). Each returns the ids of the matching customers of a marina;
// the service intersects the criteria of an audience.

func idsOf(ctx context.Context, q *bun.SelectQuery) ([]string, error) {
	var ids []string
	err := q.Scan(ctx, &ids)
	return ids, err
}

// ReachableCustomerIDs returns the customers that can receive communications (not merged, anonymised or
// archived).
func ReachableCustomerIDs(ctx context.Context, db bun.IDB, s MarinaScope) ([]string, error) {
	return idsOf(ctx, db.NewSelect().Table("customers").Column("id").Where("marina_id = ?", s.ID()).
		Where("merged_into_id IS NULL").Where("anonymized_at IS NULL").Where("archived_at IS NULL").OrderExpr("id"))
}

// ContractCustomerIDs returns the customers with a contract in statuses (kinds and terms nil = any).
func ContractCustomerIDs(ctx context.Context, db bun.IDB, s MarinaScope, statuses, kinds, terms []string) ([]string, error) {
	q := db.NewSelect().Table("contracts").ColumnExpr("DISTINCT CAST(customer_id AS TEXT)").Where("marina_id = ?", s.ID()).
		Where("customer_id IS NOT NULL").Where("status IN (?)", bun.List(statuses))
	if len(kinds) > 0 {
		q = q.Where("kind IN (?)", bun.List(kinds))
	}
	if len(terms) > 0 {
		q = q.Where("term IN (?)", bun.List(terms))
	}
	return idsOf(ctx, q)
}

// AllocationCustomerIDs returns the customers of the active allocations overlapping [from, to]; resourceIDs,
// groupIDs and structureIDs narrow the resources (nil = any).
func AllocationCustomerIDs(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date, resourceIDs, groupIDs, structureIDs []string) ([]string, error) {
	q := db.NewSelect().TableExpr("allocations AS a").ColumnExpr("DISTINCT CAST(c.customer_id AS TEXT)").
		Join("JOIN contract_lines AS l ON l.id = a.contract_line_id").Join("JOIN contracts AS c ON c.id = l.contract_id").
		Where("a.marina_id = ?", s.ID()).Where("a.status = ?", AllocActive).Where("c.customer_id IS NOT NULL").
		Where("a.last_night >= ?", from).Where("a.first_night <= ?", to)
	if resourceIDs != nil {
		ids := validIDs(resourceIDs)
		if len(ids) == 0 {
			return nil, nil
		}
		q = q.Where("a.resource_id IN (?)", bun.List(ids))
	}
	if groupIDs != nil {
		ids := validIDs(groupIDs)
		if len(ids) == 0 {
			return nil, nil
		}
		q = q.Where("a.resource_id IN (SELECT m.resource_id FROM berth_group_members AS m WHERE m.marina_id = ? AND m.group_id IN (?))", s.ID(), bun.List(ids))
	}
	if structureIDs != nil {
		ids := validIDs(structureIDs)
		if len(ids) == 0 {
			return nil, nil
		}
		q = q.Where("a.resource_type = 'berth'").
			Where("a.resource_id IN (SELECT b.id FROM berths AS b WHERE b.marina_id = ? AND b.structure_id IN (?))", s.ID(), bun.List(ids))
	}
	return idsOf(ctx, q)
}

// RightHolderCustomerIDs returns the holders of the active berth rights.
func RightHolderCustomerIDs(ctx context.Context, db bun.IDB, s MarinaScope) ([]string, error) {
	return idsOf(ctx, db.NewSelect().TableExpr("berth_right_holders AS h").ColumnExpr("DISTINCT CAST(h.customer_id AS TEXT)").
		Join("JOIN berth_rights AS r ON r.id = h.right_id").Where("h.marina_id = ?", s.ID()).Where("r.status = ?", "active"))
}

// DeadlineCustomerIDs returns the customers with an open deadline due on or before until.
func DeadlineCustomerIDs(ctx context.Context, db bun.IDB, s MarinaScope, until calendar.Date) ([]string, error) {
	return idsOf(ctx, db.NewSelect().Table("deadlines").ColumnExpr("DISTINCT CAST(customer_id AS TEXT)").
		Where("marina_id = ?", s.ID()).Where("customer_id IS NOT NULL").Where("status = ?", "open").Where("due_on <= ?", until))
}
