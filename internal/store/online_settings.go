package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Queries of online_booking_settings (m8-plan §3.1). CONTRACT ships the read used by onlineconf.Load; ACCOUNT adds
// the versioned upsert, the booking texts and the desk side of the online quotas (configuration and usage reads).

// GetOnlineBookingSettings returns the settings row of the marina, or ErrNotFound when it was never saved.
func GetOnlineBookingSettings(ctx context.Context, db bun.IDB, s MarinaScope) (*OnlineBookingSettings, error) {
	row := new(OnlineBookingSettings)
	err := db.NewSelect().Model(row).Where("marina_id = ?", s.ID()).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// SaveOnlineBookingSettings stores the settings row of the marina. With expected 0 the row must not exist yet (it
// is inserted with version 1); otherwise the stored version must equal expected (ErrVersionMismatch) and it is
// bumped. row.Version holds the new version on success.
func SaveOnlineBookingSettings(ctx context.Context, db bun.IDB, s MarinaScope, expected int, row *OnlineBookingSettings, now time.Time) error {
	row.MarinaID = s.ID()
	row.UpdatedAt = UTC(now)
	if expected == 0 {
		row.Version, row.CreatedAt = 1, UTC(now)
		_, err := db.NewInsert().Model(row).Exec(ctx)
		if IsUniqueViolation(err) {
			return ErrVersionMismatch
		}
		return err
	}
	res, err := db.NewUpdate().Model(row).ExcludeColumn("marina_id", "version", "created_at").
		Set("version = version + 1").Where("marina_id = ?", s.ID()).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetOnlineBookingSettings(ctx, db, s); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrVersionMismatch
			}
			return err
		}
		return ErrVersionMismatch
	}
	row.Version = expected + 1
	return nil
}

// ListBookingTexts returns the published texts of the marina, newest version first per kind; kind "" lists all.
func ListBookingTexts(ctx context.Context, db bun.IDB, s MarinaScope, kind string) ([]BookingText, error) {
	out := []BookingText{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID())
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	err := q.OrderExpr("kind ASC, version DESC").Scan(ctx)
	return out, err
}

// CurrentBookingTexts returns the highest published version of each kind of the marina.
func CurrentBookingTexts(ctx context.Context, db bun.IDB, s MarinaScope) ([]BookingText, error) {
	out := []BookingText{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).
		Where("version = (SELECT MAX(t2.version) FROM booking_texts AS t2 WHERE t2.marina_id = booking_text.marina_id AND t2.kind = booking_text.kind)").
		OrderExpr("kind ASC").Scan(ctx)
	return out, err
}

// NextBookingTextVersion returns the next version number of a text kind of the marina.
func NextBookingTextVersion(ctx context.Context, db bun.IDB, s MarinaScope, kind string) (int, error) {
	var v sql.NullInt64
	err := db.NewRaw("SELECT MAX(version) FROM booking_texts WHERE marina_id = ? AND kind = ?", s.ID(), kind).Scan(ctx, &v)
	if err != nil {
		return 0, err
	}
	return int(v.Int64) + 1, nil
}

// InsertBookingText inserts a published text; a concurrent publish of the same version is ErrConflict.
func InsertBookingText(ctx context.Context, db bun.IDB, t *BookingText) error {
	_, err := db.NewInsert().Model(t).Exec(ctx)
	if IsUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// UpdateOnlineQuota writes the online quota kind and value of a berth group of the marina (ErrNotFound for an
// unknown or foreign group). It does not touch the group version: quotas are not part of the group editor.
func UpdateOnlineQuota(ctx context.Context, db bun.IDB, s MarinaScope, groupID, kind string, value int64, now time.Time) error {
	if !ValidID(groupID) {
		return ErrNotFound
	}
	res, err := db.NewUpdate().Table("berth_groups").Set("online_quota_kind = ?", kind).
		Set("online_quota_value = ?", value).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("id = ?", groupID).Exec(ctx)
	return expectOne(res, err)
}

// RecapOnlineQuotaUsage rewrites the cap of the usage rows of a group from night on: cap = max(newCap, used), so
// the cap never drops below what is already sold (m8-plan §3.3).
func RecapOnlineQuotaUsage(ctx context.Context, db bun.IDB, s MarinaScope, groupID string, from calendar.Date, newCap int, now time.Time) error {
	_, err := db.NewUpdate().Table("online_quota_usage").
		Set("cap = CASE WHEN used > ? THEN used ELSE ? END", newCap, newCap).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("group_id = ?", groupID).Where("night >= ?", from).Exec(ctx)
	return err
}

// OnlineQuotaUsageRange returns the usage rows of the marina with from <= night <= to, by group and night.
func OnlineQuotaUsageRange(ctx context.Context, db bun.IDB, s MarinaScope, from, to calendar.Date) ([]OnlineQuotaUsage, error) {
	out := []OnlineQuotaUsage{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("night >= ?", from).Where("night <= ?", to).
		OrderExpr("group_id, night").Scan(ctx)
	return out, err
}

// GroupsWithAreas returns the ids of the berth groups of the marina that contain at least one dynamic area.
func GroupsWithAreas(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]bool, error) {
	out := map[string]bool{}
	counts, err := GroupMemberCounts(ctx, db, s)
	if err != nil {
		return nil, err
	}
	for gid, m := range counts {
		if m["dynamic_area"] > 0 {
			out[gid] = true
		}
	}
	return out, nil
}

// ActiveMemberBerths returns, per berth group of the marina, the number of active member berths (the base of a
// percent online quota, m8-plan §3.3).
func ActiveMemberBerths(ctx context.Context, db bun.IDB, s MarinaScope) (map[string]int, error) {
	var rows []struct {
		GroupID string `bun:"group_id"`
		N       int    `bun:"n"`
	}
	err := db.NewSelect().TableExpr("berth_group_members AS m").Column("m.group_id").ColumnExpr("COUNT(*) AS n").
		Join("JOIN berths AS b ON b.id = m.resource_id AND b.marina_id = m.marina_id").
		Where("m.marina_id = ?", s.ID()).Where("m.resource_type = 'berth'").Where("b.active").
		Group("m.group_id").Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, r := range rows {
		out[r.GroupID] = r.N
	}
	return out, nil
}

// LockBookingTextKind serialises the publishes of one text kind of a marina (next version = max + 1). On
// PostgreSQL it takes a transaction advisory lock; on SQLite every write transaction is already exclusive.
func LockBookingTextKind(ctx context.Context, tx bun.IDB, s MarinaScope, kind string) error {
	if !IsPostgres(tx) {
		return nil
	}
	_, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtext(?))", "booking_texts:"+s.ID()+":"+kind).Exec(ctx)
	return err
}
