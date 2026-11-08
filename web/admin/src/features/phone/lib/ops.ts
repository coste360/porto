// Queued operations of the phone screens (m6b-plan §4.2, §4.3): pure builders of the NewOp records handed to
// the offline queue (queue first, D2: online too), the "In coda" overlay of today's rows, and the reasons a
// control is unavailable offline. The queue (keys, ordering, replay) belongs to @porto/shared/offline.
import type { Movement, Schemas } from '@porto/shared/api';
import type { NewOp, OfflineReasonCode, QueuedOp } from '@porto/shared/offline';

const API = '/api/admin/v1/marinas';

export type PresenceEvent = 'arrived' | 'departed';

/** Who and where: every op is tagged with the signed-in user and the current marina (INV-84). */
export type OpScope = { userId: string; marinaId: string };

const marinaPath = (marinaId: string) => `${API}/${encodeURIComponent(marinaId)}`;

/**
 * Arrival or departure of a vessel on an existing allocation (the only presences allowed offline, 29.2): the body
 * carries the berth the worker saw, so a moved allocation is a conflict on replay (409 allocation_changed, D5).
 */
export function presenceOp(
  s: OpScope,
  v: {
    vesselId: string;
    vesselName: string;
    event: PresenceEvent;
    allocationId: string;
    resourceId: string;
    notes?: string;
    at: string;
  },
): NewOp {
  const notes = v.notes?.trim();
  const body: Schemas['PresenceCreate'] = {
    vessel_id: v.vesselId,
    event: v.event,
    at: v.at,
    allocation_id: v.allocationId,
    expected_resource_id: v.resourceId,
    ...(notes ? { notes: notes.slice(0, 1000) } : {}),
  };
  return {
    kind: 'presence.record',
    userId: s.userId,
    marinaId: s.marinaId,
    lane: `vessel:${v.vesselId}`,
    request: { operationId: 'recordPresence', path: `${marinaPath(s.marinaId)}/presences`, body },
    label: {
      key: v.event === 'arrived' ? 'offline:queue.label.arrived' : 'offline:queue.label.departed',
      params: { vessel: v.vesselName },
    },
  };
}

/** Photo of a presence queued just before: the presence id comes from the parent's stored result. */
export function presencePhotoOp(
  s: OpScope,
  v: { parentOpId: string; vesselId: string; vesselName: string; blob: Blob },
): NewOp {
  return {
    kind: 'presence.photo',
    userId: s.userId,
    marinaId: s.marinaId,
    lane: `vessel:${v.vesselId}`,
    dependsOn: { opId: v.parentOpId, bind: { presenceId: 'presence.id' } },
    request: {
      operationId: 'uploadPresencePhoto',
      path: `${marinaPath(s.marinaId)}/presences/{presenceId}/photo`,
      contentType: v.blob.type || 'image/jpeg',
    },
    blob: v.blob,
    label: { key: 'offline:queue.label.photo', params: { vessel: v.vesselName } },
  };
}

export type CashSource = Pick<
  Schemas['Collectible'],
  'charge_id' | 'customer_id' | 'residual_cents' | 'fiscal_mode'
>;

/** Cash collection on one existing charge, matched in full or in part (never more than the residual). */
export function cashOp(
  s: OpScope,
  v: {
    charge: CashSource;
    amountCents: number;
    registerId: string;
    receivedOn: string;
    amountLabel: string;
  },
): NewOp {
  const body: Schemas['RecordPayment'] = {
    kind: 'payment',
    method: 'cash',
    amount_cents: v.amountCents,
    received_on: v.receivedOn,
    customer_id: v.charge.customer_id,
    cash_register_id: v.registerId,
    matches: [{ charge_id: v.charge.charge_id, amount_cents: v.amountCents }],
  };
  return {
    kind: 'payment.cash',
    userId: s.userId,
    marinaId: s.marinaId,
    lane: `charge:${v.charge.charge_id}`,
    request: { operationId: 'recordPayment', path: `${marinaPath(s.marinaId)}/payments`, body },
    label: { key: 'offline:queue.label.cash', params: { amount: v.amountLabel } },
    issuesDocument: v.charge.fiscal_mode === 'receipt',
  };
}

/** Why an amount cannot be collected (null when it can). */
export function cashAmountError(amountCents: number | null, residualCents: number): string | null {
  if (amountCents === null || !Number.isSafeInteger(amountCents) || amountCents < 1)
    return 'phone:dock.cash.amountHint';
  if (amountCents > residualCents) return 'phone:dock.cash.amountHint';
  return null;
}

// ---- reasons ----

export type OfflineReason = OfflineReasonCode;

/**
 * Online-only controls of the phone screens and why (m6b-plan §4.2, D7): an unexpected arrival creates a
 * transit, releases and blocks change occupancy, a measurement changes the fit, a receipt-mode collection
 * numbers a commercial document.
 */
export const ONLINE_ONLY: Readonly<Record<string, OfflineReason>> = {
  unexpectedArrival: 'allocation',
  release: 'allocation',
  block: 'allocation',
  measure: 'online_only',
  receiptCollection: 'document',
  manualSearch: 'online_only',
};

/** Why a collectible cannot be collected now (null when it can be queued). */
export function collectReason(
  c: Pick<CashSource, 'fiscal_mode'>,
  online: boolean,
  hasSession: boolean,
): 'document' | 'session' | null {
  if (!hasSession) return 'session';
  if (c.fiscal_mode === 'receipt' && !online) return 'document';
  return null;
}

// ---- overlay of pending presences ----

const OPEN: ReadonlySet<QueuedOp['state']> = new Set([
  'pending',
  'sending',
  'retrying',
  'conflict',
  'blocked',
]);

/** Events still in the queue for each vessel (oldest first): done and expired ops do not count. */
export function queuedPresences(ops: readonly QueuedOp[]): Map<string, PresenceEvent[]> {
  const out = new Map<string, PresenceEvent[]>();
  const sorted = [...ops].sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  for (const op of sorted) {
    if (op.kind !== 'presence.record' || !OPEN.has(op.state)) continue;
    const body = op.request.body as { vessel_id?: string; event?: string } | undefined;
    if (!body?.vessel_id || (body.event !== 'arrived' && body.event !== 'departed')) continue;
    const list = out.get(body.vessel_id) ?? [];
    list.push(body.event);
    out.set(body.vessel_id, list);
  }
  return out;
}

/** Charges with a cash collection still in the queue. */
export function queuedCharges(ops: readonly QueuedOp[]): Set<string> {
  const out = new Set<string>();
  for (const op of ops) {
    if (op.kind !== 'payment.cash' || !OPEN.has(op.state)) continue;
    const body = op.request.body as { matches?: { charge_id: string }[] } | undefined;
    for (const m of body?.matches ?? []) out.add(m.charge_id);
  }
  return out;
}

/**
 * The one-tap action of a row given what is already queued for its vessel: the section's action when nothing
 * is queued; after a queued arrival the next action is the departure (arrive then depart offline); after a
 * queued departure nothing more for today.
 */
export function nextAction(
  sectionAction: PresenceEvent | null,
  queued: readonly PresenceEvent[] | undefined,
): PresenceEvent | null {
  const last = queued?.[queued.length - 1];
  if (!last) return sectionAction;
  return last === 'arrived' ? 'departed' : null;
}

/** A movement can be recorded from the phone (offline too) only with an allocation and its resource. */
export function canQueuePresence(m: Pick<Movement, 'allocation_id' | 'resource'>): boolean {
  return !!m.allocation_id && !!m.resource?.id;
}
