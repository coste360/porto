package calc

import (
	"slices"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/money"
)

// ---- R9 payments (base incasso) ----

// Payment is a payments row received in the period (any kind; credits are never cash, M5 D5).
type Payment struct {
	ID, Number   string
	Kind         string // payment, deposit, credit
	Method       string
	Source       string
	ProviderCode string
	CustomerID   string
	Amount       money.Cents
	Fee          money.Cents
	ReceivedOn   calendar.Date
}

// Refund is a refunds row (refund or payout) with its business date.
type Refund struct {
	ID, Number   string
	Kind         string // refund, payout
	Status       string
	Method       string
	Source       string
	ProviderCode string
	CustomerID   string
	Amount       money.Cents
	BusinessDate calendar.Date
}

// PaymentsInput is the input of BuildPayments.
type PaymentsInput struct {
	From, To   calendar.Date
	GroupBy    string
	Section    string // main (default), payouts
	Method     string
	CustomerID string
	Payments   []Payment
	Refunds    []Refund
	Names      map[string]string
}

// BuildPayments builds R9 (§3.9): payments and deposits received (+), succeeded refunds (−); payouts apart.
func BuildPayments(in PaymentsInput) (PaymentsResult, Facts) {
	collected := Daily{}
	keep := func(method, customer string) bool {
		return (in.Method == "" || method == in.Method) && (in.CustomerID == "" || customer == in.CustomerID)
	}
	var main, payouts []PaymentsRecord
	for _, p := range in.Payments {
		if (p.Kind != "payment" && p.Kind != "deposit") || !inPeriod(p.ReceivedOn, in.From, in.To) || !keep(p.Method, p.CustomerID) {
			continue
		}
		main = append(main, PaymentsRecord{
			Section: "main", Kind: p.Kind, ID: p.ID, Number: p.Number, Date: p.ReceivedOn,
			Method: p.Method, Source: p.Source, ProviderCode: p.ProviderCode, CustomerID: p.CustomerID,
			CustomerName: in.Names[p.CustomerID], Amount: p.Amount, Fee: p.Fee, Ref: Ref{Type: "payment", ID: p.ID, Label: p.Number},
		})
		collected.Add(p.ReceivedOn, int64(p.Amount), 0)
	}
	for _, r := range in.Refunds {
		if r.Status != "succeeded" || !inPeriod(r.BusinessDate, in.From, in.To) || !keep(r.Method, r.CustomerID) {
			continue
		}
		rec := PaymentsRecord{
			Kind: r.Kind, ID: r.ID, Number: r.Number, Date: r.BusinessDate, Method: r.Method, Source: r.Source,
			ProviderCode: r.ProviderCode, CustomerID: r.CustomerID, CustomerName: in.Names[r.CustomerID],
			Ref: Ref{Type: "refund", ID: r.ID, Label: r.Number},
		}
		if r.Kind == "payout" {
			rec.Section, rec.Amount = "payouts", r.Amount
			payouts = append(payouts, rec)
			continue
		}
		rec.Section, rec.Amount = "main", -r.Amount
		main = append(main, rec)
		collected.Add(r.BusinessDate, -int64(r.Amount), 0)
	}
	for _, s := range [][]PaymentsRecord{main, payouts} {
		slices.SortStableFunc(s, func(a, b PaymentsRecord) int {
			if c := a.Date.Compare(b.Date); c != 0 {
				return c
			}
			return compareKeys([]string{a.Number, a.ID}, []string{b.Number, b.ID})
		})
	}
	all := append(slices.Clone(main), payouts...)
	res := PaymentsResult{Totals: FoldPayments(all)}
	if in.Section == "payouts" {
		res.Records = payouts
	} else {
		res.Records = main
	}
	key := map[string]func(PaymentsRecord) string{
		"method":   func(r PaymentsRecord) string { return r.Method },
		"provider": func(r PaymentsRecord) string { return orNone(r.ProviderCode) },
		"source":   func(r PaymentsRecord) string { return r.Source },
		"month":    func(r PaymentsRecord) string { return MonthKey(r.Date) },
		"customer": func(r PaymentsRecord) string { return orNone(r.CustomerID) },
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(main, func(r PaymentsRecord) []string { return []string{key(r)} }, FoldPayments)
	}
	return res, Facts{IndCollected: collected}
}

// FoldPayments is the only producer of payments totals: collected is Σ of the main section.
func FoldPayments(recs []PaymentsRecord) PaymentsTotals {
	t := PaymentsTotals{}
	for _, r := range recs {
		if r.Section == "payouts" {
			t.Payouts += r.Amount
			continue
		}
		t.Count++
		t.Collected += r.Amount
		t.Fees += r.Fee
		if r.Amount >= 0 {
			t.Payments += r.Amount
		} else {
			t.Refunds += r.Amount
		}
	}
	return t
}

// ---- R12 receivables ----

// Ageing buckets (payment.BucketOf).
const (
	BucketNotDue  = "not_due"
	BucketD0_30   = "d0_30"    //nolint:revive // API value
	BucketD31_60  = "d31_60"   //nolint:revive // API value
	BucketD61_90  = "d61_90"   //nolint:revive // API value
	BucketD90Plus = "d90_plus" //nolint:revive // API value
)

// OpenCharge is a positive charge with a residual as of the ageing date; Bucket comes from payment.BucketOf.
type OpenCharge struct {
	ID, Number string
	CustomerID string
	OriginType string
	IssuedOn   calendar.Date
	DueOn      calendar.Date
	Total      money.Cents
	Paid       money.Cents
	Credited   money.Cents
	Bucket     string
}

// ScheduledInstallment is a scheduled contract or holder instalment (future section).
type ScheduledInstallment struct {
	Kind       string // installment, holder_installment
	ID         string
	Number     string // contract code or source label
	CustomerID string
	DueOn      calendar.Date
	Amount     money.Cents
}

// Reminder is a sent reminder.
type Reminder struct {
	ID, ChargeID, ChargeNumber string
	CustomerID                 string
	Level                      string
	SentOn                     calendar.Date
}

// ReceivablesInput is the input of BuildReceivables.
type ReceivablesInput struct {
	AsOf        calendar.Date
	GroupBy     string
	Section     string // open (default), future, reminders
	OverdueOnly bool
	CustomerID  string
	Origin      string
	Open        []OpenCharge
	Future      []ScheduledInstallment
	Reminders   []Reminder
	Names       map[string]string
}

// BuildReceivables builds R12 (§3.12): current residuals by age as of AsOf, scheduled instalments and reminders.
func BuildReceivables(in ReceivablesInput) ReceivablesResult {
	var open, future, rem []ReceivablesRecord
	for _, c := range in.Open {
		residual := c.Total - c.Paid - c.Credited
		overdue := c.DueOn.Before(in.AsOf)
		if residual <= 0 || c.IssuedOn.After(in.AsOf) || (in.OverdueOnly && !overdue) ||
			(in.CustomerID != "" && c.CustomerID != in.CustomerID) || (in.Origin != "" && c.OriginType != in.Origin) {
			continue
		}
		open = append(open, ReceivablesRecord{
			Section: "open", Kind: "charge", ID: c.ID, Number: c.Number, CustomerID: c.CustomerID,
			CustomerName: in.Names[c.CustomerID], OriginType: c.OriginType, IssuedOn: c.IssuedOn, DueOn: c.DueOn, Total: c.Total,
			Paid: c.Paid, Credited: c.Credited, Residual: residual, Overdue: overdue, Bucket: c.Bucket,
			Ref: Ref{Type: "charge", ID: c.ID, Label: c.Number},
		})
	}
	for _, f := range in.Future {
		if in.CustomerID != "" && f.CustomerID != in.CustomerID {
			continue
		}
		future = append(future, ReceivablesRecord{
			Section: "future", Kind: f.Kind, ID: f.ID, Number: f.Number, CustomerID: f.CustomerID,
			CustomerName: in.Names[f.CustomerID], DueOn: f.DueOn, Total: f.Amount, Residual: f.Amount,
			Ref: Ref{Type: f.Kind, ID: f.ID, Label: f.Number},
		})
	}
	for _, r := range in.Reminders {
		if in.CustomerID != "" && r.CustomerID != in.CustomerID {
			continue
		}
		rem = append(rem, ReceivablesRecord{
			Section: "reminders", Kind: "reminder", ID: r.ID, Number: r.ChargeNumber, CustomerID: r.CustomerID,
			CustomerName: in.Names[r.CustomerID], ReminderLevel: r.Level, SentOn: r.SentOn, Ref: Ref{Type: "charge", ID: r.ChargeID, Label: r.ChargeNumber},
		})
	}
	byDue := func(a, b ReceivablesRecord) int {
		if c := a.DueOn.Compare(b.DueOn); c != 0 {
			return c
		}
		return compareKeys([]string{a.Number, a.ID}, []string{b.Number, b.ID})
	}
	slices.SortStableFunc(open, byDue)
	slices.SortStableFunc(future, byDue)
	slices.SortStableFunc(rem, func(a, b ReceivablesRecord) int {
		if c := a.SentOn.Compare(b.SentOn); c != 0 {
			return c
		}
		return compareKeys([]string{a.Number, a.ReminderLevel, a.ID}, []string{b.Number, b.ReminderLevel, b.ID})
	})
	all := append(append(slices.Clone(open), future...), rem...)
	fold := func(rs []ReceivablesRecord) ReceivablesTotals { return FoldReceivables(in.AsOf, rs) }
	res := ReceivablesResult{Totals: fold(all)}
	switch in.Section {
	case "future":
		res.Records = future
	case "reminders":
		res.Records = rem
	default:
		res.Records = open
	}
	key := map[string]func(ReceivablesRecord) string{
		"bucket":   func(r ReceivablesRecord) string { return r.Bucket },
		"customer": func(r ReceivablesRecord) string { return orNone(r.CustomerID) },
		"origin":   func(r ReceivablesRecord) string { return r.OriginType },
	}[in.GroupBy]
	if key != nil {
		res.Groups = GroupRecords(open, func(r ReceivablesRecord) []string { return []string{key(r)} }, fold)
	}
	return res
}

// FoldReceivables is the only producer of receivables totals.
func FoldReceivables(asOf calendar.Date, recs []ReceivablesRecord) ReceivablesTotals {
	t := ReceivablesTotals{AsOf: asOf}
	for _, r := range recs {
		switch r.Section {
		case "future":
			t.Future += r.Residual
		case "reminders":
			t.RemindersSent++
		default:
			t.Count++
			t.Open += r.Residual
			if r.Overdue {
				t.Overdue += r.Residual
			}
			switch r.Bucket {
			case BucketNotDue:
				t.Buckets.NotDue += r.Residual
			case BucketD0_30:
				t.Buckets.D0_30 += r.Residual
			case BucketD31_60:
				t.Buckets.D31_60 += r.Residual
			case BucketD61_90:
				t.Buckets.D61_90 += r.Residual
			case BucketD90Plus:
				t.Buckets.D90Plus += r.Residual
			}
			t.Buckets.Total += r.Residual
		}
	}
	return t
}

// ArrearsOf sums, per customer, the residual of the open charges overdue as of asOf (due before it).
func ArrearsOf(open []OpenCharge, asOf calendar.Date) map[string]money.Cents {
	out := map[string]money.Cents{}
	for _, c := range open {
		if c.CustomerID == "" || !c.DueOn.Before(asOf) || c.IssuedOn.After(asOf) {
			continue
		}
		if r := c.Total - c.Paid - c.Credited; r > 0 {
			out[c.CustomerID] += r
		}
	}
	return out
}
