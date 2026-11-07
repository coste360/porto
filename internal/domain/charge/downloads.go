package charge

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/xlsx"
)

// Downloads of lists (CSV and Excel, Italian): the charge list and the payments export for reconciliation outside
// Porto (m5-plan §3.6).

// MaxDownloadRows bounds a list download.
const MaxDownloadRows = 20000

var invoicingLabels = map[string]string{
	store.ChargeToInvoice: "Da fatturare", store.ChargeExported: "Esportato", store.ChargeInvoiced: "Fatturato",
}

// table is a downloadable table: header, typed cells and widths.
type table struct {
	name   string
	header []string
	widths []int
	rows   [][]xlsx.Cell
}

// render writes the table as CSV (BOM, ';', CRLF, decimal comma) or XLSX.
func (t table) render(format string) ([]byte, error) {
	if format == FormatXLSX {
		var b bytes.Buffer
		err := xlsx.Write(&b, []xlsx.Sheet{{Name: t.name, Header: t.header, Widths: t.widths, Rows: t.rows}})
		return b.Bytes(), err
	}
	var b bytes.Buffer
	b.WriteString("\ufeff")
	w := csv.NewWriter(&b)
	w.Comma, w.UseCRLF = ';', true
	_ = w.Write(t.header)
	for _, r := range t.rows {
		rec := make([]string, len(r))
		for i, c := range r {
			switch c.Kind {
			case xlsx.KindString:
				rec[i] = csvText(c.S)
			case xlsx.KindNumber:
				rec[i] = strings.Replace(c.N, ".", ",", 1)
			case xlsx.KindDate:
				rec[i] = itDate(c.D)
			}
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

// ListFile returns the charges of a filter as CSV or XLSX (charges.manage or payments.collect).
func (s *Service) ListFile(ctx context.Context, p *auth.Principal, marinaID string, f ListFilter, overdue bool, format string) ([]byte, error) {
	scope, err := s.canRead(ctx, p, marinaID)
	if err != nil {
		return nil, err
	}
	db := s.d.DB.DB
	if overdue {
		f.OverdueOn = s.today(ctx, db, scope)
	}
	cs, err := store.AllCharges(ctx, db, scope, f, MaxDownloadRows)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, &ExportTooLargeError{Count: MaxDownloadRows + 1}
		}
		return nil, err
	}
	rows, err := s.rows(ctx, db, scope, p, cs)
	if err != nil {
		return nil, err
	}
	t := table{name: "Addebiti", header: []string{
		"Numero addebito", "Data addebito", "Scadenza", "Origine", "Cliente", "Rif. contratto", "Imponibile", "IVA", "Totale",
		"Pagato", "Da incassare", "Stato pagamento", "Stato fatturazione", "Fattura", "Data fattura", "Id addebito",
	}, widths: []int{14, 12, 12, 18, 28, 16, 12, 12, 12, 12, 12, 14, 14, 14, 12, 38}}
	for _, r := range rows {
		c := r.Charge
		name := ""
		if r.Customer != nil {
			name = r.Customer.DisplayName
		}
		t.rows = append(t.rows, []xlsx.Cell{
			xlsx.Str(c.Number), xlsx.Date(c.IssuedOn), xlsx.Date(c.DueOn), xlsx.Str(originLabels[c.OriginType]), xlsx.Str(name),
			xlsx.Str(r.ContractCode), xlsx.Cents(money.Cents(c.NetCents)), xlsx.Cents(money.Cents(c.VATCents)),
			xlsx.Cents(money.Cents(c.TotalCents)), xlsx.Cents(money.Cents(c.PaidAmountCents + c.CreditedCents)),
			xlsx.Cents(money.Cents(c.ResidualCents())), xlsx.Str(paymentLabels[c.PaymentStatus]),
			xlsx.Str(invoicingLabels[c.InvoicingStatus]), xlsx.Str(c.ExternalInvoiceNumber), xlsx.Date(c.ExternalInvoiceDate), xlsx.Str(c.ID),
		})
	}
	return t.render(format)
}

var methodLabels = map[string]string{
	"cash":     "Contanti", //nolint:misspell // Italian label
	"card_pos": "Carta (POS)", "bank_transfer": "Bonifico", "cheque": "Assegno", "other": "Altro",
	"card_online": "Carta online", "wallet": "Wallet", "sepa_debit": "Addebito SEPA", "credit_note": "Credito",
}

// PaymentsFile returns the payments and refunds of a period by method (charges.export): business date, method,
// provider and its reference, gross, provider fee, net of fee, customer name and fiscal code, matched charges.
func (s *Service) PaymentsFile(ctx context.Context, p *auth.Principal, marinaID string, from, to calendar.Date, method, format string) ([]byte, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermChargesExport)
	if err != nil {
		return nil, err
	}
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return nil, invalid("from", "invalid_range")
	}
	if format != FormatCSV && format != FormatXLSX {
		return nil, invalid("format", "invalid")
	}
	db := s.d.DB.DB
	rows, err := store.PaymentsForExport(ctx, db, scope, from, to, method, MaxDownloadRows+1)
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxDownloadRows {
		return nil, &ExportTooLargeError{Count: len(rows)}
	}
	var payIDs, custIDs []string
	for _, r := range rows {
		if r.Kind == "payment" {
			payIDs = append(payIDs, r.ID)
		}
		if r.CustomerID != "" {
			custIDs = append(custIDs, r.CustomerID)
		}
	}
	matched, err := store.MatchedChargeNumbers(ctx, db, scope, payIDs)
	if err != nil {
		return nil, err
	}
	custs, err := store.CustomersByIDs(ctx, db, scope, uniqueStrings(custIDs))
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.Customer{}
	for i := range custs {
		byID[custs[i].ID] = &custs[i]
	}
	t := table{name: "Incassi", header: []string{
		"Tipo", "Numero", "Data", "Metodo", "Provider", "Riferimento provider", "Importo", "Commissione", "Netto",
		"Cliente", "Codice fiscale", "Addebiti abbinati", "Stato",
	}, widths: []int{10, 14, 12, 16, 12, 28, 12, 12, 12, 28, 18, 30, 12}}
	for _, r := range rows {
		kind, sign := "Incasso", int64(1)
		if r.Kind == "refund" {
			kind, sign = "Rimborso", -1
			if r.PaymentKind == "payout" {
				kind = "Liquidazione"
			}
		} else if r.PaymentKind == "deposit" {
			kind = "Cauzione"
		}
		name, fc := "", ""
		if c := byID[r.CustomerID]; c != nil {
			name, fc = displayName(c), c.FiscalCode
		}
		gross := sign * r.AmountCents
		t.rows = append(t.rows, []xlsx.Cell{
			xlsx.Str(kind), xlsx.Str(r.Number), xlsx.Date(r.Day), xlsx.Str(methodLabels[r.Method]), xlsx.Str(r.ProviderCode),
			xlsx.Str(r.ProviderRef), xlsx.Cents(money.Cents(gross)), xlsx.Cents(money.Cents(r.FeeCents)),
			xlsx.Cents(money.Cents(gross - r.FeeCents)), xlsx.Str(name), xlsx.Str(fc), xlsx.Str(strings.Join(matched[r.ID], ", ")),
			xlsx.Str(r.Status),
		})
	}
	return t.render(format)
}
