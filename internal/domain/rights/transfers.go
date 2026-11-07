package rights

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/accrual"
	"github.com/coste360/porto/internal/domain/allot"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/docs"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/signature"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
)

// Transfers of rights (spec 14.2; m4-plan §4.2): a draft with the requirement check, the document and its
// signature (outgoing holders, incoming holders, marina) or the paper confirmation, and the completion in one
// transaction: the right is closed at effective_on − 1, a new right starts at effective_on with the incoming
// holders, the future owner occupancy moves to it and the future fee instalments are reassigned or pro-rated.
// The right row lock and its status serialise concurrent completions: one right chain (INV-01 with owner).

// Transfer is a transfer with the holders of both sides.
type Transfer struct {
	store.RightTransfer
	BerthID           string
	From              []Holder
	To                []Holder
	RequirementIssues []RequirementIssue
}

// TransferInput are the fields of a new transfer.
type TransferInput struct {
	RightID            string
	EffectiveOn        calendar.Date
	ToHolders          []allot.Share
	DeclaredPriceCents *int64
	TakeoverItemID     string
	FeesPolicy         string
}

type holderJSON struct {
	CustomerID  string `json:"customer_id"`
	ShareMillis int64  `json:"share_millis"`
}

func toHolderShares(j store.JSON) []allot.Share {
	var hs []holderJSON
	_ = json.Unmarshal(j, &hs)
	out := make([]allot.Share, len(hs))
	for i, h := range hs {
		out[i] = allot.Share{CustomerID: h.CustomerID, Millis: h.ShareMillis}
	}
	return out
}

func (s *Service) transferView(ctx context.Context, db bun.IDB, scope store.MarinaScope, p *auth.Principal, t *store.RightTransfer) (*Transfer, error) {
	r, err := store.GetBerthRight(ctx, db, scope, t.RightID)
	if err != nil {
		return nil, err
	}
	hs, err := store.HoldersOfRights(ctx, db, scope, []string{t.RightID})
	if err != nil {
		return nil, err
	}
	to := toHolderShares(t.ToHolders)
	var ids []string
	for _, h := range hs {
		ids = append(ids, h.CustomerID)
	}
	for _, h := range to {
		ids = append(ids, h.CustomerID)
	}
	nm, err := names(ctx, db, scope, can(p, scope, auth.PermCustomersRead), ids)
	if err != nil {
		return nil, err
	}
	out := &Transfer{RightTransfer: *t, BerthID: r.BerthID, From: []Holder{}, To: []Holder{}, RequirementIssues: []RequirementIssue{}}
	for _, h := range hs {
		out.From = append(out.From, Holder{CustomerID: h.CustomerID, DisplayName: nm[h.CustomerID], ShareMillis: h.ShareMillis})
	}
	for _, h := range to {
		out.To = append(out.To, Holder{CustomerID: h.CustomerID, DisplayName: nm[h.CustomerID], ShareMillis: h.Millis})
	}
	if len(t.RequirementIssues) > 0 {
		_ = json.Unmarshal(t.RequirementIssues, &out.RequirementIssues)
	}
	return out, nil
}

// ListTransfers returns a page of transfers (rights.manage).
func (s *Service) ListTransfers(ctx context.Context, p *auth.Principal, marinaID string, f store.TransferFilter, cursor string, limit int) (store.Page[Transfer], error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return store.Page[Transfer]{}, err
	}
	var out store.Page[Transfer]
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		page, err := store.ListRightTransfers(ctx, tx, scope, f, cursor, limit)
		if err != nil {
			return err
		}
		out = store.Page[Transfer]{Items: make([]Transfer, 0, len(page.Items)), NextCursor: page.NextCursor}
		for i := range page.Items {
			v, err := s.transferView(ctx, tx, scope, p, &page.Items[i])
			if err != nil {
				return err
			}
			out.Items = append(out.Items, *v)
		}
		return nil
	})
	return out, err
}

// GetTransfer returns a transfer (rights.manage).
func (s *Service) GetTransfer(ctx context.Context, p *auth.Principal, marinaID, id string) (*Transfer, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *Transfer
	err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.GetRightTransfer(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		out, err = s.transferView(ctx, tx, scope, p, t)
		return err
	})
	return out, err
}

// requirementPolicy returns the transfer requirement policy of the marina: marinas.settings
// rights.transfer_requirements (block or warn), default warn.
func requirementPolicy(ctx context.Context, db bun.IDB, scope store.MarinaScope) string {
	m, err := store.GetMarina(ctx, db, scope)
	if err != nil || len(m.Settings) == 0 {
		return RequirementWarn
	}
	var st struct {
		Rights struct {
			TransferRequirements string `json:"transfer_requirements"`
		} `json:"rights"`
	}
	if json.Unmarshal(m.Settings, &st) == nil && st.Rights.TransferRequirements == RequirementBlock {
		return RequirementBlock
	}
	return RequirementWarn
}

// requirementIssues lists the unsettled overdue holder instalments of the outgoing holders (ids and amounts only).
func requirementIssues(ctx context.Context, db bun.IDB, scope store.MarinaScope, customerIDs []string, today calendar.Date) ([]RequirementIssue, error) {
	rows, err := store.UnpaidOverdueHolderInstallments(ctx, db, scope, customerIDs, today)
	if err != nil {
		return nil, err
	}
	type key struct{ code, customer string }
	idx := map[key]int{}
	var out []RequirementIssue
	for _, h := range rows {
		code := "unsettled_fees"
		if h.SourceType == SourceCommonAdvance || h.SourceType == SourceCommonBalance {
			code = "unsettled_common_charges"
		}
		k := key{code, h.CustomerID}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, RequirementIssue{Code: code, CustomerID: h.CustomerID})
		}
		out[i].HolderInstallmentIDs = append(out[i].HolderInstallmentIDs, h.ID)
		out[i].AmountCents += h.AmountCents
	}
	return out, nil
}

// takeoverFee returns the flat price of the takeover item in the active price list of the date (0 when none).
func takeoverFee(ctx context.Context, db bun.IDB, scope store.MarinaScope, itemID string, on calendar.Date) (int64, error) {
	if itemID == "" {
		return 0, nil
	}
	if err := store.GetItemOfMarina(ctx, db, scope, itemID); err != nil {
		return 0, err
	}
	bodies, err := store.ItemBaseRuleBodies(ctx, db, scope, itemID, on)
	if err != nil {
		return 0, err
	}
	for _, b := range bodies {
		var r struct {
			AmountCents *int64 `json:"amount_cents"`
			UnitCents   *int64 `json:"unit_cents"`
		}
		if json.Unmarshal(b, &r) == nil {
			if r.AmountCents != nil {
				return *r.AmountCents, nil
			}
			if r.UnitCents != nil {
				return *r.UnitCents, nil
			}
		}
	}
	return 0, nil
}

// CreateTransfer creates a draft transfer of an active right: the requirement check on the outgoing holders
// (unsettled overdue instalments) blocks with 422 transfer_requirements or is stored as a warning, by the
// marina policy (rights.manage).
func (s *Service) CreateTransfer(ctx context.Context, p *auth.Principal, marinaID string, in TransferInput) (*Transfer, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	if !slices.Contains([]string{FeesReassign, FeesProRata}, in.FeesPolicy) {
		return nil, invalidField("fees_policy", "invalid")
	}
	if in.DeclaredPriceCents != nil && *in.DeclaredPriceCents < 0 {
		return nil, invalidField("declared_price_cents", "out_of_range")
	}
	if len(in.ToHolders) > 20 {
		return nil, invalidField("to_holders", "too_many")
	}
	if err := checkShares(in.ToHolders); err != nil {
		return nil, err
	}
	var out *Transfer
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		r, err := store.LockBerthRight(ctx, tx, scope, in.RightID)
		if err != nil {
			return err
		}
		if r.Status != RightActive {
			return ErrInvalidTransition
		}
		today := s.today(ctx, tx, scope)
		if in.EffectiveOn.IsZero() || !in.EffectiveOn.After(r.FromOn) || in.EffectiveOn.After(r.ToOn) {
			return invalidField("effective_on", "out_of_period")
		}
		if in.EffectiveOn.Before(today) {
			return invalidField("effective_on", "in_the_past")
		}
		ids := make([]string, len(in.ToHolders))
		for i, h := range in.ToHolders {
			ids[i] = h.CustomerID
		}
		if err := checkCustomers(ctx, tx, scope, ids); err != nil {
			return err
		}
		hs, err := store.HoldersOfRights(ctx, tx, scope, []string{r.ID})
		if err != nil {
			return err
		}
		outgoing := make([]string, len(hs))
		for i, h := range hs {
			outgoing[i] = h.CustomerID
		}
		issues, err := requirementIssues(ctx, tx, scope, outgoing, today)
		if err != nil {
			return err
		}
		policy := requirementPolicy(ctx, tx, scope)
		if policy == RequirementBlock && len(issues) > 0 {
			return &RequirementsError{Issues: issues}
		}
		fee, err := takeoverFee(ctx, tx, scope, in.TakeoverItemID, in.EffectiveOn)
		if err != nil {
			return err
		}
		if issues == nil {
			issues = []RequirementIssue{}
		}
		toHolders := make([]holderJSON, len(in.ToHolders))
		for i, h := range in.ToHolders {
			toHolders[i] = holderJSON{CustomerID: h.CustomerID, ShareMillis: h.Millis}
		}
		now := s.now()
		t := &store.RightTransfer{
			ID: store.NewID(), RightID: r.ID, EffectiveOn: in.EffectiveOn, ToHolders: store.MustJSON(toHolders),
			DeclaredPriceCents: in.DeclaredPriceCents, TakeoverItemID: in.TakeoverItemID, TakeoverFeeCents: fee,
			RequirementPolicy: policy, RequirementIssues: store.MustJSON(issues), FeesPolicy: in.FeesPolicy, Status: TransferDraft,
			CreatedByUserID: userOf(p), Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := store.InsertRightTransfer(ctx, tx, scope, t); err != nil {
			return err
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "transfer.create", "right_transfer", t.ID,
			map[string]any{"right_id": r.ID, "effective_on": t.EffectiveOn, "fees_policy": t.FeesPolicy, "issues": len(issues), "takeover_fee_cents": fee},
			changed("transfer", t.ID, "created")); err != nil {
			return err
		}
		out, err = s.transferView(ctx, tx, scope, p, t)
		return err
	})
	return out, err
}

// SignatureInput chooses the driver and signers of a transfer signature (zero = marina default driver and the
// signers outgoing holders, incoming holders, marina in sequence).
type SignatureInput struct {
	Driver string
}

// RequestTransferSignature composes the transfer document (PDF/A) and opens its signature request with the
// outgoing holders, the incoming holders and the marina as sequential signers (rights.manage).
func (s *Service) RequestTransferSignature(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, in SignatureInput) (*Transfer, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *Transfer
	var docID string
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.GetRightTransfer(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if _, err := store.LockBerthRight(ctx, tx, scope, t.RightID); err != nil {
			return err
		}
		next, ok := Next(TransferTransitions, t.Status, "request_signature")
		if !ok {
			return ErrInvalidTransition
		}
		if t.Version != ifMatch {
			return ErrVersionMismatch
		}
		now := s.now()
		set, err := store.GetContractSettings(ctx, tx, scope, now)
		if err != nil {
			return err
		}
		driver := in.Driver
		if driver == "" {
			driver = set.SignatureDriver
		}
		if !slices.Contains([]string{"paper", "otp", "provider"}, driver) {
			return invalidField("driver", "invalid")
		}
		docID, err = s.d.Docs.ComposeTx(ctx, tx, scope, docs.ComposeRequest{
			Kind: docs.KindTransfer, SubjectType: docs.SubjectRightTransfer, SubjectID: t.ID, PDFA: true, CreatedBy: userOf(p), At: now,
		})
		if err != nil {
			return err
		}
		hs, err := store.HoldersOfRights(ctx, tx, scope, []string{t.RightID})
		if err != nil {
			return err
		}
		var signers []signature.SignerSpec
		for _, h := range hs {
			signers = append(signers, signature.SignerSpec{Role: "outgoing_holder", CustomerID: h.CustomerID})
		}
		for _, h := range toHolderShares(t.ToHolders) {
			signers = append(signers, signature.SignerSpec{Role: "incoming_holder", CustomerID: h.CustomerID})
		}
		signers = append(signers, signature.SignerSpec{Role: "marina", UserID: userOf(p)})
		days := set.SignatureDays
		if days <= 0 {
			days = 14
		}
		reqID, err := s.d.Signatures.OpenTx(ctx, tx, scope, signature.OpenRequest{
			SubjectType: "right_transfer", SubjectID: t.ID, DocumentID: docID, Driver: driver, ProviderCode: set.ProviderCode,
			Signers: signers, Sequential: true, ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour), CreatedBy: userOf(p),
		})
		if err != nil {
			return err
		}
		t.Status, t.DocumentID, t.SignatureRequestID, t.UpdatedAt = next, docID, reqID, now
		if err := store.UpdateRightTransfer(ctx, tx, scope, t, ifMatch); err != nil {
			return err
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "transfer.request_signature", "right_transfer", t.ID,
			map[string]any{"driver": driver, "signers": len(signers), "document_id": docID, "signature_request_id": reqID},
			changed("transfer", t.ID, event.ChangeSignatureRequested)); err != nil {
			return err
		}
		out, err = s.transferView(ctx, tx, scope, p, t)
		return err
	})
	if err == nil && docID != "" {
		_ = s.d.Docs.RenderPending(ctx, docID) // after commit; the documents.render job retries
	}
	return out, err
}

// CompleteTransfer completes a draft or awaiting-signature transfer on paper ("firmato su carta"); an open
// signature request is cancelled (rights.manage).
func (s *Service) CompleteTransfer(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*Transfer, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	var out *Transfer
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.GetRightTransfer(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if t.Version != ifMatch {
			return ErrVersionMismatch
		}
		if _, ok := Next(TransferTransitions, t.Status, "complete"); !ok {
			return ErrInvalidTransition
		}
		if t.Status == TransferAwaitingSignature && t.SignatureRequestID != "" {
			if err := s.d.Signatures.CancelTx(ctx, tx, scope, "right_transfer", t.ID); err != nil {
				return err
			}
		}
		if err := s.completeTx(ctx, tx, scope, actorOf(p), t, "complete", ""); err != nil {
			return err
		}
		out, err = s.transferView(ctx, tx, scope, p, t)
		return err
	})
	return out, err
}

// CancelTransfer cancels a draft or awaiting-signature transfer (rights.manage).
func (s *Service) CancelTransfer(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int, reason string) (*Transfer, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermRightsManage)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(reason)) > 500 {
		return nil, invalidField("reason", "too_long")
	}
	var out *Transfer
	err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		t, err := store.GetRightTransfer(ctx, tx, scope, id)
		if err != nil {
			return err
		}
		if _, err := store.LockBerthRight(ctx, tx, scope, t.RightID); err != nil {
			return err
		}
		next, ok := Next(TransferTransitions, t.Status, "cancel")
		if !ok {
			return ErrInvalidTransition
		}
		if t.Version != ifMatch {
			return ErrVersionMismatch
		}
		if t.Status == TransferAwaitingSignature && t.SignatureRequestID != "" {
			if err := s.d.Signatures.CancelTx(ctx, tx, scope, "right_transfer", t.ID); err != nil {
				return err
			}
		}
		now := s.now()
		t.Status, t.CancelledAt, t.UpdatedAt = next, now, now
		if err := store.UpdateRightTransfer(ctx, tx, scope, t, ifMatch); err != nil {
			return err
		}
		if err := s.write(ctx, tx, actorOf(p), scope, "transfer.cancel", "right_transfer", t.ID, map[string]any{"status": t.Status},
			changed("transfer", t.ID, "cancelled")); err != nil {
			return err
		}
		out, err = s.transferView(ctx, tx, scope, p, t)
		return err
	})
	return out, err
}

// completeTx completes a transfer in tx (paper completion or signature callback): the old right ends at
// effective_on − 1 (status transferred) with its owner occupancy truncated, the new right starts at effective_on
// with the incoming holders and its owner occupancy, the mandates after effective_on end, the future scheduled
// fee instalments are reassigned or pro-rated, and right.transferred is written.
func (s *Service) completeTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, actor audit.Actor, t *store.RightTransfer, action, signedDocID string) error {
	r, err := store.LockBerthRight(ctx, tx, scope, t.RightID)
	if err != nil {
		return err
	}
	cur, err := store.GetRightTransfer(ctx, tx, scope, t.ID)
	if err != nil {
		return err
	}
	if cur.Version != t.Version || cur.Status != t.Status {
		return ErrVersionMismatch
	}
	if r.Status != RightActive || t.EffectiveOn.After(r.ToOn) {
		return ErrInvalidTransition
	}
	now := s.now()
	today := s.today(ctx, tx, scope)
	eff := t.EffectiveOn
	oldHolders, err := store.HoldersOfRights(ctx, tx, scope, []string{r.ID})
	if err != nil {
		return err
	}
	rel, err := s.truncateOwner(ctx, tx, scope, r.ID, eff, "truncate")
	if err != nil {
		return err
	}
	if err := s.closeMandatesFrom(ctx, tx, scope, r.ID, eff, now); err != nil {
		return err
	}
	oldTo, oldVersion := r.ToOn, r.Version
	r.Status, r.ToOn, r.EndReason, r.UpdatedAt = RightTransferred, eff.AddDays(-1), "transfer", now
	if err := store.UpdateBerthRight(ctx, tx, scope, r, oldVersion); err != nil {
		return err
	}
	var title Title
	_ = json.Unmarshal(r.Title, &title)
	nr, err := s.createRightTx(ctx, tx, scope, nil, RightInput{
		BerthID: r.BerthID, Kind: r.Kind, FromOn: eff, ToOn: oldTo, Title: title, Holders: toHolderShares(t.ToHolders),
	}, t.ID)
	if err != nil {
		return err
	}
	if err := s.moveFeeInstallments(ctx, tx, scope, r, nr, t, sharesOf(oldHolders), today, now); err != nil {
		return err
	}
	from := t.Status
	t.Status, t.NewRightID, t.CompletedAt, t.UpdatedAt = TransferCompleted, nr.ID, now, now
	if signedDocID != "" {
		t.DocumentID = signedDocID
	}
	if err := store.UpdateRightTransfer(ctx, tx, scope, t, t.Version); err != nil {
		return err
	}
	events := slices.Concat(rel, []ev{{event.RightTransferred, event.RightData{
		RightID: r.ID, BerthID: r.BerthID, Kind: r.Kind, From: r.FromOn, To: r.ToOn, NewRightID: nr.ID, TransferID: t.ID,
	}}})
	return s.write(ctx, tx, actor, scope, "transfer."+action, "right_transfer", t.ID,
		map[string]any{"from_status": from, "status": t.Status, "right_id": r.ID, "new_right_id": nr.ID, "effective_on": eff}, events...)
}

// moveFeeInstallments moves the scheduled management fee instalments of the old right due on or after
// effective_on: reassign keeps the amounts and due dates and splits each among the new holders by share
// (money.Allocate); prorata splits the year's fee of each line by days (accrual) between old and new holders and
// replans both parts as one instalment each.
func (s *Service) moveFeeInstallments(ctx context.Context, tx bun.IDB, scope store.MarinaScope, old, nr *store.BerthRight, t *store.RightTransfer,
	oldShares []allot.Share, today calendar.Date, now time.Time,
) error {
	rows, err := store.AllHolderInstallments(ctx, tx, scope, store.HolderInstallmentFilter{
		RightID: old.ID, Status: store.InstallmentScheduled, SourceType: SourceManagementFee, From: t.EffectiveOn,
	})
	if err != nil || len(rows) == 0 {
		return err
	}
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	// The rows were read under the right lock (completeTx), and issue and settle take the same lock, so every row
	// is still scheduled; a shortfall means one was issued or paid meanwhile and replanning it would exceed the
	// line (INV-31): the transfer fails with 412 instead.
	if n, err := store.CancelHolderInstallments(ctx, tx, scope, ids, now); err != nil {
		return err
	} else if n != len(ids) {
		return ErrVersionMismatch
	}
	newShares := toHolderShares(t.ToHolders)
	seqs := map[string]int{}
	nextSeq := func(sourceID, customerID string) (int, error) {
		k := sourceID + "|" + customerID
		if n, ok := seqs[k]; ok {
			seqs[k] = n + 1
			return n + 1, nil
		}
		n, err := store.MaxHolderSeq(ctx, tx, scope, SourceManagementFee, sourceID, customerID)
		if err != nil {
			return 0, err
		}
		seqs[k] = n + 1
		return n + 1, nil
	}
	add := func(out *[]store.HolderInstallment, amount money.Cents, shares []allot.Share, rightID, sourceID, runID string, issue, due calendar.Date) error {
		if amount == 0 {
			return nil
		}
		parts, err := allot.Holders(amount, shares)
		if err != nil {
			return err
		}
		for _, ph := range parts {
			if ph.Amount == 0 {
				continue
			}
			seq, err := nextSeq(sourceID, ph.CustomerID)
			if err != nil {
				return err
			}
			*out = append(*out, store.HolderInstallment{
				ID: store.NewID(), SourceType: SourceManagementFee, SourceID: sourceID, RunID: runID, RightID: rightID, CustomerID: ph.CustomerID,
				Seq: seq, Kind: "installment", IssueOn: issue, DueOn: due, AmountCents: int64(ph.Amount), Status: store.InstallmentScheduled,
				Version: 1, CreatedAt: now, UpdatedAt: now,
			})
		}
		return nil
	}
	var out []store.HolderInstallment
	if t.FeesPolicy == FeesReassign {
		type key struct {
			source, run string
			issue, due  calendar.Date
		}
		sums := map[key]money.Cents{}
		var order []key
		for _, h := range rows {
			k := key{h.SourceID, h.RunID, h.IssueOn, h.DueOn}
			if _, ok := sums[k]; !ok {
				order = append(order, k)
			}
			sums[k] += money.Cents(h.AmountCents)
		}
		for _, k := range order {
			if err := add(&out, sums[k], newShares, nr.ID, k.source, k.run, k.issue, k.due); err != nil {
				return err
			}
		}
	} else {
		type lineSum struct {
			cancelled money.Cents
			run       string
			due       calendar.Date
		}
		lines := map[string]*lineSum{}
		var order []string
		for _, h := range rows {
			ls := lines[h.SourceID]
			if ls == nil {
				ls = &lineSum{run: h.RunID, due: h.DueOn}
				lines[h.SourceID] = ls
				order = append(order, h.SourceID)
			}
			ls.cancelled += money.Cents(h.AmountCents)
			if h.DueOn.Before(ls.due) {
				ls.due = h.DueOn
			}
		}
		for _, lineID := range order {
			ls := lines[lineID]
			fl, err := store.GetFeeLine(ctx, tx, scope, lineID)
			if err != nil {
				return err
			}
			run, err := store.GetFeeRun(ctx, tx, scope, fl.RunID)
			if err != nil {
				return err
			}
			yearLine := accrual.Line{ID: lineID, Amount: money.Cents(fl.AmountCents), From: calendar.New(run.Year, 1, 1), To: calendar.New(run.Year, 12, 31)}
			newPart, err := accrual.Within(yearLine, maxDate(t.EffectiveOn, yearLine.From), yearLine.To)
			if err != nil {
				return err
			}
			if newPart > ls.cancelled {
				newPart = ls.cancelled
			}
			oldPart := ls.cancelled - newPart
			due := maxDate(ls.due, today)
			if err := add(&out, newPart, newShares, nr.ID, lineID, ls.run, today, due); err != nil {
				return err
			}
			if err := add(&out, oldPart, oldShares, old.ID, lineID, ls.run, today, maxDate(t.EffectiveOn, today)); err != nil {
				return err
			}
		}
	}
	return store.InsertHolderInstallments(ctx, tx, scope, out)
}

// SignatureSubject returns the signature subject right_transfer (registered by internal/app).
func (s *Service) SignatureSubject() signature.Subject { return transferSubject{s} }

type transferSubject struct{ s *Service }

func (transferSubject) Kind() string { return "right_transfer" }

// OnSignedTx completes the transfer when every signer has signed (in the transaction that stores the signed
// document).
func (ts transferSubject) OnSignedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, signedDocumentID string, at time.Time) error {
	t, err := store.GetRightTransfer(ctx, tx, scope, subjectID)
	if err != nil {
		return err
	}
	if t.Status == TransferCompleted {
		return nil
	}
	if _, ok := Next(TransferTransitions, t.Status, "sign"); !ok {
		return ErrInvalidTransition
	}
	_ = at
	return ts.s.completeTx(ctx, tx, scope, audit.System, t, "complete", signedDocumentID)
}

// OnFailedTx puts the transfer back to draft when its request is declined, expired or cancelled.
func (ts transferSubject) OnFailedTx(ctx context.Context, tx bun.IDB, scope store.MarinaScope, subjectID, status string) error {
	t, err := store.GetRightTransfer(ctx, tx, scope, subjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	next, ok := Next(TransferTransitions, t.Status, "signature_failed")
	if !ok {
		return nil // cancelled or completed meanwhile
	}
	v := t.Version
	t.Status, t.UpdatedAt = next, ts.s.now()
	if err := store.UpdateRightTransfer(ctx, tx, scope, t, v); err != nil {
		return err
	}
	return ts.s.write(ctx, tx, audit.System, scope, "transfer.signature_failed", "right_transfer", t.ID, map[string]any{"request_status": status},
		changed("transfer", t.ID, event.ChangeSignatureFailed))
}
