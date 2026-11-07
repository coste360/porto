import { useDesktopStatus } from '@porto/shared/api';

import { useMe } from '../../auth/hooks';
import { ownedOrganizations } from '../../auth/me';

/** Byte size for the backup list ("12,3 MB"), decimal units like the operating systems' file managers. */
export function formatBytes(bytes: number, locale = 'it'): string {
  const units = ['byte', 'kilobyte', 'megabyte', 'gigabyte'] as const;
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit += 1;
  }
  return new Intl.NumberFormat(locale, {
    style: 'unit',
    unit: units[unit],
    unitDisplay: 'short',
    maximumFractionDigits: unit === 0 ? 0 : 1,
  }).format(value);
}

/**
 * Desktop administration is owner only and loopback only (D19): `loopback` is true in the app window of the
 * desktop edition (the status endpoint answers there only), `owner` when the user owns an organization.
 */
export function useDesktopAccess() {
  const status = useDesktopStatus();
  const { data: me } = useMe();
  const owner = ownedOrganizations(me).length > 0;
  return {
    status: status.data ?? null,
    resolving: status.resolving,
    loopback: status.loopback,
    owner,
    admin: status.loopback && owner,
  };
}
