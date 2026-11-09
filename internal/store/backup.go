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
		// M4 (00007_contracts_m4, 00008_rights, 00009_documents)
		"contracts.charter", "contract_settings.marina_countersign", "contract_settings.require_paper_scan",
		"charter_fleet_vessels.active", "berth_right_vessels.is_primary", "management_fee_lines.excluded",
		"templates.is_default", "generated_documents.pdfa", "generated_documents.signed",
		// M5 (00010_charges, 00011_cash, 00012_payments)
		"billing_settings.auto_payment_request", "charge_lines.price_includes_vat", "charge_export_items.is_first",
		"invoice_reconciliations.dry_run", "cash_registers.active", "cash_registers.is_default",
		"payment_provider_settings.enabled", "payment_provider_settings.sepa_enabled", "payment_intent_charges.open_flag",
		// M8 (00016_online_booking)
		"dynamic_areas.online_sellable", "online_booking_settings.late_payment_recovery",
		"online_booking_settings.allow_without_provider", "online_booking_settings.hide_header_in_embed",
		"online_booking_settings.show_feet", "channel_connections.enabled",
		"channel_connections.push_enabled", "channel_connections.push_dirty",
		// M9 (00018_storage, 00019_maintenance_utilities)
		"scenario_items.pinned", "storage_movements.temporary_position", "equipment.active", "utility_meters.active",
		"maintenance_plans.requires_free", "maintenance_plans.active", "maintenance_tasks.out_of_service",
		// M10 (00020_yard_catalog_quotes, 00021_work_orders, 00022_equipment_slots)
		"yard_settings.advance_on_accept", "yard_settings.fixed_includes_external", "yard_settings.workers_issue_materials",
		"yard_settings.lift_request_on_move", "work_types.active", "yard_qualifications.lift_operator",
		"yard_qualifications.active", "yard_item_settings.yard_enabled", "work_packages.active",
		"yard_quote_lines.qty_overridden", "yard_quote_lines.price_overridden", "yard_quote_lines.price_includes_vat",
		"work_order_lines.price_includes_vat", "employees.active", "task_assignments.forced_qualification",
		"task_assignments.overload", "timesheets.late", "stock_movements.price_overridden", "stock_movements.late",
		"work_order_external_costs.billable", "work_order_external_costs.late", "equipment.bookable",
		// M11 (00023_rental_fleet, 00024_rental_bookings)
		"rental_skippers.active", "rental_formulas.active", "rental_extras.active", "rental_bookings.invoice_requested",
		"rental_bookings.allow_outside_hours",
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
		// M4 (00007_contracts_m4, 00008_rights, 00009_documents)
		"deadline_policies.channels", "contract_settings.default_policy", "contract_settings.required_fields",
		"renewal_campaigns.filter", "renewal_campaigns.counts", "contract_addenda.payload", "charter_contracts.out_nights",
		"waitlist_entries.group_ids", "concessions.surfaces", "berth_rights.title", "berth_right_vessels.fit",
		"berth_right_vessels.fit_override", "right_transfers.to_holders", "right_transfers.requirement_issues",
		"management_mandates.channels", "management_fee_runs.plan", "management_fee_runs.totals",
		"management_fee_lines.holders", "common_charge_runs.totals", "common_charge_runs.plan", "template_versions.page",
		"comm_campaigns.audience", "comm_campaigns.channels", "comm_campaigns.attachments", "comm_campaigns.counts",
		"import_runs.mapping", "import_runs.report",
		// M5 (00010_charges, 00011_cash, 00012_payments)
		"billing_settings.reminder_policy", "billing_settings.arrears_policy", "charge_exports.filters",
		"charge_exports.totals", "invoice_reconciliations.report", "charge_imports.mapping", "charge_imports.report",
		"cash_sessions.expected", "cash_sessions.counted", "payments.vat_split", "fiscal_documents.lines",
		"fiscal_documents.vat", "payment_provider_settings.methods", "reminder_runs.counts",
		// M8 (00016_online_booking)
		"online_booking_settings.closed_weekdays", "online_booking_settings.cancellation_tiers",
		"online_booking_settings.approval", "online_booking_settings.required_fields",
		"online_booking_settings.arrival_info", "booking_texts.texts", "channel_connections.category_map",
		// M9 (00018_storage, 00019_maintenance_utilities)
		"storage_scenarios.score", "storage_scenarios.base", "maintenance_plans.checklist", "maintenance_tasks.checklist",
		"maintenance_tasks.checklist_results", "maintenance_tasks.allocation_ids", "meter_readings.anomaly_codes",
		// M10 (00020_yard_catalog_quotes, 00021_work_orders)
		"yard_settings.test_vessel", "work_types.name", "yard_qualifications.name", "yard_item_settings.test_case",
		"formula_variables.enum_values", "formula_variables.label", "formula_tables.label", "formula_tables.band_rows",
		"work_packages.name", "work_package_versions.lines", "work_package_versions.job_vars", "yard_quotes.job_inputs",
		"yard_quotes.vessel_overrides", "yard_quotes.snapshot", "yard_quotes.estimated_hours", "yard_quote_lines.task",
		"yard_quote_lines.issues", "work_orders.rate_snapshot", "work_tasks.checklist", "work_order_billings.requested",
		// M11 (00023_rental_fleet, 00024_rental_bookings, 00025_rental_handovers)
		"rental_settings.bands", "rental_settings.checklist", "rental_settings.deposit_modes", "rental_formulas.name",
		"rental_formulas.categories", "rental_extras.categories", "rental_bookings.price_snapshot",
		"rental_handovers.checklist",
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

// TableExists reports whether table exists (a database at an older schema version lacks the newer tables).
func TableExists(ctx context.Context, db bun.IDB, d Dialect, table string) (bool, error) {
	q := "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?"
	if d == migrations.Postgres {
		q = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?"
	}
	var n int64
	if err := db.QueryRowContext(ctx, q, table).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
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
	// M4
	"contract_settings":       "marina_id",
	"charter_contracts":       "contract_id",
	"berth_right_allocations": "allocation_id",
	"signature_settings":      "marina_id",
	// M5
	"billing_settings":  "marina_id",
	"billing_sequences": "marina_id, series, period",
	"fiscal_sequences":  "marina_id, day",
	// M8
	"online_booking_settings": "marina_id",
	"online_quota_usage":      "group_id, night",
	"online_quota_claims":     "contract_id, group_id, night",
	// M9
	"storage_contract_details": "contract_id",
	"scenario_items":           "scenario_id, allocation_id",
	"scenario_moves":           "scenario_id, seq",
	"maintenance_plan_targets": "plan_id, element_type, element_id",
	"utility_settings":         "marina_id",
	// M10
	"yard_settings":           "marina_id",
	"yard_item_settings":      "item_id",
	"work_task_deps":          "task_id, depends_on_task_id",
	"employee_qualifications": "employee_id, qualification_id",
	"employee_calendars":      "employee_id, weekday",
	"equipment_operators":     "equipment_id, employee_id",
	"equipment_slots":         "equipment_id, date, slot",
	"operator_slots":          "employee_id, date, slot",
	// M11
	"rental_settings":        "marina_id",
	"rental_skipper_hours":   "skipper_id, weekday, from_slot",
	"rental_slots":           "unit_id, date, slot",
	"rental_skipper_slots":   "skipper_id, date, slot",
	"rental_booking_charges": "booking_id, charge_id",
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
