// Package csvimport is the generic CSV import of M4 (spec 11.6; m4-plan §3.7, §15 D27): reading a CSV text
// (delimiter, header, at most 5 000 rows), the column mapping, the row report (row numbers and codes only, never
// personal data) and the registry of entity handlers. Each entity handler lives in its owner's domain package
// (customers_vessels and deadlines: DOCUMENTS; contracts: CONTRACTS; rights: RIGHTS-CHARGES) and is registered by
// internal/app. A file is imported in one transaction; a dry run writes nothing but the run row.
package csvimport

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/store"
)

// MaxRows is the largest file accepted (rows after the header).
const MaxRows = 5000

// Entities.
const (
	EntityCustomersVessels = "customers_vessels"
	EntityContracts        = "contracts"
	EntityRights           = "rights"
	EntityDeadlines        = "deadlines"
)

// Sentinel errors (m4-plan §7.4).
var (
	// ErrCSVInvalid means the text is not a readable CSV, has too many rows or a mapping points outside the
	// columns. 400 csv_invalid.
	ErrCSVInvalid = errors.New("csvimport: invalid csv")
	// ErrNotUndoable means a record created by the run was changed or has dependent rows. 409
	// import_not_undoable.
	ErrNotUndoable = errors.New("csvimport: import not undoable")
	// ErrUnknownEntity means no handler is registered for the entity. 422 validation_failed.
	ErrUnknownEntity = errors.New("csvimport: unknown entity")
)

// Delimiters accepted by the API (ImportInput.delimiter).
var Delimiters = map[string]rune{"semicolon": ';', "comma": ',', "tab": '\t'}

// Table is a parsed file: the header (when present) and the data rows. Row numbers in reports are 1-based file
// lines, counting the header.
type Table struct {
	Header    []string
	Rows      [][]string
	HasHeader bool
}

// Parse reads text with delimiter (0 = detect ';' or ','). It refuses invalid UTF-8, more than MaxRows rows and
// rows of different lengths are padded (the mapping decides what is read).
func Parse(text string, delimiter rune, hasHeader bool) (*Table, error) {
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("%w: not UTF-8", ErrCSVInvalid)
	}
	text = strings.TrimPrefix(text, "\ufeff")
	if delimiter == 0 {
		delimiter = detect(text)
	}
	r := csv.NewReader(bytes.NewReader([]byte(text)))
	r.Comma = delimiter
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	t := &Table{HasHeader: hasHeader}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCSVInvalid, err)
		}
		if blank(rec) {
			continue
		}
		if hasHeader && t.Header == nil {
			t.Header = rec
			continue
		}
		t.Rows = append(t.Rows, rec)
		if len(t.Rows) > MaxRows {
			return nil, fmt.Errorf("%w: more than %d rows", ErrCSVInvalid, MaxRows)
		}
	}
	return t, nil
}

func detect(text string) rune {
	line, _, _ := strings.Cut(text, "\n")
	if strings.Count(line, ";") >= strings.Count(line, ",") {
		return ';'
	}
	return ','
}

func blank(rec []string) bool {
	for _, f := range rec {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

// Mapping maps target field names to 0-based column indexes.
type Mapping map[string]int

// Row is one data row seen through a mapping.
type Row struct {
	Line   int // file line (1-based, header included)
	values []string
	m      Mapping
}

// Get returns the trimmed value of field ("" when unmapped or missing).
func (r Row) Get(field string) string {
	i, ok := r.m[field]
	if !ok || i < 0 || i >= len(r.values) {
		return ""
	}
	return strings.TrimSpace(r.values[i])
}

// Mapped returns the data rows through m. A mapped column beyond the widest row is ErrCSVInvalid.
func (t *Table) Mapped(m Mapping) ([]Row, error) {
	width := len(t.Header)
	for _, r := range t.Rows {
		width = max(width, len(r))
	}
	for f, i := range m {
		if i < 0 || i >= width {
			return nil, fmt.Errorf("%w: field %s maps to column %d of %d", ErrCSVInvalid, f, i, width)
		}
	}
	offset := 1
	if t.HasHeader {
		offset = 2
	}
	out := make([]Row, len(t.Rows))
	for i, r := range t.Rows {
		out[i] = Row{Line: i + offset, values: r, m: m}
	}
	return out, nil
}

// Issue is a rejected row or a suspected duplicate: row, code and an optional field and reference (a code such
// as a berth code or customer code, never personal data).
type Issue struct {
	Row   int    `json:"row"`
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

// Report is the outcome of a dry run or an apply.
type Report struct {
	RunID      string         `json:"run_id,omitempty"`
	Entity     string         `json:"entity"`
	DryRun     bool           `json:"dry_run"`
	Rows       int            `json:"rows"`
	Valid      int            `json:"valid"`
	Created    map[string]int `json:"created,omitempty"`
	Rejected   []Issue        `json:"rejected"`
	Duplicates []Issue        `json:"duplicates"`
}

// Reject records a rejected row.
func (r *Report) Reject(row int, code, field, ref string) {
	r.Rejected = append(r.Rejected, Issue{Row: row, Code: code, Field: field, Ref: ref})
}

// Duplicate records a suspected duplicate (fiscal code, VAT number, registration already present).
func (r *Report) Duplicate(row int, code, ref string) {
	r.Duplicates = append(r.Duplicates, Issue{Row: row, Code: code, Ref: ref})
}

// Created records an entity written by Apply (type and id go to import_run_items for the undo).
type Created struct {
	EntityType string
	EntityID   string
}

// EntityHandler imports one entity (m4-plan §13.3). DryRun validates every row without writing; Apply writes the
// valid rows in tx (the caller rolls back when the report has rejections) and returns what it created; Undo
// removes the created records in tx or returns ErrNotUndoable.
type EntityHandler interface {
	Entity() string
	Fields() []string // target fields of the mapping
	DryRun(ctx context.Context, db bun.IDB, scope store.MarinaScope, rows []Row, rep *Report) error
	Apply(ctx context.Context, tx bun.IDB, scope store.MarinaScope, rows []Row, rep *Report) ([]Created, error)
	Undo(ctx context.Context, tx bun.IDB, scope store.MarinaScope, created []Created) error
}

// Registry holds the entity handlers.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]EntityHandler
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{handlers: map[string]EntityHandler{}} }

// Register adds (or replaces) the handler of its entity.
func (r *Registry) Register(h EntityHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[h.Entity()] = h
}

// Handler returns the handler of entity, or ErrUnknownEntity.
func (r *Registry) Handler(entity string) (EntityHandler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[entity]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownEntity, entity)
	}
	return h, nil
}

// Entities returns the registered entities, sorted.
func (r *Registry) Entities() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for e := range r.handlers {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// ErrNotImplemented is returned by Pending handlers (CONTRACT skeleton) until their owner implements them.
var ErrNotImplemented = errors.New("csvimport: entity handler not implemented")

// Pending is the placeholder handler of an entity whose owner has not implemented the import yet.
type Pending struct{ Name string }

// Entity implements EntityHandler.
func (p Pending) Entity() string { return p.Name }

// Fields implements EntityHandler.
func (Pending) Fields() []string { return nil }

// DryRun implements EntityHandler.
func (Pending) DryRun(context.Context, bun.IDB, store.MarinaScope, []Row, *Report) error {
	return ErrNotImplemented
}

// Apply implements EntityHandler.
func (Pending) Apply(context.Context, bun.IDB, store.MarinaScope, []Row, *Report) ([]Created, error) {
	return nil, ErrNotImplemented
}

// Undo implements EntityHandler.
func (Pending) Undo(context.Context, bun.IDB, store.MarinaScope, []Created) error {
	return ErrNotImplemented
}
