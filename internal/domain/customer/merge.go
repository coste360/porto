package customer

import (
	"context"
	"slices"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// maxMergeSources is the largest number of duplicates merged in one request.
const maxMergeSources = 10

// Merge folds the source customers into the target (customers.write, If-Match on the target), in one
// transaction (m1-plan §4.4): vessel parties move to the target (a role the target already holds on the same
// vessel is dropped, owner shares are added up to 1000‰), documents, deadlines and consents move to the
// target, and each source gets merged_into_id and archived_at.
func (s *Service) Merge(ctx context.Context, p *auth.Principal, marinaID, targetID string, ifMatch int, sourceIDs []string) (*Customer, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermCustomersWrite)
	if err != nil {
		return nil, err
	}
	var out *Customer
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		target, err := store.GetCustomer(ctx, tx, scope, targetID)
		if err != nil {
			return notFound(err)
		}
		var uniq []string
		for _, id := range sourceIDs {
			if !slices.Contains(uniq, id) {
				uniq = append(uniq, id)
			}
		}
		if len(uniq) == 0 || len(uniq) > maxMergeSources || slices.Contains(uniq, target.ID) {
			return &auth.ValidationError{Fields: []auth.FieldError{{Field: "source_ids", Code: "invalid"}}}
		}
		if target.Version != ifMatch {
			return ErrVersionMismatch
		}
		if err := modifiable(target); err != nil {
			return err
		}
		sources, err := store.CustomersByIDs(ctx, tx, scope, uniq)
		if err != nil {
			return err
		}
		if len(sources) != len(uniq) {
			return auth.ErrNotFound
		}
		for i := range sources {
			if err := modifiable(&sources[i]); err != nil {
				return err
			}
		}
		now := s.now()
		if err := s.moveParties(ctx, tx, scope, target.ID, uniq, now); err != nil {
			return err
		}
		if err := store.MoveCustomerRecords(ctx, tx, scope, uniq, target.ID, now); err != nil {
			return err
		}
		for _, id := range uniq {
			if err := store.MarkCustomerMerged(ctx, tx, scope, id, target.ID, now); err != nil {
				return err
			}
			if err := s.writeAudit(ctx, tx, scope, p, "customer.merged_into", id, map[string]any{"merged_into_id": target.ID}); err != nil {
				return err
			}
		}
		target.UpdatedAt = now
		if err := store.UpdateCustomer(ctx, tx, scope, ifMatch, target); err != nil {
			return versionErr(err)
		}
		slices.Sort(uniq)
		if err := s.writeAudit(ctx, tx, scope, p, "customer.merge", target.ID, map[string]any{"source_ids": uniq}); err != nil {
			return err
		}
		if _, err := s.d.Outbox.Write(ctx, tx, &scope, event.CustomerMerged, event.CustomerMergedData{CustomerID: target.ID, SourceIDs: uniq}); err != nil {
			return err
		}
		c := s.toCustomer(target)
		out = &c
		return nil
	})
	return out, err
}

// moveParties re-points the vessel parties of the sources to the target. When the target (or an earlier
// source) already holds the role on the vessel, the duplicate row is deleted; owner shares are summed, capped
// at 1000‰.
func (s *Service) moveParties(ctx context.Context, tx bun.IDB, scope store.MarinaScope, targetID string, sourceIDs []string, now time.Time) error {
	parties, err := store.PartiesOfCustomers(ctx, tx, scope, append([]string{targetID}, sourceIDs...))
	if err != nil {
		return err
	}
	type key struct{ vessel, role string }
	kept := map[key]*store.VesselParty{}
	for i := range parties {
		if parties[i].CustomerID == targetID {
			kept[key{parties[i].VesselID, parties[i].Role}] = &parties[i]
		}
	}
	changedVessels := map[string]bool{}
	for i := range parties {
		pr := &parties[i]
		if pr.CustomerID == targetID {
			continue
		}
		k := key{pr.VesselID, pr.Role}
		changedVessels[pr.VesselID] = true
		if existing, ok := kept[k]; ok {
			if err := store.DeleteVesselParty(ctx, tx, scope, pr.ID); err != nil {
				return err
			}
			if pr.Role == "owner" {
				existing.ShareMillis = min(existing.ShareMillis+pr.ShareMillis, 1000)
				if err := store.UpdateVesselParty(ctx, tx, scope, existing.ID, targetID, existing.ShareMillis, now); err != nil {
					return err
				}
			}
			continue
		}
		if err := store.UpdateVesselParty(ctx, tx, scope, pr.ID, targetID, pr.ShareMillis, now); err != nil {
			return err
		}
		pr.CustomerID = targetID
		kept[k] = pr
	}
	for id := range changedVessels {
		if err := store.BumpVesselVersion(ctx, tx, scope, id, now); err != nil {
			return err
		}
	}
	return nil
}
