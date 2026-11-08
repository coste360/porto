// PURE: pending ops overlaid on the cached day data (m6b-plan §4.3): a movement row whose vessel has a queued
// arrival or departure shows "In coda" and its next action, so that an arrival then a departure can be recorded
// offline; a charge with a queued cash collection shows "In coda" and its residual net of what is queued.
import type { QueuedOp } from './types';

export type QueuedEvent = 'arrived' | 'departed';

const OPEN: ReadonlySet<QueuedOp['state']> = new Set([
  'pending',
  'sending',
  'retrying',
  'conflict',
  'blocked',
]);

export type Overlay = {
  /** Queued presence events per vessel, oldest first. */
  presences: Map<string, QueuedEvent[]>;
  /** Cash queued per charge (cents). */
  cash: Map<string, number>;
};

/** Builds the overlay of the open ops of a marina. */
export function overlayOf(ops: readonly QueuedOp[], marinaId: string): Overlay {
  const presences = new Map<string, QueuedEvent[]>();
  const cash = new Map<string, number>();
  const sorted = [...ops]
    .filter((o) => o.marinaId === marinaId && OPEN.has(o.state))
    .sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  for (const op of sorted) {
    if (op.kind === 'presence.record') {
      const b = op.request.body as { vessel_id?: unknown; event?: unknown } | undefined;
      if (typeof b?.vessel_id !== 'string') continue;
      if (b.event !== 'arrived' && b.event !== 'departed') continue;
      presences.set(b.vessel_id, [...(presences.get(b.vessel_id) ?? []), b.event]);
    } else if (op.kind === 'payment.cash') {
      const b = op.request.body as
        { matches?: { charge_id?: unknown; amount_cents?: unknown }[] } | undefined;
      for (const m of b?.matches ?? []) {
        if (typeof m.charge_id !== 'string' || typeof m.amount_cents !== 'number') continue;
        cash.set(m.charge_id, (cash.get(m.charge_id) ?? 0) + m.amount_cents);
      }
    }
  }
  return { presences, cash };
}

/** The presence state of a vessel after its queued events (undefined: nothing queued). */
export function stateAfterQueued(
  events: readonly QueuedEvent[] | undefined,
): 'in_port' | 'out' | undefined {
  const last = events?.[events.length - 1];
  if (!last) return undefined;
  return last === 'arrived' ? 'in_port' : 'out';
}

/** Residual of a charge net of the cash already queued on it (never below zero). */
export function residualAfterQueued(chargeId: string, residualCents: number, o: Overlay): number {
  return Math.max(0, residualCents - (o.cash.get(chargeId) ?? 0));
}
