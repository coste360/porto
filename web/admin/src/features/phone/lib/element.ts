import type { MapSelection } from '../components/DayMap';
import type { PresenceEvent } from './ops';

/** The presence action of a map element: departure when the vessel is in port, else arrival. */
export function elementPresenceAction(sel: MapSelection): PresenceEvent | null {
  const s = sel.subject.subject;
  if (!s.vessel || !sel.subject.allocationId) return null;
  if (
    s.status === 'free' ||
    s.status === 'block' ||
    s.status === 'closed' ||
    s.status === 'maintenance'
  )
    return null;
  return s.presence?.state === 'in_port' ? 'departed' : 'arrived';
}
