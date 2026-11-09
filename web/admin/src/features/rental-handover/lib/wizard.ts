// Step logic of the rental check-out and check-in wizards (m11-plan §7, §8). Pure: the server's `missing` list of
// the handover draft is the truth (the same list as 422 handover_incomplete), so a step is done exactly when the
// server would accept it; the commit button is enabled only when nothing is missing.
import type { RentalBooking, RentalHandover, RentalPhase } from '@porto/shared/api';

export type CheckOutStep = 'identity' | 'licence' | 'contract' | 'deposit' | 'boat' | 'summary';
export type CheckInStep = 'return' | 'boat' | 'damages' | 'extras' | 'summary';

type Missing = RentalHandover['missing'][number];

/** Steps of the check-out: the licence only when the unit needs one (D7). */
export function checkOutSteps(licenceRequired: boolean): CheckOutStep[] {
  return [
    'identity',
    ...(licenceRequired ? (['licence'] as const) : []),
    'contract',
    'deposit',
    'boat',
    'summary',
  ];
}

export const CHECK_IN_STEPS: readonly CheckInStep[] = [
  'return',
  'boat',
  'damages',
  'extras',
  'summary',
];

/** The missing server steps a wizard step covers. */
const COVERS: Record<CheckOutStep | CheckInStep, readonly Missing[]> = {
  identity: ['identity'],
  licence: ['licence'],
  contract: ['contract'],
  deposit: ['deposit'],
  boat: ['checklist', 'fuel', 'engine_hours', 'photos'],
  summary: [],
  return: [],
  damages: [],
  extras: [],
};

/** True when the server reports nothing missing for the step. */
export function stepDone(step: CheckOutStep | CheckInStep, missing: readonly Missing[]): boolean {
  return COVERS[step].every((m) => !missing.includes(m));
}

/** The first step still to do (the summary when everything is done). */
export function firstOpen<T extends CheckOutStep | CheckInStep>(
  steps: readonly T[],
  missing: readonly Missing[],
): T {
  return steps.find((s) => !stepDone(s, missing)) ?? steps[steps.length - 1]!;
}

/** True when the commit (check-out or check-in) may be sent. */
export function canCommit(missing: readonly Missing[] | undefined): boolean {
  return (missing?.length ?? 1) === 0;
}

/** The deposit retention of a decision (cents): release 0, retain all = the deposit, retain = the typed amount. */
export function retainOf(
  decision: 'release' | 'retain_all' | 'retain',
  deposit: number,
  typed: number,
): number {
  if (decision === 'release') return 0;
  if (decision === 'retain_all') return deposit;
  return Math.max(0, Math.min(typed, deposit));
}

/** Engine hours typed on the keypad ("120,5") as milli-hours, or null. */
export function parseHours(text: string): number | null {
  const t = text.trim().replace(',', '.');
  if (!/^\d{1,6}(\.\d{1,3})?$/.test(t)) return null;
  const [i, f = ''] = t.split('.');
  return Number(i) * 1000 + Number(f.padEnd(3, '0'));
}

/** The booking id of a scanned rental QR (PR1:<id>, D23), or null. */
export function parseRentalQr(text: string): string | null {
  const m = /^PR1:([0-9a-fA-F-]{36})$/.exec(text.trim());
  return m ? m[1]!.toLowerCase() : null;
}

/** The phase whose draft matters for a booking status (the check-out until the boat leaves, then the check-in). */
export function phaseOf(status: RentalBooking['status']): RentalPhase {
  return status === 'confirmed' || status === 'held' ? 'checkout' : 'checkin';
}
