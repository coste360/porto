package view

import (
	"encoding/json"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/rights"
	"github.com/coste360/porto/internal/http/api"
)

// Wire forms of the M4 rights area (m4-plan §7.3.2): rights, transfers, mandates, fees, millesimal tables,
// budgets, common charge runs, holder instalments and the concession.

// rightsDate returns nil for the zero date.
func rightsDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func rcBerthRef(id string) api.ResourceRef {
	return api.ResourceRef{ResourceType: api.ResourceType(api.GroupResourceTypeBerth), ResourceId: id}
}

func rcCustomer(id, name string) api.CustomerBrief {
	return api.CustomerBrief{Id: id, DisplayName: String(name)}
}

func rcHolders(hs []rights.Holder) []api.RightHolder {
	out := make([]api.RightHolder, len(hs))
	for i, h := range hs {
		out[i] = api.RightHolder{Customer: rcCustomer(h.CustomerID, h.DisplayName), ShareMillis: h.ShareMillis}
	}
	return out
}

func rcFitOverride(raw json.RawMessage) *api.FitOverride {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var o struct {
		UserID string   `json:"user_id"`
		Reason string   `json:"reason"`
		Codes  []string `json:"codes"`
	}
	if json.Unmarshal(raw, &o) != nil {
		return nil
	}
	if o.Codes == nil {
		o.Codes = []string{}
	}
	return &api.FitOverride{UserId: o.UserID, Reason: o.Reason, Codes: o.Codes}
}

// Right converts a right.
func Right(r *rights.Right) api.Right {
	out := api.Right{
		Id: r.ID, Berth: rcBerthRef(r.BerthID), Kind: api.RightKind(r.Kind), FromOn: r.FromOn.String(), ToOn: r.ToOn.String(),
		Title:  api.RightTitle{Deed: String(r.Title.Deed), Number: String(r.Title.Number), Notary: String(r.Title.Notary), Date: rightsDate(r.Title.Date)},
		Status: api.RightStatus(r.Status), EndReason: String(r.EndReason), Holders: rcHolders(r.Holders), Vessels: []api.RightVessel{},
		ActiveMandateId: String(r.ActiveMandateID), CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}
	unpaid := r.UnpaidFees
	out.UnpaidFees = &unpaid
	for _, v := range r.Vessels {
		vb := api.VesselBrief{Id: v.VesselID, Name: v.Name, LoaCm: v.LOACm, BeamCm: v.BeamCm, HullType: api.HullType(v.HullType)}
		if v.DraftCm > 0 {
			d := v.DraftCm
			vb.DraftCm = &d
		}
		out.Vessels = append(out.Vessels, api.RightVessel{Vessel: vb, Primary: v.Primary, FitOverride: rcFitOverride(v.FitOverride)})
	}
	allocs := make([]api.Allocation, len(r.Allocations))
	for i := range r.Allocations {
		a := &r.Allocations[i]
		allocs[i] = api.Allocation{
			Id: a.ID, ResourceType: api.ResourceType(a.ResourceType), ResourceId: a.ResourceID, MapId: a.MapID, VesselId: String(a.VesselID),
			FirstNight: a.FirstNight.String(), LastNight: a.LastNight.String(), Kind: api.AllocationKind(a.Kind),
			Status: api.AllocationStatus(a.Status), Released: []api.NightRange{}, Version: a.Version,
			FitOverride: rcFitOverride(json.RawMessage(a.FitOverride)),
		}
	}
	out.Allocations = &allocs
	return out
}

// RightPage converts a page of rights.
func RightPage(items []rights.Right, next string) api.RightPage {
	out := api.RightPage{Items: make([]api.Right, len(items)), NextCursor: String(next)}
	for i := range items {
		out.Items[i] = Right(&items[i])
	}
	return out
}

// RightHistory converts the holder chain of a berth.
func RightHistory(h *rights.History) api.RightHistory {
	out := api.RightHistory{Berth: rcBerthRef(h.BerthID), Items: make([]api.RightHistoryEntry, len(h.Entries))}
	for i, e := range h.Entries {
		out.Items[i] = api.RightHistoryEntry{
			RightId: e.Right.ID, Kind: api.RightKind(e.Right.Kind), FromOn: e.Right.FromOn.String(), ToOn: e.Right.ToOn.String(),
			Status: api.RightStatus(e.Right.Status), TransferId: String(e.TransferID), Holders: rcHolders(e.Right.Holders),
		}
	}
	return out
}

// MapRights converts the rights lens of a map.
func MapRights(mapID string, on calendar.Date, items []rights.MapRightItem) api.MapRights {
	out := api.MapRights{MapId: mapID, Date: on.String()}
	out.Items = make([]struct {
		BerthId    string             `json:"berth_id"`             //nolint:revive // mirrors the generated anonymous struct
		MandateId  *string            `json:"mandate_id,omitempty"` //nolint:revive // mirrors the generated anonymous struct
		RightId    *string            `json:"right_id,omitempty"`   //nolint:revive // mirrors the generated anonymous struct
		Status     api.MapRightStatus `json:"status"`
		UnpaidFees bool               `json:"unpaid_fees"`
	}, len(items))
	for i, it := range items {
		out.Items[i].BerthId, out.Items[i].MandateId, out.Items[i].RightId = it.BerthID, String(it.MandateID), String(it.RightID)
		out.Items[i].Status, out.Items[i].UnpaidFees = api.MapRightStatus(it.Status), it.UnpaidFees
	}
	return out
}

// Transfer converts a transfer.
func Transfer(t *rights.Transfer) api.RightTransfer {
	from := rcHolders(t.From)
	ref := rcBerthRef(t.BerthID)
	out := api.RightTransfer{
		Id: t.ID, RightId: t.RightID, Berth: &ref, EffectiveOn: t.EffectiveOn.String(), FromHolders: &from, ToHolders: rcHolders(t.To),
		DeclaredPriceCents: t.DeclaredPriceCents, TakeoverItemId: String(t.TakeoverItemID), TakeoverFeeCents: t.TakeoverFeeCents,
		RequirementPolicy: api.RightTransferRequirementPolicy(t.RequirementPolicy), RequirementIssues: []api.TransferRequirementIssue{},
		FeesPolicy: api.RightTransferFeesPolicy(t.FeesPolicy), Status: api.TransferStatus(t.Status), NewRightId: String(t.NewRightID),
		DocumentId: String(t.DocumentID), SignatureRequestId: String(t.SignatureRequestID), CompletedAt: Time(t.CompletedAt),
		CreatedAt: t.CreatedAt.UTC(), Version: t.Version,
	}
	for _, is := range t.RequirementIssues {
		amount := is.AmountCents
		ids := is.HolderInstallmentIDs
		if ids == nil {
			ids = []string{}
		}
		out.RequirementIssues = append(out.RequirementIssues, api.TransferRequirementIssue{
			Code: api.TransferRequirementIssueCode(is.Code), CustomerId: String(is.CustomerID), AmountCents: &amount, HolderInstallmentIds: &ids,
		})
	}
	return out
}

// TransferPage converts a page of transfers.
func TransferPage(items []rights.Transfer, next string) api.RightTransferPage {
	out := api.RightTransferPage{Items: make([]api.RightTransfer, len(items)), NextCursor: String(next)}
	for i := range items {
		out.Items[i] = Transfer(&items[i])
	}
	return out
}

// Mandate converts a mandate.
func Mandate(m *rights.Mandate) api.Mandate {
	ref := rcBerthRef(m.BerthID)
	rev, share := m.RevenueCents, m.OwnerShareCents
	out := api.Mandate{
		Id: m.ID, RightId: m.RightID, Berth: &ref, FromOn: m.FromOn.String(), ToOn: m.ToOn.String(), OwnerShareBp: m.OwnerShareBP,
		MinGuaranteedCents: m.MinGuaranteedCents, Channels: make([]api.MandateChannel, len(m.Channels)),
		Settlement: api.MandateSettlement(m.Settlement), Status: api.MandateStatus(m.Status), EndedOn: rightsDate(m.EndedOn),
		RevenueCents: &rev, OwnerShareCents: &share, CreatedAt: m.CreatedAt.UTC(), Version: m.Version,
	}
	if m.Status != rights.MandateActive {
		kept := m.SoldNightsKept
		out.SoldNightsKept = &kept
	}
	for i, c := range m.Channels {
		out.Channels[i] = api.MandateChannel(c)
	}
	return out
}

// MandatePage converts a page of mandates.
func MandatePage(items []rights.Mandate, next string) api.MandatePage {
	out := api.MandatePage{Items: make([]api.Mandate, len(items)), NextCursor: String(next)}
	for i := range items {
		out.Items[i] = Mandate(&items[i])
	}
	return out
}

// MandateStatement converts a mandate statement.
func MandateStatement(st *rights.Statement) api.MandateStatement {
	out := api.MandateStatement{
		Mandate: Mandate(&st.Mandate), From: st.From.String(), To: st.To.String(), Lines: make([]api.MandateStatementLine, len(st.Lines)),
		RevenueCents: st.RevenueCents, OwnerShareCents: st.OwnerShareCents, GuaranteedTopUpCents: st.GuaranteedTopUpCents,
	}
	for i, l := range st.Lines {
		line := api.MandateStatementLine{
			AllocationId: l.AllocationID, FirstNight: rightsDate(l.FirstNight), LastNight: rightsDate(l.LastNight),
			Nights: l.Nights, RevenueCents: l.RevenueCents, OwnerShareCents: l.OwnerShareCents,
		}
		if l.ContractID != "" {
			cb := api.ContractBrief{Id: l.ContractID, Kind: api.ContractKind(l.ContractKind), Status: api.ContractStatus(l.ContractStatus), Code: l.ContractCode}
			if l.ContractTerm != "" {
				term := api.ContractTerm(l.ContractTerm)
				cb.Term = &term
			}
			line.Contract = &cb
		}
		out.Lines[i] = line
	}
	return out
}

// FeeTable converts a fee table.
func FeeTable(t *rights.FeeTable) api.FeeTable {
	out := api.FeeTable{
		Id: t.ID, Year: t.Year, Name: t.Name, Basis: api.FeeBasis(t.Basis), IstatBp: t.IstatBP,
		Status: api.FeeTableStatus(t.Status), Rows: make([]api.FeeTableRow, len(t.Rows)), Version: t.Version,
	}
	for i, r := range t.Rows {
		out.Rows[i] = api.FeeTableRow{CategoryId: String(r.CategoryID), FromCm: r.FromCm, ToCm: r.ToCm, AmountCents: r.AmountCents, RateCentsPerM2: r.RateCentsPerM2}
	}
	return out
}

// FeeTableRowsIn converts input rows.
func FeeTableRowsIn(rows []api.FeeTableRow) []rights.FeeRow {
	out := make([]rights.FeeRow, len(rows))
	for i, r := range rows {
		out[i] = rights.FeeRow{CategoryID: deref(r.CategoryId), FromCm: r.FromCm, ToCm: r.ToCm, AmountCents: r.AmountCents, RateCentsPerM2: r.RateCentsPerM2}
	}
	return out
}

// FeeSimulation converts the simulation of a fee run.
func FeeSimulation(t *rights.FeeTotals) api.FeeRunSimulation {
	out := api.FeeRunSimulation{TotalCents: t.TotalCents, PreviousTotalCents: t.PreviousTotalCents, DiffPreviousCents: t.DiffPreviousCents}
	b, _ := json.Marshal(struct {
		E []rights.FeeExcluded      `json:"excluded"`
		C []rights.FeeCategoryTotal `json:"totals_by_category"`
	}{t.Excluded, t.TotalsByCategory})
	_ = json.Unmarshal(b, &struct {
		E *[]struct {
			Reason  api.FeeRunSimulationExcludedReason `json:"reason"`
			RightId string                             `json:"right_id"` //nolint:revive // mirrors the generated anonymous struct
		} `json:"excluded"`
		C *[]struct {
			AmountCents   api.Cents  `json:"amount_cents"`
			Category      string     `json:"category"`
			PreviousCents *api.Cents `json:"previous_cents,omitempty"`
			Rights        int        `json:"rights"`
		} `json:"totals_by_category"`
	}{&out.Excluded, &out.TotalsByCategory})
	if out.Excluded == nil {
		out.Excluded = []struct {
			Reason  api.FeeRunSimulationExcludedReason `json:"reason"`
			RightId string                             `json:"right_id"` //nolint:revive // mirrors the generated anonymous struct
		}{}
	}
	if out.TotalsByCategory == nil {
		out.TotalsByCategory = []struct {
			AmountCents   api.Cents  `json:"amount_cents"`
			Category      string     `json:"category"`
			PreviousCents *api.Cents `json:"previous_cents,omitempty"`
			Rights        int        `json:"rights"`
		}{}
	}
	return out
}

// FeeRun converts a fee run.
func FeeRun(r *rights.FeeRun) api.FeeRun {
	out := api.FeeRun{
		Id: r.ID, Year: r.Year, TableId: r.TableID, Status: api.FeeRunStatus(r.Status), SimulatedAt: Time(r.SimulatedAt),
		IssuedAt: Time(r.IssuedAt), CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}
	_ = json.Unmarshal(r.Plan, &out.Plan)
	if r.Simulation != nil {
		sim := FeeSimulation(r.Simulation)
		out.Totals = &sim
	}
	return out
}

// FeeLine converts a fee line.
func FeeLine(l *rights.FeeLine) api.FeeLine {
	out := api.FeeLine{
		Id: l.ID, RightId: l.RightID, Berth: rcBerthRef(l.BerthID), Category: l.Category, BaseCents: l.BaseCents, Days: l.Days,
		AmountCents: l.AmountCents, PreviousCents: l.PreviousCents, Holders: make([]api.HolderAmount, len(l.HolderShares)),
		Excluded: l.Excluded, ExclusionReason: String(l.ExclusionReason),
	}
	for i, h := range l.HolderShares {
		out.Holders[i] = api.HolderAmount{Customer: rcCustomer(h.CustomerID, h.DisplayName), ShareMillis: h.ShareMillis, AmountCents: h.AmountCents}
	}
	return out
}

// MillesimalTable converts a millesimal table.
func MillesimalTable(t *rights.MillesimalTable) api.MillesimalTable {
	return api.MillesimalTable{
		Id: t.ID, Name: t.Name, Code: t.Code, Status: api.MillesimalTableStatus(t.Status), EntriesCount: t.EntriesCount,
		SumMillis: t.SumMillis, ActivatedAt: Time(t.ActivatedAt), Version: t.Version,
	}
}

// MillesimalEntries converts the entries of a table.
func MillesimalEntries(e *rights.MillesimalEntries) api.MillesimalEntries {
	out := api.MillesimalEntries{TableId: e.TableID, SumMillis: e.SumMillis, Entries: make([]api.MillesimalEntry, len(e.Entries))}
	for i, x := range e.Entries {
		out.Entries[i] = api.MillesimalEntry{BerthId: x.BerthID, BerthCode: String(x.BerthCode), Millis: x.Millis}
	}
	if len(e.Rejected) > 0 {
		rej := make([]api.ImportRowIssue, len(e.Rejected))
		for i, r := range e.Rejected {
			rej[i] = api.ImportRowIssue{Row: r.Row, Code: r.Code, Field: String(r.Field), Ref: String(r.Ref)}
		}
		out.Rejected = &rej
	}
	return out
}

// Budget converts a budget.
func Budget(b *rights.Budget) api.Budget {
	return api.Budget{
		Id: b.ID, Year: b.Year, Name: b.Name, Status: api.BudgetStatus(b.Status), BudgetCents: b.BudgetCents,
		ActualCents: b.ActualCents, ItemsCount: b.ItemsCount, ApprovedAt: Time(b.ApprovedAt), ClosedAt: Time(b.ClosedAt), Version: b.Version,
	}
}

// BudgetItem converts a budget item.
func BudgetItem(it *rights.BudgetItem) api.BudgetItem {
	adv := it.AdvancesCents
	return api.BudgetItem{
		Id: it.ID, BudgetId: it.BudgetID, Name: it.Name, TableId: it.TableID, BudgetCents: it.BudgetCents,
		ActualCents: it.ActualCents, AdvancesCents: &adv, Sort: it.Sort, Version: it.Version,
	}
}

// BudgetItems converts items.
func BudgetItems(items []rights.BudgetItem) api.BudgetItemList {
	out := api.BudgetItemList{Items: make([]api.BudgetItem, len(items))}
	for i := range items {
		out.Items[i] = BudgetItem(&items[i])
	}
	return out
}

// CommonRun converts a common charge run.
func CommonRun(r *rights.CommonRun) api.CommonChargeRun {
	out := api.CommonChargeRun{
		Id: r.ID, BudgetId: r.BudgetID, Kind: api.CommonRunKind(r.Kind), Seq: r.Seq, ShareBp: r.ShareBP,
		ReferenceOn: r.ReferenceOn.String(), Status: api.CommonChargeRunStatus(r.Status), TotalCents: r.Totals.TotalCents,
		Holders: r.Totals.Holders, IssuedAt: Time(r.IssuedAt), Version: r.Version,
	}
	b, _ := json.Marshal(r.Totals.Items)
	_ = json.Unmarshal(b, &out.TotalsByItem)
	if out.TotalsByItem == nil {
		_ = json.Unmarshal([]byte("[]"), &out.TotalsByItem)
	}
	return out
}

// CommonAllocation converts an allocation of a run.
func CommonAllocation(a *rights.CommonAllocation) api.CommonAllocation {
	out := api.CommonAllocation{
		Id: a.ID, ItemId: a.ItemID, Berth: rcBerthRef(a.BerthID), RightId: String(a.RightID),
		BerthShareCents: a.BerthShareCents, AmountCents: a.AmountCents,
	}
	if a.CustomerID != "" {
		c := rcCustomer(a.CustomerID, a.DisplayName)
		out.Customer = &c
	}
	return out
}

// HolderInstallment converts a holder instalment.
func HolderInstallment(h *rights.HolderInstallment) api.HolderInstallment {
	out := api.HolderInstallment{
		Id: h.ID, SourceType: api.HolderInstallmentSource(h.SourceType), SourceId: h.SourceID, RunId: String(h.RunID), RightId: String(h.RightID),
		Customer: rcCustomer(h.CustomerID, h.DisplayName), Seq: h.Seq, Kind: api.HolderInstallmentKind(h.Kind), IssueOn: h.IssueOn.String(),
		DueOn: h.DueOn.String(), AmountCents: h.AmountCents, Status: api.InstallmentStatus(h.Status), Overdue: h.Overdue,
		SettledOn: rightsDate(h.SettledOn), Version: h.Version,
	}
	if h.BerthID != "" {
		ref := rcBerthRef(h.BerthID)
		out.Berth = &ref
	}
	if h.SettlementMethod != "" {
		m := api.SettlementMethod(h.SettlementMethod)
		out.SettlementMethod = &m
	}
	return out
}

// Concession converts the concession card.
func Concession(c *rights.Concession) api.Concession {
	out := api.Concession{
		Authority: c.Authority, DeedKind: c.DeedKind, DeedNumber: c.DeedNumber, GrantedOn: rightsDate(c.GrantedOn),
		ExpiresOn: rightsDate(c.ExpiresOn), AnnualFeeCents: c.AnnualFeeCents, Surfaces: make([]api.ConcessionSurface, len(c.Surfaces)),
		Prescriptions: c.Prescriptions, Status: api.ConcessionStatus(c.Status), Version: c.Version,
	}
	for i, s := range c.Surfaces {
		out.Surfaces[i] = api.ConcessionSurface{Kind: api.ConcessionSurfaceKind(s.Kind), AreaCm2: s.AreaCm, Note: String(s.Note)}
	}
	return out
}

// Obligation converts a concession obligation.
func Obligation(o *rights.Obligation) api.ConcessionObligation {
	return api.ConcessionObligation{
		Id: o.ID, Kind: api.ObligationKind(o.Kind), Description: o.Description, DueOn: o.DueOn.String(),
		Recurrence: api.ConcessionObligationRecurrence(o.Recurrence), AmountCents: o.AmountCents, DoneOn: rightsDate(o.DoneOn), Version: o.Version,
	}
}
