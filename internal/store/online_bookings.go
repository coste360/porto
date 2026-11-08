package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of online_bookings (m8-plan §4.3, §6, §12.3). The transit state is on the contract (INV-30): these
// rows carry only the online side (reference, token nonce, idempotency, contact, approval).

// InsertOnlineBooking inserts a row; a duplicate reference or idempotency hash is a unique violation (the caller
// maps it).
func InsertOnlineBooking(ctx context.Context, db bun.IDB, b *OnlineBooking) error {
	_, err := db.NewInsert().Model(b).Exec(ctx)
	return err
}

func oneOnlineBooking(ctx context.Context, q *bun.SelectQuery) (*OnlineBooking, error) {
	if err := q.Limit(1).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return nil, nil
}

// GetOnlineBooking returns a row by id in the marina (ErrNotFound otherwise).
func GetOnlineBooking(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*OnlineBooking, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	b := new(OnlineBooking)
	if _, err := oneOnlineBooking(ctx, db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Where("id = ?", id)); err != nil {
		return nil, err
	}
	return b, nil
}

// OnlineBookingByContract returns the row of a transit (ErrNotFound when the transit is not an online one).
func OnlineBookingByContract(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (*OnlineBooking, error) {
	if !ValidID(contractID) {
		return nil, ErrNotFound
	}
	b := new(OnlineBooking)
	if _, err := oneOnlineBooking(ctx, db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID)); err != nil {
		return nil, err
	}
	return b, nil
}

// OnlineBookingByIdempotency returns the row created with an idempotency hash (ErrNotFound when none).
func OnlineBookingByIdempotency(ctx context.Context, db bun.IDB, s MarinaScope, hash string) (*OnlineBooking, error) {
	b := new(OnlineBooking)
	if _, err := oneOnlineBooking(ctx, db.NewSelect().Model(b).Where("marina_id = ?", s.ID()).Where("idempotency_hash = ?", hash)); err != nil {
		return nil, err
	}
	return b, nil
}

// OnlineBookingsByContracts returns the rows of the given transits keyed by contract id.
func OnlineBookingsByContracts(ctx context.Context, db bun.IDB, s MarinaScope, ids []string) (map[string]OnlineBooking, error) {
	out := map[string]OnlineBooking{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []OnlineBooking
	if err := db.NewSelect().Model(&rows).Where("marina_id = ?", s.ID()).Where("contract_id IN (?)", bun.List(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ContractID] = r
	}
	return out, nil
}

// UpdateOnlineBooking writes the mutable columns of a row and bumps its version.
func UpdateOnlineBooking(ctx context.Context, db bun.IDB, s MarinaScope, b *OnlineBooking, now time.Time) error {
	b.UpdatedAt = UTC(now)
	res, err := db.NewUpdate().Model(b).
		Column("contact_enc", "people_on_board", "approval_reason", "approval_deadline", "approved_at", "approved_by_user_id",
			"rejected_at", "reject_reason", "locale", "inquiry_id", "updated_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("id = ?", b.ID).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		return ErrNotFound
	}
	b.Version++
	return nil
}

// CountLiveOnlineHolds counts the online transits of the marina that still hold a place without being confirmed
// (held, pending_payment, awaiting_approval), the max_live_holds bound of m8-plan §3.1.
func CountLiveOnlineHolds(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	return toInt(db.NewSelect().Model((*Contract)(nil)).Where("marina_id = ?", s.ID()).Where("kind = ?", ContractKindTransit).
		Where("channel = ?", OnlineChannelOnline).Where("status IN ('held','pending_payment','awaiting_approval')").Count(ctx))
}

// ApprovalsPastDeadline returns the contract ids of the awaiting_approval transits not yet approved whose approval
// deadline is before now (at most limit). An approved booking waiting for its capture or manual confirmation is
// never timed out.
func ApprovalsPastDeadline(ctx context.Context, db bun.IDB, s MarinaScope, now time.Time, limit int) ([]string, error) {
	var ids []string
	err := db.NewSelect().TableExpr("online_bookings AS ob").ColumnExpr("ob.contract_id").
		Join("JOIN contracts AS c ON c.id = ob.contract_id").
		Where("ob.marina_id = ?", s.ID()).Where("c.status = 'awaiting_approval'").Where("ob.approved_at IS NULL").
		Where("ob.approval_deadline < ?", UTC(now)).
		OrderExpr("ob.approval_deadline, ob.contract_id").Limit(int64(limit)).Scan(ctx, &ids)
	return ids, err
}

// OnlineBookingFilter filters the desk list.
type OnlineBookingFilter struct {
	Status   string
	Channel  string
	From, To calendar.Date // arrival range, zero = open
}

// OnlineBookingRow is a desk list row: the online booking with its transit.
type OnlineBookingRow struct {
	OnlineBooking `bun:",extend"`
	Status        string        `bun:"status"`
	StartsOn      calendar.Date `bun:"starts_on"`
	EndsOn        calendar.Date `bun:"ends_on"`
	TotalCents    int64         `bun:"total_cents"`
	Number        int64         `bun:"number"`
	Year          int           `bun:"year"`
	VesselID      string        `bun:"vessel_id"`
	CustomerID    string        `bun:"customer_id"`
	CVersion      int           `bun:"c_version"`
}

// ListOnlineBookings returns a page of online bookings, newest first (UUIDv7 ids are time ordered).
func ListOnlineBookings(ctx context.Context, db bun.IDB, s MarinaScope, f OnlineBookingFilter, cursor string, limit int) (Page[OnlineBookingRow], error) {
	limit = ClampLimit(limit)
	var rows []OnlineBookingRow
	q := db.NewSelect().Model(&rows).
		ColumnExpr("online_booking.*").
		ColumnExpr("c.status, c.starts_on, c.ends_on, c.total_cents, c.number, c.year, coalesce(CAST(c.vessel_id AS TEXT), '') AS vessel_id").
		ColumnExpr("coalesce(CAST(c.customer_id AS TEXT), '') AS customer_id, c.version AS c_version").
		Join("JOIN contracts AS c ON c.id = online_booking.contract_id").
		Where("online_booking.marina_id = ?", s.ID())
	if f.Status != "" {
		q = q.Where("c.status = ?", f.Status)
	}
	if f.Channel != "" {
		q = q.Where("online_booking.channel = ?", f.Channel)
	}
	if !f.From.IsZero() {
		q = q.Where("c.starts_on >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("c.starts_on <= ?", f.To)
	}
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[OnlineBookingRow]{}, err
	}
	if after != "" {
		q = q.Where("online_booking.id < ?", after)
	}
	if err := q.OrderExpr("online_booking.id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return Page[OnlineBookingRow]{}, err
	}
	page := pageOf(rows, limit, func(r OnlineBookingRow) string { return r.ID })
	if page.Items == nil {
		page.Items = []OnlineBookingRow{}
	}
	return page, nil
}

// OnlineReferenceTaken reports whether a reference is already used in the marina.
func OnlineReferenceTaken(ctx context.Context, db bun.IDB, s MarinaScope, ref string) (bool, error) {
	return db.NewSelect().Model((*OnlineBooking)(nil)).Where("marina_id = ?", s.ID()).Where("reference = ?", ref).Exists(ctx)
}

// ---- Payment intents of online bookings (manual capture, m8-plan §6, §16 D6) ----

// MarkIntentAuthorized records the card authorisation of a manual-capture intent: status processing, authorized_at
// set once. It reports false when the intent was already authorised or is no longer live.
func MarkIntentAuthorized(ctx context.Context, db bun.IDB, s MarinaScope, id, paymentRef string, now time.Time) (bool, error) {
	q := db.NewUpdate().Model((*PaymentIntent)(nil)).Set("status = ?", IntentProcessing).Set("authorized_at = ?", UTC(now)).
		Set("updated_at = ?", UTC(now)).Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("authorized_at IS NULL").
		Where("status IN (?)", bun.List(LiveIntentStatuses))
	if paymentRef != "" {
		q = q.Set("provider_payment_ref = ?", paymentRef)
	}
	res, err := q.Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// MarkIntentCaptured records that the capture of an authorised intent was accepted by the provider.
func MarkIntentCaptured(ctx context.Context, db bun.IDB, s MarinaScope, id string, now time.Time) error {
	_, err := db.NewUpdate().Model((*PaymentIntent)(nil)).Set("captured_at = ?", UTC(now)).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("captured_at IS NULL").Exec(ctx)
	return err
}

// MarkIntentVoided records the release of an authorisation: voided_at set, a live intent cancelled. It reports
// false when the intent was already voided.
func MarkIntentVoided(ctx context.Context, db bun.IDB, s MarinaScope, id string, now time.Time) (bool, error) {
	res, err := db.NewUpdate().Model((*PaymentIntent)(nil)).Set("voided_at = ?", UTC(now)).Set("updated_at = ?", UTC(now)).
		Set("status = CASE WHEN status IN (?) THEN ? ELSE status END", bun.List(LiveIntentStatuses), IntentCancelled).
		Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("voided_at IS NULL").Exec(ctx)
	if err != nil {
		return false, err
	}
	return affected(res) == 1, nil
}

// AuthorizedIntent is an authorised, not yet captured nor voided intent with the state of its transit.
type AuthorizedIntent struct {
	PaymentIntent  `bun:",extend"`
	ContractStatus string    `bun:"contract_status"`
	ApprovedAt     time.Time `bun:"approved_at,nullzero"`
}

// AuthorizedIntentsPending returns the authorised intents of the marina still waiting for a capture (transit
// approved) or a void (transit cancelled or expired), at most limit.
func AuthorizedIntentsPending(ctx context.Context, db bun.IDB, s MarinaScope, limit int) ([]AuthorizedIntent, error) {
	out := []AuthorizedIntent{}
	err := db.NewSelect().Model(&out).ColumnExpr("payment_intent.*").
		ColumnExpr("c.status AS contract_status, ob.approved_at").
		Join("JOIN contracts AS c ON c.id = payment_intent.contract_id").
		Join("LEFT JOIN online_bookings AS ob ON ob.contract_id = c.id").
		Where("payment_intent.marina_id = ?", s.ID()).Where("payment_intent.authorized_at IS NOT NULL").
		Where("payment_intent.captured_at IS NULL").Where("payment_intent.voided_at IS NULL").
		Where("payment_intent.status <> ?", IntentSucceeded).
		Where("((c.status = 'awaiting_approval' AND ob.approved_at IS NOT NULL) OR c.status IN ('cancelled','expired'))").
		OrderExpr("payment_intent.id").Limit(int64(limit)).Scan(ctx)
	return out, err
}

// LiveTransitIntent returns the live intent of a transit (purpose transit) or ErrNotFound.
func LiveTransitIntent(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (*PaymentIntent, error) {
	in := new(PaymentIntent)
	err := db.NewSelect().Model(in).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).
		Where("purpose = ?", IntentPurposeTransit).Where("status IN (?)", bun.List(LiveIntentStatuses)).
		OrderExpr("id DESC").Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return in, err
}

// LatestTransitIntent returns the newest intent of a transit (any status) or ErrNotFound.
func LatestTransitIntent(ctx context.Context, db bun.IDB, s MarinaScope, contractID string) (*PaymentIntent, error) {
	in := new(PaymentIntent)
	err := db.NewSelect().Model(in).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID).
		Where("purpose = ?", IntentPurposeTransit).OrderExpr("id DESC").Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return in, err
}

// ---- Public catalogue (m8-plan §3.2) ----

// OnlineResource is an active, online sellable berth or linear dynamic area with its category (price class).
type OnlineResource struct {
	Type       string `bun:"type"`
	ID         string `bun:"id"`
	Code       string `bun:"code"`
	PriceClass string `bun:"price_class"`
	MaxLOACm   int64  `bun:"max_loa_cm"`
	MaxBeamCm  int64  `bun:"max_beam_cm"`
}

// OnlineResources returns the online sellable resources of the marina (one UNION query).
func OnlineResources(ctx context.Context, db bun.IDB, s MarinaScope) ([]OnlineResource, error) {
	out := []OnlineResource{}
	err := db.NewRaw(`SELECT 'berth' AS type, id, code, coalesce(price_class, '') AS price_class, max_loa_cm, max_beam_cm
		FROM berths WHERE marina_id = ? AND active = ? AND online_sellable = ?
		UNION ALL
		SELECT 'dynamic_area' AS type, id, code, coalesce(price_class, '') AS price_class, 0 AS max_loa_cm, 0 AS max_beam_cm
		FROM dynamic_areas WHERE marina_id = ? AND active = ? AND online_sellable = ? AND kind = 'linear'
		ORDER BY code, id`, s.ID(), true, true, s.ID(), true, true).Scan(ctx, &out)
	return out, err
}

// ResourcePriceClass returns the price class of a berth or dynamic area ("" when none or unknown).
func ResourcePriceClass(ctx context.Context, db bun.IDB, s MarinaScope, resourceType, id string) (string, error) {
	table := "berths"
	if resourceType == ResourceArea {
		table = "dynamic_areas"
	}
	var pc []string
	err := db.NewSelect().Table(table).ColumnExpr("coalesce(price_class, '')").Where("marina_id = ?", s.ID()).
		Where("id = ?", id).Limit(1).Scan(ctx, &pc)
	if err != nil || len(pc) == 0 {
		return "", err
	}
	return pc[0], nil
}

// OnlineServiceItems returns the active service items visible online, in catalogue order.
func OnlineServiceItems(ctx context.Context, db bun.IDB, s MarinaScope) ([]Item, error) {
	out := []Item{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("active = ?", true).
		Where("online_visible = ?", true).Where("item_type = ?", "service").OrderExpr("sort_order, code").Scan(ctx)
	return out, err
}

// TransitsOfCustomer returns the transits of a customer in the marina, newest first.
func TransitsOfCustomer(ctx context.Context, db bun.IDB, s MarinaScope, customerID string, limit int) ([]Contract, error) {
	out := []Contract{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("kind = ?", ContractKindTransit).
		Where("customer_id = ?", customerID).OrderExpr("starts_on DESC, id DESC").Limit(int64(ClampLimit(limit))).Scan(ctx)
	return out, err
}

// ResourceCode returns the code of a berth, dynamic area or land resource of the marina.
func ResourceCode(ctx context.Context, db bun.IDB, s MarinaScope, resourceType, id string) (string, error) {
	table := "berths"
	switch resourceType {
	case ResourceArea:
		table = "dynamic_areas"
	case ResourceLand:
		table = "land_resources"
	}
	var codes []string
	if err := db.NewSelect().Table(table).Column("code").Where("marina_id = ?", s.ID()).Where("id = ?", id).Limit(1).Scan(ctx, &codes); err != nil {
		return "", err
	}
	if len(codes) == 0 {
		return "", ErrNotFound
	}
	return codes[0], nil
}
