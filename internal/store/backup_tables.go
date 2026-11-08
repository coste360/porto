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
		// M8 online booking (00016_online_booking). online_bookings.inquiry_id points to an older inquiry; channel
		// reservations reference their contract. Portal links and sessions and channel events are excluded below.
		"online_booking_settings",
		"booking_texts",
		"online_bookings",
		"online_booking_documents",
		"online_quota_usage",
		"online_quota_claims",
		"channel_connections",
		"channel_reservations",
		// M9 storage (00018_storage) and maintenance and utilities (00019_maintenance_utilities). scenario_items and
		// scenario_moves follow their scenario; maintenance_tasks.follows_task_id points to an older task (UUIDv7
		// order); utility_billings.charge_id points to an older charge, so neither needs deferral.
		"storage_contract_details",
		"storage_scenarios",
		"scenario_items",
		"scenario_moves",
		"storage_movements",
		"equipment",
		"utility_meters",
		"maintenance_plans",
		"maintenance_plan_targets",
		"maintenance_tasks",
		"maintenance_task_files",
		"utility_settings",
		"meter_readings",
		"utility_billings",
		"utility_consumptions",
		// M10 yard (00020_yard_catalog_quotes, 00021_work_orders, 00022_equipment_slots). yard_quotes.work_order_id
		// (variation) is a plain column validated in Go; work_order_billings.charge_id points to an older charge;
		// timesheets.voids_id points to an older entry (UUIDv7 order). work_package_versions are insert-only (the
		// restore only inserts).
		"yard_settings",
		"work_types",
		"yard_qualifications",
		"yard_item_settings",
		"formula_variables",
		"formula_tables",
		"work_packages",
		"work_package_versions",
		"yard_quotes",
		"yard_quote_lines",
		"work_orders",
		"work_order_variations",
		"work_order_lines",
		"work_tasks",
		"work_task_deps",
		"work_task_files",
		"employees",
		"employee_qualifications",
		"employee_calendars",
		"employee_absences",
		"task_assignments",
		"timesheets",
		"stock_movements",
		"work_order_external_costs",
		"work_order_billings",
		"equipment_operators",
		"equipment_bookings",
		"equipment_slots",
		"operator_slots",
	}
}

// BackupExcludedTables are never backed up: short-lived state that is useless (or unsafe) on another instance.
func BackupExcludedTables() []string {
	// move_undo_log (M3): undo entries live 24 h and refer to the moment of the move; restoring them is useless.
	// signature_otps (M4): 10-minute codes, useless after a restore (signers request a new one).
	// payment_provider_events (M5): dedupe cache of provider events; after a restore provider retries are harmless
	// because payments and refunds are unique by provider reference.
	// customer_portal_links, customer_portal_sessions (M8): short-lived customer credentials, like sessions.
	// channel_events (M8): dedupe cache of channel events; channel_reservations stay unique by external reference.
	return []string{
		"sessions", "idempotency_keys", "goose_db_version", "move_undo_log", "signature_otps", "payment_provider_events",
		"customer_portal_links", "customer_portal_sessions", "channel_events",
	}
}
