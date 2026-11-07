package charge

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/calendar"
	"github.com/coste360/porto/internal/domain/chargeplan"
	"github.com/coste360/porto/internal/money"
	"github.com/coste360/porto/internal/store"
	"github.com/coste360/porto/internal/xlsx"
)

// Files of the "da fatturare" export (m5-plan §3.6): one row per charge line, in Italian, deterministic order
// (charge number, line seq). The CSV is UTF-8 with BOM, ';' separated, CRLF, decimal comma and dates GG/MM/AAAA;
// the Excel workbook has the same columns in "Righe" plus "Riepilogo IVA" and "Addebiti". No float anywhere:
// amounts are written from integer cents.

// ExportColumns are the columns of the CSV and of the sheet "Righe".
var ExportColumns = []string{
	"Numero addebito", "Data addebito", "Scadenza", "Origine", "Rif. contratto", "Rif. rata", "Imbarcazione", "Matricola",
	"Cliente", "Codice fiscale", "Partita IVA", "Codice destinatario", "PEC", "Indirizzo", "CAP", "Comune", "Provincia",
	"Nazione", "Riga", "Codice articolo", "Codice esterno", "Descrizione", "Quantità", "Prezzo unitario", "Sconto",
	"Imponibile", "Aliquota IVA", "Natura", "IVA", "Totale riga", "Classe di ricavo", "Competenza dal", "Competenza al",
	"Rettifica di", "Documento commerciale", "Stato pagamento", "Pagato", "Id addebito",
}

// originLabels are the Italian labels of the origins in the files.
var originLabels = map[string]string{
	store.ChargeOriginInstallment: "Rata", store.ChargeOriginHolderInstallment: "Rata titolare",
	store.ChargeOriginTransit: "Transito", store.ChargeOriginContractTrueUp: "Conguaglio contratto",
	store.ChargeOriginCounterSale: "Vendita al banco", store.ChargeOriginTermination: "Chiusura anticipata",
	store.ChargeOriginDepositRetention: "Trattenuta cauzione", store.ChargeOriginManual: "Manuale",
	store.ChargeOriginAdjustment: "Rettifica", store.ChargeOriginOpeningBalance: "Saldo di apertura",
	store.ChargeOriginUtility: "Utenze", store.ChargeOriginWorkOrder: "Cantiere", store.ChargeOriginRental: "Noleggio",
}

// paymentLabels are the Italian labels of the payment statuses in the files.
var paymentLabels = map[string]string{
	store.ChargeOpen: "Da pagare", store.ChargePartiallyPaid: "Pagato in parte", store.ChargePaid: "Pagato",
	store.ChargeCancelled: "Annullato",
}

// ExportCharge is a charge of an export with everything its rows show.
type ExportCharge struct {
	Charge         store.Charge
	Lines          []store.ChargeLine
	Fiscal         FiscalSnapshot
	ContractCode   string
	InstallmentSeq int
	VesselName     string
	VesselReg      string
	CorrectsNumber string
	FiscalDocument string
}

// ExportRow is one row of the files (one charge line).
type ExportRow struct {
	Charge *ExportCharge
	Line   store.ChargeLine
}

// ExportRows returns the rows of an export ordered by charge number and line seq.
func ExportRows(charges []ExportCharge) []ExportRow {
	sorted := make([]ExportCharge, len(charges))
	copy(sorted, charges)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Charge.Number < sorted[j].Charge.Number })
	var out []ExportRow
	for i := range sorted {
		c := &sorted[i]
		lines := make([]store.ChargeLine, len(c.Lines))
		copy(lines, c.Lines)
		sort.SliceStable(lines, func(a, b int) bool { return lines[a].Seq < lines[b].Seq })
		for _, l := range lines {
			out = append(out, ExportRow{Charge: c, Line: l})
		}
	}
	return out
}

// itCents formats cents with a decimal comma ("-1234,50").
func itCents(c int64) string { return strings.Replace(money.Cents(c).String(), ".", ",", 1) }

// itDate formats a date as GG/MM/AAAA ("" for a zero date).
func itDate(d calendar.Date) string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%02d/%02d/%04d", d.Day(), int(d.Month()), d.Year())
}

// decimalMilli formats a fixed-point milli value as a decimal string with sep, trailing zeros trimmed.
func decimalMilli(v int64, sep string) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v/1000, 10)
	if frac := v % 1000; frac != 0 {
		s += sep + strings.TrimRight(fmt.Sprintf("%03d", frac), "0")
	}
	if neg {
		s = "-" + s
	}
	return s
}

// ratePercent formats a VAT rate in basis points as a percentage ("22", "5,5").
func ratePercent(bp int64, sep string) string {
	s := strconv.FormatInt(bp/100, 10)
	if frac := bp % 100; frac != 0 {
		s += sep + strings.TrimRight(fmt.Sprintf("%02d", frac), "0")
	}
	return s
}

// csvText neutralises cells a spreadsheet would read as a formula.
func csvText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (r ExportRow) texts(decSep string) []string {
	c, l := r.Charge, r.Line
	ch := c.Charge
	f := c.Fiscal
	seq := ""
	if c.InstallmentSeq > 0 {
		seq = strconv.Itoa(c.InstallmentSeq)
	}
	paid := ch.PaidAmountCents + ch.CreditedCents
	amount := func(v int64) string {
		if decSep == "," {
			return itCents(v)
		}
		return money.Cents(v).String()
	}
	return []string{
		ch.Number, itDate(ch.IssuedOn), itDate(ch.DueOn), originLabels[ch.OriginType], c.ContractCode, seq,
		csvText(c.VesselName), csvText(c.VesselReg), csvText(f.Name), csvText(f.FiscalCode), csvText(f.VATNumber),
		csvText(f.SDICode), csvText(f.PEC), csvText(strings.TrimSpace(f.Address.Line1 + " " + f.Address.Line2)),
		csvText(f.Address.PostalCode), csvText(f.Address.City), csvText(f.Address.Province), csvText(f.Address.Country),
		strconv.Itoa(l.Seq), csvText(l.ItemCode), csvText(l.ExternalCode), csvText(l.Description),
		decimalMilli(l.QtyMilli, decSep), amount(l.UnitCents), amount(l.DiscountCents), amount(l.NetCents),
		ratePercent(l.VATRateBP, decSep), l.VATNature, amount(l.VATCents), amount(l.GrossCents), csvText(l.RevenueClassCode),
		itDate(l.ServiceFrom), itDate(l.ServiceTo), c.CorrectsNumber, c.FiscalDocument, paymentLabels[ch.PaymentStatus],
		amount(paid), ch.ID,
	}
}

// ExportCSV writes the CSV of the rows (UTF-8 with BOM, ';', CRLF, decimal comma, GG/MM/AAAA).
func ExportCSV(rows []ExportRow) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("\ufeff")
	w := csv.NewWriter(&b)
	w.Comma = ';'
	w.UseCRLF = true
	if err := w.Write(ExportColumns); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if err := w.Write(r.texts(",")); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

// exportWidths are the column widths of the sheet "Righe".
var exportWidths = []int{
	14, 12, 12, 18, 16, 8, 18, 12, 28, 18, 14, 10, 24, 30, 8, 18, 8, 8, 6, 14, 12, 36, 10, 12, 10,
	12, 8, 8, 10, 12, 14, 12, 12, 14, 14, 14, 12, 38,
}

// ExportXLSX writes the workbook of the rows: "Righe" (same columns as the CSV, numbers and dates typed),
// "Riepilogo IVA" (per rate and nature) and "Addebiti" (one row per charge).
func ExportXLSX(rows []ExportRow) ([]byte, error) {
	righe := xlsx.Sheet{Name: "Righe", Header: ExportColumns, Widths: exportWidths}
	var lines []chargeplan.Line
	var charges []*ExportCharge
	seen := map[string]bool{}
	for _, r := range rows {
		c, l := r.Charge, r.Line
		ch := c.Charge
		f := c.Fiscal
		seq := xlsx.Empty()
		if c.InstallmentSeq > 0 {
			seq = xlsx.Int(int64(c.InstallmentSeq))
		}
		righe.Rows = append(righe.Rows, []xlsx.Cell{
			xlsx.Str(ch.Number), xlsx.Date(ch.IssuedOn), xlsx.Date(ch.DueOn), xlsx.Str(originLabels[ch.OriginType]),
			xlsx.Str(c.ContractCode), seq, xlsx.Str(c.VesselName), xlsx.Str(c.VesselReg), xlsx.Str(f.Name),
			xlsx.Str(f.FiscalCode), xlsx.Str(f.VATNumber), xlsx.Str(f.SDICode), xlsx.Str(f.PEC),
			xlsx.Str(strings.TrimSpace(f.Address.Line1 + " " + f.Address.Line2)), xlsx.Str(f.Address.PostalCode),
			xlsx.Str(f.Address.City), xlsx.Str(f.Address.Province), xlsx.Str(f.Address.Country), xlsx.Int(int64(l.Seq)),
			xlsx.Str(l.ItemCode), xlsx.Str(l.ExternalCode), xlsx.Str(l.Description), xlsx.Decimal(decimalMilli(l.QtyMilli, ".")),
			xlsx.Cents(money.Cents(l.UnitCents)), xlsx.Cents(money.Cents(l.DiscountCents)), xlsx.Cents(money.Cents(l.NetCents)),
			xlsx.Decimal(ratePercent(l.VATRateBP, ".")), xlsx.Str(l.VATNature), xlsx.Cents(money.Cents(l.VATCents)),
			xlsx.Cents(money.Cents(l.GrossCents)), xlsx.Str(l.RevenueClassCode), xlsx.Date(l.ServiceFrom), xlsx.Date(l.ServiceTo),
			xlsx.Str(c.CorrectsNumber), xlsx.Str(c.FiscalDocument), xlsx.Str(paymentLabels[ch.PaymentStatus]),
			xlsx.Cents(money.Cents(ch.PaidAmountCents + ch.CreditedCents)), xlsx.Str(ch.ID),
		})
		lines = append(lines, planLineOf(l))
		if !seen[ch.ID] {
			seen[ch.ID] = true
			charges = append(charges, c)
		}
	}
	vat := xlsx.Sheet{Name: "Riepilogo IVA", Header: []string{"Aliquota IVA", "Natura", "Imponibile", "IVA", "Totale"}, Widths: []int{12, 8, 14, 12, 14}}
	for _, b := range chargeplan.Totals(lines) {
		vat.Rows = append(vat.Rows, []xlsx.Cell{
			xlsx.Decimal(ratePercent(b.RateBP, ".")), xlsx.Str(b.Nature), xlsx.Cents(b.Net), xlsx.Cents(b.VAT), xlsx.Cents(b.Gross),
		})
	}
	add := xlsx.Sheet{Name: "Addebiti", Header: []string{
		"Numero addebito", "Data addebito", "Scadenza", "Origine", "Rif. contratto", "Cliente", "Codice fiscale", "Partita IVA",
		"Imponibile", "IVA", "Totale", "Rettifica di", "Documento commerciale", "Stato pagamento", "Id addebito",
	}, Widths: []int{14, 12, 12, 18, 16, 28, 18, 14, 12, 12, 12, 14, 14, 14, 38}}
	for _, c := range charges {
		ch := c.Charge
		add.Rows = append(add.Rows, []xlsx.Cell{
			xlsx.Str(ch.Number), xlsx.Date(ch.IssuedOn), xlsx.Date(ch.DueOn), xlsx.Str(originLabels[ch.OriginType]),
			xlsx.Str(c.ContractCode), xlsx.Str(c.Fiscal.Name), xlsx.Str(c.Fiscal.FiscalCode), xlsx.Str(c.Fiscal.VATNumber),
			xlsx.Cents(money.Cents(ch.NetCents)), xlsx.Cents(money.Cents(ch.VATCents)), xlsx.Cents(money.Cents(ch.TotalCents)),
			xlsx.Str(c.CorrectsNumber), xlsx.Str(c.FiscalDocument), xlsx.Str(paymentLabels[ch.PaymentStatus]), xlsx.Str(ch.ID),
		})
	}
	var b bytes.Buffer
	if err := xlsx.Write(&b, []xlsx.Sheet{righe, vat, add}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ExportTotals are the totals of an export (stored on the export row as JSON).
type ExportTotals struct {
	Net          money.Cents            `json:"net_cents"`
	VAT          money.Cents            `json:"vat_cents"`
	Gross        money.Cents            `json:"gross_cents"`
	ByVAT        []chargeplan.VATBucket `json:"by_vat"`
	ByOrigin     []OriginTotal          `json:"by_origin"`
	ByRevenue    []OriginTotal          `json:"by_revenue_class"`
	ChargeCount  int                    `json:"charge_count"`
	LineCount    int                    `json:"line_count"`
	ChargeTotals money.Cents            `json:"charge_totals_cents"`
}

// OriginTotal is a count and a total for a key (origin type, revenue class).
type OriginTotal struct {
	Key   string      `json:"key"`
	Count int         `json:"count"`
	Total money.Cents `json:"total_cents"`
}

// TotalsOf computes the totals of charges and their lines: per (rate, nature) from the lines, per origin from the
// charge totals, per revenue class from the lines.
func TotalsOf(charges []store.Charge, lines []store.ChargeLine) ExportTotals {
	t := ExportTotals{ChargeCount: len(charges), LineCount: len(lines)}
	pl := make([]chargeplan.Line, 0, len(lines))
	byRev := map[string]*OriginTotal{}
	for _, l := range lines {
		pl = append(pl, planLineOf(l))
		t.Net += money.Cents(l.NetCents)
		t.VAT += money.Cents(l.VATCents)
		t.Gross += money.Cents(l.GrossCents)
		r := byRev[l.RevenueClassCode]
		if r == nil {
			r = &OriginTotal{Key: l.RevenueClassCode}
			byRev[l.RevenueClassCode] = r
		}
		r.Count++
		r.Total += money.Cents(l.GrossCents)
	}
	t.ByVAT = chargeplan.Totals(pl)
	byOrigin := map[string]*OriginTotal{}
	for _, c := range charges {
		o := byOrigin[c.OriginType]
		if o == nil {
			o = &OriginTotal{Key: c.OriginType}
			byOrigin[c.OriginType] = o
		}
		o.Count++
		o.Total += money.Cents(c.TotalCents)
		t.ChargeTotals += money.Cents(c.TotalCents)
	}
	t.ByOrigin = sortedTotals(byOrigin)
	t.ByRevenue = sortedTotals(byRev)
	return t
}

func sortedTotals(m map[string]*OriginTotal) []OriginTotal {
	out := make([]OriginTotal, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
