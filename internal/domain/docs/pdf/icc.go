package pdf

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"
	"unicode/utf16"
)

// SRGBProfile returns an ICC v4 display profile of sRGB built by own code from the published sRGB definition
// (IEC 61966-2-1): the primaries and D65 white point chromaticities, adapted to the D50 profile connection space
// with the Bradford transform, and the sRGB tone curve as a parametric curve of type 3. No third-party profile
// bytes are embedded (m4-plan §6.3). The bytes are constant.
func SRGBProfile() []byte {
	iccOnce.Do(func() { iccBytes = buildSRGB() })
	return iccBytes
}

var (
	iccOnce  sync.Once
	iccBytes []byte
)

type (
	vec3 [3]float64
	mat3 [3]vec3
)

func (m mat3) mul(v vec3) vec3 {
	var r vec3
	for i := range 3 {
		r[i] = m[i][0]*v[0] + m[i][1]*v[1] + m[i][2]*v[2]
	}
	return r
}

func (m mat3) mulM(n mat3) mat3 {
	var r mat3
	for i := range 3 {
		for j := range 3 {
			r[i][j] = m[i][0]*n[0][j] + m[i][1]*n[1][j] + m[i][2]*n[2][j]
		}
	}
	return r
}

func (m mat3) inverse() mat3 {
	a := m
	det := a[0][0]*(a[1][1]*a[2][2]-a[1][2]*a[2][1]) - a[0][1]*(a[1][0]*a[2][2]-a[1][2]*a[2][0]) +
		a[0][2]*(a[1][0]*a[2][1]-a[1][1]*a[2][0])
	var r mat3
	r[0][0] = (a[1][1]*a[2][2] - a[1][2]*a[2][1]) / det
	r[0][1] = (a[0][2]*a[2][1] - a[0][1]*a[2][2]) / det
	r[0][2] = (a[0][1]*a[1][2] - a[0][2]*a[1][1]) / det
	r[1][0] = (a[1][2]*a[2][0] - a[1][0]*a[2][2]) / det
	r[1][1] = (a[0][0]*a[2][2] - a[0][2]*a[2][0]) / det
	r[1][2] = (a[0][2]*a[1][0] - a[0][0]*a[1][2]) / det
	r[2][0] = (a[1][0]*a[2][1] - a[1][1]*a[2][0]) / det
	r[2][1] = (a[0][1]*a[2][0] - a[0][0]*a[2][1]) / det
	r[2][2] = (a[0][0]*a[1][1] - a[0][1]*a[1][0]) / det
	return r
}

func xyToXYZ(x, y float64) vec3 { return vec3{x / y, 1, (1 - x - y) / y} }

// srgbToD50 returns the RGB→XYZ(D50) matrix (columns = adapted primaries) and the Bradford D65→D50 matrix.
func srgbToD50() (mat3, mat3) {
	r, g, b := xyToXYZ(0.64, 0.33), xyToXYZ(0.30, 0.60), xyToXYZ(0.15, 0.06)
	white := xyToXYZ(0.3127, 0.3290)
	p := mat3{{r[0], g[0], b[0]}, {r[1], g[1], b[1]}, {r[2], g[2], b[2]}}
	s := p.inverse().mul(white)
	m := mat3{
		{p[0][0] * s[0], p[0][1] * s[1], p[0][2] * s[2]},
		{p[1][0] * s[0], p[1][1] * s[1], p[1][2] * s[2]},
		{p[2][0] * s[0], p[2][1] * s[1], p[2][2] * s[2]},
	}
	bradford := mat3{{0.8951, 0.2664, -0.1614}, {-0.7502, 1.7135, 0.0367}, {0.0389, -0.0685, 1.0296}}
	d50 := vec3{0.9642, 1.0, 0.8249}
	cs, cd := bradford.mul(white), bradford.mul(d50)
	scale := mat3{{cd[0] / cs[0], 0, 0}, {0, cd[1] / cs[1], 0}, {0, 0, cd[2] / cs[2]}}
	adapt := bradford.inverse().mulM(scale).mulM(bradford)
	return adapt.mulM(m), adapt
}

func s15f16(v float64) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(int32(math.Round(v*65536))))
	return b[:]
}

func xyzTag(v vec3) []byte {
	var b bytes.Buffer
	b.WriteString("XYZ ")
	b.Write(make([]byte, 4))
	for _, c := range v {
		b.Write(s15f16(c))
	}
	return b.Bytes()
}

func mlucTag(s string) []byte {
	u := utf16.Encode([]rune(s))
	var b bytes.Buffer
	b.WriteString("mluc")
	b.Write(make([]byte, 4))
	_ = binary.Write(&b, binary.BigEndian, uint32(1))  // records
	_ = binary.Write(&b, binary.BigEndian, uint32(12)) // record size
	b.WriteString("enUS")
	_ = binary.Write(&b, binary.BigEndian, uint32(len(u)*2))
	_ = binary.Write(&b, binary.BigEndian, uint32(28))
	for _, c := range u {
		_ = binary.Write(&b, binary.BigEndian, c)
	}
	return b.Bytes()
}

func paraTag() []byte {
	var b bytes.Buffer
	b.WriteString("para")
	b.Write(make([]byte, 4))
	_ = binary.Write(&b, binary.BigEndian, uint16(3)) // function type 3: Y = (aX+b)^g if X >= d, else cX
	b.Write(make([]byte, 2))
	for _, v := range []float64{2.4, 1 / 1.055, 0.055 / 1.055, 1 / 12.92, 0.04045} {
		b.Write(s15f16(v))
	}
	return b.Bytes()
}

func sf32Tag(m mat3) []byte {
	var b bytes.Buffer
	b.WriteString("sf32")
	b.Write(make([]byte, 4))
	for i := range 3 {
		for j := range 3 {
			b.Write(s15f16(m[i][j]))
		}
	}
	return b.Bytes()
}

func buildSRGB() []byte {
	rgb, adapt := srgbToD50()
	col := func(j int) vec3 { return vec3{rgb[0][j], rgb[1][j], rgb[2][j]} }
	trc := paraTag()
	tags := []struct {
		sig  string
		data []byte
	}{
		{"desc", mlucTag("Porto sRGB")},
		{"cprt", mlucTag("No copyright, use freely")},
		{"wtpt", xyzTag(vec3{0.9642, 1.0, 0.8249})},
		{"chad", sf32Tag(adapt)},
		{"rXYZ", xyzTag(col(0))},
		{"gXYZ", xyzTag(col(1))},
		{"bXYZ", xyzTag(col(2))},
		{"rTRC", trc},
		{"gTRC", trc},
		{"bTRC", trc},
	}
	pad := func(n int) int { return (n + 3) &^ 3 }
	offset := 128 + 4 + 12*len(tags)
	var table, data bytes.Buffer
	_ = binary.Write(&table, binary.BigEndian, uint32(len(tags)))
	shared := map[string]int{}
	for _, t := range tags {
		key := string(t.data)
		off, ok := shared[key]
		if !ok {
			off = offset + data.Len()
			shared[key] = off
			data.Write(t.data)
			data.Write(make([]byte, pad(len(t.data))-len(t.data)))
		}
		table.WriteString(t.sig)
		_ = binary.Write(&table, binary.BigEndian, uint32(off))
		_ = binary.Write(&table, binary.BigEndian, uint32(len(t.data)))
	}
	size := 128 + table.Len() + data.Len()
	var h bytes.Buffer
	_ = binary.Write(&h, binary.BigEndian, uint32(size))
	h.Write(make([]byte, 4))                                   // preferred CMM
	_ = binary.Write(&h, binary.BigEndian, uint32(0x04200000)) // version 4.2
	h.WriteString("mntr")                                      // display device
	h.WriteString("RGB ")                                      // data colour space
	h.WriteString("XYZ ")                                      // PCS
	for _, v := range []uint16{2026, 1, 1, 0, 0, 0} {          // creation date (constant)
		_ = binary.Write(&h, binary.BigEndian, v)
	}
	h.WriteString("acsp")
	h.Write(make([]byte, 4+4+4+4+8+4)) // platform, flags, manufacturer, model, attributes, intent (perceptual)
	h.Write(s15f16(0.9642))
	h.Write(s15f16(1.0))
	h.Write(s15f16(0.8249))
	h.WriteString("PRTO")        // creator
	h.Write(make([]byte, 16+28)) // profile id (zero = not computed) and reserved
	out := append(h.Bytes(), table.Bytes()...)
	return append(out, data.Bytes()...)
}
