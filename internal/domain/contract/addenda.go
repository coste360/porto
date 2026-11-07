package contract

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/occupancy"
	"github.com/coste360/porto/internal/store"
)

// Addenda (spec 13.4; m4-plan §3.5): a change of a contract after its signature with its own document and
// signature. The price effect is computed when the addendum is created (priced before the transaction) and
// applied at activation in one transaction: lines, allocations, billing customer, an INV-27 adjustment of the
// total and a replan of the scheduled instalments. The contract, its document and its signatures never change.

const entityAddendum = "addendum"

// Addendum kinds.
const (
	AddServiceAdd      = "service_add"
	AddServiceRemove   = "service_remove"
	AddParking         = "parking"
	AddBillingCustomer = "billing_customer"
	AddPriceChange     = "price_change"
	AddVesselChange    = "vessel_change"
)

var addendumKinds = []string{AddServiceAdd, AddServiceRemove, AddParking, AddBillingCustomer, AddPriceChange, AddVesselChange}

// AddendumPayload is the kind-specific data of an addendum.
type AddendumPayload struct {
	ItemID         string `json:"item_id,omitempty"`
	QtyMilli       int64  `json:"qty_milli,omitempty"`
	LineID         string `json:"line_id,omitempty"`
	LandResourceID string `json:"land_resource_id,omitempty"`
	CustomerID     string `json:"customer_id,omitempty"`
	NewTotalCents  *int64 `json:"new_total_cents,omitempty"`
	Reason         string `json:"reason,omitempty"`
	VesselID       string `json:"vessel_id,omitempty"`
	// Computed at creation (the price effect).
	OldRemainingCents int64  `json:"old_remaining_cents"`
	NewRemainingCents int64  `json:"new_remaining_cents"`
	FitOverride       string `json:"fit_override,omitempty"` // reason, when the fit was overridden
}

// AddendumInput creates an addendum.
type AddendumInput struct {
	Kind        string
	EffectiveOn calendar.Date
	Payload     AddendumPayload
	FitOverride *FitOverrideInput
}

// Addendum is an addendum with its decoded payload.
type Addendum struct {
	Row         store.ContractAddendum
	Payload     AddendumPayload
	Replanned   int
	PriceEffect bool
}

func decodeAddendum(a *store.ContractAddendum) Addendum {
	out := Addendum{Row: *a}
	_ = json.Unmarshal(a.Payload, &out.Payload)
	out.PriceEffect = a.Kind != AddBillingCustomer
	return out
}

// ListAddenda returns the addenda of a contract (contracts.read).
func (s *Service) ListAddenda(ctx context.Context, p *auth.Principal, marinaID, contractID string) ([]Addendum, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsRead)
	if err != nil {
		return nil, err
	}
	c, err := store.GetContract(ctx, s.d.DB.DB, scope, contractID)
	if err != nil || c.Kind == KindTransit {
		return nil, auth.ErrNotFound
	}
	rows, err := store.AddendaOfContract(ctx, s.d.DB.DB, scope, contractID)
	if err != nil {
		return nil, err
	}
	out := make([]Addendum, 0, len(rows))
	for i := range rows {
		out = append(out, decodeAddendum(&rows[i]))
	}
	return out, nil
}

// remainingPrice quotes the nights [from, ends) of a contract with a vessel and services.
func (s *Service) remainingPrice(ctx context.Context, p *auth.Principal, marinaID string, scope store.MarinaScope, c *store.Contract,
	from calendar.Date, vesselID string, services []ServiceRequest,
) (int64, error) {
	allocs, err := contractAllocs(ctx, s.d.DB.DB, scope, c.ID)
	if err != nil {
		return 0, err
	}
	var seg *Segment
	for i := range allocs {
		a := &allocs[i]
		if a.Status == store.AllocActive && a.ResourceType != store.ResourceLand && !a.LastNight.Before(from) {
			g := segmentOf(a)
			seg = &g
			break
		}
	}
	if seg == nil {
		return 0, &TransitionError{From: c.Status, Action: ActionAddendum}
	}
	pr, err := s.price(ctx, p, marinaID, priceRequest{
		contractType: PricingType(c.Kind, c.Term), arrival: from, departure: c.EndsOn, first: *seg, vesselID: vesselID,
		customerID: c.CustomerID, channel: ChannelDesk, services: services,
	})
	if err != nil {
		return 0, err
	}
	return pr.total(), nil
}

func serviceRequests(lines []store.ContractLine, skipLine string) []ServiceRequest {
	var out []ServiceRequest
	for _, l := range lines {
		if l.LineKind == "service" && l.ID != skipLine {
			out = append(out, ServiceRequest{ItemID: l.ItemID, QtyMilli: l.QtyMilli})
		}
	}
	return out
}

// CreateAddendum creates a draft addendum and computes its price effect (contracts.write; a fit override of a
// vessel change also occupancy.override_fit).
func (s *Service) CreateAddendum(ctx context.Context, p *auth.Principal, marinaID, contractID string, in AddendumInput) (*Addendum, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeOverride(ctx, p, marinaID, overrideReason(in.FitOverride)); err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	c, err := store.GetContract(ctx, db, scope, contractID)
	if err != nil || c.Kind == KindTransit {
		return nil, auth.ErrNotFound
	}
	if _, err := Next(MooringTransitions, Status(c.Status), ActionAddendum); err != nil {
		return nil, err
	}
	v := &auth.ValidationError{}
	if !contains(addendumKinds, in.Kind) {
		v.Add("kind", "invalid")
	}
	if in.EffectiveOn.Before(c.StartsOn) || !in.EffectiveOn.Before(c.EndsOn) {
		v.Add("effective_on", "outside_contract")
	}
	pl := in.Payload
	pl.OldRemainingCents, pl.NewRemainingCents = 0, 0
	switch in.Kind {
	case AddServiceAdd:
		if !store.ValidID(pl.ItemID) {
			v.Add("payload.item_id", "invalid")
		}
		if pl.QtyMilli <= 0 {
			pl.QtyMilli = 1000
		}
	case AddServiceRemove:
		if !store.ValidID(pl.LineID) {
			v.Add("payload.line_id", "invalid")
		}
	case AddParking:
		if !store.ValidID(pl.LandResourceID) {
			v.Add("payload.land_resource_id", "invalid")
		}
	case AddBillingCustomer:
		if !store.ValidID(pl.CustomerID) {
			v.Add("payload.customer_id", "invalid")
		}
	case AddPriceChange:
		if pl.NewTotalCents == nil || *pl.NewTotalCents < 0 {
			v.Add("payload.new_total_cents", "invalid")
		}
		if r, _ := cleanText(pl.Reason, 500); r == "" {
			v.Add("payload.reason", "required")
		}
	case AddVesselChange:
		if !store.ValidID(pl.VesselID) || pl.VesselID == c.VesselID {
			v.Add("payload.vessel_id", "invalid")
		}
	}
	if in.FitOverride != nil {
		if r := cleanReason(in.FitOverride); r == "" {
			v.Add("fit_override.reason", "required")
		} else {
			pl.FitOverride = r
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	lines, err := store.LinesOfContracts(ctx, db, scope, []string{c.ID})
	if err != nil {
		return nil, err
	}
	var delta int64
	priced := func(vesselID string, services []ServiceRequest) error {
		old, err := s.remainingPrice(ctx, p, marinaID, scope, c, in.EffectiveOn, c.VesselID, serviceRequests(lines, ""))
		if err != nil {
			return err
		}
		nw, err := s.remainingPrice(ctx, p, marinaID, scope, c, in.EffectiveOn, vesselID, services)
		if err != nil {
			return err
		}
		pl.OldRemainingCents, pl.NewRemainingCents, delta = old, nw, nw-old
		return nil
	}
	switch in.Kind {
	case AddServiceAdd:
		err = priced(c.VesselID, append(serviceRequests(lines, ""), ServiceRequest{ItemID: pl.ItemID, QtyMilli: pl.QtyMilli}))
	case AddServiceRemove:
		found := false
		for _, l := range lines {
			found = found || (l.ID == pl.LineID && l.LineKind == "service")
		}
		if !found {
			v.Add("payload.line_id", "invalid")
			return nil, v
		}
		err = priced(c.VesselID, serviceRequests(lines, pl.LineID))
	case AddParking:
		if ok, e := store.ResourceExists(ctx, db, scope, store.ResourceLand, pl.LandResourceID); e != nil || !ok {
			v.Add("payload.land_resource_id", "invalid")
			return nil, v
		}
		if pl.ItemID != "" {
			err = priced(c.VesselID, append(serviceRequests(lines, ""), ServiceRequest{ItemID: pl.ItemID, QtyMilli: 1000}))
		}
	case AddBillingCustomer:
		err = checkParties(ctx, db, scope, "", pl.CustomerID)
	case AddPriceChange:
		delta = *pl.NewTotalCents - c.TotalCents
		pl.OldRemainingCents, pl.NewRemainingCents = c.TotalCents, *pl.NewTotalCents
	case AddVesselChange:
		if err = checkParties(ctx, db, scope, pl.VesselID, c.CustomerID); err == nil {
			err = priced(pl.VesselID, serviceRequests(lines, ""))
		}
	}
	if err != nil {
		return nil, err
	}
	if c.TotalCents+delta < 0 {
		return nil, ErrRepriceRefused
	}
	a := actor{p}
	var out *store.ContractAddendum
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, contractID, KindMooring, 0)
		if err != nil {
			return err
		}
		seq, err := store.NextAddendumSeq(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		now := s.now()
		out = &store.ContractAddendum{
			ID: store.NewID(), MarinaID: scope.ID(), ContractID: c.ID, Seq: seq, Kind: in.Kind, EffectiveOn: in.EffectiveOn,
			Payload: store.MustJSON(pl), AmountDeltaCents: delta, Status: string(AddendumDraft), CreatedByUserID: a.userID(),
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertAddendum(ctx, tx, out); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrVersionMismatch
			}
			return err
		}
		return s.writeAudit(ctx, tx, scope, a, "addendum.create", entityAddendum, out.ID, map[string]any{
			"contract_id": c.ID, "kind": in.Kind, "effective_on": in.EffectiveOn, "amount_delta_cents": delta, "seq": seq,
		})
	})
	if err != nil {
		return nil, err
	}
	d := decodeAddendum(out)
	return &d, nil
}

// nextAddendum returns the transition of the addendum table.
func nextAddendum(from AddendumStatus, action Action) (AddendumStatus, error) {
	for _, t := range AddendumTransitions {
		if t.From == from && t.Action == action {
			return t.To, nil
		}
	}
	return "", &TransitionError{From: string(from), Action: action}
}

// onAddendum runs fn on a locked contract and addendum (contracts.write, If-Match on the addendum).
func (s *Service) onAddendum(ctx context.Context, p *auth.Principal, marinaID, contractID, id string, ifMatch int,
	fn func(ctx context.Context, tx bun.Tx, scope store.MarinaScope, a actor, c *store.Contract, ad *store.ContractAddendum) error,
) (*Addendum, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermContractsWrite)
	if err != nil {
		return nil, err
	}
	if err := s.requireAllocator(); err != nil {
		return nil, err
	}
	a := actor{p}
	var out *store.ContractAddendum
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		c, err := lockContract(ctx, tx, scope, contractID, KindMooring, 0)
		if err != nil {
			return err
		}
		ad, err := store.LockAddendum(ctx, tx, scope, contractID, id)
		if err != nil {
			return notFound(err)
		}
		if ifMatch != 0 && ad.Version != ifMatch {
			return ErrVersionMismatch
		}
		out = ad
		return fn(ctx, tx, scope, a, c, ad)
	})
	if err != nil {
		return nil, err
	}
	d := decodeAddendum(out)
	return &d, nil
}

// RequestAddendumSignature composes the addendum document and opens its signature request.
func (s *Service) RequestAddendumSignature(ctx context.Context, p *auth.Principal, marinaID, contractID, id string, ifMatch int, in SignatureInput) (*Addendum, error) {
	if err := validateSignatureInput(in); err != nil {
		return nil, err
	}
	return s.onAddendum(ctx, p, marinaID, contractID, id, ifMatch, func(ctx context.Context, tx bun.Tx, scope store.MarinaScope, a actor, c *store.Contract, ad *store.ContractAddendum) error {
		to, err := nextAddendum(AddendumStatus(ad.Status), ActionRequestSignature)
		if err != nil {
			return err
		}
		cs, err := s.settingsTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		reqID, docID, _, err := s.requestSignatureTx(ctx, tx, scope, a, cs, signatureSpec{
			subjectType: SubjectAddendum, subjectID: ad.ID, docKind: docs.KindAddendum, docSubject: docs.SubjectAddendum,
			customerID: c.CustomerID, in: in,
		})
		if err != nil {
			return err
		}
		from := ad.Status
		ad.Status, ad.SignatureRequestID, ad.DocumentID = string(to), reqID, docID
		return s.saveAddendumTx(ctx, tx, scope, a, c, ad, "addendum.request_signature", map[string]any{
			"from_status": from, "status": ad.Status, "signature_request_id": reqID, "document_id": docID,
		}, event.ChangeSignatureRequested)
	})
}

// ActivateAddendum applies an addendum signed on paper (contracts.write, If-Match; the paper scan rule of
// contracts applies).
func (s *Service) ActivateAddendum(ctx context.Context, p *auth.Principal, marinaID, contractID, id string, ifMatch int, in ActivateInput) (*Addendum, error) {
	return s.onAddendum(ctx, p, marinaID, contractID, id, ifMatch, func(ctx context.Context, tx bun.Tx, scope store.MarinaScope, a actor, c *store.Contract, ad *store.ContractAddendum) error {
		to, err := nextAddendum(AddendumStatus(ad.Status), ActionActivate)
		if err != nil {
			return err
		}
		cs, err := s.settingsTx(ctx, tx, scope)
		if err != nil {
			return err
		}
		if in.PaperScanFileID != "" {
			if _, err := store.GetStoredFile(ctx, tx, scope, in.PaperScanFileID); err != nil {
				v := &auth.ValidationError{}
				v.Add("paper_scan_file_id", "invalid")
				return v
			}
		} else if cs.Stored && cs.RequirePaperScan {
			return ErrPaperScanRequired
		}
		if ad.Status == string(AddendumAwaitingSignature) && s.signer != nil {
			if err := s.signer.CancelTx(ctx, tx, scope, SubjectAddendum, ad.ID); err != nil {
				return err
			}
		}
		return s.applyAddendumTx(ctx, tx, scope, a, c, ad, to, map[string]any{"paper_scan_file_id": in.PaperScanFileID})
	})
}

// CancelAddendum cancels a draft or awaiting addendum (contracts.write, If-Match).
func (s *Service) CancelAddendum(ctx context.Context, p *auth.Principal, marinaID, contractID, id string, ifMatch int) (*Addendum, error) {
	return s.onAddendum(ctx, p, marinaID, contractID, id, ifMatch, func(ctx context.Context, tx bun.Tx, scope store.MarinaScope, a actor, c *store.Contract, ad *store.ContractAddendum) error {
		to, err := nextAddendum(AddendumStatus(ad.Status), ActionCancel)
		if err != nil {
			return err
		}
		if ad.Status == string(AddendumAwaitingSignature) && s.signer != nil {
			if err := s.signer.CancelTx(ctx, tx, scope, SubjectAddendum, ad.ID); err != nil {
				return err
			}
		}
		from := ad.Status
		ad.Status, ad.CancelledAt = string(to), s.now()
		return s.saveAddendumTx(ctx, tx, scope, a, c, ad, "addendum.cancel", map[string]any{"from_status": from, "status": ad.Status}, "")
	})
}

// saveAddendumTx writes the addendum, its audit row and (with change) contract.changed.
func (s *Service) saveAddendumTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract,
	ad *store.ContractAddendum, auditAction string, diff map[string]any, change string,
) error {
	if err := store.UpdateAddendum(ctx, tx, scope, ad.Version, ad, s.now()); err != nil {
		return versionErr(err)
	}
	diff["contract_id"] = c.ID
	if err := s.writeAudit(ctx, tx, scope, a, auditAction, entityAddendum, ad.ID, diff); err != nil {
		return err
	}
	if change == "" {
		return nil
	}
	return s.emit(ctx, tx, scope, event.ContractChanged, event.ContractChangedData{ContractID: c.ID, Change: change})
}

// applyAddendumTx activates an addendum and applies its effect on the locked contract in the same transaction.
func (s *Service) applyAddendumTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, a actor, c *store.Contract,
	ad *store.ContractAddendum, to AddendumStatus, diff map[string]any,
) error {
	action := ActionAddendum
	if ad.Kind == AddVesselChange {
		action = ActionChangeVessel
	}
	t, err := Next(MooringTransitions, Status(c.Status), action)
	if err != nil {
		return err
	}
	pl := decodeAddendum(ad).Payload
	now := s.now()
	switch ad.Kind {
	case AddServiceAdd:
		seq, err := store.NextLineSeq(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		if err := store.InsertContractLines(ctx, tx, []store.ContractLine{{
			ID: store.NewID(), MarinaID: scope.ID(), ContractID: c.ID, Seq: seq, LineKind: "service", ItemID: pl.ItemID,
			QtyMilli: pl.QtyMilli, CreatedAt: now, UpdatedAt: now,
		}}); err != nil {
			return err
		}
	case AddServiceRemove:
		ok, err := store.DeleteServiceLine(ctx, tx, scope, c.ID, pl.LineID)
		if err != nil {
			return err
		}
		if !ok {
			return &TransitionError{From: ad.Status, Action: ActionActivate}
		}
	case AddParking:
		seq, err := store.NextLineSeq(ctx, tx, scope, c.ID)
		if err != nil {
			return err
		}
		line := store.ContractLine{
			ID: store.NewID(), MarinaID: scope.ID(), ContractID: c.ID, Seq: seq, LineKind: "resource", ItemID: pl.ItemID,
			ResourceType: store.ResourceLand, ResourceID: pl.LandResourceID, QtyMilli: 1000, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertContractLines(ctx, tx, []store.ContractLine{line}); err != nil {
			return err
		}
		kind, exp := occupancy.KindContract, (*time.Time)(nil)
		if c.Status == string(StatusDraft) || c.Status == string(StatusAwaitingSignature) {
			if until := holdUntil(ctx, tx, scope, c.ID); !until.IsZero() {
				kind, exp = occupancy.KindHold, &until
			}
		}
		al, err := s.allocateLine(ctx, tx, scope, a, now, allocation{
			lineID: line.ID, vesselID: c.VesselID, kind: kind, expiresAt: exp,
			seg: Segment{ResourceType: store.ResourceLand, ResourceID: pl.LandResourceID, First: ad.EffectiveOn, Last: c.EndsOn.AddDays(-1)},
		})
		if err != nil {
			return err
		}
		if err := s.emitAllocated(ctx, tx, scope, []store.Allocation{*al}, c.ID); err != nil {
			return err
		}
	case AddBillingCustomer:
		c.BillingCustomerID = pl.CustomerID
	case AddVesselChange:
		if err := s.changeVesselTx(ctx, tx, scope, a, c, ad.EffectiveOn, pl.VesselID, pl.FitOverride); err != nil {
			return err
		}
	}
	replanned := 0
	if ad.AmountDeltaCents != 0 {
		kind := AdjustmentAddendum
		if ad.Kind == AddVesselChange {
			kind = AdjustmentVesselChange
		}
		if err := applyAdjustment(c, PriceAdjustment{
			Kind: kind, AddendumID: ad.ID, From: ad.EffectiveOn, To: c.EndsOn.AddDays(-1),
			OldCents: c.TotalCents, NewCents: c.TotalCents + ad.AmountDeltaCents,
		}); err != nil {
			return err
		}
		rows, err := s.replanTx(ctx, tx, scope, a, c, PlanStandard)
		if err != nil {
			return err
		}
		replanned = len(rows)
	}
	if err := updateContract(ctx, tx, scope, c.Version, c, now); err != nil {
		return err
	}
	from := ad.Status
	ad.Status, ad.ActivatedAt = string(to), now
	diff["from_status"], diff["status"], diff["kind"] = from, ad.Status, ad.Kind
	diff["amount_delta_cents"], diff["replanned"] = ad.AmountDeltaCents, replanned
	if err := s.saveAddendumTx(ctx, tx, scope, a, c, ad, "addendum.activate", diff, ""); err != nil {
		return err
	}
	change := event.ChangeAddendum
	if ad.Kind == AddVesselChange {
		change = event.ChangeVesselChanged
	}
	return s.emitTransition(ctx, tx, scope, t, subject{contract: c, change: change})
}

// holdUntil returns the expiry of the live holds of a contract (zero when none).
func holdUntil(ctx context.Context, tx bun.IDB, scope store.MarinaScope, contractID string) time.Time {
	allocs, err := contractAllocs(ctx, tx, scope, contractID)
	if err != nil {
		return time.Time{}
	}
	return holdExpiry(allocs)
}

// addendumSubject is the signature.Subject of addenda.
type addendumSubject struct{ s *Service }

func (addendumSubject) Kind() string { return SubjectAddendum }

// OnSignedTx applies a signed addendum.
func (as addendumSubject) OnSignedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, signedDocumentID string, _ time.Time) error {
	s := as.s
	if err := s.requireAllocator(); err != nil {
		return err
	}
	ad, err := store.GetAddendumByID(ctx, tx, scope, subjectID)
	if err != nil {
		return notFound(err)
	}
	c, err := lockContract(ctx, tx, scope, ad.ContractID, "", 0)
	if err != nil {
		return err
	}
	if ad, err = store.LockAddendum(ctx, tx, scope, c.ID, subjectID); err != nil {
		return notFound(err)
	}
	to, err := nextAddendum(AddendumStatus(ad.Status), ActionSign)
	if err != nil {
		return err
	}
	ad.DocumentID = signedDocumentID
	return s.applyAddendumTx(ctx, tx, scope, system, c, ad, to, map[string]any{"signed_document_id": signedDocumentID})
}

// OnFailedTx brings an addendum awaiting signature back to draft.
func (as addendumSubject) OnFailedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, status string) error {
	s := as.s
	ad, err := store.GetAddendumByID(ctx, tx, scope, subjectID)
	if err != nil {
		return notFound(err)
	}
	c, err := lockContract(ctx, tx, scope, ad.ContractID, "", 0)
	if err != nil {
		return err
	}
	if ad, err = store.LockAddendum(ctx, tx, scope, c.ID, subjectID); err != nil {
		return notFound(err)
	}
	to, err := nextAddendum(AddendumStatus(ad.Status), ActionSignatureFailed)
	if err != nil {
		return nil //nolint:nilerr // not awaiting a signature any more
	}
	from := ad.Status
	ad.Status = string(to)
	return s.saveAddendumTx(ctx, tx, scope, system, c, ad, "addendum.signature_failed", map[string]any{
		"from_status": from, "status": ad.Status, "request_status": status,
	}, event.ChangeSignatureFailed)
}
