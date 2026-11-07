package admin

import (
	"net/http"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
)

// Instalment operations (m4-plan §7.3.1): contract plan, replan, scadenzario (JSON and CSV), bulk issue and
// reminders, manual settlement (single and bulk). The policy input converters shared by the M4 contract
// handlers live here too.

// ctrPaymentIn converts a payment clause.
func ctrPaymentIn(in *api.PaymentPolicy) *contract.PaymentPolicy {
	if in == nil {
		return nil
	}
	out := &contract.PaymentPolicy{
		DepositDueDays: intOr(in.DepositDueDays, 0), IssueDaysBeforeDue: intOr(in.IssueDaysBeforeDue, 0),
		RoundingUnitCents: 1, SEPA: boolOr(in.Sepa, false),
	}
	if in.RoundingUnitCents != nil {
		out.RoundingUnitCents = int64(*in.RoundingUnitCents)
	}
	if d := in.Deposit; d != nil {
		out.Deposit = &contract.DepositPolicy{BP: d.Bp, Cents: d.Cents}
	}
	if e := in.EarlyPayment; e != nil {
		out.EarlyPayment = &contract.EarlyPaymentPolicy{DiscountBP: e.DiscountBp, PayByOffsetDays: e.PayByOffsetDays}
	}
	if sc := in.Schedule; sc != nil {
		out.Schedule = contract.SchedulePolicy{
			Mode: string(sc.Mode), EveryMonths: intOr(sc.EveryMonths, 0), Count: intOr(sc.Count, 0),
			FirstDueOffsetMonths: intOr(sc.FirstDueOffsetMonths, 0), DueOffsetDays: intOr(sc.DueOffsetDays, 0),
		}
		if sc.Dates != nil {
			for _, d := range *sc.Dates {
				out.Schedule.Dates = append(out.Schedule.Dates, contract.FixedDueOn{DueOn: d.DueOn, BP: d.Bp})
			}
		}
	}
	return out
}

// ctrClausesIn adds the M4 clauses of a policy to its M3 input.
func ctrClausesIn(out *contract.PolicyInput, in *api.ContractPolicy) {
	if out == nil || in == nil {
		return
	}
	out.Payment = ctrPaymentIn(in.Payment)
	if r := in.Renewal; r != nil {
		out.Renewal = &contract.RenewalPolicy{Tacit: boolOr(r.Tacit, false), NoticeDays: intOr(r.NoticeDays, 60)}
	}
	if c := in.Cancellation; c != nil {
		cp := &contract.CancellationPolicy{NoticeDays: intOr(c.NoticeDays, 30), Refund: contract.RefundProRata}
		if c.Refund != nil {
			cp.Refund = string(*c.Refund)
		}
		if c.PenaltyBp != nil {
			cp.PenaltyBP = *c.PenaltyBp
		}
		out.Cancellation = cp
	}
}

// ListContractInstallments implements api.ServerInterface (GET .../contracts/{contractId}/installments).
func (h *Handler) ListContractInstallments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId) {
	p, err := h.Contracts.ContractInstallments(r.Context(), principal(r), marinaID, contractID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.InstallmentPlan(contractID, p, ""))
}

// ReplanInstallments implements api.ServerInterface (POST .../contracts/{contractId}/installments/replan).
func (h *Handler) ReplanInstallments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, contractID api.ContractId, params api.ReplanInstallmentsParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.ReplanInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	p, err := h.Contracts.ReplanInstallments(r.Context(), principal(r), marinaID, contractID, ifMatch, contract.ReplanInput{
		Plan: string(in.Plan), Payment: ctrPaymentIn(in.Payment),
	})
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view.InstallmentPlan(contractID, p, ""))
}

// ListInstallments implements api.ServerInterface (GET .../installments): the scadenzario, JSON or CSV.
func (h *Handler) ListInstallments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListInstallmentsParams) {
	d := newCtrDates()
	f := contract.InstallmentFilter{
		From: d.ptr(params.From), To: d.ptr(params.To), Status: str(enumPtr(params.Status)), CustomerID: str(params.CustomerId),
		ContractID: str(params.ContractId), GroupID: str(params.GroupId),
	}
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	if params.Format != nil && *params.Format == api.ListInstallmentsParamsFormatCsv {
		b, err := h.Contracts.InstallmentsCSV(r.Context(), principal(r), marinaID, f)
		if err != nil {
			respond.Error(w, r, err)
			return
		}
		writeCSV(w, "scadenzario.csv", b)
		return
	}
	page, err := h.Contracts.ListInstallments(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	var sum int64
	for _, it := range page.Items {
		sum += it.AmountCents
	}
	n := len(page.Items)
	out := api.InstallmentPage{Items: view.Installments(page.Items), NextCursor: nextCursor(page.NextCursor)}
	out.Totals = &struct {
		AmountCents *api.Cents `json:"amount_cents,omitempty"`
		Count       *int       `json:"count,omitempty"`
	}{AmountCents: &sum, Count: &n}
	respond.JSON(w, http.StatusOK, out)
}

// writeCSV answers a CSV download (UTF-8 with BOM so spreadsheets read the accents).
func writeCSV(w http.ResponseWriter, name string, b []byte) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("\ufeff"))
	_, _ = w.Write(b) //nolint:gosec // text/csv attachment built by the service, not HTML
}

// IssueInstallments implements api.ServerInterface (POST .../installments/issue).
func (h *Handler) IssueInstallments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.IssueInstallmentsParams) {
	var in api.IdList
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Contracts.IssueInstallments(r.Context(), principal(r), marinaID, in.Ids)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.IssueResult{Issued: res.Done, Skipped: res.Skipped})
}

// RemindInstallments implements api.ServerInterface (POST .../installments/remind).
func (h *Handler) RemindInstallments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.RemindInstallmentsParams) {
	var in api.IdList
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	res, err := h.Contracts.RemindInstallments(r.Context(), principal(r), marinaID, in.Ids)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.RemindResult{Queued: res.Queued, SkippedNoEmail: res.SkippedNoEmail})
}

func ctrSettleIn(d *ctrDates, method api.SettlementMethod, ref, on *string) contract.SettleInput {
	return contract.SettleInput{Method: string(method), Reference: str(ref), SettledOn: d.ptr(on)}
}

// SettleInstallment implements api.ServerInterface (POST .../installments/{installmentId}/settle).
func (h *Handler) SettleInstallment(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, id api.InstallmentId, params api.SettleInstallmentParams) {
	ifMatch, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	var in api.SettleInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	si := ctrSettleIn(d, in.Method, in.Reference, in.SettledOn)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	row, err := h.Contracts.SettleInstallment(r.Context(), principal(r), marinaID, id, ifMatch, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, row.Version)
	respond.JSON(w, http.StatusOK, view.Installment(row))
}

// SettleInstallments implements api.ServerInterface (POST .../installments/settle): all or nothing.
func (h *Handler) SettleInstallments(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.SettleInstallmentsParams) {
	var in api.BulkSettleInput
	if !respond.DecodeOrFail(w, r, &in, false) {
		return
	}
	d := newCtrDates()
	si := ctrSettleIn(d, in.Method, in.Reference, in.SettledOn)
	if !d.ok {
		h.ctrBadBody(w, r, marinaID)
		return
	}
	items := make([]contract.SettleItem, 0, len(in.Items))
	for _, it := range in.Items {
		items = append(items, contract.SettleItem{ID: it.Id, Version: it.Version})
	}
	n, err := h.Contracts.SettleInstallments(r.Context(), principal(r), marinaID, items, si)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, api.BulkSettleResult{Settled: n})
}
