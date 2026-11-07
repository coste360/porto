import type { Schemas } from '@porto/shared/api';

type Kind = Schemas['ContractKind'];
type Status = Schemas['ContractStatus'];

/** i18n key of a contract or transit status (transits have their own labels). */
export function statusKey(kind: Kind, status: Status): string {
  return kind === 'transit'
    ? `occupancyEnums:transitStatus.${status}`
    : `occupancyEnums:contractStatus.${status}`;
}

/** Badge variant of a status: live states stand out, closed ones are muted. */
export function statusVariant(status: Status): 'default' | 'secondary' | 'outline' | 'destructive' {
  switch (status) {
    case 'active':
    case 'confirmed':
    case 'arrived':
      return 'default';
    case 'draft':
    case 'held':
      return 'outline';
    case 'no_show':
    case 'cancelled':
    case 'terminated':
      return 'destructive';
    default:
      return 'secondary';
  }
}

/** Actions allowed on a transit in M3 (m3-plan §4.2), for the buttons of the detail page and the planner. */
export function transitActions(status: Status, today: string, arrival: string) {
  return {
    confirm: status === 'held',
    cancel: status === 'held' || status === 'confirmed',
    noShow: status === 'confirmed' && today >= arrival,
    stay: status === 'held' || status === 'confirmed' || status === 'arrived',
    arrivalEditable: status === 'held' || status === 'confirmed',
  };
}

/** Actions allowed on a mooring contract in M3 (m3-plan §4.1). */
export function contractActions(status: Status) {
  return {
    activate: status === 'draft',
    cancel: status === 'draft',
    terminate: status === 'active',
  };
}
