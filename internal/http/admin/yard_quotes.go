package admin

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/yardquote"
	"github.com/coste360/porto/internal/domain/yardquote/price"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/respond"
	"github.com/coste360/porto/internal/http/view"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/qty"
)

// Yard quotes (M10, m10-plan §4, §13): list, preview, create, update, recompute, send, accept, decline, PDF.
// Owned by QUOTE.

func yardDate(s *api.IsoDate) (*calendar.Date, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	d, err := calendar.Parse(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func yardIsoDate(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func yardSelections(in []api.YardQuoteSelection) []yardquote.Selection {
	out := make([]yardquote.Selection, 0, len(in))
	for _, s := range in {
		out = append(out, yardquote.Selection{PackageID: str(s.PackageId), ItemID: str(s.ItemId)})
	}
	return out
}

func yardOverrides(in *[]api.YardLineOverride) []price.Override {
	if in == nil {
		return nil
	}
	out := make([]price.Override, 0, len(*in))
	for _, o := range *in {
		po := price.Override{Seq: o.Seq, Qty: yardMilli(o.QtyMilli), Description: o.Description, Remove: o.Remove != nil && *o.Remove}
		if o.UnitCents != nil {
			c := money.Cents(*o.UnitCents)
			po.Unit = &c
		}
		if o.DiscountCents != nil {
			c := money.Cents(*o.DiscountCents)
			po.Discount = &c
		}
		out = append(out, po)
	}
	return out
}

func yardManualLines(in *[]api.YardManualLine) []yardquote.ManualLine {
	if in == nil {
		return nil
	}
	out := make([]yardquote.ManualLine, 0, len(*in))
	for _, m := range *in {
		ml := yardquote.ManualLine{
			ItemID: m.ItemId, WorkTypeID: str(m.WorkTypeId), Description: str(m.Description), Qty: qty.Milli(m.QtyMilli), Unit: money.Cents(m.UnitCents),
		}
		if m.DiscountCents != nil {
			ml.Discount = money.Cents(*m.DiscountCents)
		}
		out = append(out, ml)
	}
	return out
}

// yardQuoteInput maps the create/preview body; a malformed date or value is reported as a bad body.
func yardQuoteInput(in api.YardQuoteInput) (yardquote.QuoteInput, bool) {
	jobs, err := yardValuesOf(in.JobInputs)
	if err != nil {
		return yardquote.QuoteInput{}, false
	}
	vo, err := yardValuesOf(in.VesselOverrides)
	if err != nil {
		return yardquote.QuoteInput{}, false
	}
	out := yardquote.QuoteInput{
		CustomerID: in.CustomerId, VesselID: in.VesselId, Selections: yardSelections(in.Selections), JobInputs: jobs, VesselOverrides: vo,
		Overrides: yardOverrides(in.Overrides), ManualLines: yardManualLines(in.ManualLines), DepositBP: in.DepositBp,
		DepositCents: in.DepositCents, Conditions: str(in.Conditions), Notes: str(in.Notes),
	}
	if in.BillingMode != nil {
		out.BillingMode = string(*in.BillingMode)
	}
	for _, d := range []struct {
		src *api.IsoDate
		dst **calendar.Date
	}{{in.QuotedOn, &out.QuotedOn}, {in.ValidUntil, &out.ValidUntil}, {in.PlannedStartOn, &out.PlannedStartOn}, {in.PlannedDeliveryOn, &out.PlannedDeliveryOn}} {
		v, err := yardDate(d.src)
		if err != nil {
			return yardquote.QuoteInput{}, false
		}
		*d.dst = v
	}
	return out, true
}

func yardLineIssues(is []price.LineIssue) []api.FormulaIssue {
	out := make([]api.FormulaIssue, 0, len(is))
	for _, i := range is {
		fi := api.FormulaIssue{Code: i.Code, Field: view.String(i.Field), Ref: view.String(i.Ref)}
		if i.Pos != 0 {
			pos := i.Pos
			fi.Pos = &pos
		}
		out = append(out, fi)
	}
	return out
}

func apiYardLines(lines []yardquote.LineView) []api.YardQuoteLine {
	out := make([]api.YardQuoteLine, 0, len(lines))
	for _, l := range lines {
		it, unit := api.ItemType(l.ItemType), api.ItemUnit(l.ItemUnit)
		al := api.YardQuoteLine{
			Seq: l.Spec.Seq, Source: api.YardLineSource(l.Spec.Source), ItemId: l.Spec.ItemID, ItemCode: l.ItemCode, ItemType: &it, Unit: &unit,
			WorkTypeId: l.Spec.WorkTypeID, QualificationId: view.String(l.QualificationID), Description: l.Spec.Description,
			QtyFormula: view.String(l.Spec.QtyFormula), PriceFormula: view.String(l.Spec.PriceFormula), QtyMilli: int64(l.Qty),
			UnitCents: int64(l.Unit), DiscountCents: int64(l.Discount), QtyOverridden: l.QtyOverridden, PriceOverridden: l.PriceOverridden,
			Manual: l.Manual(), NetCents: int64(l.Net), VatCents: int64(l.VAT), GrossCents: int64(l.Gross), VatRateBp: l.VATData.RateBP,
			PriceIncludesVat: l.VATData.PriceIncludesVAT, EstimatedHoursMilli: int64(l.EstimatedHours), PriceListId: view.String(l.PriceListID),
			Issues: yardLineIssues(l.Issues),
		}
		if l.VATData.Nature != "" {
			n := api.VatNature(l.VATData.Nature)
			al.VatNature = &n
		}
		if l.Spec.Source == price.SourcePackage {
			pid, pv, ps := l.Spec.PackageID, l.Spec.PackageVersion, l.Spec.PackageLineSeq
			al.PackageId, al.PackageVersion, al.PackageLineSeq = &pid, &pv, &ps
		}
		if t := l.Spec.Task; t != nil {
			tk := api.WorkPackageTask{Title: api.I18nText{It: t.Title["it"]}}
			if en, ok := t.Title["en"]; ok && en != "" {
				tk.Title.En = &en
			}
			if len(t.Checklist) > 0 {
				c := append([]string{}, t.Checklist...)
				tk.Checklist = &c
			}
			al.Task = &tk
		}
		out = append(out, al)
	}
	return out
}

func apiYardTotals(t price.Totals) api.YardQuoteTotals {
	out := api.YardQuoteTotals{
		NetCents: int64(t.Net), VatCents: int64(t.VAT), GrossCents: int64(t.Gross), DepositAmountCents: int64(t.Deposit), Vat: []api.YardVatBucket{},
	}
	for _, b := range t.Buckets {
		vb := api.YardVatBucket{VatRateBp: b.RateBP, NetCents: int64(b.Net), VatCents: int64(b.VAT), GrossCents: int64(b.Gross)}
		if b.Nature != "" {
			n := api.VatNature(b.Nature)
			vb.VatNature = &n
		}
		out.Vat = append(out.Vat, vb)
	}
	return out
}

func apiYardHours(hs []yardquote.HoursView) []api.YardQualificationHours {
	out := make([]api.YardQualificationHours, 0, len(hs))
	for _, h := range hs {
		c := int64(h.Cost)
		out = append(out, api.YardQualificationHours{QualificationId: h.QualificationID, HoursMilli: int64(h.Hours), CostCents: &c})
	}
	return out
}

func apiYardAsks(as []yardquote.VariableAsk) []api.YardVariableInput {
	out := make([]api.YardVariableInput, 0, len(as))
	for _, a := range as {
		v := api.YardVariableInput{
			Name: a.Name, Source: api.FormulaVariableSource(a.Source), Kind: api.FormulaVariableKind(a.Kind), Unit: api.FormulaUnit(a.Unit),
			Label: apiYardText(a.Label), MinMilli: yardInt64(a.Min), MaxMilli: yardInt64(a.Max), Required: a.Required,
		}
		if len(a.Enum) > 0 {
			e := append([]string{}, a.Enum...)
			v.EnumValues = &e
		}
		if a.Default != nil {
			d := apiYardValue(*a.Default)
			v.Default = &d
		}
		if a.Value != nil {
			x := apiYardValue(*a.Value)
			v.Value = &x
		}
		out = append(out, v)
	}
	return out
}

func apiYardPreview(p *yardquote.Preview) api.YardQuotePreview {
	return api.YardQuotePreview{
		Lines: apiYardLines(p.Lines), Totals: apiYardTotals(p.Totals), EstimatedHours: apiYardHours(p.Hours),
		Variables: apiYardAsks(p.Variables), Issues: yardLineIssues(p.Issues), Blocking: p.Blocking,
	}
}

func apiYardQuote(q *yardquote.QuoteView) api.YardQuote {
	r := q.Row
	out := api.YardQuote{
		Id: r.ID, Number: q.Number, Kind: api.YardQuoteKind(r.Kind), WorkOrderId: view.String(r.WorkOrderID), CustomerId: r.CustomerID,
		VesselId: r.VesselID, Status: api.YardQuoteStatus(r.Status), QuotedOn: r.QuotedOn.String(), ValidUntil: r.ValidUntil.String(),
		ExpiresAt: r.ExpiresAt.UTC(), BillingMode: api.YardBillingMode(r.BillingMode), Conditions: r.Conditions, Notes: r.Notes,
		PlannedStartOn: yardIsoDate(r.PlannedStartOn), PlannedDeliveryOn: yardIsoDate(r.PlannedDeliveryOn),
		JobInputs: apiYardValues(q.Snapshot.JobInputs), VesselOverrides: apiYardValues(q.Snapshot.VesselOverrides),
		Lines: apiYardLines(q.Lines), Totals: apiYardTotals(q.Totals), EstimatedHours: apiYardHours(q.Hours), Issues: yardLineIssues(q.Issues),
		Blocking: q.Blocking, LanguageVersion: q.Snapshot.LanguageVersion, SentAt: view.Time(r.SentAt), DecidedAt: view.Time(r.DecidedAt),
		DeclineReason: view.String(r.DeclineReason), Version: int64(r.Version), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
	if r.DepositBP != nil {
		bp := int64(*r.DepositBP)
		out.DepositBp = &bp
	}
	if r.DepositCents != nil {
		c := *r.DepositCents
		out.DepositCents = &c
	}
	if r.AcceptedVia != "" {
		v := api.YardQuoteAcceptedVia(r.AcceptedVia)
		out.AcceptedVia = &v
	}
	return out
}

func writeYardQuote(w http.ResponseWriter, q *yardquote.QuoteView, status int) {
	respond.SetETag(w, q.Row.Version)
	respond.JSON(w, status, apiYardQuote(q))
}

// ListYardQuotes implements api.ServerInterface.
func (h *Handler) ListYardQuotes(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, params api.ListYardQuotesParams) {
	f := yardquote.QuoteFilter{CustomerID: str(params.CustomerId), VesselID: str(params.VesselId), WorkOrderID: str(params.WorkOrderId)}
	if params.Status != nil {
		f.Status = string(*params.Status)
	}
	if params.Kind != nil {
		f.Kind = string(*params.Kind)
	}
	qs, next, err := h.YardQuotes.List(r.Context(), principal(r), marinaID, f, str(params.Cursor), intOr(params.Limit, 0))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	out := make([]api.YardQuote, 0, len(qs))
	for i := range qs {
		out = append(out, apiYardQuote(&qs[i]))
	}
	respond.JSON(w, http.StatusOK, api.YardQuotePage{Items: out, NextCursor: nextCursor(next)})
}

// CreateYardQuote implements api.ServerInterface.
func (h *Handler) CreateYardQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, _ api.CreateYardQuoteParams) {
	var body api.YardQuoteInput
	if !h.yardDecode(w, r, marinaID, &body, false) {
		return
	}
	in, ok := yardQuoteInput(body)
	if !ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	q, err := h.YardQuotes.Create(r.Context(), principal(r), marinaID, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardQuote(w, q, http.StatusCreated)
}

// PreviewYardQuote implements api.ServerInterface.
func (h *Handler) PreviewYardQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId) {
	var body api.YardQuoteInput
	if !h.yardDecode(w, r, marinaID, &body, false) {
		return
	}
	in, ok := yardQuoteInput(body)
	if !ok {
		h.badPricingBody(w, r, marinaID)
		return
	}
	p, err := h.YardQuotes.Preview(r.Context(), principal(r), marinaID, in)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, apiYardPreview(p))
}

// GetYardQuote implements api.ServerInterface.
func (h *Handler) GetYardQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, yardQuoteID api.YardQuoteId) {
	q, err := h.YardQuotes.Get(r.Context(), principal(r), marinaID, yardQuoteID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardQuote(w, q, http.StatusOK)
}

// UpdateYardQuote implements api.ServerInterface.
func (h *Handler) UpdateYardQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, yardQuoteID api.YardQuoteId, params api.UpdateYardQuoteParams) {
	var in api.YardQuotePatch
	fields, err := respond.DecodeWithFields(r, &in)
	if err != nil {
		h.badPricingBody(w, r, marinaID)
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	patch := yardquote.QuotePatch{Conditions: in.Conditions, Notes: in.Notes}
	if in.Selections != nil {
		s := yardSelections(*in.Selections)
		patch.Selections = &s
	}
	if in.JobInputs != nil {
		m, err := yardValuesOf(in.JobInputs)
		if err != nil {
			h.badPricingBody(w, r, marinaID)
			return
		}
		patch.JobInputs = &m
	}
	if in.VesselOverrides != nil {
		m, err := yardValuesOf(in.VesselOverrides)
		if err != nil {
			h.badPricingBody(w, r, marinaID)
			return
		}
		patch.VesselOverrides = &m
	}
	if in.Overrides != nil {
		o := yardOverrides(in.Overrides)
		patch.Overrides = &o
	}
	if in.ManualLines != nil {
		m := yardManualLines(in.ManualLines)
		patch.ManualLines = &m
	}
	if in.ValidUntil != nil {
		d, err := yardDate(in.ValidUntil)
		if err != nil || d == nil {
			h.badPricingBody(w, r, marinaID)
			return
		}
		patch.ValidUntil = d
	}
	if in.BillingMode != nil {
		b := string(*in.BillingMode)
		patch.BillingMode = &b
	}
	if _, present := fields["deposit_bp"]; present {
		patch.DepositBP = yardquote.OptInt64{Set: true, V: in.DepositBp}
	}
	if _, present := fields["deposit_cents"]; present {
		patch.DepositCents = yardquote.OptInt64{Set: true, V: in.DepositCents}
	}
	for _, d := range []struct {
		key string
		src *calendar.Date
		dst *yardquote.OptDate
	}{
		{"planned_start_on", dateOfAPI(in.PlannedStartOn), &patch.PlannedStartOn},
		{"planned_delivery_on", dateOfAPI(in.PlannedDeliveryOn), &patch.PlannedDeliveryOn},
	} {
		if _, present := fields[d.key]; present {
			*d.dst = yardquote.OptDate{Set: true, V: d.src}
		}
	}
	q, err := h.YardQuotes.Update(r.Context(), principal(r), marinaID, yardQuoteID, version, patch)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardQuote(w, q, http.StatusOK)
}

// RecomputeYardQuote implements api.ServerInterface.
func (h *Handler) RecomputeYardQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, yardQuoteID api.YardQuoteId, params api.RecomputeYardQuoteParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	q, err := h.YardQuotes.Recompute(r.Context(), principal(r), marinaID, yardQuoteID, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardQuote(w, q, http.StatusOK)
}

// SendYardQuote implements api.ServerInterface.
func (h *Handler) SendYardQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, yardQuoteID api.YardQuoteId, params api.SendYardQuoteParams) {
	var in api.YardQuoteSendInput
	if !h.yardDecode(w, r, marinaID, &in, true) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	locale := ""
	if in.Locale != nil {
		locale = string(*in.Locale)
	}
	res, err := h.YardQuotes.Send(r.Context(), principal(r), marinaID, yardQuoteID, version, in.Manual != nil && *in.Manual, locale)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, res.Quote.Row.Version)
	respond.JSON(w, http.StatusOK, api.YardQuoteSendResult{
		Quote: apiYardQuote(res.Quote), DocumentId: view.String(res.DocumentID), LinkUrl: view.String(res.LinkURL),
		MessageId: view.String(res.MessageID),
	})
}

// AcceptYardQuote implements api.ServerInterface.
func (h *Handler) AcceptYardQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, yardQuoteID api.YardQuoteId, params api.AcceptYardQuoteParams) {
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	res, err := h.YardQuotes.Accept(r.Context(), principal(r), marinaID, yardQuoteID, version)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	respond.SetETag(w, res.Quote.Row.Version)
	respond.JSON(w, http.StatusOK, api.YardQuoteAcceptResult{
		Quote: apiYardQuote(res.Quote), WorkOrderId: res.Order.WorkOrderID, WorkOrderNumber: view.String(res.Order.Number),
		AdvanceChargeId: view.String(res.Order.AdvanceChargeID),
	})
}

// DeclineYardQuote implements api.ServerInterface.
func (h *Handler) DeclineYardQuote(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, yardQuoteID api.YardQuoteId, params api.DeclineYardQuoteParams) {
	var in api.YardQuoteDeclineInput
	if !h.yardDecode(w, r, marinaID, &in, true) {
		return
	}
	version, ok := respond.IfMatch(w, r, params.IfMatch)
	if !ok {
		return
	}
	q, err := h.YardQuotes.Decline(r.Context(), principal(r), marinaID, yardQuoteID, version, str(in.Reason))
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	writeYardQuote(w, q, http.StatusOK)
}

// GetYardQuotePdf implements api.ServerInterface.
func (h *Handler) GetYardQuotePdf(w http.ResponseWriter, r *http.Request, marinaID api.MarinaId, yardQuoteID api.YardQuoteId) {
	b, err := h.YardQuotes.PDF(r.Context(), principal(r), marinaID, yardQuoteID)
	if err != nil {
		respond.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="preventivo.pdf"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b) //nolint:gosec // G705: a PDF generated by Porto, not HTML
}

// dateOfAPI converts a nullable date of a patch body (nil = null).
func dateOfAPI(d *openapi_types.Date) *calendar.Date {
	if d == nil {
		return nil
	}
	c := calendar.New(d.Year(), d.Month(), d.Day())
	return &c
}
