package store

import (
	"context"
	"sort"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
)

// Online quota counters (m8-plan §3.3, INV-12). A claim is the conditional increment of a usage row; zero rows
// changed means the quota is used up. Rows are always touched in (group_id, night) order so that concurrent
// claims on PostgreSQL never deadlock on each other.

// QuotaGroup is a berth group with an online quota and the number of its active member berths.
type QuotaGroup struct {
	ID           string `bun:"id"`
	Kind         string `bun:"online_quota_kind"`
	Value        int64  `bun:"online_quota_value"`
	MemberBerths int    `bun:"member_berths"`
}

// QuotaGroupsOfResource returns the groups with an online quota (kind <> none) that contain the resource, ordered
// by id.
func QuotaGroupsOfResource(ctx context.Context, db bun.IDB, s MarinaScope, resourceType, resourceID string) ([]QuotaGroup, error) {
	out := []QuotaGroup{}
	err := db.NewRaw(`SELECT g.id, g.online_quota_kind, g.online_quota_value,
			(SELECT count(*) FROM berth_group_members AS mb JOIN berths AS b ON b.id = mb.resource_id
				WHERE mb.group_id = g.id AND mb.resource_type = 'berth' AND b.active = ?) AS member_berths
		FROM berth_groups AS g
		WHERE g.marina_id = ? AND g.online_quota_kind <> 'none'
			AND EXISTS (SELECT 1 FROM berth_group_members AS m WHERE m.group_id = g.id AND m.resource_type = ? AND m.resource_id = ?)
		ORDER BY g.id`, true, s.ID(), resourceType, resourceID).Scan(ctx, &out)
	return out, err
}

// QuotaGroups returns every group of the marina with an online quota, ordered by id.
func QuotaGroups(ctx context.Context, db bun.IDB, s MarinaScope) ([]QuotaGroup, error) {
	out := []QuotaGroup{}
	err := db.NewRaw(`SELECT g.id, g.online_quota_kind, g.online_quota_value,
			(SELECT count(*) FROM berth_group_members AS mb JOIN berths AS b ON b.id = mb.resource_id
				WHERE mb.group_id = g.id AND mb.resource_type = 'berth' AND b.active = ?) AS member_berths
		FROM berth_groups AS g WHERE g.marina_id = ? AND g.online_quota_kind <> 'none' ORDER BY g.id`, true, s.ID()).Scan(ctx, &out)
	return out, err
}

// QuotaUsage returns the usage rows of the groups on the nights [first, last].
func QuotaUsage(ctx context.Context, db bun.IDB, s MarinaScope, groupIDs []string, first, last calendar.Date) ([]OnlineQuotaUsage, error) {
	out := []OnlineQuotaUsage{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("group_id IN (?)", bun.List(groupIDs)).
		Where("night >= ?", first).Where("night <= ?", last).OrderExpr("group_id, night").Scan(ctx)
	return out, err
}

// ClaimQuotaNight claims one unit of a group on a night for a contract: the usage row is created with capNow when
// missing, then incremented only while used < cap. It reports false when the quota is used up (nothing written).
func ClaimQuotaNight(ctx context.Context, db bun.IDB, s MarinaScope, contractID, groupID string, night calendar.Date, capNow int, now time.Time) (bool, error) {
	now = UTC(now)
	row := &OnlineQuotaUsage{MarinaID: s.ID(), GroupID: groupID, Night: night, Cap: max(capNow, 0), UpdatedAt: now}
	if _, err := db.NewInsert().Model(row).On("CONFLICT (group_id, night) DO NOTHING").Exec(ctx); err != nil {
		return false, err
	}
	res, err := db.NewUpdate().Model((*OnlineQuotaUsage)(nil)).Set("used = used + 1").Set("updated_at = ?", now).
		Where("marina_id = ?", s.ID()).Where("group_id = ?", groupID).Where("night = ?", night).Where("used < cap").Exec(ctx)
	if err != nil {
		return false, err
	}
	if affected(res) == 0 {
		return false, nil
	}
	claim := &OnlineQuotaClaim{MarinaID: s.ID(), ContractID: contractID, GroupID: groupID, Night: night, CreatedAt: now}
	if _, err := db.NewInsert().Model(claim).Exec(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// QuotaClaimsOf returns the claims of a contract (optionally only the nights on or after from), ordered by group
// and night.
func QuotaClaimsOf(ctx context.Context, db bun.IDB, s MarinaScope, contractID string, from *calendar.Date) ([]OnlineQuotaClaim, error) {
	out := []OnlineQuotaClaim{}
	q := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("contract_id = ?", contractID)
	if from != nil {
		q = q.Where("night >= ?", *from)
	}
	err := q.OrderExpr("group_id, night").Scan(ctx)
	return out, err
}

// ReleaseQuotaClaims deletes the given claims of a contract and decrements their usage rows. It returns the
// number of claims released (a claim already released by a concurrent transaction is skipped).
func ReleaseQuotaClaims(ctx context.Context, db bun.IDB, s MarinaScope, claims []OnlineQuotaClaim, now time.Time) (int, error) {
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].GroupID != claims[j].GroupID {
			return claims[i].GroupID < claims[j].GroupID
		}
		return claims[i].Night.Before(claims[j].Night)
	})
	n := 0
	for _, c := range claims {
		res, err := db.NewDelete().Model((*OnlineQuotaClaim)(nil)).Where("marina_id = ?", s.ID()).
			Where("contract_id = ?", c.ContractID).Where("group_id = ?", c.GroupID).Where("night = ?", c.Night).Exec(ctx)
		if err != nil {
			return n, err
		}
		if affected(res) == 0 {
			continue
		}
		if _, err := db.NewUpdate().Model((*OnlineQuotaUsage)(nil)).Set("used = used - 1").Set("updated_at = ?", UTC(now)).
			Where("marina_id = ?", s.ID()).Where("group_id = ?", c.GroupID).Where("night = ?", c.Night).Where("used > 0").Exec(ctx); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SetQuotaCapFrom rewrites the cap of the future usage rows of a group (night ≥ from) to max(newCap, used), never
// below what is already sold (m8-plan §3.3).
func SetQuotaCapFrom(ctx context.Context, db bun.IDB, s MarinaScope, groupID string, from calendar.Date, newCap int, now time.Time) error {
	_, err := db.NewUpdate().Model((*OnlineQuotaUsage)(nil)).
		Set("cap = CASE WHEN used > ? THEN used ELSE ? END", newCap, max(newCap, 0)).Set("updated_at = ?", UTC(now)).
		Where("marina_id = ?", s.ID()).Where("group_id = ?", groupID).Where("night >= ?", from).Exec(ctx)
	return err
}
