package occupancy

import (
	"context"
	"errors"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/booking/policy"
	"github.com/coste360/porto/internal/domain/occupancy/engine"
	"github.com/coste360/porto/internal/store"
)

// Online quotas (spec 10.8; m8-plan §3.3, §16 D3-D5). Quotas are DB counters: online_quota_usage holds one row per
// (group, night) with CHECK (used >= 0 AND used <= cap), and a claim is the conditional
// UPDATE ... SET used = used + 1 WHERE used < cap (zero rows = exhausted), never a read-then-write in Go (INV-12).
// Only the channels online and external claim; desk and API ignore quotas. Claims follow the contract, not the
// berth: desk moves keep them; cancel, expire, reject, no-show and shortened stays release them.

// Channels that are subject to online quotas (contracts.channel values).
const (
	ChannelOnline   = "online"
	ChannelExternal = "external"
)

var (
	// ErrQuotaExhausted means an online quota of a group containing the resource is used up on some night. The
	// caller rolls the transaction back and tries the next solution; the public API maps it to 409
	// booking_unavailable.
	ErrQuotaExhausted = errors.New("occupancy: online quota exhausted")
	// ErrNotImplementedM8 is kept for callers compiled against the CONTRACT skeleton; nothing returns it.
	ErrNotImplementedM8 = errors.New("occupancy: not implemented (M8)")
)

// QuotaChannel reports whether contracts of the channel claim online quotas.
func QuotaChannel(channel string) bool { return channel == ChannelOnline || channel == ChannelExternal }

// QuotaResource names the resource whose groups are claimed (the resource of the chosen solution).
type QuotaResource struct {
	ResourceType string // berth, dynamic_area (land resources are never sold online)
	ResourceID   string
}

// ClaimQuotaTx claims one unit of every online quota group containing res for each night of [first, last]
// (inclusive) for contractID, inside the hold transaction and before AllocateTx. For each (group, night): insert
// the usage row with the cap computed now (ON CONFLICT DO NOTHING), then the conditional increment; zero rows
// affected is ErrQuotaExhausted. Then the claim rows are inserted. Channels other than online and external are a
// no-op.
func (s *Service) ClaimQuotaTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string,
	res QuotaResource, first, last calendar.Date, channel string,
) error {
	if !QuotaChannel(channel) || last.Before(first) {
		return nil
	}
	groups, err := store.QuotaGroupsOfResource(ctx, tx, scope, res.ResourceType, res.ResourceID)
	if err != nil {
		return err
	}
	now := s.now()
	for _, g := range groups {
		capNow := policy.QuotaCap(g.Kind, g.Value, g.MemberBerths)
		if capNow < 0 {
			continue
		}
		for d := first; !d.After(last); d = d.AddDays(1) {
			ok, err := store.ClaimQuotaNight(ctx, tx, scope, contractID, g.ID, d, capNow, now)
			if err != nil {
				return err
			}
			if !ok {
				return ErrQuotaExhausted
			}
		}
	}
	return nil
}

// ReleaseQuotaTx releases the claims of contractID: all of them when fromNight is nil, otherwise the nights on or
// after *fromNight (a shortened stay). Each released claim decrements its usage row and is deleted, in tx.
func (s *Service) ReleaseQuotaTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string,
	fromNight *calendar.Date,
) error {
	claims, err := store.QuotaClaimsOf(ctx, tx, scope, contractID, fromNight)
	if err != nil || len(claims) == 0 {
		return err
	}
	_, err = store.ReleaseQuotaClaims(ctx, tx, scope, claims, s.now())
	return err
}

// ReleaseQuotaNightsTx releases the claims of contractID on the nights before *beforeNight (a stay whose arrival
// moved later). Used by stay changes together with ReleaseQuotaTx.
func (s *Service) ReleaseQuotaNightsTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string,
	beforeNight calendar.Date,
) error {
	claims, err := store.QuotaClaimsOf(ctx, tx, scope, contractID, nil)
	if err != nil {
		return err
	}
	var drop []store.OnlineQuotaClaim
	for _, c := range claims {
		if c.Night.Before(beforeNight) {
			drop = append(drop, c)
		}
	}
	if len(drop) == 0 {
		return nil
	}
	_, err = store.ReleaseQuotaClaims(ctx, tx, scope, drop, s.now())
	return err
}

// SearchScoped runs a search in an already resolved marina scope, without a principal: the public booking page
// and the channels search with channel online or external (m8-plan §3.2). The input is validated as in Search.
func (s *Service) SearchScoped(ctx context.Context, scope store.MarinaScope, in SearchInput) (*SearchResult, error) {
	if !QuotaChannel(in.Channel) {
		return nil, errors.New("occupancy: SearchScoped needs channel online or external")
	}
	v := &auth.ValidationError{}
	if in.Purpose == "" {
		in.Purpose = engine.PurposeTransit
	}
	if in.Purpose != engine.PurposeTransit {
		v.Add("purpose", "invalid")
	}
	if !in.Departure.After(in.Arrival) {
		v.Add("departure", "before_arrival")
	} else if engine.Nights(in.Arrival, in.Departure.AddDays(-1)) > 400 {
		v.Add("departure", "too_long")
	}
	if in.VesselID == "" && in.Vessel == nil {
		v.Add("vessel_id", "required")
	}
	if len(v.Fields) > 0 {
		return nil, v
	}
	in.ConsiderOut, in.ResourceTypes = false, []engine.ResourceType{engine.ResourceBerth, engine.ResourceArea}
	return s.search(ctx, scope, in)
}

// fullQuotaGroups returns the groups whose online quota has no unit left on at least one night of [first, last]
// (a missing usage row means used = 0), read in the search's read transaction.
func fullQuotaGroups(ctx context.Context, tx bun.IDB, scope store.MarinaScope, first, last calendar.Date) (map[string]bool, error) {
	groups, err := store.QuotaGroups(ctx, tx, scope)
	if err != nil || len(groups) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(groups))
	caps := map[string]int{}
	out := map[string]bool{}
	for _, g := range groups {
		c := policy.QuotaCap(g.Kind, g.Value, g.MemberBerths)
		if c < 0 {
			continue
		}
		if c == 0 {
			out[g.ID] = true
		}
		caps[g.ID] = c
		ids = append(ids, g.ID)
	}
	usage, err := store.QuotaUsage(ctx, tx, scope, ids, first, last)
	if err != nil {
		return nil, err
	}
	for _, u := range usage {
		if u.Used >= u.Cap {
			out[u.GroupID] = true
		}
	}
	return out, nil
}
