package store

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// NewID returns a new UUIDv7 as a lowercase string. UUIDv7 ids sort by creation time.
func NewID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// ValidID reports whether s is a canonical UUID string. Lookups by an id that came from a request check it first:
// PostgreSQL rejects malformed uuids with an error, and the answer must be "not found" on both dialects.
func ValidID(s string) bool {
	if len(s) != 36 {
		return false
	}
	_, err := uuid.Parse(s)
	return err == nil
}

// UTC normalises an instant for storage: UTC with microsecond precision (what PostgreSQL keeps), so a value read
// back equals the value written on both dialects.
func UTC(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC().Truncate(time.Microsecond)
}

// JSON is a JSON document stored as TEXT (SQLite) or jsonb (PostgreSQL).
type JSON []byte

// Value implements driver.Valuer.
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

// Scan implements sql.Scanner.
func (j *JSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = JSON(v)
	default:
		return fmt.Errorf("store: cannot scan %T into JSON", src)
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (j *JSON) UnmarshalJSON(b []byte) error {
	*j = append((*j)[:0], b...)
	return nil
}

// MustJSON marshals v; it panics on values that cannot be marshalled (programming error).
func MustJSON(v any) JSON {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("store: marshal json: %v", err))
	}
	return b
}

// Compact returns the JSON in compact form, so that both dialects hand back the same bytes (jsonb reformats).
func (j JSON) Compact() JSON {
	if len(j) == 0 {
		return j
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, j); err != nil {
		return j
	}
	return buf.Bytes()
}

// Strings is a list of strings stored as a JSON array.
type Strings []string

// Value implements driver.Valuer.
func (s Strings) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(s))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (s *Strings) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		*s = Strings{}
		return nil
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("store: cannot scan %T into Strings", src)
	}
	var out []string
	if err := json.Unmarshal(b, &out); err != nil {
		return fmt.Errorf("store: scan Strings: %w", err)
	}
	if out == nil {
		out = []string{}
	}
	*s = out
	return nil
}

// OrgScope is proof that the caller is allowed to act on one organization. Only ResolveOrgScope builds it.
type OrgScope struct{ orgID string }

// ID returns the organization id.
func (s OrgScope) ID() string { return s.orgID }

// MarinaScope is proof that the caller is allowed to act on one (non-archived) marina. Only the Resolve
// functions build it; auth.Principal.Authorize is their only caller on request paths.
type MarinaScope struct{ marinaID, orgID string }

// ID returns the marina id.
func (s MarinaScope) ID() string { return s.marinaID }

// OrgID returns the id of the marina's organization.
func (s MarinaScope) OrgID() string { return s.orgID }

// IsZero reports whether the scope is empty.
func (s MarinaScope) IsZero() bool { return s.marinaID == "" }

// ResolveMarinaScope returns the scope of an active marina, or ErrNotFound when it does not exist or is archived.
func ResolveMarinaScope(ctx context.Context, db bun.IDB, marinaID string) (MarinaScope, error) {
	return resolveMarina(ctx, db, marinaID, false)
}

// SystemMarinaScope returns the scope of a marina even when archived. It is for background jobs (event fan-out,
// deliveries) that act on rows already written for that marina, never for request paths.
func SystemMarinaScope(ctx context.Context, db bun.IDB, marinaID string) (MarinaScope, error) {
	return resolveMarina(ctx, db, marinaID, true)
}

func resolveMarina(ctx context.Context, db bun.IDB, marinaID string, includeArchived bool) (MarinaScope, error) {
	if !ValidID(marinaID) {
		return MarinaScope{}, ErrNotFound
	}
	var row struct {
		ID             string    `bun:"id"`
		OrganizationID string    `bun:"organization_id"`
		ArchivedAt     time.Time `bun:"archived_at,nullzero"`
	}
	err := db.NewSelect().Table("marinas").Column("id", "organization_id", "archived_at").
		Where("id = ?", marinaID).Limit(1).Scan(ctx, &row)
	if err != nil {
		return MarinaScope{}, notFound(err)
	}
	if !includeArchived && !row.ArchivedAt.IsZero() {
		return MarinaScope{}, ErrNotFound
	}
	return MarinaScope{marinaID: row.ID, orgID: row.OrganizationID}, nil
}

// ResolveOrgScope returns the scope of an existing organization.
func ResolveOrgScope(ctx context.Context, db bun.IDB, orgID string) (OrgScope, error) {
	if !ValidID(orgID) {
		return OrgScope{}, ErrNotFound
	}
	var id string
	err := db.NewSelect().Table("organizations").Column("id").Where("id = ?", orgID).Limit(1).Scan(ctx, &id)
	if err != nil {
		return OrgScope{}, notFound(err)
	}
	return OrgScope{orgID: id}, nil
}

// Page is one page of a cursor-paginated list.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// Pagination limits (spec 27, m0-plan §4.1).
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// ClampLimit applies the default and the maximum page size.
func ClampLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultLimit
	case n > MaxLimit:
		return MaxLimit
	default:
		return n
	}
}

// ErrInvalidCursor is returned for a cursor that this package did not produce.
var ErrInvalidCursor = errors.New("store: invalid cursor")

// cursorPrefix versions the opaque cursor format.
const cursorPrefix = "c1:"

// EncodeCursor makes an opaque cursor from the id of the last row of a page.
func EncodeCursor(lastID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + lastID))
}

// DecodeCursor returns the id carried by a cursor ("" for an empty cursor).
func DecodeCursor(c string) (string, error) {
	if c == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", ErrInvalidCursor
	}
	id, ok := strings.CutPrefix(string(b), cursorPrefix)
	if !ok || !ValidID(id) {
		return "", ErrInvalidCursor
	}
	return id, nil
}

// pageOf trims a result fetched with limit+1 rows into a Page.
func pageOf[T any](rows []T, limit int, idOf func(T) string) Page[T] {
	if len(rows) > limit {
		rows = rows[:limit]
		return Page[T]{Items: rows, NextCursor: EncodeCursor(idOf(rows[len(rows)-1]))}
	}
	if rows == nil {
		rows = []T{}
	}
	return Page[T]{Items: rows}
}

func toInt(n int64, err error) (int, error) { return int(n), err }
