## 6. Schema dati

Convenzioni di Arenile (spec Arenile 6): ogni tabella ha `id` (UUIDv7), `created_at`, `updated_at`; le tabelle
operative hanno `marina_id` indicizzato e lo store lo riceve dal contesto autenticato, mai dal corpo della richiesta.
Misure in cm (`int64`), aree in cm², importi in centesimi, quantità frazionarie in millesimi (`qty.Milli`), date di
calendario `DATE`, istanti UTC. Soft delete solo dove indicato (`archived_at`). Le migrazioni esistono per SQLite e
PostgreSQL con la stessa numerazione.

Lo schema completo nasce con le migrazioni di ogni milestone; qui sono fissate le tabelle portanti e i vincoli che
garantiscono le invarianti.

### 6.1 Organizzazione e marina

| Tabella | Campi principali |
|---|---|
| `organizations` | name, vat_number, fiscal_code, billing_address, sdi_code, pec |
| `marinas` | organization_id, slug (unique), name, kind (`marina`, `yard`, `both`), timezone, currency, locales, contacts, address, geo, concession_ref, concession_expires_on, settings (json versionato), theme, booking_enabled, allowed_embed_origins, archived_at |
| `seasons`, `rate_periods`, `closures` | come Arenile; `closures` con ambito (marina, mappa, struttura, gruppo) |

### 6.2 Mappa

| Tabella | Campi principali | Vincoli |
|---|---|---|
| `maps` | marina_id, kind, name (i18n), width_cm, height_cm, grid_cm, background (json), georef (json), water_polygons, land_polygons, version, sort_order | |
| `harbor_structures` | map_id, kind, code, name, geometry (json: polilinea/poligono in cm), width_cm, attributes (json: depth_profile, floating, sides) | unique(map_id, code) |
| `berths` | marina_id, map_id, structure_id, code, side, position, anchor_x_cm, anchor_y_cm, rotation_dd, width_cm, length_cm, max_loa_cm, max_beam_cm, depth_cm, max_air_draft_cm, mooring_type, swing_radius_cm, pedestal_id, power_a, power_phase, water, price_class, attributes, online_sellable, active | unique(map_id, code) |
| `dynamic_areas` | marina_id, map_id, kind (`linear`, `surface`), code, structure_id, side, from_cm, to_cm, outline (json), mooring_mode, cell_cm, margin_cm, max_loa_cm, max_beam_cm, max_height_cm, depth_profile, blocked_cells (json), exits (json), grid_rotation_dd, cell_count, price_class, active | unique(map_id, code) |
| `land_resources` | marina_id, map_id, kind, code, x_cm, y_cm, rotation_dd, width_cm, length_cm, price_class, attributes, active | unique(map_id, code) |
| `pedestals` | map_id, code, x_cm, y_cm, sockets (json), meters (json) | unique(map_id, code) |
| `map_decorations` | map_id, kind, asset, variant, geometry, label (i18n) | |
| `berth_groups`, `berth_group_members` | name, code, kind; member (resource_type, resource_id) | unique(group_id, resource_type, resource_id) |

### 6.3 Occupazione

| Tabella | Campi principali | Vincoli |
|---|---|---|
| `allocations` | marina_id, contract_line_id null, rental_booking_id null, vessel_id, resource_type, resource_id, x_cm, y_cm, rotation_dd, first_night, last_night, kind, expires_at, fit_override (json null), moved_from_id | indice (resource_type, resource_id, first_night) |
| `berth_occupancies` | berth_id, night, allocation_id, kind, expires_at | **unique(berth_id, night)** |
| `cell_occupancies` | area_id, cell, night, allocation_id, kind, expires_at | **unique(area_id, cell, night)** |
| `land_occupancies` | resource_id, night, allocation_id, kind, expires_at | **unique(resource_id, night)** |
| `releases` | allocation_id, from_night, to_night, source, resold_allocation_id, credit_cents | |
| `presences` | vessel_id, allocation_id null, event (`arrived`, `departed`, `hauled_out`, `launched`), at, source (`staff`, `sensor`, `customer`), user_id, notes | |
| `move_undo_log` | come Arenile | |
| `equipment_slots`, `rental_slots` | resource_id, date, slot, owner_id, kind | **unique(resource_id, date, slot)** |

### 6.4 Clienti, barche, documenti

`customers` (persona o società, fiscali, SdI, consensi, categoria, condizioni di pagamento, IBAN), `vessels`
(dimensioni dichiarate e misurate, `dimensions_source`, tipo, motori, bandiera, matricola), `vessel_parties`
(vessel_id, customer_id, role, share_millis), `documents` (owner_type, owner_id, type, number, issued_on,
expires_on, file_ref cifrato), `deadlines` (source_type, source_id,
due_on, kind, notified_at[]).

### 6.5 Catalogo, listini, contratti

`items`, `revenue_classes`, `price_lists`, `price_rules` (basis, ambito, importi, modificatori, priorità),
`quotes` (+ `quote_options`), `contracts` (kind, term, number, year, status, customer_id, billing_customer_id,
vessel_id, charter_fleet_id, period, policy (json), price_snapshot (json), engine_version, version),
`contract_lines`, `installments` (contract_id, seq, issue_on, due_on, amount_cents, status, charge_id),
`renewal_campaigns`, `renewal_proposals`, `waitlist_entries`; vincolo unique(marina_id, kind, year, number).

### 6.6 Diritti d'uso e condominio

`berth_rights` (berth_id, kind, from, to, title (json)), `berth_right_holders` (right_id, customer_id,
share_millis), `right_transfers`, `management_mandates`, `mandate_revenues`, `management_fee_tables`,
`management_fee_runs`, `millesimal_tables` + `millesimal_entries` (table_id, berth_id, millis),
`common_charge_budgets` + `common_charge_items`, `common_charge_allocations`.

### 6.7 Documenti, firma, addebiti e incassi

`templates` (+ `template_versions`), `generated_documents` (sha256, template_version_id, source),
`signature_requests` (+ `signature_signers`), `charges` (source_type, source_id, customer_id, customer fiscal
snapshot, due_on, total_cents, paid_amount_cents, payment_status, invoicing_status, external_invoice_ref,
corrects_charge_id), `charge_lines` (item_id, qty_milli, unit_cents, vat_rate, vat_nature, revenue_class,
service_from, service_to), `charge_exports` (+ righe esportate), `payments` e `refunds` (come Arenile),
`payment_matches` (payment_id, charge_id, amount_cents), `customer_credits`, `cash_sessions`, `reminder_runs`,
`fiscal_documents` (come Arenile). Vincoli: somma degli abbinamenti ≤ importo del pagamento e ≤ residuo
dell'addebito (UPDATE condizionale, INV-52); `paid_amount` coerente con pagamenti e rimborsi (INV-50).

### 6.8 Rimessaggio, cantiere, noleggio, manutenzione, sensori

`storage_scenarios` (+ `scenario_moves`), `work_packages` (+ righe con formule), `formula_variables`, `yard_quotes`,
`work_orders`, `work_tasks`, `employees`, `employee_calendars`, `employee_absences`, `task_assignments`,
`timesheets`, `warehouses`, `stock_items`, `stock_movements`, `equipment`, `rental_units`, `rental_bookings`,
`rental_handovers`, `maintenance_plans`, `maintenance_tasks`, `meter_readings`, `sensor_devices`,
`sensor_readings`, `sensor_state_changes`, `marina_profile`.

### 6.9 Piattaforma

Come Arenile: `users`, `memberships`, `sessions`, `api_keys`, `devices`, `pairing_codes`, `idempotency_keys`,
`audit_log`, `outbox_events`, `webhook_endpoints`, `webhook_deliveries`, `message_log`, `consents`.
