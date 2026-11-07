package pricelist

import (
	"context"
	"slices"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/catalog"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/store"
)

// Activation (spec 12.2; INV-24: at most one active list per night and contract type) and archive.

// Truncated is a predecessor whose valid_to an activation shortened (ValidTo is the new value).
type Truncated struct {
	ID        string        `json:"id"`
	Code      string        `json:"code"`
	ValidFrom calendar.Date `json:"valid_from"`
	ValidTo   calendar.Date `json:"valid_to"`
}

// ActivateResult is the answer to an activation.
type ActivateResult struct {
	Document  *Document   `json:"document"`
	Truncated []Truncated `json:"truncated"`
}

func sharesContractType(a, b store.Strings) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// Activate activates a valid draft from effectiveFrom (zero = its valid_from) when its version equals ifMatch
// (pricing.configure). Every other active list sharing a contract type and overlapping [effectiveFrom, valid_to]
// is truncated to the night before effectiveFrom when it starts earlier; otherwise the activation is refused
// with the conflicts. The transaction starts by bumping pricing_settings.activation_seq, which serialises
// activations of the marina.
func (s *Service) Activate(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, effectiveFrom calendar.Date) (*ActivateResult, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	var out ActivateResult
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if _, err := catalog.EnsureSettings(ctx, tx, scope, s.defaultSettingsRow); err != nil {
			return err
		}
		if err := store.BumpActivationSeq(ctx, tx, scope); err != nil {
			return err
		}
		l, err := store.GetPriceList(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if l.Status != store.PriceListDraft {
			return ErrImmutable
		}
		if l.Version != ifMatch {
			return ErrVersionMismatch
		}
		eff := effectiveFrom
		if eff.IsZero() {
			eff = l.ValidFrom
		}
		if eff.Before(l.ValidFrom) || eff.After(l.ValidTo) {
			v := &auth.ValidationError{}
			v.Add("effective_from", "outside_validity")
			return v
		}
		l.ValidFrom = eff
		children, err := store.LoadPriceListChildren(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		cat, err := catalogOf(ctx, tx, scope, l, children)
		if err != nil {
			return err
		}
		issues, err := validate(ctx, tx, scope, cat)
		if err != nil {
			return err
		}
		if hasErrors(issues) {
			return &InvalidError{Issues: errorsOnly(issues)}
		}
		actives, err := store.ActivePriceLists(ctx, tx, scope)
		if err != nil {
			return err
		}
		var conflicts []Conflict
		var truncate []store.PriceList
		for _, a := range actives {
			if a.ID == l.ID || !sharesContractType(a.ContractTypes, l.ContractTypes) {
				continue
			}
			if a.ValidTo.Before(eff) || a.ValidFrom.After(l.ValidTo) {
				continue
			}
			if a.ValidFrom.Before(eff) {
				truncate = append(truncate, a)
				continue
			}
			conflicts = append(conflicts, Conflict{ID: a.ID, Code: a.Code, ValidFrom: a.ValidFrom, ValidTo: a.ValidTo})
		}
		if len(conflicts) > 0 {
			return &OverlapError{Conflicts: conflicts}
		}
		now := s.now()
		out.Truncated = []Truncated{}
		ids := []string{}
		for _, a := range truncate {
			to := eff.AddDays(-1)
			if err := store.TruncatePriceList(ctx, tx, scope, a.ID, to, now); err != nil {
				return mapListErr(err)
			}
			out.Truncated = append(out.Truncated, Truncated{ID: a.ID, Code: a.Code, ValidFrom: a.ValidFrom, ValidTo: to})
			ids = append(ids, a.ID)
			if err := s.d.Audit.Write(ctx, tx, auditEntry(p, scope, "price_list.truncate", a.ID, map[string]any{
				"valid_to": to.String(), "successor_id": l.ID,
			})); err != nil {
				return err
			}
		}
		// Snapshot the engine fields of the referenced items while the list is still a draft (the triggers freeze
		// the rows of a published list): from now on the list prices with them, and the hash below covers them, so
		// content_sha256 = CatalogHash(LoadCatalog(list)) however the live items change (INV-26).
		if err := store.InsertPriceListItems(ctx, tx, scope, l.ID, snapshotRows(cat, now)); err != nil {
			return mapListErr(err)
		}
		l.Status, l.ContentSHA256, l.ActivatedAt, l.UpdatedAt = store.PriceListActive, catalogHash(cat), now, now
		if p != nil && p.Kind == auth.KindUser {
			l.ActivatedByUserID = p.UserID
		}
		if err := store.UpdatePriceListStatus(ctx, tx, scope, l, ifMatch); err != nil {
			return mapListErr(err)
		}
		if out.Document, err = loadDocument(ctx, tx, scope, id); err != nil {
			return err
		}
		return s.write(ctx, tx, p, scope, "price_list.activate", id, map[string]any{
			"valid_from": eff.String(), "truncated": len(ids), "version": l.Version,
		}, event.PricingChangedData{PriceListID: id, Version: l.Version, Change: event.ChangeActivated, TruncatedIDs: ids})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Archive archives an active list when its version equals ifMatch (pricing.configure). A draft cannot be
// archived (delete it: conflict); an archived list is immutable.
func (s *Service) Archive(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Document, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermPricingConfigure)
	if err != nil {
		return nil, err
	}
	var out *Document
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		l, err := store.GetPriceList(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		switch l.Status {
		case store.PriceListDraft:
			return store.ErrConflict
		case store.PriceListArchived:
			return ErrImmutable
		}
		if l.Version != ifMatch {
			return ErrVersionMismatch
		}
		now := s.now()
		l.Status, l.ArchivedAt, l.UpdatedAt = store.PriceListArchived, now, now
		if err := store.UpdatePriceListStatus(ctx, tx, scope, l, ifMatch); err != nil {
			return mapListErr(err)
		}
		if out, err = loadDocument(ctx, tx, scope, id); err != nil {
			return err
		}
		return s.write(ctx, tx, p, scope, "price_list.archive", id, map[string]any{"version": l.Version},
			event.PricingChangedData{PriceListID: id, Version: l.Version, Change: event.ChangeArchived})
	})
	return out, err
}

func (s *Service) defaultSettingsRow() store.PricingSettings {
	now := s.now()
	return store.PricingSettings{
		ID: store.NewID(), DefaultVATRateBP: catalog.DefaultVATRateBP, DefaultPriceIncludesVAT: catalog.DefaultPriceIncludesVAT,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}
