package view

import (
	"encoding/json"

	"github.com/coste360/porto/internal/domain/contract"
	"github.com/coste360/porto/internal/domain/presence"
	"github.com/coste360/porto/internal/http/api"
)

// Views of quotes, contracts, transits, inquiries, presences and movements (m3-plan §7.3). Helpers carry the
// ctr prefix: the occupancy views of the same package are written in parallel (m3-plan §11.3).

// ContractVessel is the embedded vessel brief of contract views (effective dimensions, no registration).
func ContractVessel(v *contract.VesselRow) api.VesselBrief {
	loa, beam, draft := v.LOACm, v.BeamCm, v.DraftCm
	if v.DimensionsSource == "measured" {
		if v.MeasuredLOACm > 0 {
			loa = v.MeasuredLOACm
		}
		if v.MeasuredBeamCm > 0 {
			beam = v.MeasuredBeamCm
		}
		if v.MeasuredDraftCm > 0 {
			draft = v.MeasuredDraftCm
		}
	}
	out := api.VesselBrief{Id: v.ID, Name: v.Name, LoaCm: loa, BeamCm: beam, HullType: api.HullType(v.HullType)}
	if draft > 0 {
		out.DraftCm = &draft
	}
	return out
}

func ctrParty(p *contract.Party) *api.CustomerBrief {
	if p == nil {
		return nil
	}
	return &api.CustomerBrief{Id: p.ID, DisplayName: String(p.Name)}
}

func ctrTerm(t string) *api.ContractTerm {
	if t == "" {
		return nil
	}
	v := api.ContractTerm(t)
	return &v
}

func ctrPolicy(raw []byte) *api.ContractPolicy {
	p, err := contract.DecodePolicy(raw)
	if err != nil || p.Version == 0 {
		return nil
	}
	ver := api.ContractPolicyVersion(p.Version)
	rp := api.ReleasePolicy(p.ReleasePolicy)
	return &api.ContractPolicy{Version: &ver, RepriceOnMove: &p.RepriceOnMove, ReleasePolicy: &rp, AllowSegments: &p.AllowSegments}
}

func ctrResourceCode(refs map[string]contract.ResourceRef, id string) *string {
	if r, ok := refs[id]; ok {
		return String(r.Code)
	}
	return nil
}

func ctrAllocation(a *contract.AllocationRow, contractID string, refs map[string]contract.ResourceRef, releases []contract.ReleaseRow) api.Allocation {
	out := api.Allocation{
		Id: a.ID, ResourceType: api.ResourceType(a.ResourceType), ResourceId: a.ResourceID, MapId: a.MapID,
		FirstNight: a.FirstNight.String(), LastNight: a.LastNight.String(), Kind: api.AllocationKind(a.Kind),
		Status: api.AllocationStatus(a.Status), Version: a.Version, XCm: a.XCm, YCm: a.YCm, RotationDd: a.RotationDd,
		ExpiresAt: Time(a.ExpiresAt), BlockReason: String(a.BlockReason), MovedFromId: String(a.MovedFromID),
		ContractLineId: String(a.ContractLineID), ContractId: String(contractID), VesselId: String(a.VesselID),
		ResourceCode: ctrResourceCode(refs, a.ResourceID), Released: []api.NightRange{},
	}
	if len(a.FitOverride) > 0 {
		var fo api.FitOverride
		if json.Unmarshal(a.FitOverride, &fo) == nil {
			if fo.Codes == nil {
				fo.Codes = []string{}
			}
			out.FitOverride = &fo
		}
	}
	for _, r := range releases {
		if r.AllocationID == a.ID && r.Status == "active" {
			out.Released = append(out.Released, api.NightRange{From: r.FromNight.String(), To: r.ToNight.String()})
		}
	}
	return out
}

func ctrRelease(r *contract.ReleaseRow, refs map[string]contract.ResourceRef) api.Release {
	return api.Release{
		Id: r.ID, AllocationId: r.AllocationID, ContractId: r.ContractID, ResourceType: api.ResourceType(r.ResourceType),
		ResourceId: r.ResourceID, ResourceCode: ctrResourceCode(refs, r.ResourceID), FromNight: r.FromNight.String(),
		ToNight: r.ToNight.String(), Status: api.ReleaseStatus(r.Status), Source: api.ReleaseSource(r.Source),
		ResoldAllocationId: String(r.ResoldAllocationID), ResoldNights: int(r.ResoldNights), CreditCents: r.CreditCents,
		Notes: String(r.Notes), RevokedAt: Time(r.RevokedAt), CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}
}

func ctrPresence(v *presence.View, showNotes bool) api.Presence {
	r := v.Row
	out := api.Presence{
		Id: r.ID, Vessel: ContractVessel(&v.Vessel), Event: api.PresenceEvent(r.Event), At: r.At.UTC(), Night: r.Night.String(),
		Source: api.PresenceSource(r.Source), UserId: String(r.UserID), AllocationId: String(r.AllocationID),
		ContractId: String(r.ContractID), HasPhoto: v.HasPhoto,
	}
	if showNotes {
		out.Notes = String(r.Notes)
	}
	return out
}

// Contract converts a contract or transit detail.
func Contract(d *contract.Detail) api.Contract {
	c := d.Row
	out := api.Contract{
		Id: c.ID, Kind: api.ContractKind(c.Kind), Term: ctrTerm(c.Term), Code: d.Code, Number: c.Number, Year: c.Year,
		Status: api.ContractStatus(c.Status), Vessel: ContractVessel(&d.Vessel), Customer: ctrParty(d.Customer),
		BillingCustomer: ctrParty(d.BillingCustomer), StartsOn: c.StartsOn.String(), EndsOn: c.EndsOn.String(),
		Channel: api.Channel(c.Channel), QuoteId: String(c.QuoteID), PriceListId: String(c.PriceListID),
		EngineVersion: c.EngineVersion, TotalCents: c.TotalCents, DepositCents: c.DepositCents, Policy: ctrPolicy(c.Policy),
		ExpectedArrivalTime: String(c.ExpectedArrivalTime), ExpectedDepartureTime: String(c.ExpectedDepartureTime),
		HoldLost: c.HoldLost, HoldExpiresAt: Time(d.HoldExpiresAt), ActivatedAt: Time(c.ActivatedAt),
		SignedOnPaper: c.SignedOnPaper, ConfirmedAt: Time(c.ConfirmedAt), PayAtDeparture: c.PayAtDeparture,
		CancelledAt: Time(c.CancelledAt), CancelReason: String(c.CancelReason), TerminateReason: String(c.TerminateReason),
		CreatedAt: c.CreatedAt.UTC(), Version: c.Version,
		Lines: []api.ContractLine{}, Allocations: []api.Allocation{}, Releases: []api.Release{}, Presences: []api.Presence{},
	}
	if d.ShowNotes {
		out.Notes = String(c.Notes)
	}
	if c.ConfirmationSource != "" {
		cs := api.ContractConfirmationSource(c.ConfirmationSource)
		out.ConfirmationSource = &cs
	}
	if d.ShowRef {
		out.ConfirmationRef = String(c.ConfirmationRef)
	}
	if !c.TerminatedOn.IsZero() {
		out.TerminatedOn = String(c.TerminatedOn.String())
	}
	if d.Snapshot != nil {
		b := PriceBreakdown(d.Snapshot.Breakdown, &d.Snapshot.Resolved)
		out.Breakdown = &b
	}
	for _, l := range d.Lines {
		cl := api.ContractLine{
			Id: l.ID, Seq: l.Seq, LineKind: api.ContractLineKind(l.LineKind), ItemId: String(l.ItemID), QtyMilli: l.QtyMilli,
			AmountCents: l.AmountCents, ResourceId: String(l.ResourceID), ResourceCode: ctrResourceCode(d.Resources, l.ResourceID),
		}
		if l.ResourceType != "" {
			rt := api.ResourceType(l.ResourceType)
			cl.ResourceType = &rt
		}
		out.Lines = append(out.Lines, cl)
	}
	for i := range d.Allocations {
		out.Allocations = append(out.Allocations, ctrAllocation(&d.Allocations[i], c.ID, d.Resources, d.Releases))
	}
	for i := range d.Releases {
		out.Releases = append(out.Releases, ctrRelease(&d.Releases[i], d.Resources))
	}
	for i := range d.Presences {
		pv := presence.View{Row: d.Presences[i], Vessel: d.Vessel, HasPhoto: d.Presences[i].PhotoFileID != ""}
		out.Presences = append(out.Presences, ctrPresence(&pv, d.ShowNotes))
	}
	return out
}

// ContractSummaries converts list rows.
func ContractSummaries(ss []contract.Summary) []api.ContractSummary {
	out := make([]api.ContractSummary, 0, len(ss))
	for i := range ss {
		s := &ss[i]
		c := s.Row
		cs := api.ContractSummary{
			Id: c.ID, Kind: api.ContractKind(c.Kind), Term: ctrTerm(c.Term), Code: s.Code, Status: api.ContractStatus(c.Status),
			Vessel: ContractVessel(&s.Vessel), Customer: ctrParty(s.Customer), StartsOn: c.StartsOn.String(),
			EndsOn: c.EndsOn.String(), TotalCents: c.TotalCents, HoldLost: c.HoldLost, HoldExpiresAt: Time(s.HoldExpiresAt),
			CreatedAt: c.CreatedAt.UTC(), Version: c.Version,
		}
		if s.PresenceState != "" {
			ps := api.PresenceState(s.PresenceState)
			cs.PresenceState = &ps
		}
		//nolint:revive // the field names are those of the generated anonymous struct
		cs.Resources = make([]struct {
			Code         string           `json:"code"`
			MapId        *string          `json:"map_id,omitempty"`
			ResourceId   string           `json:"resource_id"`
			ResourceType api.ResourceType `json:"resource_type"`
		}, 0, len(s.Resources))
		for _, r := range s.Resources {
			//nolint:revive // the field names are those of the generated anonymous struct
			cs.Resources = append(cs.Resources, struct {
				Code         string           `json:"code"`
				MapId        *string          `json:"map_id,omitempty"`
				ResourceId   string           `json:"resource_id"`
				ResourceType api.ResourceType `json:"resource_type"`
			}{Code: r.Code, MapId: String(r.MapID), ResourceId: r.ID, ResourceType: api.ResourceType(r.Type)})
		}
		out = append(out, cs)
	}
	return out
}

// ContractBrief converts a contract row to its brief.
func ContractBrief(c *contract.Row) *api.ContractBrief {
	if c == nil {
		return nil
	}
	return &api.ContractBrief{Id: c.ID, Code: contract.Code(c), Kind: api.ContractKind(c.Kind), Status: api.ContractStatus(c.Status), Term: ctrTerm(c.Term)}
}

// Quote converts a quote detail.
func Quote(d *contract.QuoteDetail) api.Quote {
	q := d.Row
	out := api.Quote{
		Id: q.ID, Code: d.Code, Number: q.Number, Year: q.Year, Status: api.QuoteStatus(q.Status), Kind: api.QuoteKind(q.Kind),
		Term: ctrTerm(q.Term), Customer: *ctrParty(d.Customer), Vessel: ContractVessel(&d.Vessel), StartsOn: q.StartsOn.String(),
		EndsOn: q.EndsOn.String(), ValidUntil: q.ValidUntil.String(), ExpiresAt: q.ExpiresAt.UTC(), Channel: api.Channel(q.Channel),
		Policy: ctrPolicy(q.Policy), InquiryId: String(q.InquiryID), AcceptedOptionId: String(q.AcceptedOptionID),
		ContractId: String(q.ContractID), SentAt: Time(q.SentAt), DecidedAt: Time(q.DecidedAt),
		DeclineReason: String(q.DeclineReason), CreatedAt: q.CreatedAt.UTC(), Version: q.Version, Options: []api.QuoteOption{},
	}
	if d.ShowNotes {
		out.Notes = String(q.Notes)
	}
	for _, o := range d.Options {
		ov := api.QuoteOption{
			Id: o.Row.ID, Seq: o.Row.Seq, Label: o.Row.Label, Hold: o.Row.Hold, PriceListId: String(o.Row.PriceListID),
			EngineVersion: o.Row.EngineVersion, TotalCents: o.Row.TotalCents, DepositCents: o.Row.DepositCents,
			Placements: []api.QuotePlacement{},
		}
		if len(o.Row.FitOverride) > 0 {
			var fo api.FitOverride
			if json.Unmarshal(o.Row.FitOverride, &fo) == nil {
				if fo.Codes == nil {
					fo.Codes = []string{}
				}
				ov.FitOverride = &fo
			}
		}
		if o.Snapshot != nil {
			b := PriceBreakdown(o.Snapshot.Breakdown, &o.Snapshot.Resolved)
			ov.Breakdown = &b
		}
		for _, pl := range o.Placements {
			qp := api.QuotePlacement{
				Seq: pl.Seq, ResourceType: api.ResourceType(pl.ResourceType), ResourceId: pl.ResourceID,
				ResourceCode: ctrResourceCode(d.Resources, pl.ResourceID), XCm: pl.XCm, YCm: pl.YCm, RotationDd: pl.RotationDd,
				FirstNight: pl.FirstNight.String(), LastNight: pl.LastNight.String(), AllocationId: String(pl.AllocationID),
			}
			if r, ok := d.Resources[pl.ResourceID]; ok {
				qp.MapId = String(r.MapID)
			}
			ov.Placements = append(ov.Placements, qp)
		}
		out.Options = append(out.Options, ov)
	}
	return out
}

// QuoteSummaries converts list rows.
func QuoteSummaries(ss []contract.QuoteSummary) []api.QuoteSummary {
	out := make([]api.QuoteSummary, 0, len(ss))
	for i := range ss {
		s := &ss[i]
		q := s.Row
		out = append(out, api.QuoteSummary{
			Id: q.ID, Code: s.Code, Status: api.QuoteStatus(q.Status), Kind: api.QuoteKind(q.Kind), Term: ctrTerm(q.Term),
			Customer: *ctrParty(s.Customer), Vessel: ContractVessel(&s.Vessel), StartsOn: q.StartsOn.String(),
			EndsOn: q.EndsOn.String(), ValidUntil: q.ValidUntil.String(), OptionsCount: s.OptionsCount,
			MinTotalCents: s.MinTotalCents, CreatedAt: q.CreatedAt.UTC(), Version: q.Version,
		})
	}
	return out
}

// Inquiry converts an inquiry view.
func Inquiry(v *contract.InquiryView) api.Inquiry {
	r := v.Row
	out := api.Inquiry{
		Id: r.ID, Channel: api.Channel(r.Channel), Source: api.InquirySource(r.Source), Purpose: api.SearchPurpose(r.Purpose),
		Term: ctrTerm(r.Term), LoaCm: r.LOACm, BeamCm: r.BeamCm, Arrival: r.Arrival.String(), Departure: r.Departure.String(),
		Outcome: api.InquiryOutcome(r.Outcome), QuoteId: String(r.QuoteID), ContractId: String(r.ContractID),
		Customer: ctrParty(v.Customer), CreatedAt: r.CreatedAt.UTC(), Version: r.Version,
	}
	if r.DraftCm > 0 {
		d := r.DraftCm
		out.DraftCm = &d
	}
	if v.Vessel != nil {
		vb := ContractVessel(v.Vessel)
		out.Vessel = &vb
	}
	if v.ShowNote {
		out.Notes = String(r.Notes)
	}
	return out
}

// Presence converts a presence view.
func Presence(v *presence.View, showNotes bool) api.Presence { return ctrPresence(v, showNotes) }

func ctrMovements(ms []presence.Movement) []api.Movement {
	out := make([]api.Movement, 0, len(ms))
	for i := range ms {
		m := &ms[i]
		mv := api.Movement{
			Vessel: ContractVessel(&m.Vessel), Contract: ContractBrief(m.Contract), ExpectedTime: String(m.ExpectedTime),
			PresenceId: String(m.PresenceID), AllocationId: String(m.AllocationID),
		}
		if m.Resource != nil {
			mv.Resource = &api.MovementResource{Type: api.ResourceType(m.Resource.Type), Id: m.Resource.ID, Code: m.Resource.Code, MapId: String(m.Resource.MapID)}
		}
		out = append(out, mv)
	}
	return out
}

// Movements converts the movements of a day.
func Movements(m *presence.Movements) api.Movements {
	return api.Movements{
		Date: m.Date.String(), ArrivalsExpected: ctrMovements(m.ArrivalsExpected), DeparturesExpected: ctrMovements(m.DeparturesExpected),
		VesselsOut: ctrMovements(m.VesselsOut), ArrivedWithoutBooking: ctrMovements(m.ArrivedWithoutBooking),
		Arrived: ctrMovements(m.Arrived), Departed: ctrMovements(m.Departed),
	}
}
