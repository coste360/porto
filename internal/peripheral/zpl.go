package peripheral

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/coste360/porto/internal/desktopport"
)

// RenderZPL returns a ZPL text label: UTF-8 (^CI28), one field per line, ^ and ~ removed from the text so that a
// line can never inject a command.
func RenderZPL(l desktopport.LabelDoc) []byte {
	copies := l.Copies
	if copies < 1 {
		copies = 1
	}
	var b bytes.Buffer
	b.WriteString("^XA\n^CI28\n")
	y := 40
	for _, line := range l.Lines {
		text := strings.Map(func(r rune) rune {
			if r == '^' || r == '~' || r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, line)
		b.WriteString("^FO40," + strconv.Itoa(y) + "^A0N,32,32^FD" + text + "^FS\n")
		y += 44
	}
	b.WriteString("^PQ" + strconv.Itoa(copies) + "\n^XZ\n")
	return b.Bytes()
}

// ZPLPrinter is the generic ZPL label printer over a Transport.
type ZPLPrinter struct {
	Transport desktopport.Transport
}

// Code implements desktopport.LabelPrinter.
func (ZPLPrinter) Code() string { return desktopport.DriverZPL }

// Status implements desktopport.LabelPrinter.
func (p ZPLPrinter) Status(context.Context) desktopport.PeripheralStatus {
	if p.Transport == nil {
		return desktopport.PeripheralStatus{Driver: desktopport.DriverZPL, Message: "not_configured"}
	}
	return desktopport.PeripheralStatus{Driver: desktopport.DriverZPL, Ready: true, Message: "ready"}
}

// Print implements desktopport.LabelPrinter.
func (p ZPLPrinter) Print(ctx context.Context, l desktopport.LabelDoc) error {
	if p.Transport == nil {
		return desktopport.ErrPeripheralUnavailable
	}
	return p.Transport.Write(ctx, RenderZPL(l))
}
