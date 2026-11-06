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
	}
}

// BackupExcludedTables are never backed up: short-lived state that is useless (or unsafe) on another instance.
func BackupExcludedTables() []string {
	return []string{"sessions", "idempotency_keys", "goose_db_version"}
}
