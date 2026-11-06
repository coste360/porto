package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"github.com/uptrace/bun"
)

// Queries of the harbour map tables (m1-plan §3). Every function is scoped by the marina. The element tables
// are written only by SaveMapElements, and SaveMapElements is called only by harbor.(*Service).applyDocument
// (INV-04, single write path; TestHarbor_SingleWritePath).

// ListMaps returns the maps of a marina by sort order.
func ListMaps(ctx context.Context, db bun.IDB, s MarinaScope) ([]Map, error) {
	out := []Map{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).OrderExpr("sort_order, id").Scan(ctx)
	return out, err
}

// GetMap returns one map of a marina.
func GetMap(ctx context.Context, db bun.IDB, s MarinaScope, id string) (*Map, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(Map)
	if err := db.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// LockMap returns a map and locks its row until the end of the transaction (PostgreSQL SELECT … FOR UPDATE).
// On SQLite every write transaction is already exclusive (BEGIN IMMEDIATE behind the write queue).
func LockMap(ctx context.Context, tx bun.IDB, s MarinaScope, id string) (*Map, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	m := new(Map)
	q := tx.NewSelect().Model(m).Where("marina_id = ?", s.ID()).Where("id = ?", id)
	if IsPostgres(tx) {
		q = q.For("UPDATE")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, notFound(err)
	}
	return m, nil
}

// InsertMap inserts a map.
func InsertMap(ctx context.Context, db bun.IDB, s MarinaScope, m *Map) error {
	m.MarinaID = s.ID()
	_, err := db.NewInsert().Model(m).Exec(ctx)
	return err
}

// NextMapSortOrder returns 1 + the highest sort order of the marina's maps (0 without maps).
func NextMapSortOrder(ctx context.Context, db bun.IDB, s MarinaScope) (int, error) {
	var n sql.NullInt64
	err := db.NewSelect().Table("maps").ColumnExpr("MAX(sort_order)").Where("marina_id = ?", s.ID()).Scan(ctx, &n)
	if err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return int(n.Int64) + 1, nil
}

// UpdateMap writes the metadata of a map and bumps its version when the stored version equals expected; else
// ErrVersionMismatch (ErrNotFound when the map does not exist).
func UpdateMap(ctx context.Context, db bun.IDB, s MarinaScope, m *Map, expected int) error {
	res, err := db.NewUpdate().Model(m).
		Column("kind", "name", "icon", "width_cm", "height_cm", "grid_cm", "background_file_id", "background", "georef",
			"concession_outline", "water_polygons", "land_polygons", "updated_at").
		Set("version = version + 1").
		Where("marina_id = ?", s.ID()).Where("id = ?", m.ID).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetMap(ctx, db, s, m.ID); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	m.Version = expected + 1
	return nil
}

// SetMapSortOrders sets sort_order = index for the given map ids (reorder; versions are unchanged).
func SetMapSortOrders(ctx context.Context, db bun.IDB, s MarinaScope, ids []string, at time.Time) error {
	for i, id := range ids {
		res, err := db.NewUpdate().Table("maps").Set("sort_order = ?", i).Set("updated_at = ?", at).
			Where("marina_id = ?", s.ID()).Where("id = ?", id).Exec(ctx)
		if err := expectOne(res, err); err != nil {
			return err
		}
	}
	return nil
}

// DeleteMap deletes a map (its elements and memberships cascade) when its version equals expected.
func DeleteMap(ctx context.Context, db bun.IDB, s MarinaScope, id string, expected int) error {
	res, err := db.NewDelete().Table("maps").Where("marina_id = ?", s.ID()).Where("id = ?", id).Where("version = ?", expected).Exec(ctx)
	if err != nil {
		return err
	}
	if affected(res) == 0 {
		if _, err := GetMap(ctx, db, s, id); err != nil {
			return err
		}
		return ErrVersionMismatch
	}
	return nil
}

// MapCounts are the element counts of one map.
type MapCounts struct {
	Structures, Berths, DynamicAreas, LandResources, Pedestals, Decorations int
}

// Occupiable returns the number of berths, dynamic areas and land resources.
func (c MapCounts) Occupiable() int { return c.Berths + c.DynamicAreas + c.LandResources }

// CountMapElements returns the element counts of every map of a marina (mapID "" = all maps).
func CountMapElements(ctx context.Context, db bun.IDB, s MarinaScope, mapID string) (map[string]MapCounts, error) {
	out := map[string]MapCounts{}
	for _, t := range []struct {
		table string
		set   func(c *MapCounts, n int)
	}{
		{"harbor_structures", func(c *MapCounts, n int) { c.Structures = n }},
		{"berths", func(c *MapCounts, n int) { c.Berths = n }},
		{"dynamic_areas", func(c *MapCounts, n int) { c.DynamicAreas = n }},
		{"land_resources", func(c *MapCounts, n int) { c.LandResources = n }},
		{"pedestals", func(c *MapCounts, n int) { c.Pedestals = n }},
		{"map_decorations", func(c *MapCounts, n int) { c.Decorations = n }},
	} {
		var rows []struct {
			MapID string `bun:"map_id"`
			N     int    `bun:"n"`
		}
		q := db.NewSelect().Table(t.table).Column("map_id").ColumnExpr("COUNT(*) AS n").Where("marina_id = ?", s.ID()).Group("map_id")
		if mapID != "" {
			q = q.Where("map_id = ?", mapID)
		}
		if err := q.Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			c := out[r.MapID]
			t.set(&c, r.N)
			out[r.MapID] = c
		}
	}
	return out, nil
}

// MapElements is the stored state of the elements of one map.
type MapElements struct {
	Structures    []HarborStructure
	Pedestals     []Pedestal
	Berths        []Berth
	Areas         []DynamicArea
	LandResources []LandResource
	Decorations   []MapDecoration
}

// LoadMapElements returns every element of a map, ordered by id within each kind.
func LoadMapElements(ctx context.Context, db bun.IDB, s MarinaScope, mapID string) (*MapElements, error) {
	e := &MapElements{
		Structures: []HarborStructure{}, Pedestals: []Pedestal{}, Berths: []Berth{}, Areas: []DynamicArea{},
		LandResources: []LandResource{}, Decorations: []MapDecoration{},
	}
	for _, dst := range []any{&e.Structures, &e.Pedestals, &e.Berths, &e.Areas, &e.LandResources, &e.Decorations} {
		if err := db.NewSelect().Model(dst).Where("marina_id = ?", s.ID()).Where("map_id = ?", mapID).OrderExpr("id").Scan(ctx); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// SaveSummary counts the rows a SaveMapElements changed, per kind.
type SaveSummary struct {
	Added, Updated, Removed map[string]int
}

// tableRows abstracts one element table for the diff.
type tableRows struct {
	kind     string
	table    string
	coded    bool
	before   []elementRow
	after    []elementRow
	insertFn func(ctx context.Context, db bun.IDB, rows []elementRow) error
}

type elementRow interface {
	rowID() string
	rowCode() string
}

func (r *HarborStructure) rowID() string   { return r.ID }
func (r *HarborStructure) rowCode() string { return r.Code }
func (r *Pedestal) rowID() string          { return r.ID }
func (r *Pedestal) rowCode() string        { return r.Code }
func (r *Berth) rowID() string             { return r.ID }
func (r *Berth) rowCode() string           { return r.Code }
func (r *DynamicArea) rowID() string       { return r.ID }
func (r *DynamicArea) rowCode() string     { return r.Code }
func (r *LandResource) rowID() string      { return r.ID }
func (r *LandResource) rowCode() string    { return r.Code }
func (r *MapDecoration) rowID() string     { return r.ID }
func (r *MapDecoration) rowCode() string   { return "" }

func ptrs[T any](rows []T) []elementRow {
	out := make([]elementRow, len(rows))
	for i := range rows {
		r, ok := any(&rows[i]).(elementRow)
		if !ok {
			panic("store: not an element row") // the element tables are fixed above
		}
		out[i] = r
	}
	return out
}

// insertChunk is the number of rows per multi-row INSERT (well below SQLite's variable limit).
const insertChunk = 100

func insertRows[T any](ctx context.Context, db bun.IDB, rows []elementRow) error {
	for start := 0; start < len(rows); start += insertChunk {
		end := min(start+insertChunk, len(rows))
		batch := make([]T, 0, end-start)
		for _, r := range rows[start:end] {
			row, ok := any(r).(*T)
			if !ok {
				return errors.New("store: element row of the wrong table")
			}
			batch = append(batch, *row)
		}
		if _, err := db.NewInsert().Model(&batch).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
	}
	return nil
}

// sameRow reports whether two rows hold the same content, ignoring timestamps and JSON formatting.
func sameRow(a, b elementRow) bool {
	va, vb := reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem()
	for i := range va.NumField() {
		f := va.Type().Field(i)
		if f.Name == "BaseModel" || f.Name == "CreatedAt" || f.Name == "UpdatedAt" {
			continue
		}
		x, y := va.Field(i).Interface(), vb.Field(i).Interface()
		if jx, ok := x.(JSON); ok {
			jy, _ := y.(JSON)
			if !bytes.Equal(jx.Compact(), jy.Compact()) {
				return false
			}
			continue
		}
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}

// tempCode is the code a row takes while codes are swapped inside one save: "~" + the last 31 characters of its
// id (unique, and within the 32-character limit of the code columns).
func tempCode(id string) string {
	if len(id) > 31 {
		id = id[len(id)-31:]
	}
	return "~" + id
}

// structureInsertRank orders structure inserts so that parents exist first: other kinds, piers, fingers.
func structureInsertRank(k string) int {
	switch k {
	case "pier":
		return 1
	case "finger":
		return 2
	}
	return 0
}

// SaveMapElements replaces the elements of a map with after: rows only in before are deleted, rows in both are
// updated when they changed, rows only in after are inserted. Codes that change are first renamed to a
// temporary value, so that swaps and renumbering never trip the unique (map_id, code) indexes. Every row is
// forced into the scope's marina and the map. It must be called inside a transaction, after the document was
// validated (ONLY harbor.(*Service).applyDocument calls it).
func SaveMapElements(ctx context.Context, tx bun.IDB, s MarinaScope, mapID string, before, after *MapElements) (SaveSummary, error) {
	sum := SaveSummary{Added: map[string]int{}, Updated: map[string]int{}, Removed: map[string]int{}}
	for i := range after.Structures {
		r := &after.Structures[i]
		r.MarinaID, r.MapID = s.ID(), mapID
	}
	for i := range after.Pedestals {
		after.Pedestals[i].MarinaID, after.Pedestals[i].MapID = s.ID(), mapID
	}
	for i := range after.Berths {
		after.Berths[i].MarinaID, after.Berths[i].MapID = s.ID(), mapID
	}
	for i := range after.Areas {
		after.Areas[i].MarinaID, after.Areas[i].MapID = s.ID(), mapID
	}
	for i := range after.LandResources {
		after.LandResources[i].MarinaID, after.LandResources[i].MapID = s.ID(), mapID
	}
	for i := range after.Decorations {
		after.Decorations[i].MarinaID, after.Decorations[i].MapID = s.ID(), mapID
	}
	sortedStructures := append([]HarborStructure(nil), after.Structures...)
	stableSortBy(sortedStructures, func(r HarborStructure) int { return structureInsertRank(r.Kind) })
	tables := []*tableRows{
		{kind: "structure", table: "harbor_structures", coded: true, before: ptrs(before.Structures), after: ptrs(sortedStructures), insertFn: insertRows[HarborStructure]},
		{kind: "pedestal", table: "pedestals", coded: true, before: ptrs(before.Pedestals), after: ptrs(after.Pedestals), insertFn: insertRows[Pedestal]},
		{kind: "berth", table: "berths", coded: true, before: ptrs(before.Berths), after: ptrs(after.Berths), insertFn: insertRows[Berth]},
		{kind: "dynamic_area", table: "dynamic_areas", coded: true, before: ptrs(before.Areas), after: ptrs(after.Areas), insertFn: insertRows[DynamicArea]},
		{kind: "land_resource", table: "land_resources", coded: true, before: ptrs(before.LandResources), after: ptrs(after.LandResources), insertFn: insertRows[LandResource]},
		{kind: "decoration", table: "map_decorations", before: ptrs(before.Decorations), after: ptrs(after.Decorations), insertFn: insertRows[MapDecoration]},
	}
	type plan struct {
		inserts, updates []elementRow
		renames          []elementRow
	}
	plans := make([]plan, len(tables))
	// 1. Deletes, in reverse dependency order.
	for ti := len(tables) - 1; ti >= 0; ti-- {
		t := tables[ti]
		keep := map[string]elementRow{}
		for _, r := range t.after {
			keep[r.rowID()] = r
		}
		var del []string
		for _, r := range t.before {
			nr, ok := keep[r.rowID()]
			if !ok {
				del = append(del, r.rowID())
				continue
			}
			if !sameRow(r, nr) {
				plans[ti].updates = append(plans[ti].updates, nr)
				if t.coded && r.rowCode() != nr.rowCode() {
					plans[ti].renames = append(plans[ti].renames, nr)
				}
			}
			delete(keep, r.rowID())
		}
		for _, r := range t.after {
			if _, isNew := keep[r.rowID()]; isNew {
				plans[ti].inserts = append(plans[ti].inserts, r)
			}
		}
		for start := 0; start < len(del); start += insertChunk {
			ids := del[start:min(start+insertChunk, len(del))]
			if _, err := tx.NewDelete().Table(t.table).Where("marina_id = ?", s.ID()).Where("map_id = ?", mapID).
				Where("id IN (?)", bun.List(ids)).Exec(ctx); err != nil {
				return sum, err
			}
		}
		if len(del) > 0 {
			sum.Removed[t.kind] = len(del)
		}
	}
	// 2. Temporary codes for the rows whose code changes.
	for ti, t := range tables {
		for _, r := range plans[ti].renames {
			if _, err := tx.NewUpdate().Table(t.table).Set("code = ?", tempCode(r.rowID())).
				Where("marina_id = ?", s.ID()).Where("id = ?", r.rowID()).Exec(ctx); err != nil {
				return sum, err
			}
		}
	}
	// 3. Inserts and updates, in dependency order.
	for ti, t := range tables {
		if len(plans[ti].inserts) > 0 {
			if err := t.insertFn(ctx, tx, plans[ti].inserts); err != nil {
				return sum, err
			}
			sum.Added[t.kind] = len(plans[ti].inserts)
		}
		for _, r := range plans[ti].updates {
			res, err := tx.NewUpdate().Model(r).ExcludeColumn("id", "marina_id", "map_id", "created_at").
				Where("marina_id = ?", s.ID()).Where("map_id = ?", mapID).Where("id = ?", r.rowID()).Exec(ctx)
			if err != nil {
				if IsUniqueViolation(err) {
					return sum, ErrConflict
				}
				return sum, err
			}
			if affected(res) != 1 {
				return sum, ErrNotFound
			}
		}
		if len(plans[ti].updates) > 0 {
			sum.Updated[t.kind] = len(plans[ti].updates)
		}
	}
	return sum, nil
}

func stableSortBy[T any](s []T, key func(T) int) {
	// insertion sort keeps the order of equal keys; inputs are small (structures of one map)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && key(s[j]) < key(s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// BerthFilter narrows ListBerths.
type BerthFilter struct {
	MapID   string
	GroupID string
	Q       string // code substring, case-insensitive
}

// BerthListRow is a berth with the code of its structure.
type BerthListRow struct {
	Berth
	StructureCode string `bun:"structure_code,scanonly"`
}

// ListBerths returns a page of berths of a marina, ordered by id.
func ListBerths(ctx context.Context, db bun.IDB, s MarinaScope, f BerthFilter, cursor string, limit int) (Page[BerthListRow], error) {
	after, err := DecodeCursor(cursor)
	if err != nil {
		return Page[BerthListRow]{}, err
	}
	limit = ClampLimit(limit)
	var rows []BerthListRow
	q := db.NewSelect().Model((*Berth)(nil)).ColumnExpr("berth.*").ColumnExpr("hs.code AS structure_code").
		Join("LEFT JOIN harbor_structures AS hs ON hs.id = berth.structure_id").
		Where("berth.marina_id = ?", s.ID())
	if f.MapID != "" {
		q = q.Where("berth.map_id = ?", f.MapID)
	}
	if f.GroupID != "" {
		q = q.Where("berth.id IN (SELECT resource_id FROM berth_group_members WHERE marina_id = ? AND group_id = ? AND resource_type = 'berth')", s.ID(), f.GroupID)
	}
	if f.Q != "" {
		q = q.Where("LOWER(berth.code) LIKE ?", "%"+escapeLike(f.Q)+"%")
	}
	if after != "" {
		q = q.Where("berth.id > ?", after)
	}
	if err := q.OrderExpr("berth.id").Limit(int64(limit+1)).Scan(ctx, &rows); err != nil {
		return Page[BerthListRow]{}, err
	}
	return pageOf(rows, limit, func(r BerthListRow) string { return r.ID }), nil
}

// escapeLike lower-cases a search string and drops the LIKE wildcards.
func escapeLike(q string) string {
	b := []byte(q)
	out := b[:0]
	for _, c := range b {
		if c == '%' || c == '_' || c == '\\' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

// GroupMembersOfMap returns the group memberships of the elements of a map.
func GroupMembersOfMap(ctx context.Context, db bun.IDB, s MarinaScope, mapID string) ([]BerthGroupMember, error) {
	out := []BerthGroupMember{}
	err := db.NewSelect().Model(&out).Where("marina_id = ?", s.ID()).Where("map_id = ?", mapID).OrderExpr("id").Scan(ctx)
	return out, err
}

// ReplaceGroupMembers replaces the group memberships of the elements of a map.
func ReplaceGroupMembers(ctx context.Context, tx bun.IDB, s MarinaScope, mapID string, members []BerthGroupMember) error {
	if _, err := tx.NewDelete().Table("berth_group_members").Where("marina_id = ?", s.ID()).Where("map_id = ?", mapID).Exec(ctx); err != nil {
		return err
	}
	for i := range members {
		members[i].MarinaID, members[i].MapID = s.ID(), mapID
	}
	for start := 0; start < len(members); start += insertChunk {
		batch := members[start:min(start+insertChunk, len(members))]
		if _, err := tx.NewInsert().Model(&batch).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return ErrConflict
			}
			if IsForeignKeyViolation(err) {
				return ErrNotFound
			}
			return err
		}
	}
	return nil
}
