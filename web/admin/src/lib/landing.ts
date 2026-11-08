import type { Device, Permission } from '@porto/shared/api';

/**
 * Where a phone lands (m6b-plan §6.1, spec 31.3): after pairing (device kind `mobile`) and after sign-in on a
 * viewport narrower than 768 px (DashboardPage passes `mobile`). Desktops always open the marina overview. Paths
 * are relative to the admin base (`/admin`).
 *
 * | Permissions (first match)                         | Roles today                      | Phone landing        |
 * |---------------------------------------------------|----------------------------------|----------------------|
 * | presence.write, or map.view with transits.desk    | owner, director, office, dock    | /m/<id>/dock         |
 * | reports.all or marina.configure                   | accounting, viewer               | /m/<id>/direction    |
 * | anything else                                     | yard_mgr, yard, rental           | /m/<id> (seam)       |
 *
 * Seam, filled by later milestones (each adds its row above the fallback, before the direction row when the
 * role also holds reports.all):
 * - M9 storage (storage.manage without presence.write): /m/<id>/storage-phone; maintenance-only users
 *   (maintenance.report): /m/<id>/maintenance-phone.
 * - M10 yard (yard.timesheets, yard.manage): /m/<id>/yard-phone.
 * - M11 rentals (rental.manage without presence.write): /m/<id>/rental-phone.
 * Owners and directors hold presence.write and land on the dock; the dock's "Altro" tab links the direction
 * screen for them.
 */
export function landingFor(
  permissions: readonly Permission[],
  kind: Device['kind'],
  marinaId: string,
): string {
  const id = encodeURIComponent(marinaId);
  const base = `/m/${id}`;
  if (kind !== 'mobile') return base;
  const has = (p: Permission) => permissions.includes(p);
  if (has('presence.write') || (has('map.view') && has('transits.desk'))) return `${base}/dock`;
  if (has('reports.all') || has('marina.configure')) return `${base}/direction`;
  return base;
}

/** Whether a user may open the direction phone screen (dashboard, map, deadlines). */
export function canUseDirection(permissions: readonly Permission[]): boolean {
  return permissions.includes('reports.all') || permissions.includes('marina.configure');
}

/** Whether a user may open the dock phone screen. */
export function canUseDock(permissions: readonly Permission[]): boolean {
  return permissions.includes('presence.write') || permissions.includes('map.view');
}
