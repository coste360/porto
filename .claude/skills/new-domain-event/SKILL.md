---
name: new-domain-event
description: Add a Porto domain event - type constant, PII-free payload, outbox write in the same transaction as the change, SSE topic mapping, webhook catalogue and tests. Use when a state change must be observable by SSE clients, webhooks or extensions.
argument-hint: "[entity.verb]"
---
# New domain event: $ARGUMENTS

Read first: `docs/spec/27-api-eventi-estensioni.md` (27.3 catalogue, 27.4 SSE), `docs/m0-plan.md` §4.5 (envelope),
`.claude/rules/security.md`, `docs/architecture.md` (outbox flow).

1. **Name.** `entity.verb` in the past tense, lower snake case (`member.added`, `contract.signed`). If it is in
   the 27.3 catalogue use that exact name; otherwise propose it in the spec first.
2. **Type.** Add the constant and the `data` struct in `internal/domain/event`. `data` holds ids, enums, dates
   and integer amounts only: never names, emails, phones, addresses, documents, tokens or secrets (INV-70).
   Bump `version` only for breaking changes of `data`.
3. **Write.** In the domain service, inside the same `store.RunInTx` as the change and the audit row, call the
   outbox writer with `marina_id` from the scope. Never publish directly to the broker or a webhook.
4. **SSE.** Map the type to its topic (`marina.changed`, `team.changed`, later `occupancy.changed`, ...). SSE
   messages stay light: `{topic, type, marina_id, entity_ids}`; the client refetches.
5. **Webhooks.** Add the type to the subscribable list (OpenAPI enum + validation, `unknown_event_type`
   otherwise), then `make gen`.
6. **Tests.**
   - the service writes exactly one outbox row with the right type and `marina_id`, in the same transaction
     (rollback ⇒ no row);
   - add the sample to the table walked by `TestEvents_DataHasNoPII`;
   - dispatcher delivers it to SSE subscribers of that marina only, and a webhook delivery verifies with
     `Porto-Signature`.
7. Update `CHANGELOG.md` and the event list in `docs/architecture.md`. `make check`.
