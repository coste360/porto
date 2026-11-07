package docs

import (
	"math"
	"strconv"
	"strings"
)

// Go mirror of web/shared/src/map/vessel-shape.ts (m3-plan D25, m4-plan §15 D30): the same vessel gives the same
// path string as the web map, checked by the shared parity vectors in testdata/silhouette. The map print draws the
// polygons of the path. Floats are confined to this drawing code; every coordinate is rounded half-even to cm.

func roundHalfEven(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	f := math.Floor(v)
	d := v - f
	switch {
	case d > 0.5:
		return f + 1
	case d < 0.5:
		return f
	}
	if math.Mod(f, 2) == 0 {
		return f
	}
	return f + 1
}

func num(v float64) string {
	r := roundHalfEven(v)
	if r == 0 {
		return "0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

func pt(x, y float64) string { return num(x) + " " + num(y) }

type hullOpts struct {
	sternRatio, shoulder float64
	pointedStern         bool
}

func hullPath(x0, x1, cy, h float64, o hullOpts) string {
	l := x1 - x0
	sh := x0 + l*o.shoulder
	sternH := h * o.sternRatio
	if o.pointedStern {
		mid := x0 + l/2
		return strings.Join([]string{
			"M" + pt(x0, cy),
			"Q" + pt(x0+l*0.12, cy-h) + " " + pt(mid, cy-h),
			"Q" + pt(x1-l*0.12, cy-h) + " " + pt(x1, cy),
			"Q" + pt(x1-l*0.12, cy+h) + " " + pt(mid, cy+h),
			"Q" + pt(x0+l*0.12, cy+h) + " " + pt(x0, cy),
			"Z",
		}, " ")
	}
	return strings.Join([]string{
		"M" + pt(x0, cy-sternH),
		"L" + pt(sh, cy-h),
		"Q" + pt(x1-l*0.08, cy-h) + " " + pt(x1, cy),
		"Q" + pt(x1-l*0.08, cy+h) + " " + pt(sh, cy+h),
		"L" + pt(x0, cy+sternH),
		"Z",
	}, " ")
}

func roundedRect(x0, y0, x1, y1, rad float64) string {
	k := math.Min(rad, math.Min((x1-x0)/2, (y1-y0)/2))
	return strings.Join([]string{
		"M" + pt(x0+k, y0),
		"L" + pt(x1-k, y0),
		"Q" + pt(x1, y0) + " " + pt(x1, y0+k),
		"L" + pt(x1, y1-k),
		"Q" + pt(x1, y1) + " " + pt(x1-k, y1),
		"L" + pt(x0+k, y1),
		"Q" + pt(x0, y1) + " " + pt(x0, y1-k),
		"L" + pt(x0, y0+k),
		"Q" + pt(x0, y0) + " " + pt(x0+k, y0),
		"Z",
	}, " ")
}

// VesselPath is the SVG path of a vessel silhouette in cm (stern at x = 0, bow at x = loa, beam centred on y = 0);
// "" for invalid dimensions.
func VesselPath(loaCm, beamCm int64, hullType string) string {
	loa, beam := float64(loaCm), float64(beamCm)
	if loa <= 0 || beam <= 0 {
		return ""
	}
	h := beam / 2
	switch hullType {
	case "monohull_motor":
		return hullPath(0, loa, 0, h, hullOpts{sternRatio: 0.92, shoulder: 0.55})
	case "monohull_sail":
		return hullPath(0, loa, 0, h, hullOpts{sternRatio: 0.7, shoulder: 0.45})
	case "gozzo":
		return hullPath(0, loa, 0, h, hullOpts{sternRatio: 0, shoulder: 0.5, pointedStern: true})
	case "rib":
		tube := math.Max(beam*0.18, 1)
		return hullPath(0, loa, 0, h, hullOpts{sternRatio: 1, shoulder: 0.65}) + " " +
			hullPath(tube, loa-tube*1.5, 0, h-tube, hullOpts{sternRatio: 1, shoulder: 0.65})
	case "catamaran", "trimaran":
		n := 2
		hullW := beam * 0.26
		centres := []float64{-h + hullW/2, h - hullW/2}
		if hullType == "trimaran" {
			n, hullW = 3, beam*0.2
			centres = []float64{-h + hullW/2, 0, h - hullW/2}
		}
		parts := make([]string, 0, n+1)
		for i, cy := range centres {
			w := hullW
			if n == 3 && i == 1 {
				w = hullW * 0.6
			}
			parts = append(parts, hullPath(0, loa, cy, w/2, hullOpts{sternRatio: 0.8, shoulder: 0.6}))
		}
		parts = append(parts, roundedRect(loa*0.08, -h+hullW/2, loa*0.7, h-hullW/2, beam*0.05))
		return strings.Join(parts, " ")
	default:
		return roundedRect(0, -h, loa, h, math.Min(beam, loa)*0.3)
	}
}

// PathPolygons flattens an M/L/Q/Z path into closed polygons (quadratic curves sampled in 6 steps).
func PathPolygons(d string) [][][2]float64 {
	tok := strings.Fields(d)
	var out [][][2]float64
	var cur [][2]float64
	read := func(i int) ([2]float64, int) {
		x, _ := strconv.ParseFloat(strings.TrimLeft(tok[i], "MLQZ"), 64)
		y, _ := strconv.ParseFloat(tok[i+1], 64)
		return [2]float64{x, y}, i + 2
	}
	for i := 0; i < len(tok); {
		t := tok[i]
		switch {
		case t == "Z":
			if len(cur) > 2 {
				out = append(out, cur)
			}
			cur = nil
			i++
		case strings.HasPrefix(t, "M"), strings.HasPrefix(t, "L"):
			var p [2]float64
			p, i = read(i)
			cur = append(cur, p)
		case strings.HasPrefix(t, "Q"):
			var c, e [2]float64
			c, i = read(i)
			e, i = read(i)
			if len(cur) == 0 {
				cur = append(cur, c)
			}
			s := cur[len(cur)-1]
			for k := 1; k <= 6; k++ {
				u := float64(k) / 6
				a, b, cc := (1-u)*(1-u), 2*(1-u)*u, u*u
				cur = append(cur, [2]float64{a*s[0] + b*c[0] + cc*e[0], a*s[1] + b*c[1] + cc*e[1]})
			}
		default:
			i++
		}
	}
	if len(cur) > 2 {
		out = append(out, cur)
	}
	return out
}
