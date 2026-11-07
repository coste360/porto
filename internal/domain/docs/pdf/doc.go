// Package pdf draws a layout with fpdf (github.com/go-pdf/fpdf, MIT) using the embedded Go fonts
// (golang.org/x/image/font/gofont, BSD-3, never the core fonts), finalises documents to sign as PDF/A-2b with own
// code (XMP, own sRGB ICC output intent, file id) and appends pages by incremental update so the original bytes
// stay a byte-exact prefix (m4-plan §6.3, §15 D14). Output is deterministic: same layout and instant, same bytes.
//
// This is the only package of the document pipeline that works in floats (fpdf millimetres); it never sees money
// values as numbers, only formatted strings.
package pdf

import (
	// The PDF writer and the fonts DOCUMENTS builds on (m4-plan §13.1 adds them to go.mod).
	_ "github.com/go-pdf/fpdf"
	_ "golang.org/x/image/font/gofont/goregular"
)
