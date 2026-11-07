package store

// BackupTables lists the tables written by internal/backup, in foreign key order (parents first). Restore inserts
// in this order and wipes in reverse. A new table must be added here, or to BackupExcludedTables.
func BackupTables() []string {
	return []string{
		"organizations",
		"marinas",
		"users",
		"user_totp",
		"recovery_codes",
		"memberships",
		"password_reset_tokens",
		"api_keys",
		"devices",
		"pairing_codes",
		"audit_log",
		"outbox_events",
		"webhook_endpoints",
		"webhook_deliveries",
		"message_log",
		// M1 harbour (00002_harbor)
		"stored_files",
		"harbor_settings",
		"maps",
		"harbor_structures",
		"pedestals",
		"berths",
		"dynamic_areas",
		"land_resources",
		"map_decorations",
		"berth_groups",
		"berth_group_members",
		// M1 registry (00003_registry)
		"customers",
		"consents",
		"vessels",
		"vessel_parties",
		"documents",
		"deadlines",
		"deadline_policies",
		// M2 pricing (00004_pricing). price_lists are restored as drafts and get their real status after every
		// table is in (internal/backup, lateColumns), because the triggers freeze the children of published
		// lists: periods, bands, rules and the item snapshot.
		"pricing_settings",
		"revenue_classes",
		"items",
		"berth_categories",
		"price_lists",
		"rate_periods",
		"price_list_bands",
		"price_rules",
		"price_list_items",
	}
}

// BackupExcludedTables are never backed up: short-lived state that is useless (or unsafe) on another instance.
func BackupExcludedTables() []string {
	return []string{"sessions", "idempotency_keys", "goose_db_version"}
}
