// Resolution of a scanned transit QR (m6b-plan §6.2 Scansiona): first from today's data already on the phone
// (movements and the day view, both cached offline), then, online only, from the contract itself.
import type { DayElementData, Movement, Movements } from '@porto/shared/api';

import type { PresenceEvent } from './ops';

/** Sections of getMovements in the order a scan looks them up, with the one-tap presence of each. */
export const SCAN_SECTIONS: { key: keyof Omit<Movements, 'date'>; action: PresenceEvent | null }[] =
  [
    { key: 'arrivals_expected', action: 'arrived' },
    { key: 'departures_expected', action: 'departed' },
    { key: 'vessels_out', action: 'arrived' },
    { key: 'arrived', action: 'departed' },
    { key: 'arrived_without_booking', action: null },
    { key: 'departed', action: null },
  ];

export type ScanHit = {
  contractId: string;
  code?: string;
  movement?: Movement;
  /** Section of the movement (the default action comes from it). */
  section?: keyof Omit<Movements, 'date'>;
  action: PresenceEvent | null;
  element?: DayElementData;
};

/** The transit of a scanned contract id in today's movements and day view, or null when it is not there. */
export function resolveFromDay(
  contractId: string,
  movements: Movements | undefined,
  elements: readonly DayElementData[] | undefined,
): ScanHit | null {
  const id = contractId.toLowerCase();
  const element = elements?.find((e) => e.contract?.id.toLowerCase() === id);
  if (movements) {
    for (const { key, action } of SCAN_SECTIONS) {
      const m = movements[key].find((x) => x.contract?.id.toLowerCase() === id);
      if (m)
        return {
          contractId: id,
          ...(m.contract?.code ? { code: m.contract.code } : {}),
          movement: m,
          section: key,
          action,
          ...(element ? { element } : {}),
        };
    }
  }
  if (element) {
    const inPort = element.presence?.state === 'in_port';
    return {
      contractId: id,
      ...(element.contract?.code ? { code: element.contract.code } : {}),
      action: element.allocation_id ? (inPort ? 'departed' : 'arrived') : null,
      element,
    };
  }
  return null;
}
