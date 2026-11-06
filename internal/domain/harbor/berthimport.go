package harbor

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/domain/layout"
	"github.com/coste360/porto/internal/geo"
	"github.com/coste360/porto/internal/qty"
	"github.com/coste360/porto/internal/store"
)

// Berth CSV import (spec 8.7, m1-plan §4.3 and §11 D23): rows are placed along existing piers and quays with
// the placement maths of the generators; the dry run only reports; apply builds the full document (current
// berths + new ones) and goes through applyDocument, so validation and INV-04 are the same as saveMap.

// MaxImportRows bounds the data rows of a CSV import.
const MaxImportRows = 5000

// ImportMapping gives the column index (0-based) of each field; optional fields may be nil.
type ImportMapping struct {
	Code, Pier, Length, Width            int
	Depth, PriceClass, Side, MooringType *int
}

// BerthImport is an import request.
type BerthImport struct {
	CSV        string
	Delimiter  string // comma (default), semicolon, tab
	HasHeader  bool
	Mapping    ImportMapping
	Units      string // m | cm
	Start      string // after_existing | root
	SidePolicy string // alternate | column | left | right
	DryRun     bool
}

// ImportFieldError is a field error of a row.
type ImportFieldError struct{ Field, Code string }

// ImportRejected is a rejected row.
type ImportRejected struct {
	Row    int
	Errors []ImportFieldError
}

// ImportDuplicate is a row whose code is taken.
type ImportDuplicate struct {
	Row    int
	Code   string
	Reason string // code_in_file | code_on_map
}

// ImportPlanned is a row that will be (or was) inserted.
type ImportPlanned struct {
	Row        int
	Code, Pier string
	Side       string
}

// ImportReport is the report of an import (BerthImportReport).
type ImportReport struct {
	RowsTotal  int
	Valid      int
	Rejected   []ImportRejected
	Duplicates []ImportDuplicate
	Planned    []ImportPlanned
	Document   *SaveResult // when applied
}

func checkImport(in *BerthImport) error {
	c := checker{&auth.ValidationError{}}
	if in.Delimiter == "" {
		in.Delimiter = "comma"
	}
	c.enum("delimiter", in.Delimiter, []string{"comma", "semicolon", "tab"})
	c.enum("units", in.Units, []string{"m", "cm"})
	c.enum("placement.start", in.Start, []string{"after_existing", "root"})
	c.enum("placement.side_policy", in.SidePolicy, []string{"alternate", "column", "left", "right"})
	m := in.Mapping
	for _, f := range []struct {
		name string
		v    *int
	}{
		{"code", &m.Code},
		{"pier", &m.Pier},
		{"length", &m.Length},
		{"width", &m.Width},
		{"depth", m.Depth},
		{"price_class", m.PriceClass},
		{"side", m.Side},
		{"mooring_type", m.MooringType},
	} {
		if f.v != nil && (*f.v < 0 || *f.v > 255) {
			c.add("mapping."+f.name, "out_of_range")
		}
	}
	if in.SidePolicy == "column" && m.Side == nil {
		c.add("mapping.side", "required")
	}
	return c.v.Err()
}

func parseCSV(in *BerthImport) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(in.CSV, "\uFEFF")))
	r.Comma = map[string]rune{"comma": ',', "semicolon": ';', "tab": '\t'}[in.Delimiter]
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	var rows [][]string
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrCSVInvalid
		}
		rows = append(rows, rec)
		if len(rows) > MaxImportRows+1 {
			return nil, ErrCSVInvalid
		}
	}
	if in.HasHeader && len(rows) > 0 {
		rows = rows[1:]
	}
	if len(rows) == 0 || len(rows) > MaxImportRows {
		return nil, ErrCSVInvalid
	}
	return rows, nil
}

// parseLength parses a length in metres (up to 3 decimals, rounded half-even to the cm) or in integer cm.
func parseLength(s, units string) (int64, bool) {
	s = strings.TrimSpace(s)
	if units == "cm" {
		v, err := strconv.ParseInt(s, 10, 64)
		return v, err == nil && v > 0 && v <= int64(layout.MaxMapSide)
	}
	m, err := qty.Parse(s)
	if err != nil {
		return 0, false
	}
	v := int64(m.Round(2)) / 10
	return v, v > 0 && v <= int64(layout.MaxMapSide)
}

type importRow struct {
	row                  int
	code, pier, side     string
	width, length, depth int64
	priceClass, mooring  string
	errors               []ImportFieldError
	structure            *layout.Structure
}

// ImportBerths runs a berth CSV import on a map (harbor.configure). With DryRun the report is computed and
// nothing is written; otherwise ifMatch must equal the map version and the valid rows are saved together.
func (s *Service) ImportBerths(ctx context.Context, p *auth.Principal, marinaID, mapID string, ifMatch int, in BerthImport) (*ImportReport, error) {
	scope, err := p.Authorize(ctx, s.d.DB.DB, marinaID, auth.PermHarborConfigure)
	if err != nil {
		return nil, err
	}
	if _, err := store.GetMap(ctx, s.d.DB.DB, scope, mapID); err != nil {
		return nil, err
	}
	if err := checkImport(&in); err != nil {
		return nil, err
	}
	rows, err := parseCSV(&in)
	if err != nil {
		return nil, err
	}
	var rep *ImportReport
	run := func(ctx context.Context, tx bun.IDB, m *store.Map) error {
		d, err := loadDocument(ctx, tx, scope, m)
		if err != nil {
			return err
		}
		settings, err := loadSettings(ctx, tx, scope)
		if err != nil {
			return err
		}
		var planned []Berth
		rep, planned, err = planImport(d, rows, &in, settings.PriceClassRule)
		if err != nil {
			return err
		}
		if in.DryRun {
			return nil
		}
		if len(planned) == 0 {
			return fieldError("csv", "no_valid_rows")
		}
		req := d.Request()
		req.Berths = append(slices.Clone(req.Berths), planned...)
		res, err := s.applyDocument(ctx, tx, p, scope, m, ifMatch, &req, "map.berth_import", event.ChangeUpdated)
		if err != nil {
			return err
		}
		rep.Document = res
		return nil
	}
	if in.DryRun {
		err = s.d.DB.ReadTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			m, err := store.GetMap(ctx, tx, scope, mapID)
			if err != nil {
				return err
			}
			return run(ctx, tx, m)
		})
	} else {
		err = s.d.DB.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
			m, err := store.LockMap(ctx, tx, scope, mapID)
			if err != nil {
				return err
			}
			if m.Version != ifMatch {
				return ErrVersionMismatch
			}
			return run(ctx, tx, m)
		})
	}
	if err != nil {
		return nil, err
	}
	return rep, nil
}

func cell(rec []string, i *int) (string, bool) {
	if i == nil {
		return "", false
	}
	if *i >= len(rec) {
		return "", false
	}
	return strings.TrimSpace(rec[*i]), true
}

// planImport parses the rows and places the valid ones along their structures.
func planImport(d *Document, rows [][]string, in *BerthImport, rule layout.PriceClassRule) (*ImportReport, []Berth, error) {
	structs := map[string]*layout.Structure{}
	for _, st := range d.Structures {
		if st.Kind != string(layout.StructurePier) && st.Kind != string(layout.StructureQuay) {
			continue
		}
		ls := &layout.Structure{Ref: layout.Ref(st.ID), Kind: layout.StructureKind(st.Kind), Code: st.Code, Points: st.Points, Width: geo.Cm(st.WidthCm)}
		for _, side := range st.Sides {
			ls.Sides = append(ls.Sides, layout.Side(side))
		}
		structs[st.Code] = ls
	}
	taken := map[string]bool{}
	for _, b := range d.Berths {
		taken[b.Code] = true
	}
	for _, a := range d.DynamicAreas {
		taken[a.Code] = true
	}
	for _, l := range d.LandResources {
		taken[l.Code] = true
	}
	rep := &ImportReport{RowsTotal: len(rows), Rejected: []ImportRejected{}, Duplicates: []ImportDuplicate{}, Planned: []ImportPlanned{}}
	first := 1
	if in.HasHeader {
		first = 2
	}
	m := in.Mapping
	inFile := map[string]bool{}
	var valid []*importRow
	for i, rec := range rows {
		r := &importRow{row: first + i}
		bad := func(field, code string) { r.errors = append(r.errors, ImportFieldError{field, code}) }
		code, _ := cell(rec, &m.Code)
		if !layout.ValidCode(code) {
			bad("code", "invalid")
		}
		r.code = code
		pier, _ := cell(rec, &m.Pier)
		r.pier = pier
		if r.structure = structs[pier]; r.structure == nil {
			bad("pier", "unknown_structure")
		}
		if v, _ := cell(rec, &m.Length); true {
			if n, ok := parseLength(v, in.Units); ok {
				r.length = n
			} else {
				bad("length", "invalid")
			}
		}
		if v, _ := cell(rec, &m.Width); true {
			if n, ok := parseLength(v, in.Units); ok {
				r.width = n
			} else {
				bad("width", "invalid")
			}
		}
		if v, ok := cell(rec, m.Depth); ok && v != "" {
			if n, ok := parseLength(v, in.Units); ok {
				r.depth = n
			} else {
				bad("depth", "invalid")
			}
		}
		if v, ok := cell(rec, m.PriceClass); ok && v != "" {
			if layout.ValidCode(v) {
				r.priceClass = v
			} else {
				bad("price_class", "invalid")
			}
		}
		if v, ok := cell(rec, m.MooringType); ok && v != "" {
			if slices.Contains(enumOf(layout.MooringTypes), v) && v != string(layout.MooringBuoy) {
				r.mooring = v
			} else {
				bad("mooring_type", "invalid")
			}
		}
		if in.SidePolicy == "column" {
			v, _ := cell(rec, m.Side)
			v = strings.ToLower(v)
			if slices.Contains([]string{"left", "right", "head"}, v) {
				r.side = v
			} else {
				bad("side", "invalid")
			}
		}
		if len(r.errors) > 0 {
			rep.Rejected = append(rep.Rejected, ImportRejected{Row: r.row, Errors: r.errors})
			continue
		}
		switch {
		case inFile[code]:
			rep.Duplicates = append(rep.Duplicates, ImportDuplicate{Row: r.row, Code: code, Reason: "code_in_file"})
			continue
		case taken[code]:
			inFile[code] = true
			rep.Duplicates = append(rep.Duplicates, ImportDuplicate{Row: r.row, Code: code, Reason: "code_on_map"})
			continue
		}
		inFile[code] = true
		valid = append(valid, r)
	}
	// Placement: one cursor per structure side, starting after the existing berths or at the root.
	type cursorKey struct{ structure, side string }
	cursors := map[cursorKey]geo.Cm{}
	positions := map[cursorKey]int64{}
	alternate := map[string]int{}
	for _, b := range d.Berths {
		k := cursorKey{b.StructureID, b.Side}
		positions[k] = max(positions[k], b.Position+10)
		if in.Start != "after_existing" || b.StructureID == "" {
			continue
		}
		for _, st := range structs {
			if string(st.Ref) == b.StructureID {
				end := layout.ProjectOnStructure(*st, b.Anchor) + geo.Cm((b.WidthCm+1)/2)
				if b.Side == string(layout.SideHead) {
					end = 0 // head slots run across the head; not mixed with side slots
				}
				cursors[k] = max(cursors[k], end)
			}
		}
	}
	var planned []Berth
	for _, r := range valid {
		st := r.structure
		side := r.side
		switch in.SidePolicy {
		case "left", "right":
			side = in.SidePolicy
		case "alternate":
			if st.Kind == layout.StructureQuay {
				side = string(st.Sides[0])
			} else {
				side = []string{"left", "right"}[alternate[r.pier]%2]
				alternate[r.pier]++
			}
		}
		usable := st.Sides
		if st.Kind == layout.StructurePier && len(usable) == 0 {
			usable = []layout.Side{layout.SideLeft, layout.SideRight}
		}
		if !slices.Contains(usable, layout.Side(side)) {
			rep.Rejected = append(rep.Rejected, ImportRejected{Row: r.row, Errors: []ImportFieldError{{"side", "not_usable"}}})
			continue
		}
		k := cursorKey{string(st.Ref), side}
		at := cursors[k]
		total := layout.SupportLength(st, layout.Side(side))
		var rect geo.Rect
		placed := false
		for ; at+geo.Cm(r.width) <= total; at += layout.Snap {
			if rc, err := layout.PlaceAlong(st, layout.Side(side), at, geo.Cm(r.width), geo.Cm(r.length)); err == nil {
				rect, placed = rc, true
				break
			}
		}
		if !placed {
			rep.Rejected = append(rep.Rejected, ImportRejected{Row: r.row, Errors: []ImportFieldError{{"pier", "does_not_fit"}}})
			continue
		}
		gap := geo.Cm(0)
		if rect.Rotation%900 != 0 {
			gap = 2
		}
		cursors[k] = at + geo.Cm(r.width) + gap
		pc := r.priceClass
		if pc == "" {
			pc = layout.PriceClassFor(geo.Cm(r.width), rule)
		}
		mooring := r.mooring
		if mooring == "" {
			mooring = string(layout.MooringLine)
		}
		planned = append(planned, Berth{
			Key: fmt.Sprintf("csv:%d", r.row), Code: r.code, StructureID: string(st.Ref), Side: side, Position: positions[k],
			Anchor: rect.Anchor, RotationDd: int64(rect.Rotation), WidthCm: r.width, LengthCm: r.length, MaxLOACm: r.length,
			MaxBeamCm: int64(layout.DefaultMaxBeam(geo.Cm(r.width))), DepthCm: r.depth, MooringType: mooring, PriceClass: pc,
			Attributes: []string{}, Active: true, GroupIDs: []string{},
		})
		positions[k] += 10
		rep.Planned = append(rep.Planned, ImportPlanned{Row: r.row, Code: r.code, Pier: r.pier, Side: side})
	}
	rep.Valid = len(rep.Planned)
	return rep, planned, nil
}
