import type { Permission } from '@porto/shared/api';
import type { DaySubject } from '@porto/shared/map';

export type QuickAction =
  | 'newTransit'
  | 'newQuote'
  | 'arrived'
  | 'departed'
  | 'move'
  | 'swap'
  | 'release'
  | 'block'
  | 'unblock'
  | 'openLayout'
  | 'undo';

/** Actions of the quick card for a subject, by permission (spec 9.3, 26). */
export function quickActions(
  s: DaySubject,
  can: (p: Permission) => boolean,
  opts: { hasUndo: boolean },
): QuickAction[] {
  const st = s.subject.status;
  const free = st === 'free' || st === 'released';
  const vessel = s.subject.vessel;
  const inPort = s.subject.presence?.state === 'in_port';
  const out: QuickAction[] = [];
  const area = s.resourceType === 'dynamic_area';
  if (free && !area && can('transits.desk')) out.push('newTransit');
  if (free && !area && can('contracts.write')) out.push('newQuote');
  if (vessel && !inPort && can('presence.write')) out.push('arrived');
  if (vessel && inPort && can('presence.write')) out.push('departed');
  if (s.allocationId && st !== 'block' && st !== 'maintenance' && can('occupancy.move')) {
    out.push('move');
    if (!area) out.push('swap');
  }
  if (st === 'contract' && s.allocationId && can('contracts.write')) out.push('release');
  if (st === 'free' && !area && can('occupancy.move')) out.push('block');
  if (st === 'block' && s.allocationId && can('occupancy.move')) out.push('unblock');
  if (area && can('occupancy.move')) out.push('openLayout');
  if (opts.hasUndo && can('occupancy.move')) out.push('undo');
  return out;
}
