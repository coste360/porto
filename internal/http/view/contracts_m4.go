package view

import (
	"encoding/json"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/schedule"
	"github.com/coste360/porto/internal/http/api"
)

// Views of the M4 contract operations (m4-plan §7.3.1): instalments, settings, renewals, charter, addenda and
// the waiting list. Customer names appear only through contract.Party (filled with customers.read).

// ContractM4 is Contract with the M4 fields (signature, suspension, renewal, termination amount, charter,
// full policy v2).
func ContractM4(d *contract.Detail) api.Contract {
	out := Contract(d)
	c := d.Row
	out.Policy = PolicyM4(c.Policy)
	out.SignedAt, out.SignedDocumentId = Time(c.SignedAt), String(c.SignedDocumentID)
	out.SignatureRequestId = String(c.SignatureRequestID)
	out.SuspendedAt, out.SuspendReason = Time(c.SuspendedAt), String(c.SuspendReason)
	out.RenewedAt, out.TerminationAmountCents = Time(c.RenewedAt), c.TerminationAmountCents
	pv := c.InstallmentPlanVersion
	out.InstallmentPlanVersion = &pv
	out.RenewalProposalId, out.SuccessorContractId = String(d.RenewalProposalID), String(d.SuccessorID)
	if ch := d.Charter; ch != nil {
		info := api.CharterInfo{
			FleetId: ch.FleetID, FleetName: String(d.FleetName), ChangeoverWeekday: ch.ChangeoverWeekday,
			PricingMode: api.CharterPricingMode(ch.PricingMode), OutNights: []api.OutNights{},
		}
		var ons []contract.OutNight
		if jsonDecode(ch.OutNights, &ons) {
			for _, o := range ons {
				info.OutNights = append(info.OutNights, api.OutNights{From: o.From.String(), To: o.To.String(), BerthId: String(o.BerthID)})
			}
		}
		out.Charter = &info
	}
	return out
}

// PolicyM4 converts a stored policy with its M4 clauses (nil when absent or unreadable).
func PolicyM4(raw []byte) *api.ContractPolicy {
	p, err := contract.DecodePolicy(raw)
	if err != nil || p.Version == 0 {
		return nil
	}
	return PolicyOf(p)
}

// PolicyOf converts a decoded policy.
func PolicyOf(p contract.Policy) *api.ContractPolicy {
	ver := api.ContractPolicyVersion(p.Version)
	out := &api.ContractPolicy{Version: &ver, RepriceOnMove: &p.RepriceOnMove, AllowSegments: &p.AllowSegments}
	if p.ReleasePolicy != "" {
		rp := api.ReleasePolicy(p.ReleasePolicy)
		out.ReleasePolicy = &rp
	}
	if p.Payment != nil {
		pay := PaymentPolicy(*p.Payment)
		out.Payment = &pay
	}
	if r := p.Renewal; r != nil {
		out.Renewal = &api.RenewalPolicy{Tacit: &r.Tacit, NoticeDays: &r.NoticeDays}
	}
	if c := p.Cancellation; c != nil {
		ref := api.CancellationPolicyRefund(c.Refund)
		out.Cancellation = &api.CancellationPolicy{NoticeDays: &c.NoticeDays, PenaltyBp: &c.PenaltyBP, Refund: &ref}
	}
	return out
}

// PaymentPolicy converts a payment clause.
func PaymentPolicy(p contract.PaymentPolicy) api.PaymentPolicy {
	ru := api.PaymentPolicyRoundingUnitCents(p.RoundingUnitCents)
	if ru == 0 {
		ru = 1
	}
	out := api.PaymentPolicy{
		DepositDueDays: Int(p.DepositDueDays), IssueDaysBeforeDue: Int(p.IssueDaysBeforeDue), RoundingUnitCents: &ru,
		Sepa: &p.SEPA,
		Schedule: &api.PaymentSchedule{
			Mode: api.PaymentScheduleMode(p.Schedule.Mode), Count: optInt(p.Schedule.Count), EveryMonths: optInt(p.Schedule.EveryMonths),
			FirstDueOffsetMonths: optInt(p.Schedule.FirstDueOffsetMonths), DueOffsetDays: optInt(p.Schedule.DueOffsetDays),
		},
	}
	if d := p.Deposit; d != nil {
		out.Deposit = &api.PaymentDeposit{Bp: d.BP, Cents: d.Cents}
	}
	if e := p.EarlyPayment; e != nil {
		out.EarlyPayment = &api.EarlyPayment{DiscountBp: e.DiscountBP, PayByOffsetDays: e.PayByOffsetDays}
	}
	if len(p.Schedule.Dates) > 0 {
		ds := make([]api.FixedDue, 0, len(p.Schedule.Dates))
		for _, d := range p.Schedule.Dates {
			ds = append(ds, api.FixedDue{DueOn: d.DueOn, Bp: d.BP})
		}
		out.Schedule.Dates = &ds
	}
	return out
}

func optInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// Installment converts an instalment row of the scadenzario.
func Installment(r *contract.InstallmentRow) api.Installment {
	out := api.Installment{
		Id: r.ID, ContractId: r.ContractID, ContractCode: String(r.ContractCode), Customer: ctrParty(r.Customer),
		PlanVersion: r.PlanVersion, Seq: r.Seq, Kind: api.InstallmentKind(r.Kind), IssueOn: r.IssueOn.String(),
		DueOn: r.DueOn.String(), AmountCents: r.AmountCents, Status: api.InstallmentStatus(r.Status), Overdue: r.Overdue,
		IssuedAt: Time(r.IssuedAt), SettledAt: Time(r.SettledAt), ChargeId: String(r.ChargeID), Version: r.Version,
		SettlementReference: String(r.Reference),
	}
	if !r.SettledOn.IsZero() {
		out.SettledOn = String(r.SettledOn.String())
	}
	if r.SettlementMethod != "" {
		m := api.SettlementMethod(r.SettlementMethod)
		out.SettlementMethod = &m
	}
	if r.SettlementSource != "" {
		src := api.SettlementSource(r.SettlementSource)
		out.SettlementSource = &src
	}
	return out
}

// Installments converts rows.
func Installments(rs []contract.InstallmentRow) []api.Installment {
	out := make([]api.Installment, 0, len(rs))
	for i := range rs {
		out = append(out, Installment(&rs[i]))
	}
	return out
}

// InstallmentPlan converts the live plan of a contract.
func InstallmentPlan(contractID string, p *contract.PlanView, code string) api.InstallmentPlan {
	out := api.InstallmentPlan{
		ContractId: contractID, PlanVersion: p.PlanVersion, TotalCents: int64(p.Total), SumCents: int64(p.Sum),
		Items: make([]api.Installment, 0, len(p.Items)),
	}
	for _, r := range p.Items {
		row := contract.InstallmentRow{Installment: r, ContractCode: code}
		out.Items = append(out.Items, Installment(&row))
	}
	if alt := p.EarlyPayment; alt != nil {
		ep := &struct {
			DiscountCents api.Cents                `json:"discount_cents"`
			Items         []api.PlannedInstallment `json:"items"`
			TotalCents    api.Cents                `json:"total_cents"`
		}{DiscountCents: int64(alt.Discount), TotalCents: int64(alt.Total), Items: planned(alt.Items)}
		out.Alternatives = &struct {
			EarlyPayment *struct {
				DiscountCents api.Cents                `json:"discount_cents"`
				Items         []api.PlannedInstallment `json:"items"`
				TotalCents    api.Cents                `json:"total_cents"`
			} `json:"early_payment,omitempty"`
		}{EarlyPayment: ep}
	}
	return out
}

func planned(items []schedule.Item) []api.PlannedInstallment {
	out := make([]api.PlannedInstallment, 0, len(items))
	for _, it := range items {
		out = append(out, api.PlannedInstallment{
			Seq: it.Seq, Kind: api.InstallmentKind(it.Kind), IssueOn: it.IssueOn.String(), DueOn: it.DueOn.String(), AmountCents: int64(it.Amount),
		})
	}
	return out
}

// ContractSettings converts the settings.
func ContractSettings(s *contract.Settings) api.ContractSettings {
	r := s.Row
	out := api.ContractSettings{
		ArrearsDays: r.ArrearsDays, InsurancePolicy: api.InsurancePolicy(r.InsurancePolicy), MarinaCountersign: r.MarinaCountersign,
		ProviderCode: String(r.ProviderCode), RenewalDeadlineDays: r.RenewalDeadlineDays, RequirePaperScan: r.RequirePaperScan,
		SignatureDays: r.SignatureDays, SignatureDriver: api.SignatureDriver(r.SignatureDriver),
		SignatureReminderDays: r.SignatureReminderDays, Version: r.Version, OfficeEmail: String(s.OfficeEmail),
		RequiredFields: []api.RequiredField{},
	}
	pol := s.Policy
	if pol.Version == 0 {
		pol.Version = contract.PolicyVersion
	}
	if p := PolicyOf(pol); p != nil {
		out.DefaultPolicy = *p
	}
	var fields []string
	if jsonDecode(r.RequiredFields, &fields) {
		for _, f := range fields {
			out.RequiredFields = append(out.RequiredFields, api.RequiredField(f))
		}
	}
	if b := s.Bank; b != nil {
		out.BankDetails = &api.BankDetails{Iban: String(b.IBAN), Holder: String(b.Holder), Bank: String(b.Bank), Bic: String(b.BIC)}
	}
	return out
}

func m4ContractBrief(c *contract.Row) api.ContractBrief {
	return api.ContractBrief{Id: c.ID, Code: contract.Code(c), Kind: api.ContractKind(c.Kind), Status: api.ContractStatus(c.Status), Term: ctrTerm(c.Term)}
}

// RenewalProposal converts a proposal.
func RenewalProposal(p *contract.Proposal) api.RenewalProposal {
	r := p.Row
	out := api.RenewalProposal{
		Id: r.ID, CampaignId: String(r.CampaignID), Status: api.RenewalProposalStatus(r.Status), DeadlineOn: r.DeadlineOn.String(),
		OldTotalCents: r.OldTotalCents, NewTotalCents: r.NewTotalCents, SentAt: Time(r.SentAt), SentCount: r.SentCount,
		AcceptedAt: Time(r.AcceptedAt), DecidedAt: Time(r.DecidedAt), DeclineReason: String(r.DeclineReason),
		IncludedReason: String(r.IncludedReason), CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}
	if r.ExclusionReason != "" {
		er := api.RenewalExclusionReason(r.ExclusionReason)
		out.ExclusionReason = &er
	}
	if r.AcceptedVia != "" {
		v := api.RenewalProposalAcceptedVia(r.AcceptedVia)
		out.AcceptedVia = &v
	}
	if r.AcceptedPlan != "" {
		pl := api.PlanChoice(r.AcceptedPlan)
		out.AcceptedPlan = &pl
	}
	if p.Source != nil {
		out.SourceContract = m4ContractBrief(p.Source)
		out.Customer = &api.CustomerBrief{Id: p.Source.CustomerID}
	} else {
		out.SourceContract = api.ContractBrief{Id: r.SourceContractID}
	}
	if s := p.Successor; s != nil {
		b := m4ContractBrief(s)
		out.NewContract = &b
		out.NewStartsOn, out.NewEndsOn = String(s.StartsOn.String()), String(s.EndsOn.String())
		out.SignatureRequestId = String(s.SignatureRequestID)
		out.HoldExpiresAt = Time(p.HoldExpiresAt) // the berth hold of the successor draft (m4-plan §15 D4)
	}
	return out
}

// RenewalProposals converts proposals.
func RenewalProposals(ps []contract.Proposal) []api.RenewalProposal {
	out := make([]api.RenewalProposal, 0, len(ps))
	for i := range ps {
		out = append(out, RenewalProposal(&ps[i]))
	}
	return out
}

// RenewalCampaign converts a campaign.
func RenewalCampaign(c *contract.Campaign) api.RenewalCampaign {
	r := c.Row
	out := api.RenewalCampaign{
		Id: r.ID, Name: r.Name, Status: api.RenewalCampaignStatus(r.Status), DeadlineOn: r.DeadlineOn.String(),
		PriceListId: String(r.PriceListID), TargetStartsRule: api.RenewalCampaignTargetStartsRule(r.TargetStartsRule),
		Message: r.Message, LaunchedAt: Time(r.LaunchedAt), ClosedAt: Time(r.ClosedAt), CreatedAt: r.CreatedAt.UTC(),
		Version: r.Version, Counts: api.RenewalCounts(c.Counts), Filter: RenewalFilter(c.Filter),
	}
	if out.Counts == nil {
		out.Counts = api.RenewalCounts{}
	}
	if !r.TargetStartsOn.IsZero() {
		out.TargetStartsOn = String(r.TargetStartsOn.String())
	}
	return out
}

// RenewalFilter converts a campaign filter.
func RenewalFilter(f contract.CampaignFilter) api.RenewalFilter {
	out := api.RenewalFilter{}
	if len(f.Groups) > 0 {
		out.Groups = &f.Groups
	}
	if len(f.Structures) > 0 {
		out.Structures = &f.Structures
	}
	if len(f.ContractIDs) > 0 {
		out.ContractIds = &f.ContractIDs
	}
	if len(f.CustomerCategories) > 0 {
		out.CustomerCategories = &f.CustomerCategories
	}
	if len(f.Terms) > 0 {
		ts := make([]api.ContractTerm, 0, len(f.Terms))
		for _, t := range f.Terms {
			ts = append(ts, api.ContractTerm(t))
		}
		out.Terms = &ts
	}
	if f.EndsFrom != nil {
		out.EndsFrom = String(f.EndsFrom.String())
	}
	if f.EndsTo != nil {
		out.EndsTo = String(f.EndsTo.String())
	}
	return out
}

// CharterFleet converts a fleet.
func CharterFleet(f *contract.Fleet) api.CharterFleet {
	out := api.CharterFleet{
		Id: f.Row.ID, Name: f.Row.Name, Version: f.Row.Version, CreatedAt: f.Row.CreatedAt.UTC(),
		Customer: api.CustomerBrief{Id: f.Row.CustomerID}, Vessels: []api.CharterFleetVessel{},
	}
	if f.Customer != nil {
		out.Customer = *ctrParty(f.Customer)
	}
	for _, v := range f.Vessels {
		vb := api.VesselBrief{Id: v.VesselID}
		if row, ok := f.Names[v.VesselID]; ok {
			vb = ContractVessel(&row)
		}
		out.Vessels = append(out.Vessels, api.CharterFleetVessel{Active: v.Active, Vessel: vb})
	}
	return out
}

// Addendum converts an addendum.
func Addendum(a *contract.Addendum) api.Addendum {
	r := a.Row
	pl := a.Payload
	out := api.Addendum{
		Id: r.ID, ContractId: r.ContractID, Seq: r.Seq, Kind: api.AddendumKind(r.Kind), EffectiveOn: r.EffectiveOn.String(),
		AmountDeltaCents: r.AmountDeltaCents, Status: api.AddendumStatus(r.Status), DocumentId: String(r.DocumentID),
		SignatureRequestId: String(r.SignatureRequestID), ActivatedAt: Time(r.ActivatedAt), CreatedAt: r.CreatedAt.UTC(),
		Version: r.Version,
		Payload: api.AddendumPayload{
			ItemId: String(pl.ItemID), LineId: String(pl.LineID), LandResourceId: String(pl.LandResourceID),
			CustomerId: String(pl.CustomerID), NewTotalCents: pl.NewTotalCents, Reason: String(pl.Reason), VesselId: String(pl.VesselID),
		},
	}
	if pl.QtyMilli != 0 {
		q := pl.QtyMilli
		out.Payload.QtyMilli = &q
	}
	if a.PriceEffect {
		old, nw := pl.OldRemainingCents, pl.NewRemainingCents
		out.PriceEffect = &struct {
			NewRemainingCents     *api.Cents `json:"new_remaining_cents,omitempty"`
			OldRemainingCents     *api.Cents `json:"old_remaining_cents,omitempty"`
			ReplannedInstallments *int       `json:"replanned_installments,omitempty"`
		}{NewRemainingCents: &nw, OldRemainingCents: &old}
		if a.Replanned > 0 {
			out.PriceEffect.ReplannedInstallments = Int(a.Replanned)
		}
	}
	return out
}

// WaitlistEntry converts a waiting list entry.
func WaitlistEntry(w *contract.WaitlistRow) api.WaitlistEntry {
	out := api.WaitlistEntry{
		Id: w.ID, Customer: api.CustomerBrief{Id: w.CustomerID}, LoaCm: w.LOACm, BeamCm: w.BeamCm, Term: api.ContractTerm(w.Term),
		GroupIds: append([]string{}, w.GroupIDs...), RequestedFrom: w.RequestedFrom.String(), Priority: w.Priority,
		Status: api.WaitlistStatus(w.Status), OfferedQuoteId: String(w.OfferedQuoteID), OfferedAt: Time(w.OfferedAt),
		CreatedAt: w.CreatedAt.UTC(), Version: w.Version,
	}
	if w.Customer != nil {
		out.Customer = *ctrParty(w.Customer)
		if w.Customer.Name != "" { // names (and notes) only with customers.read
			out.Notes = w.Notes
		}
	}
	if w.DraftCm > 0 {
		d := w.DraftCm
		out.DraftCm = &d
	}
	if w.Vessel != nil {
		vb := ContractVessel(w.Vessel)
		out.Vessel = &vb
	}
	return out
}

func jsonDecode(raw []byte, v any) bool {
	return len(raw) > 0 && json.Unmarshal(raw, v) == nil
}
