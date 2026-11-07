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
		// M3 contracts and occupancy (00005_contracts, 00006_occupancy). inquiries.quote_id/contract_id and
		// quotes.accepted_option_id/contract_id point forward: restore sets them after every table is loaded
		// (internal/backup, lateColumns); allocations.moved_from_id is a deferred self reference.
		"occupancy_settings",
		"number_sequences",
		"inquiries",
		"quotes",
		"quote_options",
		"contracts",
		"contract_lines",
		"allocations",
		"quote_option_placements",
		"berth_occupancies",
		"cell_occupancies",
		"land_occupancies",
		"releases",
		"presences",
		"vessel_presence",
		"closures",
		// M4 contracts, rights and documents (00007_contracts_m4, 00008_rights, 00009_documents).
		// contracts.signed_document_id, contracts.signature_request_id, addenda/transfers document ids and
		// renewal_proposals.link_id are plain columns; renewal_proposals.new_contract_id and
		// generated_documents.signed_from_id are deferred FKs. millesimal_tables, management_fee_runs and
		// common_charge_runs are restored as drafts and get their status at the end (internal/backup, lateColumns),
		// because the triggers freeze the entries, lines and allocations of active tables and issued runs.
		"contract_settings",
		"charter_fleets",
		"charter_fleet_vessels",
		"charter_contracts",
		"installments",
		"renewal_campaigns",
		"renewal_proposals",
		"contract_addenda",
		"waitlist_entries",
		"concessions",
		"concession_obligations",
		"berth_rights",
		"berth_right_holders",
		"berth_right_vessels",
		"berth_right_allocations",
		"right_transfers",
		"management_mandates",
		"mandate_revenues",
		"management_fee_tables",
		"management_fee_table_rows",
		"management_fee_runs",
		"management_fee_lines",
		"millesimal_tables",
		"millesimal_entries",
		"common_charge_budgets",
		"common_charge_items",
		"common_charge_runs",
		"common_charge_allocations",
		"holder_installments",
		"templates",
		"template_versions",
		"generated_documents",
		"signature_requests",
		"signature_signers",
		"access_links",
		"comm_campaigns",
		"comm_recipients",
		"customer_messages",
		"signature_settings",
		"import_runs",
		"import_run_items",
		// M5 charges, cash and payments (00010_charges, 00011_cash, 00012_payments). charges.first_export_id,
		// export, intent, request, release and mandate references are plain columns; charges.corrects_charge_id always
		// points to an older charge (UUIDv7 order), so it needs no deferral. charges are restored as to_invoice and open and get their invoicing and payment
		// status at the end (internal/backup, lateColumns), because the triggers freeze the lines of exported
		// charges and every column of cancelled ones (INV-51).
		"billing_settings",
		"charge_origin_items",
		"billing_sequences",
		"charges",
		"charge_lines",
		"charge_offsets",
		"charge_exports",
		"charge_export_items",
		"invoice_reconciliations",
		"charge_imports",
		"cash_registers",
		"cash_sessions",
		"cash_movements",
		"fiscal_sequences",
		"payments",
		"payment_matches",
		"refunds",
		"fiscal_documents",
		"payment_provider_settings",
		"payment_intents",
		"payment_intent_charges",
		"sepa_mandates",
		"payment_requests",
		"payment_request_charges",
		"reminder_runs",
		"reminder_items",
		"dunning_exclusions",
		"work_item_states",
	}
}

// BackupExcludedTables are never backed up: short-lived state that is useless (or unsafe) on another instance.
func BackupExcludedTables() []string {
	// move_undo_log (M3): undo entries live 24 h and refer to the moment of the move; restoring them is useless.
	// signature_otps (M4): 10-minute codes, useless after a restore (signers request a new one).
	// payment_provider_events (M5): dedupe cache of provider events; after a restore provider retries are harmless
	// because payments and refunds are unique by provider reference.
	return []string{
		"sessions", "idempotency_keys", "goose_db_version", "move_undo_log", "signature_otps", "payment_provider_events",
	}
}
