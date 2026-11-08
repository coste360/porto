package peripheral

import (
	"bytes"
	"context"
	"strings"

	"github.com/coste360/porto/internal/desktopport"
)

// ESC/POS commands of the generic receipt printer (m6b-plan §8.2, D17). Only commands every ESC/POS printer
// understands are used: no logos, no barcodes, no printer-specific code pages besides CP858.
var (
	escInit        = []byte{0x1b, 0x40}       // ESC @
	escCodePage858 = []byte{0x1b, 0x74, 0x13} // ESC t 19: PC858 (Latin-1 with €)
	escBoldOn      = []byte{0x1b, 0x45, 0x01} // ESC E 1
	escBoldOff     = []byte{0x1b, 0x45, 0x00} // ESC E 0
	escAlignLeft   = []byte{0x1b, 0x61, 0x00} // ESC a 0
	escAlignCenter = []byte{0x1b, 0x61, 0x01} // ESC a 1
	gsPartialCut   = []byte{0x1d, 0x56, 0x42, 0x00}
)

// Columns of a line of the font A per paper width.
const (
	columns80 = 48
	columns58 = 32
)

// Columns returns the characters per line of a paper width (80 mm unless 58).
func Columns(width int) int {
	if width == desktopport.ReceiptWidth58 {
		return columns58
	}
	return columns80
}

// cp858 maps the non-ASCII runes Porto prints to code page 858; anything else becomes '?'.
var cp858 = map[rune]byte{
	'€': 0xd5, 'à': 0x85, 'á': 0xa0, 'è': 0x8a, 'é': 0x82, 'ì': 0x8d, 'í': 0xa1, 'ò': 0x95, 'ó': 0xa2,
	'ù': 0x97, 'ú': 0xa3, 'À': 0xb7, 'Á': 0xb5, 'È': 0xd4, 'É': 0x90, 'Ì': 0xde, 'Í': 0xd6, 'Ò': 0xe3,
	'Ó': 0xe0, 'Ù': 0xeb, 'Ú': 0xe9, 'ç': 0x87, 'Ç': 0x80, 'ñ': 0xa4, 'Ñ': 0xa5, 'ä': 0x84, 'ö': 0x94,
	'ü': 0x81, 'Ä': 0x8e, 'Ö': 0x99, 'Ü': 0x9a, 'ß': 0xe1, '°': 0xf8, '·': 0xfa, '’': '\'', '‘': '\'',
	'“': '"', '”': '"', '–': '-', '—': '-',
}

// EncodeCP858 encodes s for the printer: printable ASCII as is, mapped runes, '?' for the rest. Control
// characters are dropped so that a description can never inject a printer command.
func EncodeCP858(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 0x20 && r < 0x7f:
			out = append(out, byte(r))
		case r == '\t':
			out = append(out, ' ')
		case r < 0x20 || r == 0x7f:
		default:
			if b, ok := cp858[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

// RenderESCPOS returns the bytes of a courtesy receipt for a printer of the given paper width.
func RenderESCPOS(r desktopport.ReceiptDoc, width int) []byte {
	if r.Width != 0 {
		width = r.Width
	}
	cols := Columns(width)
	var b bytes.Buffer
	w := func(cmds ...[]byte) {
		for _, c := range cmds {
			b.Write(c)
		}
	}
	line := func(s string) {
		b.Write(EncodeCP858(s))
		b.WriteByte('\n')
	}
	sep := func() { line(strings.Repeat("-", cols)) }

	w(escInit, escCodePage858, escAlignCenter, escBoldOn)
	for _, l := range wrap(r.MarinaName, cols) {
		line(l)
	}
	w(escBoldOff)
	for _, s := range []string{r.LegalName, r.Address} {
		for _, l := range wrap(s, cols) {
			line(l)
		}
	}
	if r.VATNumber != "" {
		for _, l := range wrap("P.IVA "+r.VATNumber, cols) {
			line(l)
		}
	}
	w(escAlignLeft)
	sep()
	line(twoCols("Ricevuta "+r.Number, r.Date, cols))
	sep()
	for _, l := range r.Lines {
		for _, d := range wrap(l.Description, cols) {
			line(d)
		}
		left := "  "
		if l.QtyMilli != 0 && l.QtyMilli != 1000 {
			left += FormatQty(l.QtyMilli) + " x "
		}
		if l.VATRateBP != 0 {
			left += "IVA " + FormatRate(l.VATRateBP)
		}
		line(twoCols(left, "€ "+FormatCents(l.GrossCents), cols))
	}
	sep()
	w(escBoldOn)
	line(twoCols("TOTALE", "€ "+FormatCents(r.AmountCents), cols))
	w(escBoldOff)
	line(twoCols("Pagamento", methodLabel(r.Method), cols))
	if len(r.VAT) > 0 {
		sep()
		for _, v := range r.VAT {
			label := "IVA " + FormatRate(v.RateBP)
			if v.RateBP == 0 && v.Nature != "" {
				label = "Natura " + v.Nature
			}
			line(twoCols(label+" su "+FormatCents(v.NetCents), FormatCents(v.VATCents), cols))
		}
	}
	if r.FiscalNumber != "" {
		sep()
		for _, l := range wrap("Documento commerciale "+r.FiscalNumber, cols) {
			line(l)
		}
	}
	if r.Footer != "" {
		sep()
		for _, para := range strings.Split(r.Footer, "\n") {
			for _, l := range wrap(para, cols) {
				line(l)
			}
		}
	}
	sep()
	w(escAlignCenter, escBoldOn)
	line("NON VALIDO AI FINI FISCALI")
	w(escBoldOff, escAlignLeft)
	b.WriteString("\n\n\n\n")
	w(gsPartialCut)
	return b.Bytes()
}

// ESCPOSPrinter is the generic ESC/POS receipt printer over a Transport.
type ESCPOSPrinter struct {
	Transport desktopport.Transport
	Width     int
}

// Code implements desktopport.ReceiptPrinter.
func (ESCPOSPrinter) Code() string { return desktopport.DriverESCPOS }

// Status implements desktopport.ReceiptPrinter. A raw printer has no status channel in M6b: a configured
// printer is reported ready and errors surface when printing.
func (p ESCPOSPrinter) Status(context.Context) desktopport.PeripheralStatus {
	if p.Transport == nil {
		return desktopport.PeripheralStatus{Driver: desktopport.DriverESCPOS, Message: "not_configured"}
	}
	return desktopport.PeripheralStatus{Driver: desktopport.DriverESCPOS, Ready: true, Message: "ready"}
}

// Print implements desktopport.ReceiptPrinter.
func (p ESCPOSPrinter) Print(ctx context.Context, r desktopport.ReceiptDoc) error {
	if p.Transport == nil {
		return desktopport.ErrPeripheralUnavailable
	}
	return p.Transport.Write(ctx, RenderESCPOS(r, p.Width))
}
