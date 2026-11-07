package view

import (
	"encoding/json"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/money"
)

// Ledger views (m5-plan §7.3.2): payments, refunds, credits, statements, receivables, dashboard, lens, worklist.
// Customer names travel only when the service filled them (customers.read).

// LedgerCustomer is the customer brief of a ledger row (nil for walk-in payments).
func LedgerCustomer(id, name string) *api.CustomerBrief {
	if id == "" {
		return nil
	}
	return &api.CustomerBrief{Id: id, DisplayName: String(name)}
}

func ledgerCustomerValue(id, name string) api.CustomerBrief {
	return api.CustomerBrief{Id: id, DisplayName: String(name)}
}

func isoDate(d calendar.Date) api.IsoDate { return d.String() }

func isoDatePtr(d calendar.Date) *api.IsoDate {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

// LedgerBuckets converts VAT buckets.
func LedgerBuckets(bs []payment.Bucket) []api.VatBucket {
	out := make([]api.VatBucket, 0, len(bs))
	for _, b := range bs {
		out = append(out, api.VatBucket{
			VatRateBp: b.RateBP, VatNature: String(b.Nature), NetCents: int64(b.Net), VatCents: int64(b.VAT), GrossCents: int64(b.Gross),
		})
	}
	return out
}

// PaymentRefund converts a refund or payout.
func PaymentRefund(r payment.Refund) api.Refund {
	return api.Refund{
		Id: r.ID, Number: r.Number, PaymentId: String(r.PaymentID), Kind: api.RefundKind(r.Kind), Method: api.PaymentMethod(r.Method),
		Source: api.RefundSource(r.Source), ProviderCode: String(r.ProviderCode), Status: api.RefundStatus(r.Status),
		AmountCents: r.AmountCents, Reason: r.Reason, BusinessDate: isoDate(r.BusinessDate), FailureCode: String(r.FailureCode),
		CreatedAt: r.CreatedAt.UTC(),
	}
}

// PaymentRefunds converts refunds.
func PaymentRefunds(rs []payment.Refund) []api.Refund {
	out := make([]api.Refund, 0, len(rs))
	for _, r := range rs {
		out = append(out, PaymentRefund(r))
	}
	return out
}

// PaymentFiscalDocument converts a fiscal document of a payment.
func PaymentFiscalDocument(d payment.FiscalDocument) api.FiscalDocument {
	var vat []payment.Bucket
	_ = json.Unmarshal(d.VAT, &vat)
	return api.FiscalDocument{
		Id: d.ID, PaymentId: d.PaymentID, ChargeId: d.ChargeID, Driver: d.Driver, Number: d.Number,
		BusinessDate: isoDate(d.BusinessDate), Status: api.FiscalDocumentStatus(d.Status), TotalCents: d.TotalCents,
		Vat: LedgerBuckets(vat), ExternalRef: String(d.ExternalRef), ErrorCode: String(d.ErrorCode), IssuedAt: Time(d.IssuedAt),
		VoidedAt: Time(d.VoidedAt), CreatedAt: d.CreatedAt.UTC(), Version: d.Version,
	}
}

// Payment converts a payment; matches, refunds and fiscal documents only when the detail carries them.
func Payment(d payment.Detail, full bool) api.Payment {
	p := d.Payment
	split := LedgerBuckets(d.Split)
	out := api.Payment{
		Id: p.ID, Number: p.Number, Customer: LedgerCustomer(p.CustomerID, d.CustomerName), Kind: api.PaymentKind(p.Kind),
		Method: api.PaymentMethod(p.Method), Source: api.PaymentSource(p.Source), ProviderCode: String(p.ProviderCode),
		AmountCents: p.AmountCents, AllocatedCents: p.AllocatedCents, RefundedCents: p.RefundedCents,
		UnallocatedCents: p.UnallocatedCents(), FeeCents: p.FeeCents, Currency: p.Currency, ReceivedOn: isoDate(p.ReceivedOn),
		CashSessionId: String(p.CashSessionID), HasReference: &d.HasReference, Notes: String(p.Notes), VatSplit: &split,
		CreatedAt: Time(p.CreatedAt), Version: p.Version,
	}
	if full {
		ms := make([]api.PaymentMatch, 0, len(d.Matches))
		for _, m := range d.Matches {
			rev := m.Reversed
			ms = append(ms, api.PaymentMatch{
				Id: m.ID, ChargeId: m.ChargeID, ChargeNumber: m.ChargeNumber, AmountCents: m.AmountCents,
				ReversesMatchId: String(m.ReversesMatchID), Reversed: &rev, CreatedAt: m.CreatedAt.UTC(),
			})
		}
		rs := PaymentRefunds(d.Refunds)
		fd := make([]api.FiscalDocument, 0, len(d.FiscalDocuments))
		for _, f := range d.FiscalDocuments {
			fd = append(fd, PaymentFiscalDocument(f))
		}
		out.Matches, out.Refunds, out.FiscalDocuments = &ms, &rs, &fd
	}
	return out
}

// Payments converts payment list rows.
func Payments(ds []payment.Detail) []api.Payment {
	out := make([]api.Payment, 0, len(ds))
	for _, d := range ds {
		out = append(out, Payment(d, false))
	}
	return out
}

// AgeingAmounts converts an ageing.
func AgeingAmounts(a payment.Ageing) api.AgeingAmounts {
	return api.AgeingAmounts{
		NotDueCents: int64(a.NotDue), D030Cents: int64(a.D0To30), D3160Cents: int64(a.D31To60), D6190Cents: int64(a.D61To90),
		D90PlusCents: int64(a.D90Plus), TotalCents: int64(a.Total),
	}
}

// Receivable converts a scadenzario row.
func Receivable(r payment.Receivable) api.Receivable {
	out := api.Receivable{
		Kind: api.ReceivableKind(r.Kind), Id: r.ID, Number: String(r.Number), Customer: LedgerCustomer(r.CustomerID, r.CustomerName),
		ContractId: String(r.ContractID), ContractCode: String(r.ContractCode), Description: String(r.Description),
		DueOn: isoDate(r.DueOn), AmountCents: r.AmountCents, ResidualCents: r.ResidualCents, Bucket: api.AgeingBucket(r.Bucket),
	}
	if r.OriginType != "" {
		o := api.ChargeOriginType(r.OriginType)
		out.OriginType = &o
	}
	if r.LastReminderLevel != "" {
		l := api.ReminderLevel(r.LastReminderLevel)
		out.LastReminderLevel = &l
	}
	if r.Excluded {
		ex := true
		out.Excluded = &ex
	}
	return out
}

// Receivables converts scadenzario rows.
func Receivables(rs []payment.Receivable) []api.Receivable {
	out := make([]api.Receivable, 0, len(rs))
	for _, r := range rs {
		out = append(out, Receivable(r))
	}
	return out
}

// AgeingReport converts the receivables overview.
func AgeingReport(rep *payment.AgeingReport) api.AgeingReport {
	out := api.AgeingReport{Rows: make([]api.AgeingRow, 0, len(rep.Rows)), Totals: AgeingAmounts(rep.Totals)}
	for _, r := range rep.Rows {
		out.Rows = append(out.Rows, api.AgeingRow{
			Customer: ledgerCustomerValue(r.CustomerID, r.CustomerName), Amounts: AgeingAmounts(r.Amounts), CreditCents: int64(r.Credit),
		})
	}
	return out
}

// Statement converts a statement of account.
func Statement(st *payment.Statement) api.Statement {
	out := api.Statement{
		Customer: ledgerCustomerValue(st.CustomerID, st.CustomerName), From: isoDate(st.From), To: isoDate(st.To),
		OpeningCents: int64(st.Opening), ClosingCents: int64(st.Closing), Rows: make([]api.StatementRow, 0, len(st.Rows)),
	}
	for _, r := range st.Rows {
		out.Rows = append(out.Rows, api.StatementRow{
			Date: isoDate(r.Date), Kind: api.StatementRowKind(r.Kind), Id: String(r.ID), Number: r.Number, Description: r.Description,
			DueOn: isoDatePtr(r.DueOn), DebitCents: int64(r.Debit), CreditCents: int64(r.Credit), BalanceCents: int64(r.Balance),
		})
	}
	return out
}

// CreditRows converts the customers with credit.
func CreditRows(rs []payment.CreditRow) []api.CreditRow {
	out := make([]api.CreditRow, 0, len(rs))
	for _, r := range rs {
		out = append(out, api.CreditRow{Customer: ledgerCustomerValue(r.CustomerID, r.CustomerName), CreditCents: int64(r.Credit), DepositsCents: int64(r.Deposits)})
	}
	return out
}

// CustomerCredit converts the credit of a customer.
func CustomerCredit(r *payment.CreditRow, pays []payment.Detail) api.CustomerCredit {
	return api.CustomerCredit{
		Customer: ledgerCustomerValue(r.CustomerID, r.CustomerName), CreditCents: int64(r.Credit), DepositsCents: int64(r.Deposits),
		Payments: Payments(pays),
	}
}

// MapPaymentStatus converts the payment lens of a map.
func MapPaymentStatus(night calendar.Date, rs []payment.LensResource) api.MapPaymentStatus {
	out := api.MapPaymentStatus{Date: isoDate(night), Resources: make([]api.MapPaymentResource, 0, len(rs))}
	for _, r := range rs {
		m := api.MapPaymentResource{
			ResourceType: api.ResourceType(r.ResourceType), ResourceId: r.ResourceID, ContractId: String(r.ContractID),
			State: api.LensState(r.State), Suspended: r.Suspended,
		}
		if r.Residual != nil {
			v := int64(*r.Residual)
			m.ResidualCents = &v
		}
		out.Resources = append(out.Resources, m)
	}
	return out
}

// WorkItem converts a worklist item.
func WorkItem(it payment.WorkItem) api.WorkItem {
	out := api.WorkItem{
		Kind: api.WorkItemKind(it.Kind), SourceType: it.SourceType, SourceId: it.SourceID, Status: api.WorkItemStatus(it.Status),
		AssigneeUserId: String(it.AssigneeUserID), Customer: LedgerCustomer(it.CustomerID, it.CustomerName), DueOn: isoDatePtr(it.DueOn),
		CreatedAt: it.CreatedAt.UTC(),
	}
	if it.Amount != 0 {
		v := int64(it.Amount)
		out.AmountCents = &v
	}
	return out
}

// WorkItems converts worklist items.
func WorkItems(its []payment.WorkItem) []api.WorkItem {
	out := make([]api.WorkItem, 0, len(its))
	for _, it := range its {
		out = append(out, WorkItem(it))
	}
	return out
}

// Dashboard converts the customer dashboard.
func Dashboard(d *payment.Dashboard) api.CustomerDashboard {
	out := api.CustomerDashboard{
		Customer: ledgerCustomerValue(d.CustomerID, d.CustomerName), Notes: d.Notes,
		Vessels: make([]api.VesselRef, 0, len(d.Vessels)), Contracts: make([]api.DashboardContract, 0, len(d.Contracts)),
		BerthsToday: make([]api.ResourceRef, 0, len(d.BerthsToday)), DocumentsExpiring: make([]api.DashboardDocument, 0, len(d.Documents)),
		Communications: make([]api.DashboardMessage, 0, len(d.Communications)),
		WorkOrders:     []map[string]any{}, Rentals: []map[string]any{},
	}
	for _, v := range d.Vessels {
		out.Vessels = append(out.Vessels, api.VesselRef{Id: v.ID, Name: v.Name})
	}
	for _, c := range d.Contracts {
		total := int64(c.Total)
		out.Contracts = append(out.Contracts, api.DashboardContract{
			Id: c.ID, Code: c.Code, Kind: c.Kind, Status: c.Status, StartsOn: isoDate(c.StartsOn), EndsOn: isoDate(c.EndsOn), TotalCents: &total,
		})
	}
	for _, r := range d.BerthsToday {
		out.BerthsToday = append(out.BerthsToday, api.ResourceRef{ResourceType: api.ResourceType(r.ResourceType), ResourceId: r.ResourceID})
	}
	for _, doc := range d.Documents {
		out.DocumentsExpiring = append(out.DocumentsExpiring, api.DashboardDocument{Id: doc.ID, Type: doc.Type, ExpiresOn: isoDate(doc.ExpiresOn)})
	}
	for _, m := range d.Communications {
		out.Communications = append(out.Communications, api.DashboardMessage{Id: m.ID, Template: m.Template, Status: m.Status, CreatedAt: m.CreatedAt.UTC()})
	}
	if d.Billing {
		arrears := AgeingAmounts(d.Arrears)
		balance, credit := int64(d.Balance), int64(d.Credit)
		future := Receivables(d.FutureInstallments)
		pays := Payments(d.RecentPayments)
		out.Arrears, out.BalanceCents, out.CreditCents, out.FutureInstallments, out.RecentPayments = &arrears, &balance, &credit, &future, &pays
	}
	return out
}

// SettleMandateResult converts a mandate settlement.
func SettleMandateResult(r payment.SettleMandateResult) api.SettleMandateResult {
	return api.SettleMandateResult{CreditedCents: int64(r.Credited), PaidOutCents: int64(r.PaidOut)}
}

// LedgerCents converts cents.
func LedgerCents(c money.Cents) api.Cents { return int64(c) }

// LedgerCharge converts a charge without its lines and references (the retention answer; the charge detail is
// GET charges/{chargeId}).
func LedgerCharge(c *payment.Charge, customerName string) api.Charge {
	return api.Charge{
		Id: c.ID, Number: c.Number, Customer: LedgerCustomer(c.CustomerID, customerName), OriginType: api.ChargeOriginType(c.OriginType),
		OriginId: c.OriginID, ContractId: String(c.ContractID), RightId: String(c.RightID), VesselId: String(c.VesselID),
		IssuedOn: isoDate(c.IssuedOn), DueOn: isoDate(c.DueOn), ServiceFrom: isoDatePtr(c.ServiceFrom), ServiceTo: isoDatePtr(c.ServiceTo),
		TotalCents: c.TotalCents, NetCents: c.NetCents, VatCents: c.VATCents, PaidAmountCents: c.PaidAmountCents,
		CreditedCents: c.CreditedCents, ResidualCents: c.ResidualCents(), PaymentStatus: api.ChargePaymentStatus(c.PaymentStatus),
		InvoicingStatus: api.ChargeInvoicingStatus(c.InvoicingStatus), FiscalMode: api.FiscalMode(c.FiscalMode),
		Description: c.Description, CreatedAt: Time(c.CreatedAt), Version: c.Version,
	}
}
