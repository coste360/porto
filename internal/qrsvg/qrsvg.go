// Package qrsvg renders QR codes as small, self-contained SVG documents (pairing codes, TOTP enrolment). The
// bitmap comes from github.com/skip2/go-qrcode; the SVG is one path of unit squares, with no scripts or external
// references, so the web app can show it as an <img src="data:image/svg+xml,…">.
package qrsvg

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// Render returns the SVG of a QR code (error correction level M) encoding content.
func Render(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("qrsvg: %w", err)
	}
	q.DisableBorder = false
	bm := q.Bitmap()
	n := len(bm)
	var path strings.Builder
	for y, row := range bm {
		for x := 0; x < len(row); x++ {
			if !row[x] {
				continue
			}
			start := x
			for x < len(row) && row[x] {
				x++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", start, y, x-start, x-start)
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="%s"/></svg>`, n, n, n, n, path.String()), nil
}
