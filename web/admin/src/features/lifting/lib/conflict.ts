import { toProblem } from '@porto/shared/api';

import { slotRanges } from './slots';

/** Conflict text of a 409 equipment_slot_taken / operator_busy (the slots that are taken). */
export function conflictText(
  t: (k: string, p?: Record<string, unknown>) => string,
  error: unknown,
): string | null {
  const p = toProblem(error);
  if (p.type !== 'equipment_slot_taken' && p.type !== 'operator_busy') return null;
  const slots = Array.isArray(p.details.slots) ? (p.details.slots as number[]) : [];
  return t(`lifting:conflict.${p.type}`, { slots: slotRanges(slots).join(', ') });
}
