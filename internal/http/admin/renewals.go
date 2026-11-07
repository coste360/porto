package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
)

// Renewal operations (m4-plan §7.3.1): simple renewal, campaigns (create, preview, launch, close, discard,
// proposals JSON and CSV) and proposals (send, accept, decline, include on behalf of the customer).

func writeProposal(w http.ResponseWriter, p *contract.Proposal, status int) {
	respond.SetETag(w, p.Row.Version)
	respond.JSON(w, status, view.RenewalProposal(p))
}

func writeCampaign(w http.ResponseWriter, c *contract.Campaign, status int) {
	respond.SetETag(w, c.Row.Version)
	respond.JSON(w, status, view.RenewalCampaign(c))
}

// RenewContract implements api.ServerInterface (POST .../contracts/{contractId}/renew).
func (h *Handler) RenewContract(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.RenewContractParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in *api.RenewInput
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	d := newCtrDates()
	ri := contract.RenewInput{}
	if in != nil {
		ri = contract.RenewInput{StartsOn: d.ptr(in.StartsOn), DeadlineOn: d.ptr(in.DeadlineOn), PriceListID: str(in.PriceListId), Send: boolOr(in.Send, false)}
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	p, err := h.Contracts.RenewContract(r.Context(), principal(r), marinaID, contractID, ifMatch, ri)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProposal(w, p, http.StatusCreated)
}

func ctrFilterIn(d *ctrDates, f api.RenewalFilter) contract.CampaignFilter {
	out := contract.CampaignFilter{EndsFrom: d.ptr(f.EndsFrom), EndsTo: d.ptr(f.EndsTo)}
	if f.Groups != nil {
		out.Groups = *f.Groups
	}
	if f.Structures != nil {
		out.Structures = *f.Structures
	}
	if f.ContractIds != nil {
		out.ContractIDs = *f.ContractIds
	}
	if f.CustomerCategories != nil {
		out.CustomerCategories = *f.CustomerCategories
	}
	if f.Terms != nil {
		for _, t := range *f.Terms {
			out.Terms = append(out.Terms, string(t))
		}
	}
	return out
}

// ListRenewalCampaigns implements api.ServerInterface (GET .../renewal-campaigns).
func (h *Handler) ListRenewalCampaigns(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListRenewalCampaignsParams) {
	page, err := h.Contracts.ListRenewalCampaigns(r.Context(), principal(r), marinaID, str(enumPtr(params.Status)), str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := api.RenewalCampaignPage{Items: make([]api.RenewalCampaign, 0, len(page.Items)), NextCursor: nextCursor(page.NextCursor)}
	for i := range page.Items {
		out.Items = append(out.Items, view.RenewalCampaign(&page.Items[i]))
	}
	respond.JSON(w, http.StatusOK, out)
}

// CreateRenewalCampaign implements api.ServerInterface (POST .../renewal-campaigns).
func (h *Handler) CreateRenewalCampaign(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateRenewalCampaignParams) {
	var in api.RenewalCampaignInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	ci := contract.CampaignInput{
		Name: in.Name, Filter: ctrFilterIn(d, in.Filter), DeadlineOn: d.parse(in.DeadlineOn), PriceListID: str(in.PriceListId),
		TargetStartsRule: str(enumPtr(in.TargetStartsRule)), TargetStartsOn: d.ptr(in.TargetStartsOn), Message: str(in.Message),
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	c, err := h.Contracts.CreateRenewalCampaign(r.Context(), principal(r), marinaID, ci)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeCampaign(w, c, http.StatusCreated)
}

// GetRenewalCampaign implements api.ServerInterface (GET .../renewal-campaigns/{campaignId}).
func (h *Handler) GetRenewalCampaign(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CampaignId) {
	c, err := h.Contracts.GetRenewalCampaign(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeCampaign(w, c, http.StatusOK)
}

// UpdateRenewalCampaign implements api.ServerInterface (PATCH .../renewal-campaigns/{campaignId}).
func (h *Handler) UpdateRenewalCampaign(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CampaignId, params api.UpdateRenewalCampaignParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.RenewalCampaignPatch
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	patch := contract.CampaignPatch{Name: in.Name, DeadlineOn: d.ptr(in.DeadlineOn), PriceListID: in.PriceListId, Message: in.Message}
	if in.Filter != nil {
		f := ctrFilterIn(d, *in.Filter)
		patch.Filter = &f
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	c, err := h.Contracts.UpdateRenewalCampaign(r.Context(), principal(r), marinaID, id, ifMatch, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeCampaign(w, c, http.StatusOK)
}

// PreviewRenewalCampaign implements api.ServerInterface (POST .../renewal-campaigns/{campaignId}/preview).
func (h *Handler) PreviewRenewalCampaign(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CampaignId, params api.PreviewRenewalCampaignParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	ctx, p := r.Context(), principal(r)
	c, err := h.Contracts.PreviewRenewalCampaign(ctx, p, marinaID, id, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	page, err := h.Contracts.ListCampaignProposals(ctx, p, marinaID, id, "", "", 0)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	all, err := h.Contracts.AllCampaignProposals(ctx, p, marinaID, id, "")
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	var oldT, newT int64
	for _, pr := range all {
		if pr.Row.Status == string(contract.ProposalExcluded) {
			continue
		}
		oldT += pr.Row.OldTotalCents
		if pr.Row.NewTotalCents != nil {
			newT += *pr.Row.NewTotalCents
		}
	}
	respond.SetETag(w, c.Row.Version)
	respond.JSON(w, http.StatusOK, api.RenewalCampaignPreview{
		Campaign: view.RenewalCampaign(c), Counts: api.RenewalCounts(c.Counts), OldTotalCents: &oldT, NewTotalCents: &newT,
		Proposals: api.RenewalProposalPage{Items: view.RenewalProposals(page.Items), NextCursor: nextCursor(page.NextCursor)},
	})
}

// LaunchRenewalCampaign implements api.ServerInterface (POST .../renewal-campaigns/{campaignId}/launch).
func (h *Handler) LaunchRenewalCampaign(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CampaignId, params api.LaunchRenewalCampaignParams) {
	h.campaignAction(w, r, marinaID, id, params.IfMatch, h.Contracts.LaunchRenewalCampaign)
}

// CloseRenewalCampaign implements api.ServerInterface (POST .../renewal-campaigns/{campaignId}/close).
func (h *Handler) CloseRenewalCampaign(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CampaignId, params api.CloseRenewalCampaignParams) {
	h.campaignAction(w, r, marinaID, id, params.IfMatch, h.Contracts.CloseRenewalCampaign)
}

// DiscardRenewalCampaign implements api.ServerInterface (POST .../renewal-campaigns/{campaignId}/discard).
func (h *Handler) DiscardRenewalCampaign(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CampaignId, params api.DiscardRenewalCampaignParams) {
	h.campaignAction(w, r, marinaID, id, params.IfMatch, h.Contracts.DiscardRenewalCampaign)
}

type campaignFn func(ctx context.Context, p *auth.Principal, marinaID, id string, ifMatch int) (*contract.Campaign, error)

func (h *Handler) campaignAction(w http.ResponseWriter, r *http.Request, marinaID, id string, ifMatchHdr *string, fn campaignFn) {
	ifMatch, ok := respond.IfMatch(w, r, ifMatchHdr)
	if !ok {
		return
	}
	c, err := fn(r.Context(), principal(r), marinaID, id, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeCampaign(w, c, http.StatusOK)
}

// ListCampaignProposals implements api.ServerInterface (GET .../renewal-campaigns/{campaignId}/proposals).
func (h *Handler) ListCampaignProposals(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.CampaignId, params api.ListCampaignProposalsParams) {
	ctx, p := r.Context(), principal(r)
	status := str(enumPtr(params.Status))
	if params.Format != nil && *params.Format == api.ListCampaignProposalsParamsFormatCsv {
		all, err := h.Contracts.AllCampaignProposals(ctx, p, marinaID, id, status)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeCSV(w, "rinnovi.csv", proposalsCSV(all))
		return
	}
	page, err := h.Contracts.ListCampaignProposals(ctx, p, marinaID, id, status, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.RenewalProposalPage{Items: view.RenewalProposals(page.Items), NextCursor: nextCursor(page.NextCursor)})
}

// proposalsCSV writes proposals as CSV (Italian: ';', decimal comma; codes and amounts only).
func proposalsCSV(ps []contract.Proposal) []byte {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	cw.Comma = ';'
	_ = cw.Write([]string{"contratto", "nuovo_contratto", "stato", "esclusione", "importo_attuale", "importo_nuovo", "scadenza", "inviata_volte"})
	euro := func(c int64) string { return strings.Replace(money.Cents(c).String(), ".", ",", 1) }
	for _, p := range ps {
		src, succ, nw := "", "", ""
		if p.Source != nil {
			src = contract.Code(p.Source)
		}
		if p.Successor != nil {
			succ = contract.Code(p.Successor)
		}
		if p.Row.NewTotalCents != nil {
			nw = euro(*p.Row.NewTotalCents)
		}
		_ = cw.Write([]string{
			src, succ, p.Row.Status, p.Row.ExclusionReason, euro(p.Row.OldTotalCents), nw, p.Row.DeadlineOn.String(),
			strconv.Itoa(p.Row.SentCount),
		})
	}
	cw.Flush()
	return buf.Bytes()
}

// GetRenewalProposal implements api.ServerInterface (GET .../renewal-proposals/{proposalId}).
func (h *Handler) GetRenewalProposal(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ProposalId) {
	p, err := h.Contracts.GetRenewalProposal(r.Context(), principal(r), marinaID, id)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProposal(w, p, http.StatusOK)
}

// SendRenewalProposal implements api.ServerInterface (POST .../renewal-proposals/{proposalId}/send).
func (h *Handler) SendRenewalProposal(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ProposalId, params api.SendRenewalProposalParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	p, err := h.Contracts.SendRenewalProposal(r.Context(), principal(r), marinaID, id, ifMatch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProposal(w, p, http.StatusOK)
}

// AcceptRenewalProposal implements api.ServerInterface (POST .../renewal-proposals/{proposalId}/accept).
func (h *Handler) AcceptRenewalProposal(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ProposalId, params api.AcceptRenewalProposalParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in *api.RenewalAccept
	if !respond.DecodeOrFail(w, r, &in, true) {
		return
	}
	plan := ""
	if in != nil {
		plan = str(enumPtr(in.Plan))
	}
	p, err := h.Contracts.AcceptRenewalProposal(r.Context(), principal(r), marinaID, id, ifMatch, plan)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProposal(w, p, http.StatusOK)
}

// DeclineRenewalProposal implements api.ServerInterface (POST .../renewal-proposals/{proposalId}/decline).
func (h *Handler) DeclineRenewalProposal(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ProposalId, params api.DeclineRenewalProposalParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	reason, ok := ctrReason(r, w)
	if !ok {
		return
	}
	p, err := h.Contracts.DeclineRenewalProposal(r.Context(), principal(r), marinaID, id, ifMatch, reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProposal(w, p, http.StatusOK)
}

// IncludeRenewalProposal implements api.ServerInterface (POST .../renewal-proposals/{proposalId}/include).
func (h *Handler) IncludeRenewalProposal(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.ProposalId, params api.IncludeRenewalProposalParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ReasonRequired
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	p, err := h.Contracts.IncludeRenewalProposal(r.Context(), principal(r), marinaID, id, ifMatch, in.Reason)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeProposal(w, p, http.StatusOK)
}
