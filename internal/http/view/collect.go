package view

import (
	"encoding/json"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/cash"
	"github.com/coste360/porto/internal/domain/cash/summary"
	"github.com/coste360/porto/internal/domain/collect"
	"github.com/coste360/porto/internal/domain/fiscal"
	"github.com/coste360/porto/internal/domain/payment"
	"github.com/coste360/porto/internal/http/api"
)

// Cash, fiscal and collection views (m5-plan §7.3.3). Customer names travel only when the handler resolved them
// with customers.read; provider secrets never travel.

// CashRegister converts a register.
func CashRegister(r cash.Register) api.CashRegister {
	return api.CashRegister{
		Id: r.ID, Code: r.Code, Name: r.Name, Active: r.Active, IsDefault: r.IsDefault, OpenSessionId: String(r.OpenSessionID),
		Version: r.Version,
	}
}

// CashRegisters converts registers.
func CashRegisters(rs []cash.Register) api.CashRegisterList {
	out := api.CashRegisterList{Items: make([]api.CashRegister, 0, len(rs))}
	for _, r := range rs {
		out.Items = append(out.Items, CashRegister(r))
	}
	return out
}

func methodAmounts(list []summary.MethodAmount) []api.MethodAmount {
	out := make([]api.MethodAmount, 0, len(list))
	for _, m := range list {
		out = append(out, api.MethodAmount{Method: api.PaymentMethod(m.Method), AmountCents: int64(m.Amount)})
	}
	return out
}

// CashSummary converts a session summary.
func CashSummary(s summary.Result) api.CashSummary {
	return api.CashSummary{
		Expected: methodAmounts(s.Expected), Counted: methodAmounts(s.Counted), DifferenceCents: int64(s.Difference),
		PaymentsCents: int64(s.Payments), RefundsCents: int64(s.Refunds), MovementsInCents: int64(s.MovementsIn),
		MovementsOutCents: int64(s.MovementsOut),
	}
}

// CashMovement converts a movement.
func CashMovement(m cash.Movement) api.CashMovement {
	return api.CashMovement{
		Id: m.ID, Kind: api.CashMovementKind(m.Kind), AmountCents: m.AmountCents, Reason: m.Reason,
		CreatedByUserId: String(m.CreatedByUserID), CreatedAt: m.CreatedAt.UTC(),
	}
}

// CashSession converts a session; movements only when withMovements.
func CashSession(s *cash.Session, withMovements bool) api.CashSession {
	out := api.CashSession{
		Id: s.ID, RegisterId: s.RegisterID, RegisterCode: String(s.RegisterCode), Status: api.CashSessionStatus(s.Status),
		BusinessDate: s.BusinessDate.String(), FloatCents: s.FloatCents, OpenedAt: s.OpenedAt.UTC(),
		OpenedByUserId: String(s.OpenedByUserID), ClosedAt: Time(s.ClosedAt), ClosedByUserId: String(s.ClosedByUserID),
		Notes: String(s.Notes), Summary: CashSummary(s.Summary), Version: s.Version,
	}
	if withMovements {
		ms := make([]api.CashMovement, 0, len(s.Movements))
		for _, m := range s.Movements {
			ms = append(ms, CashMovement(m))
		}
		out.Movements = &ms
	}
	return out
}

// CashSessionPage converts a page of sessions.
func CashSessionPage(p cash.SessionPage) api.CashSessionPage {
	out := api.CashSessionPage{Items: make([]api.CashSession, 0, len(p.Items)), NextCursor: String(p.NextCursor)}
	for i := range p.Items {
		out.Items = append(out.Items, CashSession(&p.Items[i], false))
	}
	return out
}

// FiscalDocuments converts a page of fiscal documents.
func FiscalDocuments(p fiscal.DocumentPage) api.FiscalDocumentPage {
	out := api.FiscalDocumentPage{Items: make([]api.FiscalDocument, 0, len(p.Items)), NextCursor: String(p.NextCursor)}
	for _, d := range p.Items {
		out.Items = append(out.Items, PaymentFiscalDocument(d))
	}
	return out
}

// FiscalTotals converts the corrispettivi.
func FiscalTotals(t *fiscal.Totals) api.FiscalDailyTotals {
	out := api.FiscalDailyTotals{
		From: t.From.String(), To: t.To.String(), Group: api.FiscalDailyTotalsGroup(t.Group), TotalCents: int64(t.Total),
		Rows: make([]api.FiscalTotalRow, 0, len(t.Rows)),
	}
	for _, r := range t.Rows {
		row := api.FiscalTotalRow{
			Period: r.Period, VatRateBp: r.RateBP, Method: api.PaymentMethod(r.Method), Documents: r.Documents,
			GrossCents: int64(r.Gross), NetCents: int64(r.Net), VatCents: int64(r.VAT),
		}
		if r.Nature != "" {
			n := api.VatNature(r.Nature)
			row.VatNature = &n
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

// CounterSale converts the outcome of a counter sale.
func CounterSale(res *cash.CounterSaleResult, customerName, receiptURL string) api.CounterSaleResult {
	var split []payment.Bucket
	_ = json.Unmarshal(res.Payment.VATSplit, &split)
	out := api.CounterSaleResult{
		Charge:     LedgerCharge(res.Charge, customerName),
		Payment:    Payment(payment.Detail{Payment: *res.Payment, CustomerName: customerName, Split: split}, false),
		ReceiptUrl: receiptURL,
	}
	if res.FiscalDocument != nil {
		fd := PaymentFiscalDocument(*res.FiscalDocument)
		out.FiscalDocument = &fd
	}
	return out
}

// PaymentRequest converts a request.
func PaymentRequest(v collect.RequestView, names map[string]string) api.PaymentRequest {
	r := v.Request
	out := api.PaymentRequest{
		Id: r.ID, Customer: ledgerCustomerValue(r.CustomerID, names[r.CustomerID]), AmountCents: r.AmountCents,
		Currency: r.Currency, Status: api.PaymentRequestStatus(r.Status), Source: api.PaymentRequestSource(r.Source),
		ExpiresAt: Time(r.ExpiresAt), SentAt: Time(r.SentAt), SentCount: r.SentCount, CreatedAt: r.CreatedAt.UTC(),
		Version: r.Version, Charges: make([]api.PaymentRequestCharge, 0, len(v.Charges)),
	}
	for _, c := range v.Charges {
		out.Charges = append(out.Charges, api.PaymentRequestCharge{
			ChargeId: c.Charge.ID, Number: c.Charge.Number, Description: String(c.Charge.Description),
			DueOn: c.Charge.DueOn.String(), AmountCents: c.Amount, ResidualCents: c.Residual,
		})
	}
	return out
}

// PaymentRequestCreated converts a created or sent request with its link.
func PaymentRequestCreated(v *collect.RequestView, names map[string]string) api.PaymentRequestCreated {
	return api.PaymentRequestCreated{Request: PaymentRequest(*v, names), Url: String(v.URL)}
}

// PaymentRequestPage converts a page of requests.
func PaymentRequestPage(p collect.RequestPage, names map[string]string) api.PaymentRequestPage {
	out := api.PaymentRequestPage{Items: make([]api.PaymentRequest, 0, len(p.Items)), NextCursor: String(p.NextCursor)}
	for _, v := range p.Items {
		out.Items = append(out.Items, PaymentRequest(v, names))
	}
	return out
}

// ReminderRun converts a run.
func ReminderRun(r *collect.Run) api.ReminderRun {
	counts := map[string]int{}
	_ = json.Unmarshal(r.Counts, &counts)
	return api.ReminderRun{
		Id: r.ID, RunOn: r.RunOn.String(), Status: api.ReminderRunStatus(r.Status), Counts: counts, StartedAt: r.StartedAt.UTC(),
		FinishedAt: Time(r.FinishedAt),
	}
}

// ReminderRunPage converts a page of runs.
func ReminderRunPage(p collect.RunPage) api.ReminderRunPage {
	out := api.ReminderRunPage{Items: make([]api.ReminderRun, 0, len(p.Items)), NextCursor: String(p.NextCursor)}
	for i := range p.Items {
		out.Items = append(out.Items, ReminderRun(&p.Items[i]))
	}
	return out
}

// ReminderItem converts an item.
func ReminderItem(v collect.ItemView, names map[string]string) api.ReminderItem {
	it := v.Item
	out := api.ReminderItem{
		Id: it.ID, RunId: String(it.RunID), ChargeId: it.ChargeID, Customer: ledgerCustomerValue(it.CustomerID, names[it.CustomerID]),
		Level: api.ReminderLevel(it.Level), Status: api.ReminderItemStatus(it.Status), RequestId: String(it.RequestID),
		CreatedAt: it.CreatedAt.UTC(), Version: it.Version,
	}
	if v.Charge != nil {
		res := v.Charge.ResidualCents()
		out.ChargeNumber, out.ResidualCents = String(v.Charge.Number), &res
	}
	return out
}

// ReminderRunDetail converts a run with a page of items.
func ReminderRunDetail(r *collect.Run, items collect.ItemPage, names map[string]string) api.ReminderRunDetail {
	out := api.ReminderRunDetail{Run: ReminderRun(r), Items: make([]api.ReminderItem, 0, len(items.Items)), NextCursor: String(items.NextCursor)}
	for _, v := range items.Items {
		out.Items = append(out.Items, ReminderItem(v, names))
	}
	return out
}

// DunningExclusion converts an exclusion.
func DunningExclusion(e *collect.Exclusion, today calendar.Date, names map[string]string) api.DunningExclusion {
	out := api.DunningExclusion{
		Id: e.ID, ChargeId: String(e.ChargeID), Kind: api.DunningExclusionKind(e.Kind), Note: e.Note,
		Until: isoDatePtr(e.UntilOn), Active: collect.ExclusionActive(e, today), CreatedAt: e.CreatedAt.UTC(), RemovedAt: Time(e.RemovedAt),
	}
	if e.CustomerID != "" {
		out.Customer = LedgerCustomer(e.CustomerID, names[e.CustomerID])
	}
	return out
}

// DunningExclusionPage converts a page of exclusions.
func DunningExclusionPage(p collect.ExclusionPage, today calendar.Date, names map[string]string) api.DunningExclusionPage {
	out := api.DunningExclusionPage{Items: make([]api.DunningExclusion, 0, len(p.Items)), NextCursor: String(p.NextCursor)}
	for i := range p.Items {
		out.Items = append(out.Items, DunningExclusion(&p.Items[i], today, names))
	}
	return out
}

// PaymentProviders converts the driver list.
func PaymentProviders(list []collect.ProviderInfo) api.PaymentProviderList {
	out := api.PaymentProviderList{Items: make([]api.PaymentProviderInfo, 0, len(list))}
	for _, p := range list {
		info := api.PaymentProviderInfo{
			Code: p.Code, Enabled: p.Enabled,
			Capabilities: api.PaymentProviderCapabilities{
				Checkout: p.Capabilities.Checkout, MandateSetup: p.Capabilities.MandateSetup, MandateCharge: p.Capabilities.MandateCharge,
				Refund: p.Capabilities.Refund, StatusCheck: p.Capabilities.StatusCheck, Methods: nonNil(p.Capabilities.Methods),
			},
		}
		if p.Mode != "" {
			m := api.PaymentProviderInfoMode(p.Mode)
			info.Mode = &m
		}
		out.Items = append(out.Items, info)
	}
	return out
}

// ProviderSettings converts provider settings (secrets reduced to presence flags and the last 4 characters of the
// publishable key).
func ProviderSettings(s *collect.ProviderSettings) api.PaymentProviderSettings {
	return api.PaymentProviderSettings{
		ProviderCode: s.Row.ProviderCode, Enabled: s.Row.Enabled, Mode: api.PaymentProviderSettingsMode(s.Row.Mode),
		HasSecretKey: s.HasSecretKey, HasWebhookSecret: s.HasWebhookSecret, PublishableKeyLast4: String(s.Row.PublishableKeyLast4),
		SepaEnabled: s.Row.SEPAEnabled, Methods: nonNil(s.Methods), StatementDescriptor: s.Row.StatementDescriptor,
		Version: s.Row.Version, WebhookUrl: s.WebhookURL,
	}
}

// SepaMandate converts a mandate (references stay encrypted; only the IBAN's last 4 digits travel).
func SepaMandate(m *collect.Mandate) api.SepaMandate {
	return api.SepaMandate{
		Id: m.ID, ProviderCode: m.ProviderCode, Status: api.SepaMandateStatus(m.Status), IbanLast4: m.IBANLast4,
		MandateRef: String(m.MandateRef), ActivatedAt: Time(m.ActivatedAt), RevokedAt: Time(m.RevokedAt), CreatedAt: m.CreatedAt.UTC(),
	}
}

// SepaMandates converts mandates.
func SepaMandates(ms []collect.Mandate) api.SepaMandateList {
	out := api.SepaMandateList{Items: make([]api.SepaMandate, 0, len(ms))}
	for i := range ms {
		out.Items = append(out.Items, SepaMandate(&ms[i]))
	}
	return out
}

// PayPage converts the public payment page.
func PayPage(p *collect.PayPage) api.PayPage {
	out := api.PayPage{
		AmountCents: p.Amount, Currency: p.Currency, Online: p.Online, Status: api.PaymentRequestStatus(p.Status),
		ExpiresAt: Time(p.ExpiresAt), Charges: make([]api.PayCharge, 0, len(p.Charges)), Methods: make([]api.PayPageMethods, 0, len(p.Methods)),
	}
	out.Marina.Name, out.Marina.DefaultLocale, out.Marina.Locales = p.MarinaName, api.Locale(p.DefaultLocale), Locales(p.Locales)
	for _, c := range p.Charges {
		out.Charges = append(out.Charges, api.PayCharge{Number: c.Number, Description: c.Description, DueOn: c.DueOn.String(), ResidualCents: c.Residual})
	}
	for _, m := range p.Methods {
		out.Methods = append(out.Methods, api.PayPageMethods(m))
	}
	if p.Bank != nil {
		out.Bank = &api.PayBank{Iban: p.Bank.IBAN, Holder: p.Bank.Holder, Bank: String(p.Bank.Bank), Bic: String(p.Bank.BIC), Reference: p.Bank.Reference}
	}
	return out
}
