package onlineconf

import (
	"context"
	"encoding/json"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/store"
)

// Desk side of the online quotas (m8-plan §3.3, §11): configuration per berth group and the usage table. The
// counters themselves (online_quota_usage, claims) are written by the booking transactions (occupancy.ClaimQuotaTx).

// MaxUsageDays bounds the range of a usage read.
const MaxUsageDays = 92

// Quota is the online quota configuration of a berth group.
type Quota struct {
	GroupID      string
	GroupCode    string
	GroupName    LongText
	Kind         string // store.QuotaNone, QuotaCount, QuotaPercent
	Value        int64
	MemberBerths int  // active member berths (base of percent quotas)
	HasAreas     bool // percent quotas are refused for groups with dynamic areas
}

// QuotaSet changes the quota of one group.
type QuotaSet struct {
	GroupID string
	Kind    string
	Value   int64
}

// GroupUsage is the usage of one group over a range of nights (missing nights have no row: used 0).
type GroupUsage struct {
	GroupID string
	Nights  []QuotaNight
}

// QuotaCap is the cap of a quota for a night: count = value, percent = floor(value × active member berths / 100),
// none = no cap (-1). It mirrors policy.QuotaCap (m8-plan §3.3, §16 D5).
func QuotaCap(kind string, value int64, memberBerths int) int {
	switch kind {
	case store.QuotaCount:
		return int(value)
	case store.QuotaPercent:
		return int(value * int64(memberBerths) / 100)
	}
	return -1
}

func (s *Service) quotas(ctx context.Context, db bun.IDB, scope store.MarinaScope) ([]Quota, error) {
	groups, err := store.ListBerthGroups(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	berths, err := store.ActiveMemberBerths(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	areas, err := store.GroupsWithAreas(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	out := make([]Quota, 0, len(groups))
	for _, g := range groups {
		var name LongText
		_ = json.Unmarshal(g.Name, &name) // an unreadable name shows empty
		kind := g.OnlineQuotaKind
		if kind == "" {
			kind = store.QuotaNone
		}
		out = append(out, Quota{
			GroupID: g.ID, GroupCode: g.Code, GroupName: name, Kind: kind, Value: g.OnlineQuotaValue,
			MemberBerths: berths[g.ID], HasAreas: areas[g.ID],
		})
	}
	return out, nil
}

// Quotas returns the online quotas of the berth groups of the marina (harbor.configure).
func (s *Service) Quotas(ctx context.Context, p *auth.Principal, marinaID string) ([]Quota, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	return s.quotas(ctx, s.d.DB.DB, scope)
}

// UpdateQuotas changes the quotas of some groups (harbor.configure) and rewrites the cap of the usage rows of the
// future nights as max(new cap, used): a quota never drops below what is already sold. Groups containing dynamic
// areas accept only count quotas (m8-plan §16 D5).
func (s *Service) UpdateQuotas(ctx context.Context, p *auth.Principal, marinaID string, in []QuotaSet) ([]Quota, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	var out []Quota
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		current, err := s.quotas(ctx, tx, scope)
		if err != nil {
			return err
		}
		byID := map[string]Quota{}
		for _, q := range current {
			byID[q.GroupID] = q
		}
		v := &auth.ValidationError{}
		seen := map[string]bool{}
		for _, q := range in {
			g, ok := byID[q.GroupID]
			switch {
			case !ok:
				v.Add("items.group_id", "unknown")
			case seen[q.GroupID]:
				v.Add("items.group_id", "duplicate")
			case q.Kind != store.QuotaNone && q.Kind != store.QuotaCount && q.Kind != store.QuotaPercent:
				v.Add("items.kind", "invalid")
			case q.Value < 0 || (q.Kind == store.QuotaPercent && q.Value > 100) || q.Value > 100000:
				v.Add("items.value", "out_of_range")
			case q.Kind == store.QuotaPercent && g.HasAreas:
				v.Add("items.kind", "percent_with_areas")
			}
			seen[q.GroupID] = true
		}
		if err := v.Err(); err != nil {
			return err
		}
		m, err := store.GetMarina(ctx, tx, scope)
		if err != nil {
			return err
		}
		loc, err := calendar.LoadLocation(m.Timezone)
		if err != nil {
			return err
		}
		now := s.d.Clock.Now()
		today := calendar.In(now, loc)
		for _, q := range in {
			value := q.Value
			if q.Kind == store.QuotaNone {
				value = 0
			}
			if err := store.UpdateOnlineQuota(ctx, tx, scope, q.GroupID, q.Kind, value, now); err != nil {
				return err
			}
			if c := QuotaCap(q.Kind, value, byID[q.GroupID].MemberBerths); c >= 0 {
				if err := store.RecapOnlineQuotaUsage(ctx, tx, scope, q.GroupID, today, c, now); err != nil {
					return err
				}
			}
		}
		if s.d.Audit != nil && len(in) > 0 {
			items := make([]map[string]any, 0, len(in))
			for _, q := range in {
				items = append(items, map[string]any{"group_id": q.GroupID, "kind": q.Kind, "value": q.Value})
			}
			if err := s.d.Audit.Write(ctx, tx, audit.Entry{
				OrganizationID: scope.OrgID(), MarinaID: scope.ID(), Actor: p.Actor(), Action: "online_quotas.update",
				EntityType: "online_quotas", EntityID: scope.ID(), Diff: map[string]any{"items": items},
			}); err != nil {
				return err
			}
		}
		out, err = s.quotas(ctx, tx, scope)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// QuotaUsage returns the usage of every group with a quota for the nights from..to inclusive (harbor.configure).
// Missing bounds, an inverted range or more than MaxUsageDays nights are a validation error.
func (s *Service) QuotaUsage(ctx context.Context, p *auth.Principal, marinaID string, from, to calendar.Date) ([]GroupUsage, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if from.IsZero() {
		v.Add("from", "required")
	}
	if to.IsZero() {
		v.Add("to", "required")
	}
	if !from.IsZero() && !to.IsZero() {
		if to.Before(from) {
			v.Add("to", "before_from")
		} else if from.DaysUntil(to)+1 > MaxUsageDays {
			v.Add("to", "range_too_long")
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	qs, err := s.quotas(ctx, s.d.DB.DB, scope)
	if err != nil {
		return nil, err
	}
	rows, err := store.OnlineQuotaUsageRange(ctx, s.d.DB.DB, scope, from, to)
	if err != nil {
		return nil, err
	}
	byKey := map[string]store.OnlineQuotaUsage{}
	for _, r := range rows {
		byKey[r.GroupID+"|"+r.Night.String()] = r
	}
	out := []GroupUsage{}
	for _, q := range qs {
		if q.Kind == store.QuotaNone {
			continue
		}
		defCap := QuotaCap(q.Kind, q.Value, q.MemberBerths)
		g := GroupUsage{GroupID: q.GroupID, Nights: []QuotaNight{}}
		for _, d := range calendar.Range(from, to) {
			n := QuotaNight{Night: d, Cap: defCap}
			if r, ok := byKey[q.GroupID+"|"+d.String()]; ok {
				n.Cap, n.Used = r.Cap, r.Used
			}
			g.Nights = append(g.Nights, n)
		}
		out = append(out, g)
	}
	return out, nil
}
