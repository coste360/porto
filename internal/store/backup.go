package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/coste360/porto/internal/migrations"
)

// ColumnKind is the portable type of a column in a backup.
type ColumnKind string

// Column kinds. Values are written to backups as: text and date as strings, int as numbers, bool as booleans,
// time as RFC 3339 UTC strings, json as JSON values, bytes as base64 strings.
const (
	KindText  ColumnKind = "text"
	KindInt   ColumnKind = "int"
	KindBool  ColumnKind = "bool"
	KindTime  ColumnKind = "time"
	KindDate  ColumnKind = "date"
	KindJSON  ColumnKind = "json"
	KindBytes ColumnKind = "bytes"
)

// Column is one column of a table.
type Column struct {
	Name string     `json:"name"`
	Kind ColumnKind `json:"kind"`
}

// SQLite stores booleans as INTEGER and JSON as TEXT; these lists give them their portable kind. They must match
// the BOOLEAN and JSONB columns of the PostgreSQL schema (TestBackupColumns_DialectsAgree).
var (
	sqliteBoolColumns = []string{
		"marinas.booking_enabled", "users.active", "sessions.totp_verified", "webhook_endpoints.active",
		// M1 (00002_harbor, 00003_registry)
		"berths.overhang_allowed", "berths.water", "berths.wifi", "berths.online_sellable", "berths.active",
		"dynamic_areas.active", "land_resources.active", "consents.granted",
		// M2 (00004_pricing)
		"pricing_settings.default_price_includes_vat", "items.price_includes_vat", "items.active",
		"items.online_visible", "price_rules.stackable", "price_rules.active",
		"price_list_items.price_includes_vat", "price_list_items.active",
		// M3 (00005_contracts, 00006_occupancy)
		"occupancy_settings.reprice_on_move", "quote_options.hold", "contracts.hold_lost", "contracts.signed_on_paper",
		"contracts.pay_at_departure",
	}
	sqliteJSONColumns = []string{
		"organizations.billing_address", "marinas.locales", "marinas.contacts", "marinas.address", "marinas.geo",
		"marinas.settings", "marinas.theme", "marinas.allowed_embed_origins", "api_keys.scopes", "audit_log.diff",
		"outbox_events.payload", "webhook_endpoints.events",
		// M1 (00002_harbor, 00003_registry)
		"harbor_settings.price_class_rule", "maps.name", "maps.background", "maps.georef", "maps.concession_outline",
		"maps.water_polygons", "maps.land_polygons", "harbor_structures.geometry", "harbor_structures.attributes",
		"pedestals.sockets", "pedestals.meters", "berths.attributes", "dynamic_areas.outline", "dynamic_areas.supports",
		"dynamic_areas.depth_profile", "dynamic_areas.blocked_cells", "dynamic_areas.exits", "land_resources.attributes",
		"map_decorations.geometry", "map_decorations.label", "berth_groups.name", "customers.billing_address",
		"customers.emails", "customers.phones", "customers.tags", "customers.payment_terms", "vessels.engines",
		"deadlines.notified_at", "deadline_policies.lead_days",
		// M2 (00004_pricing)
		"revenue_classes.name", "items.name", "items.description", "berth_categories.name",
		"berth_categories.applies_to", "price_lists.name", "price_lists.contract_types", "price_lists.settings",
		"rate_periods.name", "rate_periods.ranges", "price_rules.body",
		// M3 (00005_contracts, 00006_occupancy)
		"quotes.policy", "quote_options.price_snapshot", "quote_options.fit_override", "contracts.price_snapshot",
		"contracts.policy", "allocations.fit_override", "move_undo_log.payload",
	}
)

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// TableColumns returns the columns of table in declaration order with their portable kinds.
func TableColumns(ctx context.Context, db bun.IDB, d Dialect, table string) ([]Column, error) {
	var cols []Column
	if d == migrations.Postgres {
		rows, err := db.QueryContext(ctx, `SELECT column_name, data_type FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? ORDER BY ordinal_position`, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, typ string
			if err := rows.Scan(&name, &typ); err != nil {
				return nil, err
			}
			cols = append(cols, Column{Name: name, Kind: postgresKind(typ)})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	} else {
		rows, err := db.QueryContext(ctx, "SELECT name, type FROM pragma_table_info(?) ORDER BY cid", table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var name, typ string
			if err := rows.Scan(&name, &typ); err != nil {
				return nil, err
			}
			k := sqliteKind(typ)
			switch full := table + "." + name; {
			case slices.Contains(sqliteBoolColumns, full):
				k = KindBool
			case slices.Contains(sqliteJSONColumns, full):
				k = KindJSON
			}
			cols = append(cols, Column{Name: name, Kind: k})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("store: table %s not found", table)
	}
	return cols, nil
}

func postgresKind(t string) ColumnKind {
	switch strings.ToLower(t) {
	case "boolean":
		return KindBool
	case "integer", "bigint", "smallint":
		return KindInt
	case "jsonb", "json":
		return KindJSON
	case "bytea":
		return KindBytes
	case "date":
		return KindDate
	case "timestamp with time zone", "timestamp without time zone":
		return KindTime
	default:
		return KindText
	}
}

func sqliteKind(t string) ColumnKind {
	switch strings.ToUpper(t) {
	case "INTEGER", "BIGINT":
		return KindInt
	case "BLOB":
		return KindBytes
	case "DATE":
		return KindDate
	case "TIMESTAMP", "DATETIME":
		return KindTime
	default:
		return KindText
	}
}

// CountRows returns the number of rows of table.
func CountRows(ctx context.Context, db bun.IDB, table string) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(table)).Scan(&n)
	return n, err
}

// dumpOrder is the ORDER BY of the tables without an id column (M3): their primary key.
var dumpOrder = map[string]string{
	"occupancy_settings": "marina_id",
	"number_sequences":   "marina_id, series, year",
	"berth_occupancies":  "berth_id, night",
	"cell_occupancies":   "area_id, cell, night",
	"land_occupancies":   "resource_id, night",
	"vessel_presence":    "vessel_id",
}

// DumpTable calls fn with every row of table (ordered by id, or by its primary key when it has no id), converted
// to portable values (see ColumnKind).
func DumpTable(ctx context.Context, db bun.IDB, table string, cols []Column, fn func(row []any) error) error {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = quoteIdent(c.Name)
	}
	order := "id"
	if o, ok := dumpOrder[table]; ok {
		order = o
	}
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(names, ", ")+" FROM "+quoteIdent(table)+" ORDER BY "+order)
	if err != nil {
		return err
	}
	defer rows.Close()
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		out := make([]any, len(cols))
		for i, c := range cols {
			v, err := exportValue(c.Kind, raw[i])
			if err != nil {
				return fmt.Errorf("store: %s.%s: %w", table, c.Name, err)
			}
			out[i] = v
		}
		if err := fn(out); err != nil {
			return err
		}
	}
	return rows.Err()
}

var timeLayouts = []string{
	time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999", "2006-01-02",
}

func parseTime(s string) (time.Time, error) {
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsable time %q", s)
}

func asString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []byte:
		return string(x), true
	}
	return "", false
}

func exportValue(k ColumnKind, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch k {
	case KindInt:
		switch x := v.(type) {
		case int64:
			return x, nil
		case int32:
			return int64(x), nil
		case int:
			return int64(x), nil
		}
	case KindBool:
		switch x := v.(type) {
		case bool:
			return x, nil
		case int64:
			return x != 0, nil
		}
		if s, ok := asString(v); ok {
			return s == "t" || s == "true" || s == "1", nil
		}
	case KindTime:
		if t, ok := v.(time.Time); ok {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
		if s, ok := asString(v); ok {
			t, err := parseTime(s)
			if err != nil {
				return nil, err
			}
			return t.Format(time.RFC3339Nano), nil
		}
	case KindDate:
		if t, ok := v.(time.Time); ok {
			return t.Format("2006-01-02"), nil
		}
		if s, ok := asString(v); ok && len(s) >= 10 {
			return s[:10], nil
		}
	case KindJSON:
		if s, ok := asString(v); ok {
			if !json.Valid([]byte(s)) {
				return nil, errors.New("invalid JSON")
			}
			return json.RawMessage(s), nil
		}
	case KindBytes:
		switch x := v.(type) {
		case []byte:
			return slices.Clone(x), nil
		case string:
			return []byte(x), nil
		}
	default:
		if s, ok := asString(v); ok {
			return s, nil
		}
		return fmt.Sprint(v), nil
	}
	return nil, fmt.Errorf("unexpected %T for kind %s", v, k)
}

// ImportValue converts a portable value (as decoded from JSON with UseNumber) into a query argument for a
// column of kind k.
func ImportValue(k ColumnKind, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch k {
	case KindInt:
		if n, ok := v.(json.Number); ok {
			return n.Int64()
		}
	case KindBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case KindTime:
		if s, ok := v.(string); ok {
			return parseTime(s)
		}
	case KindDate, KindText:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case KindJSON:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return string(b), nil
	case KindBytes:
		if b, ok := v.([]byte); ok {
			return b, nil
		}
	}
	return nil, fmt.Errorf("unexpected %T for kind %s", v, k)
}

// InsertRow inserts one row of portable-converted arguments.
func InsertRow(ctx context.Context, db bun.IDB, table string, cols []string, args []any) error {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quoteIdent(c)
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(args)), ", ")
	all := append([]any{bun.Ident(table), bun.Safe(strings.Join(quoted, ", "))}, args...)
	_, err := db.NewRaw("INSERT INTO ? (?) VALUES ("+marks+")", all...).Exec(ctx)
	return err
}

// SetColumnByID sets one column of the row with the given id (used by restore for self-referencing keys).
func SetColumnByID(ctx context.Context, db bun.IDB, table, column, id string, value any) error {
	_, err := db.NewRaw("UPDATE ? SET ? = ? WHERE id = ?", bun.Ident(table), bun.Ident(column), value, id).Exec(ctx)
	return err
}
