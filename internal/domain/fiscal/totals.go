package fiscal

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/xlsx"
)

// Corrispettivi (m5-plan §5.2): from the issued sale documents and the succeeded refunds of their payments, per
// period (day or month) × VAT rate/nature × method: documents, gross, net and VAT. Each row's net/VAT comes from
// the documents' frozen buckets (refunds spread over the buckets of their payment's document by gross); the grand
// total is Σ receipt-mode payments documented − Σ their refunds of the range.

// Groupings of the corrispettivi.
const (
	GroupDay   = "day"
	GroupMonth = "month"
)

// MaxRangeDays bounds a corrispettivi query.
const MaxRangeDays = 400

// TotalsDoc is an issued sale document as aggregated (pure input).
type TotalsDoc struct {
	PaymentID string
	Date      calendar.Date
	Method    string
	Buckets   []Bucket
	// SplitOnly marks a document outside the range: its buckets only split the refunds of its payment.
	SplitOnly bool
}

// TotalsRefund is a succeeded refund of a documented payment (pure input).
type TotalsRefund struct {
	PaymentID string
	Date      calendar.Date
	Method    string
	Amount    money.Cents
}

// TotalRow is one corrispettivi row.
type TotalRow struct {
	Period    string
	RateBP    int64
	Nature    string
	Method    string
	Documents int
	Gross     money.Cents
	Net       money.Cents
	VAT       money.Cents
}

// Totals are the corrispettivi of a range.
type Totals struct {
	From, To calendar.Date
	Group    string
	Rows     []TotalRow
	Total    money.Cents
}

func period(d calendar.Date, group string) string {
	if group == GroupMonth {
		return d.String()[:7]
	}
	return d.String()
}

// Aggregate builds the rows (pure). Refunds of a payment without document buckets go to rate 0 with no nature.
// Rows are sorted by period, rate descending, nature, method.
func Aggregate(docs []TotalsDoc, refunds []TotalsRefund, group string) ([]TotalRow, money.Cents) {
	type key struct {
		period, nature, method string
		rate                   int64
	}
	idx := map[key]int{}
	var rows []TotalRow
	add := func(k key, b Bucket, doc bool) {
		i, ok := idx[k]
		if !ok {
			idx[k] = len(rows)
			rows = append(rows, TotalRow{Period: k.period, RateBP: k.rate, Nature: k.nature, Method: k.method})
			i = len(rows) - 1
		}
		rows[i].Gross += b.Gross
		rows[i].Net += b.Net
		rows[i].VAT += b.VAT
		if doc {
			rows[i].Documents++
		}
	}
	bucketsOf := map[string][]Bucket{}
	var total money.Cents
	for _, d := range docs {
		if _, ok := bucketsOf[d.PaymentID]; !ok {
			bucketsOf[d.PaymentID] = d.Buckets
		}
		if d.SplitOnly {
			continue
		}
		for _, b := range d.Buckets {
			add(key{period(d.Date, group), b.Nature, d.Method, b.RateBP}, b, true)
			total += b.Gross
		}
	}
	for _, r := range refunds {
		for _, b := range spread(bucketsOf[r.PaymentID], r.Amount) {
			add(key{period(r.Date, group), b.Nature, r.Method, b.RateBP}, Bucket{RateBP: b.RateBP, Nature: b.Nature, Gross: -b.Gross, Net: -b.Net, VAT: -b.VAT}, false)
		}
		total -= r.Amount
	}
	slices.SortFunc(rows, func(a, b TotalRow) int {
		return cmp.Or(cmp.Compare(a.Period, b.Period), cmp.Compare(b.RateBP, a.RateBP), cmp.Compare(a.Nature, b.Nature), cmp.Compare(a.Method, b.Method))
	})
	return rows, total
}

// spread splits amount over the buckets by gross (largest remainder), net in proportion.
func spread(buckets []Bucket, amount money.Cents) []Bucket {
	if amount == 0 {
		return nil
	}
	weights := make([]int64, len(buckets))
	positive := false
	for i, b := range buckets {
		if b.Gross > 0 {
			weights[i], positive = int64(b.Gross), true
		}
	}
	if !positive {
		return []Bucket{{Gross: amount, Net: amount}}
	}
	shares, err := money.Allocate(amount, weights)
	if err != nil {
		return []Bucket{{Gross: amount, Net: amount}}
	}
	out := make([]Bucket, 0, len(buckets))
	for i, b := range buckets {
		if shares[i] == 0 {
			continue
		}
		net := b.Net
		if shares[i] != b.Gross {
			net = b.Net.MulRat(int64(shares[i]), int64(b.Gross))
		}
		out = append(out, Bucket{RateBP: b.RateBP, Nature: b.Nature, Gross: shares[i], Net: net, VAT: shares[i] - net})
	}
	return out
}

func sortBuckets(b []Bucket) {
	slices.SortFunc(b, func(x, y Bucket) int {
		return cmp.Or(cmp.Compare(y.RateBP, x.RateBP), cmp.Compare(x.Nature, y.Nature))
	})
}

// DailyTotals returns the corrispettivi of [from, to] (charges.export).
func (s *Service) DailyTotals(ctx context.Context, p *auth.Principal, marinaID string, from, to calendar.Date, group string) (*Totals, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, err
	}
	if group == "" {
		group = GroupDay
	}
	if from.IsZero() || to.IsZero() || to.Before(from) || from.DaysUntil(to) > MaxRangeDays || (group != GroupDay && group != GroupMonth) {
		return nil, ErrInvalidRange
	}
	return s.totals(ctx, scope, from, to, group)
}

func (s *Service) totals(ctx context.Context, scope store.MarinaScope, from, to calendar.Date, group string) (*Totals, error) {
	db := s.d.DB.DB
	rawDocs, err := store.FiscalDocumentsForTotals(ctx, db, scope, from, to)
	if err != nil {
		return nil, err
	}
	rawRefunds, err := store.FiscalRefundsForTotals(ctx, db, scope, from, to)
	if err != nil {
		return nil, err
	}
	docs := make([]TotalsDoc, 0, len(rawDocs))
	for _, d := range rawDocs {
		var b []Bucket
		_ = json.Unmarshal(d.VAT, &b)
		docs = append(docs, TotalsDoc{PaymentID: d.PaymentID, Date: d.BusinessDate, Method: d.Method, Buckets: b})
	}
	// Refunds may concern payments documented before the range: load their buckets.
	var missing []string
	have := map[string]bool{}
	for _, d := range docs {
		have[d.PaymentID] = true
	}
	for _, r := range rawRefunds {
		if !have[r.PaymentID] {
			missing = append(missing, r.PaymentID)
		}
	}
	extra, err := store.FiscalVATOfPayments(ctx, db, scope, missing)
	if err != nil {
		return nil, err
	}
	refunds := make([]TotalsRefund, 0, len(rawRefunds))
	for _, r := range rawRefunds {
		refunds = append(refunds, TotalsRefund{PaymentID: r.PaymentID, Date: r.BusinessDate, Method: r.Method, Amount: money.Cents(r.AmountCents)})
		if raw, ok := extra[r.PaymentID]; ok {
			var b []Bucket
			_ = json.Unmarshal(raw, &b)
			docs = append(docs, TotalsDoc{PaymentID: r.PaymentID, Buckets: b, SplitOnly: true})
			delete(extra, r.PaymentID)
		}
	}
	rows, total := Aggregate(docs, refunds, group)
	return &Totals{From: from, To: to, Group: group, Rows: rows, Total: total}, nil
}

// TotalsHeader is the header of the corrispettivi files (Italian, as the export of charges).
var TotalsHeader = []string{"Periodo", "Aliquota IVA", "Natura", "Metodo", "Documenti", "Imponibile", "IVA", "Totale"}

// rate formats basis points as an Italian percentage ("22", "4,5").
func rate(bp int64) string {
	s := strconv.FormatInt(bp/100, 10)
	if r := bp % 100; r != 0 {
		frac := strings.TrimRight(strconv.FormatInt(100+r, 10)[1:], "0")
		s += "," + frac
	}
	return s
}

func italian(c money.Cents) string { return strings.Replace(c.String(), ".", ",", 1) }

// bom is the UTF-8 byte order mark of the CSV files.
const bom = "\xef\xbb\xbf"

// TotalsCSV renders the corrispettivi as an Italian CSV (UTF-8 BOM, ";", CRLF, decimal comma).
func TotalsCSV(t *Totals) []byte {
	var b bytes.Buffer
	b.WriteString(bom)
	b.WriteString(strings.Join(TotalsHeader, ";") + "\r\n")
	for _, r := range t.Rows {
		b.WriteString(strings.Join([]string{
			r.Period, rate(r.RateBP), r.Nature, r.Method, strconv.Itoa(r.Documents), italian(r.Net), italian(r.VAT), italian(r.Gross),
		}, ";") + "\r\n")
	}
	b.WriteString(strings.Join([]string{"Totale", "", "", "", "", "", "", italian(t.Total)}, ";") + "\r\n")
	return b.Bytes()
}

// TotalsXLSX renders the corrispettivi as a workbook with one sheet "Corrispettivi".
func TotalsXLSX(t *Totals) ([]byte, error) {
	sh := xlsx.Sheet{Name: "Corrispettivi", Header: TotalsHeader, Widths: []int{12, 12, 8, 14, 10, 14, 12, 14}}
	for _, r := range t.Rows {
		sh.Rows = append(sh.Rows, []xlsx.Cell{
			xlsx.Str(r.Period), xlsx.Decimal(strings.Replace(rate(r.RateBP), ",", ".", 1)), xlsx.Str(r.Nature), xlsx.Str(r.Method),
			xlsx.Int(int64(r.Documents)), xlsx.Cents(r.Net), xlsx.Cents(r.VAT), xlsx.Cents(r.Gross),
		})
	}
	sh.Rows = append(sh.Rows, []xlsx.Cell{xlsx.Str("Totale"), xlsx.Empty(), xlsx.Empty(), xlsx.Empty(), xlsx.Empty(), xlsx.Empty(), xlsx.Empty(), xlsx.Cents(t.Total)})
	var buf bytes.Buffer
	if err := xlsx.Write(&buf, []xlsx.Sheet{sh}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
