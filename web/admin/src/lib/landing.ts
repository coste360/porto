import type { Device, Permission } from '@porto/shared/api';

/**
 * Where a just-paired browser device lands (m6a-plan §4.1, seam for the M6b phone screens): a phone whose user
 * works the quay (presence or map) opens the dock panel, everything else opens the marina overview. Paths are
 * relative to the admin base (`/admin`).
 */
export function landingFor(
  permissions: readonly Permission[],
  kind: Device['kind'],
  marinaId: string,
): string {
  const id = encodeURIComponent(marinaId);
  const dock = permissions.includes('presence.write') || permissions.includes('map.view');
  if (kind === 'mobile' && dock) return `/m/${id}/dock`;
  return `/m/${id}`;
}
